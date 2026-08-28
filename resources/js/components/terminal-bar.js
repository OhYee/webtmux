// Terminal view chrome for touch screens: one bar, nothing else.
//
// Getting here was already a deliberate act — you tapped a specific pane on the
// board — so this carries only what you need once you have arrived: a way back,
// which pane you are in, and the two controls that make a phone-sized terminal
// usable. Everything else stays in the key panel, leaving the rest of the
// screen to the terminal.
import { LitElement, html, css } from 'lit';

class WebtmuxTerminalBar extends LitElement {
  static properties = {
    layout: { type: Object },
    keysOpen: { type: Boolean },
  };

  static styles = css`
    :host {
      display: flex;
      align-items: stretch;
      height: 44px;
      padding-top: env(safe-area-inset-top);
      background: var(--bg-lift);
      border-bottom: 1px solid var(--line);
      flex-shrink: 0;
    }

    button {
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

    button:active {
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
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .cmd {
      font-family: var(--mono);
      font-size: 10px;
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      color: var(--faint);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .tool {
      width: 46px;
      border-left: 1px solid var(--line-soft);
      flex-direction: column;
      gap: 3px;
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
    }

    .tool svg {
      width: 16px;
      height: 16px;
    }

    .tool.on {
      background: var(--card-hi);
      color: var(--text);
    }
  `;

  constructor() {
    super();
    this.layout = null;
    this.keysOpen = false;

    window.addEventListener('tmux-layout-update', (e) => {
      this.layout = e.detail;
    });
    window.addEventListener('webtmux-keys-toggled', (e) => {
      this.keysOpen = e.detail;
    });
  }

  render() {
    const win = this.layout?.windows?.find(w => w.active);
    const pane = win?.panes?.find(p => p.id === this.layout?.activePaneId);
    const address = win
      ? `${this.layout.sessionName}:${win.index}.${pane?.index ?? 0}`
      : '—';
    const zoomed = !!win?.zoomed;

    return html`
      <button class="back" @click=${this.back} aria-label="返回上一级">‹</button>

      <div class="where">
        <span class="addr">${address}</span>
        <span class="cmd">${pane?.command || win?.name || ''}</span>
      </div>

      <button class="tool" @click=${this.readerView} title="切换到阅读模式">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
          <path d="M3 5h8a2 2 0 0 1 2 2v12a2 2 0 0 0-2-2H3z"/>
          <path d="M21 5h-8a2 2 0 0 0-2 2v12a2 2 0 0 1 2-2h8z"/>
        </svg>
        阅读
      </button>

      <button class="tool ${zoomed ? 'on' : ''}" @click=${this.toggleZoom}>
        ${zoomed ? html`
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
            <polyline points="4 13 11 13 11 20"/><polyline points="20 11 13 11 13 4"/>
          </svg>
        ` : html`
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
            <polyline points="14 3 21 3 21 10"/><polyline points="10 21 3 21 3 14"/>
          </svg>
        `}
        ${zoomed ? 'Full' : 'Focus'}
      </button>

      <button class="tool ${this.keysOpen ? 'on' : ''}" @click=${this.toggleKeys}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
          <rect x="2" y="6" width="20" height="12" rx="1"/>
          <line x1="7" y1="14" x2="17" y2="14"/>
          <circle cx="7" cy="10" r="0.6" fill="currentColor"/>
          <circle cx="12" cy="10" r="0.6" fill="currentColor"/>
          <circle cx="17" cy="10" r="0.6" fill="currentColor"/>
        </svg>
        Keys
      </button>
    `;
  }

  back() {
    window.webtmux?.goBack();
  }

  readerView() {
    window.webtmux?.showReader(this.layout?.activePaneId);
  }

  toggleZoom() {
    const zoomed = !!this.layout?.windows?.find(w => w.active)?.zoomed;
    window.webtmux?.zoomPane('', !zoomed);
  }

  toggleKeys() {
    window.dispatchEvent(new CustomEvent('webtmux-toggle-keys'));
  }
}

customElements.define('webtmux-terminal-bar', WebtmuxTerminalBar);
