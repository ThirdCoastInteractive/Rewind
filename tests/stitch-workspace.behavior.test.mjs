import test from 'node:test';
import assert from 'node:assert/strict';

globalThis.CSS = { escape: (value) => String(value) };
globalThis.window = { EventSource: undefined };
globalThis.document = { querySelectorAll: () => [] }; globalThis.MutationObserver = class { observe() {} disconnect() {} }; globalThis.ResizeObserver = class { observe() {} disconnect() {} };
const workspace = await import('../static/js/stitch-workspace.js');

test('source insertion preserves provenance and canonical video type', () => {
  const op = workspace.buildInsertSegmentOperation({ video_id: 'v1', clip_id: 'c1', export_job_id: 'j1', source_in_us: 2000000, duration_us: 5000000 }, 9000000);
  assert.deepEqual(op, { type: 'video', video_id: 'v1', clip_id: 'c1', export_job_id: 'j1', source_in_us: 2000000, duration_us: 5000000, start_us: 9000000 });
});

test('trim start is relative and end preserves duration', () => {
  assert.deepEqual(workspace.buildTrimOperation({ id: 's1', source_in_us: 2000000 }, 4000000, 12000000), { target_id: 's1', start_us: 2000000, end_us: 10000000 });
});

test('remote revision defers while a draft or request is pending', () => {
  assert.equal(workspace.shouldDeferRemoteRevision(4, 5, 1, null), true);
  assert.equal(workspace.shouldDeferRemoteRevision(4, 5, 0, { type: 'trim_segment' }), true);
  assert.equal(workspace.shouldDeferRemoteRevision(4, 5, 0, null), false);
  assert.equal(workspace.shouldDeferRemoteRevision(5, 4, 1, null), false);
});

test('command queue serializes concurrent gestures and continues after a conflict', async () => {
  const state = { commandQueue: null };
  const events = [];
  const first = workspace.queueSerialized(state, async () => {
    events.push('first-start');
    await new Promise((resolve) => setTimeout(resolve, 5));
    events.push('first-conflict');
    throw new Error('409');
  }).catch(() => {});
  const second = workspace.queueSerialized(state, async () => {
    events.push('second-start');
    events.push('second-replay');
  });
  await Promise.all([first, second]);
  assert.deepEqual(events, ['first-start', 'first-conflict', 'second-start', 'second-replay']);
  assert.equal(state.commandQueue, null);
});

test('unlink and ungroup resolve the existing selected group', () => {
  const document = { timing_links: [{ id: 'timing-1', members: ['a', 'b'] }], position_groups: [{ id: 'position-1', members: ['x', 'y'] }] };
  assert.equal(workspace.findExistingGroup(document, ['a', 'b'], 'timing').id, 'timing-1');
  assert.equal(workspace.findExistingGroup(document, ['x', 'y'], 'position').id, 'position-1');
  assert.equal(workspace.findExistingGroup(document, ['a', 'z'], 'timing'), undefined);
});

