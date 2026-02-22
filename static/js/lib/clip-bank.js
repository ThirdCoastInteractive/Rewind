import { normalizeClip } from './utils.js';

/**
 * ClipBank - Event-driven clip data store.
 *
 * Replaces the setInterval polling loops and MutationObserver hack in the
 * monolithic init() with strongly-typed events driven by DataStar signal patches.
 *
 * Signal bridge wiring in the template:
 *   <div class="hidden"
 *        data-on-signal-patch="window.cutEditor?.clipBank?.handleSignalPatch()"
 *        data-on-signal-patch-filter="{include: /^(_selectedClipId|_clipDirty|_clipStartTs|_clipEndTs)$/}">
 *   </div>
 *
 * Clip list reload wiring (on backend DOM patch):
 *   <div data-clip-list ...
 *        data-on-signal-patch="window.cutEditor?.clipBank?.scheduleReload()">
 *   </div>
 *   - OR, if DOM-mutation based, keep the MutationObserver but delegate to clipBank.scheduleReload().
 *
 * Events emitted:
 *   clips:loaded    { clips: ClipData[] }              - after fetch or SSE-triggered reload
 *   clip:selected   { clip: ClipData, seekTime: number } - when _selectedClipId changes
 *   clip:deselected {}                                  - when _selectedClipId becomes empty
 *   clip:updated    { clip: ClipData }                  - after re-fetch finds updated clip
 *   clip:created    { clip: ClipData }                  - after re-fetch discovers new clip
 *   clip:deleted    { clipId: string }                  - after re-fetch finds clip removed
 */
export class ClipBank extends EventTarget {
  /**
   * @param {string} videoID - UUID of the video
   */
  constructor(videoID) {
    super();
    this._videoID = videoID;
    this._clips = [];
    this._selectedClipId = null;
    this._reloadTimer = null;
    this._lastSignals = {
      selectedClipId: '',
      clipDirty: false,
      clipStartTs: 0,
      clipEndTs: 0,
    };
  }

  // ---------------------------------------------------------------------------
  // Public read-only accessors
  // ---------------------------------------------------------------------------

  /** @returns {Array} Current clip list (normalized) */
  get clips() { return this._clips; }

  /** @returns {string|null} Currently selected clip ID */
  get selectedClipId() { return this._selectedClipId; }

  /**
   * Find a clip by ID.
   * @param {string} id
   * @returns {object|undefined}
   */
  getClipById(id) {
    return this._clips.find(c => c.id === id);
  }

  // ---------------------------------------------------------------------------
  // Data fetching
  // ---------------------------------------------------------------------------

  /**
   * Fetch clips from the API and emit appropriate events.
   * Called on init and whenever the clip list DOM is patched by SSE.
   */
  async reload() {
    const oldClips = this._clips;
    const oldIds = new Set(oldClips.map(c => c.id));

    try {
      const res = await fetch(
        `/api/videos/${encodeURIComponent(this._videoID)}/clips`,
        { headers: { 'Accept': 'application/json' } }
      );
      if (!res.ok) return;
      this._clips = (await res.json()).map(normalizeClip);
    } catch (_) {
      // Best-effort - network error shouldn't crash.
      return;
    }

    const newIds = new Set(this._clips.map(c => c.id));

    // Emit clips:loaded with the full new list.
    this.dispatchEvent(new CustomEvent('clips:loaded', {
      detail: { clips: this._clips },
    }));

    // Detect created clips (in new but not in old).
    for (const clip of this._clips) {
      if (!oldIds.has(clip.id)) {
        this.dispatchEvent(new CustomEvent('clip:created', {
          detail: { clip },
        }));
      }
    }

    // Detect deleted clips (in old but not in new).
    for (const clip of oldClips) {
      if (!newIds.has(clip.id)) {
        this.dispatchEvent(new CustomEvent('clip:deleted', {
          detail: { clipId: clip.id },
        }));
      }
    }

    // Detect updated clips (same ID, different data).
    for (const clip of this._clips) {
      if (oldIds.has(clip.id)) {
        const old = oldClips.find(c => c.id === clip.id);
        if (old && hasClipChanged(old, clip)) {
          this.dispatchEvent(new CustomEvent('clip:updated', {
            detail: { clip },
          }));
        }
      }
    }
  }

