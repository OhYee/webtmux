package tmux

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SetClientPID records the process this connection spawned to attach to tmux.
//
// Every browser gets its own `tmux attach`, and they are all children of this
// server, so "descends from us" is not specific enough to tell them apart —
// one viewer switching sessions would move another viewer's client. The PID
// pins ownership to a single connection.
func (c *Controller) SetClientPID(pid int) {
	c.clientPID = pid
}

// ourClient returns the tty of the tmux client this connection owns, or "" if
// it has no client attached yet.
//
// Switching sessions has to name a client explicitly. `switch-client` without
// -c acts on whichever client tmux considers current, which may well be the
// terminal the user is sitting in front of — so a phone tapping a card on the
// watch board could yank their desktop to another session. tmux exposes
// #{client_pid}, so ownership can be settled by walking the process tree.
func (c *Controller) ourClient() string {
	// The tty does not change for the life of a connection, and resolving it
	// walks the process tree with a ps per hop per client — a burst of dozens
	// of processes on every card tap if it were done each time.
	if c.clientTTY != "" {
		return c.clientTTY
	}

	out, err := c.runTmux("list-clients", "-F", "#{client_pid}\t#{client_tty}")
	if err != nil {
		return ""
	}

	// Prefer the connection's own process; fall back to this server's tree
	// only when no PID was recorded.
	self := c.clientPID
	if self == 0 {
		self = os.Getpid()
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := splitTmuxFields(line)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		if isDescendantOf(pid, self) {
			c.clientTTY = parts[1]
			return c.clientTTY
		}
	}

	return ""
}

// isDescendantOf reports whether pid is ancestor, or a descendant of it.
func isDescendantOf(pid, ancestor int) bool {
	// A handful of hops is plenty; the bound also stops a cycle from hanging.
	for i := 0; i < 12 && pid > 1; i++ {
		if pid == ancestor {
			return true
		}
		out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return false
		}
		parent, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			return false
		}
		pid = parent
	}
	return false
}

// switchOurClient points this process's client at a session, leaving every
// other client where it is. It is a no-op when we have no client, which is the
// safe outcome: better to do nothing than to move someone else's terminal.
func (c *Controller) switchOurClient(session string) error {
	tty := c.ourClient()
	if tty == "" {
		return nil
	}
	_, err := c.runTmux("switch-client", "-c", tty, "-t", session)
	return err
}
