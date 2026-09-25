import { listen as pageListen, pageTimeout, pageFrame, pageFetch, PageMutationObserver, PageResizeObserver, onPageCleanup } from './lib/page-scope.js';
import {
  clamp, isFiniteNumber, clampNumber, parseAspectRatio,
  formatTime, formatTimecode, formatFrameTimecode,
  DEFAULT_KEYBINDINGS, getKeybindingsFromDOM, buildKeyMap,
  resolveColorToRGB, rgbToOklch, computeContrastBorder,
  mixRGB, rgbToString, luminance, timeFromEvent,
  normalizeClip, normalizeMarker,
  setDragCursor, clearDragCursor,
} from './lib/utils.js';
import { FilterPreviewEngine } from './lib/filter-preview-engine.js';
import { autoInitFilterDials } from './lib/filter-dial.js';
import { AudioToolsEngine } from './lib/audio-tools-engine.js';
import { WaveformRenderer } from './lib/waveform-renderer.js';
import { TransportMixin } from './lib/transport.js';
import { CropOverlay } from './lib/crop-overlay.js';
import { ClipBank } from './lib/clip-bank.js';
import { ColorSwatches } from './lib/color-swatches.js';
import { SeekThumbnails } from './lib/seek-thumbnails.js';
import { Controls } from './lib/keyboard.js';
import { Timeline } from './lib/timeline-render.js';
import { ClipEditingMixin } from './lib/clip-editing.js';
import { WindowNavMixin } from './lib/window-nav.js';
import { DragHandlerMixin } from './lib/drag-handlers.js';
import { AttachMixin } from './lib/attach.js';
import { MulticamEngine } from './lib/multicam.js';

