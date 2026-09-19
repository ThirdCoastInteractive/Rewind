import { SequencePlayback } from './lib/sequence-playback.js';
import { onPageCleanup, listen, pageFrame, pageTimeout, PageResizeObserver } from './lib/page-scope.js';

/* Stitch's editor client owns ephemeral selection/playhead state. Documents are
 * always replaced by canonical command responses or project SSE snapshots. */
const esc = (v) => String(v ?? '');
const fmt = (us, fps = 30) => { const frames = Math.max(0, Math.round(Number(us || 0) * fps / 1e6)); const perSecond = Math.max(1, Math.round(fps)); const totalSeconds = Math.floor(frames / perSecond); const ff = frames % perSecond; return `${String(Math.floor(totalSeconds / 3600)).padStart(2, '0')}:${String(Math.floor(totalSeconds / 60) % 60).padStart(2, '0')}:${String(totalSeconds % 60).padStart(2, '0')}:${String(ff).padStart(2, '0')}`; };
const id = (v) => CSS.escape(String(v));

export const ZOOM_MIN = 0.5;
export const ZOOM_MAX = 4000;
export const ZOOM_DEFAULT = 80;
export const ZOOM_SLIDER_MAX = 1000;
export const TIMELINE_GUTTER = 96;
const NICE_TICK_SECONDS = [1 / 60, 1 / 30, 1 / 15, 0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];

export function clampZoom(zoom) {
  const value = Number(zoom);
  if (!Number.isFinite(value)) return ZOOM_DEFAULT;
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value));
}

export function zoomToSlider(zoom) {
  const z = clampZoom(zoom);
  return Math.round(Math.log(z / ZOOM_MIN) / Math.log(ZOOM_MAX / ZOOM_MIN) * ZOOM_SLIDER_MAX);
}

export function sliderToZoom(slider) {
  const t = Math.max(0, Math.min(ZOOM_SLIDER_MAX, Number(slider) || 0)) / ZOOM_SLIDER_MAX;
  return clampZoom(ZOOM_MIN * Math.pow(ZOOM_MAX / ZOOM_MIN, t));
}

export function stepZoom(zoom, direction) {
  return clampZoom(zoom * (direction > 0 ? 1.25 : 0.8));
}

export function fitZoom(durationUS, viewportPx) {
  const seconds = Math.max(0.5, Number(durationUS || 0) / 1e6);
  const width = Math.max(64, Number(viewportPx || 0));
  return clampZoom(width / seconds);
}

export function timelineContentWidth(durationUS, zoom) {
  return Math.max(64, (Number(durationUS || 0) / 1e6) * clampZoom(zoom));
}

export function rulerStepUS(zoom, fps = 30) {
  const targetSeconds = 90 / Math.max(ZOOM_MIN, Number(zoom) || ZOOM_DEFAULT);
  const frame = 1 / Math.max(1, Number(fps) || 30);
  if (targetSeconds <= frame * 1.5) return Math.round(frame * 1e6);
  const found = NICE_TICK_SECONDS.find((step) => step >= targetSeconds);
  return Math.round((found || NICE_TICK_SECONDS[NICE_TICK_SECONDS.length - 1]) * 1e6);
}

export function rulerLabel(us, fps, stepUS) {
  const full = fmt(us, fps);
  if (stepUS < 1e6) return full.slice(6);
  if (stepUS < 60e6) return full.slice(3, 8);
  return full.slice(0, 8);
}

export function overlaySignature(overlays, playheadUS) {
  return (overlays || [])
    .filter((overlay) => overlay.visible !== false && playheadUS >= Number(overlay.start_us || 0) && playheadUS < Number(overlay.end_us || Number.MAX_SAFE_INTEGER))
    .map((overlay) => overlay.id)
    .join('|');
}

export function clipRelativeSeconds(segment, playheadUS) {
  const clipStartUS = Number(segment?.clip_start_us || segment?.source_in_us || 0);
  const sourceUS = Number(segment?.source_in_us || 0) + (Number(playheadUS) - Number(segment?.start_us || 0));
  return (sourceUS - clipStartUS) / 1e6;
}

export function activeMulticamCropId(segment, playheadUS) {
  const shots = Array.isArray(segment?.shots) ? segment.shots : [];
  if (!shots.length) return '';
  const t = clipRelativeSeconds(segment, playheadUS);
  const shot = shots.find((entry) => t >= Number(entry.start) && t < Number(entry.end));
  return shot?.crop_id || '';
}

export function layoutPreviewSignature(timeline, playheadUS) {
  const segment = (timeline || []).find((item) => playheadUS >= Number(item.start_us || 0) && playheadUS < Number(item.start_us || 0) + Number(item.duration_us || 0));
  if (!segment) return '';
  const shots = Array.isArray(segment.shots) ? segment.shots : [];
  const crops = Array.isArray(segment.crops) ? segment.crops : [];
  if (shots.length > 0 && crops.length > 0) {
    return `${segment.id}:multicam:${activeMulticamCropId(segment, playheadUS)}`;
  }
  const layout = segment.layout;
  if (!layout) return '';
  return `${segment.id}:${layout.mode}:${(layout.crops || []).length}`;
}

export function captionPreviewSignature(captions, resolved, playheadUS) {
  const cue = (captions || []).find((caption) => {
    const segment = (resolved || []).map((entry) => entry.segment || entry).find((item) => item.id === caption.segment_id);
    const inCue = playheadUS >= Number(caption.start_us || 0) && playheadUS < Number(caption.end_us || 0);
    const inSegment = !segment || (playheadUS >= Number(segment.start_us || 0) && playheadUS < Number(segment.start_us || 0) + Number(segment.duration_us || 0));
    return inCue && inSegment;
  });
  if (!cue) return '';
  const style = cue.style || {};
  const words = cue.alignment === 'valid' && style.word_highlight === true && Array.isArray(cue.words) && cue.words.length ? cue.words : null;
  const wordIndex = words ? words.findIndex((word) => playheadUS >= Number(word.start_us || 0) && playheadUS < Number(word.end_us || 0)) : -1;
  return `${cue.id}:${wordIndex}`;
}

export function titleCardSignature(resolved, playheadUS, usingSeq) {
  if (usingSeq) return 'seq';
  const segment = (resolved || []).map((entry) => entry.segment || entry).find((item) => item && item.type === 'title' && playheadUS >= Number(item.start_us || 0) && playheadUS < Number(item.start_us || 0) + Number(item.duration_us || 0));
  return segment?.id || '';
}

export function buildInsertSegmentOperation(source, projectDurationUS) {
  return { type: source.source_type === 'clip' ? 'clip' : source.source_type === 'stitch' ? 'stitch' : 'video', video_id: source.video_id || undefined, clip_id: source.clip_id || undefined, export_job_id: source.export_job_id || undefined, source_in_us: Number(source.source_in_us || 0), duration_us: Number(source.duration_us || 0), start_us: projectDurationUS };
}

export function buildTrimOperation(segment, sourceInUS, sourceOutUS) {
  const base = Number(segment.source_in_us || 0);
  const requestedIn = Number(sourceInUS);
  const requestedOut = Number(sourceOutUS);
  if (!Number.isFinite(requestedIn) || !Number.isFinite(requestedOut) || requestedOut <= requestedIn) {
    throw new Error('Trim range must be finite and positive.');
  }
  const start = Math.round(requestedIn - base);
  const end = Math.round(requestedOut - base);
  if (end <= start) throw new Error('Trim range must be at least one microsecond.');
  return { target_id: segment.id, start_us: start, end_us: end };
}

export function shouldDeferRemoteRevision(currentRevision, incomingRevision, pending, draft) {
  return incomingRevision > currentRevision && (pending > 0 || Boolean(draft));
}

export function findExistingGroup(document, members, kind) {
  const groups = kind === 'timing' ? (document?.timing_links || []) : (document?.position_groups || []);
  const selected = new Set(members || []);
  return groups.find((group) => selected.size > 0 && [...selected].every((id) => (group.members || []).includes(id)));
}

export function pointerOnScrollbar(el, event) {
  if (!el) return false;
  const r = el.getBoundingClientRect();
  const x = event.clientX - r.left;
  const y = event.clientY - r.top;
  return x < 0 || y < 0 || x >= el.clientWidth || y >= el.clientHeight;
}

export function queueSerialized(state, operation) {
  const previous = state.commandQueue || Promise.resolve();
  const run = previous.catch(() => {}).then(operation);
  state.commandQueue = run;
  return run.finally(() => {
    if (state.commandQueue === run) state.commandQueue = null;
  });
}

function parseLegacy(raw) {
  if (!raw) return {};
  if (typeof raw === 'object' && !Array.isArray(raw)) return raw;
  try { return JSON.parse(raw); } catch { return {}; }
}

export function titlePayloadForSegment(segment) {
  const legacy = parseLegacy(segment?.legacy);
  const gap = segment?.type === 'gap';
  return {
    text: segment?.text || legacy.text || legacy.title || '',
    subtitle: legacy.subtitle || '',
    bg_color: legacy.bg_color || legacy.background || '#000000',
    text_color: legacy.text_color || legacy.color || '#ffffff',
    font: legacy.font || 'UnifrakturCook',
    font_size: legacy.font_size || 72,
    position: legacy.position || 'center',
    ...(gap ? { text: '' } : {}),
  };
}

export function segmentStreamUrl(segment) {
  if (segment?.video_id) return `/api/videos/${encodeURIComponent(segment.video_id)}/stream`;
  if (segment?.export_job_id) return `/api/stitch/${encodeURIComponent(segment.export_job_id)}/stream`;
  return '';
}

export function isTitleSegment(segment) {
  return segment?.type === 'title' || segment?.type === 'gap';
}

export function buildPlaybackClips(timeline) {
  return (timeline || []).map((segment) => {
    const duration = Number(segment.duration_us || 0) / 1e6;
    const src = segmentStreamUrl(segment);
    const title = isTitleSegment(segment) || !src;
    const tr = segment.transition;
    const trDur = tr ? Number(tr.duration_us || tr.duration || 0) / (tr.duration_us ? 1e6 : 1) : 0;
    return {
      kind: title ? 'title' : 'video',
      src: title ? '' : src,
      startTime: title ? 0 : Number(segment.source_in_us || 0) / 1e6,
      endTime: title ? duration : (Number(segment.source_in_us || 0) + Number(segment.duration_us || 0)) / 1e6,
      duration,
      label: titlePayloadForSegment(segment).text || segment.id || '',
      title: title ? titlePayloadForSegment(segment) : null,
      transition: tr && trDur > 0 ? { type: tr.kind || tr.type || 'fade', duration: trDur } : null,
    };
  });
}

// drawTeaserLayoutFrame renders the same normalized composition used by the
// editor preview, including clipping each stacked destination panel.
export function drawTeaserLayoutFrame(ctx, video, layout, width, height) {
  const fit = (crop, dx, dy, dw, dh, cover) => {
    const sx = crop ? video.videoWidth * Number(crop.x || 0) : 0; const sy = crop ? video.videoHeight * Number(crop.y || 0) : 0;
    const sw = crop ? video.videoWidth * Number(crop.width || 1) : video.videoWidth; const sh = crop ? video.videoHeight * Number(crop.height || 1) : video.videoHeight;
    const scale = cover ? Math.max(dw / sw, dh / sh) : Math.min(dw / sw, dh / sh); const rw = sw * scale; const rh = sh * scale;
    ctx.save(); ctx.beginPath(); ctx.rect(dx, dy, dw, dh); ctx.clip(); ctx.drawImage(video, sx, sy, sw, sh, dx + (dw - rw) / 2, dy + (dh - rh) / 2, rw, rh); ctx.restore();
  };
  ctx.clearRect(0, 0, width, height); const crops = layout?.crops || [];
  if (layout?.mode === 'two_speakers' && crops.length >= 2) { fit(crops[0], 0, 0, width, height / 2, true); fit(crops[1], 0, height / 2, width, height - height / 2, true); }
  else if (layout?.mode === 'preserve_scene') { ctx.save(); ctx.filter = 'blur(24px)'; fit(null, 0, 0, width, height, true); ctx.restore(); fit(crops[0], 0, 0, width, height, false); }
  else fit(crops[0], 0, 0, width, height, true);
}

