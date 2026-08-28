package tmux

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Control talks to a tmux server over control mode instead of spawning a
// process per query.
//
// Every `tmux <command>` is a fork, an exec, a socket connect and a teardown,
// all serviced by a single-threaded server. Polling a handful of panes that way
// runs to tens of processes a second and has taken the user's tmux down three
// times. Control mode replaces all of it with one long-lived connection:
// commands are written as lines and answered in `%begin`/`%end` blocks, and the
// server pushes `%output`, `%window-add` and friends as they happen — so the
// polling that made the load necessary can go away too.
type Control struct {
	socket  string
	session string

	runMu   sync.Mutex
	mu      sync.Mutex
	stdin   io.WriteCloser
	cmd     *exec.Cmd
	waiters []chan reply
	running bool
	initial chan reply
	failed  error

	// events carries notifications to whoever is watching. It is dropped on
	// the floor when nobody is keeping up, because a stale layout event is
	// worth less than blocking the reader.
	events chan Event

	closeOnce sync.Once
	closed    chan struct{}
}

var controlCommandTimeout = 10 * time.Second

type reply struct {
	out string
	err error
}

// NewControl dials control mode for a socket, attaching to session so that
// session's notifications arrive. Note that tmux scopes %output and most
// layout notifications to the attached session — a single connection cannot
// watch the whole server, which is why activity detection does not rely on it.
func NewControl(socket, session string) (*Control, error) {
	c := &Control{
		socket:  socket,
		session: session,
		events:  make(chan Event, 256),
		closed:  make(chan struct{}),
	}

	if err := c.start(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Control) start() error {
	args := []string{}
	if socketArgsFor(c.socket) != nil {
		args = append(args, socketArgsFor(c.socket)...)
	}
	// ignore-size keeps this client out of the window-size calculation. A
	// control client has no real terminal, and tmux sizes a window to its
	// smallest client — an earlier bug of exactly that shape squashed the
	// user's panes to 10x4.
	args = append(args, "-C", "attach-session", "-f", "ignore-size")
	if c.session != "" {
		args = append(args, "-t", c.session)
	}

	cmd := exec.Command("tmux", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	initial := make(chan reply, 1)
	c.mu.Lock()
	c.cmd, c.stdin, c.initial = cmd, stdin, initial
	c.mu.Unlock()

	go c.read(stdout)

	timer := time.NewTimer(controlCommandTimeout)
	defer timer.Stop()
	select {
	case r := <-initial:
		if r.err != nil {
			c.Close()
			return fmt.Errorf("failed to attach tmux control client: %w", r.err)
		}
		c.mu.Lock()
		if c.failed != nil {
			err := c.failed
			c.mu.Unlock()
			c.Close()
			return fmt.Errorf("tmux control connection ended while attaching: %w", err)
		}
		c.running = true
		c.mu.Unlock()
		return nil
	case <-timer.C:
		c.Close()
		return fmt.Errorf("tmux control attach timed out")
	case <-c.closed:
		return fmt.Errorf("control connection closed while attaching")
	}
}

// read consumes the control stream, routing command output to waiters and
// everything else to events.
func (c *Control) read(stdout io.ReadCloser) {
	defer c.fail(fmt.Errorf("control connection closed"))
	defer close(c.events)

	// Control output can carry a whole pane's scrollback in one block, so the
	// reader needs a buffer far larger than the default.
	r := bufio.NewReaderSize(stdout, 1<<20)

	var (
		inBlock bool
		failed  bool
		block   strings.Builder
	)

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if line == "" {
				return
			}
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case strings.HasPrefix(line, "%begin"):
			inBlock, failed = true, false
			block.Reset()

		case strings.HasPrefix(line, "%end"), strings.HasPrefix(line, "%error"):
			if !inBlock {
				continue
			}
			inBlock = false
			out := block.String()
			block.Reset()

			var e error
			if strings.HasPrefix(line, "%error") || failed {
				e = fmt.Errorf("tmux: %s", strings.TrimSpace(out))
			}
			c.deliver(reply{out: out, err: e})

		case inBlock:
			block.WriteString(line)
			block.WriteByte('\n')

		case strings.HasPrefix(line, "%"):
			c.notify(line)
		}

		if err != nil {
			return
		}
	}
}

