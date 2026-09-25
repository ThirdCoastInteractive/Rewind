import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

async function loadSeekThumbnails() {
  const source = (await readFile(new URL('./seek-thumbnails.js', import.meta.url), 'utf8'))
    .replace(/^import .*;\r?\n/gm, '')
    .replace('export class SeekThumbnails', 'class SeekThumbnails');
  const document = {
    createElement() {
      const element = {
        className: '',
        style: {},
        children: [],
        appendChild(child) { this.children.push(child); },
      };
      Object.defineProperty(element, 'innerHTML', {
        get() { return ''; },
        set() { element.children = []; },
      });
      return element;
    },
    body: { appendChild() {} },
  };
  const context = vm.createContext({
    document,
    window: { innerWidth: 1280 },
    console,
    setTimeout,
    clearTimeout,
    URL,
    isFiniteNumber: (value) => typeof value === 'number' && Number.isFinite(value),
    clamp: (value, min, max) => Math.max(min, Math.min(max, value)),
    formatTimecode() { return ''; },
    formatFrameTimecode() { return ''; },
  });
  return vm.runInContext(source + '\nSeekThumbnails', context);
}

function deferred() {
  let resolve;
  const promise = new Promise((res) => { resolve = res; });
  return { promise, resolve };
}

function manifest() {
  return {
    levels: [{
      name: 'medium', interval_seconds: 1, cols: 1, rows: 1,
      thumb_width: 100, thumb_height: 100,
    }],
  };
}

function cues() {
  return Array.from({ length: 8 }, (_, idx) => ({
    start: idx,
    end: idx + 1,
    sheet: 'seek-000.jpg',
    x: 0,
    y: 0,
    w: 100,
    h: 100,
  }));
}

function container() {
  const row = {
    style: {},
    children: [],
    appendChild(child) { this.children.push(child); },
  };
  Object.defineProperty(row, 'innerHTML', {
    get() { return ''; },
    set() { row.children = []; },
  });
  return row;
}

test('overview and work renders can await the same VTT load independently', async () => {
  const SeekThumbnails = await loadSeekThumbnails();
  const seek = new SeekThumbnails({ videoID: 'video-1' });
  seek.manifest = manifest();
  const pending = deferred();
  seek.ensureVttLoaded = async () => pending.promise;

  const overview = container();
  const work = container();
  const overviewRender = seek.renderRow('overview', overview, 0, 4, 400);
  const workRender = seek.renderRow('work', work, 0, 4, 400);
  pending.resolve(cues());
  await Promise.all([overviewRender, workRender]);

  assert.ok(overview.children.length > 0, 'overview row should survive a concurrent work render');
  assert.ok(work.children.length > 0, 'work row should render its own thumbnails');
});

test('a newer render cancels only an older render of the same row', async () => {
  const SeekThumbnails = await loadSeekThumbnails();
  const seek = new SeekThumbnails({ videoID: 'video-1' });
  seek.manifest = manifest();
  const first = deferred();
  const second = deferred();
  let load = 0;
  seek.ensureVttLoaded = async () => (load++ === 0 ? first.promise : second.promise);

  const row = container();
  const staleRender = seek.renderRow('overview', row, 0, 4, 400);
  const currentRender = seek.renderRow('overview', row, 2, 6, 400);
  second.resolve(cues());
  await currentRender;
  const currentChildren = row.children.length;
  first.resolve(cues());
  await staleRender;

  assert.ok(currentChildren > 0, 'latest render should populate the row');
  assert.equal(row.children.length, currentChildren, 'stale render must not overwrite the latest row');
});
