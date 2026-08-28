// Pane map: the tmux window drawn at its real proportions.
//
// tmux's value is spatial — you remember where things are, not what they are
// called. A list of "Pane 1 / Pane 2 / Pane 3" throws that away, so this draws
// each pane at its actual position and size using the geometry already carried
// in the layout, and labels it with the command running inside.
import { LitElement, html, css } from 'lit';

// tmux reports sizes in character cells, which are roughly twice as tall as
// they are wide. Correcting for that makes the map match what you see.
const CELL_ASPECT = 0.5;

class WebtmuxPaneMap extends LitElement {
  static properties = {
    layout: { type: Object },
    // "zoom" focuses the tapped pane (touch); "select" only moves the cursor.
    mode: { type: String },
  };

  static styles = css`
    :host {
      display: block;
    }

    .frame {
      position: relative;
      width: 100%;
      /* A tall window would otherwise push the map to the height of the whole
         rail; it is a reference, not the main event. */
      max-height: 132px;
      margin: 0 auto;
      background: var(--bg);
      border: 1px solid var(--line);
      border-radius: 3px;
      overflow: hidden;
    }

    .pane {
      position: absolute;
      background: var(--raised);
      border: 1px solid transparent;
      box-shadow: inset 0 0 0 1px var(--line-soft);
      color: var(--faint);
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      justify-content: flex-start;
      gap: 1px;
      padding: 5px 6px;
      cursor: pointer;
      overflow: hidden;
      font-family: var(--mono);
      -webkit-tap-highlight-color: transparent;
      transition: background 0.12s, color 0.12s;
    }

    .pane:hover {
      background: var(--raised-hi);
      color: var(--muted);
    }

    .pane.active {
      background: var(--raised-hi);
      border-color: var(--text);
      box-shadow: none;
      color: var(--text);
    }

    .pane.dimmed {
      opacity: 0.35;
    }

    .idx {
      font-size: 11px;
      font-variant-numeric: tabular-nums;
      line-height: 1;
      font-weight: 600;
    }

    .cmd {
      font-size: 9px;
      line-height: 1.2;
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      opacity: 0.75;
    }

    .zoom-tag {
      position: absolute;
      top: 4px;
      right: 5px;
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
      color: var(--text);
    }

    .empty {
      padding: 14px 0;
      text-align: center;
      color: var(--faint);
      font-family: var(--mono);
      font-size: var(--legend-size);
      letter-spacing: var(--legend-track);
    }
  `;

  constructor() {
    super();
    this.layout = null;
    this.mode = 'select';
  }

  activeWindow() {
    return this.layout?.windows?.find(w => w.active);
  }

  render() {
    const win = this.activeWindow();
    const panes = win?.panes || [];
    if (!panes.length) return html`<div class="empty">NO PANES</div>`;

    // The window's extent in cells, derived from the panes themselves so the
    // map stays correct even while tmux is mid-resize.
    const cols = panes.reduce((max, p) => Math.max(max, p.left + p.width), 0) || 1;
    const rows = panes.reduce((max, p) => Math.max(max, p.top + p.height), 0) || 1;
    const aspect = cols / (rows / CELL_ASPECT);

    // A zoomed window reports the zoomed pane at full size, so the map already
    // collapses to a single cell; the tag explains why.
    const zoomed = !!win?.zoomed;

    return html`
      <div class="frame" style="aspect-ratio: ${aspect.toFixed(3)}">
        ${panes.map(pane => {
          const active = pane.id === this.layout?.activePaneId;
          return html`
            <div
              class="pane ${active ? 'active' : ''} ${zoomed && !active ? 'dimmed' : ''}"
              style="
                left: ${(pane.left / cols) * 100}%;
                top: ${(pane.top / rows) * 100}%;
                width: ${(pane.width / cols) * 100}%;
                height: ${(pane.height / rows) * 100}%;
              "
              role="button"
              tabindex="0"
              title="${pane.command || ''}"
              @click=${() => this.pick(pane.id)}
              @keydown=${(e) => (e.key === 'Enter' || e.key === ' ') && this.pick(pane.id)}
            >
              <span class="idx">${pane.index}</span>
              <span class="cmd">${pane.command || ''}</span>
            </div>
          `;
        })}
        ${zoomed ? html`<span class="zoom-tag">ZOOM</span>` : ''}
      </div>
    `;
  }

  pick(paneId) {
    if (this.mode === 'zoom') {
      window.webtmux?.zoomPane(paneId, true);
    } else {
      window.webtmux?.selectPane(paneId);
    }
    this.dispatchEvent(new CustomEvent('pane-picked', { detail: paneId, bubbles: true, composed: true }));
  }
}

customElements.define('webtmux-pane-map', WebtmuxPaneMap);
