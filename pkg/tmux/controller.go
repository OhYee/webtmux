package tmux

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Controller manages tmux interactions for a session
type Controller struct {
	sessionName string
	sessionMu   sync.RWMutex
	// socket selects which tmux server to talk to. A value containing a
	// slash is treated as a socket path (-S), anything else as a socket name
	// (-L). Empty means tmux's default server.
	socket string

	layoutCache *Layout
	layoutMu    sync.RWMutex

	// watch caches pane previews and reader captures across connections.
	watch *WatchTracker

	// control carries commands over a single long-lived connection instead of
	// spawning a process each time. Shared across connections; nil falls back
	// to spawning.
	control *Control
	runner  interface {
		Run(args ...string) (string, error)
	}

	// guard stops this server from overloading tmux. Shared across connections
	// because the ceiling is a property of the tmux server, not of a viewer.
	guard *Guard

	// clientPID identifies the tmux client belonging to this connection, and
	// clientTTY caches what it resolves to.
	clientPID int
	clientTTY string

	// Scroll requests accumulate here so the read loop never waits on tmux.
	scrollMu      sync.Mutex
	scrollPending int
	scrollWake    chan struct{}

	eventChan chan Event
	closeChan chan struct{}
}

// Tests and standalone package users may construct a Controller without the
// server-owned Executor. Keep that compatibility path globally serialized too;
// it must never become a second unbounded process fan-out.
var directTmuxSlot = make(chan struct{}, 1)

// NewController creates a new tmux controller for the given session on the
// given server. Pass an empty socket to use tmux's default server, and a
// tracker shared with every other controller on the same server.
// UseControl routes this controller's commands over a control-mode connection.
func (c *Controller) UseControl(ctl *Control) { c.control = ctl }

// UseRunner routes every command through a shared, recoverable executor.
func (c *Controller) UseRunner(r interface {
	Run(args ...string) (string, error)
}) {
	c.runner = r
}

// UseGuard subjects this controller's commands to a shared rate ceiling.
func (c *Controller) UseGuard(g *Guard) { c.guard = g }

func NewController(sessionName, socket string, watch *WatchTracker) (*Controller, error) {
	if watch == nil {
		watch = NewWatchTracker()
	}

	c := &Controller{
		sessionName: sessionName,
		socket:      socket,
		watch:       watch,
		eventChan:   make(chan Event, 100),
		closeChan:   make(chan struct{}),
		scrollWake:  make(chan struct{}, 1),
	}

	go c.runScrollWorker()

	return c, nil
}

// socketArgs returns the leading tmux flags that pin commands to this
// controller's server. Note that TMUX_TMPDIR is deliberately not used for
// this: it is silently ignored on some builds, which makes any command that
// relies on it — a kill-server above all — land on the user's real server.
func (c *Controller) socketArgs() []string {
	switch {
	case c.socket == "":
		return nil
	case strings.Contains(c.socket, "/"):
		return []string{"-S", c.socket}
	default:
		return []string{"-L", c.socket}
	}
}

// Start initializes the controller and gets initial layout
func (c *Controller) Start() error {
	if err := c.RefreshLayout(); err == nil {
		return nil
	} else if !errors.Is(err, errSessionNotFound) {
		return fmt.Errorf("failed to get initial layout: %w", err)
	}

	session := c.SessionName()
	// An empty or temporarily inconsistent layout is not proof that the
	// session is absent. Control mode can reconnect while tmux is still
	// publishing its initial state; blindly creating here turns that harmless
	// race into "duplicate session" and leaves the browser without a controller.
	if _, err := c.runTmux("has-session", "-t", session); err == nil {
		c.watch.invalidateLayout()
		if retryErr := c.RefreshLayout(); retryErr != nil {
			return fmt.Errorf("tmux session %s exists but its layout is unavailable: %w", session, retryErr)
		}
		return nil
	} else if !strings.Contains(err.Error(), "can't find session") &&
		!strings.Contains(err.Error(), "no server running") {
		return fmt.Errorf("failed to check tmux session %s: %w", session, err)
	}

	if _, err := c.runTmux("new-session", "-d", "-s", session); err != nil {
		return fmt.Errorf("failed to create tmux session %s: %w", session, err)
	}
	c.watch.invalidateLayout()
	return c.RefreshLayout()
}

var errSessionNotFound = errors.New("tmux session not found")

