import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

/**
 * Helper to load and parse the Timeline class from timeline-render.js.
 * Handles multi-line imports and provides minimal fake DOM and utilities.
 */
async function loadTimeline() {
  const source = (await readFile(new URL('./timeline-render.js', import.meta.url), 'utf8'))
    .replace(/^import [\s\S]*?;\r?\n/m, '')
    .replace('export class Timeline', 'class Timeline');

  // Create a fake element factory that tracks listeners
  const mkFakeElement = () => {
    const el = {
      className: '',
      style: {},
      dataset: {},
      children: [],
      listeners: {},
      appendChild(child) {
        this.children.push(child);
      },
      replaceChildren() {
        this.children = [];
      },
      querySelector() {
        return null;
      },
      set innerHTML(val) {
        this.children = [];
      },
      get innerHTML() {
        return '';
      },
      title: '',
      textContent: '',
      addEventListener(type, fn) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(fn);
      },
      getContext() {
        return {
          fillRect() {},
          drawImage() {},
        };
      },
      getBoundingClientRect() {
        return { left: 0, top: 0, width: 1000, height: 80 };
      },
    };
    return el;
  };

  const context = vm.createContext({
    document: {
      createElement(tag) {
        return mkFakeElement();
      },
      querySelectorAll: () => [],
      querySelector: () => null,
    },
    window: {
      devicePixelRatio: 1,
    },
    getComputedStyle: () => ({ backgroundColor: 'rgb(10,10,10)' }),
    clamp: (v, min, max) => Math.max(min, Math.min(max, v)),
    isFiniteNumber: (n) => typeof n === 'number' && isFinite(n),
    formatTime: (t) => {
      const s = Math.floor(t);
      const m = Math.floor(s / 60);
      const sec = s % 60;
      return `${m}:${sec.toString().padStart(2, '0')}`;
    },
    timeFromEvent: (el, evt, start, end) => {
      const rect = el.getBoundingClientRect();
      const x = (evt.clientX - rect.left) / rect.width;
      return start + (end - start) * x;
    },
    computeContrastBorder: (color) => color,
  });

  const Timeline = vm.runInContext(source + '\nTimeline', context);
  return { Timeline, context };
}

/**
 * Walk the fake element tree recursively to find elements matching a predicate.
 */
function findElements(root, predicate) {
  const results = [];
  const walk = (el) => {
    if (!el) return;
    if (predicate(el)) {
      results.push(el);
    }
    if (el.children && Array.isArray(el.children)) {
      el.children.forEach(walk);
    }
  };
  walk(root);
  return results;
}

/**
 * Find range marker elements by their color (independent of the fix).
 * Range markers have a background color and a width set.
 */
function findRangeMarkers(layer) {
  return findElements(layer, (el) => {
    const hasColor = el.style && el.style.background && el.style.background !== '';
    const hasWidth = el.style && el.style.width && el.style.width !== '';
    return hasColor && hasWidth && el.className && el.className.includes('bg-white/20');
  });
}

/**
 * Find point marker elements by their width class.
 * Point markers have the w-[2px] class.
 */
function findPointMarkers(layer) {
  return findElements(layer, (el) => {
    return el.className && el.className.includes('w-[2px]');
  });
}

test('range markers have pointer-events-none and no click handlers; point markers keep theirs', async () => {
  const { Timeline, context } = await loadTimeline();

  // Create fake editor layers that will track appendChild
  const overviewLayer = context.document.createElement('div');
  overviewLayer.clientWidth = 1000;
  overviewLayer.clientHeight = 80;

  const workLayer = context.document.createElement('div');
  workLayer.clientWidth = 1000;
  workLayer.clientHeight = 80;

  // Create a fake editor with minimal required fields
  const editor = {
    overviewLayer: overviewLayer,
    workLayer: workLayer,
    overviewEl: overviewLayer,
    workEl: workLayer,
    duration: 1000,
    overviewStart: 0,
    overviewEnd: 1000,
    workStart: 0,
    workEnd: 500,
    selectedContextWindowID: null,
    selectedClipId: null,
    inPoint: NaN,
    outPoint: NaN,
    workHeadTime: NaN,
    contextWindows: [],
    clips: [],
    markers: [
      { timestamp: 100, duration: 300, color: '#a67c52', title: 'Chapter' },
      { timestamp: 50, duration: null, color: '', title: 'Point' },
    ],
    waveform: null,
    seek: null,
    showFilmstrip: false,
    video: { currentTime: 0 },
    ensureOverviewWindow() {},
    drawWaveformToCanvas() {},
    seekThumbs: { renderRow() {} },
    getLiveClipColor: () => null,
    selectContextWindow() {},
    selectClip() {},
    isOverviewZoomed() { return false; },
  };

  const timeline = new Timeline(editor);
  timeline.renderOverview();

  // Find range and point markers in overview
  const rangeElements = findRangeMarkers(editor.overviewLayer);
  const pointElements = findPointMarkers(editor.overviewLayer);

  // Verify range marker in overview
  assert.equal(rangeElements.length, 1, 'should have one range marker in overview');
  const rangeOverview = rangeElements[0];
  assert.ok(rangeOverview.className.includes('pointer-events-none'), 'range should have pointer-events-none');
  assert.equal(
    (rangeOverview.listeners['click'] || []).length,
    0,
    'range should have no click listener'
  );
  assert.equal(rangeOverview.dataset.markerEl, undefined, 'range should not have markerEl dataset');

  // Verify point marker in overview keeps its handler
  assert.equal(pointElements.length, 1, 'should have one point marker in overview');
  const pointOverview = pointElements[0];
  assert.ok((pointOverview.listeners['click'] || []).length > 0, 'point marker should have click listener');
  assert.equal(pointOverview.dataset.markerEl, '', 'point marker should have markerEl dataset');

  // Test renderWork
  timeline.renderWork();

  // Find range and point markers in work layer
  const workRangeElements = findRangeMarkers(editor.workLayer);
  const workPointElements = findPointMarkers(editor.workLayer);

  // Verify range marker in work layer
  assert.equal(workRangeElements.length, 1, 'should have one range marker in work layer');
  const rangeWork = workRangeElements[0];
  assert.ok(rangeWork.className.includes('pointer-events-none'), 'work range should have pointer-events-none');
  assert.equal(
    (rangeWork.listeners['click'] || []).length,
    0,
    'work range should have no click listener'
  );
  assert.equal(rangeWork.dataset.markerEl, undefined, 'work range should not have markerEl dataset');

  // Verify point marker in work layer keeps its handler
  assert.equal(workPointElements.length, 1, 'should have one point marker in work layer');
  const pointWork = workPointElements[0];
  assert.ok((pointWork.listeners['click'] || []).length > 0, 'work point marker should have click listener');
  assert.equal(pointWork.dataset.markerEl, '', 'work point marker should have markerEl dataset');
});
