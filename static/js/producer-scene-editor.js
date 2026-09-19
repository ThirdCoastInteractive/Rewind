import { listen as pageListen, pageTimeout, pageFrame, pageFetch } from './lib/page-scope.js';
// producer-scene-editor.js — the producer's client-authoritative scene editor.
//
// Owns the v3 scene model (scenes -> sources), renders the Scenes / Sources /
// Properties panels, applies every edit to the local SceneCore instantly (no
// round-trip), and debounce-persists the whole collection to the server, which
// broadcasts it to viewers and other hosts. Remote edits arrive via
// onRemoteScene(); the model's `_origin` suppresses the sender's own echo.

const UID = () => 's' + Math.random().toString(36).slice(2, 9);

// b64utf8 decodes base64 as UTF-8 (atob alone yields Latin-1, mangling non-ASCII
// text like em-dashes, accents, and emoji in text sources).
function b64utf8(b64) {
  const bin = atob(b64 || '');
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

const TYPE_META = {
  background: { icon: 'fa-wand-magic-sparkles', label: 'Background' },
  videostage: { icon: 'fa-display', label: 'Video Stage' },
  webcam: { icon: 'fa-video', label: 'Webcam' },
  screen: { icon: 'fa-desktop', label: 'Screen' },
  image: { icon: 'fa-image', label: 'Image' },
  text: { icon: 'fa-font', label: 'Text' },
  video: { icon: 'fa-film', label: 'Video' }, // legacy per-clip source (back-compat)
};

// Fixed aspect ratios for the Video Stage (and the size control derives w/h).
const ASPECTS = { '16:9': 16 / 9, '4:3': 4 / 3, '1:1': 1, '9:16': 9 / 16, '21:9': 21 / 9 };
const ASPECT_KEYS = ['16:9', '4:3', '1:1', '9:16', '21:9'];

// Layout presets arranging the video stage + webcams.
const LAYOUTS = [
  ['two-shot', 'Two-shot'],
  ['stage-full', 'Stage full'],
  ['stage-cams-right', 'Stage + cams →'],
  ['stage-cams-bottom', 'Stage + cams ↓'],
  ['stage-pip', 'Stage + PiP'],
  ['split', 'Split'],
  ['news', 'News (2-box)'],
  ['cams-grid', 'Cams grid'],
  ['solo-cam', 'Solo cam'],
];

const BG_MODES = [
  ['space', 'Space'],
  ['perlin-nebula', 'Nebula'],
  ['starfield', 'Starfield'],
  ['plasma', 'Plasma'],
  ['color', 'Color'],
  ['none', 'None'],
];

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}
function n2(v) {
  return Math.round((Number(v) || 0) * 100) / 100;
}

function defaultScene() {
  return {
    version: 3,
    active: 'main',
    transition: { type: 'fade', ms: 400 },
    stage: { aspect: '16:9' },
    program: null,
    scenes: [
      {
        id: 'main',
        name: 'Main',
        sources: [
          { id: 'bg', type: 'background', name: 'Background', visible: true, mode: 'perlin-nebula', speed: 1, seed: 0, tint: { l: 1, c: 0, h: 0 } },
          { id: 'stage', type: 'videostage', name: 'Video Stage', visible: true, aspect: '16:9', scale: 0.95, x: 0.5, y: 0.5, w: 0.95, h: 0.95 },
        ],
      },
    ],
  };
}

// toV3 converts a stored scene (v3 passthrough, or flat v1/v2) into an editable model.
function toV3(scene) {
  if (scene && scene.version >= 3 && Array.isArray(scene.scenes) && scene.scenes.length) {
    scene.active = scene.active || scene.scenes[0].id;
    scene.transition = scene.transition || { type: 'fade', ms: 400 };
    scene.stage = scene.stage || { aspect: '16:9' };
    scene.scenes.forEach((sc, i) => {
      sc.id = sc.id || UID();
      sc.name = sc.name || 'Scene ' + (i + 1);
      sc.sources = Array.isArray(sc.sources) ? sc.sources : [];
      sc.sources.forEach((s) => {
        s.id = s.id || UID();
      });
    });
    return scene;
  }
  const d = defaultScene();
  const bg = (scene && scene.background) || {};
  d.scenes[0].sources[0] = {
    id: 'bg',
    type: 'background',
    name: 'Background',
    visible: (bg.mode || 'perlin-nebula') !== 'none',
    mode: bg.mode || 'perlin-nebula',
    speed: bg.speed != null ? bg.speed : 1,
    seed: bg.seed || 0,
    tint: bg.tint_oklch || { l: 1, c: 0, h: 0 },
  };
  const content = scene && scene.content;
  if (content && content.src) {
    // Migrate the old single content clip onto the program bus + position the
    // default Video Stage from the old video transform.
    d.program = {
      video_id: content.video_id || '',
      src: content.src,
      title: '',
      t0_epoch_ms: content.t0_epoch_ms || 0,
      t: content.t || 0,
      paused: !!content.paused,
      volume: content.volume != null ? content.volume : 1,
      muted: !!content.muted,
      loop: !!content.loop,
    };
    const vt = (scene && scene.video) || {};
    const stage = d.scenes[0].sources.find((s) => s.type === 'videostage');
    if (stage && vt.x != null) {
      stage.x = vt.x;
      stage.y = vt.y != null ? vt.y : 0.5;
      stage.scale = Math.max(0.2, Math.min(1.2, vt.width != null ? vt.width : vt.w != null ? vt.w : 0.95));
    }
  }
  return d;
}

export class SceneEditor {
  constructor(opts) {
    this.noteId = opts.noteId;
    this.clientId = opts.clientId || UID();
    this.userId = opts.userId || '';
    this.core = opts.core;
    this.sourcesEl = opts.sourcesEl;
    this.propsEl = opts.propsEl;
    this.scenesEl = opts.scenesEl;
    this.ds = opts.ds || null;
    this.sel = null;
    this._persistT = null;
    this.model = defaultScene();
    this._wire();
  }

  load(scene) {
    this.model = toV3(scene);
    const sc = this.active();
    this.sel = sc.sources.length ? sc.sources[sc.sources.length - 1].id : null;
    this.autoAdvance = false;
    this.core.setProgramEndedCallback(() => {
      if (this.autoAdvance) this.next();
    });
    this.apply();
    this.render();
    this._syncProgram();
  }

  // ── model accessors ──
  active() {
    return this.model.scenes.find((s) => s.id === this.model.active) || this.model.scenes[0];
  }
  sources() {
    return this.active().sources;
  }
  get(id) {
    return this.sources().find((s) => s.id === id);
  }

