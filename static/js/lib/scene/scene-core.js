import { listen as pageListen, pageFrame } from '../page-scope.js';
// scene-core.js — the shared Three.js compositor used by both the producer
// preview and the program output. It renders a scene as a z-ordered list of
// SOURCES — background (animated effect), video (program content w/ transport),
// webcam (remote WebRTC stream), image (logo/overlay), and text (lower-third) —
// each with a normalized, center-based transform.
//
// Scene JSON v3:
//   { version:3, active:"<sceneId>", transition:{type,ms}, stage:{aspect},
//     scenes:[ { id, name, sources:[ source... ] } ] }
// Older v1/v2 scenes (flat background+content+layout+cams) are synthesized into a
// single source list so existing notes + the pre-v3 producer keep rendering.

import * as THREE from 'three';
import { createEffectMaterial } from './effects.js';

function num(v, d) {
  const n = Number(v);
  return Number.isFinite(n) ? n : d;
}
function clamp(v, lo, hi, d) {
  const n = num(v, d);
  return Math.max(lo, Math.min(hi, n));
}
function parseAspect(s) {
  if (typeof s !== 'string') return null;
  const m = s.match(/^\s*(\d+(?:\.\d+)?)\s*[:/]\s*(\d+(?:\.\d+)?)\s*$/);
  if (!m) return null;
  const a = Number(m[1]),
    b = Number(m[2]);
  return b > 0 ? a / b : null;
}

