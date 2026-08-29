package tmux

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Executor is the single bounded path from webtmux to a tmux server.
//
// It owns control-mode recovery and the process fallback, and serializes both.
// Controllers keep a pointer to this object rather than to one Control, so an
// old browser automatically uses a replacement connection after tmux restarts.
type Executor struct {
	socket  string
	session string
	guard   *Guard

	runMu sync.Mutex
	mu    sync.Mutex
	ctl   *Control

	onControl func(*Control)
	closed    bool
	nextDial  time.Time
	dialDelay time.Duration
}

var fallbackCommandTimeout = 10 * time.Second

func NewExecutor(socket, session string, guard *Guard, onControl func(*Control)) *Executor {
	return &Executor{socket: socket, session: session, guard: guard, onControl: onControl}
}

func (e *Executor) Run(args ...string) (string, error) {
	e.runMu.Lock()
	defer e.runMu.Unlock()

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return "", fmt.Errorf("tmux executor closed")
	}
	e.mu.Unlock()

	ctl := e.control()
	if err := e.guard.Record(); err != nil {
		return "", err
	}
	if ctl != nil {
		started := time.Now()
		out, err := ctl.Run(args...)
		if err != nil {
			slog.Warn("tmux control command failed; invalidating connection", "command", commandName(args), "session", e.session, "socket", e.socket, "duration", time.Since(started).Round(time.Millisecond), "error", err)
			e.invalidate(ctl)
		} else {
			slog.Debug("tmux command completed", "transport", "control", "command", commandName(args), "session", e.session, "duration", time.Since(started).Round(time.Millisecond))
		}
		return out, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), fallbackCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", append(socketArgsFor(e.socket), args...)...)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		slog.Error("tmux command timed out", "transport", "process", "command", commandName(args), "session", e.session, "socket", e.socket, "duration", time.Since(started).Round(time.Millisecond))
		return "", fmt.Errorf("tmux command timed out: %w", ctx.Err())
	}
	if err != nil {
		stderr := truncate(string(output), 2048)
		slog.Warn("tmux command failed", "transport", "process", "command", commandName(args), "session", e.session, "socket", e.socket, "duration", time.Since(started).Round(time.Millisecond), "error", err, "output", stderr)
		return "", fmt.Errorf("tmux command failed: %w: %s", err, stderr)
	}
	slog.Debug("tmux command completed", "transport", "process", "command", commandName(args), "session", e.session, "duration", time.Since(started).Round(time.Millisecond))
	return string(output), nil
}

func (e *Executor) control() *Control {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	if time.Now().Before(e.nextDial) {
		e.mu.Unlock()
		return nil
	}
	if e.ctl != nil && e.ctl.Alive() {
		ctl := e.ctl
		e.mu.Unlock()
		return ctl
	}
	old := e.ctl
	e.ctl = nil
	e.mu.Unlock()
	if old != nil {
		old.Close()
	}

	// Attaching a control client is itself work serviced by tmux and must be
	// visible to the same safety ceiling as ordinary commands.
	if err := e.guard.Record(); err != nil {
		return nil
	}
	ctl, err := NewControl(e.socket, e.session)
	if err != nil {
		e.mu.Lock()
		if e.dialDelay == 0 {
			e.dialDelay = time.Second
		} else {
			e.dialDelay *= 2
			if e.dialDelay > 15*time.Second {
				e.dialDelay = 15 * time.Second
			}
		}
		e.nextDial = time.Now().Add(e.dialDelay)
		slog.Warn("tmux control connection failed; using process fallback", "session", e.session, "socket", e.socket, "retry_in", e.dialDelay, "error", err)
		e.mu.Unlock()
		return nil
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		ctl.Close()
		return nil
	}
	e.ctl = ctl
	e.nextDial = time.Time{}
	e.dialDelay = 0
	onControl := e.onControl
	e.mu.Unlock()
	slog.Info("tmux control connection established", "session", e.session, "socket", e.socket)
	if onControl != nil {
		onControl(ctl)
	}
	return ctl
}

func commandName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func (e *Executor) invalidate(ctl *Control) {
	e.mu.Lock()
	if e.ctl == ctl {
		e.ctl = nil
	}
	e.mu.Unlock()
	ctl.Close()
}

func (e *Executor) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	ctl := e.ctl
	e.ctl = nil
	e.mu.Unlock()
	if ctl != nil {
		ctl.Close()
	}
}
