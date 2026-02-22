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
        this.selectClip(clip, seekTime);
      });

      // clip:deselected - clear selection state
      this.clipBank.addEventListener('clip:deselected', () => {
        // Don't call clearSelectedClip() here - that would mergePatch the signal
        // back to empty, creating a loop. Just clear local JS state.
        this.selectedClipId = null;
        this.editMode = false;
        this.pendingClipStart = null;
        this.pendingClipEnd = null;
        this.pendingClipDirty = false;
        this.render();
      });
    }

    attach() {
      // Video element event listeners (loadedmetadata, timeupdate, play/pause/ended)
      this._attachVideoListeners();

      window.addEventListener('resize', () => this.render());
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
      document.addEventListener('mousemove', (e) => this.handleDocumentMouseMove(e));
      document.addEventListener('mouseup', () => this.handleDocumentMouseUp());

      // Buttons (set in/out, create, loop, transport, etc.)
      this._attachButtons();

      // Keyboard shortcuts (delegated to Controls class - see lib/keyboard.js)
      document.addEventListener('keydown', (e) => this.controls.handleKeyDown(e));
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
      await Promise.all([this.loadMarkers(), this.clipBank.reload(), this.seekThumbs.loadManifest(), this.loadWaveformAssets()]);
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
      const zeroCross = this.findNearestZeroCrossingTime(t, 0.5);
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
        const res = await fetch(`/api/videos/${encodeURIComponent(this.videoID)}/markers`, {
          headers: { 'Accept': 'application/json' }
        });
        if (!res.ok) return;
        this.markers = (await res.json()).map(normalizeMarker);
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
          this._formAutoSaveTimer = setTimeout(() => {
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
        new ResizeObserver(resizeCanvases).observe(
          document.querySelector('[data-audio-tools]') || document.body
        );
      }

      // Crop overlay resize observer
      if (editor.video && editor.cropSurfaceEl) {
        const updateCropLayout = () => {
          editor.updateCropSurfaceLayout();
          editor.renderCropOverlay();
        };
        new ResizeObserver(updateCropLayout).observe(editor.video);
        window.addEventListener('resize', updateCropLayout);
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
        new MutationObserver(() => {
          editor.clipBank.scheduleReload();
        }).observe(clipBankEl, { childList: true, subtree: true });
      }
    }

    // Signal bridge for _selectedClipId - delegated to ClipBank.
    // Driven by data-on-signal-patch in the template, which calls
    // clipBank.handleSignalPatch(). No polling loop needed.

    // DataStar signal watcher for seek position
    const seekObserver = new MutationObserver(() => {
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

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
