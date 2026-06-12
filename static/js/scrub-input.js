// ============================================================================
// SCRUB INPUT — DAW-style draggable number input
//
// Click + drag on the label to scrub the value. Click the value to type.
// Used in Premiere, After Effects, Ableton, etc.
//
// Usage (from JS):
//   scrubInput(container, {
//     label: 'X',
//     value: 0.5,
//     min: 0, max: 1, step: 0.01,
//     precision: 3,
//     onChange: (val) => { ... },
//   });
//
// Or declaratively via data attributes:
//   <div data-scrub data-scrub-label="X" data-scrub-value="0.5"
//        data-scrub-min="0" data-scrub-max="1" data-scrub-step="0.01"
//        data-scrub-precision="3"></div>
// ============================================================================

const SENSITIVITY = 0.5; // pixels per unit at step=1
const SHIFT_MULTIPLIER = 0.1; // hold shift for fine control
const ALT_MULTIPLIER = 10; // hold alt for coarse control

/**
 * Create a scrub input inside the given container element.
 * @param {HTMLElement} container
 * @param {Object} opts
 * @returns {{ getValue: () => number, setValue: (v: number) => void, destroy: () => void }}
 */
export function scrubInput(container, opts = {}) {
  const label = opts.label || '';
  const min = opts.min ?? -Infinity;
  const max = opts.max ?? Infinity;
  const step = opts.step ?? 1;
  const precision = opts.precision ?? (step < 1 ? Math.max(0, -Math.floor(Math.log10(step))) : 0);
  let value = clamp(opts.value ?? 0, min, max);
  let onChange = opts.onChange || (() => {});

  // Build DOM
  container.innerHTML = '';
  container.classList.add('scrub-input');

  const labelEl = document.createElement('span');
  labelEl.className = 'scrub-input-label';
  labelEl.textContent = label;
  labelEl.title = `Drag to adjust ${label}`;

  const valueEl = document.createElement('span');
  valueEl.className = 'scrub-input-value';
  valueEl.textContent = formatValue(value, precision);

  const inputEl = document.createElement('input');
  inputEl.type = 'number';
  inputEl.className = 'scrub-input-editor';
  inputEl.step = step;
  inputEl.min = min;
  inputEl.max = max;
  inputEl.style.display = 'none';

  container.appendChild(labelEl);
  container.appendChild(valueEl);
  container.appendChild(inputEl);

  // --- Drag-to-scrub on label ---
  // Use document-level move/up listeners for reliable dragging even when
  // the pointer leaves the small label element (pointer capture can fail
  // if touch-action is not set, or on some browsers).
  let dragging = false;
  let startX = 0;
  let startValue = 0;

  function onPointerDown(e) {
    if (e.button !== 0) return;
    dragging = true;
    startX = e.clientX;
    startValue = value;
    labelEl.classList.add('scrub-active');
    document.body.style.cursor = 'ew-resize';
    document.addEventListener('pointermove', onPointerMove);
    document.addEventListener('pointerup', onPointerUp);
    e.preventDefault();
  }

  function onPointerMove(e) {
    if (!dragging) return;
    const dx = e.clientX - startX;
    let multiplier = 1;
    if (e.shiftKey) multiplier = SHIFT_MULTIPLIER;
    if (e.altKey) multiplier = ALT_MULTIPLIER;
    const delta = dx * step * SENSITIVITY * multiplier;
    const newVal = clamp(startValue + delta, min, max);
    setValue(newVal);
    onChange(value);
  }

  function onPointerUp() {
    if (!dragging) return;
    dragging = false;
    labelEl.classList.remove('scrub-active');
    document.body.style.cursor = '';
    document.removeEventListener('pointermove', onPointerMove);
    document.removeEventListener('pointerup', onPointerUp);
  }

  labelEl.addEventListener('pointerdown', onPointerDown);

  // --- Click value to edit inline ---
  valueEl.addEventListener('dblclick', startEditing);

  function startEditing() {
    inputEl.value = value;
    inputEl.style.display = '';
    valueEl.style.display = 'none';
    inputEl.focus();
    inputEl.select();
  }

  function finishEditing() {
    const parsed = parseFloat(inputEl.value);
    if (!isNaN(parsed)) {
      setValue(clamp(parsed, min, max));
      onChange(value);
    }
    inputEl.style.display = 'none';
    valueEl.style.display = '';
  }

  inputEl.addEventListener('blur', finishEditing);
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { finishEditing(); e.preventDefault(); }
    if (e.key === 'Escape') {
      inputEl.style.display = 'none';
      valueEl.style.display = '';
      e.preventDefault();
    }
  });

  // --- Scroll to adjust ---
  container.addEventListener('wheel', (e) => {
    e.preventDefault();
    e.stopPropagation();
    let multiplier = 1;
    if (e.shiftKey) multiplier = SHIFT_MULTIPLIER;
    if (e.altKey) multiplier = ALT_MULTIPLIER;
    const delta = (e.deltaY > 0 ? -1 : 1) * step * multiplier;
    setValue(clamp(value + delta, min, max));
    onChange(value);
  }, { passive: false });

  // --- Helpers ---
  function setValue(v) {
    // Snap to step precision to avoid floating point drift
    value = roundToStep(clamp(v, min, max), step);
    valueEl.textContent = formatValue(value, precision);
  }

  function getValue() { return value; }

  function destroy() {
    labelEl.removeEventListener('pointerdown', onPointerDown);
    document.removeEventListener('pointermove', onPointerMove);
    document.removeEventListener('pointerup', onPointerUp);
    container.innerHTML = '';
  }

  return { getValue, setValue, destroy };
}

