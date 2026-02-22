import { resolveColorToRGB, rgbToOklch } from './utils.js';

/**
 * ColorSwatches - localStorage-backed swatch palette for clip colors.
 *
 * Manages a small palette of user-saved colors, stored in localStorage
 * under 'rewind.colorSwatches'. Renders a list of swatches that apply
 * colors to the DataStar-bound clip color inputs when clicked.
 *
 * Usage:
 *   const swatches = new ColorSwatches();
 *   swatches.init();
 *
 * DOM requirements:
 *   [data-color-swatch-list]  - container for rendered swatch rows
 *   [data-color-swatch-name]  - input for swatch name
 *   [data-color-swatch-save]  - button to save current color as swatch
 *   [data-bind="clipColor"]   - DataStar-bound color input
 *   [data-bind="clipColorL"]  - DataStar-bound OKLCH L channel
 *   [data-bind="clipColorC"]  - DataStar-bound OKLCH C channel
 *   [data-bind="clipColorH"]  - DataStar-bound OKLCH H channel
 */

const STORAGE_KEY = 'rewind.colorSwatches';
const MAX_SWATCHES = 24;

export class ColorSwatches {
  constructor() {
    this._initialized = false;
    this._panel = null;
    this._nameInput = null;
    this._saveBtn = null;
  }

  /**
   * Initialize the swatch panel. Safe to call multiple times - will no-op
   * if already initialized and the panel element still exists.
   */
  init() {
    if (this._initialized && this._panel) return;

    const panel = document.querySelector('[data-color-swatch-list]');
    const nameInput = document.querySelector('[data-color-swatch-name]');
    const saveBtn = document.querySelector('[data-color-swatch-save]');
    if (!panel || !nameInput || !saveBtn) return;

    this._initialized = true;
    this._panel = panel;
    this._nameInput = nameInput;
    this._saveBtn = saveBtn;

    saveBtn.addEventListener('click', () => this._handleSave());
    this.render();
  }

  // ---------------------------------------------------------------------------
  // Storage
  // ---------------------------------------------------------------------------

  /**
   * Load swatches from localStorage.
   * @returns {{ name: string, color: string }[]}
   */
  load() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      const data = raw ? JSON.parse(raw) : [];
      if (!Array.isArray(data)) return [];
      return data
        .filter(s => s && typeof s === 'object')
        .map(s => ({
          name: typeof s.name === 'string' ? s.name : '',
          color: typeof s.color === 'string' ? s.color : '',
        }))
        .filter(s => s.color && s.color.length < 128);
    } catch (_) {
      return [];
    }
  }

  /**
   * Save swatches to localStorage.
   * @param {{ name: string, color: string }[]} list
   */
  save(list) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list));
    } catch (_) {
      // Best-effort - quota exceeded, etc.
    }
  }

  // ---------------------------------------------------------------------------
  // Color application
  // ---------------------------------------------------------------------------

  /**
   * Apply a color to the DataStar-bound clip color inputs.
   * Updates both the hex input and the OKLCH channel sliders.
   * @param {string} color
   */
  applyColor(color) {
    const colorInput = document.querySelector('[data-bind="clipColor"]');
    const lInput = document.querySelector('[data-bind="clipColorL"]');
    const cInput = document.querySelector('[data-bind="clipColorC"]');
    const hInput = document.querySelector('[data-bind="clipColorH"]');
    if (!colorInput || !lInput || !cInput || !hInput) return;

    colorInput.value = color;
    colorInput.dispatchEvent(new Event('input', { bubbles: true }));

    const rgb = resolveColorToRGB(color);
    if (rgb) {
      const oklch = rgbToOklch(rgb);
      lInput.value = oklch.L.toFixed(1);
      cInput.value = oklch.C.toFixed(3);
      hInput.value = oklch.H.toFixed(1);

      lInput.dispatchEvent(new Event('input', { bubbles: true }));
      cInput.dispatchEvent(new Event('input', { bubbles: true }));
      hInput.dispatchEvent(new Event('input', { bubbles: true }));
    }

    colorInput.dispatchEvent(new Event('change', { bubbles: true }));
  }

  // ---------------------------------------------------------------------------
  // Rendering
  // ---------------------------------------------------------------------------

  /**
   * Render the swatch list into the panel element.
   */
  render() {
    if (!this._panel) return;

    const list = this.load();
    this._panel.innerHTML = '';

    list.forEach((swatch, idx) => {
      const row = document.createElement('div');
      row.className = 'flex items-center gap-2 border-2 border-white/10 px-2 py-1';

      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'w-6 h-6 border-2 border-white/20';
      chip.style.background = swatch.color || '';
      chip.addEventListener('click', () => this.applyColor(swatch.color || ''));

      const label = document.createElement('div');
      label.className = 'flex-1 text-[11px] text-white/70 font-mono truncate';
      label.textContent = (swatch.name && swatch.name.trim()) || swatch.color || 'Swatch';

      const del = document.createElement('button');
      del.type = 'button';
      del.className = 'text-[10px] text-white/40 hover:text-white/80';
      del.textContent = '\u2715';
      del.addEventListener('click', () => {
        const next = list.filter((_, i) => i !== idx);
        this.save(next);
        this.render();
      });

      row.appendChild(chip);
      row.appendChild(label);
      row.appendChild(del);
      this._panel.appendChild(row);
    });
  }

  // ---------------------------------------------------------------------------
  // Private
  // ---------------------------------------------------------------------------

  _handleSave() {
    const colorInput = document.querySelector('[data-bind="clipColor"]');
    const color = (colorInput?.value || '').trim();
    if (!color) return;

    const name = (this._nameInput?.value || '').trim();
    const list = this.load();
    list.unshift({ name, color });
    this.save(list.slice(0, MAX_SWATCHES));

    if (this._nameInput) this._nameInput.value = '';
    this.render();
  }
}
