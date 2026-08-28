package webtty

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"webtmux/pkg/tmux"
)

// TmuxController interface for tmux operations
type TmuxController interface {
	GetLayout() *tmux.Layout
	RefreshLayout() error
	SelectPane(paneID string) error
	SelectWindow(windowID string) error
	SwitchSession(sessionName string) error
	SplitPane(horizontal bool) error
	ClosePane(paneID string) error
	ZoomPane(paneID string, zoom bool) error
	GotoPane(paneID string) error
	Watch() (*tmux.Watch, error)
	Capture(paneID string, lines int, known string) (*tmux.Capture, error)
	EnterCopyMode() error
	ExitCopyMode() error
	ScrollUp(lines int) error
	ScrollDown(lines int) error
	NewWindow() error
	Events() <-chan tmux.Event
}

// SetTmuxController sets the tmux controller for the WebTTY instance
func (wt *WebTTY) SetTmuxController(tc TmuxController) {
	wt.tmuxCtrl = tc
}

// SendTmuxLayout sends the current tmux layout to the client
func (wt *WebTTY) SendTmuxLayout() error {
	if wt.tmuxCtrl == nil {
		return nil
	}

	layout := wt.tmuxCtrl.GetLayout()
	if layout == nil {
		return nil
	}

	data, err := json.Marshal(layout)
	if err != nil {
		return errors.Wrap(err, "failed to marshal tmux layout")
	}

	return wt.masterWrite(append([]byte{TmuxLayoutUpdate}, data...))
}

// SendTmuxModeUpdate sends the copy mode state to the client
func (wt *WebTTY) SendTmuxModeUpdate(inCopyMode bool) error {
	state := tmux.ModeState{
		InCopyMode: inCopyMode,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "failed to marshal tmux mode state")
	}

	return wt.masterWrite(append([]byte{TmuxModeUpdate}, data...))
}

// handleTmuxMessage handles tmux-specific messages from the client
func (wt *WebTTY) handleTmuxMessage(msgType byte, payload []byte) error {
	if wt.tmuxCtrl == nil {
		return nil // Silently ignore if no tmux controller
	}

	switch msgType {
	case TmuxSelectPane:
		paneID := string(payload)
		if err := wt.tmuxCtrl.SelectPane(paneID); err != nil {
			return errors.Wrap(err, "failed to select pane")
		}
		return wt.SendTmuxLayout()

	case TmuxSelectWindow:
		windowID := string(payload)
		if err := wt.tmuxCtrl.SelectWindow(windowID); err != nil {
			return errors.Wrap(err, "failed to select window")
		}
		return wt.SendTmuxLayout()

	case TmuxSplitPane:
		horizontal := string(payload) == "h"
		if err := wt.tmuxCtrl.SplitPane(horizontal); err != nil {
			return errors.Wrap(err, "failed to split pane")
		}
		return wt.SendTmuxLayout()

	case TmuxClosePane:
		paneID := string(payload)
		if err := wt.tmuxCtrl.ClosePane(paneID); err != nil {
			return errors.Wrap(err, "failed to close pane")
		}
		return wt.SendTmuxLayout()

	case TmuxZoomPane:
		// payload is "<0|1>:<paneID>"; an empty pane ID means the session's
		// current pane.
		zoom, paneID := false, ""
		if parts := strings.SplitN(string(payload), ":", 2); len(parts) == 2 {
			zoom, paneID = parts[0] == "1", parts[1]
		}
		if err := wt.tmuxCtrl.ZoomPane(paneID, zoom); err != nil {
			return errors.Wrap(err, "failed to zoom pane")
		}
		return wt.SendTmuxLayout()

	case TmuxWatch:
		board, err := wt.tmuxCtrl.Watch()
		if err != nil {
			return errors.Wrap(err, "failed to build watch board")
		}
		response := board
		if known := string(payload); known != "" && known == board.Digest {
			response = &tmux.Watch{Digest: board.Digest, Unchanged: true}
		}
		data, err := json.Marshal(response)
		if err != nil {
			return errors.Wrap(err, "failed to marshal watch board")
		}
		return wt.masterWrite(append([]byte{TmuxWatchUpdate}, data...))

	case TmuxCapture:
		// payload is "<lines>:<known digest>:<paneID>"
		lines, known, paneID := 0, "", string(payload)
		if parts := strings.SplitN(string(payload), ":", 3); len(parts) == 3 {
			lines, _ = strconv.Atoi(parts[0])
			known, paneID = parts[1], parts[2]
		}
		capture, err := wt.tmuxCtrl.Capture(paneID, lines, known)
		if err != nil {
			return errors.Wrap(err, "failed to capture pane")
		}
		data, err := json.Marshal(capture)
		if err != nil {
			return errors.Wrap(err, "failed to marshal capture")
		}
		return wt.masterWrite(append([]byte{TmuxCaptureUpdate}, data...))

	case TmuxGotoPane:
		if err := wt.tmuxCtrl.GotoPane(string(payload)); err != nil {
			return errors.Wrap(err, "failed to go to pane")
		}
		return wt.SendTmuxLayout()

	case TmuxCopyMode:
		enter := string(payload) == "1"
		var err error
		if enter {
			err = wt.tmuxCtrl.EnterCopyMode()
		} else {
			err = wt.tmuxCtrl.ExitCopyMode()
		}
		if err != nil {
			return errors.Wrap(err, "failed to toggle copy mode")
		}
		return wt.SendTmuxModeUpdate(enter)

	case TmuxScrollUp:
		lines, _ := strconv.Atoi(string(payload))
		if lines <= 0 {
			lines = 1
		}
		return wt.tmuxCtrl.ScrollUp(lines)

	case TmuxScrollDown:
		lines, _ := strconv.Atoi(string(payload))
		if lines <= 0 {
			lines = 1
		}
		return wt.tmuxCtrl.ScrollDown(lines)

	case TmuxNewWindow:
		if err := wt.tmuxCtrl.NewWindow(); err != nil {
			return errors.Wrap(err, "failed to create new window")
		}
		return wt.SendTmuxLayout()

	case TmuxSwitchSession:
		sessionName := string(payload)
		if err := wt.tmuxCtrl.SwitchSession(sessionName); err != nil {
			return errors.Wrap(err, "failed to switch session")
		}
		return wt.SendTmuxLayout()

	default:
		return errors.Errorf("unknown tmux message type: %c", msgType)
	}
}

// isTmuxMessage returns true if the message type is a tmux-specific message
func isTmuxMessage(msgType byte) bool {
	switch msgType {
	case TmuxSelectPane, TmuxSelectWindow, TmuxSplitPane, TmuxClosePane,
		TmuxCopyMode, TmuxSendCommand, TmuxScrollUp, TmuxScrollDown, TmuxNewWindow,
		TmuxSwitchSession, TmuxZoomPane, TmuxWatch, TmuxGotoPane, TmuxCapture:
		return true
	default:
		return false
	}
}
