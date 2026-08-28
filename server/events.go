package server

import (
	"sync"

	"webtmux/pkg/tmux"
)

// layoutEvents are the notifications that mean the shape of things changed:
// a split, a new window, a rename, a different active pane. Everything else
// tmux pushes is output.
var layoutEvents = map[string]bool{
	"layout-change":           true,
	"window-add":              true,
	"window-close":            true,
	"window-renamed":          true,
	"window-pane-changed":     true,
	"session-changed":         true,
	"session-window-changed":  true,
	"sessions-changed":        true,
	"unlinked-window-add":     true,
	"unlinked-window-close":   true,
	"unlinked-window-renamed": true,
}

// dirty broadcasts "the layout moved" to every connected viewer.
//
// Each connection used to ask tmux for the layout on a timer whether or not
// anything had happened. Now tmux says when, once, and everyone hears it.
type dirty struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]bool
}

func newDirty() *dirty {
	return &dirty{subscribers: map[chan struct{}]bool{}}
}

func (d *dirty) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	d.mu.Lock()
	d.subscribers[ch] = true
	d.mu.Unlock()

	return ch, func() {
		d.mu.Lock()
		delete(d.subscribers, ch)
		d.mu.Unlock()
	}
}

func (d *dirty) broadcast() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range d.subscribers {
		select {
		case ch <- struct{}{}:
		default: // already flagged; one wake-up is enough
		}
	}
}

// pumpEvents turns the control stream into layout wake-ups.
//
// It deliberately does not drive activity detection. tmux scopes %output to
// the session the control client is attached to, so trusting it would leave
// every other session on the board frozen — a failure that would have been
// invisible until an agent in the wrong session went unnoticed. Activity keeps
// coming from the periodic sweep, which now costs no processes at all.
func (server *Server) pumpEvents(ctl *tmux.Control) {
	for ev := range ctl.Events() {
		if layoutEvents[ev.Type] {
			server.layoutDirty.broadcast()
		}
	}
}
