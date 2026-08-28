package webtty

// Protocols defines the name of this protocol,
// which is supposed to be used to the subprotocol of Websockt streams.
var Protocols = []string{"webtty"}

const (
	// Unknown message type, maybe sent by a bug
	UnknownInput = '0'
	// User input typically from a keyboard
	Input = '1'
	// Ping to the server
	Ping = '2'
	// Notify that the browser size has been changed
	ResizeTerminal = '3'
	// Change encoding
	SetEncoding = '4'
)

const (
	// Unknown message type, maybe set by a bug
	UnknownOutput = '0'
	// Normal output to the terminal
	Output = '1'
	// Pong to the browser
	Pong = '2'
	// Set window title of the terminal
	SetWindowTitle = '3'
	// Set terminal preference
	SetPreferences = '4'
	// Make terminal to reconnect
	SetReconnect = '5'
	// Set the input buffer size
	SetBufferSize = '6'

	// Tmux layout update (JSON payload)
	TmuxLayoutUpdate = '7'
	// Tmux pane-specific output
	TmuxPaneOutput = '8'
	// Tmux mode update (copy mode state)
	TmuxModeUpdate = '9'
	// Tmux session info
	TmuxSessionInfo = 'A'
	// Tmux error
	TmuxError = 'B'
	// Watch board snapshot (JSON payload)
	TmuxWatchUpdate = 'C'
	// Reflowed pane output for the reader (JSON payload)
	TmuxCaptureUpdate = 'D'
)

// Tmux input message types (client -> server)
const (
	// Select a pane by ID
	TmuxSelectPane = '5'
	// Select a window by ID
	TmuxSelectWindow = '6'
	// Split current pane (payload: "h" or "v")
	TmuxSplitPane = '7'
	// Close a pane by ID
	TmuxClosePane = '8'
	// Enter/exit copy mode (payload: "1" or "0")
	TmuxCopyMode = '9'
	// Raw tmux command
	TmuxSendCommand = 'A'
	// Scroll in copy mode (payload: lines as string)
	TmuxScrollUp   = 'B'
	TmuxScrollDown = 'C'
	// Create new window
	TmuxNewWindow = 'D'
	// Switch session by name
	TmuxSwitchSession = 'E'
	// Zoom or unzoom a pane (payload: "<0|1>:<paneID>"; empty paneID means the
	// session's current pane)
	TmuxZoomPane = 'F'
	// Request one watch board snapshot. The client polls only while the board
	// is on screen, which keeps capture-pane off the server the rest of the
	// time and avoids any push-loop lifecycle on the server.
	TmuxWatch = 'G'
	// Jump to a pane anywhere on the server (payload: pane ID). Unlike
	// TmuxSelectPane this also switches session and window as needed.
	TmuxGotoPane = 'H'
	// Ask for a pane's output as reflowable lines (payload: "<lines>:<paneID>")
	TmuxCapture = 'I'
)