// deliver hands a completed block to the command waiting at the front of the
// queue. tmux answers in the order it was asked, so a queue is enough.
func (c *Control) deliver(r reply) {
	c.mu.Lock()
	if c.initial != nil {
		initial := c.initial
		c.initial = nil
		c.mu.Unlock()
		initial <- r
		return
	}
	if len(c.waiters) == 0 {
		c.mu.Unlock()
		return
	}
	w := c.waiters[0]
	c.waiters = c.waiters[1:]
	c.mu.Unlock()

	select {
	case w <- r:
	default:
	}
}

// notify parses a notification line into an event.
func (c *Control) notify(line string) {
	kind, payload := line, ""
	if i := strings.IndexByte(line, ' '); i >= 0 {
		kind, payload = line[:i], line[i+1:]
	}

	select {
	case c.events <- Event{Type: strings.TrimPrefix(kind, "%"), Payload: payload}:
	default: // a dropped notification is better than a stalled reader
	}
}

// fail wakes every pending command so nothing hangs when the connection dies.
func (c *Control) fail(err error) {
	c.mu.Lock()
	waiters := c.waiters
	c.waiters = nil
	initial := c.initial
	c.initial = nil
	c.running = false
	c.failed = err
	c.mu.Unlock()

	if initial != nil {
		select {
		case initial <- reply{err: err}:
		default:
		}
	}
	for _, w := range waiters {
		select {
		case w <- reply{err: err}:
		default:
		}
	}
}

// Alive reports whether the connection is usable.
func (c *Control) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Run issues a command and returns its output, mirroring the process-spawning
// path it replaces so callers need not care which is in use.
func (c *Control) Run(args ...string) (string, error) {
	// tmux itself services commands serially. Keeping only one command in flight
	// prevents browser pollers from turning a slow command into an unbounded
	// control-mode backlog, and makes a timeout unambiguous.
	c.runMu.Lock()
	defer c.runMu.Unlock()

	line := formatCommand(args)
	ch := make(chan reply, 1)

	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return "", fmt.Errorf("control connection not running")
	}
	// The write and the queue push must happen together, or two callers could
	// interleave and each take the other's answer.
	c.waiters = append(c.waiters, ch)
	_, err := io.WriteString(c.stdin, line+"\n")
	c.mu.Unlock()

	if err != nil {
		c.fail(err)
		return "", err
	}

	timer := time.NewTimer(controlCommandTimeout)
	defer timer.Stop()

	select {
	case r := <-ch:
		return r.out, r.err
	case <-timer.C:
		err := fmt.Errorf("tmux control command timed out: %s", line)
		// Once a FIFO reply times out there is no safe way to match subsequent
		// replies to callers. Invalidate the whole stream and let the server dial
		// a fresh one instead of feeding replies to stale waiters.
		c.fail(err)
		c.Close()
		return "", err
	case <-c.closed:
		return "", fmt.Errorf("control connection closed")
	}
}

// Events exposes the notification stream.
func (c *Control) Events() <-chan Event { return c.events }

// Close shuts the connection down.
func (c *Control) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)

		c.mu.Lock()
		stdin, cmd := c.stdin, c.cmd
		c.running = false
		c.mu.Unlock()

		if stdin != nil {
			stdin.Close()
		}
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
}

// formatCommand renders argv as a control-mode command line.
//
// The process path passed argv straight to exec, where the shell was never
// involved. Here the arguments become text that tmux parses itself, so
// anything with a space, a tab or a quote in it — every format string this
// package uses — has to be quoted back into a single token.
func formatCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, quoteArg(a))
	}
	return strings.Join(quoted, " ")
}

// quoteArg wraps a value so tmux reads it as one literal token.
//
// Single quotes are literal in tmux, which is what formats need — `#{...}`
// must not be expanded by the parser. A single quote inside the value has to
// leave the quoted run, escape itself, and start a new one.
func quoteArg(a string) string {
	if a == "" {
		return "''"
	}
	// A literal tab is an argument separator in control-mode command text,
	// even inside quotes on some tmux versions. Formats use tabs as robust field
	// delimiters, so encode them as \t and let tmux's format parser expand them.
	a = strings.ReplaceAll(a, "\t", `\t`)
	if !strings.ContainsAny(a, " \t\n'\"\\$#{}[];") {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// socketArgsFor mirrors Controller.socketArgs for use before a controller
// exists.
func socketArgsFor(socket string) []string {
	switch {
	case socket == "":
		return nil
	case strings.Contains(socket, "/"):
		return []string{"-S", socket}
	default:
		return []string{"-L", socket}
	}
}
