package tmux

import (
	"os"
	"testing"
)

// Ownership decides whether we are allowed to move a tmux client at all, so
// getting it wrong means hijacking the terminal the user is sitting in front of.
func TestIsDescendantOf(t *testing.T) {
	self := os.Getpid()

	if !isDescendantOf(self, self) {
		t.Error("a process should count as its own descendant")
	}
	if isDescendantOf(1, self) {
		t.Error("init must not be reported as our descendant")
	}
	if isDescendantOf(self, -1) {
		t.Error("a nonsense ancestor must not match")
	}
}

// With no client of our own attached, switching must do nothing rather than
// fall back to whichever client tmux considers current.
func TestSwitchOurClientIsNoopWithoutOurClient(t *testing.T) {
	s := newTestServer(t, "wtmxtest-client")
	c := s.controller()

	// The test server has no attached client at all.
	if tty := c.ourClient(); tty != "" {
		t.Fatalf("ourClient() = %q, want empty on a server with no clients", tty)
	}
	if err := c.switchOurClient("t"); err != nil {
		t.Errorf("switchOurClient with no client should be a no-op, got %v", err)
	}
}
