/**
 * filter-dial.js — Circular dial/knob widget for filter parameters.
 *
 * Looks for [data-filter-dial] elements that haven't been initialised yet
 * and wires up mouse-drag (and touch) interaction. The dial maps a 270°
 * arc (from 135° to 405°, i.e. bottom-left → bottom-right clockwise) to
 * the parameter's [min, max] range.
 *
 * Auto-attaches a MutationObserver on #filter-stack-list so dials are
 * initialised every time the SSE patch morphs new elements in.
 */

const ARC_START_DEG = 135;   // degrees clockwise from 12 o'clock
const ARC_SPAN_DEG = 270;

/**
 * Initialise all un-initialised dial widgets inside `root`.
 * @param {Element} root - DOM subtree to scan.
 */
export function initFilterDials(root) {
  if (!root) return;
  const dials = root.querySelectorAll('[data-filter-dial]:not([data-dial-ready])');
  dials.forEach(setupDial);
}

/**
 * Start watching #filter-stack-list for mutations and auto-init dials.
 * Safe to call multiple times; only one observer is created.
 */
export function autoInitFilterDials() {
  const watchRoot = () => {
    const list = document.getElementById('filter-stack-list');
    if (!list) return;
    initFilterDials(list);
    new MutationObserver(() => initFilterDials(list))
      .observe(list, { childList: true, subtree: true });
  };
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', watchRoot);
  } else {
    watchRoot();
  }
  // Also re-check when DataStar patches could add the list later
  new MutationObserver(() => {
    const list = document.getElementById('filter-stack-list');
    if (list) initFilterDials(list);
  }).observe(document.body, { childList: true, subtree: true });
}

function setupDial(el) {
  el.setAttribute('data-dial-ready', '');

  const min  = parseFloat(el.dataset.dialMin);
  const max  = parseFloat(el.dataset.dialMax);
  const step = parseFloat(el.dataset.dialStep);
  const hiddenInput = el.querySelector('[data-dial-input]');
  const indicator   = el.querySelector('.dial-indicator');
  if (!hiddenInput || !indicator) return;

  // Position the indicator dot for the current value
  function positionIndicator(val) {
    const ratio = (val - min) / (max - min);
    const angleDeg = ARC_START_DEG + ratio * ARC_SPAN_DEG;
    const angleRad = (angleDeg - 90) * Math.PI / 180; // SVG 0° = 3 o'clock
    const cx = 24 + 20 * Math.cos(angleRad);
    const cy = 24 + 20 * Math.sin(angleRad);
    indicator.setAttribute('cx', cx.toFixed(1));
    indicator.setAttribute('cy', cy.toFixed(1));
  }

  function getValue() {
    return parseFloat(hiddenInput.value) || 0;
  }

  function setValue(raw) {
    let v = Math.round(raw / step) * step;
    v = Math.max(min, Math.min(max, v));
    // Avoid floating-point noise
    v = parseFloat(v.toFixed(10));
    if (v === getValue()) return;
    hiddenInput.value = v;
    positionIndicator(v);
    // Fire change event so DataStar picks it up
    hiddenInput.dispatchEvent(new Event('change', { bubbles: true }));
  }

  // Initial position
  positionIndicator(getValue());

  // Watch for external value changes (signal sync via data-effect)
  const obs = new MutationObserver(() => positionIndicator(getValue()));
  obs.observe(hiddenInput, { attributes: true, attributeFilter: ['value'] });
  // Also re-sync when data-effect fires (it sets .value, not attribute)
  const origSet = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
  const proxy = new Proxy(hiddenInput, {
    set(target, prop, val) {
      const result = Reflect.set(target, prop, val);
      if (prop === 'value') positionIndicator(parseFloat(val) || 0);
      return result;
    }
  });
  // Intercept programmatic .value = ... by wrapping via effect observer
  // Actually, simpler: just re-position on every animationFrame when dragging
  // isn't active.  For non-drag, we use a periodic check.

  // --- Drag interaction ---
  let dragging = false;
  let startY = 0;
  let startVal = 0;

  function onPointerDown(e) {
    if (e.button && e.button !== 0) return;
    e.preventDefault();
    dragging = true;
    startY = e.clientY;
    startVal = getValue();
    document.addEventListener('pointermove', onPointerMove);
    document.addEventListener('pointerup', onPointerUp);
    el.setPointerCapture?.(e.pointerId);
  }

  function onPointerMove(e) {
    if (!dragging) return;
    const dy = startY - e.clientY; // up = positive
    const range = max - min;
    // Shift for fine control
    const sensitivity = e.shiftKey ? 0.001 : 0.005;
    const delta = dy * range * sensitivity;
    setValue(startVal + delta);
  }

  function onPointerUp(e) {
    if (!dragging) return;
    dragging = false;
    document.removeEventListener('pointermove', onPointerMove);
    document.removeEventListener('pointerup', onPointerUp);
    el.releasePointerCapture?.(e.pointerId);
    // Fire a final save via the hidden input's change
    hiddenInput.dispatchEvent(new Event('change', { bubbles: true }));
  }

  el.addEventListener('pointerdown', onPointerDown);

  // Double-click to reset to default
  el.addEventListener('dblclick', () => {
    const defaultVal = parseFloat(hiddenInput.defaultValue) || 0;
    setValue(defaultVal);
  });

  // Mouse wheel for fine adjustment
  el.addEventListener('wheel', (e) => {
    e.preventDefault();
    const direction = e.deltaY < 0 ? 1 : -1;
    const multiplier = e.shiftKey ? 1 : 5;
    setValue(getValue() + direction * step * multiplier);
  }, { passive: false });
}