// ============================================================================
// Inline helper used by compose-page.js to generate scrub input HTML
// that works without framework overhead (pure DOM, no import needed).
// ============================================================================

/**
 * Create a scrub input field as raw HTML + attach event listeners.
 * Returns the outer HTML string.  Call `initScrubInputs(parentEl)` after
 * inserting the HTML to wire up the drag/scroll behavior.
 */
export function scrubInputHTML(id, label, value, step, min, max, precision) {
  const displayVal = formatValue(value, precision ?? guessPrecision(step));
  return `<div class="scrub-field" data-scrub-id="${id}" data-scrub-step="${step}" data-scrub-min="${min}" data-scrub-max="${max}" data-scrub-precision="${precision ?? guessPrecision(step)}">` +
    `<span class="scrub-label" title="Drag to adjust ${label}">${label}</span>` +
    `<span class="scrub-value">${displayVal}</span>` +
    `<input type="number" class="scrub-editor" step="${step}" min="${min}" max="${max}" value="${value}" style="display:none"/>` +
    `</div>`;
}

/**
 * After injecting HTML from scrubInputHTML, call this on the parent element
 * to attach pointer-drag, wheel, and inline-edit behavior.
 *
 * @param {HTMLElement} root - the container that holds all `.scrub-field` elements
 * @param {(id: string, value: number) => void} onChange - called on every value change
 */
export function initScrubInputs(root, onChange) {
  root.querySelectorAll('.scrub-field').forEach(el => {
    const id = el.dataset.scrubId;
    const step = parseFloat(el.dataset.scrubStep) || 1;
    const min = parseFloat(el.dataset.scrubMin) ?? -Infinity;
    const max = parseFloat(el.dataset.scrubMax) ?? Infinity;
    const precision = parseInt(el.dataset.scrubPrecision) || 0;

    const labelEl = el.querySelector('.scrub-label');
    const valueEl = el.querySelector('.scrub-value');
    const inputEl = el.querySelector('.scrub-editor');
    if (!labelEl || !valueEl || !inputEl) return;

    let currentVal = parseFloat(valueEl.textContent) || 0;
    let dragging = false, startX = 0, startVal = 0;

    function setVal(v) {
      currentVal = roundToStep(clamp(v, min, max), step);
      valueEl.textContent = formatValue(currentVal, precision);
    }

    // Drag — use document-level move/up so dragging works even when
    // the pointer leaves the small label element
    function onMove(e) {
      if (!dragging) return;
      let mult = 1;
      if (e.shiftKey) mult = SHIFT_MULTIPLIER;
      if (e.altKey) mult = ALT_MULTIPLIER;
      const delta = (e.clientX - startX) * step * SENSITIVITY * mult;
      setVal(startVal + delta);
      onChange(id, currentVal);
    }
    function onUp() {
      if (!dragging) return;
      dragging = false;
      labelEl.classList.remove('scrub-active');
      document.body.style.cursor = '';
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
    }
    labelEl.addEventListener('pointerdown', e => {
      if (e.button !== 0) return;
      dragging = true; startX = e.clientX; startVal = currentVal;
      labelEl.classList.add('scrub-active');
      document.body.style.cursor = 'ew-resize';
      document.addEventListener('pointermove', onMove);
      document.addEventListener('pointerup', onUp);
      e.preventDefault();
    });

    // Double-click to edit
    valueEl.addEventListener('dblclick', () => {
      inputEl.value = currentVal;
      inputEl.style.display = '';
      valueEl.style.display = 'none';
      inputEl.focus(); inputEl.select();
    });
    const finish = () => {
      const p = parseFloat(inputEl.value);
      if (!isNaN(p)) { setVal(p); onChange(id, currentVal); }
      inputEl.style.display = 'none';
      valueEl.style.display = '';
    };
    inputEl.addEventListener('blur', finish);
    inputEl.addEventListener('keydown', e => {
      if (e.key === 'Enter') { finish(); e.preventDefault(); }
      if (e.key === 'Escape') { inputEl.style.display = 'none'; valueEl.style.display = ''; e.preventDefault(); }
    });

    // Scroll — stopPropagation so parent overflow-auto panels don't eat the event
    el.addEventListener('wheel', e => {
      e.preventDefault();
      e.stopPropagation();
      let mult = 1;
      if (e.shiftKey) mult = SHIFT_MULTIPLIER;
      if (e.altKey) mult = ALT_MULTIPLIER;
      const delta = (e.deltaY > 0 ? -1 : 1) * step * mult;
      setVal(currentVal + delta);
      onChange(id, currentVal);
    }, { passive: false });
  });
}

// ============================================================================
// Utilities
// ============================================================================

function clamp(v, lo, hi) { return Math.min(Math.max(v, lo), hi); }

function roundToStep(v, step) {
  if (step >= 1) return Math.round(v / step) * step;
  // For fractional steps, round to avoid floating point noise
  const inv = 1 / step;
  return Math.round(v * inv) / inv;
}

function formatValue(v, precision) {
  return precision > 0 ? v.toFixed(precision) : String(Math.round(v));
}

function guessPrecision(step) {
  if (step >= 1) return 0;
  return Math.max(0, -Math.floor(Math.log10(step)));
}
