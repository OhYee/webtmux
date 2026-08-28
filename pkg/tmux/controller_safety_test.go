package tmux

import (
	"strings"
	"testing"
)

type scriptedRunner struct {
	listCalls int
	created   bool
}

func (r *scriptedRunner) Run(args ...string) (string, error) {
	switch args[0] {
	case "list-panes":
		r.listCalls++
		if r.listCalls == 1 {
			return "", nil
		}
		return strings.Join([]string{
			"$1", "background", "1", "1",
			"@1", "main", "0", "1",
			"0", "0", "0",
			"%1", "0", "1", "80", "24",
			"0", "0", "bash", "shell",
		}, "\t"), nil
	case "has-session":
		return "", nil
	case "new-session":
		r.created = true
		return "", nil
	default:
		return "", nil
	}
}

func TestStartDoesNotCreateDuplicateSessionAfterTransientEmptyLayout(t *testing.T) {
	runner := &scriptedRunner{}
	c, err := NewController("background", "", NewWatchTracker())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	c.UseRunner(runner)

	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if runner.created {
		t.Fatal("Start created a session that has-session confirmed already exists")
	}
	if runner.listCalls != 2 {
		t.Fatalf("list-panes calls = %d, want retry after transient empty layout", runner.listCalls)
	}
}

func TestScrollBacklogIsBounded(t *testing.T) {
	c := &Controller{scrollWake: make(chan struct{}, 1)}

	if err := c.requestScroll(1 << 30); err != nil {
		t.Fatal(err)
	}
	c.scrollMu.Lock()
	pending := c.scrollPending
	c.scrollPending = 0
	c.scrollMu.Unlock()
	if pending != maxScrollLines {
		t.Fatalf("positive backlog = %d, want %d", pending, maxScrollLines)
	}

	if err := c.requestScroll(-(1 << 30)); err != nil {
		t.Fatal(err)
	}
	c.scrollMu.Lock()
	pending = c.scrollPending
	c.scrollMu.Unlock()
	if pending != -maxScrollLines {
		t.Fatalf("negative backlog = %d, want %d", pending, -maxScrollLines)
	}
}
