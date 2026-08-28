// Reader: the pane's output, reflowed.
//
// A terminal is a fixed grid. On a phone that means either scrolling sideways
// or shrinking the text to nothing, and neither is reading. The server hands
// over logical lines with their colours, so here they simply wrap to whatever
// width the screen is — selectable, zoomable, ordinary text.
//
// It makes no assumption about what is running: this is whatever the pane
// printed, so a build log reads as well as an agent does.
import { LitElement, html, css } from 'lit';

const POLL_MS = 1500;
// The first read stays small so the reader opens quickly; scrolling back asks
// for more. The ceiling matches the server's own cap.
const HISTORY_LINES = 500;
const HISTORY_STEP = 1500;

class WebtmuxReaderView extends LitElement {
  static properties = {
    capture: { type: Object },
    paneId: { type: String },
    sending: { type: Boolean },
  };

  static styles = css`
    :host {
      display: flex;
      flex-direction: column;
      flex: 1;
      min-width: 0;
      height: 100%;
      background: var(--bg);
      overflow: hidden;
    }

    header {
      flex-shrink: 0;
      display: flex;
      align-items: stretch;
      height: 44px;
      padding-top: env(safe-area-inset-top);
      background: var(--bg-lift);
      border-bottom: 1px solid var(--line);
    }

    header button {
      border: none;
      background: transparent;
      color: var(--muted);
      font-family: var(--mono);
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    header button:active {
      background: var(--card-hi);
      color: var(--text);
    }

    .back {
      width: 46px;
      font-size: 19px;
      border-right: 1px solid var(--line-soft);
    }

    .where {
      flex: 1;
      min-width: 0;
      display: flex;
      flex-direction: column;
      justify-content: center;
      gap: 1px;
      padding: 0 12px;
    }

    .addr {
      font-family: var(--mono);
      font-size: 13px;
      font-weight: 600;
      letter-spacing: -0.01em;
      color: var(--text);
    }

    .cmd {
      font-family: var(--mono);
      font-size: 10px;
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      color: var(--faint);
    }

    .tool {
      width: 54px;
      border-left: 1px solid var(--line-soft);
      flex-direction: column;
      gap: 3px;
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
    }

    /* ---- the output ---- */

    .out {
      flex: 1;
      overflow-y: auto;
      -webkit-overflow-scrolling: touch;
      padding: 12px 14px 16px;
      font-family: var(--mono);
      font-size: 13px;
      line-height: 1.55;
      color: var(--text);
      /* The whole point: wrap to the screen, not to the terminal's columns. */
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      -webkit-text-size-adjust: 100%;
    }

    .line {
      min-height: 1.55em;
    }

    .mark {
      color: var(--faint);
      font-family: var(--sans);
      font-size: 11px;
      text-align: center;
      padding: 6px 0 12px;
      border-bottom: 1px solid var(--line-soft);
      margin-bottom: 10px;
    }

    .empty {
      color: var(--faint);
      font-family: var(--sans);
      font-size: 13px;
      text-align: center;
      padding: 40px 0;
    }

    /* ANSI palette, matching the terminal's own theme so the two views agree. */
    .f0  { color: #3a3a40 } .f8  { color: #6a6a73 }
    .f1  { color: #e5534b } .f9  { color: #ff6b62 }
    .f2  { color: #57ab5a } .f10 { color: #6bc46d }
    .f3  { color: #c69026 } .f11 { color: #daaa3f }
    .f4  { color: #539bf5 } .f12 { color: #6cb6ff }
    .f5  { color: #b083f0 } .f13 { color: #c297ff }
    .f6  { color: #39c5cf } .f14 { color: #56d4dd }
    .f7  { color: #b9b9c0 } .f15 { color: #f0f0f2 }

    .b0 { background: #3a3a40 } .b1 { background: #e5534b }
    .b2 { background: #57ab5a } .b3 { background: #c69026 }
    .b4 { background: #539bf5 } .b5 { background: #b083f0 }
    .b6 { background: #39c5cf } .b7 { background: #b9b9c0 }

    .bo { font-weight: 600 }
    .di { opacity: 0.6 }
    .it { font-style: italic }
    .un { text-decoration: underline }
    .in { filter: invert(1) }

    /* ---- the input, always in reach ---- */

    form {
      flex-shrink: 0;
      display: flex;
      align-items: flex-end;
      gap: 8px;
      padding: 8px 10px calc(8px + env(safe-area-inset-bottom));
      background: var(--bg-lift);
      border-top: 1px solid var(--line);
    }

    textarea {
      flex: 1;
      min-width: 0;
      resize: none;
      max-height: 30vh;
      padding: 9px 11px;
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 6px;
      color: var(--text);
      font-family: var(--mono);
      font-size: 15px; /* 16px or below zooms iOS Safari on focus; 15 is safe with the meta tag */
      line-height: 1.4;
    }

    textarea:focus {
      outline: none;
      border-color: var(--muted);
    }

    textarea::placeholder {
      color: var(--faint);
    }

    .send {
      flex-shrink: 0;
      height: 38px;
      padding: 0 16px;
      border: none;
      border-radius: 6px;
      background: var(--text);
      color: var(--bg);
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      font-weight: 600;
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
    }

    .send:disabled {
      background: var(--card-hi);
      color: var(--faint);
      cursor: default;
    }

    /* On a desktop the rail already says where you are and owns the switch
       between reader and terminal, so this header would only repeat it.
       This has to come after the header rule above, or the two cancel out. */
    @media (min-width: 1024px) {
      header {
        display: none;
      }
    }
  `;

