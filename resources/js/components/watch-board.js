// The watch board: every pane in the tmux session/window hierarchy.
//
// It is an index of the real server, with a small output preview for each pane.
import { LitElement, html, css } from 'lit';

const POLL_MS = 5000;

class WebtmuxWatchBoard extends LitElement {
  static properties = {
    board: { type: Object },
    stale: { type: Object },
    // "full" is the touch front door; "rail" is the always-present desktop
    // column beside the terminal, where the board never has the screen to
    // itself and has to stay quiet enough to sit next to live output.
    variant: { type: String, reflect: true },
    activePane: { type: String },
  };

  static styles = css`
    :host {
      display: flex;
      flex-direction: column;
      height: 100%;
      background: transparent;
      overflow: hidden;
    }

    /* ---- compact board header ---- */

    header {
      flex-shrink: 0;
      padding: calc(12px + env(safe-area-inset-top)) 16px 12px;
      border-bottom: 1px solid var(--line);
      background: var(--bg-lift);
    }

    .eyebrow {
      display: flex;
      justify-content: space-between;
      align-items: baseline;
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      color: var(--faint);
    }

    /* ---- cards ---- */

    .list {
      flex: 1;
      overflow-y: auto;
      -webkit-overflow-scrolling: touch;
      padding-bottom: calc(16px + env(safe-area-inset-bottom));
    }

    .session {
      border-bottom: 1px solid var(--line);
    }

    .session-head,
    .window-head {
      display: flex;
      align-items: baseline;
      min-width: 0;
      margin: 0;
      font-family: var(--mono);
    }

    .session-head {
      position: sticky;
      top: 0;
      z-index: 1;
      justify-content: space-between;
      gap: 10px;
      padding: 9px 14px 8px;
      background: var(--bg-lift);
      color: var(--text);
      font-size: 16px;
      font-weight: 600;
    }

    .session-count,
    .window-count {
      flex-shrink: 0;
      color: var(--faint);
      font-size: 10px;
      font-weight: 400;
      font-variant-numeric: tabular-nums;
    }

    .window-head {
      gap: 7px;
      padding: 7px 14px 6px;
      border-top: 1px solid var(--line-soft);
      border-bottom: 1px solid var(--line-soft);
      color: var(--muted);
      font-size: 14px;
      font-weight: 600;
    }

    .window-index {
      color: var(--faint);
      font-variant-numeric: tabular-nums;
    }

    .window-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .card {
      content-visibility: auto;
      contain-intrinsic-size: 92px;
      display: flex;
      width: 100%;
      border: none;
      border-bottom: 1px solid var(--line-soft);
      /* Transparent so the list and its surrounding rail read as one surface;
         an opaque card colour leaves a slab of dead panel below the last one. */
      background: transparent;
      padding: 0;
      text-align: left;
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
      font: inherit;
      color: inherit;
    }

    .card:active {
      background: var(--bg-lift);
    }

    .body {
      flex: 1;
      min-width: 0;
      padding: 11px 14px 12px;
    }

    .meta {
      display: flex;
      align-items: baseline;
      gap: 8px;
    }

    .addr {
      font-family: var(--mono);
      font-size: 12px;
      font-weight: 600;
      letter-spacing: -0.01em;
      color: var(--text);
      white-space: nowrap;
    }

    .cmd {
      font-family: var(--mono);
      font-size: 11px;
      color: var(--muted);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .dot {
      width: 5px;
      height: 5px;
      border-radius: 50%;
      background: var(--signal);
      flex-shrink: 0;
      align-self: center;
    }

    /* The tail is the pane's real output. Older lines fade upward, so a card
       reads as a live window rather than a static excerpt. */
    .tail {
      margin: 7px 0 0;
      font-family: var(--mono);
      font-size: 11px;
      line-height: 1.45;
      color: var(--faint);
      white-space: pre;
      overflow: hidden;
      text-overflow: ellipsis;
      /* Older lines recede, but only just: the first line is often the most
         useful one on the card, so it must stay readable. */
      -webkit-mask-image: linear-gradient(to bottom, rgba(0, 0, 0, 0.45), #000 40%);
      mask-image: linear-gradient(to bottom, rgba(0, 0, 0, 0.45), #000 40%);
    }

    .tail div {
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .empty {
      padding: 40px 20px;
      text-align: center;
      color: var(--faint);
      font-family: var(--sans);
      font-size: 14px;
    }

    /* ---- rail: the desktop column ----
       Same information, tuned down. Beside a live terminal the board must be
       scannable in peripheral vision without competing for attention, so the
       headline shrinks, the tail drops to two lines, and the currently viewed
       pane is marked so the column and the terminal never disagree. */

    :host([variant="rail"]) header {
      padding: 12px 13px 11px;
    }

    :host([variant="rail"]) .body {
      padding: 9px 12px 10px;
    }

    :host([variant="rail"]) .session-head {
      padding: 7px 12px 6px;
      font-size: 14px;
    }

    :host([variant="rail"]) .window-head {
      padding: 6px 12px 5px;
      font-size: 12px;
    }

    :host([variant="rail"]) .addr {
      font-size: 11px;
    }

    :host([variant="rail"]) .cmd {
      font-size: 10px;
    }

    :host([variant="rail"]) .tail {
      margin-top: 5px;
    }

    /* Which pane the terminal is showing, so the two halves stay in sync. */
    .card.viewing {
      background: var(--card);
    }

    .card.viewing .body {
      box-shadow: inset 1px 0 0 var(--text);
    }
  `;

