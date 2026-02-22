/**
 * DragHandlerMixin - document-level mousemove/mouseup handlers for all
 * drag interactions: work-pan, overview-pan, clip trim, overview drag
 * (set/pan/resize), and work selection drag (set/move/resize).
 *
 * All drag state is consolidated into `this.drag` (a flat object with
 * `type` discriminant) set up in the constructor.  See cut-page.js
 * `resetDrag()` for the canonical shape.
 *
 * Applied to CutPageEditor.prototype via Object.assign.
 */

import { clamp, isFiniteNumber, timeFromEvent, setDragCursor } from './utils.js';

export const DragHandlerMixin = {

  handleDocumentMouseMove(e) {
    const d = this.drag;

    // --- Drag-to-pan on work timeline (middle-click or Alt+drag) ---
    if (d.type === 'work-pan' && this.workEl) {
      const dx = e.clientX - d.startX;
      const rect = this.workEl.getBoundingClientRect();
      if (rect.width > 0 && isFiniteNumber(d.origStart) && isFiniteNumber(d.origEnd)) {
        const windowSize = d.origEnd - d.origStart;
        const timeDelta = -(dx / rect.width) * windowSize;
        const maxStart = Math.max(0, this.duration - windowSize);
        const newStart = clamp(d.origStart + timeDelta, 0, maxStart);
        this.workStart = newStart;
        this.workEnd = newStart + windowSize;
        this.render();
      }
      return;
    }

    // --- Drag-to-pan on overview timeline (middle-click or Alt+drag) ---
    if (d.type === 'overview-pan' && this.overviewEl) {
      const dx = e.clientX - d.startX;
      const rect = this.overviewEl.getBoundingClientRect();
      if (rect.width > 0 && isFiniteNumber(d.origStart) && isFiniteNumber(d.origEnd)) {
        const ovSpan = d.origEnd - d.origStart;
        const timeDelta = -(dx / rect.width) * ovSpan;
        const maxStart = Math.max(0, this.duration - ovSpan);
        const newStart = clamp(d.origStart + timeDelta, 0, maxStart);
        this.overviewStart = newStart;
        this.overviewEnd = newStart + ovSpan;
        this.render();
      }
      return;
    }

    // --- Clip trim drag ---
    if (d.type === 'clip-trim' && this.workEl && isFiniteNumber(this.duration) && this.duration > 0) {
      const t = this.snapTime(timeFromEvent(this.workEl, e, this.workStart, this.workEnd), e);
      const clip = this.findClipByID(d.clipId);
      if (!clip || !d.trimSide) return;

      if (!isFiniteNumber(d.origStart) || !isFiniteNumber(d.origEnd) || d.origEnd <= d.origStart) return;

      const minSize = 0.05;
      if (d.trimSide === 'start') {
        const newStart = clamp(t, 0, Math.max(0, d.origEnd - minSize));
        if (Math.abs(newStart - d.origStart) > 0.0001) d.didMove = true;
        clip.startTs = newStart;
        this.inPoint = newStart;
        this.outPoint = d.origEnd;
        this.render();
      } else if (d.trimSide === 'end') {
        const newEnd = clamp(t, Math.min(this.duration, d.origStart + minSize), this.duration);
        if (Math.abs(newEnd - d.origEnd) > 0.0001) d.didMove = true;
        clip.endTs = newEnd;
        this.inPoint = d.origStart;
        this.outPoint = newEnd;
        this.render();
      }
      return;
    }

    // --- Overview drag (set / pan / resize / seek) ---
    if (d.type === 'overview' && this.overviewEl) {
      const dx = isFiniteNumber(d.startX) ? Math.abs(e.clientX - d.startX) : 0;
      const dy = isFiniteNumber(d.startY) ? Math.abs(e.clientY - d.startY) : 0;
      const dragThresholdPx = 8;

      if (!d.didDrag) {
        if (dx <= dragThresholdPx && dy <= dragThresholdPx) return;
        d.didDrag = true;
        this.suppressNextOverviewClick = true;
      }

      const t = timeFromEvent(this.overviewEl, e, this.overviewStart, this.overviewEnd);

      if (d.subMode === 'seek') {
        const seekT = clamp(t, 0, this.duration);
        this.workHeadTime = seekT;
        if (this.video) this.video.currentTime = seekT;
        return;
      }
      if (d.subMode === 'set') {
        this.setWorkWindow(d.anchor, t);
      } else if (
        d.subMode === 'pan' &&
        isFiniteNumber(d.anchor) &&
        isFiniteNumber(d.origStart) &&
        isFiniteNumber(d.origEnd)
      ) {
        const delta = t - d.anchor;
        const size = d.origEnd - d.origStart;
        const start = clamp(d.origStart + delta, 0, Math.max(0, this.duration - size));
        this.workStart = start;
        this.workEnd = start + size;
        this.render();
      } else if (
        (d.subMode === 'resize-left' || d.subMode === 'resize-right') &&
        isFiniteNumber(d.origStart) &&
        isFiniteNumber(d.origEnd)
      ) {
        const minSize = 5;
        if (d.subMode === 'resize-left') {
          const newStart = clamp(t, 0, Math.max(0, d.origEnd - minSize));
          this.workStart = newStart;
          this.workEnd = d.origEnd;
          if (this.workEnd - this.workStart < minSize) {
            this.workStart = Math.max(0, this.workEnd - minSize);
          }
          this.render();
        } else {
          const newEnd = clamp(t, Math.min(this.duration, d.origStart + minSize), this.duration);
          this.workStart = d.origStart;
          this.workEnd = newEnd;
          if (this.workEnd - this.workStart < minSize) {
            this.workEnd = Math.min(this.duration, this.workStart + minSize);
          }
          this.render();
        }
      }
    }

    // --- Start work selection drag only after the mouse moves past threshold ---
    if (d.type === 'work-pending' && this.workEl && isFiniteNumber(d.startX) && isFiniteNumber(d.startY) && isFiniteNumber(d.anchor)) {
      const dx = Math.abs(e.clientX - d.startX);
      const dy = Math.abs(e.clientY - d.startY);
      if (dx > 3 || dy > 3) {
        const t0 = d.anchor;
        const hit = d.hitTest || 'set';

        // Promote to selection drag
        this.drag = {
          type: 'selection', subMode: hit, anchor: t0,
          startX: d.startX, startY: d.startY,
          origStart: null, origEnd: null,
          clipId: null, trimSide: null, hitTest: null,
          didDrag: true, didMove: false,
        };
        this.suppressNextWorkClick = true;

        // Set forced cursor for the drag mode
        if (hit === 'set') setDragCursor('crosshair');
        else if (hit === 'move') setDragCursor('grabbing');
        else if (hit === 'resize-left' || hit === 'resize-right') setDragCursor('ew-resize');

        if (hit === 'move' || hit === 'resize-left' || hit === 'resize-right') {
          const sel = this.getSelectionRange();
          if (sel) {
            this.drag.origStart = sel.start;
            this.drag.origEnd = sel.end;
          } else {
            this.drag.subMode = 'set';
            this.inPoint = t0;
            this.outPoint = t0;
            this.render();
          }
        } else {
          this.inPoint = t0;
          this.outPoint = t0;
          this.render();
        }
      }
    }

    // --- Active work selection drag ---
    if (d.type === 'selection' && this.workEl) {
      const t = this.snapTime(timeFromEvent(this.workEl, e, this.workStart, this.workEnd), e);
      const mode = d.subMode || 'set';
      const minSize = 0.05;

      if (mode === 'set') {
        this.inPoint = d.anchor;
        this.outPoint = t;
        this.render();
      } else if (mode === 'move') {
        if (!isFiniteNumber(d.anchor) || !isFiniteNumber(d.origStart) || !isFiniteNumber(d.origEnd)) return;
        const size = d.origEnd - d.origStart;
        if (!isFiniteNumber(size) || size <= 0) return;
        const delta = t - d.anchor;
        const maxDur = isFiniteNumber(this.duration) && this.duration > 0 ? this.duration : Infinity;
        const start = clamp(d.origStart + delta, 0, Math.max(0, maxDur - size));
        const end = clamp(start + size, 0, maxDur);
        this.inPoint = start;
        this.outPoint = end;
        this.render();
      } else if (mode === 'resize-left') {
        if (!isFiniteNumber(d.origEnd)) return;
        const maxDur = isFiniteNumber(this.duration) && this.duration > 0 ? this.duration : Infinity;
        const end = clamp(d.origEnd, 0, maxDur);
        const start = clamp(t, 0, Math.max(0, end - minSize));
        this.inPoint = start;
        this.outPoint = end;
        this.render();
      } else if (mode === 'resize-right') {
        if (!isFiniteNumber(d.origStart)) return;
        const maxDur = isFiniteNumber(this.duration) && this.duration > 0 ? this.duration : Infinity;
        const start = clamp(d.origStart, 0, maxDur);
        const end = clamp(t, Math.min(maxDur, start + minSize), maxDur);
        this.inPoint = start;
        this.outPoint = end;
        this.render();
      }

      if (this.editMode && this.selectedClipId) {
        const clip = this.findClipByID(this.selectedClipId);
        if (clip && isFiniteNumber(this.inPoint) && isFiniteNumber(this.outPoint)) {
          const s = Math.min(this.inPoint, this.outPoint);
          const ee = Math.max(this.inPoint, this.outPoint);
          if (ee > s) {
            clip.startTs = s;
            clip.endTs = ee;
            d.didMove = true;
          }
        }
      }
    }
  },

  handleDocumentMouseUp() {
    const d = this.drag;

    if (d.type === 'work-pan' || d.type === 'overview-pan') {
      this.resetDrag();
      return;
    }

    if (d.type === 'overview') {
      if (d.didDrag) {
        this.suppressNextOverviewClick = true;
      }
      if (!d.didDrag && d.subMode !== 'set' && this.video && isFiniteNumber(d.anchor) && isFiniteNumber(this.duration) && this.duration > 0) {
        const t = clamp(d.anchor, 0, this.duration);
        this.workHeadTime = t;
        this.video.currentTime = t;
      }
      this.resetDrag();
      return;
    }

    if (d.type === 'selection') {
      const moved = d.didMove;
      this.resetDrag();
      if (this.selectedClipId && moved) {
        const clip = this.findClipByID(this.selectedClipId);
        if (isFiniteNumber(clip?.startTs) && isFiniteNumber(clip?.endTs) && clip.endTs > clip.startTs) {
          this.markPendingClipTiming(clip.startTs, clip.endTs);
        }
      }
      return;
    }

    if (d.type === 'clip-trim') {
      const id = d.clipId;
      const clip = this.findClipByID(id);
      const startTs = clip?.startTs;
      const endTs = clip?.endTs;
      const moved = d.didMove;
      this.resetDrag();
      if (moved) this.suppressNextWorkClick = true;
      if (moved && typeof id === 'string' && isFiniteNumber(startTs) && isFiniteNumber(endTs) && endTs > startTs) {
        this.markPendingClipTiming(startTs, endTs);
      }
      return;
    }

    // Reset work-pending (click without drag)
    if (d.type === 'work-pending') {
      this.resetDrag();
    }
  },
};
