# webtmux

A web-based terminal with tmux-specific features. Access your tmux sessions from any browser with a visual pane layout, touch-friendly controls, and automatic scroll-to-copy-mode.

## Quick Start (Sprite)

Deploy webtmux as a service on [Sprite](https://sprites.app):

```bash
VERSION=v1.0.0 # replace with a release from the GitHub Releases page
curl -fsSL "https://github.com/OhYee/webtmux/releases/download/${VERSION}/webtmux-linux-amd64.tar.gz" |
  tar -xz
sudo install -m 0755 webtmux-linux-amd64 /usr/local/bin/webtmux
sprite-env services create webtmux \
  --cmd /usr/local/bin/webtmux \
  --args '-w,tmux,new-session,-A,-s,main' \
  --http-port 8080
```

The service prints an automatically generated password at startup. Configure
explicit credentials with `-c user:password` when your platform supports
passing secrets securely.

## Features

- **Visual Pane Layout**: Sidebar minimap shows your tmux pane arrangement - click to switch panes
- **Window Tabs**: Quick window switching via clickable tabs
- **Touch-Friendly**: Mobile controls for split, new window, and pane switching
- **Scroll-to-Copy-Mode**: Scroll up automatically enters tmux copy mode
- **Secure by Default**: HTTP Basic Auth with auto-generated credentials
- **Single Binary**: All assets embedded - just download and run
- **Real-time Updates**: Layout changes sync automatically

## Installation

### Prebuilt Binaries

Prebuilt archives and checksums are published on the
[GitHub Releases](https://github.com/OhYee/webtmux/releases) page.

```bash
# After downloading the archive for your platform (Linux x64 example)
tar -xzf webtmux-linux-amd64.tar.gz
install -m 0755 webtmux-linux-amd64 "$HOME/.local/bin/webtmux"
```

### Build from Source

```bash
# Clone the repository
git clone https://github.com/OhYee/webtmux.git
cd webtmux

# Build for current platform
make build

# Or cross-compile for all platforms
make cross-compile
```

### Docker

```bash
docker build -t webtmux .
docker run --rm -p 127.0.0.1:8080:8080 webtmux
```

Binding to `127.0.0.1` keeps the service local to the host. Use a trusted TLS
reverse proxy before exposing webtmux to a network.

## Usage

### Basic Usage

```bash
# Start with tmux (auto-generates credentials)
webtmux -w tmux new-session -A -s main

# Output:
# ========================================
#   Authentication Required (default)
#   Username: admin
#   Password: <random-32-char-password>
# ========================================
```

### Custom Credentials

```bash
webtmux -w -c user:password tmux new-session -A -s main
```

### Disable Authentication (not recommended)

```bash
webtmux -w --no-auth tmux new-session -A -s main
```

### Common Options

| Flag | Description |
|------|-------------|
| `-w, --permit-write` | Allow input to the terminal (required for interactive use) |
| `-p, --port PORT` | Port to listen on (default: 8080) |
| `-a, --address ADDR` | Address to bind to (default: 0.0.0.0) |
| `-c, --credential USER:PASS` | Set custom credentials for HTTP Basic Auth |
| `--no-auth` | Disable authentication (NOT RECOMMENDED) |
| `--ws-origin REGEX` | Regex for allowed WebSocket origins |
| `-t, --tls` | Enable TLS/SSL |
| `--tls-crt FILE` | TLS certificate file |
| `--tls-key FILE` | TLS key file |
| `-r, --random-url` | Add random string to URL path |
| `--reconnect` | Enable automatic reconnection |
| `--once` | Accept only one client, then exit |
| `--keys FILE` | Load the mobile key panel from a JSON file |

Run `webtmux --help` for all available options.

### Custom Mobile Keys

All supplemental keys are kept in the foldable **Keys** panel. Pass a JSON
file with `--keys` to replace the built-in keys:

```json
{
  "row": [
    {"label": "ESC", "seq": "\u001b"},
    {"label": "^C", "seq": "\u0003", "hint": "Interrupt"}
  ],
  "groups": [
    {
      "label": "My keys",
      "keys": [
        {"label": "HOME", "seq": "\u001b[H"},
        {"label": "Wide action", "seq": "\u001b[20;5~", "wide": true}
      ]
    }
  ]
}
```

`row` is displayed in the first **Keys** tab; every item in `groups` becomes
another tab. `label` is visible text, `seq` is the exact byte sequence sent to
the terminal, and `key`, `hint`, and `wide` are optional display fields.

```bash
webtmux -w --keys ./keys.json tmux new-session -A -s main
```

The **配置** tab inside the Keys panel edits this same file and applies changes
immediately. When webtmux starts without `--keys`, the tab can inspect the
built-in configuration but remains read-only.

### launchd supervision

`launchctl` supervision is opt-in and defaults to disabled. Local installations
that provide a LaunchAgent can declare the intended state in the same HCL file:

```hcl
launchctl = false
```

Changing this value requires the local service wrapper to reconcile the
LaunchAgent. With supervision enabled, stop the service with `launchctl
bootout`; killing only the webtmux process is treated as a failure and launchd
starts it again.

## Architecture

```
Browser                              Go Backend
+------------------+                +------------------+
| xterm.js         |<--WebSocket-->| webtty core      |<--PTY--> tmux
| Lit.js Sidebar   |   (extended)  | tmux controller  |
| Touch Controls   |               |                  |
+------------------+                +------------------+
```

### Extended WebSocket Protocol

WebTmux extends the gotty protocol with tmux-specific message types:

**Client -> Server:**
- `5` TmuxSelectPane - Switch to pane by ID
- `6` TmuxSelectWindow - Switch to window by ID
- `7` TmuxSplitPane - Split current pane (h/v)
- `8` TmuxClosePane - Close pane by ID
- `9` TmuxCopyMode - Enter/exit copy mode
- `B` TmuxScrollUp - Scroll up in copy mode
- `C` TmuxScrollDown - Scroll down in copy mode
- `D` TmuxNewWindow - Create new window

**Server -> Client:**
- `7` TmuxLayoutUpdate - Full layout JSON
- `9` TmuxModeUpdate - Copy mode state

## Development

### Project Structure

```
webtmux/
├── main.go                 # CLI entry point
├── server/                 # HTTP server & WebSocket handlers
├── webtty/                 # WebTTY protocol implementation
├── pkg/tmux/               # Tmux controller
├── backend/localcommand/   # PTY backend
├── bindata/static/         # Embedded web assets
│   ├── index.html
│   └── js/bundle.js        # Generated frontend bundle
└── resources/              # Source assets (for development)
```

### Building

```bash
# Development build (copies fresh assets)
make dev

# Production build
make build

# Cross-compile all platforms
make cross-compile

# Create release archives
make release
```

### Tech Stack

- **Backend**: Go, gorilla/websocket
- **Frontend**: xterm.js and Lit
- **Embedded Assets**: Go 1.16+ embed directive

## Contributing and Security

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before
opening a pull request. Please report vulnerabilities privately as described
in [SECURITY.md](SECURITY.md), not in a public issue. Community participation
is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Credits

This project is based on the original
[chrismccord/webtmux](https://github.com/chrismccord/webtmux), which is a fork
of [gotty](https://github.com/yudai/gotty) by Iwasaki Yudai.

## License

MIT License - See [LICENSE](LICENSE) file for details.