// OKLCH -> linear sRGB (the hand-rolled fallback from the flat renderer; no DOM).
function oklchToRGB(t) {
  if (!t || typeof t !== 'object') return [1, 1, 1];
  const L = clamp(t.l, 0, 1, 1);
  const C = clamp(t.c, 0, 1, 0);
  const h = (clamp(t.h, 0, 360, 0) * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l3 = l_ * l_ * l_,
    m3 = m_ * m_ * m_,
    s3 = s_ * s_ * s_;
  const toSrgb = (v) => {
    v = Math.max(0, Math.min(1, v));
    return v <= 0.0031308 ? 12.92 * v : 1.055 * Math.pow(v, 1 / 2.4) - 0.055;
  };
  return [
    toSrgb(4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3),
    toSrgb(-1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3),
    toSrgb(-0.0041960863 * l3 - 0.7034186147 * m3 + 1.707614701 * s3),
  ];
}

export class SceneCore {
  constructor(canvas, opts = {}) {
    this.renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true, premultipliedAlpha: false });
    // Transparent clear for OBS; opaque (black) for direct viewing.
    this.renderer.setClearColor(0x000000, opts.transparent ? 0 : 1);

    this.scene = new THREE.Scene();
    this.camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0.1, 100);
    this.camera.position.z = 10;

    // Source registry: id -> entry {type, mesh, mat, ...type-specific}.
    this._sources = new Map();
    this._lastSources = null; // last reconciled source list (for cam re-eval)
    this._lastRawScene = null; // last scene passed to applyScene (for re-synth)
    this._order = []; // current source ids, back -> front

    // Warm video element cache (shared by all video sources) so re-selecting a
    // clip is instant; webcam registry comes from webrtc-room.js.
    this._videoCache = new Map(); // src -> HTMLVideoElement
    this._camRegistry = new Map(); // streamId -> HTMLVideoElement
    this._camMeta = new Map(); // streamId -> { userId, username, self }
    this._camMetaSig = ''; // change detector for identity updates

    this._statusCb = null;
    this._contentState = '';
    this._contentSrc = ''; // src of the active program/video (drives status)
    this._selVideo = null; // selected video source id (legacy transport target)

    // The program bus: the "now playing" clip from the show-note playlist. Video
    // Stage sources render it; the transport targets it. Top-level on the scene
    // (shared across scenes), not per-source.
    this._program = null;
    this._programSrc = '';
    this._programVideo = null;
    this._programEndedCb = null;

    this.epochMs = 0;
    this.fixedW = opts.fixedWidth || 0; // OBS: lock render resolution
    this.fixedH = opts.fixedHeight || 0;
    this.stageAspect = 16 / 9;
    this._transition = null;
    this._activeId = null;
    this._rt = null; // render target for the transition snapshot
    this._fadeMesh = null;
    this._fadeMat = null;
    this._fadeStart = 0;
    this._fadeMs = 0;
    this._localEpoch = Date.now();

    this._onResize = () => this._resize();
    pageListen(window, 'resize', this._onResize, { passive: true });
    this._resize();
    this._running = true;
    this._loop();
  }

  _resize() {
    const c = this.renderer.domElement;
    const w = Math.max(1, this.fixedW || c.clientWidth);
    const h = Math.max(1, this.fixedH || c.clientHeight);
    this.renderer.setPixelRatio(this.fixedW ? 1 : Math.min(2, window.devicePixelRatio || 1));
    this.renderer.setSize(w, h, !this.fixedW);
    const va = w / h;
    this.camera.left = -va;
    this.camera.right = va;
    this.camera.top = 1;
    this.camera.bottom = -1;
    this.camera.updateProjectionMatrix();
    if (this._lastSources) this._reconcile(this._lastSources);
  }

  // The stage rect (in world units) honoring stageAspect, letter/pillarboxed.
  _stageRect() {
    const va = this.camera.right;
    if (va >= this.stageAspect) {
      const sh = 2;
      return { w: sh * this.stageAspect, h: sh };
    }
    const sw = va * 2;
    return { w: sw, h: sw / this.stageAspect };
  }

  _normToWorld(nx, ny, stage) {
    return [(nx - 0.5) * stage.w, (0.5 - ny) * stage.h];
  }

  // stageRectPx returns the letter/pillarboxed stage rectangle in canvas CSS
  // pixels, used by the producer's drag-to-position overlay to map pointer
  // positions to normalized stage coordinates.
  stageRectPx() {
    const c = this.renderer.domElement;
    const W = c.clientWidth || 1;
    const H = c.clientHeight || 1;
    let w, h;
    if (W / H >= this.stageAspect) {
      h = H;
      w = H * this.stageAspect;
    } else {
      w = W;
      h = W / this.stageAspect;
    }
    return { left: (W - w) / 2, top: (H - h) / 2, w, h, cw: W, ch: H };
  }

  // ── Scene normalization ────────────────────────────────────────────────────

  // _normalize resolves the active scene's source list, supporting v3 directly
  // and synthesizing one from v1/v2 (flat background+content+layout+cams).
  _normalize(scene) {
    scene = scene || {};
    const stageAspect = parseAspect(scene.stage && scene.stage.aspect) || 16 / 9;
    if (scene.version >= 3 && Array.isArray(scene.scenes)) {
      const active = scene.scenes.find((s) => s && s.id === scene.active) || scene.scenes[0] || { sources: [] };
      return { sources: Array.isArray(active.sources) ? active.sources : [], stageAspect, transition: scene.transition, active: active.id };
    }
    return { sources: this._synthSources(scene), stageAspect, transition: null, active: null };
  }

  // _synthSources converts a v1/v2 scene into the v3 source list.
  _synthSources(scene) {
    const out = [];
    const bg = scene.background || {};
    const mode = bg.mode || 'perlin-nebula';
    out.push({
      id: '_bg',
      type: 'background',
      visible: mode !== 'none',
      mode,
      speed: bg.speed,
      seed: bg.seed,
      tint: bg.tint_oklch,
      epoch_ms: bg.epoch_ms,
    });

    const layout = scene.layout || 'content-pip';
    const showContent = layout !== 'solo' && layout !== 'duo' && layout !== 'grid';
    const content = scene.content;
    const vt = scene.video || {};
    if (content && content.src && showContent) {
      out.push({
        id: '_content',
        type: 'video',
        visible: true,
        x: num(vt.x, 0.5),
        y: num(vt.y, 0.5),
        w: num(vt.width != null ? vt.width : vt.w, 0.9),
        h: num(vt.height != null ? vt.height : vt.h, 0.9),
        video_id: content.video_id,
        src: content.src,
        t0_epoch_ms: content.t0_epoch_ms,
        t: content.t,
        paused: content.paused,
        volume: content.volume,
        muted: content.muted,
        loop: content.loop,
      });
    }

    const camIds = this._orderedCamIds();
    const tfs = this._layoutCamTransforms(layout, camIds.length);
    camIds.forEach((id, i) => {
      out.push({ id: '_cam_' + id, type: 'webcam', visible: layout !== 'content-only', cam: i, ...tfs[i] });
    });
    return out;
  }

  // _orderedCamIds returns webcam stream ids in a deterministic (sorted) order so
  // positional slotting (cam:index) is identical on the producer, other hosts, and
  // the viewer — each sees the same set of stream ids and sorts them the same way.
  _orderedCamIds() {
    return [...this._camRegistry.keys()].sort();
  }

  // _resolveCamVideo picks the <video> for a webcam/screen source. It matches the
  // requested KIND (a webcam source wants a camera stream, a screen source wants a
  // screen stream), then binds by hostId (semantic + stable across reorder) when
  // set, else by positional slot (cam index) into the stable-sorted stream list.
  // A hostId that isn't connected yet resolves to null (placeholder).
  _resolveCamVideo(s) {
    const wantScreen = !!(s && s.type === 'screen');
    if (s && s.hostId) {
      for (const [sid, m] of this._camMeta) {
        if (!m || m.userId !== s.hostId) continue;
        if ((m.kind === 'screen') !== wantScreen) continue;
        if (this._camRegistry.has(sid)) return this._camRegistry.get(sid);
      }
      return null;
    }
    const ids = this._orderedCamIds().filter((id) => {
      const m = this._camMeta.get(id);
      return (m && m.kind === 'screen') === wantScreen;
    });
    const id = ids[num(s && s.cam, 0)];
    return id ? this._camRegistry.get(id) : null;
  }

  // _layoutCamTransforms returns normalized (center-based) transforms for n
  // webcam tiles under the named legacy layout. On a 16:9 stage a 16:9 tile has
  // equal width/height fractions, which keeps this math simple.
  _layoutCamTransforms(layout, n) {
    const out = [];
    if (n === 0) return out;
    if (layout === 'content-pip') {
      const w = Math.min(0.85 / n, 0.24);
      const y = 1 - w / 2 - 0.02;
      for (let i = 0; i < n; i++) out.push({ x: 0.5 + (i - (n - 1) / 2) * w, y, w, h: w });
      return out;
    }
    const cols = Math.ceil(Math.sqrt(n));
    const rows = Math.ceil(n / cols);
    const w = Math.min(0.95 / cols, 0.9 / rows);
    for (let i = 0; i < n; i++) {
      const r = Math.floor(i / cols);
      const c = i % cols;
      out.push({ x: 0.5 + (c - (cols - 1) / 2) * w, y: 0.5 + (r - (rows - 1) / 2) * w, w, h: w });
    }
    return out;
  }

  // ── Apply + reconcile ──────────────────────────────────────────────────────

  applyScene(scene) {
    this._lastRawScene = scene;
    const norm = this._normalize(scene);
    this.stageAspect = norm.stageAspect;
    this._transition = norm.transition || null;
    // Crossfade when switching between scenes (not on first apply or edits within
    // a scene): snapshot the current frame, reconcile underneath, fade it out.
    if (this._activeId != null && norm.active != null && norm.active !== this._activeId) {
      this._beginTransition();
    }
    this._activeId = norm.active;
    this._applyProgram(scene && scene.program); // program bus before reconcile
    this._reconcile(norm.sources);
  }

  // _applyProgram drives the program bus: swaps the shared "now playing" video
  // when the playlist cue changes, then applies its transport. Video Stage
  // sources texture this._programVideo.
  _applyProgram(program) {
    this._program = program || null;
    const src = program && program.src ? program.src : '';
    if (src !== this._programSrc) {
      if (this._programVideo) {
        try {
          this._programVideo.pause();
        } catch (_) {}
      }
      this._programSrc = src;
      this._contentSrc = src;
      this._programVideo = src ? this._videoFor(src) : null;
      this._emitStatus(src ? (this._programVideo.readyState >= 3 ? 'playing' : 'loading') : 'idle');
    }
    const v = this._programVideo;
    if (!v) return;
    const c = program || {};
    v.volume = Math.max(0, Math.min(1, c.volume == null ? 1 : c.volume));
    v.muted = !!c.muted;
    v.loop = !!c.loop;
    const paused = !!c.paused;
    const anchorT = c.t || 0;
    const t0 = c.t0_epoch_ms || 0;
    const target = paused ? anchorT : t0 > 0 ? anchorT + (Date.now() - t0) / 1000 : anchorT;
    const apply = () => {
      if (Number.isFinite(v.duration) && v.duration > 0) {
        const tp = Math.max(0, Math.min(target, v.duration - 0.05));
        if (Math.abs(v.currentTime - tp) > 0.6) {
          try {
            v.currentTime = tp;
          } catch (_) {}
        }
      }
      if (paused) v.pause();
      else
        v.play().catch(() => {
          v.muted = true;
          v.play().catch(() => {});
        });
    };
    if (v.readyState >= 1) apply();
    else v.addEventListener('loadedmetadata', apply, { once: true });
  }

  // _beginTransition captures the current composited frame to a render target and
  // overlays it at full opacity; _loop fades it out over transition.ms, revealing
  // the freshly-reconciled scene underneath.
  _beginTransition() {
    const ms = (this._transition && this._transition.ms) || 0;
    if (!this._transition || this._transition.type === 'cut' || ms <= 0) return;
    const c = this.renderer.domElement;
    const w = Math.max(1, c.width);
    const h = Math.max(1, c.height);
    if (!this._rt || this._rt.width !== w || this._rt.height !== h) {
      if (this._rt) this._rt.dispose();
      this._rt = new THREE.WebGLRenderTarget(w, h);
    }
    this.renderer.setRenderTarget(this._rt);
    this.renderer.render(this.scene, this.camera);
    this.renderer.setRenderTarget(null);
    if (!this._fadeMesh) {
      this._fadeMat = new THREE.MeshBasicMaterial({ transparent: true, depthTest: false, depthWrite: false });
      this._fadeMesh = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), this._fadeMat);
      this._fadeMesh.renderOrder = 99999;
      this.scene.add(this._fadeMesh);
    }
    this._fadeMat.map = this._rt.texture;
    this._fadeMat.opacity = 1;
    this._fadeMesh.visible = true;
    const va = this.camera.right;
    this._fadeMesh.scale.set(va * 2, 2, 1);
    this._fadeMesh.position.set(0, 0, 6);
    this._fadeStart = Date.now();
    this._fadeMs = ms;
  }

  // _reconcile creates/updates/removes source meshes to match the source list,
  // positioning each by its transform and z-ordering by array index.
  _reconcile(sources) {
    this._lastSources = sources;
    const stage = this._stageRect();
    const seen = new Set();
    const videoIds = [];

    sources.forEach((s, i) => {
      if (!s || !s.id || !s.type) return;
      seen.add(s.id);
      let e = this._sources.get(s.id);
      if (!e || e.type !== s.type) {
        if (e) this._disposeEntry(e);
        e = this._makeEntry(s);
        this._sources.set(s.id, e);
      }
      this._updateEntry(e, s, i, stage);
      e.mesh.visible = s.visible !== false && !(s.type === 'background' && (s.mode || '') === 'none');
      if (e.backing) e.backing.visible = e.mesh.visible;
      if (s.type === 'video') videoIds.push(s.id);
    });

    for (const id of [...this._sources.keys()]) {
      if (seen.has(id)) continue;
      this._disposeEntry(this._sources.get(id));
      this._sources.delete(id);
    }
    this._order = sources.map((s) => s && s.id).filter(Boolean);

    // Keep a valid selected video source for the transport UI.
    if (!this._selVideo || !this._sources.has(this._selVideo) || this._sources.get(this._selVideo).type !== 'video') {
      this._selVideo = videoIds[0] || null;
    }
    const sel = this._selVideo && this._sources.get(this._selVideo);
    this._contentSrc = sel && sel.src ? sel.src : '';
    if (!this._contentSrc) this._emitStatus('idle');
  }

  _makeEntry(s) {
    switch (s.type) {
      case 'background':
        return this._makeBackground(s);
      case 'videostage':
        return this._makeMeshEntry('videostage');
      case 'video':
        return this._makeMeshEntry('video');
      case 'webcam':
        return this._makeMeshEntry('webcam');
      case 'screen':
        return this._makeMeshEntry('screen');
      case 'image':
        return this._makeMeshEntry('image');
      case 'text':
        return this._makeMeshEntry('text');
      default:
        return this._makeMeshEntry(s.type || 'unknown');
    }
  }

  _makeMeshEntry(type) {
    const mat = new THREE.MeshBasicMaterial({ color: 0x111317, transparent: true, opacity: type === 'video' ? 0.45 : 1 });
    const mesh = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), mat);
    this.scene.add(mesh);
    const e = { type, mesh, mat, src: '', video: null, tex: null, url: '', textKey: '', vid: null, box: null, backing: null, backingMat: null };
    // Letterboxed media (stage/video/webcam/screen) get a black backing so the
    // aspect-fit content shows clean bars rather than the scene behind.
    if (type === 'videostage' || type === 'video' || type === 'webcam' || type === 'screen') {
      const bmat = new THREE.MeshBasicMaterial({ color: 0x000000, transparent: true, opacity: 1 });
      const bmesh = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), bmat);
      this.scene.add(bmesh);
      e.backing = bmesh;
      e.backingMat = bmat;
    }
    return e;
  }

  // _letterboxed: media that should preserve aspect (fit) within its box.
  _letterboxed(type) {
    return type === 'videostage' || type === 'video' || type === 'webcam' || type === 'screen' || type === 'image';
  }

  // _sourceAspect returns the content's native width/height, or 0 if unknown yet.
  _sourceAspect(e) {
    if (e.type === 'videostage') {
      const v = this._programVideo;
      return v && v.videoWidth ? v.videoWidth / v.videoHeight : 0;
    }
    if (e.type === 'video') return e.video && e.video.videoWidth ? e.video.videoWidth / e.video.videoHeight : 0;
    if (e.type === 'webcam' || e.type === 'screen') return e.vid && e.vid.videoWidth ? e.vid.videoWidth / e.vid.videoHeight : 0;
    if (e.type === 'image') return e.tex && e.tex.image && e.tex.image.width ? e.tex.image.width / e.tex.image.height : 0;
    return 0;
  }

  // _fitTarget sizes the content mesh within its box while preserving the content
  // aspect: `contain` (default) fits inside with bars; `cover` (images only) fills
  // the box, letting the excess extend past the box edges. Computed in the render
  // loop so it updates once dimensions are known.
  _fitTarget(e) {
    const b = e.box;
    if (!b) return;
    let cw = b.w;
    let ch = b.h;
    const a = this._sourceAspect(e);
    if (a > 0) {
      const ba = b.w / b.h;
      if (e.fit === 'cover') {
        if (a > ba) {
          ch = b.h;
          cw = b.h * a;
        } else {
          cw = b.w;
          ch = b.w / a;
        }
      } else if (a > ba) {
        ch = b.w / a;
      } else {
        cw = b.h * a;
      }
    }
    e.mesh.userData.target = { px: b.x, py: b.y, sx: cw, sy: ch };
  }

  _makeBackground(s) {
    const mat = createEffectMaterial(s.mode || 'perlin-nebula');
    const mesh = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), mat);
    this.scene.add(mesh);
    return { type: 'background', mesh, mat, mode: s.mode || 'perlin-nebula', speed: 1, seed: 0, epoch: 0 };
  }

  _updateEntry(e, s, index, stage) {
    switch (s.type) {
      case 'background':
        this._updateBackground(e, s, index);
        return; // background is full-frame; no transform
      case 'videostage':
        this._updateVideoStage(e);
        break;
      case 'video':
        this._updateVideo(e, s);
        break;
      case 'webcam':
      case 'screen':
        this._updateWebcam(e, s);
        break;
      case 'image':
        this._updateImage(e, s);
        break;
      case 'text':
        this._updateText(e, s);
        break;
    }
    this._positionSource(e, s, index, stage);
  }

  // _updateVideoStage textures the program bus video onto a stage source; a dim
  // placeholder shows when nothing is cued.
  _updateVideoStage(e) {
    const v = this._programVideo;
    if (v === e.vid) return;
    if (e.tex) {
      e.tex.dispose();
      e.tex = null;
    }
    e.vid = v;
    if (v) {
      const tex = new THREE.VideoTexture(v);
      tex.colorSpace = THREE.SRGBColorSpace;
      e.tex = tex;
      e.mat.map = tex;
      e.mat.color.setHex(0xffffff);
      e.mat.opacity = 1;
    } else {
      e.mat.map = null;
      e.mat.color.setHex(0x0a0a0a);
      e.mat.opacity = 0.45;
    }
    e.mat.needsUpdate = true;
  }

  _positionSource(e, s, index, stage) {
    const w = clamp(s.w, 0.02, 1.5, 0.3) * stage.w;
    const h = clamp(s.h, 0.02, 1.5, 0.3) * stage.h;
    const [x, y] = this._normToWorld(num(s.x, 0.5), num(s.y, 0.5), stage);
    e.box = { x, y, w, h };
    const z = 0.01 * (index + 1);
    const ro = index + 1;
    if (e.backing) {
      e.backing.userData.target = { px: x, py: y, sx: w, sy: h };
      e.backing.position.z = z - 0.004;
      e.backing.renderOrder = ro - 0.5;
    }
    e.mesh.position.z = z;
    e.mesh.renderOrder = ro;
    if (this._letterboxed(e.type)) {
      this._fitTarget(e); // content fits its aspect within the box (refined each frame)
    } else {
      e.mesh.userData.target = { px: x, py: y, sx: w, sy: h };
    }
  }

  _updateBackground(e, s, index) {
    const mode = s.mode || 'perlin-nebula';
    if (mode !== 'none' && mode !== e.mode) {
      const old = e.mat;
      e.mat = createEffectMaterial(mode);
      e.mesh.material = e.mat;
      old.dispose();
    }
    e.mode = mode;
    e.speed = clamp(s.speed, 0, 10, 1);
    e.seed = num(s.seed, 0);
    e.epoch = num(s.epoch_ms, 0);
    if (e.mat.uniforms && e.mat.uniforms.u_tint) {
      const rgb = oklchToRGB(s.tint || s.tint_oklch);
      e.mat.uniforms.u_tint.value.setRGB(rgb[0], rgb[1], rgb[2]);
    }
    const va = this.camera.right;
    e.mesh.scale.set(va * 2, 2, 1);
    e.mesh.position.set(0, 0, 0.01 * (index + 1));
    e.mesh.renderOrder = index + 1;
    e.mesh.userData.target = null; // background never tweens
  }

  _updateVideo(e, s) {
    const src = s.src || '';
    if (src !== e.src) {
      if (e.video) {
        try {
          e.video.pause();
        } catch (_) {}
      }
      if (e.tex) {
        e.tex.dispose();
        e.tex = null;
      }
      e.src = src;
      e.video = null;
      if (!src) {
        e.mat.map = null;
        e.mat.color.setHex(0x111317);
        e.mat.opacity = 0.45;
        e.mat.needsUpdate = true;
        return;
      }
      const v = this._videoFor(src);
      e.video = v;
      const tex = new THREE.VideoTexture(v);
      tex.colorSpace = THREE.SRGBColorSpace;
      e.tex = tex;
      e.mat.map = tex;
      e.mat.color.setHex(0xffffff);
      e.mat.opacity = 1;
      e.mat.needsUpdate = true;
      if (s.id === this._selVideo) this._emitStatus(v.readyState >= 3 ? 'playing' : 'loading');
    }
    if (!e.video) return;

    const v = e.video;
    v.volume = Math.max(0, Math.min(1, s.volume == null ? 1 : s.volume));
    v.muted = !!s.muted;
    v.loop = !!s.loop;

    const paused = !!s.paused;
    const anchorT = s.t || 0;
    const t0 = s.t0_epoch_ms || 0;
    const target = paused ? anchorT : t0 > 0 ? anchorT + (Date.now() - t0) / 1000 : anchorT;

    const apply = () => {
      if (Number.isFinite(v.duration) && v.duration > 0) {
        const tp = Math.max(0, Math.min(target, v.duration - 0.05));
        if (Math.abs(v.currentTime - tp) > 0.6) {
          try {
            v.currentTime = tp;
          } catch (_) {}
        }
      }
      if (paused) v.pause();
      else
        v.play().catch(() => {
          v.muted = true;
          v.play().catch(() => {});
        });
    };
    if (v.readyState >= 1) apply();
    else v.addEventListener('loadedmetadata', apply, { once: true });
  }

  _updateWebcam(e, s) {
    const vid = this._resolveCamVideo(s) || null;
    if (vid !== e.vid) {
      if (e.tex) {
        e.tex.dispose();
        e.tex = null;
      }
      e.vid = vid;
      if (vid) {
        const tex = new THREE.VideoTexture(vid);
        tex.colorSpace = THREE.SRGBColorSpace;
        e.tex = tex;
        e.mat.map = tex;
        e.mat.color.setHex(0xffffff);
      } else {
        e.mat.map = null;
        e.mat.color.setHex(0x1a1d24);
      }
      e.mat.needsUpdate = true;
    }
  }

  _updateImage(e, s) {
    const url = s.url || '';
    if (url && url !== e.url) {
      e.url = url;
      new THREE.TextureLoader().load(
        url,
        (tex) => {
          tex.colorSpace = THREE.SRGBColorSpace;
          if (e.tex) e.tex.dispose();
          e.tex = tex;
          e.mat.map = tex;
          e.mat.color.setHex(0xffffff);
          e.mat.needsUpdate = true;
        },
        undefined,
        () => {
          e.mat.map = null;
          e.mat.color.setHex(0x331111);
          e.mat.needsUpdate = true;
        },
      );
    } else if (!url) {
      e.url = '';
      e.mat.map = null;
      e.mat.color.setHex(0x222630);
      e.mat.needsUpdate = true;
    }
    e.fit = s.fit === 'cover' ? 'cover' : 'contain';
    e.mat.opacity = clamp(s.opacity, 0, 1, 1);
  }

  // _updateText renders the lower-third to a canvas whose aspect matches the
  // source box (so the plane maps it 1:1 — no horizontal smear) with the font
  // sized as a fraction of the box height, so the text scales with the box.
  _updateText(e, s) {
    const planeAspect = Math.max(0.25, (num(s.w, 0.5) / num(s.h, 0.1)) * this.stageAspect);
    const key = JSON.stringify({ t: s.text || '', sz: s.size, c: s.color, bg: s.bg, a: s.align, pa: Math.round(planeAspect * 20) });
    if (key === e.textKey) return;
    e.textKey = key;
    const H = 256;
    const W = Math.max(64, Math.min(8192, Math.round(H * planeAspect)));
    const cv = e.canvas || (e.canvas = document.createElement('canvas'));
    cv.width = W;
    cv.height = H;
    const ctx = cv.getContext('2d');
    ctx.clearRect(0, 0, W, H);
    if (s.bg) {
      ctx.fillStyle = s.bg;
      ctx.fillRect(0, 0, W, H);
    }
    const fs = clamp(s.size, 0.1, 1, 0.5) * H * 0.6;
    ctx.font = `700 ${fs}px ui-sans-serif, system-ui, sans-serif`;
    ctx.fillStyle = s.color || '#ffffff';
    ctx.textBaseline = 'middle';
    const align = s.align === 'center' ? 'center' : s.align === 'right' ? 'right' : 'left';
    ctx.textAlign = align;
    const pad = H * 0.18;
    const x = align === 'center' ? W / 2 : align === 'right' ? W - pad : pad;
    ctx.fillText(s.text || '', x, H / 2);
    if (e.tex) e.tex.dispose();
    e.tex = new THREE.CanvasTexture(cv);
    e.tex.colorSpace = THREE.SRGBColorSpace;
    e.mat.map = e.tex;
    e.mat.color.setHex(0xffffff);
    e.mat.needsUpdate = true;
  }

  _disposeEntry(e) {
    if (!e) return;
    if (e.type === 'video' && e.video) {
      try {
        e.video.pause();
      } catch (_) {}
    }
    this.scene.remove(e.mesh);
    if (e.tex) e.tex.dispose();
    if (e.mat) e.mat.dispose();
    if (e.mesh && e.mesh.geometry) e.mesh.geometry.dispose();
    if (e.backing) {
      this.scene.remove(e.backing);
      if (e.backingMat) e.backingMat.dispose();
      if (e.backing.geometry) e.backing.geometry.dispose();
    }
  }

  // ── Content video cache + status ───────────────────────────────────────────

  setStatusCallback(cb) {
    this._statusCb = cb;
  }

  _emitStatus(state) {
    if (state === this._contentState) return;
    this._contentState = state;
    if (this._statusCb) {
      try {
        this._statusCb(state);
      } catch (_) {}
    }
  }

  _videoFor(src) {
    let v = this._videoCache.get(src);
    if (v) return v;
    v = document.createElement('video');
    v.crossOrigin = 'anonymous';
    v.muted = true;
    v.playsInline = true;
    v.loop = false;
    v.preload = 'auto';
    v.src = src;
    const loading = () => {
      if (this._contentSrc === src) this._emitStatus('loading');
    };
    const ready = () => {
      if (this._contentSrc === src) this._emitStatus('playing');
    };
    v.addEventListener('waiting', loading);
    v.addEventListener('seeking', loading);
    v.addEventListener('playing', ready);
    v.addEventListener('canplaythrough', ready);
    v.addEventListener('ended', () => {
      if (this._programSrc === src && this._programEndedCb) this._programEndedCb();
    });
    v.load();
    this._videoCache.set(src, v);
    return v;
  }

  // setProgramEndedCallback fires when the current program clip finishes, so the
  // producer's playlist can auto-advance.
  setProgramEndedCallback(cb) {
    this._programEndedCb = cb;
  }

  preload(urls) {
    for (const u of urls) {
      if (u && !this._videoCache.has(u)) this._videoFor(u);
    }
  }

  // ── Transport (targets the selected, or first, video source) ───────────────

  setSelectedSource(id) {
    if (this._sources.has(id) && this._sources.get(id).type === 'video') {
      this._selVideo = id;
      const sel = this._sources.get(id);
      this._contentSrc = sel.src || '';
    }
  }

  _videoEntry(id) {
    const e = this._sources.get(id || this._selVideo);
    return e && e.type === 'video' ? e : null;
  }

  contentInfo(id) {
    const v = this._programVideo || (this._videoEntry(id) && this._videoEntry(id).video);
    if (!v) return null;
    return { t: v.currentTime || 0, dur: Number.isFinite(v.duration) ? v.duration : 0, paused: v.paused };
  }

  // setTransport applies transport to the program bus (or a legacy selected video
  // source) immediately so the producer's monitor responds without waiting for
  // the broadcast round-trip; the broadcast re-syncs via _applyProgram.
  setTransport(o, id) {
    const v = this._programVideo || (this._videoEntry(id) && this._videoEntry(id).video);
    if (!v || !o) return;
    if (o.volume != null) v.volume = Math.max(0, Math.min(1, o.volume));
    if (o.muted != null) v.muted = !!o.muted;
    if (o.loop != null) v.loop = !!o.loop;
    if (o.seekTo != null && Number.isFinite(v.duration) && v.duration > 0) {
      try {
        v.currentTime = Math.max(0, Math.min(o.seekTo, v.duration - 0.05));
      } catch (_) {}
    }
    if (o.paused != null) {
      if (o.paused) v.pause();
      else
        v.play().catch(() => {
          v.muted = true;
          v.play().catch(() => {});
        });
    }
  }

  // ── Webcam registry ────────────────────────────────────────────────────────

  // setCamStreams receives the live webcam registry from webrtc-room.js — either
  // the full { videos, meta } object or a bare videos Map. When the stream set or
  // the stream→host identities change, re-apply the scene so webcam sources (and
  // v1/v2 synthesis) pick up the new streams / bindings.
  setCamStreams(registry) {
    const videos = registry && registry.videos ? registry.videos : registry;
    const meta = registry && registry.meta ? registry.meta : null;
    if (!videos) return;
    const videosChanged =
      videos.size !== this._camRegistry.size || [...videos.keys()].some((k) => !this._camRegistry.has(k));
    let metaSig = '';
    if (meta) {
      const parts = [];
      for (const [k, v] of meta) parts.push(k + '=' + ((v && v.userId) || ''));
      metaSig = parts.sort().join('|');
    }
    const metaChanged = metaSig !== this._camMetaSig;
    this._camRegistry = videos;
    if (meta) this._camMeta = meta;
    this._camMetaSig = metaSig;
    if ((videosChanged || metaChanged) && this._lastRawScene) this.applyScene(this._lastRawScene);
  }

  // ── Render loop ────────────────────────────────────────────────────────────

  _loop() {
    if (!this._running) return;
    pageFrame(() => this._loop());
    if (document.visibilityState === 'hidden') return;
    const va = this.camera.right;
    for (const e of this._sources.values()) {
      if (e.type === 'background' && e.mat.uniforms) {
        const epoch = e.epoch > 0 ? e.epoch : this._localEpoch;
        e.mat.uniforms.u_time.value = ((Date.now() - epoch) / 1000) * (e.speed || 0);
        if (e.mat.uniforms.u_seed) e.mat.uniforms.u_seed.value = e.seed;
        if (e.mat.uniforms.u_aspect) e.mat.uniforms.u_aspect.value = va;
      } else {
        if (e.backing) this._tween(e.backing);
        if (e.box && this._letterboxed(e.type)) this._fitTarget(e); // keep aspect-fit live
        this._tween(e.mesh);
      }
    }
    if (this._fadeMesh && this._fadeMesh.visible) {
      const t = this._fadeMs > 0 ? (Date.now() - this._fadeStart) / this._fadeMs : 1;
      if (t >= 1) {
        this._fadeMesh.visible = false;
        if (this._fadeMat) this._fadeMat.opacity = 0;
      } else if (this._fadeMat) {
        this._fadeMat.opacity = 1 - t;
      }
    }
    this.renderer.render(this.scene, this.camera);
  }

  // _tween eases a mesh toward userData.target for smooth size/position changes.
  _tween(mesh) {
    const t = mesh.userData.target;
    if (!t) return;
    const k = 0.18;
    mesh.position.x += (t.px - mesh.position.x) * k;
    mesh.position.y += (t.py - mesh.position.y) * k;
    mesh.scale.x += (t.sx - mesh.scale.x) * k;
    mesh.scale.y += (t.sy - mesh.scale.y) * k;
  }

  dispose() {
    this._running = false;
    window.removeEventListener('resize', this._onResize);
    this.renderer.dispose();
  }
}

