import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

async function loadAttachMixin() {
  const source = (await readFile(new URL('./attach.js', import.meta.url), 'utf8'))
    .replace(/^import .*;\r?\n/gm, '')
    .replace('export const AttachMixin =', 'const AttachMixin =');
  const context = vm.createContext({
    EventTarget,
    console,
    clamp: (value, min, max) => Math.max(min, Math.min(max, value)),
    isFiniteNumber: (value) => typeof value === 'number' && Number.isFinite(value),
    timeFromEvent: () => 0,
    setDragCursor() {},
  });
  return vm.runInContext(source + '\nAttachMixin', context);
}

function editorFor(video) {
  return {
    video,
    duration: NaN,
    overviewStart: NaN,
    overviewEnd: NaN,
    workHeadTime: NaN,
    ensureDefaultWorkWindow() {},
    renderCalls: 0,
    transportCalls: 0,
    render() { this.renderCalls++; },
    updateTransportTime() { this.transportCalls++; },
  };
}

test('loadedmetadata initializes transport text along with timeline state', async () => {
  const AttachMixin = await loadAttachMixin();
  const video = new EventTarget();
  video.readyState = 0;
  video.duration = 4;
  video.currentTime = 0;
  const editor = editorFor(video);

  AttachMixin._attachVideoListeners.call(editor);
  video.dispatchEvent(new Event('loadedmetadata'));

  assert.equal(editor.duration, 4);
  assert.equal(editor.renderCalls, 1);
  assert.equal(editor.transportCalls, 1);
});

test('already-loaded video initializes when listeners attach after metadata', async () => {
  const AttachMixin = await loadAttachMixin();
  const video = new EventTarget();
  video.readyState = 1;
  video.duration = 4;
  video.currentTime = 0;
  const editor = editorFor(video);

  AttachMixin._attachVideoListeners.call(editor);

  assert.equal(editor.duration, 4);
  assert.equal(editor.renderCalls, 1);
  assert.equal(editor.transportCalls, 1);
});