  apply() {
    this.sources().forEach((s) => this._syncStage(s)); // keep stage dims = aspect × size
    this.model._origin = this.clientId;
    this.core.applyScene(this.model);
  }

  persist() {
    clearTimeout(this._persistT);
    this._persistT = pageTimeout(() => {
      this.model._origin = this.clientId;
      pageFetch('/api/show-notes/' + this.noteId + '/scene/set', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.model),
      }).catch(() => {});
    }, 250);
  }

  commit() {
    this.apply();
    this.render();
    this.persist();
  }

  // ── mutations ──
  _defaults(type) {
    switch (type) {
      case 'background':
        return { mode: 'perlin-nebula', speed: 1, seed: 0, tint: { l: 1, c: 0, h: 0 }, x: 0.5, y: 0.5, w: 1, h: 1 };
      case 'videostage':
        return { aspect: '16:9', scale: 0.95, x: 0.5, y: 0.5, w: 0.92, h: 0.86 };
      case 'video':
        return { src: '', video_id: '', t0_epoch_ms: 0, t: 0, paused: false, volume: 1, muted: false, loop: false, x: 0.5, y: 0.5, w: 0.8, h: 0.8 };
      case 'webcam':
        return { cam: 0, hostId: '', hostName: '', x: 0.8, y: 0.78, w: 0.28, h: 0.28 };
      case 'screen':
        return { cam: 0, hostId: '', hostName: '', x: 0.4, y: 0.5, w: 0.7, h: 0.7 };
      case 'image':
        return { url: '', opacity: 1, fit: 'contain', x: 0.15, y: 0.15, w: 0.2, h: 0.2 };
      case 'text':
        return { text: 'Lower third', size: 0.5, color: '#ffffff', bg: 'rgba(8,10,18,0.7)', align: 'left', x: 0.5, y: 0.86, w: 0.7, h: 0.12 };
      default:
        return {};
    }
  }
  addSource(type, extra) {
    const s = Object.assign({ id: UID(), type, name: TYPE_META[type].label, visible: true }, this._defaults(type), extra || {});
    this.sources().push(s);
    this.sel = s.id;
    this.commit();
    this._syncSel();
    return s;
  }
  addVideoSource(videoId, src, title) {
    return this.addSource('video', { video_id: videoId, src, name: title || 'Program', t0_epoch_ms: Date.now(), paused: false });
  }
  remove(id) {
    const a = this.sources();
    const i = a.findIndex((s) => s.id === id);
    if (i < 0) return;
    a.splice(i, 1);
    if (this.sel === id) this.sel = a.length ? a[Math.max(0, i - 1)].id : null;
    this.commit();
    this._syncSel();
  }
  move(id, dir) {
    const a = this.sources();
    const i = a.findIndex((s) => s.id === id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= a.length) return;
    const [s] = a.splice(i, 1);
    a.splice(j, 0, s);
    this.commit();
  }
  toggle(id) {
    const s = this.get(id);
    if (s) {
      s.visible = s.visible === false;
      this.commit();
    }
  }
  select(id) {
    this.sel = id;
    this.render();
    this._syncSel();
  }
  patch(id, p) {
    const s = this.get(id);
    if (s) {
      Object.assign(s, p);
      this.commit();
    }
  }
  // patchPath writes a dotted path (e.g. tint.c) on the selected source. Used by
  // the live Properties inputs, so it applies + persists + refreshes the sources
  // list (for name changes) but does NOT rebuild the Properties panel — that
  // would reset the slider/field being edited mid-drag.
  patchPath(id, path, val) {
    const s = this.get(id);
    if (!s) return;
    const parts = path.split('.');
    let o = s;
    for (let i = 0; i < parts.length - 1; i++) o = o[parts[i]] || (o[parts[i]] = {});
    o[parts[parts.length - 1]] = val;
    this.apply();
    this.renderSources();
    this.persist();
  }

  // _merge patches DataStar signals, resolving __dsAPI lazily (it may not exist
  // when the editor is constructed).
  _merge(o) {
    const ds = window.__dsAPI;
    if (ds && ds.mergePatch) ds.mergePatch(o);
  }

  // setTransformLive applies a transform patch during a stage drag: instant
  // local render + debounced persist, without rebuilding the Properties panel.
  setTransformLive(id, patch) {
    const s = this.get(id);
    if (!s) return;
    Object.assign(s, patch);
    if (s.type === 'videostage' && (patch.w != null || patch.h != null)) {
      // a constrained corner-resize set w/h; back-compute the size (scale) and
      // re-derive clean dims so the Size slider + aspect stay in sync.
      const sa = this._stageAspect();
      const ar = ASPECTS[s.aspect] || 16 / 9;
      const hf = ar >= sa ? sa / ar : 1;
      s.scale = Math.max(0.1, Math.min(1.2, (s.h || hf) / hf));
      this._syncStage(s);
    }
    this.apply();
    this.renderSources();
    this.persist();
  }

  // ── Video Stage geometry: a fixed aspect ratio + a size, not freeform w/h ──
  _stageAspect() {
    const a = (this.model.stage && this.model.stage.aspect) || '16:9';
    const m = String(a).match(/(\d+(?:\.\d+)?)\s*[:/]\s*(\d+(?:\.\d+)?)/);
    return m ? Number(m[1]) / Number(m[2]) : 16 / 9;
  }
  // _stageDims returns the normalized w/h for a stage source: the largest box of
  // its aspect that fits the scene, times its size (scale).
  _stageDims(s) {
    const ar = ASPECTS[s.aspect] || 16 / 9;
    const sa = this._stageAspect();
    const scale = Math.max(0.1, Math.min(1.2, s.scale != null ? s.scale : 0.95));
    let wf, hf;
    if (ar >= sa) {
      wf = 1;
      hf = sa / ar;
    } else {
      hf = 1;
      wf = ar / sa;
    }
    return { w: wf * scale, h: hf * scale };
  }
  _syncStage(s) {
    if (!s || s.type !== 'videostage') return;
    if (!s.aspect) s.aspect = '16:9';
    if (s.scale == null) s.scale = 0.95;
    const d = this._stageDims(s);
    s.w = d.w;
    s.h = d.h;
  }
  // lockedAspectN is the normalized w/h aspect a source must keep on resize, or null.
  lockedAspectN(id) {
    const s = this.get(id);
    if (!s || s.type !== 'videostage') return null;
    return (ASPECTS[s.aspect] || 16 / 9) / this._stageAspect();
  }
  setStageAspect(a) {
    const s = this.get(this.sel);
    if (!s || s.type !== 'videostage') return;
    s.aspect = a;
    this._syncStage(s);
    this.commit();
  }
  setStageScale(v) {
    const s = this.get(this.sel);
    if (!s || s.type !== 'videostage') return;
    s.scale = v;
    this._syncStage(s);
    this.apply();
    this.persist(); // no props rebuild — keep the size slider stable while dragging
  }

  // hitTest returns the topmost (front-most) non-background source whose rect
  // contains the normalized stage point, or null.
  hitTest(nx, ny) {
    const a = this.sources();
    for (let i = a.length - 1; i >= 0; i--) {
      const s = a[i];
      if (s.type === 'background' || s.visible === false) continue;
      const hw = (s.w || 0) / 2;
      const hh = (s.h || 0) / 2;
      if (Math.abs(nx - (s.x != null ? s.x : 0.5)) <= hw && Math.abs(ny - (s.y != null ? s.y : 0.5)) <= hh) return s;
    }
    return null;
  }

  _syncSel() {
    // Selection drives the Properties panel only; the transport bar follows the
    // program bus (the cued playlist item), independent of which layer is selected.
  }

  // ── Playlist: the show-note rundown drives the program bus ──
  _playlistItems() {
    const list = document.getElementById('live-content-sources');
    if (!list) return [];
    return [...list.querySelectorAll('[data-content-src]')].map((el) => ({
      id: el.dataset.videoId || (el.dataset.contentSrc || '').split('/').pop(),
	  occurrence: el.dataset.occurrenceKey || '',
	  start: Number(el.dataset.startSeconds || 0),
	  end: Number(el.dataset.endSeconds || 0),
      src: el.dataset.contentSrc,
      title: (el.textContent || '').trim().replace(/^\d+\s+/, ''), // drop the row-number prefix
    }));
  }
  cue(videoId, src, title, occurrence, start, end) {
    const prev = this.model.program || {};
	start = Number(start || 0);
	end = Number(end || 0);
    this.model.program = {
      video_id: videoId,
	  occurrence_key: occurrence || videoId,
      src,
      title: title || '',
	  t0_epoch_ms: Date.now(),
	  t: start,
	  start_t: start,
	  end_t: end,
      paused: false,
      volume: prev.volume != null ? prev.volume : 1,
      muted: false,
      loop: !!prev.loop,
    };
    this.apply(); // swap the program bus + re-texture video stages
    this._syncProgram();
    this.persist();
  }
  cueIndex(i) {
    const items = this._playlistItems();
    if (i >= 0 && i < items.length) this.cue(items[i].id, items[i].src, items[i].title, items[i].occurrence, items[i].start, items[i].end);
  }
  _curIndex() {
	const program = this.model.program || {};
	return this._playlistItems().findIndex((x) => (program.occurrence_key && x.occurrence === program.occurrence_key) || (!program.occurrence_key && x.id === program.video_id));
  }
  next() {
    const items = this._playlistItems();
    if (!items.length) return;
    const i = this._curIndex();
    this.cueIndex(i < 0 ? 0 : Math.min(i + 1, items.length - 1));
  }
  prev() {
    this.cueIndex(Math.max(0, this._curIndex() - 1));
  }
  setAutoAdvance(on) {
    this.autoAdvance = !!on;
  }

  enforceBounds(currentTime) {
	const p = this.model.program;
	if (!p || !p.end_t || p.paused || currentTime < p.end_t) return;
	if (this.autoAdvance) {
	  this.next();
	  return;
	}
	p.t = p.end_t;
	p.paused = true;
	p.t0_epoch_ms = Date.now();
	this.apply();
	this._syncProgram();
	this.persist();
  }

  // ── Program transport (the transport bar drives the program bus) ──
  _syncProgram() {
    const p = this.model.program;
    this._merge({
      programOn: !!(p && p.src),
      programVideoId: p ? p.video_id : '',
      videoPaused: !!(p && p.paused),
      videoVolume: p && p.volume != null ? p.volume : 1,
      videoMuted: !!(p && p.muted),
      videoLoop: !!(p && p.loop),
    });
  }
  videoToggle() {
    const p = this.model.program;
    if (!p || !p.src) return;
    const info = this.core.contentInfo();
    p.t = info ? info.t : p.t || 0;
    p.t0_epoch_ms = Date.now();
    p.paused = !p.paused;
    this.core.setTransport({ paused: p.paused });
    this._merge({ videoPaused: p.paused });
    this.persist();
  }
  videoSeek(t) {
    const p = this.model.program;
    if (!p || !p.src) return;
    p.t = t;
    p.t0_epoch_ms = Date.now();
    this.core.setTransport({ seekTo: t });
    this.persist();
  }
  videoVolume(v) {
    const p = this.model.program;
    if (!p) return;
    p.volume = v;
    this.core.setTransport({ volume: v });
    this.persist();
  }
  videoMute() {
    const p = this.model.program;
    if (!p) return;
    p.muted = !p.muted;
    this.core.setTransport({ muted: p.muted });
    this._merge({ videoMuted: p.muted });
    this.persist();
  }
  videoLoop() {
    const p = this.model.program;
    if (!p) return;
    p.loop = !p.loop;
    this.core.setTransport({ loop: p.loop });
    this._merge({ videoLoop: p.loop });
    this.persist();
  }

  // ── Layout presets: arrange the video stage + webcams ──
  arrange(name) {
    const stage = this.sources().find((s) => s.type === 'videostage' || s.type === 'video');
    const cams = this.sources().filter((s) => s.type === 'webcam');
    // The stage keeps its fixed aspect — presets set its size (scale) + position.
    const setS = (x, y, scale) => {
      if (!stage) return;
      stage.visible = true;
      stage.x = x;
      stage.y = y;
      if (stage.type === 'videostage') {
        stage.scale = scale;
        this._syncStage(stage);
      } else {
        stage.w = scale;
        stage.h = scale;
      }
    };
    const hideS = () => {
      if (stage) stage.visible = false;
    };
    const hideCams = () => cams.forEach((c) => (c.visible = false));
    switch (name) {
      case 'two-shot':
        // Cam-only: hide the stage and place every webcam side-by-side as clean
        // 16:9 boxes filling the width, centered vertically (background shows
        // above/below). Two cams → the classic side-by-side podcast two-shot.
        hideS();
        this._twoShot(cams);
        break;
      case 'stage-full':
        setS(0.5, 0.5, 1);
        hideCams();
        break;
      case 'stage-cams-right':
        setS(0.32, 0.5, 0.64);
        this._stack(cams, 0.83, 0.3);
        break;
      case 'stage-cams-bottom':
        setS(0.5, 0.37, 0.72);
        this._strip(cams, 0.85, 0.24);
        break;
      case 'stage-pip':
        setS(0.5, 0.5, 1);
        this._pip(cams);
        break;
      case 'split':
        setS(0.27, 0.5, 0.5);
        cams.forEach((c, i) => {
          c.visible = i === 0;
          if (i === 0) Object.assign(c, { x: 0.75, y: 0.5, w: 0.46, h: 0.72 });
        });
        break;
      case 'news':
        setS(0.32, 0.46, 0.58);
        cams.forEach((c, i) => {
          c.visible = i === 0;
          if (i === 0) Object.assign(c, { x: 0.76, y: 0.46, w: 0.44, h: 0.8 });
        });
        break;
      case 'cams-grid':
        hideS();
        this._grid(cams, 0.5, 0.5, 0.98, 0.98);
        break;
      case 'solo-cam':
        hideS();
        cams.forEach((c, i) => {
          c.visible = i === 0;
          if (i === 0) Object.assign(c, { x: 0.5, y: 0.5, w: 1, h: 1 });
        });
        break;
    }
    this.commit();
  }
  _stack(cams, x, w) {
    const n = cams.length;
    if (!n) return;
    const gap = 0.012;
    const h = Math.min(w, (0.94 - (n - 1) * gap) / n);
    const y0 = 0.5 - (n * h + (n - 1) * gap) / 2 + h / 2;
    cams.forEach((c, i) => Object.assign(c, { visible: true, x, w, h, y: y0 + i * (h + gap) }));
  }
  _strip(cams, y, h) {
    const n = cams.length;
    if (!n) return;
    const gap = 0.012;
    const w = Math.min(h, (0.96 - (n - 1) * gap) / n);
    const x0 = 0.5 - (n * w + (n - 1) * gap) / 2 + w / 2;
    cams.forEach((c, i) => Object.assign(c, { visible: true, y, w, h, x: x0 + i * (w + gap) }));
  }
  _pip(cams) {
    const w = 0.2;
    const gap = 0.015;
    cams.forEach((c, i) => Object.assign(c, { visible: true, w, h: w, x: 0.99 - w / 2 - i * (w + gap), y: 0.99 - w / 2 }));
  }
  _grid(cams, cx, cy, bw, bh) {
    const n = cams.length;
    if (!n) return;
    const cols = Math.ceil(Math.sqrt(n));
    const rows = Math.ceil(n / cols);
    const cw = bw / cols;
    const ch = bh / rows;
    cams.forEach((c, i) => {
      const r = Math.floor(i / cols);
      const col = i % cols;
      Object.assign(c, { visible: true, w: cw * 0.98, h: ch * 0.98, x: cx - bw / 2 + cw * (col + 0.5), y: cy - bh / 2 + ch * (r + 0.5) });
    });
  }
  // _twoShot lays cams out in one row of equal boxes filling the width. On a 16:9
  // stage a box with w === h is itself 16:9, so a 16:9 cam fills it with no bars.
  _twoShot(cams) {
    const n = cams.length;
    if (!n) return;
    const s = Math.min(1 / n, 1);
    cams.forEach((c, i) => Object.assign(c, { visible: true, x: (i + 0.5) / n, y: 0.5, w: s, h: s }));
  }
  // camsForHosts ensures one webcam source per connected host (bound by hostId),
  // then arranges a two-shot. Falls back to two positional slots when no host
  // identities are known yet (e.g. co-host hasn't joined the SFU).
  camsForHosts() {
    const meta = (window.__rewindCams && window.__rewindCams.meta) || new Map();
    const hosts = [];
    const seen = new Set();
    for (const [, m] of meta) {
      if (m && m.userId && !seen.has(m.userId)) {
        seen.add(m.userId);
        hosts.push(m);
      }
    }
    const srcs = this.sources();
    if (hosts.length) {
      hosts.forEach((h) => {
        if (srcs.some((s) => s.type === 'webcam' && s.hostId === h.userId)) return;
        srcs.push(Object.assign({ id: UID(), type: 'webcam', name: h.username || 'Host', visible: true }, this._defaults('webcam'), { hostId: h.userId, hostName: h.username || '' }));
      });
    } else {
      const existing = srcs.filter((s) => s.type === 'webcam');
      for (let i = existing.length; i < 2; i++) {
        srcs.push(Object.assign({ id: UID(), type: 'webcam', name: 'Cam ' + (i + 1), visible: true }, this._defaults('webcam'), { cam: i }));
      }
    }
    this.arrange('two-shot'); // arranges the row + commits
  }
  // bindHost binds the selected webcam to a specific host (stable identity), or
  // clears the binding (empty id → positional slot). Names the source after the host.
  bindHost(userId) {
    const s = this.get(this.sel);
    if (!s || (s.type !== 'webcam' && s.type !== 'screen')) return;
    s.hostId = userId || '';
    if (userId) {
      const meta = (window.__rewindCams && window.__rewindCams.meta) || new Map();
      for (const [, m] of meta) {
        if (m && m.userId === userId) {
          s.hostName = m.username || '';
          const generic = ['Webcam', 'Host', 'Screen'];
          if (!s.name || generic.includes(s.name)) s.name = (m.username || s.name) + (s.type === 'screen' ? ' — screen' : '');
          break;
        }
      }
    }
    this.apply();
    this.renderSources();
    this.persist();
  }

  // fillFrame turns the selected image into a full-frame backdrop: cover-fit,
  // full stage, placed just above any background source (so it reads as the
  // backdrop but a shader background, if present, stays behind it).
  fillFrame(id) {
    const s = this.get(id);
    if (!s) return;
    Object.assign(s, { x: 0.5, y: 0.5, w: 1, h: 1, fit: 'cover' });
    const a = this.sources();
    const i = a.findIndex((x) => x.id === id);
    if (i < 0) return;
    a.splice(i, 1);
    let insert = 0;
    for (let k = 0; k < a.length; k++) if (a[k].type === 'background') insert = k + 1;
    a.splice(insert, 0, s);
    this.commit();
  }

  // addScreenForSelf adds a Screen source bound to this producer, so their shared
  // screen lands on the stage the moment they hit Share screen. Called by
  // webrtc-room.js once getDisplayMedia succeeds.
  addScreenForSelf() {
    let hostId = this.userId || '';
    if (!hostId) {
      const meta = (window.__rewindCams && window.__rewindCams.meta) || new Map();
      for (const [, m] of meta)
        if (m && m.self) {
          hostId = m.userId;
          break;
        }
    }
    if (this.sources().some((s) => s.type === 'screen' && s.hostId === hostId)) return;
    this.addSource('screen', { hostId, hostName: '', name: 'My screen' });
  }

  // ── scenes ──
  addScene() {
    const sc = { id: UID(), name: 'Scene ' + (this.model.scenes.length + 1), sources: [{ id: UID(), type: 'background', name: 'Background', visible: true, mode: 'perlin-nebula', speed: 1, seed: 0, tint: { l: 1, c: 0, h: 0 } }] };
    this.model.scenes.push(sc);
    this.model.active = sc.id;
    this.sel = sc.sources[0].id;
    this.commit();
    this._syncSel();
  }
  switchScene(id) {
    if (!this.model.scenes.find((s) => s.id === id)) return;
    this.model.active = id;
    const a = this.sources();
    this.sel = a.length ? a[a.length - 1].id : null;
    this.commit();
    this._syncSel();
  }
  renameScene(id, name) {
    const sc = this.model.scenes.find((s) => s.id === id);
    if (sc) {
      sc.name = name;
      this.commit();
    }
  }
  removeScene(id) {
    if (this.model.scenes.length <= 1) return;
    this.model.scenes = this.model.scenes.filter((s) => s.id !== id);
    if (this.model.active === id) this.model.active = this.model.scenes[0].id;
    this.sel = this.sources()[0] ? this.sources()[0].id : null;
    this.commit();
    this._syncSel();
  }

  // ── remote sync ──
  onRemoteScene(json) {
    if (!json || json._origin === this.clientId) return; // ignore our own echo
    this.model = toV3(json);
    if (!this.get(this.sel)) this.sel = this.sources()[0] ? this.sources()[0].id : null;
    this.sources().forEach((s) => this._syncStage(s)); // keep stage dims = aspect × size
    this.core.applyScene(this.model);
    this.render();
    this._syncProgram();
  }

  // ── rendering (innerHTML + event delegation) ──
  _wire() {
    if (this.sourcesEl)
      this.sourcesEl.addEventListener('click', (e) => {
        const b = e.target.closest('[data-act]');
        if (!b) return;
        const id = b.dataset.id;
        const act = b.dataset.act;
        if (act === 'select') this.select(id);
        else if (act === 'toggle') this.toggle(id);
        else if (act === 'up') this.move(id, -1);
        else if (act === 'down') this.move(id, 1);
        else if (act === 'del') this.remove(id);
        else if (act === 'add') this.addSource(b.dataset.type);
        else if (act === 'camsforhosts') this.camsForHosts();
      });
    if (this.propsEl) {
      const onEdit = (e) => {
        const t = e.target.closest('[data-prop],[data-pact],[data-stagescale],[data-hostbind]');
        if (!t) return;
        if (t.dataset.hostbind != null) {
          this.bindHost(t.value);
          return;
        }
        if (t.dataset.stagescale != null) {
          this.setStageScale(Number(t.value));
          return;
        }
        if (t.dataset.pact) {
          this[t.dataset.pact] && this[t.dataset.pact](t.dataset.arg);
          return;
        }
        let v = t.type === 'checkbox' ? t.checked : t.value;
        if (t.dataset.num) v = Number(v);
        this.patchPath(this.sel, t.dataset.prop, v);
      };
      this.propsEl.addEventListener('input', onEdit);
      this.propsEl.addEventListener('change', (e) => {
        const f = e.target.closest('input[type="file"][data-imgfile]');
        if (!f || !f.files || !f.files[0]) return;
        const reader = new FileReader();
        reader.onload = () => this.patch(this.sel, { url: String(reader.result) });
        reader.readAsDataURL(f.files[0]); // self-hosted data URL — no external hotlink
      });
      this.propsEl.addEventListener('click', (e) => {
        const b = e.target.closest('[data-setmode],[data-arrange],[data-stageaspect],[data-imgfit],[data-imgfill]');
        if (!b) return;
        if (b.dataset.setmode) this.patch(this.sel, { mode: b.dataset.setmode });
        else if (b.dataset.arrange) this.arrange(b.dataset.arrange);
        else if (b.dataset.stageaspect) this.setStageAspect(b.dataset.stageaspect);
        else if (b.dataset.imgfit) this.patch(this.sel, { fit: b.dataset.imgfit });
        else if (b.dataset.imgfill != null) this.fillFrame(this.sel);
      });
    }
    if (this.scenesEl) {
      this.scenesEl.addEventListener('click', (e) => {
        const b = e.target.closest('[data-sact]');
        if (!b) return;
        const a = b.dataset.sact;
        if (a === 'switch') this.switchScene(b.dataset.id);
        else if (a === 'add') this.addScene();
        else if (a === 'del') this.removeScene(b.dataset.id);
        else if (a === 'trans') this.setTransitionType(b.dataset.type);
      });
      this.scenesEl.addEventListener('input', (e) => {
        const t = e.target.closest('[data-sact="transms"]');
        if (t) this.setTransitionMs(Number(t.value));
      });
    }
  }

  setTransitionType(type) {
    this.model.transition = Object.assign({ type: 'fade', ms: 400 }, this.model.transition, { type });
    this.renderScenes();
    this.persist();
  }
  setTransitionMs(ms) {
    this.model.transition = Object.assign({ type: 'fade', ms: 400 }, this.model.transition, { ms });
    this.persist(); // no re-render: keep the slider stable while dragging
  }

  render() {
    this.renderSources();
    this.renderProps();
    this.renderScenes();
  }

  renderSources() {
    if (!this.sourcesEl) return;
    const a = this.sources();
    const rows = a
      .map((s, i) => {
        const m = TYPE_META[s.type] || { icon: 'fa-cube' };
        const selCls = s.id === this.sel ? 'bg-white/10 border-white/40' : 'border-transparent hover:bg-white/5';
        const dim = s.visible === false ? 'opacity-40' : '';
        return `<div class="flex items-center gap-1 px-1 py-0.5 border-l-2 ${selCls} ${dim}" data-id="${s.id}">
          <button data-act="toggle" data-id="${s.id}" class="w-5 shrink-0 text-white/60 hover:text-white" title="Show/hide"><i class="fa-sharp fa-solid ${s.visible === false ? 'fa-eye-slash' : 'fa-eye'}"></i></button>
          <button data-act="select" data-id="${s.id}" class="flex-1 min-w-0 flex items-center gap-1.5 text-left text-xs truncate">
            <i class="fa-sharp fa-solid ${m.icon} text-white/40 shrink-0"></i><span class="truncate">${esc(s.name || m.label)}</span>
          </button>
          <button data-act="up" data-id="${s.id}" class="w-4 shrink-0 text-white/30 hover:text-white ${i === 0 ? 'invisible' : ''}" title="Forward"><i class="fa-sharp fa-solid fa-chevron-up text-[10px]"></i></button>
          <button data-act="down" data-id="${s.id}" class="w-4 shrink-0 text-white/30 hover:text-white ${i === a.length - 1 ? 'invisible' : ''}" title="Back"><i class="fa-sharp fa-solid fa-chevron-down text-[10px]"></i></button>
          <button data-act="del" data-id="${s.id}" class="w-4 shrink-0 text-white/30 hover:text-red-400" title="Delete"><i class="fa-sharp fa-solid fa-xmark text-[10px]"></i></button>
        </div>`;
      })
      .reverse()
      .join('');
    const add = ['videostage', 'webcam', 'screen', 'image', 'text', 'background']
      .map((t) => `<button data-act="add" data-type="${t}" class="btn-ghost btn-sm flex-1 text-[10px] px-1" title="Add ${TYPE_META[t].label}"><i class="fa-sharp fa-solid ${TYPE_META[t].icon}"></i></button>`)
      .join('');
    this.sourcesEl.innerHTML =
      `<div class="space-y-px">${rows || '<div class="text-xs text-white/30 p-1">No sources.</div>'}</div>` +
      `<div class="flex gap-1 mt-1 pt-1 border-t border-white/10">${add}</div>` +
      `<button data-act="camsforhosts" class="btn-ghost btn-sm w-full mt-1 text-[10px]" title="Add one webcam per connected host and arrange a two-shot"><i class="fa-sharp fa-solid fa-users mr-1"></i>One cam per host</button>`;
  }

  renderProps() {
    if (!this.propsEl) return;
    const s = this.get(this.sel);
    if (!s) {
      this.propsEl.innerHTML = '<div class="text-xs text-white/30 p-1">Select a source.</div>';
      return;
    }
    const m = TYPE_META[s.type] || {};
    let body = `<div class="flex items-center gap-1.5 text-xs text-white/70 mb-2"><i class="fa-sharp fa-solid ${m.icon}"></i>
      <input data-prop="name" class="form-input py-0.5 text-xs flex-1 min-w-0" value="${esc(s.name || '')}"/></div>`;

    const tf = (label, prop, min, max, step) =>
      `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">${label} <span class="text-white/60">${n2(s[prop])}</span></label>
       <input type="range" data-prop="${prop}" data-num="1" min="${min}" max="${max}" step="${step}" value="${s[prop] != null ? s[prop] : 0.5}" class="w-full"/>`;
    if (s.type === 'videostage') {
      // Fixed aspect ratio + a single size control (no freeform w/h).
      body += `<div class="grid grid-cols-2 gap-x-2"><div>${tf('X', 'x', 0, 1, 0.01)}</div><div>${tf('Y', 'y', 0, 1, 0.01)}</div></div>`;
      body +=
        `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Aspect ratio</label><div class="grid grid-cols-5 gap-1">` +
        ASPECT_KEYS.map(
          (a) =>
            `<button data-stageaspect="${a}" class="px-1 py-1 text-[10px] font-mono border ${(s.aspect || '16:9') === a ? 'border-white/60 bg-white/10' : 'border-white/20 hover:border-white/40'}">${a}</button>`,
        ).join('') +
        `</div>`;
      body += `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Size <span class="text-white/60">${Math.round((s.scale != null ? s.scale : 0.95) * 100)}%</span></label>
        <input type="range" data-stagescale data-num="1" min="0.2" max="1.2" step="0.02" value="${s.scale != null ? s.scale : 0.95}" class="w-full"/>`;
    } else if (s.type !== 'background') {
      body +=
        `<div class="grid grid-cols-2 gap-x-2">` +
        `<div>${tf('X', 'x', 0, 1, 0.01)}</div><div>${tf('Y', 'y', 0, 1, 0.01)}</div>` +
        `<div>${tf('W', 'w', 0.02, 1.5, 0.01)}</div><div>${tf('H', 'h', 0.02, 1.5, 0.01)}</div></div>`;
    }

    if (s.type === 'background') {
      body +=
        `<div class="grid grid-cols-2 gap-1 my-1">` +
        BG_MODES.map(([v, l]) => `<button data-setmode="${v}" class="px-2 py-1 text-xs font-mono border-2 ${s.mode === v ? 'border-white/60 bg-white/10' : 'border-white/20 hover:border-white/40'}">${l}</button>`).join('') +
        `</div>` +
        `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Speed</label><input type="range" data-prop="speed" data-num="1" min="0" max="5" step="0.1" value="${s.speed != null ? s.speed : 1}" class="w-full"/>` +
        `<label class="block text-[10px] text-white/40 uppercase tracking-wider">Tint lightness</label><input type="range" data-prop="tint.l" data-num="1" min="0" max="1" step="0.01" value="${s.tint && s.tint.l != null ? s.tint.l : 1}" class="w-full"/>` +
        `<label class="block text-[10px] text-white/40 uppercase tracking-wider">Tint chroma</label><input type="range" data-prop="tint.c" data-num="1" min="0" max="0.4" step="0.01" value="${(s.tint && s.tint.c) || 0}" class="w-full"/>` +
        `<label class="block text-[10px] text-white/40 uppercase tracking-wider">Tint hue</label><input type="range" data-prop="tint.h" data-num="1" min="0" max="360" step="1" value="${(s.tint && s.tint.h) || 0}" class="w-full"/>`;
    } else if (s.type === 'webcam' || s.type === 'screen') {
      const isScreen = s.type === 'screen';
      const meta = (window.__rewindCams && window.__rewindCams.meta) || new Map();
      const seen = new Set();
      let opts = `<option value=""${s.hostId ? '' : ' selected'}>— Auto (slot ${s.cam || 0}) —</option>`;
      for (const [, m] of meta) {
        if (!m || !m.userId || seen.has(m.userId)) continue;
        // For screen sources, only offer hosts that are actually sharing a screen.
        if (isScreen && m.kind !== 'screen') continue;
        if (!isScreen && m.kind === 'screen') continue;
        seen.add(m.userId);
        const nm = esc(m.username || 'Host') + (m.self ? ' (you)' : '');
        opts += `<option value="${esc(m.userId)}"${s.hostId === m.userId ? ' selected' : ''}>${nm}</option>`;
      }
      if (s.hostId && !seen.has(s.hostId)) {
        opts += `<option value="${esc(s.hostId)}" selected>${esc(s.hostName || 'Host (offline)')}</option>`;
      }
      const label = isScreen ? "Bind to host's screen" : 'Bind to host';
      const hint = isScreen
        ? 'Shows a host’s shared screen. Hosts appear here once they hit <b class="text-white/60">Share screen</b>.'
        : 'Bind a box to a specific host so it stays put across everyone’s view. Leave on <b class="text-white/60">Auto</b> to fill by join order.';
      body += `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">${label}</label>
        <select data-hostbind class="form-input py-0.5 text-xs w-full">${opts}</select>
        <div class="text-[10px] text-white/40 mt-1 leading-snug">${hint}</div>`;
    } else if (s.type === 'image') {
      const isData = (s.url || '').startsWith('data:');
      const fitCover = s.fit === 'cover';
      body += `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Image ${isData ? '<span class="text-green-400">✓ uploaded</span>' : ''}</label>
        <input type="file" accept="image/*" data-imgfile class="block w-full text-[10px] text-white/60 file:mr-2 file:py-0.5 file:px-2 file:border file:border-white/20 file:bg-white/5 file:text-white"/>
        <input data-prop="url" class="form-input py-0.5 text-xs w-full mt-1" placeholder="…or paste a local URL" value="${isData ? '' : esc(s.url || '')}"/>
        <label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Fit</label>
        <div class="grid grid-cols-2 gap-1">
          <button data-imgfit="contain" class="px-1 py-1 text-[10px] font-mono border ${fitCover ? 'border-white/20 hover:border-white/40' : 'border-white/60 bg-white/10'}">Contain</button>
          <button data-imgfit="cover" class="px-1 py-1 text-[10px] font-mono border ${fitCover ? 'border-white/60 bg-white/10' : 'border-white/20 hover:border-white/40'}">Cover</button>
        </div>
        <button data-imgfill class="btn-ghost btn-sm w-full mt-1 text-[10px]" title="Fill the whole frame (cover) and send behind the cams — for a full-screen backdrop"><i class="fa-sharp fa-solid fa-expand mr-1"></i>Fill frame as backdrop</button>
        <label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Opacity</label><input type="range" data-prop="opacity" data-num="1" min="0" max="1" step="0.05" value="${s.opacity != null ? s.opacity : 1}" class="w-full"/>`;
    } else if (s.type === 'text') {
      body += `<label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Text</label><input data-prop="text" class="form-input py-0.5 text-xs w-full" value="${esc(s.text || '')}"/>
        <div class="grid grid-cols-2 gap-2 items-center mt-1">
          <div><label class="block text-[10px] text-white/40 uppercase tracking-wider">Size</label><input type="range" data-prop="size" data-num="1" min="0.1" max="1" step="0.05" value="${s.size != null ? s.size : 0.5}" class="w-full"/></div>
          <div><label class="block text-[10px] text-white/40 uppercase tracking-wider">Color</label><input type="color" data-prop="color" value="${esc(s.color || '#ffffff')}" class="w-full h-6 bg-transparent"/></div>
        </div>
        <label class="block text-[10px] text-white/40 uppercase tracking-wider mt-1">Align</label>
        <select data-prop="align" class="form-input py-0.5 text-xs w-full"><option value="left"${s.align === 'left' ? ' selected' : ''}>Left</option><option value="center"${s.align === 'center' ? ' selected' : ''}>Center</option><option value="right"${s.align === 'right' ? ' selected' : ''}>Right</option></select>`;
    } else if (s.type === 'videostage') {
      body += `<div class="text-[10px] text-white/50 mt-2 leading-snug">The Video Stage plays the <b class="text-white/80">program</b> — cue clips from the PLAYLIST panel; the transport bar below the stage drives playback.</div>`;
    } else if (s.type === 'video') {
      body += `<div class="text-[10px] text-white/40 uppercase tracking-wider mt-2">Legacy clip source.</div>`;
    }

    body +=
      `<div class="mt-2 pt-1 border-t border-white/10"><div class="text-[10px] text-white/40 uppercase tracking-wider mb-1">Layout</div>
      <div class="grid grid-cols-2 gap-1">` +
      LAYOUTS.map(([k, l]) => `<button data-arrange="${k}" class="btn-ghost btn-sm text-[10px]">${l}</button>`).join('') +
      `</div></div>`;
    this.propsEl.innerHTML = body;
  }

  renderScenes() {
    if (!this.scenesEl) return;
    const tabs = this.model.scenes
      .map(
        (sc) =>
          `<div class="flex items-center gap-0.5 shrink-0">
            <button data-sact="switch" data-id="${sc.id}" class="px-2 py-1 text-xs font-mono border-2 ${sc.id === this.model.active ? 'border-white/60 bg-white/10' : 'border-white/20 hover:border-white/40'}">${esc(sc.name)}</button>
            ${this.model.scenes.length > 1 ? `<button data-sact="del" data-id="${sc.id}" class="text-white/20 hover:text-red-400 px-0.5" title="Delete scene"><i class="fa-sharp fa-solid fa-xmark text-[10px]"></i></button>` : ''}
          </div>`,
      )
      .join('');
    const tr = this.model.transition || { type: 'fade', ms: 400 };
    const tb = (type, label) =>
      `<button data-sact="trans" data-type="${type}" class="px-1.5 py-0.5 border ${tr.type === type ? 'border-white/60 bg-white/10' : 'border-white/20 hover:border-white/40'}">${label}</button>`;
    this.scenesEl.innerHTML =
      `<div class="flex items-center gap-1 flex-wrap">${tabs}<button data-sact="add" class="btn-ghost btn-sm shrink-0" title="Add scene"><i class="fa-sharp fa-solid fa-plus"></i></button></div>` +
      `<div class="flex items-center gap-1 mt-1.5 pt-1.5 border-t border-white/10 text-[10px]">
        <i class="fa-sharp fa-solid fa-right-left text-white/40 shrink-0" title="Scene transition"></i>${tb('cut', 'Cut')}${tb('fade', 'Fade')}
        <input data-sact="transms" type="range" min="100" max="1500" step="50" value="${tr.ms || 400}" class="flex-1 min-w-0" ${tr.type === 'cut' ? 'disabled' : ''}/>
        <span class="text-white/50 tabular-nums shrink-0">${tr.ms || 400}ms</span>
      </div>`;
  }
}

