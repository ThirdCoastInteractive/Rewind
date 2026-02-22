/**
 * AttachMixin - timeline DOM event wiring extracted from attach().
 *
 * Provides _attachOverviewListeners() and _attachWorkListeners()
 * which are called from the main attach() method.
 *
 * Applied to CutPageEditor.prototype via Object.assign.
 */

import { clamp, isFiniteNumber, timeFromEvent, setDragCursor } from './utils.js';

export const AttachMixin = {

  _attachVideoListeners() {
    if (!this.video) return;

    this.video.addEventListener('loadedmetadata', () => {
      this.duration = this.video.duration;
      this.overviewStart = 0;
      this.overviewEnd = this.duration;
      this.ensureDefaultWorkWindow();
      if (isFiniteNumber(this.video.currentTime)) {
        this.workHeadTime = this.video.currentTime;
      } else {
        this.workHeadTime = 0;
      }
      this.render();
    });

    this.video.addEventListener('timeupdate', () => {
      this.renderPlayheads();
      this.updateTransportTime();
      this.handleSelectionPlaybackTick();
    });

    this.video.addEventListener('play', () => {
      this.renderPlaySelectionButton();
      this.updateTransportPlayButton();
    });
    this.video.addEventListener('pause', () => {
      this.renderPlaySelectionButton();
      this.updateTransportPlayButton();
    });
    this.video.addEventListener('ended', () => {
      if (this.transportLoopEnabled) {
        this.video.currentTime = 0;
        this.video.play().catch(() => {});
        return;
      }
      this.renderPlaySelectionButton();
      this.updateTransportPlayButton();
    });
  },

  _attachOverviewListeners() {
    if (!this.overviewEl) return;

    this.overviewEl.addEventListener('click', (e) => {
      if (this.suppressNextOverviewClick) {
        this.suppressNextOverviewClick = false;
        return;
      }
      if (!this.video || !isFiniteNumber(this.duration) || this.duration <= 0) return;
      const t = timeFromEvent(this.overviewEl, e, this.overviewStart, this.overviewEnd);
      this.workHeadTime = clamp(t, 0, this.duration);
      this.video.currentTime = clamp(t, 0, this.duration);
    });

    // Prevent default middle-click behavior (auto-scroll)
    this.overviewEl.addEventListener('auxclick', (e) => {
      if (e.button === 1) e.preventDefault();
    });

    this.overviewEl.addEventListener('mousedown', (e) => {
      if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
      e.preventDefault();

      if (e.button === 1 || (e.button === 0 && e.altKey && !e.shiftKey)) {
        this.drag = { type: 'overview-pan', subMode: null, anchor: null, startX: e.clientX, startY: null, origStart: this.overviewStart, origEnd: this.overviewEnd, clipId: null, trimSide: null, hitTest: null, didDrag: false, didMove: false };
        this.suppressNextOverviewClick = true;
        setDragCursor('grabbing');
        return;
      }

      // Default: drag pans the existing work window.
      // Shift+drag: drag out a new work window range.
      const ovAnchor = timeFromEvent(this.overviewEl, e, this.overviewStart, this.overviewEnd);
      this.workHeadTime = clamp(ovAnchor, 0, this.duration);

      let ovSubMode, ovOrigStart, ovOrigEnd;
      if (e.shiftKey) {
        ovSubMode = 'set';
        this.setWorkWindow(ovAnchor, ovAnchor);
        ovOrigStart = null;
        ovOrigEnd = null;
        this.suppressNextOverviewClick = true;
      } else {
        const hit = this.timeline.getOverviewWorkWindowHit(e);
        if (hit === 'resize-left') {
          ovSubMode = 'resize-left';
          ovOrigStart = this.workStart;
          ovOrigEnd = this.workEnd;
        } else if (hit === 'resize-right') {
          ovSubMode = 'resize-right';
          ovOrigStart = this.workStart;
          ovOrigEnd = this.workEnd;
        } else if (hit === 'move') {
          ovSubMode = 'pan';
          ovOrigStart = this.workStart;
          ovOrigEnd = this.workEnd;
        } else {
          ovSubMode = 'seek';
          ovOrigStart = null;
          ovOrigEnd = null;
        }
      }

      this.drag = {
        type: 'overview', subMode: ovSubMode, anchor: ovAnchor,
        startX: e.clientX, startY: e.clientY,
        origStart: ovOrigStart, origEnd: ovOrigEnd,
        clipId: null, trimSide: null, hitTest: null,
        didDrag: false, didMove: false,
      };

      // Set forced cursor for the drag operation
      if (ovSubMode === 'set' || ovSubMode === 'seek') setDragCursor('crosshair');
      else if (ovSubMode === 'pan') setDragCursor('grabbing');
      else if (ovSubMode === 'resize-left' || ovSubMode === 'resize-right') setDragCursor('ew-resize');
    });

    this.overviewEl.addEventListener('mousemove', (e) => {
      this.seekThumbs.queueTooltipUpdate('overview', e);

      // Don't update hover cursor during active drags (forced cursor handles it)
      if (this.drag.type !== 'none') return;

      if (e.shiftKey) {
        this.overviewPointerMode = 'set';
        this.overviewEl.style.cursor = 'crosshair';
        return;
      }

      const hit = this.timeline.getOverviewWorkWindowHit(e);
      this.overviewPointerMode = hit;

      if (hit === 'resize-left' || hit === 'resize-right') {
        this.overviewEl.style.cursor = 'ew-resize';
      } else if (e.target.closest('[data-clip-bar]')) {
        this.overviewEl.style.cursor = 'pointer';
      } else if (e.target.closest('[data-marker-el]')) {
        this.overviewEl.style.cursor = 'pointer';
      } else if (hit === 'move') {
        this.overviewEl.style.cursor = 'grab';
      } else {
        this.overviewEl.style.cursor = 'crosshair';
      }
    });

    this.overviewEl.addEventListener('mouseleave', () => this.seekThumbs.hideTooltip('overview'));

    // Wheel: Ctrl zooms overview, Shift/horizontal pans overview, plain zooms work window
    this.overviewEl.addEventListener('wheel', (e) => {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) {
        const rect = this.overviewEl.getBoundingClientRect();
        const x = clamp(e.clientX - rect.left, 0, rect.width);
        const pct = rect.width > 0 ? x / rect.width : 0.5;
        const ovSpan = this.overviewEnd - this.overviewStart;
        const centerTime = this.overviewStart + pct * ovSpan;
        const factor = e.deltaY > 0 ? 1.15 : 0.87;
        this.zoomOverview(factor, centerTime);
      } else if (e.shiftKey || e.deltaX !== 0) {
        const delta = (e.deltaX !== 0 ? e.deltaX : e.deltaY);
        this.panOverview(delta > 0 ? 0.05 : -0.05);
      } else {
        const factor = e.deltaY > 0 ? 1.15 : 0.87;
        this.zoomWorkWindow(factor);
      }
    }, { passive: false });
  },

  _attachWorkListeners() {
    if (!this.workEl) return;

    this.workEl.addEventListener('mousemove', (e) => {
      this.seekThumbs.queueTooltipUpdate('work', e);

      // Don't update hover cursor during active drags (forced cursor handles it)
      if (this.drag.type !== 'none' && this.drag.type !== 'work-pending') return;

      // Priority 1: selected clip bar edge → trim cursor
      const clipBar = e.target.closest('[data-clip-bar]');
      if (clipBar && clipBar.dataset.clipId === this.selectedClipId) {
        const trimHit = this.getTrimHitForBar(clipBar, e);
        if (trimHit === 'start' || trimHit === 'end') {
          this.workEl.style.cursor = 'ew-resize';
          return;
        }
      }

      // Priority 2: selection edge → resize cursor
      const selHit = this.getWorkSelectionHit(e);
      if (selHit === 'resize-left' || selHit === 'resize-right') {
        this.workEl.style.cursor = 'ew-resize';
      } else if (selHit === 'move') {
        // Priority 3: inside selection → grab cursor
        this.workEl.style.cursor = 'grab';
      } else if (clipBar) {
        // Priority 4: over any clip bar body → pointer
        this.workEl.style.cursor = 'pointer';
      } else if (e.target.closest('[data-marker-el]')) {
        // Priority 5: over marker → pointer
        this.workEl.style.cursor = 'pointer';
      } else {
        // Priority 6: empty space → crosshair (draw selection / seek)
        this.workEl.style.cursor = 'crosshair';
      }
    });

    // Prevent default middle-click behavior (auto-scroll)
    this.workEl.addEventListener('auxclick', (e) => {
      if (e.button === 1) e.preventDefault();
    });

    this.workEl.addEventListener('mousedown', (e) => {
      if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
      e.preventDefault();

      // Middle-click or Alt+Left: start drag-to-pan
      if (e.button === 1 || (e.button === 0 && e.altKey)) {
        this.drag = { type: 'work-pan', subMode: null, anchor: null, startX: e.clientX, startY: null, origStart: this.workStart, origEnd: this.workEnd, clipId: null, trimSide: null, hitTest: null, didDrag: false, didMove: false };
        this.suppressNextWorkClick = true;
        setDragCursor('grabbing');
        return;
      }

      const t = this.snapTime(timeFromEvent(this.workEl, e, this.workStart, this.workEnd), e);
      this.workHeadTime = clamp(t, 0, this.duration);

      // Check for clip bar edge trim on the selected clip
      const clipBar = e.target.closest('[data-clip-bar]');
      if (clipBar) {
        const clipId = clipBar.dataset.clipId;
        if (clipId && clipId === this.selectedClipId) {
          const trimHit = this.getTrimHitForBar(clipBar, e);
          if (trimHit === 'start' || trimHit === 'end') {
            const clip = this.findClipByID(clipId);
            if (clip && isFiniteNumber(clip.startTs) && isFiniteNumber(clip.endTs)) {
              this.drag = {
                type: 'clip-trim', subMode: null, anchor: t,
                startX: e.clientX, startY: e.clientY,
                origStart: clip.startTs, origEnd: clip.endTs,
                clipId: clipId, trimSide: trimHit,
                hitTest: null, didDrag: true, didMove: false,
              };
              this.suppressNextWorkClick = true;
              setDragCursor('ew-resize');
              return;
            }
          }
        }
      }

      this.drag = {
        type: 'work-pending', subMode: null, anchor: t,
        startX: e.clientX, startY: e.clientY,
        origStart: null, origEnd: null,
        clipId: null, trimSide: null,
        hitTest: this.getWorkSelectionHit(e),
        didDrag: false, didMove: false,
      };
    });

    this.workEl.addEventListener('click', (e) => {
      if (this.suppressNextWorkClick) {
        this.suppressNextWorkClick = false;
        return;
      }
      if (!this.video || !isFiniteNumber(this.duration) || this.duration <= 0) return;
      const t = this.snapTime(timeFromEvent(this.workEl, e, this.workStart, this.workEnd), e);
      this.workHeadTime = clamp(t, 0, this.duration);
      this.video.currentTime = clamp(t, 0, this.duration);
    });

    this.workEl.addEventListener('mouseleave', () => this.seekThumbs.hideTooltip('work'));

    // Wheel: Ctrl zooms, otherwise pans
    this.workEl.addEventListener('wheel', (e) => {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) {
        const factor = e.deltaY > 0 ? 1.15 : 0.87;
        this.zoomWorkWindow(factor);
      } else {
        const delta = (e.deltaX !== 0 ? e.deltaX : e.deltaY);
        this.panWorkWindow(delta > 0 ? 0.05 : -0.05);
      }
    }, { passive: false });
  },

  _attachButtons() {
    if (this.btnSetIn) {
      this.btnSetIn.addEventListener('click', () => {
        const t = isFiniteNumber(this.workHeadTime) ? this.workHeadTime : this.video?.currentTime;
        if (!isFiniteNumber(t)) return;
        this.inPoint = clamp(t, 0, this.duration || t);

        if (this.editMode && this.selectedClipId) {
          const clip = this.findClipByID(this.selectedClipId);
          if (clip) {
            clip.startTs = this.inPoint;
            const endTs = clip.endTs;
            if (isFiniteNumber(endTs) && endTs > this.inPoint) {
              this.markPendingClipTiming(this.inPoint, endTs);
            }
          }
        }

        this.renderRange();
        this.render();
      });
    }

    if (this.btnSetOut) {
      this.btnSetOut.addEventListener('click', () => {
        const t = isFiniteNumber(this.workHeadTime) ? this.workHeadTime : this.video?.currentTime;
        if (!isFiniteNumber(t)) return;
        this.outPoint = clamp(t, 0, this.duration || t);

        if (this.editMode && this.selectedClipId) {
          const clip = this.findClipByID(this.selectedClipId);
          if (clip) {
            clip.endTs = this.outPoint;
            const startTs = clip.startTs;
            if (isFiniteNumber(startTs) && this.outPoint > startTs) {
              this.markPendingClipTiming(startTs, this.outPoint);
            }
          }
        }

        this.renderRange();
        this.render();
      });
    }

    if (this.btnCreateClip) {
      this.btnCreateClip.addEventListener('click', () => this.createClipFromRange());
    }

    if (this.btnPlaySelection) {
      this.btnPlaySelection.addEventListener('click', () => this.togglePlaySelection());
      this.renderPlaySelectionButton();
    }

    if (this.btnLoop) {
      this.btnLoop.addEventListener('click', () => this.toggleLoop());
      this.renderLoopButton();
    }

    if (this.btnPanLeft) {
      this.btnPanLeft.addEventListener('click', () => this.panWorkWindow(-0.1));
    }
    if (this.btnPanRight) {
      this.btnPanRight.addEventListener('click', () => this.panWorkWindow(0.1));
    }
    if (this.btnZoomIn) {
      this.btnZoomIn.addEventListener('click', () => this.zoomWorkWindow(0.8));
    }
    if (this.btnZoomOut) {
      this.btnZoomOut.addEventListener('click', () => this.zoomWorkWindow(1.25));
    }
    if (this.btnToggleFilmstrip) {
      this.btnToggleFilmstrip.addEventListener('click', () => this.toggleFilmstrip());
    }

    // Transport controls
    if (this.btnTransportStart) {
      this.btnTransportStart.addEventListener('click', () => this.transportGoToStart());
    }
    if (this.btnTransportPrevFrame) {
      this.btnTransportPrevFrame.addEventListener('click', () => this.transportPrevFrame());
    }
    if (this.btnTransportStop) {
      this.btnTransportStop.addEventListener('click', () => this.transportStop());
    }
    if (this.btnTransportPlay) {
      this.btnTransportPlay.addEventListener('click', () => this.transportTogglePlay());
    }
    if (this.btnTransportNextFrame) {
      this.btnTransportNextFrame.addEventListener('click', () => this.transportNextFrame());
    }
    if (this.btnTransportEnd) {
      this.btnTransportEnd.addEventListener('click', () => this.transportGoToEnd());
    }
    if (this.btnTransportLoop) {
      this.btnTransportLoop.addEventListener('click', () => this.transportToggleLoop());
    }
  },
};
