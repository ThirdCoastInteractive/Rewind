/**
 * WindowNavMixin - work/overview window management, selection hit-testing,
 * and range display helpers.
 *
 * Applied to CutPageEditor.prototype via Object.assign.
 * All methods use `this` to access editor state.
 */

import { clamp, isFiniteNumber, formatTime } from './utils.js';

export const WindowNavMixin = {

  getWorkSelectionHit(evt) {
    if (!this.workEl) return 'set';
    if (!evt) return 'set';

    const sel = this.getSelectionRange();
    if (!sel) return 'set';

    // Only consider selection interactions if selection is visible within the work window.
    const a = clamp(sel.start, this.workStart, this.workEnd);
    const b = clamp(sel.end, this.workStart, this.workEnd);
    if (!isFiniteNumber(a) || !isFiniteNumber(b) || b <= a) return 'set';

    const rect = this.workEl.getBoundingClientRect();
    const x = clamp(evt.clientX - rect.left, 0, rect.width);
    const windowSize = this.workEnd - this.workStart;
    if (!isFiniteNumber(windowSize) || windowSize <= 0) return 'set';

    const leftX = ((a - this.workStart) / windowSize) * rect.width;
    const rightX = ((b - this.workStart) / windowSize) * rect.width;

    const edgePx = 8;
    const nearLeft = Math.abs(x - leftX) <= edgePx;
    const nearRight = Math.abs(x - rightX) <= edgePx;
    const inside = x > leftX + edgePx && x < rightX - edgePx;

    if (nearLeft) return 'resize-left';
    if (nearRight) return 'resize-right';
    if (inside) return 'move';
    return 'set';
  },

  ensureDefaultWorkWindow() {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    if (isFiniteNumber(this.workEnd) && this.workEnd > this.workStart) return;

    const defaultSize = clamp(this.duration * 0.02, 60, 300);
    const center = isFiniteNumber(this.video?.currentTime) ? this.video.currentTime : 0;
    const start = clamp(center - defaultSize / 2, 0, Math.max(0, this.duration - defaultSize));
    const end = clamp(start + defaultSize, 0, this.duration);
    this.workStart = start;
    this.workEnd = end;
    this.renderRange();
  },

  setWorkWindow(a, b) {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    const start = clamp(Math.min(a, b), 0, this.duration);
    const end = clamp(Math.max(a, b), 0, this.duration);
    const minSize = 5;
    this.workStart = start;
    this.workEnd = Math.max(end, start + minSize);
    if (this.workEnd > this.duration) {
      this.workEnd = this.duration;
      this.workStart = Math.max(0, this.workEnd - minSize);
    }
    this.render();
  },

  panWorkWindow(fraction) {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    const size = this.workEnd - this.workStart;
    const delta = size * fraction;
    const start = clamp(this.workStart + delta, 0, Math.max(0, this.duration - size));
    this.workStart = start;
    this.workEnd = start + size;
    this.render();
  },

  zoomWorkWindow(scale) {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    const center = (this.workStart + this.workEnd) / 2;
    const size = clamp((this.workEnd - this.workStart) * scale, 1, this.duration);
    const start = clamp(center - size / 2, 0, Math.max(0, this.duration - size));
    this.workStart = start;
    this.workEnd = start + size;
    this.render();
  },

  // --- Overview zoom/pan ---

  ensureOverviewWindow() {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    if (isFiniteNumber(this.overviewEnd) && this.overviewEnd > this.overviewStart) return;
    this.overviewStart = 0;
    this.overviewEnd = this.duration;
  },

  zoomOverview(scale, centerTime) {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    this.ensureOverviewWindow();
    const center = isFiniteNumber(centerTime) ? centerTime : (this.overviewStart + this.overviewEnd) / 2;
    const minSize = Math.max(5, this.duration * 0.01); // never zoom to less than 1% of video
    const newSize = clamp((this.overviewEnd - this.overviewStart) * scale, minSize, this.duration);
    const start = clamp(center - newSize / 2, 0, Math.max(0, this.duration - newSize));
    this.overviewStart = start;
    this.overviewEnd = start + newSize;
    this.render();
  },

  panOverview(fraction) {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    this.ensureOverviewWindow();
    const size = this.overviewEnd - this.overviewStart;
    const delta = size * fraction;
    const start = clamp(this.overviewStart + delta, 0, Math.max(0, this.duration - size));
    this.overviewStart = start;
    this.overviewEnd = start + size;
    this.render();
  },

  resetOverviewZoom() {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return;
    this.overviewStart = 0;
    this.overviewEnd = this.duration;
    this.render();
  },

  isOverviewZoomed() {
    if (!isFiniteNumber(this.duration) || this.duration <= 0) return false;
    return this.overviewStart > 0.01 || (this.duration - this.overviewEnd) > 0.01;
  },

  renderRange() {
    if (!this.rangeEl) return;
    const fmt = (v) => (isFiniteNumber(v) ? v.toFixed(2) : '--');
    this.rangeEl.textContent = `In: ${fmt(this.inPoint)}  Out: ${fmt(this.outPoint)}  Work: ${formatTime(this.workStart)}–${formatTime(this.workEnd)}`;
  },

  getSelectionRange() {
    if (!isFiniteNumber(this.inPoint) || !isFiniteNumber(this.outPoint)) return null;
    const start = Math.min(this.inPoint, this.outPoint);
    const end = Math.max(this.inPoint, this.outPoint);
    if (!isFiniteNumber(start) || !isFiniteNumber(end) || end <= start) return null;
    const dur = isFiniteNumber(this.duration) && this.duration > 0 ? this.duration : null;
    return {
      start: clamp(start, 0, dur ?? start),
      end: clamp(end, 0, dur ?? end)
    };
  },
};
