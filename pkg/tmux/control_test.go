package tmux

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestControlLiveLayout(t *testing.T) {
	session := os.Getenv("WEBTMUX_LIVE_TEST")
	if session == "" {
		t.Skip("set WEBTMUX_LIVE_TEST to a disposable or existing tmux session")
	}
	c, err := NewControl("", session)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out, err := c.Run("list-panes", "-a", "-F", layoutFormat)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		t.Fatal("control returned no panes")
	}
	fields := splitTmuxFields(lines[0])
	if len(fields) < 2 || fields[1] != session {
		t.Fatalf("control output does not contain session %q: %q", session, out)
	}
}

func TestExecutorLiveControllerStart(t *testing.T) {
	session := os.Getenv("WEBTMUX_LIVE_TEST")
	if session == "" {
		t.Skip("set WEBTMUX_LIVE_TEST to an existing tmux session")
	}
	guard := NewGuard(40)
	executor := NewExecutor("", session, guard, nil)
	defer executor.Close()
	controller, err := NewController(session, "", NewWatchTracker())
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Stop()
	controller.UseGuard(guard)
	controller.UseRunner(executor)
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	if layout := controller.GetLayout(); layout == nil || layout.SessionName != session {
		t.Fatalf("layout = %#v", layout)
	}
}

type testWriteCloser struct{ io.Writer }

func (testWriteCloser) Close() error { return nil }

// Arguments used to go straight to exec, where nothing reparsed them. Over
// control mode tmux parses the line itself, so every format string this
// package sends has to survive the round trip as one token.
func TestQuoteArg(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "list-panes", "list-panes"},
		{"flag", "-a", "-a"},
		{"pane id", "%3", "%3"},
		{"empty", "", "''"},
		{"space", "a b", "'a b'"},
		{"format braces", "#{pane_id}", "'#{pane_id}'"},
		{"tab separated format", "#{a}\t#{b}", `'#{a}\t#{b}'`},
		{"single quote inside", "it's", `'it'\''s'`},
		// A plain target has nothing tmux would reparse, so it needs no quoting.
		{"target with colon", "dev:1.2", "dev:1.2"},
		// A semicolon would otherwise end the command.
		{"semicolon must be quoted", "a;b", "'a;b'"},
		// Session names may contain spaces.
		{"session with space", "my work:1.2", "'my work:1.2'"},
	}

	for _, tc := range cases {
		if got := quoteArg(tc.in); got != tc.want {
			t.Errorf("%s: quoteArg(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestFormatCommandKeepsFormatsIntact(t *testing.T) {
	line := formatCommand([]string{"list-panes", "-a", "-F", layoutFormat})

	if !strings.HasPrefix(line, "list-panes -a -F '") {
		t.Errorf("command line = %q", line)
	}
	// The format must be one token: no unquoted spaces may appear inside it.
	body := strings.TrimSuffix(strings.TrimPrefix(line, "list-panes -a -F '"), "'")
	want := strings.ReplaceAll(layoutFormat, "\t", `\t`)
	if body != want {
		t.Errorf("format was altered:\n got %q\nwant %q", body, want)
	}
}

func TestSplitTmuxFieldsAcceptsProcessAndControlEscapes(t *testing.T) {
	for _, line := range []string{"a\tb\tc", `a\tb\tc`, `a\011b\011c`} {
		fields := splitTmuxFields(line)
		if got := strings.Join(fields, ","); got != "a,b,c" {
			t.Fatalf("splitTmuxFields(%q) = %q", line, got)
		}
	}
}

func TestControlTimeoutInvalidatesConnection(t *testing.T) {
	oldTimeout := controlCommandTimeout
	controlCommandTimeout = 20 * time.Millisecond
	t.Cleanup(func() { controlCommandTimeout = oldTimeout })

	c := &Control{
		stdin:   testWriteCloser{Writer: io.Discard},
		running: true,
		events:  make(chan Event, 1),
		closed:  make(chan struct{}),
	}

	start := time.Now()
	_, err := c.Run("list-panes")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Run() error = %v, want a timeout", err)
	}
	t.Logf("timeout returned after %s", time.Since(start))

	if c.Alive() {
		t.Fatal("a timed-out control connection must not be reused")
	}
	c.mu.Lock()
	pending := len(c.waiters)
	c.mu.Unlock()
	if pending != 0 {
		t.Fatalf("timed-out control connection kept %d stale waiters", pending)
	}
}

func TestControlReaderClosesEventsWhenConnectionEnds(t *testing.T) {
	c := &Control{
		running: true,
		events:  make(chan Event, 1),
		closed:  make(chan struct{}),
	}

	c.read(io.NopCloser(strings.NewReader("")))

	select {
	case _, ok := <-c.Events():
		if ok {
			t.Fatal("event stream remained open after the control reader ended")
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("event stream remained open after the control reader ended")
	}
}

func TestControlInitialAttachReplyCannotSatisfyFirstCommand(t *testing.T) {
	initial := make(chan reply, 1)
	command := make(chan reply, 1)
	c := &Control{
		running: true,
		initial: initial,
		waiters: []chan reply{command},
		events:  make(chan Event, 1),
		closed:  make(chan struct{}),
	}

	c.deliver(reply{out: "attach"})
	c.deliver(reply{out: "list-panes"})

	if got := <-initial; got.out != "attach" {
		t.Fatalf("initial reply = %q", got.out)
	}
	if got := <-command; got.out != "list-panes" {
		t.Fatalf("command reply shifted by attach handshake: %q", got.out)
	}
}
