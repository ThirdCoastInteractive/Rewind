import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

async function loadClass(file, name, extra = {}) {
 const source = (await readFile(new URL(file, import.meta.url), 'utf8'))
   .replace(/^import .*;\r?\n/gm, '').replace('export class ' + name, 'class ' + name);
 const context = vm.createContext({
   window: {}, document: { querySelector: () => null }, structuredClone,
   listen() {}, onPageCleanup() {}, console, ...extra,
 });
 return vm.runInContext(source + '\n' + name, context);
}

test('crop saves capture identity and geometry; selection never replays a save', async () => {
 const saves = [];
 const CropOverlay = await loadClass('./crop-overlay.js', 'CropOverlay', {
   CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
   document: { querySelector: selector => selector === '[data-crop-save-panel]' ? { dispatchEvent: event => saves.push(event) } : null },
 });
 const overlay = new CropOverlay({});
 overlay.setSelectedCropId('camera-a');
 overlay.crop = { x: 0.25, y: 0.5, width: 0.3, height: 0.8 };
 overlay.persistSelectedCrop();
 overlay.setSelectedCropId('camera-b');
 overlay.crop.x = 0.75;
 assert.equal(saves.length, 1);
 assert.equal(saves[0].type, 'crop-save');
 assert.equal(saves[0].detail.cropId, 'camera-a');
 assert.equal(saves[0].detail.crop.x, 0.25);
 overlay.persistSelectedCrop();
 assert.equal(saves.length, 2);
 assert.equal(saves[1].detail.cropId, 'camera-b');
 assert.equal(saves[1].detail.crop.x, 0.75);
 overlay.setSelectedCropId(null);
 overlay.persistSelectedCrop();
 assert.equal(saves.length, 2);
});

test('selected full-frame crop remains visible and resizable', async () => {
 const CropOverlay = await loadClass('./crop-overlay.js', 'CropOverlay');
 const overlay = new CropOverlay({});
 const hidden = new Set(['hidden']);
 const layer = { classList: { toggle(name, on) { on ? hidden.add(name) : hidden.delete(name); } } };
 const rect = { style: {} };
 overlay.bindDOM(layer, { clientWidth: 1600, clientHeight: 900 }, rect, {});
 overlay.selectedCropId = 'full-frame';
 overlay.renderOverlay();
 assert.equal(hidden.has('hidden'), false);
 assert.equal(rect.style.width, '1600px');
 assert.equal(rect.style.height, '900px');
 overlay.editing = false;
 overlay.renderOverlay();
 assert.equal(hidden.has('hidden'), true);
});

test('multicam can revise an earlier shot without dropping later coverage', async () => {
 const Multicam = await loadClass('./multicam.js', 'MulticamEngine');
 const engine = new Multicam({
   selectedClipId: 'clip', video: { currentTime: 110 },
   clips: [{ id: 'clip', start: 100, duration: 30 }],
 });
 engine._render = () => {};
 engine._persist = () => {};
 engine._addShotAtPlayhead('a');
 engine.editor.video.currentTime = 120;
 engine._addShotAtPlayhead('b');
 engine.editor.video.currentTime = 110;
 engine._addShotAtPlayhead('c');
 assert.deepEqual(JSON.parse(JSON.stringify(engine.shots.map(s => [s.crop_id, s.start, s.end]))),
   [['a', 0, 10], ['c', 10, 20], ['b', 20, 30]]);
 const previous = engine._history.pop();
 assert.deepEqual(JSON.parse(JSON.stringify(previous.map(s => [s.crop_id, s.start, s.end]))),
   [['a', 0, 20], ['b', 20, 30]]);
});

test('program output switches crops at shot boundaries and clears outside the clip', async () => {
 const draws = [];
 const dataset = { cropName: 'Camera B', cropX: '0.75', cropY: '0.5', cropW: '0.5', cropH: '1' };
 const Multicam = await loadClass('./multicam.js', 'MulticamEngine', {
   document: { querySelectorAll: () => [{ dataset: { cropId: 'b' }, querySelector: () => ({ dataset }) }] },
 });
 const video = { currentTime: 110, readyState: 2, videoWidth: 1920, videoHeight: 1080 };
 const engine = new Multicam({ video, clips: [{ id: 'clip', start: 100, duration: 20 }] });
 engine.clipId = 'clip';
 engine.shots = [{ crop_id: 'a', start: 0, end: 10 }, { crop_id: 'b', start: 10, end: 20 }];
 engine._program = { clientWidth: 320, width: 640, height: 360,
   getContext: () => ({ fillRect() {}, drawImage: (...args) => draws.push(args) }) };
 engine._programCamera = {};
 engine._programEmpty = { style: {} };
 engine.renderProgram();
 assert.equal(engine._programCamera.textContent, 'Camera B');
 assert.deepEqual(draws[0].slice(1), [960, 0, 960, 1080, 160, 0, 320, 360]);
 assert.equal(engine._programEmpty.hidden, true);
 video.currentTime = 120;
 engine.renderProgram();
 assert.equal(engine._programCamera.textContent, 'No camera');
 assert.equal(engine._programEmpty.hidden, false);
 assert.equal(draws.length, 1);
});
