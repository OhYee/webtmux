// WebTmux - Main entry point
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';

// Import components
import './components/sidebar.js';
import './components/watch-board.js';
import './components/terminal-bar.js';
import './components/reader-view.js';
import './components/key-bar.js';

// Protocol message types (must match Go constants)
const MSG = {
  // Input (client -> server)
  Input: '1',
  Ping: '2',
  ResizeTerminal: '3',
  SetEncoding: '4',
  TmuxSelectPane: '5',
  TmuxSelectWindow: '6',
  TmuxSplitPane: '7',
  TmuxClosePane: '8',
  TmuxCopyMode: '9',
  TmuxScrollUp: 'B',
  TmuxScrollDown: 'C',
  TmuxNewWindow: 'D',
  TmuxSwitchSession: 'E',
  TmuxZoomPane: 'F',
  TmuxWatch: 'G',
  TmuxGotoPane: 'H',
  TmuxCapture: 'I',

  // Output (server -> client)
  Output: '1',
  Pong: '2',
  SetWindowTitle: '3',
  SetPreferences: '4',
  SetReconnect: '5',
  SetBufferSize: '6',
  TmuxLayoutUpdate: '7',
  TmuxModeUpdate: '9',
  TmuxWatchUpdate: 'C',
  TmuxCaptureUpdate: 'D',
};

// Mirrors maxScrollLines on the server.
const MAX_SCROLL_LINES = 200;

// Desktop goes straight to the terminal; touch starts on the board.
const TOUCH_LAYOUT = window.matchMedia('(max-width: 1023px)');

class WebTmux {
  constructor() {
    this.terminal = null;
    this.fitAddon = null;
    this.ws = null;
    this.reconnectInterval = null;
    this.bufferSize = 1024 * 1024;
    this.inCopyMode = false;
    this.layout = null;
    this.pendingSessionSwitch = null;
    this.lastResize = '';
    this.resizeTimer = 0;
    this.retryDelay = 0;
    this.connectedAt = 0;
    this.heartbeat = 0;
    this.viewDepth = 0;
    this.oscBuffer = ''; // Buffer for OSC sequence detection
    this.mods = { ctrl: false, alt: false, shift: false }; // armed by the key panel

    this.init();
  }