// StageInteract adds OBS-style direct manipulation on the producer stage: click
// a source to select it, drag to move, drag a corner handle to resize. It draws
// a selection box + handles as DOM overlays positioned over the letter/pillarboxed
// stage rect, and writes transforms back through the editor (instant + persisted).
class StageInteract {
  constructor(editor, core) {
    this.editor = editor;
    this.core = core;
    this.canvas = core.renderer.domElement;
    this.container = this.canvas.parentElement;
    this.drag = null;
    this._build();
    this._bind();
    this._raf();
  }

  _mk(z, pe) {
    const d = document.createElement('div');
    d.style.position = 'absolute';
    d.style.display = 'none';
    d.style.zIndex = String(z);
    d.style.pointerEvents = pe;
    this.container.appendChild(d);
    return d;
  }

  _build() {
    this.box = this._mk(15, 'none');
    this.box.style.border = '1px solid rgba(255,255,255,0.85)';
    this.box.style.boxShadow = '0 0 0 1px rgba(0,0,0,0.6)';
    this.handles = {};
    for (const corner of ['nw', 'ne', 'sw', 'se']) {
      const h = this._mk(16, 'auto');
      h.dataset.corner = corner;
      h.style.width = '11px';
      h.style.height = '11px';
      h.style.background = '#fff';
      h.style.border = '1px solid #000';
      h.style.cursor = corner === 'nw' || corner === 'se' ? 'nwse-resize' : 'nesw-resize';
      this.handles[corner] = h;
    }
  }

