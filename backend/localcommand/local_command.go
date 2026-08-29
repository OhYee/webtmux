package localcommand

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/pkg/errors"
)

const (
	DefaultCloseSignal  = syscall.SIGINT
	DefaultCloseTimeout = 10 * time.Second
)

type LocalCommand struct {
	command string
	argv    []string

	closeSignal  syscall.Signal
	closeTimeout time.Duration

	cmd       *exec.Cmd
	pty       *os.File
	ptyClosed chan struct{}
	startedAt time.Time

	resizeMu   sync.Mutex
	lastWidth  int
	lastHeight int
}

func New(command string, argv []string, headers map[string][]string, options ...Option) (*LocalCommand, error) {
	cmd := exec.Command(command, argv...)

	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Combine headers into key=value pairs to set as env vars
	// Prefix the headers with "http_" so we don't overwrite any other env vars
	// which potentially has the same name and to bring these closer to what
	// a (F)CGI server would proxy to a backend service
	// Replace hyphen with underscore and make them all upper case
	for key, values := range headers {
		h := "HTTP_" + strings.Replace(strings.ToUpper(key), "-", "_", -1) + "=" + strings.Join(values, ",")
		// log.Printf("Adding header: %s", h)
		cmd.Env = append(cmd.Env, h)
	}

	pty, err := pty.Start(cmd)
	if err != nil {
		// todo close cmd?
		return nil, errors.Wrapf(err, "failed to start command `%s`", command)
	}
	ptyClosed := make(chan struct{})

	lcmd := &LocalCommand{
		command: command,
		argv:    argv,

		closeSignal:  DefaultCloseSignal,
		closeTimeout: DefaultCloseTimeout,

		cmd:       cmd,
		pty:       pty,
		ptyClosed: ptyClosed,
		startedAt: time.Now(),
	}

	for _, option := range options {
		option(lcmd)
	}

	// When the process is closed by the user,
	// close pty so that Read() on the pty breaks with an EOF.
	go func() {
		defer func() {
			lcmd.pty.Close()
			close(lcmd.ptyClosed)
		}()

		err := lcmd.cmd.Wait()
		attrs := []any{"command", lcmd.command, "pid", lcmd.cmd.Process.Pid, "duration", time.Since(lcmd.startedAt).Round(time.Millisecond)}
		if lcmd.cmd.ProcessState != nil {
			attrs = append(attrs, "exit_code", lcmd.cmd.ProcessState.ExitCode())
		}
		if err != nil {
			slog.Warn("child process exited", append(attrs, "error", err)...)
		} else {
			slog.Info("child process exited", attrs...)
		}
	}()

	return lcmd, nil
}

func (lcmd *LocalCommand) Read(p []byte) (n int, err error) {
	return lcmd.pty.Read(p)
}

func (lcmd *LocalCommand) Write(p []byte) (n int, err error) {
	return lcmd.pty.Write(p)
}

func (lcmd *LocalCommand) Close() error {
	if lcmd == nil || lcmd.cmd == nil || lcmd.cmd.Process == nil {
		return nil
	}
	_ = lcmd.cmd.Process.Signal(lcmd.closeSignal)
	slog.Debug("sent child close signal", "command", lcmd.command, "pid", lcmd.cmd.Process.Pid, "signal", lcmd.closeSignal)
	timeout := lcmd.closeTimeoutC()
	for {
		select {
		case <-lcmd.ptyClosed:
			return nil
		case <-timeout:
			_ = lcmd.cmd.Process.Signal(syscall.SIGKILL)
			slog.Warn("child close timed out; sent SIGKILL", "command", lcmd.command, "pid", lcmd.cmd.Process.Pid, "timeout", lcmd.closeTimeout)
			timeout = make(chan time.Time)
		}
	}
}

func (lcmd *LocalCommand) WindowTitleVariables() map[string]interface{} {
	return map[string]interface{}{
		"command": lcmd.command,
		"argv":    lcmd.argv,
		"pid":     lcmd.cmd.Process.Pid,
	}
}

func (lcmd *LocalCommand) ResizeTerminal(width int, height int) error {
	if width < 2 || width > 1000 || height < 2 || height > 500 {
		return errors.Errorf("terminal resize outside safe bounds: %dx%d", width, height)
	}
	lcmd.resizeMu.Lock()
	defer lcmd.resizeMu.Unlock()
	if width == lcmd.lastWidth && height == lcmd.lastHeight {
		return nil
	}
	window := pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
		X:    0,
		Y:    0,
	}
	err := pty.Setsize(lcmd.pty, &window)
	if err != nil {
		return err
	}
	lcmd.lastWidth, lcmd.lastHeight = width, height
	return nil
}

func (lcmd *LocalCommand) closeTimeoutC() <-chan time.Time {
	if lcmd.closeTimeout >= 0 {
		return time.After(lcmd.closeTimeout)
	}

	return make(chan time.Time)
}

// Pid returns the process ID of the spawned command, or 0 if it is not
// running. The tmux controller uses it to tell which attached client belongs
// to this connection, so that switching sessions never moves someone else's.
func (lcmd *LocalCommand) Pid() int {
	if lcmd.cmd == nil || lcmd.cmd.Process == nil {
		return 0
	}
	return lcmd.cmd.Process.Pid
}