  constructor() {
    super();
    this.capture = null;
    this.paneId = '';
    this.sending = false;
    this._atBottom = true;
    this._history = HISTORY_LINES;

    window.addEventListener('webtmux-capture-update', (e) => {
      if (this.paneId && e.detail?.paneId !== this.paneId) return;
      this._pending = false;
      clearTimeout(this._pendingTimer);

      // An unchanged reply carries no lines; keeping the old capture avoids
      // rebuilding several hundred lines of DOM for nothing.
      this._digest = e.detail?.digest || '';
      if (e.detail?.unchanged) return;

      this._atBottom = this.isAtBottom();
      this.capture = e.detail;
    });
  }

  connectedCallback() {
    super.connectedCallback();
    this._timer = setInterval(() => this.poll(), POLL_MS);
    this.poll();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this._timer);
    clearTimeout(this._pendingTimer);
  }

  poll() {
    if (document.hidden || this._pending) return;
    const box = this.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) return;
    this._pending = true;
    this._pendingTimer = setTimeout(() => { this._pending = false; }, 10000);
    window.webtmux?.requestCapture(this.paneId, this._history, this._digest);
  }

  // More is worth asking for only while the server says there is more.
  hasMore() {
    const c = this.capture;
    // The server reports its own ceiling separately from tmux's, so asking
    // again is only worth it while neither has been reached.
    return !!c && !c.capped && c.available > c.requested;
  }

  // Reaching the top pulls in another slice of scrollback.
  //
  // The read starts small because most of the time you want the end of the
  // output, and shipping a whole session's history to a phone to show its last
  // few lines would be absurd. Going further back is a deliberate act, so it
  // is what asks for the rest.
  onScroll(e) {
    const out = e.target;
    this._atBottom = this.isAtBottom();

    if (out.scrollTop > 240 || this._loading || !this.hasMore()) return;

    this._loading = true;
    this._anchor = out.scrollHeight;
    this._history += HISTORY_STEP;
    // The digest describes the shorter capture, so it must not suppress this.
    this._digest = '';
    this.poll();
  }

  isAtBottom() {
    const out = this.renderRoot?.querySelector('.out');
    if (!out) return true;
    return out.scrollHeight - out.scrollTop - out.clientHeight < 40;
  }

  updated() {
    const out = this.renderRoot.querySelector('.out');
    if (!out) return;

    // Older lines were just prepended: hold the reader where it was rather
    // than letting the new content shove the page around.
    if (this._loading) {
      if (this._anchor) out.scrollTop += out.scrollHeight - this._anchor;
      this._loading = false;
      this._anchor = 0;
      // _loading is not a reactive field, so the marker would otherwise keep
      // claiming it is still reading.
      this.requestUpdate();
      return;
    }

    // Follow new output, but only when the reader was already at the end —
    // yanking the view down while someone is reading history is worse than
    // missing a line.
    if (this._atBottom) out.scrollTop = out.scrollHeight;
  }

  render() {
    const lines = this.capture?.lines || [];

    return html`
      <header>
        <button class="back" @click=${() => window.webtmux?.goBack()} aria-label="返回上一级">‹</button>
        <div class="where">
          <span class="addr">${this.capture?.address || '—'}</span>
          <span class="cmd">${this.capture?.command || ''}</span>
        </div>
        <button class="tool" @click=${() => window.webtmux?.showTerminal()} title="切换到原始终端">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6">
            <rect x="2" y="4" width="20" height="16" rx="1"/>
            <polyline points="6 9 9 12 6 15"/><line x1="12" y1="15" x2="17" y2="15"/>
          </svg>
          终端
        </button>
      </header>

      <div class="out" @scroll=${this.onScroll}>
        ${lines.length ? this.historyMark() : ''}
        ${lines.length
          ? lines.map(line => html`<div class="line">${(line || []).map(s => this.span(s))}</div>`)
          : html`<p class="empty">${this.capture ? '这个 pane 还没有输出' : '读取中…'}</p>`}
      </div>

      <form @submit=${this.send}>
        <textarea
          rows="1"
          placeholder="输入消息，发送到这个 pane…"
          @input=${this.grow}
        ></textarea>
        <button class="send" type="submit" ?disabled=${this.sending}>发送</button>
      </form>
    `;
  }

  // Tell the reader whether there is anything further back, so the top of the
  // list is not silently also the start of the output.
  historyMark() {
    if (this._loading) return html`<div class="mark">读取更早的输出…</div>`;
    if (this.hasMore()) return html`<div class="mark">向上滚动读取更早的输出</div>`;

    // These are different things and saying so matters: one means there is
    // nothing older, the other means there is, and you are not being shown it.
    const c = this.capture;
    if (c?.capped) {
      return html`<div class="mark">已达 ${c.requested} 行上限，tmux 还留有约 ${c.available} 行（--reader-history 可调）</div>`;
    }
    return html`<div class="mark">已是最早的输出</div>`;
  }

  span(s) {
    return s.s
      ? html`<span class="${s.c || ''}" style="${s.s}">${s.t}</span>`
      : html`<span class="${s.c || ''}">${s.t}</span>`;
  }

  grow(e) {
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = el.scrollHeight + 'px';
  }

  send(e) {
    e.preventDefault();
    const el = this.renderRoot.querySelector('textarea');
    const text = el.value;
    if (!text.trim()) return;

    window.webtmux?.sendToPane(text);
    el.value = '';
    el.style.height = 'auto';
    this._atBottom = true;
  }
}

customElements.define('webtmux-reader-view', WebtmuxReaderView);