  _coords(e) {
    const r = this.canvas.getBoundingClientRect();
    const sr = this.core.stageRectPx();
    return { nx: (e.clientX - r.left - sr.left) / sr.w, ny: (e.clientY - r.top - sr.top) / sr.h, sr };
  }

  _bind() {
    const clamp = (v) => Math.max(0, Math.min(1, v));
    this.canvas.addEventListener('pointerdown', (e) => {
      const { nx, ny } = this._coords(e);
      const s = this.editor.hitTest(nx, ny);
      if (!s) return;
      if (s.id !== this.editor.sel) this.editor.select(s.id);
      this.drag = { mode: 'move', id: s.id, startNx: nx, startNy: ny, ox: s.x, oy: s.y };
      e.preventDefault();
    });
    for (const corner in this.handles) {
      this.handles[corner].addEventListener('pointerdown', (e) => {
        const s = this.editor.get(this.editor.sel);
        if (!s) return;
        this.drag = { mode: 'resize', corner, id: s.id, fixedX: corner.includes('w') ? s.x + s.w / 2 : s.x - s.w / 2, fixedY: corner.includes('n') ? s.y + s.h / 2 : s.y - s.h / 2 };
        e.preventDefault();
        e.stopPropagation();
      });
    }
    pageListen(window, 'pointermove', (e) => {
      if (!this.drag) return;
      const { nx, ny } = this._coords(e);
      if (this.drag.mode === 'move') {
        this.editor.setTransformLive(this.drag.id, { x: clamp(this.drag.ox + (nx - this.drag.startNx)), y: clamp(this.drag.oy + (ny - this.drag.startNy)) });
      } else {
        const px = clamp(nx),
          py = clamp(ny);
        let w = Math.abs(px - this.drag.fixedX);
        let h = Math.abs(py - this.drag.fixedY);
        const lockN = this.editor.lockedAspectN ? this.editor.lockedAspectN(this.drag.id) : null;
        if (lockN) {
          // keep the source's locked aspect: grow to the dominant axis.
          if (w / h > lockN) h = w / lockN;
          else w = h * lockN;
          const farX = this.drag.fixedX + (px >= this.drag.fixedX ? w : -w);
          const farY = this.drag.fixedY + (py >= this.drag.fixedY ? h : -h);
          this.editor.setTransformLive(this.drag.id, { x: (this.drag.fixedX + farX) / 2, y: (this.drag.fixedY + farY) / 2, w: Math.max(0.02, w), h: Math.max(0.02, h) });
        } else {
          this.editor.setTransformLive(this.drag.id, { x: (this.drag.fixedX + px) / 2, y: (this.drag.fixedY + py) / 2, w: Math.max(0.02, w), h: Math.max(0.02, h) });
        }
      }
    });
    pageListen(window, 'pointerup', () => {
      if (this.drag) {
        this.drag = null;
        this.editor.renderProps(); // sync the Properties sliders to the dragged values
      }
    });
  }