  init() {
    this.setupVisualViewport();

    // Create terminal
    this.terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      lineHeight: 1.2,
      fontFamily: 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace',
      // A neutral background matters beyond taste: a blue-tinted one shifts
      // every ANSI colour drawn on top of it, so blues and cyans in agent
      // output stop reading true.
      theme: {
        background: '#0b0b0c',
        foreground: '#ececee',
        cursor: '#ececee',
        cursorAccent: '#0b0b0c',
        selectionBackground: 'rgba(236, 236, 238, 0.22)',

        black: '#3a3a40',
        red: '#e5534b',
        green: '#57ab5a',
        yellow: '#c69026',
        blue: '#539bf5',
        magenta: '#b083f0',
        cyan: '#39c5cf',
        white: '#b9b9c0',

        brightBlack: '#6a6a73',
        brightRed: '#ff6b62',
        brightGreen: '#6bc46d',
        brightYellow: '#daaa3f',
        brightBlue: '#6cb6ff',
        brightMagenta: '#c297ff',
        brightCyan: '#56d4dd',
        brightWhite: '#f0f0f2',
      },
      scrollback: 0, // tmux handles scrollback via copy mode
      allowProposedApi: true,
    });

    // Add fit addon
    this.fitAddon = new FitAddon();
    this.terminal.loadAddon(this.fitAddon);

    // Open terminal
    const container = document.getElementById('terminal');
    this.terminal.open(container);

    // Try to load WebGL addon
    try {
      const webglAddon = new WebglAddon();
      this.terminal.loadAddon(webglAddon);
    } catch (e) {
      console.warn('WebGL addon not supported:', e);
    }

    // Fit terminal and focus
    this.container = container;
    this.refit();
    this.terminal.focus();

    const resizeObserver = new ResizeObserver(() => this.refit());
    resizeObserver.observe(container);

    // Setup input handling
    this.encoder = new TextEncoder();

    // Intercept arrow keys and control chars to ensure correct sequences
    this.terminal.attachCustomKeyEventHandler((ev) => {
      // Only handle keydown events
      if (ev.type !== 'keydown') return true;

      // Allow Cmd+C / Ctrl+C to copy selected text
      if ((ev.metaKey || ev.ctrlKey) && ev.key === 'c') {
        const selection = this.terminal.getSelection();
        if (selection) {
          navigator.clipboard.writeText(selection).catch(err => {
            console.warn('Failed to copy:', err);
          });
          return false; // Handled
        }
        // No selection - let it pass through as Ctrl+C (interrupt)
        return true;
      }

      // Allow Cmd+V / Ctrl+V to paste
      if ((ev.metaKey || ev.ctrlKey) && ev.key === 'v') {
        ev.preventDefault(); // Prevent browser's native paste
        navigator.clipboard.readText().then(text => {
          if (text) {
            const bytes = this.encoder.encode(text);
            const binary = String.fromCharCode(...bytes);
            this.sendMessage(MSG.Input, btoa(binary));
          }
        }).catch(err => {
          console.warn('Failed to paste:', err);
        });
        return false; // Handled
      }

      // Map arrow keys to CSI sequences (ESC [ A/B/C/D)
      // Using CSI instead of SS3 for better compatibility
      const arrowMap = {
        'ArrowUp': '\x1b[A',
        'ArrowDown': '\x1b[B',
        'ArrowRight': '\x1b[C',
        'ArrowLeft': '\x1b[D',
      };

      if (arrowMap[ev.key]) {
        // Send raw CSI sequence
        const seq = arrowMap[ev.key];
        const binary = String.fromCharCode(...[...seq].map(c => c.charCodeAt(0)));
        this.sendMessage(MSG.Input, btoa(binary));
        return false; // Prevent xterm.js default handling
      }

      // Handle Ctrl+N (down) and Ctrl+P (up) for fzf navigation
      if (ev.ctrlKey && !ev.altKey && !ev.metaKey) {
        const ctrlMap = {
          'n': '\x0e', // Ctrl+N = 0x0e = 14
          'p': '\x10', // Ctrl+P = 0x10 = 16
          'j': '\x0a', // Ctrl+J = newline
          'k': '\x0b', // Ctrl+K
        };
        const key = ev.key.toLowerCase();
        if (ctrlMap[key]) {
          const binary = String.fromCharCode(ctrlMap[key].charCodeAt(0));
          this.sendMessage(MSG.Input, btoa(binary));
          return false;
        }
      }

      return true; // Let xterm.js handle other keys
    });

    this.terminal.onData((data) => {
      // Armed modifiers are applied here rather than on keydown. A soft
      // keyboard rarely reports a usable ev.key — most Android IMEs send
      // keyCode 229 and key "Unidentified" — but the character it produces
      // always arrives here, whatever produced it.
      if (this.hasArmedMods()) {
        const combined = this.applyArmedMods(data);
        if (combined !== null) {
          this.sendRaw(combined);
          return;
        }
      }

      if (this.inCopyMode && data.length === 1) {
        // Exit copy mode on any key press (except scroll keys)
        this.sendMessage(MSG.TmuxCopyMode, '0');
        this.inCopyMode = false;
      }
      // Encode string to bytes, then to base64 (matches original gotty)
      const bytes = this.encoder.encode(data);
      const binary = String.fromCharCode(...bytes);
      this.sendMessage(MSG.Input, btoa(binary));
    });

    // Setup touch/scroll handling for copy mode
    this.setupTouchHandling();

    // Connect WebSocket
    this.connect();

    // Expose for components
    window.webtmux = this;

    // Desktop has no board, so it must not start hidden behind one.
    this.setView(TOUCH_LAYOUT.matches ? 'board' : 'terminal', { replace: true });
    TOUCH_LAYOUT.addEventListener('change', (e) => {
      if (e.matches) {
        this.showBoard({ replace: true });
      } else {
        this.showTerminal({ replace: true });
      }
    });

    window.addEventListener('popstate', (e) => {
      if (!e.state?.webtmuxView) return;
      this.viewDepth = e.state.depth || 0;
      this.applyView(e.state.webtmuxView);
    });
  }

  setupTouchHandling() {
    const container = document.getElementById('terminal');
    let touchStartY = 0;

    // Touch handling for mobile scroll -> copy mode
    container.addEventListener('touchstart', (e) => {
      touchStartY = e.touches[0].clientY;
    }, { passive: true });

    container.addEventListener('touchmove', (e) => {
      const deltaY = touchStartY - e.touches[0].clientY;
      const threshold = 30;

      if (Math.abs(deltaY) > threshold) {
        if (!this.inCopyMode) {
          this.sendMessage(MSG.TmuxCopyMode, '1');
          this.inCopyMode = true;
        }

        const lines = Math.floor(Math.abs(deltaY) / 20);
        if (lines > 0) {
          // Swipe up (deltaY > 0) = scroll DOWN in history (show newer)
          // Swipe down (deltaY < 0) = scroll UP in history (show older)
          this.queueScroll(deltaY > 0 ? lines : -lines);
          touchStartY = e.touches[0].clientY;
        }
      }
    }, { passive: true });

    // Mouse wheel for desktop scroll -> copy mode
    this.terminal.attachCustomWheelEventHandler((event) => {
      // Only intercept scroll up (entering history) - deltaY < 0 = wheel up
      if (event.deltaY < 0) {
        if (!this.inCopyMode) {
          this.sendMessage(MSG.TmuxCopyMode, '1');
          this.inCopyMode = true;
        }
      }

      if (this.inCopyMode) {
        const lines = Math.max(1, Math.floor(Math.abs(event.deltaY) / 50));
        // Wheel up (deltaY < 0) = scroll UP in tmux (show older history)
        // Wheel down (deltaY > 0) = scroll DOWN in tmux (show newer)
        this.queueScroll(event.deltaY < 0 ? -lines : lines);
        return false; // Prevent default scroll
      }

      return true; // Allow normal handling when not in copy mode
    });
  }

  // Mobile browsers lay the page out against the full screen even while the
  // software keyboard covers part of it. Follow the visual viewport instead,
  // so the terminal, reader and their bottom controls shrink into the space
  // that is actually visible.
  setupVisualViewport() {
    const viewport = window.visualViewport;
    let frame = 0;

    const sync = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const height = viewport?.height || window.innerHeight;
        document.documentElement.style.setProperty(
          '--visual-viewport-height',
          `${Math.round(height)}px`,
        );
        this.refit();
      });
    };

    sync();
    window.addEventListener('resize', sync);
    viewport?.addEventListener('resize', sync);
    // iOS may pan the visual viewport to the focused textarea without changing
    // the layout viewport. A scroll notification is the only signal it sends.
    viewport?.addEventListener('scroll', sync);
  }

  // Coalesce scrolling into one message per frame.
  //
  // touchmove fires around sixty times a second and a wheel can be faster
  // still. Sending each one separately turned a single flick into a burst of
  // tmux commands, which is far more work than the gesture is worth and left
  // the terminal chasing input long after the finger had lifted.
  queueScroll(lines) {
    this.pendingScroll = (this.pendingScroll || 0) + lines;

    if (this.scrollQueued) return;
    this.scrollQueued = true;

    requestAnimationFrame(() => {
      this.scrollQueued = false;
      const total = this.pendingScroll || 0;
      this.pendingScroll = 0;
      if (total === 0) return;

      const magnitude = Math.min(Math.abs(total), MAX_SCROLL_LINES);
      this.sendMessage(total > 0 ? MSG.TmuxScrollDown : MSG.TmuxScrollUp, String(magnitude));
    });
  }

  connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}${window.location.pathname}ws`;

    clearTimeout(this.reconnectTimer);
    const ws = new WebSocket(wsUrl, ['webtty']);
    this.ws = ws;

    ws.onopen = () => {
      console.log('WebSocket connected');
      this.connectedAt = Date.now();
      clearInterval(this.heartbeat);
      this.heartbeat = setInterval(() => this.sendMessage(MSG.Ping), 25000);

      // Send auth token
      const authToken = window.gotty_auth_token || '';
      this.ws.send(JSON.stringify({ AuthToken: authToken, Arguments: '' }));

      // Tell server to expect base64 encoded input
      this.sendMessage(MSG.SetEncoding, 'base64');

      // Send initial size and focus terminal
      setTimeout(() => {
        this.lastResize = '';
        this.sendResize();
        this.terminal.focus();
      }, 100);

      // Switch to pending session if we reconnected after session ended
      if (this.pendingSessionSwitch) {
        setTimeout(() => {
          console.log('Switching to session:', this.pendingSessionSwitch);
          this.switchSession(this.pendingSessionSwitch);
          this.pendingSessionSwitch = null;
        }, 200);
      }
    };

    ws.onmessage = (event) => {
      if (this.ws !== ws) return;
      this.handleMessage(event.data);
    };

    ws.onclose = () => {
      if (this.ws !== ws) return;
      console.log('WebSocket closed');
      clearInterval(this.heartbeat);
      this.heartbeat = 0;
      if (Date.now() - this.connectedAt >= 30000) this.retryDelay = 0;

      // Check if there are other sessions to switch to
      const otherSessions = this.layout?.sessions?.filter(s => !s.active) || [];
      if (otherSessions.length > 0) {
        // Auto-reconnect and switch to another session
        this.pendingSessionSwitch = otherSessions[0].name;
        console.log('Auto-reconnecting to session:', this.pendingSessionSwitch);
        this.reconnectTimer = setTimeout(() => this.connect(), this.backoff());
      } else if (this.reconnectInterval) {
        const delay = Math.max(this.backoff(), this.reconnectInterval * 1000);
        this.reconnectTimer = setTimeout(() => this.connect(), delay);
      }
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
    };
  }

  // Back off between reconnects.
  //
  // Retrying every half second regardless is how a server hiccup turns into a
  // stampede: each successful handshake spawns a tmux client and a controller,
  // so a phone left on a dead connection was hammering the very thing it was
  // waiting for.
  backoff() {
    this.retryDelay = Math.min((this.retryDelay || 500) * 2, 15000);
    return this.retryDelay;
  }

  handleMessage(data) {
    const type = data[0];
    const payload = data.slice(1);

    switch (type) {
      case MSG.Output:
        // Decode base64 to Uint8Array for proper UTF-8 handling
        const binaryString = atob(payload);

        // Check for OSC 52 clipboard sequences and handle them
        const processed = this.handleOSC52(binaryString);

        const bytes = new Uint8Array(processed.length);
        for (let i = 0; i < processed.length; i++) {
          bytes[i] = processed.charCodeAt(i);
        }
        this.terminal.write(bytes);
        break;

      case MSG.Pong:
        // Ignore pong
        break;

      case MSG.SetWindowTitle:
        document.title = payload;
        break;

      case MSG.SetPreferences:
        const prefs = JSON.parse(payload);
        if (prefs.fontSize) {
          this.terminal.options.fontSize = prefs.fontSize;
          this.refit();
        }
        break;

      case MSG.SetReconnect:
        this.reconnectInterval = parseInt(payload, 10);
        break;

      case MSG.SetBufferSize:
        this.bufferSize = parseInt(payload, 10);
        break;

      case MSG.TmuxLayoutUpdate:
        this.layout = JSON.parse(payload);
        this.dispatchLayoutUpdate();
        break;

      case MSG.TmuxModeUpdate:
        const modeState = JSON.parse(payload);
        this.inCopyMode = modeState.inCopyMode;
        break;

      case MSG.TmuxWatchUpdate:
        {
          const board = JSON.parse(payload);
          this.watchDigest = board.digest || this.watchDigest || '';
          window.dispatchEvent(new CustomEvent(
            board.unchanged ? 'webtmux-watch-ack' : 'webtmux-watch-update',
            { detail: board },
          ));
        }
        break;

      case MSG.TmuxCaptureUpdate:
        window.dispatchEvent(new CustomEvent('webtmux-capture-update', {
          detail: JSON.parse(payload)
        }));
        break;

      default:
        console.warn('Unknown message type:', type);
    }
  }

  sendMessage(type, payload = '') {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(type + payload);
    } else {
      console.warn('WebSocket not ready, state:', this.ws?.readyState);
    }
  }

  // True only when the terminal actually occupies space. On a phone the board
  // hides the terminal outright, and a hidden element measures as zero.
  isTerminalVisible() {
    if (!this.container) return false;
    const box = this.container.getBoundingClientRect();
    return box.width >= 1 && box.height >= 1;
  }

  // Re-fit the terminal to its container, but never while it is hidden.
  //
  // FitAddon floors its proposal rather than refusing, so measuring a
  // display:none container yields a 10x4 terminal. Pushing that to the PTY
  // resizes the tmux client — and because tmux sizes a window to its smallest
  // attached client, a phone returning to the board would squash the layout of
  // every other client, including the desktop the user is sitting at, until
  // the panes no longer fit and the client dies.
  refit() {
    if (!this.isTerminalVisible()) return;
    this.fitAddon.fit();
    this.sendResize();
  }

  sendResize() {
    if (!this.isTerminalVisible()) return;
    clearTimeout(this.resizeTimer);
    this.resizeTimer = setTimeout(() => {
      if (!this.isTerminalVisible()) return;
      const dims = { columns: this.terminal.cols, rows: this.terminal.rows };
      const payload = JSON.stringify(dims);
      if (payload === this.lastResize) return;
      this.lastResize = payload;
      this.sendMessage(MSG.ResizeTerminal, payload);
    }, 100);
  }

  dispatchLayoutUpdate() {
    // Notify sidebar and other components
    window.dispatchEvent(new CustomEvent('tmux-layout-update', {
      detail: this.layout
    }));
  }

  // Public API for components

  // Write a raw byte sequence to the TTY, as if it had been typed.
  sendRaw(seq) {
    const bytes = this.encoder.encode(seq);
    const binary = String.fromCharCode(...bytes);
    this.sendMessage(MSG.Input, btoa(binary));
  }

  setModifiers(mods) {
    this.mods = { ...mods };
  }

  hasArmedMods() {
    return this.mods.ctrl || this.mods.alt || this.mods.shift;
  }

  // Rewrite typed input using the modifiers armed on the on-screen row.
  //
  // Returns the sequence to send, or null to leave the input alone. Modifiers
  // are one-shot: they clear after a single character, the way a sticky-key
  // keyboard behaves.
  applyArmedMods(data) {
    // Only a single printable character combines cleanly. A paste or an escape
    // sequence passes through untouched.
    if (data.length !== 1 || data < ' ') return null;

    const { ctrl, alt, shift } = this.mods;
    let key = shift ? data.toUpperCase() : data;

    if (ctrl) {
      const code = key.toUpperCase().charCodeAt(0);
      // Ctrl maps @A-Z[\]^_ onto 0x00-0x1f; anything else has no control code.
      if (code >= 0x40 && code <= 0x5f) key = String.fromCharCode(code & 0x1f);
    }
    if (alt) key = '\x1b' + key;

    this.clearMods();
    return key;
  }

  // Apply latched modifiers to a fixed sequence such as an arrow key.
  //
  // CSI sequences carry their modifiers as a parameter rather than as a prefix
  // byte, so Ctrl+Left is "\x1b[1;5D" and not an escaped anything. Sequences
  // with no such form are sent unchanged rather than mangled.
  applyArmedModsToSequence(seq) {
    const { ctrl, alt, shift } = this.mods;
    // CSI modifier parameter: 1 + shift(1) + alt(2) + ctrl(4).
    const param = 1 + (shift ? 1 : 0) + (alt ? 2 : 0) + (ctrl ? 4 : 0);
    this.clearMods();
    if (param === 1) return seq;

    // Arrows and Home/End: ESC [ <letter>  ->  ESC [ 1 ; <param> <letter>
    let m = seq.match(/^\x1b\[([A-HD-F])$/);
    if (m) return `\x1b[1;${param}${m[1]}`;

    // Tilde keys such as Delete and PageUp: ESC [ n ~  ->  ESC [ n ; <param> ~
    m = seq.match(/^\x1b\[(\d+)~$/);
    if (m) return `\x1b[${m[1]};${param}~`;

    return seq;
  }

  clearMods() {
    this.mods = { ctrl: false, alt: false, shift: false };
    window.dispatchEvent(new CustomEvent('webtmux-mods-changed', { detail: this.mods }));
  }

  selectPane(paneId) {
    this.sendMessage(MSG.TmuxSelectPane, paneId);
  }

  // Focus a single pane by zooming its window; pass zoom=false to restore.
  zoomPane(paneId, zoom) {
    this.sendMessage(MSG.TmuxZoomPane, `${zoom ? '1' : '0'}:${paneId || ''}`);
  }

  requestWatch() {
    this.sendMessage(MSG.TmuxWatch, this.watchDigest || '');
  }

  requestCapture(paneId, lines, digest) {
    this.sendMessage(MSG.TmuxCapture, `${lines || 0}:${digest || ''}:${paneId || ''}`);
  }

  // Type into a pane from the reader's input box.
  //
  // The text is wrapped in a bracketed paste so a multi-line message arrives as
  // one paste rather than as a series of Enters, which would make a shell run
  // each line and an agent submit halfway through. Enter is sent separately
  // afterwards to actually commit it.
  sendToPane(text) {
    this.sendRaw('\x1b[200~' + text + '\x1b[201~');
    setTimeout(() => this.sendRaw('\r'), 30);
  }

  // Open a pane from the board: jump to it wherever it lives, then show the
  // terminal. Touch also zooms, because one pane of a split window is
  // unreadable on a phone; a desktop has room for the whole window, and
  // zooming there would fight the layout the user arranged.
  // Open a pane from the board.
  //
  // Touch lands in the reader, where the output is reflowed to the screen
  // instead of being squeezed into the terminal's fixed columns; the raw
  // terminal stays one tap away for anything that needs a real TTY. A desktop
  // has the width for the terminal itself, so it goes straight there.
  openPane(paneId) {
    this.sendMessage(MSG.TmuxGotoPane, paneId);

    if (TOUCH_LAYOUT.matches) {
      this.showReader(paneId);
      return;
    }

    // On a desktop the rail stays put, so picking a card moves the detail view
    // without changing which mode you were in.
    if (document.documentElement.dataset.view === 'reader') {
      this.showReader(paneId);
      return;
    }
    this.zoomPane(paneId, true);
    this.showTerminal();
  }

  showReader(paneId) {
    const reader = document.querySelector('webtmux-reader-view');
    // Falling back to the active pane lets the reader be opened from the rail
    // without picking anything first.
    const target = paneId || this.layout?.activePaneId || '';

    if (reader && reader.paneId !== target) {
      reader.paneId = target;
      reader.capture = null;
      reader._digest = '';
      reader._history = 500;
    }
    this.setView('reader');
  }

  setView(view, { replace = false } = {}) {
    const current = document.documentElement.dataset.view;
    if (replace) {
      window.history.replaceState(
        { webtmuxView: view, depth: this.viewDepth },
        '',
      );
    } else if (current !== view) {
      this.viewDepth++;
      window.history.pushState(
        { webtmuxView: view, depth: this.viewDepth },
        '',
      );
    }
    this.applyView(view);
  }

  applyView(view) {
    document.documentElement.dataset.view = view;
    window.dispatchEvent(new CustomEvent('webtmux-view-changed', { detail: view }));

    if (view === 'terminal') {
      requestAnimationFrame(() => {
        this.refit();
        this.terminal.focus();
      });
    } else if (view === 'reader') {
      requestAnimationFrame(() => {
        document.querySelector('webtmux-reader-view')?.poll();
      });
    }
  }

  showBoard(options) {
    // On a desktop the board is the rail and never leaves, so "back to the
    // board" just means the terminal. Setting a board view there would leave
    // the state disagreeing with what is actually on screen.
    this.setView(TOUCH_LAYOUT.matches ? 'board' : 'terminal', options);
  }

  goBack() {
    window.history.back();
  }

  // The reader remembers which pane it was on, so the terminal bar can hand
  // back to it without the board in between.
  currentReaderPane() {
    return document.querySelector('webtmux-reader-view')?.paneId || '';
  }

  showTerminal(options) {
    this.setView('terminal', options);
  }

  // True when the active window is currently zoomed onto one pane.
  isZoomed() {
    return !!this.layout?.windows?.find(w => w.id === this.layout.activeWindowId)?.zoomed;
  }

  selectWindow(windowId) {
    this.sendMessage(MSG.TmuxSelectWindow, windowId);
  }

  splitPane(horizontal) {
    this.sendMessage(MSG.TmuxSplitPane, horizontal ? 'h' : 'v');
  }

  closePane(paneId) {
    this.sendMessage(MSG.TmuxClosePane, paneId);
  }

  newWindow() {
    this.sendMessage(MSG.TmuxNewWindow, '');
  }

  switchSession(sessionName) {
    this.sendMessage(MSG.TmuxSwitchSession, sessionName);
  }

  enterCopyMode() {
    this.sendMessage(MSG.TmuxCopyMode, '1');
    this.inCopyMode = true;
  }

  exitCopyMode() {
    this.sendMessage(MSG.TmuxCopyMode, '0');
    this.inCopyMode = false;
  }

  // Handle OSC 52 clipboard sequences from tmux
  // Format: ESC ] 52 ; Pc ; Pd BEL  or  ESC ] 52 ; Pc ; Pd ESC \
  handleOSC52(data) {
    const ESC = String.fromCharCode(0x1b);
    const oscStart = ESC + ']52;';
    let result = data;
    let startIdx = data.indexOf(oscStart);

    while (startIdx !== -1) {
      // Find the terminator (BEL \x07 or ST \x1b\\)
      let endIdx = -1;
      let termLen = 1;

      for (let i = startIdx + oscStart.length; i < data.length; i++) {
        if (data.charCodeAt(i) === 0x07) { // BEL
          endIdx = i;
          termLen = 1;
          break;
        }
        if (data.charCodeAt(i) === 0x1b && i + 1 < data.length && data[i + 1] === '\\') { // ST
          endIdx = i;
          termLen = 2;
          break;
        }
      }

      if (endIdx === -1) break;

      // Extract the content between start and terminator
      const content = data.substring(startIdx + oscStart.length, endIdx);

      // Content format: Pc;Pd where Pc is selection and Pd is base64 data
      const semiIdx = content.indexOf(';');
      if (semiIdx !== -1) {
        const base64Data = content.substring(semiIdx + 1);

        if (base64Data && base64Data !== '?') {
          try {
            // Decode base64 to bytes, then UTF-8 decode for proper emoji support
            const binaryStr = atob(base64Data);
            const bytes = Uint8Array.from(binaryStr, c => c.charCodeAt(0));
            const text = new TextDecoder('utf-8').decode(bytes);
            navigator.clipboard.writeText(text);
          } catch (e) {
            // Silently ignore decode errors
          }
        }
      }

      // Remove this OSC sequence from output
      const fullSeq = data.substring(startIdx, endIdx + termLen);
      result = result.replace(fullSeq, '');

      // Look for more
      startIdx = data.indexOf(oscStart, startIdx + 1);
    }

    return result;
  }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
  new WebTmux();
});