export class StitchWorkspace {
  constructor(root) {
    this.root = root; this.projectID = root.dataset.projectId; this.readOnly = root.dataset.readonly === 'true'; this.doc = null; this.revision = 0;
    this.description = ''; this.tags = [];
    this.selected = new Set(); this.playhead = 0; this.zoom = ZOOM_DEFAULT; this.tab = 'edit'; this.seq = null; this.playbackKey = ''; this.pending = 0;
    this.waveformCache = new Map(); this.waveformInflight = new Map();
    this.fonts = ['Tomorrow', 'UnifrakturCook'];
    this.setState('Loading…');
    this.bind(); this.bindOverlayTools(); this.bindTimelineNav(); this.loadFonts(); this.load(); this.loadSources(); this.history(); this.loadExports();
    this.restoreAssist = this.mountAssist();
    this.assistNavigationCleanup = this.bindNavbarAssist();
    this.cleanup = onPageCleanup(() => {
      this.destroy();
    });
  }
  bind() {
    this.root.addEventListener('click', (e) => { const action = e.target.closest('[data-action]')?.dataset.action; if (action === 'reload-latest') return this.reloadLatest(); if (action === 'apply-draft') return this.applyDraft(); const source = e.target.closest('[data-source]'); if (source) { const value = JSON.parse(source.dataset.source); this.command('insert_segment', { segment: buildInsertSegmentOperation({ ...value, duration_us: Number(value.duration_us || value.duration || 0) }, this.duration()) }); return; } if (action) this.action(action, e); const tab = e.target.closest('[data-tab]')?.dataset.tab; if (tab) this.setTab(tab); const item = e.target.closest('[data-select-id]'); if (item) this.select(item.dataset.selectId, e.shiftKey); const transport = e.target.closest('[data-transport]')?.dataset.transport; if (transport) this.transport(transport); });
    this.root.addEventListener('change', (e) => { if (e.target.matches('[data-time-field]')) { const item = this.findSelected(); if (!item || !(this.doc.segments || []).some((segment) => segment.id === item.id)) return; const value = Math.round(Number(e.target.value) * 1e6); const field = e.target.dataset.timeField; if (field === 'project_start_us') { this.command('move_segment', { delta_us: value - Number(item.start_us || 0) }); return; } const sourceIn = field === 'source_in_us' ? value : Number(item.source_in_us || 0); const sourceOut = field === 'source_out_us' ? value : Number(item.source_in_us || 0) + Number(item.duration_us || 0); this.command('trim_segment', buildTrimOperation(item, sourceIn, sourceOut)); } });
    this.root.addEventListener('input', (e) => { if (e.target.matches('[data-zoom]')) { this.setZoom(sliderToZoom(e.target.value), { fromSlider: true }); } if (e.target.matches('[data-source-search]')) { clearTimeout(this.sourceSearchTimer); this.sourceSearchTimer = setTimeout(() => this.loadSources(), 250); } if (e.target.matches('[data-caption-text]')) { const cue = this.findSelected(); if (cue) { cue.text = e.target.value; cue.words = []; this.draft = cue; this.renderCaptionPreview(); clearTimeout(this.captionTimer); this.captionTimer = setTimeout(() => this.command('upsert_caption', { caption: cue }), 500); } } });
    this.root.querySelector('[data-timeline]').addEventListener('pointerdown', (e) => this.seekAt(e));
    this.root.querySelector('[data-overlay-svg]').addEventListener('pointerdown', (e) => { const target = e.target.closest('[data-select-id]'); if (!target) return; const overlay = (this.doc?.overlays || []).find(o => o.id === target.dataset.selectId); if (!overlay || overlay.locked) { this.error('Overlay is locked.'); return; } this.select(overlay.id, e.shiftKey); this.overlayDrag = { id: overlay.id, x: e.clientX, y: e.clientY, originX: Number(overlay.x || 0), originY: Number(overlay.y || 0) }; target.setPointerCapture?.(e.pointerId); });
    this.root.querySelector('[data-overlay-svg]').addEventListener('pointermove', (e) => { if (!this.overlayDrag) return; const overlay = this.doc.overlays.find(o => o.id === this.overlayDrag.id); if (!overlay) return; const dx = (e.clientX - this.overlayDrag.x) * Number(this.doc?.width || 1920) / this.root.querySelector('[data-overlay-svg]').clientWidth; const dy = (e.clientY - this.overlayDrag.y) * Number(this.doc?.height || 1080) / this.root.querySelector('[data-overlay-svg]').clientHeight; overlay.x += dx; overlay.y += dy; this.overlayDrag.x = e.clientX; this.overlayDrag.y = e.clientY; this.renderOverlay(); this.renderTitleCard(); });
    this.root.querySelector('[data-overlay-svg]').addEventListener('pointerup', () => { if (!this.overlayDrag) return; const drag = this.overlayDrag; const o = this.doc.overlays.find(x => x.id === drag.id); this.overlayDrag = null; if (o) this.command('move_overlay', { target_id: drag.id, delta_x: Number(o.x || 0) - drag.originX, delta_y: Number(o.y || 0) - drag.originY }); });
    this.root.querySelector('[data-export-form]').addEventListener('submit', (e) => { e.preventDefault(); this.exportJob(new FormData(e.target)); });
    this.root.addEventListener('change', (e) => {
      if (e.target.matches('[data-caption-mode], [data-font-select="caption"]')) this.persistCaptionSettings(e.target);
    });
  }

  bindTimelineNav() {
    const timeline = this.root.querySelector('[data-timeline]');
    if (!timeline) return;
    listen(timeline, 'wheel', (event) => {
      if (!event.ctrlKey && !event.metaKey && !event.altKey) return;
      event.preventDefault();
      const rect = timeline.getBoundingClientRect();
      this.setZoom(this.zoom * (event.deltaY < 0 ? 1.25 : 0.8), { originX: event.clientX - rect.left });
    }, { passive: false });
    listen(timeline, 'scroll', () => this.scheduleRuler(), { passive: true });
    listen(this.root, 'keydown', (event) => {
      if (event.target.closest('input, textarea, select, [contenteditable]')) return;
      if (event.key === '=' || event.key === '+') { event.preventDefault(); this.setZoom(stepZoom(this.zoom, 1)); }
      else if (event.key === '-' || event.key === '_') { event.preventDefault(); this.setZoom(stepZoom(this.zoom, -1)); }
      else if (event.key === '0' && !event.ctrlKey && !event.metaKey) { event.preventDefault(); this.fitTimeline(); }
    });
    this.timelineResize = new PageResizeObserver(() => {
      this._gutter = 0;
      if (this.doc) this.renderTimeline();
    });
    this.timelineResize.observe(timeline);
    this.syncZoomSlider();
  }

  viewportWidth() {
    const timeline = this.root.querySelector('[data-timeline]');
    return Math.max(64, (timeline?.clientWidth || 320) - this.timelineGutter());
  }

  syncZoomSlider() {
    const slider = this.root.querySelector('[data-zoom]');
    if (!slider) return;
    slider.value = String(zoomToSlider(this.zoom));
    slider.title = `${this.zoom >= 10 ? Math.round(this.zoom) : this.zoom.toFixed(1)} px/s`;
  }

  setZoom(next, { originX, fromSlider } = {}) {
    const timeline = this.root.querySelector('[data-timeline]');
    const gutter = this.timelineGutter();
    const zoom = clampZoom(next);
    const origin = Number.isFinite(originX) ? originX : (timeline ? timeline.clientWidth / 2 : gutter);
    const timeS = timeline ? Math.max(0, (timeline.scrollLeft + origin - gutter) / Math.max(ZOOM_MIN, this.zoom)) : 0;
    const changed = Math.abs(zoom - this.zoom) > 0.001;
    this.zoom = zoom;
    if (!fromSlider) this.syncZoomSlider();
    if (changed) this.renderTimeline();
    if (timeline) {
      timeline.scrollLeft = Math.max(0, timeS * this.zoom - origin + gutter);
      this.renderRuler();
    }
    this.updatePlayhead();
  }

  fitTimeline() {
    const timeline = this.root.querySelector('[data-timeline]');
    this.setZoom(fitZoom(this.duration(), this.viewportWidth()), { originX: this.timelineGutter() });
    if (timeline) {
      timeline.scrollLeft = 0;
      this.renderRuler();
    }
  }

  fitSelection() {
    const item = this.findSelected();
    if (!item) return this.fitTimeline();
    const start = Number(item.start_us || 0);
    const end = Number(item.end_us || start + Number(item.duration_us || 0));
    const timeline = this.root.querySelector('[data-timeline]');
    this.setZoom(fitZoom(Math.max(1, end - start), this.viewportWidth()));
    if (timeline) {
      timeline.scrollLeft = Math.max(0, start / 1e6 * this.zoom);
      this.renderRuler();
    }
  }

  scheduleRuler() {
    if (this.rulerRAF) return;
    this.rulerRAF = pageFrame(() => {
      this.rulerRAF = null;
      this.renderRuler();
    });
  }

  renderRuler() {
    const ruler = this.root.querySelector('[data-ruler]');
    const timeline = this.root.querySelector('[data-timeline]');
    if (!ruler || !timeline || !this.doc) return;
    const total = this.duration();
    const gutter = this.timelineGutter();
    const zoom = this.zoom;
    const content = Math.ceil(timelineContentWidth(total, zoom));
    const step = rulerStepUS(zoom, this.doc.fps);
    timeline.style.setProperty('--timeline-content', `${content}px`);
    ruler.style.setProperty('--tick', `${Math.max(8, step / 1e6 * zoom)}px`);
    const startUS = Math.max(0, (timeline.scrollLeft - gutter) / zoom * 1e6);
    const endUS = Math.min(total, (timeline.scrollLeft + timeline.clientWidth) / zoom * 1e6);
    const first = Math.floor(startUS / step) * step;
    const ticks = [];
    for (let t = first; t <= endUS + step && t <= total + step; t += step) {
      if (t < 0) continue;
      ticks.push(t);
      if (ticks.length > 400) break;
    }
    ruler.replaceChildren(...ticks.map((t) => {
      const tick = document.createElement('span');
      tick.style.left = `${gutter + t / 1e6 * zoom}px`;
      tick.textContent = rulerLabel(t, this.doc.fps, step);
      return tick;
    }));
  }