  constructor() {
    super();
    this.board = null;
    this.variant = 'full';
    this.activePane = '';
    this._timer = null;

    window.addEventListener('webtmux-watch-update', (e) => {
      this._pending = false;
      clearTimeout(this._pendingTimer);
      this.board = e.detail;
    });
    window.addEventListener('webtmux-watch-ack', () => {
      this._pending = false;
      clearTimeout(this._pendingTimer);
    });
    window.addEventListener('tmux-layout-update', (e) => {
      this.activePane = e.detail?.activePaneId || '';
    });
  }

  connectedCallback() {
    super.connectedCallback();
    this.poll();
    this._timer = setInterval(() => this.poll(), POLL_MS);
    // Stop polling a board nobody is looking at.
    this._visibility = () => !document.hidden && this.poll();
    document.addEventListener('visibilitychange', this._visibility);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this._timer);
    clearTimeout(this._pendingTimer);
    document.removeEventListener('visibilitychange', this._visibility);
  }

  poll() {
    // Both the touch board and the desktop rail stay mounted, so only the one
    // actually on screen should ask the server to capture every pane.
    // Measure the box rather than reading offsetParent: this element lives
    // inside the sidebar's shadow tree, where offsetParent is null even when
    // the element is perfectly visible.
    if (document.hidden || this._pending) return;
    const box = this.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) return;
    this._pending = true;
    this._pendingTimer = setTimeout(() => { this._pending = false; }, 10000);
    window.webtmux?.requestWatch();
  }

  render() {
    const panes = this.board?.panes || [];
    return html`
      <header>
        <div class="eyebrow">
          <span>Panes</span>
          <span>${panes.length} panes</span>
        </div>
      </header>

      <div class="list">
        ${panes.length
          ? this.groups(panes).map(session => this.session(session))
          : html`<p class="empty">${this.board ? '没有可显示的 pane' : '连接中…'}</p>`}
      </div>
    `;
  }

  groups(panes) {
    const sessions = new Map();

    for (const pane of panes) {
      let session = sessions.get(pane.session);
      if (!session) {
        session = { name: pane.session || '未命名 session', windows: new Map(), count: 0 };
        sessions.set(pane.session, session);
      }

      const windowKey = pane.windowId || `${pane.windowIndex}:${pane.windowName}`;
      let win = session.windows.get(windowKey);
      if (!win) {
        win = {
          id: windowKey,
          index: pane.windowIndex ?? 0,
          name: pane.windowName || '未命名 window',
          panes: [],
        };
        session.windows.set(windowKey, win);
      }

      win.panes.push(pane);
      session.count++;
    }

    return [...sessions.values()]
      .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }))
      .map(session => ({
        ...session,
        windows: [...session.windows.values()]
          .sort((a, b) => a.index - b.index)
          .map(win => ({
            ...win,
            panes: win.panes.slice().sort((a, b) => (a.paneIndex ?? 0) - (b.paneIndex ?? 0)),
          })),
      }));
  }

  session(session) {
    return html`
      <section class="session">
        <h2 class="session-head">
          <span>${session.name}</span>
          <span class="session-count">${session.count} panes</span>
        </h2>
        ${session.windows.map(win => html`
          <section class="window">
            <h3 class="window-head" title="${win.index}:${win.name}">
              <span class="window-index">${win.index}:</span>
              <span class="window-name">${win.name}</span>
              <span class="window-count">${win.panes.length}</span>
            </h3>
            ${win.panes.map(p => this.card(p))}
          </section>
        `)}
      </section>
    `;
  }

  card(p) {
    // Trim by line count rather than by CSS height: the newest line carries
    // whatever you have to answer, so it must be the one that survives.
    const lines = this.variant === 'rail' ? 2 : 4;
    const tail = (p.tail || []).slice(-lines);

    return html`
      <button
        class="card ${p.id === this.activePane ? 'viewing' : ''}"
        @click=${() => this.open(p)}
      >
        <span class="body">
          <span class="meta">
            <span class="addr">${p.address}</span>
            <span class="cmd">${p.command}</span>
            ${p.bell ? html`<span class="dot"></span>` : ''}
          </span>
          ${tail.length
            ? html`<div class="tail">${tail.map(l => html`<div>${l || ' '}</div>`)}</div>`
            : ''}
        </span>
      </button>
    `;
  }

  open(p) {
    window.webtmux?.openPane(p.id);
  }
}

customElements.define('webtmux-watch-board', WebtmuxWatchBoard);