func (c *Controller) SessionName() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionName
}

func (c *Controller) setSessionName(name string) {
	c.sessionMu.Lock()
	c.sessionName = name
	c.sessionMu.Unlock()
}

// Stop closes the controller
func (c *Controller) Stop() error {
	close(c.closeChan)
	return nil
}

// Events returns the channel for tmux events
func (c *Controller) Events() <-chan Event {
	return c.eventChan
}

// GetLayout returns the cached layout
func (c *Controller) GetLayout() *Layout {
	c.layoutMu.RLock()
	defer c.layoutMu.RUnlock()
	return c.layoutCache
}

// layoutFormat carries everything the layout needs in one row per pane, so
// building it costs a single tmux invocation.
//
// Tabs separate the fields because window and session names may contain
// commas, which the earlier format used and silently corrupted.
const layoutFormat = "#{session_id}\t#{session_name}\t#{session_windows}\t#{session_attached}\t" +
	"#{window_id}\t#{window_name}\t#{window_index}\t#{window_active}\t" +
	"#{window_zoomed_flag}\t#{window_bell_flag}\t#{window_activity_flag}\t" +
	"#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t" +
	"#{pane_top}\t#{pane_left}\t#{pane_current_command}\t#{pane_title}"

// RefreshLayout fetches the current tmux layout.
//
// This runs on a timer for every connected client, so its cost is paid
// continuously. It used to issue one call for the session, one for the session
// list, one for the windows and one more per window — around eight processes
// every refresh. At two refreshes a second across a couple of browsers that was
// tens of tmux invocations per second against a single-threaded server, all day.
// One call answers all of it.
func (c *Controller) RefreshLayout() error {
	out, err := c.watch.layoutSnapshot(func() (string, error) {
		return c.runTmux("list-panes", "-a", "-F", layoutFormat)
	})
	if err != nil {
		return err
	}

	layout := &Layout{}
	currentSession := c.SessionName()
	windowAt := map[string]int{}
	seenSession := map[string]bool{}
	nonEmptyRows, parsedRows, notificationRows := 0, 0, 0

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		nonEmptyRows++
		if strings.HasPrefix(line, "%") {
			notificationRows++
		}
		f := splitTmuxFields(line)
		if len(f) < 20 {
			continue
		}
		parsedRows++

		sessionName := f[1]

		// Every session on the server is offered for switching.
		if !seenSession[sessionName] {
			seenSession[sessionName] = true
			windows, _ := strconv.Atoi(f[2])
			layout.Sessions = append(layout.Sessions, Session{
				ID:       f[0],
				Name:     sessionName,
				Windows:  windows,
				Attached: f[3] == "1",
				Active:   sessionName == currentSession,
			})
		}

		// Only the session being viewed contributes windows and panes.
		if sessionName != currentSession {
			continue
		}
		if layout.SessionID == "" {
			layout.SessionID, layout.SessionName = f[0], sessionName
		}

		windowID := f[4]
		idx, ok := windowAt[windowID]
		if !ok {
			windowIndex, _ := strconv.Atoi(f[6])
			active := f[7] == "1"
			layout.Windows = append(layout.Windows, Window{
				ID:       windowID,
				Name:     f[5],
				Index:    windowIndex,
				Active:   active,
				Zoomed:   f[8] == "1",
				Bell:     f[9] == "1",
				Activity: f[10] == "1",
			})
			idx = len(layout.Windows) - 1
			windowAt[windowID] = idx

			if active {
				layout.ActiveWinID = windowID
			}
		}

		paneIndex, _ := strconv.Atoi(f[12])
		width, _ := strconv.Atoi(f[14])
		height, _ := strconv.Atoi(f[15])
		top, _ := strconv.Atoi(f[16])
		left, _ := strconv.Atoi(f[17])
		paneActive := f[13] == "1"

		layout.Windows[idx].Panes = append(layout.Windows[idx].Panes, Pane{
			ID:      f[11],
			Index:   paneIndex,
			Active:  paneActive,
			Width:   width,
			Height:  height,
			Top:     top,
			Left:    left,
			Command: f[18],
			Title:   f[19],
		})

		if paneActive && layout.Windows[idx].Active {
			layout.ActivePaneID = f[11]
		}
	}

	if layout.SessionName == "" {
		return fmt.Errorf("%w: %s (output bytes=%d rows=%d parsed=%d notifications=%d sessions=%d tabs=%d escaped-tabs=%d octal-tabs=%d)",
			errSessionNotFound, currentSession, len(out), nonEmptyRows, parsedRows, notificationRows,
			len(seenSession), strings.Count(out, "\t"), strings.Count(out, `\t`), strings.Count(out, `\011`))
	}

	sort.Slice(layout.Windows, func(i, j int) bool {
		return layout.Windows[i].Index < layout.Windows[j].Index
	})

	c.layoutMu.Lock()
	c.layoutCache = layout
	c.layoutMu.Unlock()

	return nil
}