(() => {
  class CutPageEditor {
    constructor(root) {
      this.root = root;
      this.videoID = root.dataset.videoId || null;
      this.videoFps = Number(root.dataset.videoFps || 0) || 0;
      this.video = root.querySelector('video');

      this.keybindings = { ...DEFAULT_KEYBINDINGS, ...getKeybindingsFromDOM() };
      this.keyMap = buildKeyMap(this.keybindings);

      this.overviewEl = document.querySelector('[data-cut-overview]');
      this.overviewLayer = document.querySelector('[data-cut-overview-layer]');
      this.workEl = document.querySelector('[data-cut-work]');
      this.workLayer = document.querySelector('[data-cut-work-layer]');
      this.rangeEl = document.querySelector('[data-cut-range]');
      this.clipBankEl = document.querySelector('[data-cut-clip-bank]');

      this.btnPanLeft = document.querySelector('[data-cut-pan-left]');
      this.btnPanRight = document.querySelector('[data-cut-pan-right]');
      this.btnZoomIn = document.querySelector('[data-cut-zoom-in]');
      this.btnZoomOut = document.querySelector('[data-cut-zoom-out]');
      this.btnToggleFilmstrip = document.querySelector('[data-cut-toggle-filmstrip]');
      this.btnSetIn = document.querySelector('[data-cut-set-in]');
      this.btnSetOut = document.querySelector('[data-cut-set-out]');
      this.btnSaveClip = document.querySelector('[data-cut-save-clip]');
      this.btnCreateClip = document.querySelector('[data-cut-create-clip]');
      this.btnPlaySelection = document.querySelector('[data-cut-play-selection]');
      this.btnLoop = document.querySelector('[data-cut-loop]');
      this.contextListEl = document.querySelector('[data-cut-context-list]');
      this.contextTitleEl = document.querySelector('[data-cut-context-title]');
      this.btnContextCreate = document.querySelector('[data-cut-context-create]');
      this.btnContextUpdate = document.querySelector('[data-cut-context-update]');
      this.btnContextDelete = document.querySelector('[data-cut-context-delete]');

      // Transport controls
      this.btnTransportStart = document.querySelector('[data-cut-transport-start]');
      this.btnTransportPrevFrame = document.querySelector('[data-cut-transport-prev-frame]');
      this.btnTransportStop = document.querySelector('[data-cut-transport-stop]');
      this.btnTransportPlay = document.querySelector('[data-cut-transport-play]');
      this.btnTransportNextFrame = document.querySelector('[data-cut-transport-next-frame]');
      this.btnTransportEnd = document.querySelector('[data-cut-transport-end]');
      this.btnTransportLoop = document.querySelector('[data-cut-transport-loop]');
      this.transportTimeEl = document.querySelector('[data-cut-transport-time]');
      this.transportLoopEnabled = false;

      // Crop overlay (over the video preview) - delegated to CropOverlay module
      this.cropOverlay = new CropOverlay(this);

      // Multicam shot list engine
      this.multicam = new MulticamEngine(this);
      this.cropLayerEl = root.querySelector('[data-cut-crop-layer]');
      this.cropSurfaceEl = root.querySelector('[data-cut-crop-surface]');
      this.cropRectEl = root.querySelector('[data-cut-crop-rect]');
      this.cropHandleEl = root.querySelector('[data-cut-crop-handle]');
      this.cropOverlay.bindDOM(this.cropLayerEl, this.cropSurfaceEl, this.cropRectEl, this.cropHandleEl);

      this.selectedClipId = null;
      this.pendingClipStart = null;
      this.pendingClipEnd = null;
      this.pendingClipDirty = false;
      this.editMode = false; // true = selection is attached to clip timing
      this.duration = NaN;
      this.workStart = 0;
      this.workEnd = 0;
      this.inPoint = null;
      this.outPoint = null;

      // "Work head" = last timeline position you clicked/started an edit gesture from.
      // This is separate from the playhead (video.currentTime), so you can edit ranges
      // without always seeking the video.
      this.workHeadTime = NaN;

      this.loopEnabled = false;
      this.stopAtOut = false;

      this.markers = [];
      this.clips = [];
      this.contextWindows = [];
      this.selectedContextWindowID = null;

      // Event-driven clip data store - replaces polling loops
      this.clipBank = new ClipBank(this.videoID);
      this._wireClipBankEvents();

      // Seek thumbnails (spritesheets + VTT) + waveform peaks (best-effort)
      this.seekThumbs = new SeekThumbnails(this);
      // Backward-compat alias so existing code reading this.seek.manifest still works
      this.seek = this.seekThumbs;
      this.waveformRenderer = new WaveformRenderer(this);
      // Backward-compatible alias: code that reads this.waveform.peaks/manifest
      // will go through the module instance.
      this.waveform = this.waveformRenderer;

      // Filmstrip visibility toggle (persisted to localStorage)
      this.showFilmstrip = localStorage.getItem('cut-editor-show-filmstrip') !== 'false';

      // Overview zoom viewport - defaults to full video range, can be zoomed in
      this.overviewStart = 0;
      this.overviewEnd = 0; // initialised to duration in ensureOverviewWindow()

      // Unified drag state machine (see lib/drag-handlers.js)
      this.drag = { type: 'none' };
      this.overviewPointerMode = 'seek'; // 'seek' | 'move' | 'resize-left' | 'resize-right' | 'set'
      this.suppressNextOverviewClick = false;
      this.suppressNextWorkClick = false;

      // Timeline renderer (class, see lib/timeline-render.js)
      this.timeline = new Timeline(this);

      // Keyboard / Controls handler (class, see lib/keyboard.js)
      this.controls = new Controls(this);

      // Filter preview engine (imported from lib/)
      this.filterPreview = new FilterPreviewEngine(this.video, this.video.parentElement);

      // Audio tools engine - real-time VU meters, spectrum, scope
      this.audioTools = new AudioToolsEngine(this.filterPreview.audioGraph);
      // Wire analyser taps into the audio graph rebuild cycle
      this.filterPreview.audioGraph.onRebuild = (source, lastNode, dest) => {
        if (this.audioTools) this.audioTools.tap(source, lastNode, dest);
      };

      this.attach();
      this.load();
    }

    /**
     * Wire ClipBank events to editor methods.
     * This replaces the setInterval polling loops and MutationObserver in init().
     */
    _wireClipBankEvents() {
      // clips:loaded - update local clips array and re-render timeline
      this.clipBank.addEventListener('clips:loaded', (e) => {
        this.clips = e.detail.clips;
        this.render();
      });

      // clip:selected - seek, set in/out, center work window
      this.clipBank.addEventListener('clip:selected', (e) => {
        const { clip, seekTime } = e.detail;
        if (this.selectedClipId !== clip.id) this.cropOverlay.setSelectedCropId(null);
        this.selectClip(clip, seekTime);
        // Load multicam state for this clip (shot list comes from server via SSE panel patch)
        if (this.multicam) {
          pageFrame(() => {
            this.multicam.loadForClip(clip.id, clip.shotList || []);
          });
        }
      });

      // clip:deselected - clear selection state
      this.clipBank.addEventListener('clip:deselected', () => {
        // Don't call clearSelectedClip() here - that would mergePatch the signal
        // back to empty, creating a loop. Just clear local JS state.
        this.selectedClipId = null;
        this.cropOverlay.setSelectedCropId(null);
        this.editMode = false;
        this.pendingClipStart = null;
        this.pendingClipEnd = null;
        this.pendingClipDirty = false;
        if (this.multicam) this.multicam.clear();
        this.render();
      });
    }

    attach() {
      // Video element event listeners (loadedmetadata, timeupdate, play/pause/ended)
      this._attachVideoListeners();

      pageListen(window, 'resize', () => this.render());
      this.initColorSwatches();

      // Crop overlay pointer interaction
      if (this.cropRectEl) {
        this.cropRectEl.addEventListener('pointerdown', (e) => this.cropOverlay.beginDrag('move', e));
      }
      if (this.cropHandleEl) {
        this.cropHandleEl.addEventListener('pointerdown', (e) => this.cropOverlay.beginDrag('resize', e));
      }

      // Timeline listeners (overview + work)
      this._attachOverviewListeners();
      this._attachWorkListeners();

      // Document-level drag handlers (work-pan, overview-pan, clip trim,
      // overview drag, work selection drag) - see lib/drag-handlers.js
      pageListen(document, 'mousemove', (e) => this.handleDocumentMouseMove(e));
      pageListen(document, 'mouseup', () => this.handleDocumentMouseUp());

      // Buttons (set in/out, create, loop, transport, etc.)
      this._attachButtons();
      this._attachContextWindowButtons();

      // Keyboard shortcuts (delegated to Controls class - see lib/keyboard.js)
      pageListen(document, 'keydown', (e) => this.controls.handleKeyDown(e));
    }

    // Transport methods (seekRelative, transportGoToStart, transportGoToEnd,
    // transportPrevFrame, transportNextFrame, transportStop, transportPlay,
    // transportTogglePlay, transportToggleLoop, updateTransportPlayButton,
    // updateTransportLoopButton, updateTransportTime, toggleFilmstrip)
    // are provided by TransportMixin - see lib/transport.js

    // Window navigation methods (getWorkSelectionHit, ensureDefaultWorkWindow,
    // setWorkWindow, panWorkWindow, zoomWorkWindow, ensureOverviewWindow,
    // zoomOverview, panOverview, resetOverviewZoom, isOverviewZoomed,
    // renderRange, getSelectionRange) are provided by WindowNavMixin -
    // see lib/window-nav.js

    // Clip editing methods (selectClip, clearSelectedClip, enterEditMode, nudge*,
    // splitClipAtPlayhead, createClipFromRange, deleteClip, markPendingClipTiming,
    // scheduleAutoSave, setSignalInput) are provided by ClipEditingMixin -
    // see lib/clip-editing.js

    initColorSwatches() {
      if (!this._colorSwatches) {
        this._colorSwatches = new ColorSwatches();
      }
      this._colorSwatches.init();
    }





    async load() {
      if (!this.videoID) return;
      await Promise.all([this.loadMarkers(), this.clipBank.reload(), this.seekThumbs.loadManifest(), this.loadWaveformAssets(), this.loadContextWindows()]);
      this.render();
      const params = new URLSearchParams(window.location.search);
      const clipID = params.get('clip');
      if (clipID) {
        this.root.querySelector('[data-clip-row][data-clip-id="' + CSS.escape(clipID) + '"]')?.click();
      } else {
        const context = this.contextWindows.find(item => item.id === params.get('context'));
        if (context) this.selectContextWindow(context);
      }
    }

    _attachContextWindowButtons() {
      this.btnContextCreate?.addEventListener('click', () => this.createContextWindow());
      this.btnContextUpdate?.addEventListener('click', () => this.updateContextWindow());
      this.btnContextDelete?.addEventListener('click', () => this.deleteContextWindow());
    }

    async loadContextWindows() {
      if (!this.videoID) return;
      try {
        const res = await pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/context-windows`, {
          headers: { Accept: 'application/json' },
        });
        if (!res.ok) throw new Error(`Context Windows request failed (${res.status})`);
        const body = await res.json();
        const rows = Array.isArray(body) ? body : [];
        this.contextWindows = rows.map((raw) => ({
          id: String(raw.ID ?? raw.id ?? ''),
          start: Number(raw.StartTs ?? raw.start_ts ?? 0),
          end: Number(raw.EndTs ?? raw.end_ts ?? 0),
          title: String(raw.Title ?? raw.title ?? 'Context'),
          summary: String(raw.Summary ?? raw.summary ?? ''),
          topics: Array.isArray(raw.Topics ?? raw.topics) ? (raw.Topics ?? raw.topics).map((item) => String(item ?? '').trim()).filter(Boolean) : [],
          entities: Array.isArray(raw.Entities ?? raw.entities) ? (raw.Entities ?? raw.entities).map((item) => String(item ?? '').trim()).filter(Boolean) : [],
          stale: Boolean(raw.EvidenceStale ?? raw.evidence_stale ?? false),
          kind: String(raw.Kind ?? raw.kind ?? 'window'),
          parentId: String(raw.ParentID ?? raw.parent_id ?? ''),
          hook: String(raw.Hook ?? raw.hook ?? ''),
        })).filter((item) => item.id && item.end > item.start);
      } catch (error) {
        console.warn('Failed to load Context Windows:', error);
        this.contextWindows = [];
      }
      this.renderContextWindowList();
    }

    renderContextWindowList() {
      if (!this.contextListEl) return;
      this.contextListEl.replaceChildren();
      const windows = this.contextWindows.filter((item) => item.kind !== 'short');
      if (!windows.length) {
        const empty = document.createElement('p');
        empty.className = 'font-mono text-xs text-white/40';
        empty.textContent = 'No Context Windows yet.';
        this.contextListEl.appendChild(empty);
        return;
      }
      const shortsByParent = new Map();
      this.contextWindows.forEach((item) => {
        if (item.kind !== 'short' || !item.parentId) return;
        const list = shortsByParent.get(item.parentId) || [];
        list.push(item);
        shortsByParent.set(item.parentId, list);
      });
      windows.forEach((item) => {
        this.contextListEl.appendChild(this.contextWindowButton(item, false));
        (shortsByParent.get(item.id) || []).forEach((sh) => {
          this.contextListEl.appendChild(this.contextWindowButton(sh, true));
        });
      });
    }

    contextWindowButton(item, isShort) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = isShort
        ? 'w-full border-2 p-2 text-left font-mono text-xs ml-3'
        : 'w-full border-2 p-2 text-left font-mono text-xs';
      const selected = item.id === this.selectedContextWindowID;
      button.classList.add(selected
        ? (isShort ? 'border-cyan-300/70' : 'border-amber-300/70')
        : (isShort ? 'border-cyan-400/30' : 'border-white/10'));
      const title = document.createElement('span');
      title.className = 'block text-white';
      title.textContent = (isShort ? 'Short · ' : '') + item.title + (item.stale ? ' · evidence stale' : '');
      const range = document.createElement('span');
      range.className = isShort ? 'block text-cyan-300/70' : 'block text-white/40';
      range.textContent = `${formatTime(item.start)}–${formatTime(item.end)}`;
      const tooltip = [item.title];
      if (item.hook) tooltip.push(item.hook);
      if (item.topics?.length) tooltip.push(`Topics: ${item.topics.join(', ')}`);
      if (item.entities?.length) tooltip.push(`Entities: ${item.entities.join(', ')}`);
      button.title = tooltip.join('\n');
      button.append(title, range);
      if (isShort && item.hook) {
        const hook = document.createElement('span');
        hook.className = 'block text-white/50 italic';
        hook.textContent = item.hook;
        button.append(hook);
      }
      button.addEventListener('click', () => this.selectContextWindow(item));
      return button;
    }

    selectContextWindow(item) {
      // Load the window's range only. Clip creation is a separate CREATE action.
      if (this.selectedClipId) {
        this.clearSelectedClip();
      }
      this.selectedContextWindowID = item.id;
      this.inPoint = item.start;
      this.outPoint = item.end;
      this.workHeadTime = item.start;
      if (this.contextTitleEl) this.contextTitleEl.value = item.title;
      if (this.btnContextUpdate) this.btnContextUpdate.disabled = false;
      if (this.btnContextDelete) this.btnContextDelete.disabled = false;
      if (this.video) this.video.currentTime = item.start;
      this.setWorkWindow(item.start, item.end);
      this.renderContextWindowList();
    }

    contextRange() {
      if (!isFiniteNumber(this.inPoint) || !isFiniteNumber(this.outPoint)) return null;
      const start = Math.min(this.inPoint, this.outPoint);
      const end = Math.max(this.inPoint, this.outPoint);
      return end > start ? { start, end } : null;
    }

    async createContextWindow() {
      const range = this.contextRange();
      const title = this.contextTitleEl?.value?.trim();
      if (!range || !title) return;
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/context-windows`, {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: JSON.stringify({ start: range.start, end: range.end, title, boundary_quality: 'manual' }),
      });
      if (!res.ok) return;
      const created = await res.json();
      await this.loadContextWindows();
      const id = String(created.ID ?? created.id ?? '');
      const item = this.contextWindows.find((window) => window.id === id);
      if (item) this.selectContextWindow(item);
      this.render();
    }

    async updateContextWindow() {
      const range = this.contextRange();
      const title = this.contextTitleEl?.value?.trim();
      if (!this.selectedContextWindowID || !range || !title) return;
      const res = await pageFetch(`/api/context-windows/${encodeURIComponent(this.selectedContextWindowID)}`, {
        method: 'PUT',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: JSON.stringify({ start: range.start, end: range.end, title, boundary_quality: 'manual' }),
      });
      if (!res.ok) return;
      await this.loadContextWindows();
      const item = this.contextWindows.find((window) => window.id === this.selectedContextWindowID);
      if (item) this.selectContextWindow(item);
      this.render();
    }

    async deleteContextWindow() {
      if (!this.selectedContextWindowID) return;
      const res = await pageFetch(`/api/context-windows/${encodeURIComponent(this.selectedContextWindowID)}`, { method: 'DELETE' });
      if (!res.ok) return;
      this.selectedContextWindowID = null;
      if (this.btnContextUpdate) this.btnContextUpdate.disabled = true;
      if (this.btnContextDelete) this.btnContextDelete.disabled = true;
      await this.loadContextWindows();
      this.render();
    }

    // Seek thumbnail methods – delegated to SeekThumbnails module (this.seekThumbs)
    async loadSeekManifest() { return this.seekThumbs.loadManifest(); }
    chooseSeekLevelForRange(s, e, w) { return this.seekThumbs.chooseLevelForRange(s, e, w); }

    async loadWaveformAssets() {
      return this.waveformRenderer.loadAssets();
    }

    snapTime(rawTime, evt) {
      if (!isFiniteNumber(rawTime)) return rawTime;
      if (evt && evt.altKey) return rawTime;

      let t = rawTime;
      const zeroCross = this.findNearestZeroCrossingTime(t, 5);
      if (isFiniteNumber(zeroCross)) {
        t = zeroCross;
      }

      const fps = isFiniteNumber(this.videoFps) && this.videoFps > 0 ? this.videoFps : 0;
      if (fps > 0) {
        t = Math.round(t * fps) / fps;
      }

      return clamp(t, 0, this.duration || t);
    }

    findNearestZeroCrossingTime(time, windowSeconds) {
      return this.waveformRenderer.findNearestZeroCrossingTime(time, windowSeconds);
    }

    drawWaveformToCanvas(canvas, startTime, endTime) {
      if (!canvas || !isFiniteNumber(startTime) || !isFiniteNumber(endTime) || endTime <= startTime) return;
      this.waveformRenderer.drawToCanvas(canvas, startTime, endTime);
    }

    async loadMarkers() {
      try {
        const res = await pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/markers`, {
          headers: { 'Accept': 'application/json' }
        });
        if (!res.ok) return;
        const body = await res.json();
        this.markers = (Array.isArray(body) ? body : []).map(normalizeMarker);
      } catch (_) {
        // Best-effort.
      }
    }

    async loadClipsForTimeline() {
      // Delegate to ClipBank - it fetches, normalizes, and fires events.
      // The clips:loaded event handler syncs this.clips and re-renders.
      await this.clipBank.reload();
    }

    // Selection playback methods (renderLoopButton, renderPlaySelectionButton,
    // toggleLoop, togglePlaySelection, playSelection, handleSelectionPlaybackTick)
    // are provided by TransportMixin - see lib/transport.js

    render() {
      this.ensureDefaultWorkWindow();
      this.ensureOverviewWindow();
      this.renderRange();
      this.timeline.renderOverview();
      this.timeline.renderWork();
      this.timeline.renderPlayheads();

      this.initColorSwatches();

      this.updateCropSurfaceLayout();
      this.renderCropOverlay();
    }

    // --- Crop overlay (delegated to CropOverlay module - see lib/crop-overlay.js) ---

    /** @returns {object} Current crop state from CropOverlay */
    get crop() { return this.cropOverlay.crop; }
    /** @returns {string|null} Selected crop ID from CropOverlay */
    get selectedCropId() { return this.cropOverlay.selectedCropId; }

    loadCrop(cropId, x, y, width, height, aspect) {
      this.cropOverlay.loadCrop(cropId, x, y, width, height, aspect);
    }

    updateCropSurfaceLayout() {
      this.cropOverlay.updateSurfaceLayout();
    }

    renderCropOverlay() {
      this.cropOverlay.renderOverlay();
    }

    /**
     * Return the live (uncommitted) clip color from the DataStar signal.
     * Used during timeline rendering so the selected clip bar reflects
     * color changes in real time as the user edits in the inspector.
     * Returns null if no live color is available.
     */
    getLiveClipColor() {
      if (!this.selectedClipId) return null;
      const api = window.__dsAPI;
      if (api) {
        const c = api.getPath('clipColor');
        if (typeof c === 'string' && c.trim()) return c.trim();
      }
      // Fallback: read from the bound input element
      const input = document.querySelector('[data-bind="clipColor"]');
      const v = input?.value?.trim();
      return v || null;
    }

    // --- Timeline delegators (forwarded to Timeline class instance) ---
    renderOverview() { this.timeline.renderOverview(); }
    renderWork() { this.timeline.renderWork(); }
    renderPlayheads() { this.timeline.renderPlayheads(); }
    findClipByID(id) { return this.timeline.findClipByID(id); }
    getTrimHitForBar(clip, bar) { return this.timeline.getTrimHitForBar(clip, bar); }
    getOverviewWorkWindowHit(e) { return this.timeline.getOverviewWorkWindowHit(e); }

    /** Reset drag state to idle. Called at the end of every gesture. */
    resetDrag() {
      this.drag = { type: 'none' };
      clearDragCursor();
    }

    // --- Signal bridge methods (called from DataStar data-effect / data-on-signal-patch) ---

    /** Apply a filter stack array to the live video preview. */
    applyFilterStack(stack) {
      if (!Array.isArray(stack)) return;
      if (this.filterPreview) {
        this.filterPreview.apply(stack);
      }
    }

    /** Re-render timeline when the live clip color signal changes. */
    onClipColorChange(_color) {
      if (this.selectedClipId) {
        this.render();
      }
    }

    /** Autosave bridge: schedule a save when _clipDirty becomes true. */
    onAutosaveCheck(dirty, autoSave, clipId) {
      if (dirty && autoSave && clipId) {
        if (!this._formAutoSaveTimer) {
          this._formAutoSaveTimer = pageTimeout(() => {
            this._formAutoSaveTimer = null;
            const trigger = document.querySelector('[data-cut-autosave-trigger]');
            if (trigger) trigger.click();
          }, 1500);
        }
      } else if (!dirty) {
        clearTimeout(this._formAutoSaveTimer);
        this._formAutoSaveTimer = null;
      }
    }

  }

  // Apply transport methods (play, seek, frame-step, loop, selection playback)
  // to the prototype so they're available as instance methods.
  Object.assign(CutPageEditor.prototype, TransportMixin, ClipEditingMixin, WindowNavMixin, DragHandlerMixin, AttachMixin);

  function init() {
    const root = document.querySelector('[data-cut-page][data-video-id]');
    if (!root) return;
    const editor = new CutPageEditor(root);
    // Make cutEditor globally accessible for template onclick handlers
    window.cutEditor = editor;
    onPageCleanup(() => {
      editor.video?.pause();
      editor.filterPreview?.destroy();
      editor.audioTools?.destroy();
      delete window.cutEditor;
    });

    // Called by CropRow templ component via data-on:click.
    // Reads crop data from data-* attributes - no inline JS escaping needed.
    window.cropRowSelect = function(btn) {
      const name = btn.dataset.cropName || '';
      const aspect = btn.dataset.cropAspect || '';
      const id = btn.closest('[data-crop-id]')?.dataset.cropId || '';

      // Update DataStar signals for UI display
      const api = window.__dsAPI;
      if (api) {
        api.mergePatch({
          _selectedCropId: id,
          _selectedCropName: name || (aspect ? aspect + ' Crop' : 'Crop'),
          _selectedCropAspect: aspect || 'custom',
        });
      }

      // Load crop into the cut editor overlay
      if (window.cutEditor) {
        window.cutEditor.loadCrop(
          id,
          parseFloat(btn.dataset.cropX),
          parseFloat(btn.dataset.cropY),
          parseFloat(btn.dataset.cropW),
          parseFloat(btn.dataset.cropH),
          aspect
        );
      }
    };

    // Filter preview, clip color, autosave, and clip selection are now
    // driven by DataStar signal bridges (data-effect / data-on-signal-patch)
    // in video_cut.templ, which call editor methods directly.
    // No polling loops needed.

    // Audio tools: attach canvases and start render loop.
    // We start on first user interaction (play) because AudioContext needs a gesture.
    if (editor.audioTools) {
      const meterCanvas = document.querySelector('[data-audio-meter]');
      const spectrumCanvas = document.querySelector('[data-audio-spectrum]');
      const scopeCanvas = document.querySelector('[data-audio-scope]');
      if (meterCanvas || spectrumCanvas || scopeCanvas) {
        editor.audioTools.attach(meterCanvas, spectrumCanvas, scopeCanvas);
        // Resize canvases to match display size (retina-aware)
        const resizeCanvases = () => {
          [meterCanvas, spectrumCanvas, scopeCanvas].forEach(c => {
            if (!c) return;
            const dpr = window.devicePixelRatio || 1;
            const rect = c.getBoundingClientRect();
            if (rect.width > 0 && rect.height > 0) {
              c.width = Math.round(rect.width * dpr);
              c.height = Math.round(rect.height * dpr);
              c.getContext('2d').scale(dpr, dpr);
            }
          });
        };
        // Start on first play (AudioContext requires user gesture)
        const startOnPlay = () => {
          editor.video.removeEventListener('play', startOnPlay);
          // Ensure AudioPreviewGraph context exists for analysers
          if (editor.filterPreview?.audioGraph) {
            editor.filterPreview.audioGraph.ensureContext();
            // If no filters active, manually trigger tap
            const ag = editor.filterPreview.audioGraph;
            if (ag.source && ag.ctx) {
              editor.audioTools.tap(ag.source, ag.activeNodes.length > 0
                ? ag.activeNodes[ag.activeNodes.length - 1]
                : ag.source, ag.ctx.destination);
            }
          }
          resizeCanvases();
          editor.audioTools.start();
        };
        editor.video.addEventListener('play', startOnPlay);
        // Also handle resize
        new PageResizeObserver(resizeCanvases).observe(
          document.querySelector('[data-audio-tools]') || document.body
        );
      }

      // Crop overlay resize observer
      if (editor.video && editor.cropSurfaceEl) {
        const updateCropLayout = () => {
          editor.updateCropSurfaceLayout();
          editor.renderCropOverlay();
        };
        new PageResizeObserver(updateCropLayout).observe(editor.video);
        pageListen(window, 'resize', updateCropLayout);
      }
    }

    // Clip list DOM observer - when SSE patches the clip bank via PatchElementTempl,
    // delegate to ClipBank.scheduleReload() which debounces and re-fetches.
    // NOTE: We observe the *parent* [data-clip-bank] element, not [data-clip-list],
    // because WithModeReplace() replaces the entire [data-clip-list] element,
    // which would destroy any MutationObserver attached directly to it.
    {
      const clipBankEl = document.querySelector('[data-clip-bank]');
      if (clipBankEl) {
        new PageMutationObserver(() => {
          editor.clipBank.scheduleReload();
        }).observe(clipBankEl, { childList: true, subtree: true });
      }
    }

    // Signal bridge for _selectedClipId - delegated to ClipBank.
    // Driven by data-on-signal-patch in the template, which calls
    // clipBank.handleSignalPatch(). No polling loop needed.

    // DataStar signal watcher for seek position
    const seekObserver = new PageMutationObserver(() => {
      const seekTo = document.body.dataset.seekTo;
      if (seekTo !== undefined && seekTo !== '') {
        const timestamp = parseFloat(seekTo);
        if (!isNaN(timestamp)) {
          editor.videoElement.currentTime = timestamp;
          // Clear the signal after seeking
          delete document.body.dataset.seekTo;
        }
      }
    });

    seekObserver.observe(document.body, {
      attributes: true,
      attributeFilter: ['data-seek-to']
    });

    // Clip color live updates are driven by data-effect="...$clipColor..."
    // in the template, which calls editor.onClipColorChange(). No polling needed.

    // Autosave is driven by data-effect="...$_clipDirty..."
    // in the template, which calls editor.onAutosaveCheck(). No polling needed.
  }

  // Auto-initialise filter dial widgets when SSE patches add them
  autoInitFilterDials();

  if (document.readyState === 'loading') {
    pageListen(document, 'DOMContentLoaded', init);
  } else {
    init();
  }
})();
