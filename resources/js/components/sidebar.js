// The desktop rail: the watch board living permanently beside the terminal.
//
// A phone has to choose between watching and working. A desktop does not, so
// the board never goes away — you keep an eye on every pane in peripheral
// vision while the terminal stays live. Clicking a card moves the terminal,
// not the page.
//
// Below the board sits the one thing the board cannot show: how the current
// window is actually arranged.
import { LitElement, html, css } from 'lit';
import './pane-map.js';
import './watch-board.js';

class WebtmuxSidebar extends LitElement {
  static properties = {
    layout: { type: Object },
    collapsed: { type: Boolean, reflect: true },
    view: { type: String },
  };

  static styles = css`
    :host {
      display: flex;
      flex-direction: column;
      width: 268px;
      flex-shrink: 0;
      background: var(--bg-lift);
      border-right: 1px solid var(--line);
      overflow: hidden;
    }

    /* Below the breakpoint the full-screen board takes over. */
    @media (max-width: 1023px) {
      :host {
        display: none;
      }
    }

    :host([collapsed]) {
      width: 34px;
    }

    /* Shrink to the cards rather than stretching: a half-empty list with the
       layout map stranded at the bottom of the rail reads as a gap, not as
       breathing room. With many panes the list scrolls instead. */
    webtmux-watch-board {
      flex: 0 1 auto;
      min-height: 0;
      border-bottom: 1px solid var(--line);
    }

    :host([collapsed]) .full {
      display: none;
    }

    .full {
      display: flex;
      flex-direction: column;
      min-height: 0;
      flex: 1;
    }

    .collapsed-strip {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 9px;
      padding-top: 0;
    }

    /* Collapsed, one neutral mark per pane keeps a sense of board size. */
    .pip {
      width: 4px;
      height: 22px;
      border-radius: 1px;
      background: var(--line);
    }

    /* The rail owns the switch between the two detail views, so the main
       column stays entirely content and grows no chrome of its own. */
    .modes {
      flex-shrink: 0;
      display: flex;
      border-bottom: 1px solid var(--line);
    }

    .mode {
      position: relative;
      flex: 1;
      border: none;
      background: transparent;
      color: var(--faint);
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      padding: 9px 0;
      cursor: pointer;
    }

    .mode:hover {
      color: var(--text);
    }

    .mode.on {
      color: var(--text);
    }

    .mode.on::after {
      content: '';
      position: absolute;
      left: 12px;
      right: 12px;
      bottom: 0;
      height: 2px;
      background: var(--text);
    }

    .layout {
      flex-shrink: 0;
      padding: 11px 12px;
    }

    .legend {
      display: block;
      color: var(--faint);
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      margin: 0 0 7px;
    }

    footer {
      flex-shrink: 0;
      display: flex;
      align-items: stretch;
      border-top: 1px solid var(--line);
      background: var(--bg);
    }

    .where {
      flex: 1;
      min-width: 0;
      padding: 8px 12px;
      font-family: var(--mono);
      font-size: 11px;
      color: var(--muted);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-variant-numeric: tabular-nums;
      display: flex;
      align-items: center;
    }

    .act {
      border: none;
      border-left: 1px solid var(--line);
      background: transparent;
      color: var(--faint);
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      padding: 0 11px;
      cursor: pointer;
    }

    .act:hover {
      color: var(--text);
      background: var(--card-hi);
    }

    .chev {
      width: 33px;
      height: 30px;
      border: none;
      border-bottom: 1px solid var(--line);
      background: transparent;
      color: var(--faint);
      font-family: var(--mono);
      font-size: 14px;
      cursor: pointer;
      margin-bottom: 8px;
    }

    .chev:hover {
      color: var(--text);
      background: var(--card-hi);
    }
  `;

  constructor() {
    super();
    this.layout = null;
    this.collapsed = false;
    this.board = null;

    this.view = document.documentElement.dataset.view || 'terminal';

    window.addEventListener('tmux-layout-update', (e) => {
      this.layout = e.detail;
    });
    window.addEventListener('webtmux-view-changed', (e) => {
      this.view = e.detail;
    });
    window.addEventListener('webtmux-watch-update', (e) => {
      this.board = e.detail;
      if (this.collapsed) this.requestUpdate();
    });
  }

  render() {
    return this.collapsed ? this.renderCollapsed() : this.renderFull();
  }

  renderCollapsed() {
    return html`
      <div class="collapsed-strip">
        <button class="chev" @click=${this.toggle} title="展开值班板">›</button>
        ${this.renderPips()}
      </div>
    `;
  }

  toggle() {
    this.collapsed = !this.collapsed;
  }

  // Collapsed to 34px, retain one neutral mark per pane.
  renderPips() {
    const panes = this.board?.panes || [];
    return html`
      ${panes.map(p => html`
        <span class="pip" title="${p.address} · ${p.command}"></span>
      `)}
    `;
  }

  renderFull() {
    const win = this.layout?.windows?.find(w => w.active);
    const pane = win?.panes?.find(p => p.id === this.layout?.activePaneId);
    const address = win
      ? `${this.layout.sessionName}:${win.index}.${pane?.index ?? 0}`
      : '—';

    return html`
      <div class="full">
        <div class="modes">
          <button class="mode ${this.view !== 'reader' ? 'on' : ''}"
                  @click=${() => window.webtmux?.showTerminal()}>终端</button>
          <button class="mode ${this.view === 'reader' ? 'on' : ''}"
                  @click=${() => window.webtmux?.showReader()}>阅读</button>
        </div>

        <webtmux-watch-board variant="rail"></webtmux-watch-board>

        <div class="layout">
          <span class="legend">Layout</span>
          <webtmux-pane-map .layout=${this.layout} mode="select"></webtmux-pane-map>
        </div>

        <footer>
          <span class="where" title="当前 pane">${address}</span>
          <button class="act" @click=${this.toggle} title="收起值班板">‹</button>
          <button class="act" @click=${() => window.webtmux?.newWindow()} title="新建 Window">+ Win</button>
        </footer>
      </div>
    `;
  }
}

customElements.define('webtmux-sidebar', WebtmuxSidebar);
