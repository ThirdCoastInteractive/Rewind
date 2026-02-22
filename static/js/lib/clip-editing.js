/**
 * ClipEditingMixin - clip selection, editing, trimming nudges, split, create/delete,
 * and autosave helpers.
 *
 * Applied to CutPageEditor.prototype via Object.assign.
 * All methods use `this` to access editor state (selectedClipId, inPoint, outPoint, etc.).
 */

import { clamp, isFiniteNumber } from './utils.js';

export const ClipEditingMixin = {

  selectClipById(clipId) {
    if (!clipId || !this.clips) return;
    const clip = this.clips.find(c => c.id === clipId);
    if (!clip) return;
    this.selectClip(clip, clip.startTs);
  },

  selectClip(clip, seekTime) {
    const id = clip?.id;
    if (!id || typeof id !== 'string') return;

    const startTs = clip.startTs;
    const endTs = clip.endTs;
    if (isFiniteNumber(startTs) && isFiniteNumber(endTs) && endTs > startTs) {
      this.inPoint = startTs;
      this.outPoint = endTs;

      // Keep the work window centered around the clip.
      const pad = Math.min(10, (endTs - startTs) * 0.2);
      if (isFiniteNumber(this.duration) && this.duration > 0) {
        this.workStart = clamp(startTs - pad, 0, this.duration);
        this.workEnd = clamp(endTs + pad, 0, this.duration);
      }
    }

    this.selectedClipId = id;
    this.editMode = true; // selection is now attached to clip timing
    this.pendingClipStart = startTs;
    this.pendingClipEnd = endTs;
    this.pendingClipDirty = false;
    // Clip bank highlighting is driven by DataStar data-class reactive to $_selectedClipId.
    // Inspector visibility is now driven by data-show="$_selectedClipId" on the template.
    // No need for manual hidden-class toggling or synthetic row clicks.

    if (this.video && isFiniteNumber(seekTime)) {
      this.workHeadTime = clamp(seekTime, 0, this.duration || seekTime);
      this.video.currentTime = clamp(seekTime, 0, this.duration || seekTime);
    }

    this.render();
    this.queueInspectorFocus();
  },

  queueInspectorFocus() {
    this._inspectorFocusToken = (this._inspectorFocusToken || 0) + 1;
    const token = this._inspectorFocusToken;

    const attemptFocus = (triesLeft) => {
      if (token !== this._inspectorFocusToken) return;

      const formEl = document.querySelector('[data-cut-clip-form]');
      if (!formEl || formEl.classList.contains('hidden')) {
        if (triesLeft > 0) {
          setTimeout(() => attemptFocus(triesLeft - 1), 60);
        }
        return;
      }

      const active = document.activeElement;
      if (active && formEl.contains(active)) {
        return;
      }

      const target = formEl.querySelector('input, textarea, select, [contenteditable="true"]');
      if (target && typeof target.focus === 'function') {
        try {
          target.focus({ preventScroll: true });
        } catch (_) {
          target.focus();
        }
        return;
      }

      if (triesLeft > 0) {
        setTimeout(() => attemptFocus(triesLeft - 1), 60);
      }
    };

    attemptFocus(8);
  },

  clearSelectedClip() {
    this.selectedClipId = null;
    this.editMode = false;
    this.pendingClipStart = null;
    this.pendingClipEnd = null;
    this.pendingClipDirty = false;
    if (this.drag && this.drag.type === 'clip-trim') this.resetDrag();

    // Clear DataStar signals so stale values don't leak into future requests.
    // Use hidden inputs for clip-specific signals, mergePatch for the rest.
    this.setSignalInput('[data-cut-clip-dirty]', false);
    this.setSignalInput('[data-cut-clip-start-ts]', 0);
    this.setSignalInput('[data-cut-clip-end-ts]', 0);

    const api = window.__dsAPI;
    if (api) {
      api.mergePatch({
        _selectedClipId: '',
        _filterStack: [],
      });
    }

    // Inspector visibility is driven by data-show="$_selectedClipId" on the template.
    // Clearing the signal above makes the form hide and empty state show automatically.

    this.render();
  },

  enterEditMode() {
    if (!this.selectedClipId) return;
    this.editMode = true;
    this.render();
  },

  exitEditMode() {
    this.editMode = false;
    this.render();
  },

  nudgeInPoint(delta) {
    if (!isFiniteNumber(this.inPoint)) return;
    const newIn = clamp(this.inPoint + delta, 0, this.duration || Infinity);
    if (isFiniteNumber(this.outPoint) && newIn >= this.outPoint) return;
    this.inPoint = newIn;
    this.markPendingClipTiming(this.inPoint, this.outPoint);
    this.render();
  },

  nudgeOutPoint(delta) {
    if (!isFiniteNumber(this.outPoint)) return;
    const newOut = clamp(this.outPoint + delta, 0, this.duration || Infinity);
    if (isFiniteNumber(this.inPoint) && newOut <= this.inPoint) return;
    this.outPoint = newOut;
    this.markPendingClipTiming(this.inPoint, this.outPoint);
    this.render();
  },

  nudgeSelection(delta) {
    if (!isFiniteNumber(this.inPoint) || !isFiniteNumber(this.outPoint)) return;
    const dur = this.duration || Infinity;
    let newIn = this.inPoint + delta;
    let newOut = this.outPoint + delta;
    if (newIn < 0) { newOut -= newIn; newIn = 0; }
    if (newOut > dur) { newIn -= (newOut - dur); newOut = dur; }
    if (newIn < 0) newIn = 0;
    this.inPoint = newIn;
    this.outPoint = newOut;
    this.markPendingClipTiming(this.inPoint, this.outPoint);
    this.render();
  },

  async splitClipAtPlayhead() {
    if (!this.selectedClipId || !this.editMode) return;
    const playhead = this.video?.currentTime;
    if (!isFiniteNumber(playhead)) return;

    const clipStart = this.inPoint;
    const clipEnd = this.outPoint;
    if (!isFiniteNumber(clipStart) || !isFiniteNumber(clipEnd)) return;

    const frameDur = (this.videoFps > 0) ? (1 / this.videoFps) : (1 / 30);
    if (playhead <= clipStart + frameDur || playhead >= clipEnd - frameDur) {
      console.warn('Split point must be inside the clip bounds (at least 1 frame from edges)');
      return;
    }

    try {
      const res = await fetch(`/api/clips/${encodeURIComponent(this.selectedClipId)}/split`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ at: playhead }),
      });
      if (!res.ok) {
        const msg = await res.text();
        console.error('Split failed:', msg);
        return;
      }
      // Reload clips and exit edit mode
      this.exitEditMode();
      this.clearSelectedClip();
      await this.loadClipsForTimeline();
      this.render();
    } catch (err) {
      console.error('Split error:', err);
    }
  },

  async createClipFromRange() {
    if (!this.videoID) return;
    if (!isFiniteNumber(this.inPoint) || !isFiniteNumber(this.outPoint)) return;

    const start = Math.min(this.inPoint, this.outPoint);
    const end = Math.max(this.inPoint, this.outPoint);
    if (!isFiniteNumber(start) || !isFiniteNumber(end) || end <= start) return;

    const startInput = document.querySelector('[data-cut-create-start]');
    const endInput = document.querySelector('[data-cut-create-end]');
    const submitBtn = document.querySelector('[data-cut-create-submit]');

    if (!startInput || !endInput || !submitBtn) return;

    startInput.value = start;
    endInput.value = end;
    startInput.dispatchEvent(new Event('input', { bubbles: true }));
    endInput.dispatchEvent(new Event('input', { bubbles: true }));
    submitBtn.click();
  },

  // NOTE: exportClip and deleteClip removed – both are now handled by
  // DataStar actions in the template (confirm + @delete / @post for exports).

  markPendingClipTiming(startTs, endTs) {
    this.pendingClipStart = startTs;
    this.pendingClipEnd = endTs;
    this.pendingClipDirty = true;

    // Sync to DataStar signals via bound hidden inputs (most reliable path).
    this.setSignalInput('[data-cut-clip-start-ts]', startTs);
    this.setSignalInput('[data-cut-clip-end-ts]', endTs);
    this.setSignalInput('[data-cut-clip-dirty]', true);

    // Autosave: if enabled, debounce and trigger save after timing changes settle.
    this.scheduleAutoSave();
  },

  /**
   * Schedule an autosave if the _localAutoSave signal is enabled.
   * Uses a debounce to coalesce rapid changes (e.g. dragging in/out points).
   */
  scheduleAutoSave() {
    clearTimeout(this._autoSaveTimer);
    const api = window.__dsAPI;
    if (!api) return;
    if (!api.getPath('_localAutoSave')) return;

    this._autoSaveTimer = setTimeout(() => {
      const trigger = document.querySelector('[data-cut-autosave-trigger]');
      if (trigger) trigger.click();
    }, 600);
  },

  /**
   * Set a DataStar-bound hidden input's value and dispatch an 'input' event
   * so DataStar picks up the change via data-bind two-way binding.
   */
  setSignalInput(selector, value) {
    const el = document.querySelector(selector);
    if (!el) return;
    const strVal = typeof value === 'boolean' ? (value ? 'true' : '') : String(value);
    if (el.value !== strVal) {
      el.value = strVal;
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
  },
};
