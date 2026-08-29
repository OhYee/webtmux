// The key panel: a grouped drawer opened from the terminal toolbar.
//
// A soft keyboard has no Ctrl, no Alt, no Escape and no arrows, which is most
// of what a terminal is driven by. All supplemental keys live in one foldable
// panel, leaving the bottom of the terminal unobstructed while it is closed.
//
// Everything renders from keys.json, so there is exactly one place that decides
// what a key is called and what bytes it sends.
//
// Modifiers latch rather than fire. Tap Ctrl, then type a letter on the normal
// keyboard, and the two combine — the same sticky-key bargain Termux makes,
// because a phone cannot hold two keys at once.
import { LitElement, html, css } from 'lit';

class WebtmuxKeyBar extends LitElement {
  static properties = {
    open: { type: Boolean, reflect: true },
    row: { type: Array },
    groups: { type: Array },
    activeGroup: { type: Number },
    mods: { type: Object },
    configOpen: { type: Boolean },
    configText: { type: String },
    configWritable: { type: Boolean },
    configStatus: { type: String },
    saving: { type: Boolean },
  };

  static styles = css`
    :host {
      display: block;
      height: 0;
      position: relative;
      /* xterm's link layer is a full-size canvas. Without an explicit
         stacking level it is painted above this zero-height flex item, so the
         drawer remains visible but every tap is intercepted by the terminal. */
      z-index: 100;
      pointer-events: none;
      flex-shrink: 0;
    }

    /* A physical keyboard has all of these already. */
    @media (min-width: 1024px) {
      :host {
        display: none;
      }
    }

    /*
     * The drawer floats over the terminal rather than shrinking it. Terminal
     * size is shared with tmux, so opening a local control must not resize
     * somebody else's client.
     */
    .drawer {
      position: absolute;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 1;
      pointer-events: auto;
      background: var(--bg-lift);
      border-top: 1px solid var(--line);
      box-shadow: 0 -12px 24px rgba(0, 0, 0, 0.45);
      padding-bottom: env(safe-area-inset-bottom);
    }

    .tabs {
      display: flex;
      align-items: stretch;
      height: 34px;
      border-bottom: 1px solid var(--line-soft);
      overflow-x: auto;
      scrollbar-width: none;
      -webkit-overflow-scrolling: touch;
    }

    .tab {
      position: relative;
      flex-shrink: 0;
      border: none;
      background: transparent;
      color: var(--faint);
      padding: 0 13px;
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      text-transform: uppercase;
      cursor: pointer;
      white-space: nowrap;
      -webkit-tap-highlight-color: transparent;
    }

    .tab.active {
      color: var(--text);
    }

    .tab.active::after {
      content: '';
      position: absolute;
      left: 9px;
      right: 9px;
      bottom: 0;
      height: 2px;
      background: var(--text);
    }

    .tab.config {
      margin-left: auto;
      border-left: 1px solid var(--line-soft);
    }

    /*
     * A group rarely divides evenly into four, so the separators are drawn by
     * the cells themselves rather than by a coloured gap in the container: an
     * unfilled slot in the last row then reads as empty space, not as a hole
     * where the background shows through.
     *
     * The drawer is capped rather than allowed to grow, because a phone held
     * sideways has barely more height than the drawer wants, and the terminal
     * it covers is the thing being worked on.
     */
    .grid {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      max-height: 46vh;
      overflow-y: auto;
      overscroll-behavior: contain;
      -webkit-overflow-scrolling: touch;
    }

    .cell {
      background: transparent;
      border: none;
      border-right: 1px solid var(--line-soft);
      border-bottom: 1px solid var(--line-soft);
      color: var(--text);
      padding: 8px 4px;
      font-family: var(--mono);
      font-size: 12px;
      cursor: pointer;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 2px;
      min-height: 44px;
      touch-action: manipulation;
      -webkit-tap-highlight-color: transparent;
    }

    .cell:active {
      background: var(--card-hi);
    }

    /* Landscape on a phone leaves almost no height; trade the chord captions
       and some padding for keeping the terminal visible behind the drawer. */
    @media (max-height: 480px) {
      .grid {
        max-height: 38vh;
      }

      .cell {
        min-height: 36px;
        padding: 5px 4px;
      }

      .cell .chord {
        display: none;
      }
    }

    .cell.wide {
      grid-column: span 2;
    }

    .cell .chord {
      font-size: 9px;
      color: var(--faint);
      font-variant-numeric: tabular-nums;
    }

    .empty {
      grid-column: 1 / -1;
      color: var(--faint);
      font-family: var(--sans);
      font-size: 11px;
      text-align: center;
      padding: 14px 8px;
    }

    .tabs::-webkit-scrollbar {
      display: none;
    }

    /* A latched modifier is a live state, so it inverts rather than tinting —
       there is no mistaking it for a button that merely looks pressed. */
    .mod.on {
      background: var(--text);
      color: var(--bg);
      font-weight: 600;
    }

    .config-panel {
      display: flex;
      flex-direction: column;
      gap: 8px;
      padding: 10px;
      max-height: 52vh;
    }

    .config-editor {
      box-sizing: border-box;
      width: 100%;
      min-height: 210px;
      max-height: 38vh;
      resize: vertical;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 5px;
      background: var(--bg);
      color: var(--text);
      font: 11px/1.45 var(--mono);
      tab-size: 2;
    }

    .config-editor:focus {
      outline: none;
      border-color: var(--muted);
    }

    .config-status {
      min-height: 1.4em;
      color: var(--faint);
      font: 11px/1.4 var(--sans);
    }

    .config-status.error {
      color: var(--alert);
    }

    .config-actions {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
    }

    .config-actions button {
      min-height: 36px;
      padding: 0 14px;
      border: 1px solid var(--line);
      border-radius: 5px;
      background: var(--card);
      color: var(--text);
      font: 11px var(--mono);
      cursor: pointer;
    }

    .config-actions button:disabled {
      color: var(--faint);
      cursor: default;
    }

  `;