  /**
   * Schedule a reload with 50ms debounce.
   * Called when the clip list DOM is mutated by SSE PatchElements.
   */
  scheduleReload() {
    clearTimeout(this._reloadTimer);
    this._reloadTimer = setTimeout(() => this.reload(), 50);
  }

  // ---------------------------------------------------------------------------
  // Signal bridge
  // ---------------------------------------------------------------------------

  /**
   * Called when DataStar patches signals relevant to clips.
   * Reads current signal values from the DataStar API and fires events
   * for any state transitions.
   *
   * Wired via:
   *   data-on-signal-patch="window.cutEditor?.clipBank?.handleSignalPatch()"
   *   data-on-signal-patch-filter="{include: /^(_selectedClipId|_clipDirty|_clipStartTs|_clipEndTs)$/}"
   */
  handleSignalPatch() {
    const api = window.__dsAPI;
    if (!api) return;

    const selectedClipId = (api.getPath('_selectedClipId') || '').toString();
    const clipDirty = !!api.getPath('_clipDirty');
    const clipStartTs = parseFloat(api.getPath('_clipStartTs')) || 0;
    const clipEndTs = parseFloat(api.getPath('_clipEndTs')) || 0;

    const prev = this._lastSignals;

    // Detect selection change
    if (selectedClipId !== prev.selectedClipId) {
      prev.selectedClipId = selectedClipId;
      this._selectedClipId = selectedClipId || null;

      if (selectedClipId) {
        // Clip selected - find it, fire event (with retry for async clip loading)
        this._resolveSelection(selectedClipId);
      } else {
        // Clip deselected
        this.dispatchEvent(new CustomEvent('clip:deselected', { detail: {} }));
      }
    }

    // Track dirty and timing for downstream consumers (autosave, etc.)
    if (clipDirty !== prev.clipDirty) {
      prev.clipDirty = clipDirty;
      this.dispatchEvent(new CustomEvent('clip:dirty-changed', {
        detail: { dirty: clipDirty, clipId: this._selectedClipId },
      }));
    }

    if (clipStartTs !== prev.clipStartTs || clipEndTs !== prev.clipEndTs) {
      prev.clipStartTs = clipStartTs;
      prev.clipEndTs = clipEndTs;
      this.dispatchEvent(new CustomEvent('clip:timing-changed', {
        detail: { startTs: clipStartTs, endTs: clipEndTs, clipId: this._selectedClipId },
      }));
    }
  }

  // ---------------------------------------------------------------------------
  // Selection
  // ---------------------------------------------------------------------------

  /**
   * Resolve a clip selection, retrying if the clip hasn't been loaded yet.
   * Replaces the trySelect(10) retry loop from the old polling code.
   * @param {string} clipId
   * @param {number} [retries=10]
   */
  _resolveSelection(clipId, retries = 10) {
    const clip = this.getClipById(clipId);
    if (clip) {
      this.dispatchEvent(new CustomEvent('clip:selected', {
        detail: { clip, seekTime: clip.startTs },
      }));
      return;
    }

    // Clip not in local store yet - SSE may still be loading the DOM.
    if (retries > 0) {
      setTimeout(() => this._resolveSelection(clipId, retries - 1), 100);
    }
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Check if a clip's data has meaningfully changed (timing, color, title).
 */
function hasClipChanged(a, b) {
  return (
    a.startTs !== b.startTs ||
    a.endTs !== b.endTs ||
    a.color !== b.color ||
    a.title !== b.title
  );
}
