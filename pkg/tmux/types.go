package tmux

// Session represents a tmux session
type Session struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Windows  int    `json:"windows"`
	Attached bool   `json:"attached"`
	Active   bool   `json:"active"`
}

// Layout represents the complete tmux state
type Layout struct {
	SessionID    string    `json:"sessionId"`
	SessionName  string    `json:"sessionName"`
	Sessions     []Session `json:"sessions"`
	Windows      []Window  `json:"windows"`
	ActiveWinID  string    `json:"activeWindowId"`
	ActivePaneID string    `json:"activePaneId"`
}

// Window represents a tmux window
type Window struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Active bool   `json:"active"`
	Zoomed bool   `json:"zoomed"`
	// Bell and Activity drive the only coloured mark in the chrome.
	Bell     bool   `json:"bell"`
	Activity bool   `json:"activity"`
	Panes    []Pane `json:"panes"`
}

// Pane represents a tmux pane
type Pane struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Active  bool   `json:"active"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Top     int    `json:"top"`
	Left    int    `json:"left"`
	Command string `json:"command"`
	Title   string `json:"title"`
}

// PaneWatch is one pane as it appears on the watch board.
type PaneWatch struct {
	ID string `json:"id"`
	// Address is tmux's own notation, e.g. "dev:1.2".
	Address      string   `json:"address"`
	Session      string   `json:"session"`
	WindowID     string   `json:"windowId"`
	WindowName   string   `json:"windowName"`
	WindowIndex  int      `json:"windowIndex"`
	PaneIndex    int      `json:"paneIndex"`
	Command      string   `json:"command"`
	Title        string   `json:"title"`
	Active       bool     `json:"active"`
	WindowActive bool     `json:"windowActive"`
	Bell         bool     `json:"bell"`
	Tail         []string `json:"tail"`
}

// Watch is the whole board: every pane on the server in tmux hierarchy order.
type Watch struct {
	Panes     []PaneWatch `json:"panes,omitempty"`
	Digest    string      `json:"digest"`
	Unchanged bool        `json:"unchanged,omitempty"`
}

// ModeState represents the current mode of a pane (normal, copy, etc.)
type ModeState struct {
	PaneID         string `json:"paneId"`
	InCopyMode     bool   `json:"inCopyMode"`
	ScrollPosition int    `json:"scrollPosition"`
	HistorySize    int    `json:"historySize"`
}

// Event represents a tmux control mode event
type Event struct {
	Type    string
	Payload string
}