  constructor() {
    super();
    this.open = false;
    this.row = [];
    this.groups = [];
    this.activeGroup = 0;
    this.mods = { ctrl: false, alt: false, shift: false };
    this.configOpen = false;
    this.configText = '';
    this.configWritable = false;
    this.configStatus = '';
    this.saving = false;

    this.loadKeys();

    window.addEventListener('webtmux-toggle-keys', () => this.toggleDrawer());

    // The terminal clears latched modifiers once it consumes them, so mirror
    // that back into the button state.
    window.addEventListener('webtmux-mods-changed', (e) => {
      this.mods = { ...e.detail };
    });
  }

  async loadKeys() {
    try {
      const res = await fetch(this.endpoint('keys.json'), { credentials: 'same-origin' });
      const panel = await res.json();
      this.row = (panel.row || []).filter(k => k && k.seq);
      this.groups = panel.groups || [];
    } catch (e) {
      console.warn('Failed to load key bar:', e);
      this.row = [];
      this.groups = [];
    }
  }

  render() {
    return this.open ? this.renderDrawer() : '';
  }

  renderDrawer() {
    const groups = this.panelGroups();
    const group = groups[this.activeGroup] || groups[0];

    return html`
      <div class="drawer">
        <div class="tabs">
          ${groups.map((g, i) => html`
            <button
              class="tab ${!this.configOpen && i === this.activeGroup ? 'active' : ''}"
              @click=${() => {
                this.configOpen = false;
                this.activeGroup = i;
              }}
            >${g.label}</button>
          `)}
          <button
            class="tab config ${this.configOpen ? 'active' : ''}"
            @click=${this.openConfig}
          >配置</button>
        </div>

        ${this.configOpen
          ? this.renderConfig()
          : html`
              <div class="grid">
                ${group
                  ? group.keys.map(k => this.renderCell(k))
                  : html`<div class="empty">按键配置未加载</div>`}
              </div>
            `}
      </div>
    `;
  }