  async loadSources() {
    const search = this.root.querySelector('[data-source-search]');
    const query = search ? `?q=${encodeURIComponent(search.value || '')}&limit=30` : '?limit=30';
    try {
      const response = await fetch(`/api/stitch/sources/json${query}`, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`Source browser failed (${response.status})`);
      const data = await response.json();
      const rows = data.sources || data.rows || data.results || [];
      const out = this.root.querySelector('[data-source-list]');
      out.replaceChildren(...rows.map((source) => {
        const item = document.createElement('button');
        item.type = 'button'; item.className = 'stitch-list-item'; item.dataset.sourceId = source.id || source.video_id;
        item.dataset.source = JSON.stringify(source);
        const main = document.createElement('div'); main.className = 'stitch-list-main';
        const title = document.createElement('div'); title.className = 'stitch-list-title';
        title.textContent = source.title || source.name || source.video_id || 'Source';
        const meta = document.createElement('div'); meta.className = 'stitch-list-meta';
        meta.textContent = fmt(Number(source.duration_us || source.duration || 0), this.doc?.fps);
        main.append(title, meta); item.append(main);
        return item;
      }));
    } catch (error) { this.error(error.message); }
  }
  async load() { try { const r = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/document`, { headers: { Accept: 'application/json' } }); if (!r.ok) throw new Error(`Document request failed (${r.status})`); const data = await r.json(); this.revision = Number(data.revision || 0); this.doc = data.document || data; this.description = typeof data.description === 'string' ? data.description : ''; this.tags = Array.isArray(data.tags) ? data.tags.map((tag) => String(tag).trim()).filter(Boolean) : []; this.resolved = Array.isArray(data.resolved) ? data.resolved : []; this.resolvedCaptions = Array.isArray(data.resolved_captions) ? data.resolved_captions : []; this.setState('Live'); this.applyExportDefaults(); this.render(); this.subscribe(); this.loadExports(); } catch (e) { this.setState('Unavailable'); this.error(e.message); } }
  subscribe() {
    if (!window.EventSource) return;
    const url = `/api/stitch/projects/${encodeURIComponent(this.projectID)}/events`;
    this.events = window.RewindPage?.eventSource ? window.RewindPage.eventSource(url) : new EventSource(url);
    this.events.addEventListener('project', (e) => { try { const d = JSON.parse(e.data); const revision = Number(d.revision || 0); if (typeof d.description === 'string') this.description = d.description; if (Array.isArray(d.tags)) this.tags = d.tags.map((tag) => String(tag).trim()).filter(Boolean); this.fillYouTubeFields(); if (revision <= this.revision) return; if (this.pending || this.draft || this.conflictDraft) { this.remoteRevision = revision; this.setState('Remote update deferred; local draft preserved'); return; } this.revision = revision; this.doc = d.document || d; this.resolved = Array.isArray(d.resolved) ? d.resolved : this.resolved; this.resolvedCaptions = Array.isArray(d.resolved_captions) ? d.resolved_captions : this.resolvedCaptions; this.render(); this.setState('Live'); } catch (_) {} });
    this.events.onopen = () => { if (this.doc) this.setState('Live'); };
    this.events.onerror = () => { if (this.doc) this.setState('Live'); };
  }
  setState(s) { const el = this.root.querySelector('[data-sync-state]'); if (el) el.textContent = s; }
  error(s) { const el = this.root.querySelector('[data-error]'); if (el) el.textContent = s; }
  clearError() { this.error(''); }
  youtubeFieldsFocused() {
    const active = this.root.ownerDocument.activeElement;
    const desc = this.root.querySelector('[data-youtube-description]');
    const tags = this.root.querySelector('[data-youtube-tags]');
    return active === desc || active === tags;
  }
  parseYouTubeTags(value) {
    return String(value || '').split(/[,\n]/).map((tag) => tag.trim()).filter(Boolean);
  }
  fillYouTubeFields({ force = false } = {}) {
    const title = this.root.querySelector('[data-youtube-title]');
    const desc = this.root.querySelector('[data-youtube-description]');
    const tags = this.root.querySelector('[data-youtube-tags]');
    if (title) title.value = this.doc?.title || '';
    if (!force && this.youtubeFieldsFocused()) return;
    if (desc) desc.value = this.description || '';
    if (tags) tags.value = (Array.isArray(this.tags) ? this.tags : []).join(', ');
  }
  setYouTubeStatus(message) {
    const status = this.root.querySelector('[data-youtube-status]');
    if (status) status.textContent = message || '';
  }
  async copyYouTube(kind) {
    const field = kind === 'title'
      ? this.root.querySelector('[data-youtube-title]')
      : kind === 'description'
        ? this.root.querySelector('[data-youtube-description]')
        : this.root.querySelector('[data-youtube-tags]');
    const button = this.root.querySelector(`[data-action="youtube-copy-${kind}"]`);
    if (!field) return;
    const text = kind === 'tags' ? this.parseYouTubeTags(field.value).join(', ') : String(field.value || '');
    const label = button?.textContent || 'Copy';
    try {
      await navigator.clipboard.writeText(text);
      if (button) {
        button.textContent = 'Copied';
        clearTimeout(this._youtubeCopyTimer);
        this._youtubeCopyTimer = setTimeout(() => { button.textContent = label; }, 1200);
      }
    } catch (_) {
      if (typeof field.select === 'function') field.select();
    }
  }
  async saveYouTube() {
    if (this.readOnly) return;
    const desc = this.root.querySelector('[data-youtube-description]');
    const tags = this.root.querySelector('[data-youtube-tags]');
    const description = desc ? desc.value : (this.description || '');
    const nextTags = this.parseYouTubeTags(tags ? tags.value : '');
    this.setYouTubeStatus('Saving…');
    try {
      const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/youtube`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ description, tags: nextTags }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || data.message || `Save failed (${response.status})`);
      this.description = typeof data.description === 'string' ? data.description : description;
      this.tags = Array.isArray(data.tags) ? data.tags.map((tag) => String(tag).trim()).filter(Boolean) : nextTags;
      this.fillYouTubeFields({ force: !this.youtubeFieldsFocused() });
      this.setYouTubeStatus('Saved');
    } catch (error) {
      this.setYouTubeStatus(error.message || 'Save failed');
    }
  }
  render() { if (!this.doc) return; this.root.querySelector('[data-project-label]').textContent = this.doc.title || this.projectID.slice(0, 8); this.root.querySelector('[data-revision]').textContent = `rev ${this.revision}`; this.root.querySelector('[data-action="undo"]').disabled = false; this.root.querySelector('[data-action="redo"]').disabled = false; const preview = this.root.querySelector('[data-preview]'); if (preview && this.doc.width && this.doc.height) preview.style.aspectRatio = `${this.doc.width} / ${this.doc.height}`; this.renderSegments(); this.renderCaptions(); this.renderLayers(); this.renderInspector(); if (this.findSelected() && (this.doc.overlays || []).some((item) => item.id === this.findSelected().id)) this.renderOverlayFields(); this.renderTimeline(); this.renderTime(); this.refreshPlayback(); this.fillYouTubeFields(); }
  renderSegments() { const out = this.root.querySelector('[data-segment-list]'); const list = this.doc.segments || []; out.replaceChildren(...list.map((s, n) => { const el = document.createElement('div'); el.className = `stitch-list-item${this.selected.has(s.id) ? ' is-selected' : ''}`; el.dataset.selectId = s.id; const index = document.createElement('span'); index.className = 'stitch-list-index'; index.textContent = n + 1; const main = document.createElement('div'); main.className = 'stitch-list-main'; const title = document.createElement('div'); title.className = 'stitch-list-title'; title.textContent = s.title || titlePayloadForSegment(s).text || s.video_id || s.clip_id || 'Untitled segment'; const meta = document.createElement('div'); meta.className = 'stitch-list-meta'; meta.textContent = `${fmt(s.duration_us, this.doc.fps)} · ${s.transition?.kind || s.transition?.type || 'cut'}`; main.append(title, meta); el.append(index, main); return el; })); }
  renderCaptions() {
    const out = this.root.querySelector('[data-caption-list]');
    const list = this.doc.captions || [];
    if (!list.length) {
      const empty = document.createElement('p');
      empty.className = 'stitch-muted';
      empty.textContent = 'Select a sequence clip, then Add or Import.';
      out.replaceChildren(empty);
      return;
    }
    out.replaceChildren(...list.map((c) => {
      const el = document.createElement('div');
      el.className = `stitch-list-item${this.selected.has(c.id) ? ' is-selected' : ''}`;
      el.dataset.selectId = c.id;
      const main = document.createElement('div');
      main.className = 'stitch-list-main';
      const text = document.createElement('div');
      text.className = 'stitch-list-title';
      text.textContent = c.text || '(empty caption)';
      const meta = document.createElement('div');
      meta.className = 'stitch-list-meta';
      meta.textContent = `${c.language || 'und'} · ${fmt(c.start_us, this.doc.fps)}`;
      main.append(text, meta);
      el.append(main);
      return el;
    }));
  }
  renderLayers() { const out = this.root.querySelector('[data-layer-list]'); const list = this.doc.overlays || []; out.replaceChildren(...list.map(o => { const el = document.createElement('div'); el.className = `stitch-list-item${this.selected.has(o.id) ? ' is-selected' : ''}`; el.dataset.selectId = o.id; const visibility = document.createElement('span'); visibility.setAttribute('aria-hidden', 'true'); visibility.textContent = o.visible === false ? '◌' : '●'; const main = document.createElement('div'); main.className = 'stitch-list-main'; const title = document.createElement('div'); title.className = 'stitch-list-title'; title.textContent = o.text || o.kind || 'Overlay'; const meta = document.createElement('div'); meta.className = 'stitch-list-meta'; meta.textContent = `${o.locked ? 'locked · ' : ''}${fmt(o.start_us, this.doc.fps)} → ${fmt(o.end_us, this.doc.fps)}`; main.append(title, meta); el.append(visibility, main); return el; })); }
  findSelected() { const key = [...this.selected][0]; return [...(this.doc?.segments || []), ...(this.doc?.captions || []), ...(this.doc?.overlays || [])].find(x => x.id === key); }
  renderInspectorBase() { const out = this.root.querySelector('[data-inspector-content]'); const chosen = [...this.selected].map(k => [...(this.doc.segments || []), ...(this.doc.captions || []), ...(this.doc.overlays || [])].find(x => x.id === k)).filter(Boolean); this.root.querySelector('[data-selection-count]').textContent = `${chosen.length} selected`; out.replaceChildren(); if (!chosen.length) { const p = document.createElement('p'); p.className = 'stitch-muted'; p.textContent = 'Select a segment, caption, or layer to inspect it.'; out.append(p); return; } const x = chosen[0]; const p = document.createElement('p'); p.className = 'stitch-muted'; p.textContent = `${x.type || x.kind || 'item'} · ${x.id}`; out.append(p); ['start_us','end_us'].forEach((field) => { const wrap = document.createElement('label'); wrap.className = 'stitch-field'; wrap.textContent = field === 'start_us' ? 'Start (s)' : 'End (s)'; const input = document.createElement('input'); input.type = 'number'; input.step = '0.001'; input.dataset.timeField = field === 'start_us' ? 'start' : 'end'; input.value = (Number(x[field] || 0) / 1e6).toFixed(3); wrap.append(input); out.append(wrap); }); const actions = document.createElement('div'); actions.className = 'stitch-inspector-actions'; [['trim-start','Trim start'],['trim-end','Trim end'],['split','Split at playhead'],['duplicate','Duplicate'],['remove','Remove'],['nudge-left','Nudge ←'],['nudge-right','Nudge →'],['link-timing','Link timing'],['group-position','Group position']].forEach(([a,t]) => { const b = document.createElement('button'); b.dataset.action = a; b.textContent = t; actions.append(b); }); out.append(actions); }
  renderTimeline() {
    if (!this.doc) return;
    this.renderRuler();
    const tracks = this.root.querySelector('[data-track-list]');
    tracks.replaceChildren();
    [['VIDEO', this.doc.segments || [], 'segment'], ['CAPTIONS', this.doc.captions || [], 'caption'], ['OVERLAYS', this.doc.overlays || [], 'overlay']].forEach(([name, items, cls]) => {
      const row = document.createElement('div');
      row.className = 'timeline-track';
      const label = document.createElement('div');
      label.className = 'timeline-track-label';
      label.textContent = name;
      const lane = document.createElement('div');
      lane.className = 'timeline-track-lane';
      items.forEach((item) => {
        const clip = document.createElement('div');
        clip.className = `timeline-clip ${cls}${this.selected.has(item.id) ? ' is-selected' : ''}`;
        clip.dataset.selectId = item.id;
        const start = Number(item.start_us || 0);
        const end = Number(item.end_us || start + Number(item.duration_us || 0));
        clip.style.left = `${start / 1e6 * this.zoom}px`;
        clip.style.width = `${Math.max(2, (end - start) / 1e6 * this.zoom)}px`;
        clip.textContent = item.text || item.title || titlePayloadForSegment(item).text || item.video_id || item.kind || item.type || 'item';
        if (cls === 'segment' && item.video_id) {
          const thumbnail = document.createElement('img');
          thumbnail.src = `/api/videos/${encodeURIComponent(item.video_id)}/thumbnail`;
          thumbnail.alt = '';
          thumbnail.className = 'timeline-thumbnail';
          clip.prepend(thumbnail);
          const waveform = document.createElement('canvas');
          waveform.className = 'timeline-waveform';
          waveform.width = 240; waveform.height = 24;
          clip.append(waveform);
          this.paintWaveform(waveform, item.video_id);
        }
        if (cls === 'segment') {
          [['start', 'left'], ['end', 'right']].forEach(([edge, side]) => {
            const handle = document.createElement('button');
            handle.type = 'button';
            handle.className = `timeline-trim-handle ${side}`;
            handle.dataset.trimHandle = edge;
            handle.dataset.trimId = item.id;
            handle.setAttribute('aria-label', `${edge} trim handle`);
            handle.addEventListener('pointerdown', (event) => this.beginTrim(event, item, edge));
            clip.append(handle);
          });
        }
        lane.append(clip);
      });
      row.append(label, lane);
      tracks.append(row);
    });
  }

  paintWaveform(canvas, videoId) {
    const draw = (values) => {
      if (!canvas.isConnected || !values?.length) return;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.strokeStyle = '#57d5b2';
      ctx.beginPath();
      for (let i = 0; i < canvas.width; i += 1) {
        const value = Math.abs(values[Math.floor(i * values.length / canvas.width)] || 0) / 32768;
        const y = value * canvas.height / 2;
        ctx.moveTo(i, canvas.height / 2 - y);
        ctx.lineTo(i, canvas.height / 2 + y);
      }
      ctx.stroke();
    };
    const cached = this.waveformCache.get(videoId);
    if (cached) { draw(cached); return; }
    if (this.waveformInflight.has(videoId)) {
      this.waveformInflight.get(videoId).then(draw).catch(() => {});
      return;
    }
    const pending = fetch(`/api/videos/${encodeURIComponent(videoId)}/waveform/peaks.i16`)
      .then((response) => response.arrayBuffer())
      .then((buffer) => {
        const values = new Int16Array(buffer);
        this.waveformCache.set(videoId, values);
        this.waveformInflight.delete(videoId);
        return values;
      });
    this.waveformInflight.set(videoId, pending);
    pending.then(draw).catch(() => { this.waveformInflight.delete(videoId); });
  }

  beginTrim(event, segment, edge) {
    event.preventDefault();
    event.stopPropagation();
    this.select(segment.id, false);
    this.trimGesture = { id: segment.id, edge, startX: event.clientX, original: { source_in_us: Number(segment.source_in_us || 0), duration_us: Number(segment.duration_us || 0), start_us: Number(segment.start_us || 0) }, current: segment };
    const move = (moveEvent) => this.updateTrim(moveEvent);
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      this.finishTrim();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up, { once: true });
    this.trimGesture.listeners = { move, up };
  }

  updateTrim(event) {
    const gesture = this.trimGesture;
    if (!gesture) return;
    const fps = Number(this.doc?.fps || 30);
    const frame = Math.max(1, Math.round(1e6 / fps));
    const delta = Math.round((event.clientX - gesture.startX) / this.zoom * 1e6 / frame) * frame;
    const original = gesture.original;
    const neighbors = (this.doc.segments || []).filter((item) => item.id !== gesture.id).sort((a, b) => Number(a.start_us || 0) - Number(b.start_us || 0));
    const segment = gesture.current;
    if (gesture.edge === 'start') {
      const maxStart = original.start_us + original.duration_us - frame;
      const neighbor = neighbors.filter((item) => Number(item.start_us || 0) < original.start_us).at(-1);
      const minStart = neighbor ? Number(neighbor.start_us || 0) + Number(neighbor.duration_us || 0) : 0;
      // A trim gesture may restore earlier source/project material. Keep the
      // resulting project start and source in non-negative, while allowing a
      // negative operation offset relative to the original source in.
      const minStartFromSource = original.start_us - original.source_in_us;
      const projectStart = Math.max(minStart, 0, minStartFromSource, Math.min(maxStart, original.start_us + delta));
      segment.start_us = projectStart;
      segment.source_in_us = original.source_in_us + (projectStart - original.start_us);
      segment.duration_us = original.duration_us - (projectStart - original.start_us);
    } else {
      const minEnd = original.start_us + frame;
      const neighbor = neighbors.find((item) => Number(item.start_us || 0) > original.start_us);
      const maxEnd = neighbor ? Number(neighbor.start_us || 0) : original.start_us + original.duration_us + delta;
      const end = Math.max(minEnd, Math.min(maxEnd, original.start_us + original.duration_us + delta));
      segment.duration_us = end - original.start_us;
    }
    this.playhead = Math.max(segment.start_us, Math.min(this.playhead, segment.start_us + segment.duration_us));
    this.renderTimeline();
    this.updatePlayhead();
  }

  finishTrim() {
    const gesture = this.trimGesture;
    if (!gesture) return;
    this.trimGesture = null;
    const segment = gesture.current;
    const relativeStart = Number(segment.source_in_us || 0) - gesture.original.source_in_us;
    this.command('trim_segment', { target_id: gesture.id, start_us: relativeStart, end_us: Math.max(relativeStart + 1, relativeStart + Number(segment.duration_us || 0)) });
  }
  renderTime() {
    this.updatePlayhead();
    this._overlaySig = null;
    this._captionSig = null;
    this._titleSig = null;
    this._layoutSig = null;
    this.syncPlayheadChrome();
  }

  updatePlayhead() {
    const total = this.duration();
    const clock = fmt(this.playhead, this.doc?.fps);
    const text = `${clock} / ${fmt(total, this.doc?.fps)}`;
    if (this._timeText !== text) {
      this._timeText = text;
      const transport = this.root.querySelector('[data-transport-time]');
      const timecode = this.root.querySelector('[data-timecode]');
      const durationEl = this.root.querySelector('[data-duration]');
      if (transport) transport.textContent = text;
      if (timecode) timecode.textContent = clock;
      if (durationEl) durationEl.textContent = `Duration ${fmt(total, this.doc?.fps)}`;
    }
    const playhead = this.root.querySelector('[data-playhead]');
    if (playhead) playhead.style.left = `${this.timelineGutter() + this.playhead / 1e6 * this.zoom}px`;
    if (this.seq && !this.seq.paused) this.followPlayhead();
  }

  followPlayhead() {
    const timeline = this.root.querySelector('[data-timeline]');
    if (!timeline) return;
    const gutter = this.timelineGutter();
    const x = gutter + this.playhead / 1e6 * this.zoom;
    const left = timeline.scrollLeft;
    const right = left + timeline.clientWidth;
    const margin = 64;
    if (x > right - margin) timeline.scrollLeft = x - timeline.clientWidth + margin + 120;
    else if (x < left + gutter + margin) timeline.scrollLeft = Math.max(0, x - gutter - margin);
  }

  schedulePlayheadChrome() {
    if (this.liveChromeRAF) return;
    this.liveChromeRAF = pageFrame(() => {
      this.liveChromeRAF = null;
      this.syncPlayheadChrome();
    });
  }

  syncPlayheadChrome() {
    const resolved = (this.resolved || []).map((entry) => entry.segment || entry).filter(Boolean);
    const timeline = resolved.length ? resolved : (this.doc?.segments || []);
    const overlaySig = overlaySignature(this.doc?.overlays, this.playhead);
    const captionSig = captionPreviewSignature(this.doc?.captions, this.resolved, this.playhead);
    const titleSig = titleCardSignature(this.resolved, this.playhead, Boolean(this.seq));
    const layoutSig = layoutPreviewSignature(timeline, this.playhead);
    if (overlaySig !== this._overlaySig) { this._overlaySig = overlaySig; this.renderOverlay(); }
    if (captionSig !== this._captionSig) { this._captionSig = captionSig; this.renderCaptionPreview(); }
    if (titleSig !== this._titleSig) { this._titleSig = titleSig; this.renderTitleCard(); }
    if (layoutSig !== this._layoutSig) { this._layoutSig = layoutSig; this.renderTeaserLayoutPreview(); this.syncMulticamCameraButtons(); }
    if (!this.seq || this.seq.paused) this.scheduleEditorContext();
  }

  scheduleEditorContext() {
    if (this.contextTimer) return;
    this.contextTimer = pageTimeout(() => {
      this.contextTimer = null;
      this.publishEditorContext();
    }, 250);
  }
  duration() {
    const resolved = this.resolved || [];
    if (resolved.length) return resolved.reduce((n, entry) => {
      const segment = entry.segment || entry;
      const end = Number(entry.end_us ?? (Number(segment.start_us || 0) + Number(segment.duration_us || 0)));
      return Math.max(n, end);
    }, 0);
    return (this.doc?.segments || []).reduce((n, s) => Math.max(n, Number(s.start_us || 0) + Number(s.duration_us || 0)), 0);
  }
  select(value, additive) { if (!additive) this.selected.clear(); this.selected.has(value) ? this.selected.delete(value) : this.selected.add(value); this.render(); this.publishEditorContext(); }
  setTab(tab) {
    this.tab = tab;
    this.root.querySelectorAll('[data-tab]').forEach((x) => x.classList.toggle('is-active', x.dataset.tab === tab));
    this.root.querySelectorAll('[data-panel]').forEach((x) => {
      if (x.dataset.panel === 'edit') x.hidden = tab === 'youtube';
      else x.hidden = x.dataset.panel !== tab;
    });
    this.root.querySelector('[data-list="segments"]').hidden = tab !== 'edit';
    this.syncOverlayTool();
  }
  timelineGutter() {
    if (this._gutter > 0) return this._gutter;
    const label = this.root.querySelector('.timeline-track-label');
    this._gutter = label ? Math.round(label.getBoundingClientRect().width) : TIMELINE_GUTTER;
    return this._gutter || TIMELINE_GUTTER;
  }
  seekAt(e) {
    const timeline = this.root.querySelector('[data-timeline]');
    if (!timeline || pointerOnScrollbar(timeline, e)) return;
    if (e.target.closest('[data-trim-handle], button, input')) return;
    const r = timeline.getBoundingClientRect();
    const gutter = this.timelineGutter();
    const localX = e.clientX - r.left;
    if (localX < gutter) return;
    const x = localX + timeline.scrollLeft - gutter;
    this.playhead = Math.max(0, Math.min(this.duration(), Math.round(x / this.zoom * 1e6)));
    if (this.seq) this.seq.seekTo(this.playhead / 1e6);
    else this.root.querySelector('[data-preview-video]').currentTime = this.playhead / 1e6;
    this.renderTime();
  }
  setPlaying(on) {
    const icon = this.root.querySelector('[data-transport="play"] i');
    if (icon) icon.className = on ? 'fa-sharp fa-solid fa-pause' : 'fa-sharp fa-solid fa-play';
  }
  transport(kind) { const v = this.root.querySelector('[data-preview-video]'); if (kind === 'play') { if (this.seq) this.seq.paused ? this.seq.play() : this.seq.pause(); else v.paused ? v.play() : v.pause(); this.setPlaying(this.seq ? !this.seq.paused : !v.paused); } else if (kind === 'start') { this.playhead = 0; this.seq ? this.seq.seekTo(0) : v.currentTime = 0; } else if (kind === 'end') { this.playhead = this.duration(); this.seq ? this.seq.seekTo(this.playhead / 1e6) : v.currentTime = this.playhead / 1e6; } else { const step = Math.round(1e6 / Number(this.doc?.fps || 30)); this.playhead = Math.max(0, Math.min(this.duration(), this.playhead + (kind === 'next' ? step : -step))); this.seq ? this.seq.seekTo(this.playhead / 1e6) : v.currentTime = this.playhead / 1e6; } this.renderTime(); }
  refreshPlayback() {
    const segments = (this.resolved || []).map((entry) => entry.segment || entry).filter(Boolean);
    const timeline = segments.length ? segments : (this.doc?.segments || []);
    const preview = this.root.querySelector('[data-preview]');
    const empty = this.root.querySelector('[data-preview-empty]');
    const key = timeline.map((x) => `${x.id}:${x.video_id}:${x.clip_id}:${x.export_job_id}:${x.type}:${x.text || ''}:${x.start_us}:${x.source_in_us}:${x.duration_us}:${x.transition?.kind || ''}:${x.transition?.duration_us || 0}`).join('|');
    if (preview && key && key !== this.playbackKey) {
      const wasPlaying = this.seq ? !this.seq.paused : false;
      const oldTime = this.playhead / 1e6;
      this.playbackKey = key;
      if (!this.seq) {
        this.seq = new SequencePlayback(preview);
        this.seq.on('timeupdate', (t) => { this.playhead = Number(t || 0) * 1e6; this.updatePlayhead(); this.schedulePlayheadChrome(); });
        this.seq.on('play', () => this.setPlaying(true));
        this.seq.on('pause', () => { this.setPlaying(false); this.publishEditorContext(); });
        this.seq.on('ended', () => { this.setPlaying(false); this.playhead = this.duration(); this.renderTime(); });
      }
      this.seq.load(buildPlaybackClips(timeline));
      this.seq.seekTo(Math.min(oldTime, this.seq.duration || oldTime));
      if (wasPlaying) this.seq.play();
    }
    if (empty) empty.hidden = timeline.length > 0;
    this.renderOverlay();
    this.renderTitleCard();
  }
  renderOverlay() { const svg = this.root.querySelector('[data-overlay-svg]'); const active = (this.doc?.overlays || []).filter(o => o.visible !== false && this.playhead >= Number(o.start_us || 0) && this.playhead < Number(o.end_us || Number.MAX_SAFE_INTEGER)).sort((a,b) => Number(a.z || 0) - Number(b.z || 0)); svg.replaceChildren(...active.map(o => { const kind = o.kind === 'rectangle' ? 'rect' : o.kind === 'ellipse' ? 'ellipse' : o.kind === 'arrow' ? 'line' : o.kind === 'freehand' || o.kind === 'path' ? 'polyline' : o.kind === 'image' ? 'image' : o.kind === 'callout' || o.kind === 'text' ? 'text' : 'rect'; const t = document.createElementNS('http://www.w3.org/2000/svg', kind); t.dataset.selectId = o.id; t.setAttribute('opacity', o.opacity ?? 1); t.setAttribute('transform', `rotate(${o.rotation || 0} ${(o.x || 0) + (o.width || 0)/2} ${(o.y || 0) + (o.height || 0)/2})`); if (kind === 'text') { t.setAttribute('x', o.x || 0); t.setAttribute('y', (o.y || 0) + (o.font_size || 42)); t.setAttribute('fill', o.color || '#fff'); t.setAttribute('font-size', o.font_size || 42); t.setAttribute('font-family', o.font || 'Arial'); t.textContent = o.text || ''; } else if (kind === 'image') { t.setAttribute('x', o.x || 0); t.setAttribute('y', o.y || 0); t.setAttribute('width', o.width || 320); t.setAttribute('height', o.height || 180); t.setAttribute('preserveAspectRatio', 'none'); t.setAttribute('href', `/api/stitch/projects/${encodeURIComponent(this.projectID)}/assets/${encodeURIComponent(o.asset_id || '')}`); } else if (kind === 'ellipse') { t.setAttribute('cx', (o.x || 0) + (o.width || 0)/2); t.setAttribute('cy', (o.y || 0) + (o.height || 0)/2); t.setAttribute('rx', (o.width || 320)/2); t.setAttribute('ry', (o.height || 180)/2); t.setAttribute('fill', o.background || 'none'); t.setAttribute('stroke', o.color || '#57d5b2'); } else if (kind === 'line') { t.setAttribute('x1', o.x || 0); t.setAttribute('y1', o.y || 0); t.setAttribute('x2', (o.x || 0) + (o.width || 320)); t.setAttribute('y2', (o.y || 0) + (o.height || 180)); t.setAttribute('stroke', o.color || '#57d5b2'); t.setAttribute('stroke-width', 8); } else if (kind === 'polyline') { t.setAttribute('points', (o.points || []).map(p => `${p.x},${p.y}`).join(' ')); t.setAttribute('fill', 'none'); t.setAttribute('stroke', o.color || '#57d5b2'); t.setAttribute('stroke-width', 8); } else { t.setAttribute('x', o.x || 0); t.setAttribute('y', o.y || 0); t.setAttribute('width', o.width || 320); t.setAttribute('height', o.height || 180); t.setAttribute('rx', o.kind === 'callout' ? 18 : 0); t.setAttribute('fill', o.background || 'none'); t.setAttribute('stroke', o.color || '#57d5b2'); } return t; })); this.enhanceOverlayShapes(); }
  enhanceOverlayShapes() {
    const ns = 'http://www.w3.org/2000/svg';
    (this.doc?.overlays || []).filter((overlay) => overlay.visible !== false && this.playhead >= Number(overlay.start_us || 0) && this.playhead < Number(overlay.end_us || Number.MAX_SAFE_INTEGER)).forEach((overlay) => {
      const node = this.root.querySelector(`[data-overlay-svg] [data-select-id="${CSS.escape(overlay.id)}"]`);
      if (!node || node.dataset.enhanced) return;
      const group = document.createElementNS(ns, 'g');
      group.dataset.selectId = overlay.id;
      group.dataset.enhanced = 'true';
      group.setAttribute('opacity', overlay.opacity ?? 1);
      group.setAttribute('transform', `translate(${overlay.x || 0} ${overlay.y || 0}) rotate(${overlay.rotation || 0} ${(overlay.width || 0) / 2} ${(overlay.height || 0) / 2})`);
      if (overlay.kind === 'callout') {
        const bubble = document.createElementNS(ns, 'rect');
        bubble.setAttribute('x', '0'); bubble.setAttribute('y', '0'); bubble.setAttribute('width', overlay.width || 320); bubble.setAttribute('height', overlay.height || 180); bubble.setAttribute('rx', '18'); bubble.setAttribute('fill', overlay.background || '#13241f'); bubble.setAttribute('stroke', overlay.color || '#57d5b2');
        const tail = document.createElementNS(ns, 'polygon');
        tail.setAttribute('points', `${Math.min(96, Number(overlay.width || 320) - 48)},${overlay.height || 180} ${Math.min(144, Number(overlay.width || 320) - 16)},${overlay.height || 180} ${Math.min(120, Number(overlay.width || 320) - 32)},${Number(overlay.height || 180) + 42}`); tail.setAttribute('fill', overlay.background || '#13241f');
        const text = document.createElementNS(ns, 'text');
        text.setAttribute('x', '24'); text.setAttribute('y', String(24 + Number(overlay.font_size || 42))); text.setAttribute('fill', overlay.color || '#fff'); text.setAttribute('font-size', overlay.font_size || 42); text.setAttribute('font-family', overlay.font || 'Arial'); text.textContent = overlay.text || '';
        group.append(bubble, tail, text);
      } else if (overlay.kind === 'arrow') {
        const line = document.createElementNS(ns, 'line');
        line.setAttribute('x1', '0'); line.setAttribute('y1', '0'); line.setAttribute('x2', overlay.width || 320); line.setAttribute('y2', overlay.height || 180); line.setAttribute('stroke', overlay.color || '#57d5b2'); line.setAttribute('stroke-width', '8');
        const head = document.createElementNS(ns, 'polygon');
        const x = Number(overlay.width || 320); const y = Number(overlay.height || 180);
        head.setAttribute('points', `${x},${y} ${x - 34},${y - 10} ${x - 18},${y - 34}`); head.setAttribute('fill', overlay.color || '#57d5b2');
        group.append(line, head);
      } else if (overlay.kind === 'freehand' || overlay.kind === 'path') {
        const path = document.createElementNS(ns, 'polyline');
        path.setAttribute('points', (overlay.points || []).map((point) => `${point.x},${point.y}`).join(' ')); path.setAttribute('fill', 'none'); path.setAttribute('stroke', overlay.color || '#57d5b2'); path.setAttribute('stroke-width', '8'); group.append(path);
      } else return;
      node.parentNode.replaceChild(group, node);
    });
  }

  async command(type, fields = {}) {
    if (this.readOnly) return;
    const allowedWithoutTarget = type === 'undo' || type === 'redo' || type === 'set_canvas' || Boolean(fields.segment) || Boolean(fields.overlay) || Boolean(fields.caption) || Boolean(fields.settings);
    if (!this.selected.size && !fields.target_id && !allowedWithoutTarget) return;
    const operationKey = fields.operation_key || crypto.randomUUID();
    const endpoint = type === 'undo' || type === 'redo' ? type : 'commands';
    return queueSerialized(this, async () => {
      if (this.conflictDraft) return;
      const op = { type, target_id: fields.target_id || [...this.selected][0], ...fields };
      const body = type === 'undo' || type === 'redo'
        ? { expected_revision: this.revision, operation_key: operationKey }
        : { expected_revision: this.revision, operation_key: operationKey, summary: type, operations: [op] };
      this.pending = (this.pending || 0) + 1;
      this.clearError();
      try {
        const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/${endpoint}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (response.status === 409) {
          if (!this.conflictDraft) this.conflictDraft = { type, fields: { ...fields }, operationKey };
          this.showConflict('Document changed on the server; choose Reload latest or Apply draft.');
          return;
        }
        if (!response.ok) throw new Error(`${type} failed (${response.status})`);
        const result = await response.json();
        if (Number(result.revision || 0) < this.revision) return;
        this.revision = Number(result.revision ?? this.revision);
        this.doc = result.document || this.doc;
        this.resolved = Array.isArray(result.resolved) ? result.resolved : this.resolved;
        this.resolvedCaptions = Array.isArray(result.resolved_captions) ? result.resolved_captions : this.resolvedCaptions;
        this.draft = null;
        if ((this.remoteRevision || 0) <= this.revision) {
          this.remoteRevision = 0;
          this.setState('Live');
        }
        this.hideConflict();
        this.render();
      } catch (error) {
        this.error(error.message);
      } finally {
        this.pending = Math.max(0, this.pending - 1);
      }
    });
  }

  showConflict(message) {
    const box = this.root.querySelector('[data-conflict]');
    if (box) {
      box.hidden = false;
      box.querySelector('[data-conflict-message]').textContent = message;
      box.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
    }
    this.setState('Conflict: local draft preserved');
  }

  hideConflict() {
    const box = this.root.querySelector('[data-conflict]');
    if (box) box.hidden = true;
  }

  async reloadLatest() {
    const draft = this.conflictDraft;
    try {
      const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/document`, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`Reload failed (${response.status})`);
      const result = await response.json();
      this.revision = Number(result.revision || this.revision);
      this.doc = result.document || result;
      this.resolved = Array.isArray(result.resolved) ? result.resolved : this.resolved;
      this.resolvedCaptions = Array.isArray(result.resolved_captions) ? result.resolved_captions : this.resolvedCaptions;
      this.render();
      this.showConflict('Latest document loaded; your draft remains available to apply.');
    } catch (error) { this.error(error.message); }
  }

  async applyDraft() {
    const draft = this.conflictDraft;
    if (!draft) return;
    this.conflictDraft = null;
    this.hideConflict();
    await this.command(draft.type, { ...draft.fields, operation_key: draft.operationKey });
  }
  action(a) { if (a === 'youtube-copy-title') return this.copyYouTube('title'); if (a === 'youtube-copy-description') return this.copyYouTube('description'); if (a === 'youtube-copy-tags') return this.copyYouTube('tags'); if (a === 'youtube-save') return this.saveYouTube(); if (a === 'undo' || a === 'redo') return this.command(a); if (a === 'history') { this.root.querySelector('[data-history-list]')?.closest('details')?.setAttribute('open', ''); return this.history(); } if (a === 'export') { this.openExportPanel(); this.root.querySelector('[data-export-form]')?.requestSubmit(); return; } if (a === 'split-caption') { const cue = this.findSelected(); const delta = Math.round(this.playhead); return cue ? this.command('split_caption', { target_id: cue.id, delta_us: delta }) : undefined; } if (a === 'merge-caption') { return this.command('merge_caption', { ids: [...this.selected] }); } if (a === 'add-segment') { return this.command('insert_segment', { segment: { type: 'title', duration_us: 3_000_000, start_us: this.duration(), text: 'Title', legacy: { text: 'Title', bg_color: '#000000', text_color: '#ffffff' } } }); } if (a === 'add-caption') return this.addCaption(); if (a === 'import-captions') { const item = this.findSelected(); const segmentID = item?.segment_id || ((this.doc?.segments || []).some((segment) => segment.id === item?.id) ? item.id : null); if (!segmentID) { this.error('Select a sequence clip before importing captions.'); return; } return this.importCaptions(segmentID, item.language || 'en'); } const selected = [...this.selected][0]; const selectedIsOverlay = (this.doc?.overlays || []).some(x => x.id === selected); const frame = Math.round(1e6 / Number(this.doc?.fps || 30)); const current = (this.doc?.segments || []).find(x => x.id === selected); const map = { 'trim-start':'trim_segment', 'trim-end':'trim_segment', split:'split_segment', duplicate:'duplicate_segment', remove:selectedIsOverlay ? 'delete_overlay' : 'remove_segment', 'nudge-left':'move_segment', 'nudge-right':'move_segment', 'move-earlier':'reorder_segments', 'move-later':'reorder_segments', 'link-timing':'link_timing', 'unlink-timing':'unlink_timing', 'group-position':'group_position', 'ungroup-position':'ungroup_position', 'add-overlay':'upsert_overlay' }; if (map[a]) { const groupAction = a === 'link-timing' || a === 'unlink-timing' || a === 'group-position' || a === 'ungroup-position'; const groupKind = a === 'link-timing' || a === 'unlink-timing' ? 'timing' : 'position'; const existingGroup = (a === 'unlink-timing' || a === 'ungroup-position') ? findExistingGroup(this.doc, [...this.selected], groupKind) : null; if ((a === 'unlink-timing' || a === 'ungroup-position') && !existingGroup) { this.error('Select members of an existing group to unlink or ungroup.'); return; } const fields = groupAction ? { group: { id: existingGroup?.id || crypto.randomUUID(), members: existingGroup?.members || [...this.selected] } } : {}; if (a === 'nudge-left' || a === 'nudge-right') fields.delta_us = a === 'nudge-left' ? -frame : frame; if (a === 'move-earlier' || a === 'move-later') { const segments = this.doc.segments || []; const index = segments.findIndex((segment) => segment.id === selected); const before = a === 'move-earlier' ? segments[index - 1] : segments[index + 2]; fields.before_id = before?.id || null; } if (a === 'split') fields.delta_us = current ? Math.max(frame, Math.min(Number(current.duration_us || 0) - frame, this.playhead - Number(current.start_us || 0))) : 0; if (a === 'trim-start' || a === 'trim-end') { fields.start_us = a === 'trim-start' ? frame : 0; fields.end_us = current ? Number(current.duration_us || 0) - (a === 'trim-end' ? frame : 0) : 0; } return this.command(map[a], fields); } if (a === 'zoom-in') this.setZoom(stepZoom(this.zoom, 1)); else if (a === 'zoom-out') this.setZoom(stepZoom(this.zoom, -1)); else if (a === 'fit') this.fitTimeline(); else if (a === 'fit-selection') this.fitSelection(); else if (a === 'import-captions') this.error('Caption import requires a segment selection.'); }
  renderTitleCard() {
    const preview = this.root.querySelector('[data-preview]');
    const card = preview?.querySelector('[data-title-card]');
    if (this.seq) {
      card?.remove();
      return;
    }
    const segment = (this.resolved || []).map((entry) => entry.segment || entry).find((item) => item && item.type === 'title' && this.playhead >= Number(item.start_us || 0) && this.playhead < Number(item.start_us || 0) + Number(item.duration_us || 0));
    if (!segment) { card?.remove(); return; }
    let next = card;
    if (!next) { next = document.createElement('div'); next.dataset.titleCard = 'true'; preview.append(next); }
    const painted = titlePayloadForSegment(segment);
    next.textContent = painted.text;
    next.style.cssText = `position:absolute;inset:0;display:grid;place-items:center;padding:8%;text-align:center;color:${painted.text_color};background:${painted.bg_color};font-family:${painted.font}, serif;font-size:${painted.font_size}px;z-index:2;`;
  }

  openExportPanel() {
    this.root.querySelector('[data-export-form]')?.closest('details')?.setAttribute('open', '');
  }
  setExportStatus(message) {
    const status = this.root.querySelector('[data-export-status]');
    if (status) status.textContent = message || '';
  }
  exportError(message) {
    this.error(message);
    this.setExportStatus(message);
  }
  async exportJob(form) {
    this.clearError();
    this.openExportPanel();
    const range = form.get('scope') === 'selection';
    const selected = this.findSelected();
    if (range && !selected) { this.exportError('Select a segment or range before exporting.'); return; }
    const start = range ? Number(selected.start_us || 0) : 0;
    const end = range ? Number(selected.end_us || start + Number(selected.duration_us || 0)) : this.duration();
    if (end <= start) { this.exportError('Export range is empty.'); return; }
    const captionMode = form.get('caption_mode') || form.get('caption_delivery') || 'none';
    const submit = this.root.querySelector('[data-export-form] button[type="submit"]');
    if (submit) submit.disabled = true;
    this.setExportStatus('Queueing export…');
    try {
      const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/exports`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ revision: this.revision, operation_key: crypto.randomUUID(), format: form.get('format') || 'mp4', quality: form.get('quality') || 'high', caption_mode: captionMode, scope: range ? 'range' : 'all', start_us: start, end_us: end, loudness_target: Number(form.get('loudness') || -14) }) });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) { this.exportError(payload.error || payload.message || `Export request failed (${response.status})`); return; }
      this.setState('Export queued');
      this.setExportStatus('Export queued');
      await this.loadExports();
      this.pollExports();
    } catch (error) {
      this.exportError(error.message);
    } finally {
      if (submit) submit.disabled = false;
    }
  }
  async loadExports() {
    const out = this.root.querySelector('[data-export-list]');
    if (!out) return;
    try {
      const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/render-jobs`, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`Export list failed (${response.status})`);
      const data = await response.json();
      const jobs = (Array.isArray(data.jobs) ? data.jobs : []).filter((job) => !job.kind || job.kind === 'export');
      if (!jobs.length) {
        const empty = document.createElement('p');
        empty.className = 'stitch-muted';
        empty.textContent = 'No exports yet';
        out.replaceChildren(empty);
        return;
      }
      out.replaceChildren(...jobs.map((job) => {
        const row = document.createElement('div');
        row.className = 'stitch-export-row';
        const title = document.createElement('div');
        title.className = 'stitch-list-title';
        const status = job.status || 'queued';
        const pct = Number(job.progress_pct ?? job.progress ?? 0);
        title.textContent = status === 'processing' && pct > 0 ? `export · ${status} ${pct}%` : `export · ${status}`;
        const meta = document.createElement('div');
        meta.className = 'stitch-list-meta';
        if (status === 'ready' && job.id) {
          const link = document.createElement('a');
          link.href = `/api/stitch/${encodeURIComponent(job.id)}/download`;
          link.textContent = 'Download';
          link.setAttribute('download', '');
          meta.append(link);
        } else if (job.error) {
          meta.textContent = job.error;
        } else {
          meta.textContent = job.options?.format || '';
        }
        row.append(title, meta);
        return row;
      }));
      const active = jobs.some((job) => job.status === 'queued' || job.status === 'processing');
      if (active) {
        this.setExportStatus('Export in progress…');
        this.pollExports();
      } else if (jobs.some((job) => job.status === 'ready')) {
        this.setExportStatus('');
      } else if (jobs.some((job) => job.error || job.status === 'error')) {
        this.setExportStatus(jobs.find((job) => job.error)?.error || 'Export failed');
      }
    } catch (error) {
      this.exportError(error.message);
    }
  }
  pollExports() {
    if (this.exportTimer) return;
    this.exportTimer = setTimeout(() => {
      this.exportTimer = null;
      this.loadExports();
    }, 2000);
  }
  applyExportDefaults() {
    const settings = this.doc?.settings || {};
    const form = this.root.querySelector('[data-export-form]');
    if (!form) return;
    if (settings.format) form.elements.format.value = settings.format;
    if (settings.quality) form.elements.quality.value = settings.quality;
    if (settings.caption_mode) this.root.querySelectorAll('[data-caption-mode]').forEach((el) => { el.value = settings.caption_mode; });
    if (settings.caption_font) this.root.querySelectorAll('[data-font-select="caption"]').forEach((el) => { el.value = settings.caption_font; });
    if (Number.isFinite(Number(settings.loudness_target)) && Number(settings.loudness_target) !== 0) form.elements.loudness.value = String(settings.loudness_target);
    this.fillFontSelects();
  }
  async loadFonts() {
    try {
      const response = await fetch('/api/fonts/json', { headers: { Accept: 'application/json' } });
      if (response.ok) {
        const faces = await response.json();
        this.fonts = [...new Set(['Tomorrow', 'UnifrakturCook', ...((Array.isArray(faces) ? faces : []).map((face) => face.family).filter(Boolean))])];
      }
    } catch {
      this.fonts = ['Tomorrow', 'UnifrakturCook'];
    }
    this.fillFontSelects();
  }
  fillFontSelects() {
    const families = this.fonts?.length ? this.fonts : ['Tomorrow', 'UnifrakturCook'];
    const captionFont = this.doc?.settings?.caption_font || 'Tomorrow';
    this.root.querySelectorAll('[data-font-select]').forEach((select) => {
      const current = select.dataset.fontSelect === 'caption' ? captionFont : (select.value || select.dataset.current || families[0]);
      select.replaceChildren(...families.map((family) => {
        const option = document.createElement('option');
        option.value = family;
        option.textContent = family;
        option.selected = family === current;
        return option;
      }));
      if (current && !families.includes(current)) {
        const option = document.createElement('option');
        option.value = current;
        option.textContent = current;
        option.selected = true;
        select.prepend(option);
      }
    });
  }
  persistCaptionSettings(source) {
    const settings = { ...(this.doc?.settings || {}) };
    if (source?.name === 'caption_mode') settings.caption_mode = source.value;
    if (source?.name === 'caption_font') settings.caption_font = source.value;
    this.root.querySelectorAll('[data-caption-mode]').forEach((el) => { if (settings.caption_mode) el.value = settings.caption_mode; });
    this.root.querySelectorAll('[data-font-select="caption"]').forEach((el) => { if (settings.caption_font) el.value = settings.caption_font; });
    this.command('set_settings', { settings });
  }
  async history() { try { const r = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/history`); if (!r.ok) throw new Error(`History request failed (${r.status})`); const d = await r.json(); const out = this.root.querySelector('[data-history-list]'); out.replaceChildren(...(d.edits || d.events || []).map(edit => { const p = document.createElement('p'); p.className = 'stitch-muted'; p.textContent = `${edit.summary || 'Edit'} · ${edit.created_at || ''}`; return p; })); } catch (e) { this.error(e.message); } }
  renderCaptionPreview() {
    const out = this.root.querySelector('[data-live-caption]');
    const localDraft = this.draft && (this.doc?.captions || []).some((caption) => caption.id === this.draft.id) ? this.draft : null;
    const serverCaptions = Array.isArray(this.resolvedCaptions) ? this.resolvedCaptions : (this.doc?.captions || []);
    const captions = localDraft
      ? [localDraft, ...(this.resolvedCaptions || []).filter((caption) => caption.id !== localDraft.id)]
      : serverCaptions;
    const cue = captions.find((caption) => {
      const segment = (this.resolved || []).map((entry) => entry.segment || entry).find((item) => item.id === caption.segment_id);
      const inCue = this.playhead >= Number(caption.start_us || 0) && this.playhead < Number(caption.end_us || 0);
      const inSegment = !segment || (this.playhead >= Number(segment.start_us || 0) && this.playhead < Number(segment.start_us || 0) + Number(segment.duration_us || 0));
      return inCue && inSegment;
    });
    if (!cue) { out.replaceChildren(); return; }
    const style = cue.style || {};
    const words = cue.alignment === 'valid' && style.word_highlight === true && Array.isArray(cue.words) && cue.words.length ? cue.words : null;
    const shown = words || [{ text: cue.text || '' }];
    out.replaceChildren(...shown.map((word) => {
      const span = document.createElement('span');
      span.textContent = `${word.text || ''} `;
      if (words && this.playhead >= Number(word.start_us || 0) && this.playhead < Number(word.end_us || 0)) span.className = 'active-word';
      return span;
    }));
    out.style.color = style.color || '';
    out.style.setProperty('--stitch-highlight-color', style.highlight_color || '#57d5b2');
    const preview = this.root.querySelector('[data-preview]');
    const scale = preview && this.doc?.width ? preview.clientWidth / Number(this.doc.width) : 1;
    out.style.fontSize = `${Number(style.font_size || 48) * scale}px`;
    out.style.fontFamily = style.font || 'Arial';
    out.style.textShadow = 'none';
    out.style.fontWeight = style.bold ? '700' : '400';
    out.style.fontStyle = style.italic ? 'italic' : 'normal';
    out.style.background = style.background || 'transparent';
    out.style.webkitTextStroke = style.outline_width ? `${Number(style.outline_width) * scale}px ${style.outline_color || '#000'}` : '';
    const canvasWidth = Number(this.doc?.width || 1920);
    const canvasHeight = Number(this.doc?.height || 1080);
    let anchorX = Number(style.x || 0);
    let anchorY = Number(style.y || 0);
    if (!anchorX && !anchorY) {
      anchorX = canvasWidth / 2;
      anchorY = canvasHeight - 48;
    }
    if (style.safe_area) {
      anchorX = Math.max(canvasWidth * 0.1, Math.min(canvasWidth * 0.9, anchorX));
      anchorY = Math.max(canvasHeight * 0.1, Math.min(canvasHeight * 0.9, anchorY));
      out.style.width = 'max-content';
      out.style.maxWidth = '80%';
    } else {
      out.style.width = 'max-content';
      out.style.maxWidth = '100%';
    }
    out.style.left = `${anchorX * scale}px`;
    out.style.top = `${anchorY * scale}px`;
    out.style.transform = 'translate(-50%, -100%)';
    out.style.right = 'auto';
    out.style.bottom = 'auto';
    out.style.padding = '';
  }

  renderInspector() {
    const out = this.root.querySelector('[data-inspector-content]');
    const selected = this.findSelected();
    this.root.querySelector('[data-selection-count]').textContent = `${selected ? 1 : 0} selected`;
    out.replaceChildren();
    if (!selected) {
      const empty = document.createElement('p');
      empty.className = 'stitch-muted';
      empty.textContent = 'Select a segment, caption, or layer to inspect it.';
      out.append(empty);
      return;
    }
    const heading = document.createElement('p');
    heading.className = 'stitch-muted';
    heading.textContent = `${selected.type || selected.kind || 'item'} · ${selected.id}`;
    out.append(heading);
    if (selected.type === 'title') {
      const legacy = typeof selected.legacy === 'object' && selected.legacy !== null ? selected.legacy : {};
      const titleLabel = document.createElement('label');
      titleLabel.className = 'stitch-field';
      titleLabel.textContent = 'Title card text';
      const titleInput = document.createElement('textarea');
      titleInput.rows = 3;
      titleInput.value = legacy.text || legacy.title || '';
      titleInput.addEventListener('change', () => {
        const segment = { ...selected, legacy: { ...legacy, text: titleInput.value, title: titleInput.value } };
        this.command('update_segment', { target_id: selected.id, segment });
      });
      titleLabel.append(titleInput);
      out.append(titleLabel);
      const fontLabel = document.createElement('label');
      fontLabel.className = 'stitch-field';
      fontLabel.textContent = 'Font';
      const fontSelect = document.createElement('select');
      fontSelect.dataset.fontSelect = 'title';
      fontSelect.dataset.current = legacy.font || 'UnifrakturCook';
      fontSelect.addEventListener('change', () => {
        this.command('update_segment', { target_id: selected.id, segment: { ...selected, legacy: { ...legacy, font: fontSelect.value } } });
      });
      fontLabel.append(fontSelect);
      out.append(fontLabel);
      const sizeLabel = document.createElement('label');
      sizeLabel.className = 'stitch-field';
      sizeLabel.textContent = 'Font size';
      const sizeInput = document.createElement('input');
      sizeInput.type = 'number';
      sizeInput.min = '24';
      sizeInput.max = '144';
      sizeInput.step = '4';
      sizeInput.value = String(legacy.font_size || 72);
      sizeInput.addEventListener('change', () => {
        this.command('update_segment', { target_id: selected.id, segment: { ...selected, legacy: { ...legacy, font_size: Number(sizeInput.value) } } });
      });
      sizeLabel.append(sizeInput);
      out.append(sizeLabel);
      this.fillFontSelects();
    }
    if (['clip', 'video'].includes(selected.type)) this.renderMulticamControls(out, selected);
    if (['clip', 'video', 'stitch'].includes(selected.type)) this.renderTeaserLayoutControls(out, selected);
    if ((this.doc.captions || []).some((caption) => caption.id === selected.id)) {
      const text = document.createElement('textarea');
      text.rows = 3;
      text.dataset.captionText = 'true';
      text.value = selected.text || '';
      text.setAttribute('aria-label', 'Caption text');
      out.append(text);
      const captionActions = document.createElement('div');
      captionActions.className = 'stitch-inspector-actions';
      [['split-caption', 'Split cue'], ['merge-caption', 'Merge selected cues']].forEach(([action, labelText]) => {
        const button = document.createElement('button');
        button.dataset.action = action;
        button.textContent = labelText;
        captionActions.append(button);
      });
      const alignment = document.createElement('p');
      alignment.className = 'stitch-muted';
      alignment.textContent = selected.alignment === 'valid' ? 'Word timing aligned' : (selected.alignment || 'Word timing pending');
      const alignButton = document.createElement('button');
      alignButton.type = 'button';
      alignButton.className = 'stitch-button';
      alignButton.textContent = selected.alignment === 'pending' ? 'Alignment pending…' : 'Align words';
      alignButton.disabled = selected.alignment === 'pending' || !selected.text?.trim();
      alignButton.title = selected.alignment === 'pending'
        ? 'Alignment is already queued for this caption.'
        : 'Queue forced alignment for the supplied caption text.';
      alignButton.addEventListener('click', () => this.command('request_alignment', {
        target_id: selected.id,
        language: selected.language || 'en',
        model_version: 'whisperx',
      }));
      out.append(captionActions, alignment, alignButton);
      (selected.words || []).forEach((word, index) => {
        const label = document.createElement('label');
        label.className = 'stitch-field';
        label.textContent = `${word.text || 'Word'} ${index + 1}`;
        const input = document.createElement('input');
        input.type = 'text';
        input.value = `${(Number(word.start_us || 0) / 1e6).toFixed(3)}-${(Number(word.end_us || 0) / 1e6).toFixed(3)}`;
        input.addEventListener('change', () => {
          const [start, end] = input.value.split('-').map((value) => Math.round(Number(value) * 1e6));
          if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) { this.error('Word timing must be a valid range.'); return; }
          word.start_us = start; word.end_us = end; selected.alignment = 'valid'; this.command('upsert_caption', { caption: selected });
        });
        label.append(input); out.append(label);
      });
      [['start_us', 'Start (s)'], ['end_us', 'End (s)']].forEach(([field, labelText]) => {
        const label = document.createElement('label');
        label.className = 'stitch-field';
        label.textContent = labelText;
        const input = document.createElement('input');
        input.type = 'number';
        input.step = '0.001';
        input.value = (Number(selected[field] || 0) / 1e6).toFixed(3);
        input.addEventListener('change', () => {
          selected[field] = Math.round(Number(input.value) * 1e6);
          this.command('upsert_caption', { caption: selected });
        });
        label.append(input);
        out.append(label);
      });
      const style = selected.style || {};
      const fontLabel = document.createElement('label');
      fontLabel.className = 'stitch-field';
      fontLabel.textContent = 'Font';
      const fontSelect = document.createElement('select');
      fontSelect.dataset.fontSelect = 'style';
      fontSelect.dataset.current = style.font || this.doc?.settings?.caption_font || 'Tomorrow';
      fontSelect.addEventListener('change', () => {
        selected.style = { ...(selected.style || {}), font: fontSelect.value };
        this.draft = selected;
        this.renderCaptionPreview();
        clearTimeout(this.captionTimer);
        this.captionTimer = setTimeout(() => this.command('upsert_caption', { caption: selected }), 500);
      });
      fontLabel.append(fontSelect);
      out.append(fontLabel);
      this.fillFontSelects();
      [['Color', 'color', style.color || '#ffffff'], ['Background', 'background', style.background || '#000000'], ['Size', 'font_size', style.font_size || 42]].forEach(([labelText, key, value]) => {
        const label = document.createElement('label');
        label.className = 'stitch-field';
        label.textContent = labelText;
        const input = document.createElement('input');
        input.value = value;
        input.addEventListener('change', () => {
          selected.style = { ...(selected.style || {}), [key]: key === 'font_size' ? Number(input.value) : input.value };
          this.draft = selected;
          this.renderCaptionPreview();
          clearTimeout(this.captionTimer);
          this.captionTimer = setTimeout(() => this.command('upsert_caption', { caption: selected }), 500);
        });
        label.append(input);
        out.append(label);
      });
      const captionStyleControls = [
        ['Highlight color', 'highlight_color', style.highlight_color || '#57d5b2', 'color'],
        ['Outline color', 'outline_color', style.outline_color || '#000000', 'color'],
        ['Outline width', 'outline_width', style.outline_width || 0, 'number'],
        ['Placement X', 'x', style.x || 0, 'number'],
        ['Placement Y', 'y', style.y || 0, 'number'],
      ];
      captionStyleControls.forEach(([labelText, key, value, type]) => {
        const label = document.createElement('label');
        label.className = 'stitch-field';
        label.textContent = labelText;
        const input = document.createElement('input');
        input.type = type;
        input.step = type === 'number' ? '1' : undefined;
        input.value = value;
        input.addEventListener('change', () => {
          selected.style = { ...(selected.style || {}), [key]: type === 'number' ? Number(input.value) : input.value };
          this.draft = selected;
          this.renderCaptionPreview();
          clearTimeout(this.captionTimer);
          this.captionTimer = setTimeout(() => this.command('upsert_caption', { caption: selected }), 500);
        });
        label.append(input);
        out.append(label);
      });
      const toggles = [
        ['Word highlight', 'word_highlight', style.word_highlight === true],
        ['Safe area', 'safe_area', style.safe_area === true],
        ['Bold', 'bold', style.bold === true],
        ['Italic', 'italic', style.italic === true],
      ];
      toggles.forEach(([labelText, key, checked]) => {
        const label = document.createElement('label');
        label.className = 'stitch-field';
        const input = document.createElement('input');
        input.type = 'checkbox';
        input.checked = checked;
        input.addEventListener('change', () => {
          selected.style = { ...(selected.style || {}), [key]: input.checked };
          this.draft = selected;
          this.renderCaptionPreview();
          clearTimeout(this.captionTimer);
          this.captionTimer = setTimeout(() => this.command('upsert_caption', { caption: selected }), 500);
        });
        label.append(input, document.createTextNode(labelText));
        out.append(label);
      });
      const languageLabel = document.createElement('label');
      languageLabel.className = 'stitch-field';
      languageLabel.textContent = 'Language';
      const language = document.createElement('input');
      language.value = selected.language || 'en';
      language.addEventListener('change', () => {
        selected.language = language.value.trim() || 'en';
        this.command('upsert_caption', { caption: selected });
      });
      languageLabel.append(language);
      out.append(languageLabel);
      const importButton = document.createElement('button');
      importButton.className = 'stitch-button';
      importButton.textContent = 'Import source captions';
      importButton.disabled = !selected.segment_id;
      importButton.title = selected.segment_id ? 'Import captions from the selected source segment' : 'Select a caption attached to a segment';
      importButton.addEventListener('click', () => {
        if (selected.segment_id) this.importCaptions(selected.segment_id, selected.language || 'en');
        else this.error('Caption import requires a caption attached to a segment.');
      });
      out.append(importButton);
      return;
    }
    [['source_in_us', 'Source In (s)'], ['source_out_us', 'Source Out (s)'], ['project_start_us', 'Project Start (s)']].forEach(([field, labelText]) => {
      const label = document.createElement('label');
      label.className = 'stitch-field';
      label.textContent = labelText;
      const input = document.createElement('input');
      input.type = 'number';
      input.step = '0.001';
      const value = field === 'source_out_us' ? Number(selected.source_in_us || 0) + Number(selected.duration_us || 0) : field === 'project_start_us' ? Number(selected.start_us || 0) : Number(selected.source_in_us || 0);
      input.value = (value / 1e6).toFixed(3);
      input.addEventListener('change', () => {
        const requested = Math.round(Number(input.value) * 1e6);
        if (!Number.isFinite(requested)) {
          this.error(`${labelText} must be a valid time.`);
          return;
        }
        if (field === 'project_start_us') {
          this.command('move_segment', { target_id: selected.id, delta_us: requested - Number(selected.start_us || 0) });
          return;
        }
        const sourceIn = field === 'source_in_us' ? requested : Number(selected.source_in_us || 0);
        const sourceOut = field === 'source_out_us' ? requested : Number(selected.source_in_us || 0) + Number(selected.duration_us || 0);
        this.command('trim_segment', buildTrimOperation(selected, sourceIn, sourceOut));
      });
      label.append(input);
      out.append(label);
    });
    const actions = document.createElement('div');
    actions.className = 'stitch-inspector-actions';
    [['trim-start', 'Trim start'], ['trim-end', 'Trim end'], ['split', 'Split at playhead'], ['duplicate', 'Duplicate'], ['remove', 'Remove'], ['nudge-left', 'Nudge ←'], ['nudge-right', 'Nudge →'], ['move-earlier', 'Move earlier'], ['move-later', 'Move later'], ['link-timing', 'Link timing'], ['unlink-timing', 'Unlink timing'], ['group-position', 'Group position'], ['ungroup-position', 'Ungroup position']].forEach(([action, labelText]) => {
      const button = document.createElement('button');
      button.dataset.action = action;
      button.textContent = labelText;
      actions.append(button);
    });
    out.append(actions);
  }

  legacyMulticam(segment) {
    const legacy = parseLegacy(segment?.legacy);
    const meta = legacy?.legacy_metadata && typeof legacy.legacy_metadata === 'object' ? legacy.legacy_metadata : parseLegacy(legacy?.legacy_metadata);
    return meta && typeof meta === 'object' ? meta : {};
  }

  segmentCrops(segment) {
    if (Array.isArray(segment?.crops) && segment.crops.length) return segment.crops;
    const crops = this.legacyMulticam(segment).crops;
    return Array.isArray(crops) ? crops : [];
  }

  segmentShots(segment) {
    const copy = (shots) => shots.map((shot) => ({ ...shot, transition_out: shot.transition_out ? { ...shot.transition_out } : null }));
    if (Array.isArray(segment?.shots)) return copy(segment.shots);
    const fromMeta = this.legacyMulticam(segment).shot_list;
    return Array.isArray(fromMeta) ? copy(fromMeta) : [];
  }

  clipDurationSec(segment) {
    const clipStartUS = Number(segment?.clip_start_us || segment?.source_in_us || 0);
    const legacy = parseLegacy(segment?.legacy);
    const meta = legacy?.legacy_metadata && typeof legacy.legacy_metadata === 'object' ? legacy.legacy_metadata : parseLegacy(legacy?.legacy_metadata);
    const fromMeta = Number(meta?.duration_us);
    const fromTrim = Number(segment?.source_in_us || 0) + Number(segment?.duration_us || 0) - clipStartUS;
    const shots = this.segmentShots(segment);
    const lastEnd = shots.reduce((max, shot) => Math.max(max, Number(shot.end) || 0), 0);
    const us = Number.isFinite(fromMeta) && fromMeta > 0 ? fromMeta : fromTrim;
    const sec = Number.isFinite(us) ? us / 1e6 : lastEnd;
    return Number.isFinite(sec) && sec > 0 ? Math.max(sec, lastEnd) : Math.max(lastEnd, 0);
  }

  renderMulticamControls(out, selected) {
    const crops = this.segmentCrops(selected);
    const section = document.createElement('section');
    section.className = 'stitch-inspector-section stitch-cameras-section';
    const heading = document.createElement('p');
    heading.className = 'stitch-muted';
    heading.textContent = 'Cameras';
    section.append(heading);
    if (crops.length < 2) {
      if (selected.type === 'clip') {
        const hint = document.createElement('p');
        hint.className = 'stitch-muted';
        hint.textContent = 'Add 2+ crops on the clip in Cut to use multicam here.';
        section.append(hint);
        out.append(section);
      }
      return;
    }
    const hint = document.createElement('p');
    hint.className = 'stitch-muted';
    hint.textContent = 'Cut between crops at the playhead. Saved on this stitch segment, not the clip bank.';
    section.append(hint);

    const activeCropId = activeMulticamCropId(selected, this.playhead);
    const buttons = document.createElement('div');
    buttons.className = 'stitch-camera-buttons';
    crops.forEach((crop, index) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'stitch-camera-button' + (crop.id === activeCropId ? ' is-active' : '');
      button.dataset.stitchCamera = crop.id;
      button.textContent = crop.name || `Camera ${index + 1}`;
      button.title = button.textContent;
      if (!this.readOnly) {
        button.addEventListener('click', () => this.cutMulticamToCamera(selected, crop.id));
      } else {
        button.disabled = true;
      }
      buttons.append(button);
    });
    section.append(buttons);

    if (!this.readOnly) {
      const transitionLabel = document.createElement('label');
      transitionLabel.className = 'stitch-field';
      transitionLabel.textContent = 'Transition';
      const transition = document.createElement('select');
      transition.dataset.stitchMulticamTransition = 'true';
      [['fade', 'Fade'], ['cut', 'Cut']].forEach(([value, label]) => {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = label;
        option.selected = value === 'fade';
        transition.append(option);
      });
      transitionLabel.append(transition);
      section.append(transitionLabel);
    }

    const shots = this.segmentShots(selected);
    const count = document.createElement('p');
    count.className = 'stitch-muted';
    count.dataset.stitchShotCount = 'true';
    count.textContent = `${shots.length} shot${shots.length === 1 ? '' : 's'}`;
    section.append(count);

    const list = document.createElement('div');
    list.className = 'stitch-shot-list';
    list.dataset.stitchShotList = 'true';
    if (!shots.length) {
      const empty = document.createElement('p');
      empty.className = 'stitch-muted';
      empty.textContent = 'No shots yet. Click a camera at the playhead to start.';
      list.append(empty);
    } else {
      shots.forEach((shot, index) => {
        const crop = crops.find((entry) => entry.id === shot.crop_id);
        const row = document.createElement('div');
        row.className = 'stitch-list-item stitch-shot-row';
        const main = document.createElement('div');
        main.className = 'stitch-list-main';
        const title = document.createElement('div');
        title.className = 'stitch-list-title';
        title.textContent = crop?.name || shot.crop_id || `Camera`;
        const meta = document.createElement('div');
        meta.className = 'stitch-list-meta';
        meta.textContent = `${Number(shot.start).toFixed(1)}–${Number(shot.end).toFixed(1)}s`;
        main.append(title, meta);
        row.append(main);
        if (!this.readOnly) {
          const remove = document.createElement('button');
          remove.type = 'button';
          remove.className = 'stitch-button';
          remove.textContent = 'Remove';
          remove.title = 'Remove shot';
          remove.addEventListener('click', () => this.removeMulticamShot(selected, index));
          row.append(remove);
        }
        list.append(row);
      });
    }
    section.append(list);

    if (!this.readOnly && shots.length) {
      const clear = document.createElement('button');
      clear.type = 'button';
      clear.className = 'stitch-button';
      clear.textContent = 'Clear all';
      clear.addEventListener('click', () => {
        if (confirm('Clear all shots?')) this.persistMulticam(selected, []);
      });
      section.append(clear);
    }
    out.append(section);
  }

  syncMulticamCameraButtons() {
    const selected = this.findSelected();
    if (!selected || !['clip', 'video'].includes(selected.type)) return;
    const activeCropId = activeMulticamCropId(selected, this.playhead);
    this.root.querySelectorAll('[data-stitch-camera]').forEach((button) => {
      button.classList.toggle('is-active', button.dataset.stitchCamera === activeCropId);
    });
  }

  cutMulticamToCamera(segment, cropId) {
    if (this.readOnly || !segment || !cropId) return;
    const start = Number(segment.start_us || 0);
    const end = start + Number(segment.duration_us || 0);
    if (this.playhead < start || this.playhead >= end) {
      this.error('Move the playhead inside the selected clip to switch cameras.');
      return;
    }
    const clipRelSec = clipRelativeSeconds(segment, this.playhead);
    const shots = this.segmentShots(segment);
    const transitionEl = this.root.querySelector('[data-stitch-multicam-transition]');
    const trType = transitionEl?.value || 'fade';
    const trDur = 0.3;

    if (shots.length === 0) {
      const clipDurationSec = this.clipDurationSec(segment);
      if (!(clipDurationSec > 0)) {
        this.error('Could not determine clip duration for multicam.');
        return;
      }
      shots.push({ crop_id: cropId, start: 0, end: clipDurationSec, transition_out: null });
    } else {
      const shotIndex = shots.findIndex((shot) => clipRelSec >= Number(shot.start) && clipRelSec < Number(shot.end));
      if (shotIndex < 0) return;
      const current = shots[shotIndex];
      if (current.crop_id === cropId) return;
      if (clipRelSec >= Number(current.end) - 0.1) return;
      if (clipRelSec <= Number(current.start) + 0.1) {
        current.crop_id = cropId;
        this.persistMulticam(segment, shots);
        return;
      }
      const transition = trType === 'cut' ? null : { type: trType, duration: Math.min(trDur, (clipRelSec - Number(current.start)) / 2) };
      const previousEnd = Number(current.end);
      const previousTransition = current.transition_out;
      current.transition_out = transition;
      current.end = clipRelSec;
      shots.splice(shotIndex + 1, 0, {
        crop_id: cropId,
        start: clipRelSec,
        end: previousEnd,
        transition_out: previousTransition
          ? { ...previousTransition, duration: Math.min(Number(previousTransition.duration) || trDur, (previousEnd - clipRelSec) / 2) }
          : null,
      });
    }
    this.persistMulticam(segment, shots);
  }

  removeMulticamShot(segment, index) {
    if (this.readOnly) return;
    const shots = this.segmentShots(segment);
    if (index < 0 || index >= shots.length) return;
    shots.splice(index, 1);
    if (shots.length) {
      const clipDurationSec = this.clipDurationSec({ ...segment, shots });
      shots[0].start = 0;
      for (let i = 1; i < shots.length; i++) shots[i].start = Number(shots[i - 1].end);
      shots[shots.length - 1].end = clipDurationSec > 0 ? clipDurationSec : Number(shots[shots.length - 1].end);
    }
    this.persistMulticam(segment, shots);
  }

  persistMulticam(segment, shots) {
    const crops = this.segmentCrops(segment);
    segment.crops = crops;
    segment.shots = shots;
    for (const entry of this.resolved || []) {
      const resolved = entry.segment || entry;
      if (resolved?.id === segment.id) {
        resolved.crops = crops;
        resolved.shots = shots;
      }
    }
    this._layoutSig = '';
    this.renderTeaserLayoutPreview();
    this.syncMulticamCameraButtons();
    this.command('set_segment_multicam', { target_id: segment.id, crops, shots });
    const selected = this.findSelected();
    if (selected?.id === segment.id) this.renderInspector();
  }

  renderTeaserLayoutControls(out, selected) {
    const section = document.createElement('section');
    section.className = 'stitch-inspector-section';
    const heading = document.createElement('p');
    heading.className = 'stitch-muted';
    heading.textContent = 'Portrait teaser layout';
    section.append(heading);
    const current = selected.layout || { mode: 'preserve_scene', crops: [] };
    const mode = document.createElement('select');
    mode.className = 'stitch-field';
    mode.dataset.layoutMode = 'true';
    [['preserve_scene', 'Preserve scene'], ['single_speaker', 'Single speaker'], ['two_speakers', 'Two speakers']].forEach(([value, label]) => {
      const option = document.createElement('option'); option.value = value; option.textContent = label; option.selected = current.mode === value; mode.append(option);
    });
    const modeLabel = document.createElement('label'); modeLabel.className = 'stitch-field'; modeLabel.textContent = 'Mode'; modeLabel.append(mode); section.append(modeLabel);
    const cropFields = document.createElement('div'); cropFields.dataset.layoutCrops = 'true';
    const defaults = (value) => value === 'two_speakers' ? [{ x: 0, y: 0, width: .5, height: 1 }, { x: .5, y: 0, width: .5, height: 1 }] : value === 'single_speaker' ? [{ x: 0, y: 0, width: 1, height: 1 }] : [];
    const drawCrops = (value, crops) => {
      cropFields.replaceChildren();
      const list = crops.length ? crops : defaults(value);
      list.forEach((crop, index) => {
        const row = document.createElement('div'); row.className = 'stitch-layout-crop';
        ['x', 'y', 'width', 'height'].forEach((key) => { const label = document.createElement('label'); label.className = 'stitch-field'; label.textContent = `${index + 1} ${key}`; const input = document.createElement('input'); input.type = 'number'; input.min = '0'; input.max = '1'; input.step = '0.01'; input.value = Number(crop[key] ?? 0).toFixed(2); input.dataset.layoutCrop = key; row.append(label); label.append(input); });
        cropFields.append(row);
      });
    };
    const save = () => {
      const crops = [...cropFields.querySelectorAll('.stitch-layout-crop')].map((row) => Object.fromEntries([...row.querySelectorAll('[data-layout-crop]')].map((input) => [input.dataset.layoutCrop, Number(input.value)])));
      if (crops.some((crop) => !Object.values(crop).every((value) => Number.isFinite(value) && value >= 0 && value <= 1) || crop.x + crop.width > 1 || crop.y + crop.height > 1 || crop.width <= 0 || crop.height <= 0)) { this.error('Crop rectangles must stay within normalized 0–1 bounds.'); return; }
      this.command('set_segment_layout', { target_id: selected.id, layout: { mode: mode.value, crops } });
    };
    drawCrops(current.mode, current.crops || []);
    mode.addEventListener('change', () => { drawCrops(mode.value, []); save(); });
    cropFields.addEventListener('change', save);
    section.append(cropFields);
    const hint = document.createElement('p'); hint.className = 'stitch-muted'; hint.textContent = 'Crops use normalized source coordinates. Two speakers stack top and bottom.'; section.append(hint);
    out.append(section);
  }

  renderTeaserLayoutPreview() {
    const preview = this.root.querySelector('[data-preview]');
    const video = preview?.querySelector('[data-preview-video]');
    if (!preview || !video) return;
    const resolved = (this.resolved || []).map((entry) => entry.segment || entry).filter(Boolean);
    const timeline = resolved.length ? resolved : (this.doc?.segments || []);
    const segment = timeline.find((item) => this.playhead >= Number(item.start_us || 0) && this.playhead < Number(item.start_us || 0) + Number(item.duration_us || 0));
    const crops = this.segmentCrops(segment);
    const shots = Array.isArray(segment?.shots) ? segment.shots : [];
    let layout = segment?.layout;
    let guideText = '';
    if (shots.length > 0 && crops.length > 0) {
      const cropId = activeMulticamCropId(segment, this.playhead);
      const activeCrop = crops.find((crop) => crop.id === cropId);
      if (activeCrop) {
        const width = Number(activeCrop.width || 0);
        const height = Number(activeCrop.height || 0);
        layout = {
          mode: 'single_speaker',
          crops: [{
            x: Math.max(0, Number(activeCrop.x || 0) - width / 2),
            y: Math.max(0, Number(activeCrop.y || 0) - height / 2),
            width,
            height,
          }],
        };
        guideText = `camera · ${activeCrop.name || cropId || 'Camera'}`;
      }
    }
    let canvas = preview.querySelector('[data-layout-canvas]');
    if (!canvas) { canvas = document.createElement('canvas'); canvas.dataset.layoutCanvas = 'true'; canvas.className = 'stitch-layout-canvas'; preview.insertBefore(canvas, preview.querySelector('[data-overlay-svg]')); }
    let guide = preview.querySelector('[data-layout-guide]');
    if (!guide) { guide = document.createElement('div'); guide.dataset.layoutGuide = 'true'; guide.className = 'stitch-layout-guide'; preview.append(guide); }
    if (!layout) {
      this.layoutPreviewSegment = null;
      this.layoutPreviewGeneration = (this.layoutPreviewGeneration || 0) + 1;
      if (this.layoutPreviewRAF) cancelAnimationFrame(this.layoutPreviewRAF);
      if (this.layoutPreviewVideoCallback && video.cancelVideoFrameCallback) video.cancelVideoFrameCallback(this.layoutPreviewVideoCallback);
      this.layoutPreviewRAF = null; this.layoutPreviewVideoCallback = null; this.layoutPreviewDraw = null; this.layoutPreviewRunning = false;
      canvas.hidden = true; guide.hidden = true; video.style.visibility = ''; video.style.objectFit = 'contain';
      return;
    }
    canvas.hidden = false; guide.hidden = false; video.style.visibility = 'hidden'; guide.textContent = guideText || `${layout.mode.replaceAll('_', ' ')} · ${layout.crops?.length || 0} crop${layout.crops?.length === 1 ? '' : 's'}`; this.layoutPreviewSegment = layout;
    if (this.layoutPreviewDraw) { this.layoutPreviewDraw(false); return; }
    const generation = (this.layoutPreviewGeneration || 0) + 1;
    this.layoutPreviewGeneration = generation;
    const draw = (schedule) => {
      if (generation !== this.layoutPreviewGeneration || !this.layoutPreviewSegment) return;
      const width = Number(this.doc?.width || 1080); const height = Number(this.doc?.height || 1920);
      if (canvas.width !== width || canvas.height !== height) { canvas.width = width; canvas.height = height; }
      const ctx = canvas.getContext('2d');
      if (!video.videoWidth || !video.videoHeight) { if (schedule) this.layoutPreviewRAF = requestAnimationFrame(() => draw(true)); return; }
      drawTeaserLayoutFrame(ctx, video, this.layoutPreviewSegment, width, height);
      if (schedule) {
        if (video.requestVideoFrameCallback) this.layoutPreviewVideoCallback = video.requestVideoFrameCallback(() => draw(true));
        else this.layoutPreviewRAF = requestAnimationFrame(() => draw(true));
      }
    };
    this.layoutPreviewDraw = (schedule = false) => draw(schedule);
    this.layoutPreviewRunning = true; draw(true);
  }

  captionSegment() {
    const item = this.findSelected();
    const segments = this.doc?.segments || [];
    if (item && segments.some((segment) => segment.id === item.id)) return item;
    if (item?.segment_id && segments.some((segment) => segment.id === item.segment_id)) {
      return segments.find((segment) => segment.id === item.segment_id);
    }
    return segments.find((segment) => this.playhead >= Number(segment.start_us || 0) && this.playhead < Number(segment.start_us || 0) + Number(segment.duration_us || 0)) || segments[0];
  }

  addCaption() {
    const segment = this.captionSegment();
    if (!segment) { this.error('Add a sequence clip before adding captions.'); return; }
    const start = Math.max(Number(segment.start_us || 0), Math.round(this.playhead));
    const end = Math.min(Number(segment.start_us || 0) + Number(segment.duration_us || 0), start + 2_000_000);
    this.setTab('captions');
    return this.command('upsert_caption', { caption: { segment_id: segment.id, text: 'New caption', language: 'en', start_us: start, end_us: Math.max(start + 1, end), style: { font: this.doc?.settings?.caption_font || 'Tomorrow', font_size: 42, color: '#ffffff' } } });
  }

  async importCaptions(segmentID, language = 'en') {
    if (!segmentID) {
      this.error('Select a sequence clip before importing captions.');
      return;
    }
    try {
      const r = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/captions/import`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ segment_id: segmentID, language, expected_revision: this.revision, operation_key: crypto.randomUUID() }),
      });
      if (!r.ok) throw new Error(`Caption import failed (${r.status})`);
      const result = await r.json();
      this.revision = Number(result.revision ?? this.revision);
      this.doc = result.document || this.doc;
      this.resolved = Array.isArray(result.resolved) ? result.resolved : this.resolved;
      this.resolvedCaptions = Array.isArray(result.resolved_captions) ? result.resolved_captions : this.resolvedCaptions;
      this.setTab('captions');
      this.render();
      this.setState(`Imported ${(this.doc?.captions || []).length} captions`);
    } catch (e) {
      this.error(e.message);
    }
  }

  syncOverlayTool() {
    this.root.querySelectorAll('[data-overlay-kind]').forEach((btn) => btn.classList.toggle('is-active', btn.dataset.overlayKind === this.overlayTool));
    this.root.querySelector('[data-preview]')?.classList.toggle('is-drawing', Boolean(this.overlayTool));
    const hint = this.root.querySelector('[data-overlay-hint]');
    if (hint) hint.textContent = this.overlayTool === 'freehand' ? 'Drag on the preview to draw.' : 'Click a tool to add it on the current frame.';
  }

  placeOverlay(kind, extra = {}) {
    if (this.readOnly) return;
    const now = Math.round(this.playhead);
    const width = Number(this.doc?.width || 1920);
    const height = Number(this.doc?.height || 1080);
    const isText = kind === 'text' || kind === 'callout';
    const overlay = {
      kind,
      text: isText ? (kind === 'callout' ? 'Callout' : 'Text') : '',
      x: width * 0.18,
      y: height * 0.18,
      width: isText ? width * 0.64 : width * 0.32,
      height: isText ? height * 0.18 : height * 0.22,
      start_us: now,
      end_us: now + 5_000_000,
      opacity: 1,
      z: (this.doc?.overlays || []).length,
      visible: true,
      locked: false,
      color: '#57d5b2',
      background: isText ? '' : '#13241f',
      font: 'Tomorrow',
      font_size: 42,
      ...extra,
    };
    this.overlayTool = null;
    this.syncOverlayTool();
    this.setTab('layers');
    return this.command('upsert_overlay', { overlay });
  }

  bindOverlayTools() {
    const svg = this.root.querySelector('[data-overlay-svg]');
    this.root.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && (this.overlayDrawing || this.overlayTool)) {
        this.overlayDrawing = null;
        this.overlayTool = null;
        this.pendingAssetID = null;
        this.syncOverlayTool();
        this.renderOverlay();
        this.setState('Drawing canceled');
      }
    });
    this.root.addEventListener('click', (event) => {
      const tool = event.target.closest('[data-overlay-kind]');
      if (tool && this.root.contains(tool)) {
        const kind = tool.dataset.overlayKind;
        this.setTab('layers');
        if (kind === 'freehand') {
          this.overlayTool = this.overlayTool === 'freehand' ? null : 'freehand';
          this.syncOverlayTool();
          this.setState(this.overlayTool ? 'Drag on the preview to draw' : 'Draw canceled');
          return;
        }
        if (kind === 'image') {
          const input = document.createElement('input');
          input.type = 'file';
          input.accept = 'image/*';
          input.addEventListener('change', async () => {
            const file = input.files?.[0];
            if (!file) return;
            const form = new FormData();
            form.append('file', file);
            const response = await fetch(`/api/stitch/projects/${encodeURIComponent(this.projectID)}/assets`, { method: 'POST', body: form });
            if (!response.ok) { this.error(`Image upload failed (${response.status})`); return; }
            const asset = await response.json();
            this.placeOverlay('image', { asset_id: asset.id || asset.asset_id });
          });
          input.click();
          return;
        }
        this.placeOverlay(kind);
        return;
      }
      const layerButton = event.target.closest('[data-layer-z]');
      if (layerButton && this.root.contains(layerButton)) {
        const overlay = this.findSelected();
        if (!overlay || !(this.doc?.overlays || []).some((item) => item.id === overlay.id)) return;
        overlay.z = Math.max(0, Number(overlay.z || 0) + (layerButton.dataset.layerZ === 'z-up' ? 1 : -1));
        this.command('upsert_overlay', { overlay });
      }
    });
    svg.addEventListener('pointerdown', (event) => {
      if (!this.overlayTool || event.target.closest('[data-select-id]')) return;
      if (this.overlayTool === 'image' && !this.pendingAssetID) {
        this.error('Choose an image before drawing its frame.');
        this.overlayTool = null;
        return;
      }
      const box = svg.getBoundingClientRect();
      const point = { x: (event.clientX - box.left) * Number(this.doc?.width || 1920) / box.width, y: (event.clientY - box.top) * Number(this.doc?.height || 1080) / box.height };
      this.overlayDrawing = { kind: this.overlayTool, points: [point], x: point.x, y: point.y };
      svg.setPointerCapture?.(event.pointerId);
      event.preventDefault();
    });
    svg.addEventListener('pointermove', (event) => {
      if (!this.overlayDrawing) return;
      const box = svg.getBoundingClientRect();
      if (this.overlayDrawing.points.length < 200) {
        this.overlayDrawing.points.push({ x: (event.clientX - box.left) * Number(this.doc?.width || 1920) / box.width, y: (event.clientY - box.top) * Number(this.doc?.height || 1080) / box.height });
      }
      this.renderDrawingPreview();
    });
    svg.addEventListener('pointercancel', () => {
      this.overlayDrawing = null;
      this.overlayTool = null;
      svg.querySelector('[data-drawing-ghost]')?.remove();
    });
    svg.addEventListener('pointerup', () => {
      if (!this.overlayDrawing) return;
      const drawing = this.overlayDrawing;
      this.overlayDrawing = null;
      this.overlayTool = null;
      const last = drawing.points[drawing.points.length - 1];
      const now = Math.round(this.playhead);
      const isText = drawing.kind === 'text' || drawing.kind === 'callout';
      const minX = Math.min(...drawing.points.map((point) => point.x));
      const minY = Math.min(...drawing.points.map((point) => point.y));
      const maxX = Math.max(...drawing.points.map((point) => point.x));
      const maxY = Math.max(...drawing.points.map((point) => point.y));
      const isFreehand = drawing.kind === 'freehand' || drawing.kind === 'path';
      const overlay = {
        kind: drawing.kind,
        asset_id: drawing.kind === 'image' ? this.pendingAssetID : undefined,
        points: isFreehand ? drawing.points.slice(0, 200).map((point) => ({ x: point.x - minX, y: point.y - minY })) : undefined,
        x: isFreehand ? minX : Math.min(drawing.x, last.x),
        y: isFreehand ? minY : Math.min(drawing.y, last.y),
        width: isText ? Math.max(120, maxX - minX) : Math.max(1, maxX - minX),
        height: isText ? Math.max(80, maxY - minY) : Math.max(1, maxY - minY),
        start_us: now,
        end_us: now + 5000000,
        opacity: 1,
        z: (this.doc?.overlays || []).length,
        visible: true,
        locked: false,
        color: '#57d5b2',
        background: isText ? '' : '#13241f',
        font: 'Tomorrow',
        font_size: 42,
        text: isText ? 'New overlay' : '',
      };
      svg.querySelector('[data-drawing-ghost]')?.remove();
      this.command('upsert_overlay', { overlay });
    });
  }

  renderDrawingPreview() {
    const svg = this.root.querySelector('[data-overlay-svg]');
    const drawing = this.overlayDrawing;
    if (!svg || !drawing) return;
    let ghost = svg.querySelector('[data-drawing-ghost]');
    if (!ghost) {
      ghost = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
      ghost.dataset.drawingGhost = 'true';
      ghost.setAttribute('fill', 'none');
      ghost.setAttribute('stroke', '#57d5b2');
      ghost.setAttribute('stroke-dasharray', '6 4');
      ghost.setAttribute('stroke-width', '4');
      svg.append(ghost);
    }
    ghost.setAttribute('points', drawing.points.map((point) => `${point.x},${point.y}`).join(' '));
  }

  renderOverlayFields() {
    const overlay = this.findSelected();
    if (!overlay || !(this.doc?.overlays || []).some((item) => item.id === overlay.id)) return;
    const out = this.root.querySelector('[data-inspector-content]');
    const controls = document.createElement('div');
    controls.className = 'stitch-inspector-actions';
    [['visible', overlay.visible !== false], ['locked', overlay.locked === true]].forEach(([key, checked]) => {
      const label = document.createElement('label');
      label.className = 'stitch-field';
      label.textContent = key;
      const input = document.createElement('input');
      input.type = 'checkbox';
      input.checked = checked;
      input.addEventListener('change', () => {
        overlay[key] = input.checked;
        this.command('upsert_overlay', { overlay });
      });
      label.append(input);
      controls.append(label);
    });
    [['x', overlay.x], ['y', overlay.y], ['width', overlay.width], ['height', overlay.height], ['rotation', overlay.rotation || 0], ['opacity', overlay.opacity ?? 1], ['start_us', overlay.start_us], ['end_us', overlay.end_us]].forEach(([key, value]) => {
      const label = document.createElement('label');
      label.className = 'stitch-field';
      label.textContent = key;
      const input = document.createElement('input');
      input.type = 'number';
      input.step = key.endsWith('_us') ? '1000' : '0.01';
      input.value = value || 0;
      input.addEventListener('change', () => {
        overlay[key] = Number(input.value);
        this.renderOverlay();
        this.command('upsert_overlay', { overlay });
      });
      label.append(input);
      controls.append(label);
    });
    [['z-up', 'Raise layer'], ['z-down', 'Lower layer']].forEach(([kind, labelText]) => {
      const button = document.createElement('button');
      button.dataset.layerZ = kind;
      button.textContent = labelText;
      button.addEventListener('click', () => {
        overlay.z = Math.max(0, Number(overlay.z || 0) + (kind === 'z-up' ? 1 : -1));
        this.command('upsert_overlay', { overlay });
      });
      controls.append(button);
    });
    out.append(controls);
  }

  mountAssist() {
    const panel = document.getElementById('rewind-agent');
    const mount = this.root.querySelector('#stitch-assist-mount');
    if (!panel || !mount || !panel.parentNode) return null;
    const state = {
      panel,
      parent: panel.parentNode,
      next: panel.nextSibling,
      className: panel.className,
      style: panel.getAttribute('style'),
      ariaHidden: panel.getAttribute('aria-hidden'),
      inert: panel.hasAttribute('inert'),
    };
    mount.append(panel);
    panel.className = 'stitch-embedded-agent';
    panel.style.cssText = 'display:block;position:static;inset:auto;width:100%;height:auto;max-height:none;transform:none;visibility:visible;opacity:1;';
    panel.removeAttribute('aria-hidden');
    panel.removeAttribute('inert');
    return state;
  }

  bindNavbarAssist() {
    const buttons = [...document.querySelectorAll('[data-agent-toggle]')];
    if (!buttons.length) return null;
    const handlers = buttons.map((button) => {
      const handler = (event) => {
        event.preventDefault();
        event.stopPropagation();
        this.setTab('assist');
        const assistTab = this.root.querySelector('[data-tab="assist"]');
        assistTab?.focus?.();
      };
      button.addEventListener('click', handler, true);
      return [button, handler];
    });
    return () => handlers.forEach(([button, handler]) => button.removeEventListener('click', handler, true));
  }

  publishEditorContext() {
    const mergePatch = window.__dsAPI?.mergePatch;
    if (typeof mergePatch !== 'function') return;
    const selected = this.findSelected();
    mergePatch({ agentEditorContext: { project_id: this.projectID, revision: this.revision, selected_ids: [...this.selected], start_us: Number(selected?.start_us || 0), end_us: Number(selected?.end_us || (selected ? Number(selected.start_us || 0) + Number(selected.duration_us || 0) : 0)), playhead_us: Math.round(this.playhead) } });
  }
  destroy() {
    if (this.exportTimer) { clearTimeout(this.exportTimer); this.exportTimer = null; }
    if (this.events) this.events.close();
    if (this.liveChromeRAF) cancelAnimationFrame(this.liveChromeRAF);
    if (this.rulerRAF) cancelAnimationFrame(this.rulerRAF);
    if (this.contextTimer) clearTimeout(this.contextTimer);
    this.liveChromeRAF = null; this.rulerRAF = null; this.contextTimer = null;
    this.layoutPreviewSegment = null;
    this.layoutPreviewRunning = false;
    if (this.layoutPreviewRAF) cancelAnimationFrame(this.layoutPreviewRAF);
    const video = this.root.querySelector('[data-preview-video]');
    if (video?.cancelVideoFrameCallback && this.layoutPreviewVideoCallback) video.cancelVideoFrameCallback(this.layoutPreviewVideoCallback);
    this.layoutPreviewRAF = null; this.layoutPreviewVideoCallback = null; this.layoutPreviewDraw = null;
    window.__dsAPI?.mergePatch?.({ agentEditorContext: null });
    this.assistNavigationCleanup?.();
    this.assistNavigationCleanup = null;
    if (this.cleanup) this.cleanup = null;
    if (this.restoreAssist) {
      const state = this.restoreAssist;
      state.parent.insertBefore(state.panel, state.next && state.next.parentNode === state.parent ? state.next : null);
      state.panel.className = state.className;
      if (state.style === null) state.panel.removeAttribute('style'); else state.panel.setAttribute('style', state.style);
      if (state.ariaHidden === null) state.panel.removeAttribute('aria-hidden'); else state.panel.setAttribute('aria-hidden', state.ariaHidden);
      if (state.inert) state.panel.setAttribute('inert', ''); else state.panel.removeAttribute('inert');
      this.restoreAssist = null;
    }
  }
}

export function mountStitchWorkspaces(rootDocument = document) {
  if (!rootDocument?.querySelectorAll) return;
  rootDocument.querySelectorAll('[data-stitch-workspace]').forEach((root) => {
    if (root.dataset.mounted === '1') return;
    root.dataset.mounted = '1';
    const workspace = new StitchWorkspace(root);
    if (typeof window !== 'undefined') window.stitchWorkspace = workspace;
  });
}

if (typeof window !== 'undefined') {
  window.mountStitchWorkspaces = mountStitchWorkspaces;
  if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', () => mountStitchWorkspaces(), { once: true });
    else mountStitchWorkspaces();
  }
  if (typeof window.addEventListener === 'function') {
    window.addEventListener('rewind:page-ready', () => mountStitchWorkspaces());
  }
}






