test('real command, reloadLatest, and applyDraft preserve live caption drafts across 409', async () => {
  const response = (status, body) => ({
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  });
  const editor = Object.create(workspace.StitchWorkspace.prototype);
  const liveCue = { id: 'caption-1', text: 'local typing', start_us: 0, end_us: 2000000 };
  editor.projectID = 'project-1';
  editor.revision = 3;
  editor.doc = { captions: [liveCue] };
  editor.resolved = [];
  editor.resolvedCaptions = [];
  editor.selected = new Set(['caption-1']);
  editor.pending = 0;
  editor.draft = liveCue;
  editor.conflictDraft = null;
  editor.clearError = () => {};
  editor.hideConflict = () => {};
  editor.showConflict = () => {};
  editor.setState = (state) => { editor.lastState = state; };
  editor.render = () => {};
  editor.error = (message) => { throw new Error(message); };

  const requests = [];
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = async (url, options = {}) => {
      requests.push({ url, options });
      return response(409, { error: 'revision conflict' });
    };
    await editor.command('upsert_caption', {
      target_id: 'caption-1',
      caption: { ...liveCue, text: 'saved after debounce' },
    });
    assert.equal(requests.length, 1);
    assert.equal(editor.draft, liveCue, 'a live caption draft remains separate from a conflict operation');
    assert.equal(editor.conflictDraft.type, 'upsert_caption');

    globalThis.fetch = async (url, options = {}) => {
      requests.push({ url, options });
      assert.match(url, /\/document$/);
      return response(200, {
        revision: 4,
        document: { captions: [{ ...liveCue, text: 'remote edit' }] },
        resolved: [{ segment: { id: 'segment-1' }, end_us: 3000000, track: 0 }],
      });
    };
    await editor.reloadLatest();
    assert.equal(editor.revision, 4);
    assert.equal(editor.doc.captions[0].text, 'remote edit');
    assert.equal(editor.draft, liveCue, 'reload does not replace the live cue draft');
    assert.equal(editor.conflictDraft.type, 'upsert_caption', 'reload keeps the operation available to apply');

    globalThis.fetch = async (url, options = {}) => {
      requests.push({ url, options });
      const body = JSON.parse(options.body);
      assert.equal(body.expected_revision, 4, 'apply uses the reloaded revision');
      assert.equal(body.operations[0].caption.text, 'saved after debounce');
      return response(200, {
        revision: 5,
        document: { captions: [{ ...liveCue, text: 'saved after debounce' }] },
        resolved: [],
      });
    };
    await editor.applyDraft();
    assert.equal(editor.conflictDraft, null, 'apply clears the conflict operation before retrying');
    assert.equal(editor.revision, 5);

    globalThis.fetch = async (url, options = {}) => {
      const body = JSON.parse(options.body);
      assert.equal(body.expected_revision, 5, 'the next gesture sees the committed revision');
      return response(200, { revision: 6, document: editor.doc, resolved: [] });
    };
    await editor.command('move_segment', { target_id: 'segment-1', delta_us: 33333 });
    assert.equal(editor.revision, 6);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('scrollbar gutter clicks are not timeline seeks', () => {
  const el = { clientWidth: 400, clientHeight: 180, getBoundingClientRect: () => ({ left: 10, top: 20, right: 430, bottom: 220 }) };
  assert.equal(workspace.pointerOnScrollbar(el, { clientX: 50, clientY: 40 }), false);
  assert.equal(workspace.pointerOnScrollbar(el, { clientX: 200, clientY: 205 }), true);
  assert.equal(workspace.pointerOnScrollbar(el, { clientX: 420, clientY: 80 }), true);
});

test('playback clips keep clip media, title text, and transition kind', () => {
  const clips = workspace.buildPlaybackClips([
    { id: 't1', type: 'title', start_us: 0, duration_us: 3_000_000, text: 'Hello Test', legacy: { bg_color: '#111', text_color: '#eee' } },
    { id: 'c1', type: 'clip', video_id: 'vid-1', clip_id: 'clip-1', start_us: 3_000_000, source_in_us: 12_000_000, duration_us: 5_000_000, transition: { kind: 'circleclose', duration_us: 500_000 } },
    { id: 'g1', type: 'gap', start_us: 8_000_000, duration_us: 1_000_000 },
  ]);
  assert.equal(clips[0].kind, 'title');
  assert.equal(clips[0].title.text, 'Hello Test');
  assert.equal(clips[0].src, '');
  assert.equal(clips[1].kind, 'video');
  assert.equal(clips[1].src, '/api/videos/vid-1/stream');
  assert.equal(clips[1].startTime, 12);
  assert.equal(clips[1].endTime, 17);
  assert.deepEqual(clips[1].transition, { type: 'circleclose', duration: 0.5 });
  assert.equal(clips[2].kind, 'title');
  assert.equal(clips[2].title.text, '');
});

test('timeline zoom spans overview through frame editing and maps the slider logarithmically', () => {
  assert.equal(workspace.ZOOM_MIN < 10, true);
  assert.equal(workspace.ZOOM_MAX > 240, true);
  assert.equal(workspace.clampZoom(0), workspace.ZOOM_MIN);
  assert.equal(workspace.clampZoom(1e9), workspace.ZOOM_MAX);
  assert.equal(workspace.zoomToSlider(workspace.ZOOM_MIN), 0);
  assert.equal(workspace.zoomToSlider(workspace.ZOOM_MAX), workspace.ZOOM_SLIDER_MAX);
  assert.equal(workspace.zoomToSlider(workspace.sliderToZoom(250)), 250);
  assert.equal(workspace.zoomToSlider(workspace.sliderToZoom(800)), 800);
  assert.ok(workspace.stepZoom(80, 1) > 80);
  assert.ok(workspace.stepZoom(80, -1) < 80);
  assert.equal(workspace.fitZoom(10_000_000, 800), 80);
  assert.equal(workspace.fitZoom(1_000_000, 800), 800);
  assert.equal(workspace.timelineContentWidth(10_000_000, 80), 800);
});

test('ruler ticks get coarser when zoomed out and finer when zoomed in', () => {
  assert.equal(workspace.rulerStepUS(4000, 30), Math.round(1e6 / 30));
  assert.ok(workspace.rulerStepUS(80, 30) >= 1e6);
  assert.ok(workspace.rulerStepUS(0.5, 30) >= 60e6);
  assert.equal(workspace.rulerLabel(0, 30, 1e6), '00:00');
  assert.equal(workspace.rulerLabel(5_000_000, 30, 1e6), '00:05');
});

test('playhead chrome signatures ignore unchanged overlays and captions', () => {
  const overlays = [
    { id: 'a', visible: true, start_us: 0, end_us: 2_000_000 },
    { id: 'b', visible: true, start_us: 3_000_000, end_us: 4_000_000 },
  ];
  assert.equal(workspace.overlaySignature(overlays, 500_000), 'a');
  assert.equal(workspace.overlaySignature(overlays, 3_500_000), 'b');
  assert.equal(workspace.overlaySignature(overlays, 2_500_000), '');
  const captions = [{ id: 'c1', start_us: 0, end_us: 2_000_000, words: [{ start_us: 0, end_us: 400_000 }, { start_us: 400_000, end_us: 2_000_000 }], alignment: 'valid', style: { word_highlight: true } }];
  assert.equal(workspace.captionPreviewSignature(captions, [], 100_000), 'c1:0');
  assert.equal(workspace.captionPreviewSignature(captions, [], 800_000), 'c1:1');
  assert.equal(workspace.captionPreviewSignature(captions, [], 3_000_000), '');
  const timeline = [{ id: 's1', start_us: 0, duration_us: 5_000_000, layout: { mode: 'two_speakers', crops: [{}, {}] } }];
  assert.equal(workspace.layoutPreviewSignature(timeline, 1_000_000), 's1:two_speakers:2');
  assert.equal(workspace.layoutPreviewSignature(timeline, 9_000_000), '');
  const multicam = [{
    id: 's2', start_us: 0, duration_us: 5_000_000, source_in_us: 0, clip_start_us: 0,
    crops: [{ id: 'cam-a', name: 'A', x: 0.5, y: 0.5, width: 0.4, height: 0.4 }, { id: 'cam-b', name: 'B', x: 0.5, y: 0.5, width: 0.5, height: 0.5 }],
    shots: [{ crop_id: 'cam-a', start: 0, end: 2 }, { crop_id: 'cam-b', start: 2, end: 5 }],
    layout: { mode: 'two_speakers', crops: [{}, {}] },
  }];
  assert.equal(workspace.layoutPreviewSignature(multicam, 500_000), 's2:multicam:cam-a');
  assert.equal(workspace.layoutPreviewSignature(multicam, 3_000_000), 's2:multicam:cam-b');
  assert.equal(workspace.titleCardSignature([{ segment: { id: 't1', type: 'title', start_us: 0, duration_us: 1_000_000 } }], 100_000, false), 't1');
  assert.equal(workspace.titleCardSignature([{ segment: { id: 't1', type: 'title', start_us: 0, duration_us: 1_000_000 } }], 100_000, true), 'seq');
});

test('paused teaser redraw composes and clips both speaker panels', () => {
  const calls = [];
  const ctx = {
    clearRect: (...args) => calls.push(['clear', ...args]), save: () => calls.push(['save']), restore: () => calls.push(['restore']),
    beginPath: () => calls.push(['begin']), rect: (...args) => calls.push(['rect', ...args]), clip: () => calls.push(['clip']),
    drawImage: (...args) => calls.push(['draw', ...args]),
  };
  const video = { videoWidth: 320, videoHeight: 180 };
  const layout = { mode: 'two_speakers', crops: [{ x: 0, y: 0, width: 0.5, height: 1 }, { x: 0.5, y: 0, width: 0.5, height: 1 }] };
  workspace.drawTeaserLayoutFrame(ctx, video, layout, 1080, 1920);
  assert.equal(calls.filter(([kind]) => kind === 'draw').length, 2);
  assert.deepEqual(calls.filter(([kind]) => kind === 'rect').map(([, x, y, width, height]) => [x, y, width, height]), [[0, 0, 1080, 960], [0, 960, 1080, 960]]);
  assert.equal(calls.filter(([kind]) => kind === 'clip').length, 2);
});

test('mountStitchWorkspaces skips roots that are already mounted', () => {
  const root = { dataset: { mounted: '1' } };
  workspace.mountStitchWorkspaces({ querySelectorAll: () => [root] });
  assert.equal(root.dataset.mounted, '1');
});

function mockExportRoot() {
  const element = () => {
    const node = {
      className: '',
      textContent: '',
      href: '',
      children: [],
      attrs: {},
      append(...kids) { this.children.push(...kids); },
      setAttribute(name, value) { this.attrs[name] = value; },
      getAttribute(name) { return name === 'href' ? this.href : this.attrs[name]; },
      querySelector(sel) {
        const all = [];
        const walk = (n) => { all.push(n); (n.children || []).forEach(walk); };
        walk(this);
        if (sel.startsWith('.')) return all.find((n) => n.className === sel.slice(1)) || null;
        return all.find((n) => n.tag === sel) || null;
      },
    };
    return node;
  };
  globalThis.document.createElement = (tag) => {
    const node = element();
    node.tag = tag;
    return node;
  };
  const nodes = new Map();
  const details = { open: false, setAttribute(name, value) { if (name === 'open') this.open = value === '' || value === true; } };
  const status = { textContent: '' };
  const list = { children: [], replaceChildren(...children) { this.children = children; } };
  const submit = { disabled: false, type: 'submit' };
  const form = { querySelector: (sel) => sel.includes('submit') ? submit : null, closest: () => details };
  nodes.set('[data-export-form]', form);
  nodes.set('[data-export-status]', status);
  nodes.set('[data-export-list]', list);
  nodes.set('[data-error]', { textContent: '' });
  nodes.set('[data-sync-state]', { textContent: '' });
  return {
    details, status, list, submit, form,
    querySelector: (sel) => nodes.get(sel) || null,
  };
}

test('export posts caption_mode and shows server errors in the export panel', async () => {
  const editor = Object.create(workspace.StitchWorkspace.prototype);
  const root = mockExportRoot();
  editor.root = root;
  editor.projectID = 'project-export';
  editor.revision = 8;
  editor.selected = new Set();
  editor.resolved = [{ segment: { id: 's1', start_us: 0, duration_us: 2_000_000 }, end_us: 2_000_000 }];
  editor.doc = { segments: [{ id: 's1', start_us: 0, duration_us: 2_000_000 }] };
  editor.findSelected = () => null;
  editor.duration = workspace.StitchWorkspace.prototype.duration;
  editor.clearError = workspace.StitchWorkspace.prototype.clearError;
  editor.error = workspace.StitchWorkspace.prototype.error;
  editor.setState = workspace.StitchWorkspace.prototype.setState;
  editor.openExportPanel = workspace.StitchWorkspace.prototype.openExportPanel;
  editor.setExportStatus = workspace.StitchWorkspace.prototype.setExportStatus;
  editor.exportError = workspace.StitchWorkspace.prototype.exportError;
  editor.pollExports = () => { editor.polled = true; };
  editor.loadExports = async () => { editor.loaded = true; };
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = async (url, options = {}) => {
      editor.posted = { url, body: JSON.parse(options.body) };
      return { ok: false, status: 400, json: async () => ({ error: 'caption q has pending word alignment' }) };
    };
    const form = new Map([['format', 'mp4'], ['quality', 'high'], ['caption_mode', 'none'], ['scope', 'all'], ['loudness', '-12']]);
    form.get = Map.prototype.get;
    await workspace.StitchWorkspace.prototype.exportJob.call(editor, form);
    assert.equal(root.details.open, true);
    assert.match(editor.posted.url, /\/exports$/);
    assert.equal(editor.posted.body.caption_mode, 'none');
    assert.equal(editor.posted.body.scope, 'all');
    assert.equal(editor.posted.body.revision, 8);
    assert.equal(editor.posted.body.loudness_target, -12);
    assert.equal(root.status.textContent, 'caption q has pending word alignment');
    assert.equal(root.submit.disabled, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('loadExports lists download links and ignores preview jobs', async () => {
  const editor = Object.create(workspace.StitchWorkspace.prototype);
  const root = mockExportRoot();
  editor.root = root;
  editor.projectID = 'project-export';
  editor.exportError = workspace.StitchWorkspace.prototype.exportError;
  editor.error = workspace.StitchWorkspace.prototype.error;
  editor.setExportStatus = workspace.StitchWorkspace.prototype.setExportStatus;
  editor.pollExports = () => { editor.polled = true; };
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = async () => ({
      ok: true,
      json: async () => ({
        jobs: [
          { id: 'preview-1', kind: 'preview', status: 'ready' },
          { id: 'export-1', kind: 'export', status: 'ready', options: { format: 'mp4' } },
          { id: 'export-2', kind: 'export', status: 'processing', progress_pct: 17 },
        ],
      }),
    });
    await workspace.StitchWorkspace.prototype.loadExports.call(editor);
    assert.equal(root.list.children.length, 2);
    assert.equal(root.list.children[0].querySelector('.stitch-list-title').textContent, 'export · ready');
    assert.equal(root.list.children[0].querySelector('a').getAttribute('href'), '/api/stitch/export-1/download');
    assert.equal(root.list.children[1].querySelector('.stitch-list-title').textContent, 'export · processing 17%');
    assert.equal(editor.polled, true);
    assert.equal(root.status.textContent, 'Export in progress…');
  } finally {
    globalThis.fetch = originalFetch;
  }
});
