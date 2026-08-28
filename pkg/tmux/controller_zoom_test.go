package tmux

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// testServer is a tmux server private to one test.
//
// Isolation is done with -L on every single invocation. TMUX_TMPDIR is not
// used: some tmux builds ignore it entirely, in which case commands silently
// fall through to the user's real server — and a stray kill-server there
// destroys live sessions.
type testServer struct {
	t      *testing.T
	socket string
}

func newTestServer(t *testing.T, socket string) *testServer {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if socket == "" {
		t.Fatal("refusing to run against tmux's default server")
	}

	s := &testServer{t: t, socket: socket}

	// Fail loudly if a server is already listening on this name, rather than
	// adopting (and later killing) something we did not create.
	if out, err := s.try("list-sessions"); err == nil {
		t.Fatalf("socket %q already has a tmux server: %s", socket, out)
	}

	s.run("-f", "/dev/null", "new-session", "-d", "-s", "t", "-x", "80", "-y", "24")
	s.run("split-window", "-t", "t")
	s.run("split-window", "-t", "t")

	// Prove the isolation held before anything destructive is registered: the
	// private server must contain exactly the one session we just made.
	if out, _ := s.try("list-sessions", "-F", "#{session_name}"); strings.TrimSpace(out) != "t" {
		t.Fatalf("isolation check failed, sessions on %q: %q", socket, out)
	}

	t.Cleanup(func() {
		// Always scoped by -L; never a bare kill-server.
		exec.Command("tmux", "-L", s.socket, "kill-server").Run()
	})

	return s
}

func (s *testServer) try(args ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", s.socket}, args...)...).CombinedOutput()
	return string(out), err
}

func (s *testServer) run(args ...string) {
	s.t.Helper()
	if out, err := s.try(args...); err != nil {
		s.t.Fatalf("tmux %s: %v (%s)", strings.Join(args, " "), err, out)
	}
}

func (s *testServer) controller() *Controller {
	s.t.Helper()
	c, err := NewController("t", s.socket, NewWatchTracker())
	if err != nil {
		s.t.Fatalf("NewController: %v", err)
	}
	if err := c.Start(); err != nil {
		s.t.Fatalf("Start: %v", err)
	}
	return c
}

func activeWindow(t *testing.T, c *Controller) Window {
	t.Helper()
	layout := c.GetLayout()
	if layout == nil {
		t.Fatal("layout is nil")
	}
	for _, w := range layout.Windows {
		if w.Active {
			return w
		}
	}
	t.Fatal("no active window in layout")
	return Window{}
}

func TestZoomPaneFocusesAndRestores(t *testing.T) {
	c := newTestServer(t, "wtmxtest-focus").controller()

	win := activeWindow(t, c)
	if len(win.Panes) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(win.Panes))
	}
	if win.Zoomed {
		t.Fatal("window should not start zoomed")
	}

	// Focus a pane that is not currently active.
	var target Pane
	for _, p := range win.Panes {
		if !p.Active {
			target = p
			break
		}
	}
	if target.ID == "" {
		t.Fatal("could not find an inactive pane to focus")
	}

	if err := c.ZoomPane(target.ID, true); err != nil {
		t.Fatalf("ZoomPane(zoom): %v", err)
	}

	if !activeWindow(t, c).Zoomed {
		t.Error("window should be zoomed after ZoomPane(true)")
	}
	if got := c.GetLayout().ActivePaneID; got != target.ID {
		t.Errorf("active pane = %s, want %s", got, target.ID)
	}

	if err := c.ZoomPane("", false); err != nil {
		t.Fatalf("ZoomPane(unzoom): %v", err)
	}
	if activeWindow(t, c).Zoomed {
		t.Error("window should not be zoomed after ZoomPane(false)")
	}
}

// Zooming pane B while pane A is zoomed must land on B rather than toggling
// the existing zoom off.
func TestZoomPaneSwitchesTargetWhileZoomed(t *testing.T) {
	c := newTestServer(t, "wtmxtest-switch").controller()

	panes := activeWindow(t, c).Panes
	first, second := panes[0], panes[1]

	if err := c.ZoomPane(first.ID, true); err != nil {
		t.Fatalf("zoom first: %v", err)
	}
	if err := c.ZoomPane(second.ID, true); err != nil {
		t.Fatalf("zoom second: %v", err)
	}

	if !activeWindow(t, c).Zoomed {
		t.Error("window should still be zoomed after switching target")
	}
	if got := c.GetLayout().ActivePaneID; got != second.ID {
		t.Errorf("active pane = %s, want %s", got, second.ID)
	}
}

// ZoomPane(false) carries an explicit desired state, so repeating it must not
// toggle zoom back on.
func TestZoomPaneUnzoomIsIdempotent(t *testing.T) {
	c := newTestServer(t, "wtmxtest-idem").controller()

	for i := 0; i < 2; i++ {
		if err := c.ZoomPane("", false); err != nil {
			t.Fatalf("unzoom %d: %v", i, err)
		}
		if activeWindow(t, c).Zoomed {
			t.Fatalf("window zoomed after unzoom call %d", i)
		}
	}
}

// Bell and activity are the only things allowed to put colour in the UI
// chrome, so the layout has to actually carry them.
func TestLayoutReportsBellAndActivity(t *testing.T) {
	s := newTestServer(t, "wtmxtest-bell")
	s.run("set", "-g", "monitor-bell", "on")
	s.run("set", "-g", "monitor-activity", "on")
	s.run("new-window", "-t", "t", "-n", "bg")
	s.run("select-window", "-t", "t:^") // make the first window current again

	c := s.controller()

	// Ring the bell in the window that is not focused.
	var background Window
	for _, w := range c.GetLayout().Windows {
		if !w.Active {
			background = w
			break
		}
	}
	if background.ID == "" {
		t.Fatal("expected a background window")
	}
	s.run("send-keys", "-t", background.ID, `printf "\a"`, "Enter")
	time.Sleep(700 * time.Millisecond)

	if err := c.RefreshLayout(); err != nil {
		t.Fatalf("RefreshLayout: %v", err)
	}

	for _, w := range c.GetLayout().Windows {
		if w.ID == background.ID {
			if !w.Bell {
				t.Error("background window should report Bell after a bell")
			}
			return
		}
	}
	t.Fatal("background window vanished from layout")
}

// The controller must never touch the default server when a socket is set.
func TestSocketArgs(t *testing.T) {
	cases := []struct {
		socket string
		want   []string
	}{
		{"", nil},
		{"work", []string{"-L", "work"}},
		{"/tmp/tmux-501/custom", []string{"-S", "/tmp/tmux-501/custom"}},
	}

	for _, tc := range cases {
		c := &Controller{socket: tc.socket}
		got := c.socketArgs()
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("socketArgs(%q) = %v, want %v", tc.socket, got, tc.want)
		}
	}
}
