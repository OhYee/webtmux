# WebTmux Makefile
# Builds portable binaries for all standard platforms

VERSION ?= $(shell git describe --tags 2>/dev/null || echo "dev")
GIT_COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME = $(shell date -u '+%Y-%m-%d_%H:%M:%S')
BUILD_OPTIONS = -ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT)"

OUTPUT_DIR = ./builds
BINARY_NAME = webtmux
TMUX_SESSION ?= webtmux
TMUX_WINDOW ?= webtmux
WEBTMUX_FLAGS ?= -w

# Platforms to build for (PTY not supported on Windows)
PLATFORMS = \
	linux/amd64 \
	linux/arm64 \
	linux/arm \
	darwin/amd64 \
	darwin/arm64 \
	freebsd/amd64

export CGO_ENABLED=0

.PHONY: all deps bundle sync-assets check-generated build install test clean cross-compile release dev start logs help

# Default target
all: build

# Install frontend build dependencies (only needed when changing JS).
deps:
	npm ci

# Bundle the browser assets into bindata. Everything is vendored rather than
# pulled from a CDN, because this is typically opened from a phone on a LAN or
# tailnet with no route to the internet.
bundle:
	@npm run --silent bundle
	@cp node_modules/@xterm/xterm/css/xterm.css bindata/static/css/xterm.css

# Verify committed browser assets without changing the working tree.
check-generated:
	@tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	./node_modules/.bin/esbuild resources/js/webtmux.js --bundle --format=esm \
		--minify --target=safari15 --outfile="$$tmpdir/bundle.js" >/dev/null; \
	cmp -s "$$tmpdir/bundle.js" bindata/static/js/bundle.js || \
		{ echo "error: bundle.js is stale; run 'make bundle'"; exit 1; }; \
	cmp -s node_modules/@xterm/xterm/css/xterm.css bindata/static/css/xterm.css || \
		{ echo "error: xterm.css is stale; run 'make bundle'"; exit 1; }

# Sync resources to bindata (for embedding).
# index.html and the stylesheets are copied too: leaving them out means a new
# component added to resources/index.html never reaches the embedded build.
# The bundle is rebuilt whenever the toolchain is present, so editing JS and
# running `make build` cannot silently ship a stale bundle.
sync-assets:
	@cp resources/index.html resources/manifest.json bindata/static/
	@cp resources/index.css resources/xterm_customize.css bindata/static/css/
	@cp resources/favicon.ico resources/icon.svg resources/icon_192.png bindata/static/
	@if [ -d node_modules ]; then \
		$(MAKE) --no-print-directory bundle; \
	else \
		echo "note: node_modules missing, using the committed bundle.js (run 'make deps' after changing JS)"; \
	fi

# Build for current platform
build: sync-assets
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	go build $(BUILD_OPTIONS) -o $(BINARY_NAME) .
	@echo "Done: ./$(BINARY_NAME)"

# Install to GOPATH/bin
install: sync-assets
	go install $(BUILD_OPTIONS) .

# Run tests
test: check-generated
	go test ./...
	go vet ./...

# Clean build artifacts
clean:
	rm -rf $(BINARY_NAME) $(OUTPUT_DIR)

# Cross-compile for all platforms
cross-compile: clean sync-assets
	@echo "Cross-compiling $(BINARY_NAME) $(VERSION) for all platforms..."
	@mkdir -p $(OUTPUT_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		output=$(OUTPUT_DIR)/$(BINARY_NAME)-$$os-$$arch; \
		if [ "$$os" = "windows" ]; then output=$$output.exe; fi; \
		echo "  Building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch go build $(BUILD_OPTIONS) -o $$output . || exit 1; \
	done
	@echo "Done! Binaries in $(OUTPUT_DIR)/"
	@ls -lh $(OUTPUT_DIR)/

# Create release archives
release: cross-compile
	@echo "Creating release archives..."
	@mkdir -p $(OUTPUT_DIR)/dist
	@cd $(OUTPUT_DIR) && for f in $(BINARY_NAME)-*; do \
		if [ -f "$$f" ]; then \
			tar -czf dist/$$f.tar.gz $$f; \
		fi; \
	done
	@cd $(OUTPUT_DIR)/dist && shasum -a 256 * > SHA256SUMS
	@echo "Release archives in $(OUTPUT_DIR)/dist/"
	@ls -lh $(OUTPUT_DIR)/dist/

# Development build with fresh bundled assets
dev: build

# Recreate a predictable tmux home for webtmux. The server runs in the
# webtmux:webtmux pane and browser connections attach back to that same session.
start: build
	@tmux kill-session -t "$(TMUX_SESSION)" 2>/dev/null || true
	@tmux new-session -d -s "$(TMUX_SESSION)" -n "$(TMUX_WINDOW)"
	@tmux set-option -w -t "$(TMUX_SESSION):$(TMUX_WINDOW)" remain-on-exit on
	@tmux respawn-pane -k -t "$(TMUX_SESSION):$(TMUX_WINDOW)" \
		env -u TMUX "$(CURDIR)/$(BINARY_NAME)" $(WEBTMUX_FLAGS) \
		tmux attach-session -t "$(TMUX_SESSION)"
	@echo "Started $(BINARY_NAME) in tmux session $(TMUX_SESSION), window $(TMUX_WINDOW)."
	@echo "View logs: make logs"
	@echo "Open pane: tmux attach-session -t $(TMUX_SESSION)"

logs:
	@tmux capture-pane -p -S - -t "$(TMUX_SESSION):$(TMUX_WINDOW)"

help:
	@echo "WebTmux Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make              Build for current platform"
	@echo "  make build        Build for current platform"
	@echo "  make install      Install to GOPATH/bin"
	@echo "  make test         Run tests"
	@echo "  make clean        Remove build artifacts"
	@echo "  make cross-compile Build for all platforms"
	@echo "  make release      Create release archives"
	@echo "  make dev          Build with fresh assets"
	@echo "  make start        Recreate webtmux:webtmux and serve that tmux session"
	@echo "  make logs         Print the webtmux pane log, including startup failures"
	@echo "  make help         Show this help"
