import assert from 'node:assert/strict';
import test from 'node:test';
import { formatTimestamp, parseShowNoteReference, parseTimestampCue, parseTimestampSpec, parseYouTubeURL, renderYouTubeEmbed } from './shownote-embeds.js';

test('accepts official YouTube URL forms and query timestamps', () => {
  assert.deepEqual(parseYouTubeURL('https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1m23s'), { videoId: 'dQw4w9WgXcQ', start: 83, url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1m23s' });
  assert.equal(parseYouTubeURL('https://youtu.be/dQw4w9WgXcQ?t=2m').start, 120);
  assert.equal(parseYouTubeURL('https://evil.example/watch?v=dQw4w9WgXcQ'), null);
  assert.equal(parseYouTubeURL('https://user:pass@youtube.com/watch?v=dQw4w9WgXcQ'), null);
  assert.equal(parseYouTubeURL('https://youtube.com:8443/watch?v=dQw4w9WgXcQ'), null);
  assert.equal(parseYouTubeURL('https://youtube.com/watch/extra?v=dQw4w9WgXcQ'), null);
  assert.equal(parseYouTubeURL('javascript:alert(1)'), null);
  assert.equal(parseYouTubeURL('https://www.youtube.com/watch?v=short'), null);
});

test('parses timestamp forms and ranges', () => {
  assert.deepEqual(parseTimestampSpec('1:02'), { start: 62, end: null });
  assert.deepEqual(parseTimestampSpec('1:02 - 2:03'), { start: 62, end: 123 });
  assert.deepEqual(parseTimestampSpec('1:02–2:03'), { start: 62, end: 123 });
  assert.equal(parseTimestampSpec('2:03-1:02'), null);
  assert.equal(parseTimestampSpec('1:02-999999999999:00'), null);
  assert.equal(parseTimestampSpec('999999999999:00'), null);
  assert.equal(formatTimestamp(3661), '1:01:01');
});

test('binds explicit timestamp to the matching YouTube reference', () => {
  const ref = parseShowNoteReference('[talk](https://youtu.be/dQw4w9WgXcQ) @ 1:02–2:03');
  assert.equal(ref.kind, 'youtube');
  assert.equal(ref.videoId, 'dQw4w9WgXcQ');
  assert.equal(ref.start, 62);
  assert.equal(ref.end, 123);
  assert.equal(parseShowNoteReference('[local](rewind://video/123) @ 0:10').kind, 'video');
  assert.deepEqual(parseTimestampCue('  - 1:02–2:03 segment'), { start: 62, end: 123, time: '1:02–2:03', label: 'segment' });
});

test('YouTube widget cues, bounds ranges, copies, handles errors, and disposes', async () => {
  const elements = [];
  class FakeElement {
    constructor(tag) { this.tagName = tag; this.children = []; this.hidden = false; this.disabled = false; this.dataset = {}; this.className = ''; this.textContent = ''; }
    append(...items) { this.children.push(...items); }
    appendChild(item) { this.children.push(item); return item; }
    setAttribute() {}
    addEventListener(type, fn) { this[`on${type}`] = fn; }
  }
  let fakePlayer;
  global.document = { createElement: (tag) => { const element = new FakeElement(tag); elements.push(element); return element; }, head: { appendChild() {} } };
  Object.defineProperty(global, 'navigator', { configurable: true, value: { clipboard: { writeText: async (value) => { global.copied = value; } } } });
  const listeners = {};
  global.window = { location: { origin: 'https://rewind.test' }, setTimeout, clearTimeout, setInterval, clearInterval, addEventListener: (type, fn) => { listeners[type] = fn; }, removeEventListener: () => {}, dispatchEvent: (event) => listeners[event.type]?.(event), YT: { PlayerState: { PLAYING: 1 }, Player: class { constructor(_, opts) { fakePlayer = this; this.current = 0; this.cue = null; this.paused = false; this.destroyed = false; this.opts = opts; this.cueVideoById = (value) => { this.cue = value; }; this.seekTo = (value) => { this.current = value; }; this.getCurrentTime = () => this.current; this.pauseVideo = () => { this.paused = true; }; this.destroy = () => { this.destroyed = true; }; queueMicrotask(() => opts.events.onReady()); } } } };
  const card = renderYouTubeEmbed({ videoId: 'dQw4w9WgXcQ', sourceKey: 'note-a:1', providerURL: 'https://youtu.be/dQw4w9WgXcQ', start: 2, end: 4 });
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(fakePlayer.cue, { videoId: 'dQw4w9WgXcQ', startSeconds: 2, endSeconds: 4 });
  assert.equal(fakePlayer.current, 0);
  fakePlayer.opts.events.onStateChange({ data: 1 }); fakePlayer.current = 5;
  await new Promise((resolve) => setTimeout(resolve, 250));
  assert.equal(fakePlayer.paused, true);
  fakePlayer.paused = false; global.window.dispatchEvent(new CustomEvent('rewind:youtube-seek', { detail: { sourceKey: 'note-a:1', start: 1, end: null } }));
  fakePlayer.current = 5; fakePlayer.opts.events.onStateChange({ data: 1 });
  await new Promise((resolve) => setTimeout(resolve, 250));
  assert.equal(fakePlayer.paused, false);
  card.children[1].children[2].onclick();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(global.copied, '0:05');
  card.__rewindDestroy(); assert.equal(fakePlayer.destroyed, true);
  fakePlayer.opts.events.onError(); assert.equal(card.children[2].hidden, false);
  delete global.document; delete global.navigator; delete global.window;
});