  _raf() {
    const tick = () => {
      this._position();
      this._rafId = pageFrame(tick);
    };
    tick();
  }

  _position() {
    const s = this.editor.get(this.editor.sel);
    if (!s || s.type === 'background' || s.visible === false) {
      this.box.style.display = 'none';
      for (const k in this.handles) this.handles[k].style.display = 'none';
      return;
    }
    const sr = this.core.stageRectPx();
    const w = (s.w || 0.2) * sr.w;
    const h = (s.h || 0.2) * sr.h;
    const L = sr.left + (s.x != null ? s.x : 0.5) * sr.w - w / 2;
    const T = sr.top + (s.y != null ? s.y : 0.5) * sr.h - h / 2;
    this.box.style.display = 'block';
    this.box.style.left = L + 'px';
    this.box.style.top = T + 'px';
    this.box.style.width = w + 'px';
    this.box.style.height = h + 'px';
    const place = (el, x, y) => {
      el.style.display = 'block';
      el.style.left = x - 6 + 'px';
      el.style.top = y - 6 + 'px';
    };
    place(this.handles.nw, L, T);
    place(this.handles.ne, L + w, T);
    place(this.handles.sw, L, T + h);
    place(this.handles.se, L + w, T + h);
  }
}

// Bootstrap: mount the editor on the producer page once SceneCore is ready.
(function () {
  const root = document.getElementById('scene-editor-root');
  if (!root) return;
  function start() {
    const core = window.__rewindScene;
    if (!core) {
      pageTimeout(start, 60);
      return;
    }
    const editor = new SceneEditor({
      noteId: root.dataset.noteId,
      clientId: root.dataset.clientId,
      userId: root.dataset.userId,
      core,
      sourcesEl: document.getElementById('scene-sources'),
      propsEl: document.getElementById('scene-props'),
      scenesEl: document.getElementById('scene-tabs'),
    });
    let initial = {};
    try {
      initial = JSON.parse(b64utf8(root.dataset.sceneB64));
    } catch (_) {}
    editor.load(initial);
    window.__sceneEditor = editor;
    window.__stageInteract = new StageInteract(editor, core);
  }
  start();
})();