// SelectPane switches to the specified pane
func (c *Controller) SelectPane(paneID string) error {
	_, err := c.runTmux("select-pane", "-t", paneID)
	if err != nil {
		return err
	}
	c.refreshChanged()
	return nil
}

// isZoomed reports whether the window containing target is currently zoomed.
func (c *Controller) isZoomed(target string) bool {
	out, err := c.runTmux("display-message", "-t", target, "-p", "#{window_zoomed_flag}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

// ZoomPane focuses a single pane by toggling tmux's window zoom.
//
// When zoom is true the pane is made active and its window zoomed, so the pane
// occupies the whole client viewport — this is what mobile clients use to read
// one pane on a small screen. Any existing zoom is cleared first so that
// zooming pane B while pane A is zoomed lands deterministically on B.
//
// An empty paneID targets the current pane of the session.
func (c *Controller) ZoomPane(paneID string, zoom bool) error {
	target := paneID
	if target == "" {
		target = c.SessionName()
	}

	// Clear any existing zoom so select-pane can move freely and so the
	// toggle below starts from a known state.
	if c.isZoomed(target) {
		if _, err := c.runTmux("resize-pane", "-Z", "-t", target); err != nil {
			return err
		}
	}

	if zoom {
		if paneID != "" {
			if _, err := c.runTmux("select-pane", "-t", paneID); err != nil {
				return err
			}
		}
		if _, err := c.runTmux("resize-pane", "-Z", "-t", target); err != nil {
			return err
		}
	}

	c.refreshChanged()
	return nil
}

// GotoPane focuses a pane anywhere on the server, switching session and window
// as needed. The watch board lists every pane regardless of session, so tapping
// a card cannot assume the target is already in view.
func (c *Controller) GotoPane(paneID string) error {
	out, err := c.runTmux("display-message", "-t", paneID, "-p", "#{session_name}\t#{window_id}")
	if err != nil {
		return err
	}
	parts := splitTmuxFields(strings.TrimSpace(out))
	if len(parts) < 2 {
		return fmt.Errorf("could not locate pane %s", paneID)
	}
	session, windowID := parts[0], parts[1]

	if session != c.SessionName() {
		if err := c.switchOurClient(session); err != nil {
			return err
		}
		c.setSessionName(session)
	}
	if _, err := c.runTmux("select-window", "-t", windowID); err != nil {
		return err
	}
	if _, err := c.runTmux("select-pane", "-t", paneID); err != nil {
		return err
	}

	c.refreshChanged()
	return nil
}

// splitTmuxFields accepts both process output (literal tabs) and control-mode
// output, where tmux may encode a tab delimiter as \t or \011.
func splitTmuxFields(line string) []string {
	line = strings.ReplaceAll(line, `\011`, "\t")
	line = strings.ReplaceAll(line, `\t`, "\t")
	return strings.Split(line, "\t")
}

// SelectWindow switches to the specified window
func (c *Controller) SelectWindow(windowID string) error {
	_, err := c.runTmux("select-window", "-t", windowID)
	if err != nil {
		return err
	}
	c.refreshChanged()
	return nil
}

// SwitchSession switches to the specified session
func (c *Controller) SwitchSession(sessionName string) error {
	if err := c.switchOurClient(sessionName); err != nil {
		return err
	}
	c.setSessionName(sessionName)
	c.refreshChanged()
	return nil
}

// SplitPane splits the current pane
func (c *Controller) SplitPane(horizontal bool) error {
	flag := "-v"
	if horizontal {
		flag = "-h"
	}
	_, err := c.runTmux("split-window", "-t", c.SessionName(), flag)
	if err != nil {
		return err
	}
	c.refreshChanged()
	return nil
}

// ClosePane closes the specified pane
func (c *Controller) ClosePane(paneID string) error {
	_, err := c.runTmux("kill-pane", "-t", paneID)
	if err != nil {
		return err
	}
	c.refreshChanged()
	return nil
}

// EnterCopyMode enters copy mode on the active pane
func (c *Controller) EnterCopyMode() error {
	_, err := c.runTmux("copy-mode", "-t", c.SessionName())
	return err
}

// ExitCopyMode exits copy mode
func (c *Controller) ExitCopyMode() error {
	_, err := c.runTmux("send-keys", "-t", c.SessionName(), "-X", "cancel")
	return err
}

// maxScrollLines bounds a single scroll request. A flick can only mean so
// much; anything larger is a client bug and must not turn into work here.
const maxScrollLines = 200

// ScrollUp scrolls up in copy mode
func (c *Controller) ScrollUp(lines int) error { return c.requestScroll(-lines) }

// ScrollDown scrolls down in copy mode
func (c *Controller) ScrollDown(lines int) error { return c.requestScroll(lines) }

// requestScroll records a scroll and returns immediately.
//
// Scrolling is handled on the connection's read loop, so doing the tmux call
// inline stalls everything else the client sends. A flick produces scroll
// messages far faster than tmux can service them: bursting 200 of them left
// the server 34 seconds behind, by which point the browser had given up on the
// socket and tmux reported "lost tty".
//
// Scrolling is additive, so a backlog can simply be summed. Requests
// accumulate here and a single worker applies whatever has piled up, which
// bounds the outstanding work to one tmux call no matter how fast the finger
// moves.
func (c *Controller) requestScroll(lines int) error {
	if lines == 0 {
		return nil
	}

	c.scrollMu.Lock()
	if lines > maxScrollLines {
		lines = maxScrollLines
	} else if lines < -maxScrollLines {
		lines = -maxScrollLines
	}
	c.scrollPending += lines
	if c.scrollPending > maxScrollLines {
		c.scrollPending = maxScrollLines
	} else if c.scrollPending < -maxScrollLines {
		c.scrollPending = -maxScrollLines
	}
	c.scrollMu.Unlock()

	select {
	case c.scrollWake <- struct{}{}:
	default: // a wake-up is already pending; the worker will see the new total
	}

	return nil
}

// runScrollWorker applies accumulated scrolling until the controller stops.
func (c *Controller) runScrollWorker() {
	for {
		select {
		case <-c.closeChan:
			return
		case <-c.scrollWake:
			for {
				select {
				case <-c.closeChan:
					return
				default:
				}
				c.scrollMu.Lock()
				lines := c.scrollPending
				c.scrollPending = 0
				c.scrollMu.Unlock()

				if lines == 0 {
					break
				}

				command := "scroll-down"
				if lines < 0 {
					command, lines = "scroll-up", -lines
				}

				// One call for the whole backlog; send-keys takes a count.
				if _, err := c.runTmux("send-keys", "-t", c.SessionName(), "-X", "-N", strconv.Itoa(lines), command); err != nil {
					break
				}
			}
		}
	}
}

// NewWindow creates a new window
func (c *Controller) NewWindow() error {
	_, err := c.runTmux("new-window", "-t", c.SessionName())
	if err != nil {
		return err
	}
	c.refreshChanged()
	return nil
}

func (c *Controller) refreshChanged() {
	c.watch.invalidateLayout()
	_ = c.RefreshLayout()
}

// runTmux executes a tmux command with the given arguments
func (c *Controller) runTmux(args ...string) (string, error) {
	if c.runner != nil {
		return c.runner.Run(args...)
	}

	// Every tmux command in this program passes through here, which makes it
	// the only place the ceiling can be enforced honestly.
	if err := c.guard.Record(); err != nil {
		return "", err
	}

	// One connection, no fork. Falling back keeps the server usable if control
	// mode could not be established.
	if c.control != nil && c.control.Alive() {
		return c.control.Run(args...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case directTmuxSlot <- struct{}{}:
		defer func() { <-directTmuxSlot }()
	case <-ctx.Done():
		return "", fmt.Errorf("tmux command queue timed out: %w", ctx.Err())
	}
	cmd := exec.CommandContext(ctx, "tmux", append(c.socketArgs(), args...)...)
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("tmux command timed out: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("tmux command failed: %w", err)
	}
	return string(output), nil
}