  renderConfig() {
    const error = this.configStatus.startsWith('错误：');
    return html`
      <div class="config-panel">
        <textarea
          class="config-editor"
          spellcheck="false"
          .value=${this.configText}
          @input=${(e) => (this.configText = e.target.value)}
        ></textarea>
        <div class="config-status ${error ? 'error' : ''}">
          ${this.configStatus || (this.configWritable
            ? '保存后立即刷新按键面板。'
            : '启动时使用 --keys FILE 后可保存；当前仅供查看。')}
        </div>
        <div class="config-actions">
          <button @click=${() => (this.configOpen = false)}>返回按键</button>
          <button
            ?disabled=${!this.configWritable || this.saving}
            @click=${this.saveConfig}
          >${this.saving ? '保存中…' : '保存'}</button>
        </div>
      </div>
    `;
  }

  async openConfig() {
    this.configOpen = true;
    this.configStatus = '读取中…';

    try {
      const res = await fetch(this.endpoint('keys-config.json'), {
        credentials: 'same-origin',
      });
      const config = await res.json();
      if (!res.ok) throw new Error(config.error || `HTTP ${res.status}`);
      this.configText = config.content || '';
      this.configWritable = !!config.writable;
      this.configStatus = config.error ? `错误：${config.error}` : '';
    } catch (e) {
      this.configWritable = false;
      this.configStatus = `错误：${e.message}`;
    }
  }

  async saveConfig() {
    this.saving = true;
    this.configStatus = '';

    try {
      // Parse locally first so syntax errors do not need a round trip.
      JSON.parse(this.configText);
      const res = await fetch(this.endpoint('keys-config.json'), {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: this.configText,
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);

      this.configText = data.content;
      const panel = JSON.parse(data.content);
      this.row = (panel.row || []).filter(k => k && k.seq);
      this.groups = panel.groups || [];
      this.configStatus = '已保存并应用。';
    } catch (e) {
      this.configStatus = `错误：${e.message}`;
    } finally {
      this.saving = false;
    }
  }

  endpoint(name) {
    const base = window.location.pathname.replace(/[^/]*$/, '');
    return `${base}${name}`;
  }

  panelGroups() {
    const modifiers = [
      { label: 'CTRL', mod: 'ctrl', hint: '点亮后再敲一个键，两者组合发送' },
      { label: 'ALT', mod: 'alt', hint: '点亮后再敲一个键，两者组合发送' },
      { label: 'SHIFT', mod: 'shift', hint: '点亮后再敲一个键，两者组合发送' },
    ];
    return [{ label: 'Keys', keys: [...modifiers, ...this.row] }, ...this.groups];
  }

  renderCell(k) {
    return html`
      <button
        class="cell ${k.wide ? 'wide' : ''} ${k.mod ? `mod ${this.mods[k.mod] ? 'on' : ''}` : ''}"
        title="${k.hint || ''}"
        @click=${() => k.mod ? this.toggleMod(k.mod) : this.send(k.seq)}
      >
        <span>${k.label}</span>
        ${k.key ? html`<span class="chord">${k.key}</span>` : ''}
      </button>
    `;
  }

  toggleDrawer() {
    this.open = !this.open;
    window.dispatchEvent(new CustomEvent('webtmux-keys-toggled', { detail: this.open }));
  }

  toggleMod(m) {
    this.mods = { ...this.mods, [m]: !this.mods[m] };
    window.webtmux?.setModifiers(this.mods);
  }

  send(seq) {
    // A latched modifier applies to these too: Ctrl then ← is a word jump in
    // most shells, and it would be strange for the bar to ignore its own state.
    const combined = window.webtmux?.hasArmedMods()
      ? window.webtmux.applyArmedModsToSequence(seq)
      : seq;
    window.webtmux?.sendRaw(combined ?? seq);
    window.webtmux?.terminal?.focus();
  }
}

customElements.define('webtmux-key-bar', WebtmuxKeyBar);
