import { clamp, clampNumber, parseAspectRatio, isFiniteNumber } from './utils.js';

/**
 * CropOverlay - manages the crop rectangle UI on the video surface.
 *
 * @param {object} editor  CutPageEditor instance
 */
export class CropOverlay {
  constructor(editor) {
    this.editor = editor;

    // State
    this.crop = { x: 0.5, y: 0.5, width: 1, height: 1, aspect: '' };
    this.draggingCrop = false;
    this.cropDragMode = null;
    this.cropDragStart = null;
    this.selectedCropId = null;

    // DOM (set after construction by the editor)
    this.cropLayerEl = null;
    this.cropSurfaceEl = null;
    this.cropRectEl = null;
    this.cropHandleEl = null;
  }

  /** Bind DOM elements from the editor. */
  bindDOM(cropLayerEl, cropSurfaceEl, cropRectEl, cropHandleEl) {
    this.cropLayerEl = cropLayerEl;
    this.cropSurfaceEl = cropSurfaceEl;
    this.cropRectEl = cropRectEl;
    this.cropHandleEl = cropHandleEl;
  }

  getCropSurfaceAspectRatio() {
    const el = this.cropSurfaceEl;
    const w = el?.clientWidth || 0;
    const h = el?.clientHeight || 0;
    if (!w || !h) return 16 / 9;
    return w / h;
  }

  enforceAspectOnSize(width, height, ratio, surfaceAspect) {
    const minDim = 0.05;
    const maxDim = 1.0;

    const hFromW = (width * surfaceAspect) / ratio;
    const wFromH = (height * ratio) / surfaceAspect;

    let w = width;
    let h = hFromW;
    if (Number.isFinite(hFromW) && Number.isFinite(wFromH)) {
      const dKeepW = Math.abs(hFromW - height);
      const dKeepH = Math.abs(wFromH - width);
      if (dKeepH < dKeepW) {
        w = wFromH;
        h = height;
      }
    }

    if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) {
      return { width, height };
    }

    const down = Math.min(1, maxDim / w, maxDim / h);
    w *= down;
    h *= down;

    const up = Math.max(1, minDim / w, minDim / h);
    w *= up;
    h *= up;

    const down2 = Math.min(1, maxDim / w, maxDim / h);
    w *= down2;
    h *= down2;

    return { width: w, height: h };
  }

  clampCropToBounds(next) {
    const surfaceAspect = this.getCropSurfaceAspectRatio();

    let w = clampNumber(next.width, 0.05, 1.0, 1.0);
    let h = clampNumber(next.height, 0.05, 1.0, 1.0);

    const ratio = parseAspectRatio(next.aspect);
    if (ratio) {
      const sized = this.enforceAspectOnSize(w, h, ratio, surfaceAspect);
      w = sized.width;
      h = sized.height;
    }

    const x = clampNumber(next.x, w / 2, 1 - w / 2, 0.5);
    const y = clampNumber(next.y, h / 2, 1 - h / 2, 0.5);

    return {
      x,
      y,
      width: w,
      height: h,
      aspect: typeof next.aspect === 'string' ? next.aspect : ''
    };
  }

  setCropState(partial) {
    const next = {
      x: typeof partial.x !== 'undefined' ? partial.x : this.crop.x,
      y: typeof partial.y !== 'undefined' ? partial.y : this.crop.y,
      width: typeof partial.width !== 'undefined' ? partial.width : this.crop.width,
      height: typeof partial.height !== 'undefined' ? partial.height : this.crop.height,
      aspect: typeof partial.aspect !== 'undefined' ? partial.aspect : this.crop.aspect
    };
    this.crop = this.clampCropToBounds(next);
  }

  loadCrop(cropId, x, y, width, height, aspect) {
    this.setSelectedCropId(cropId);

    let aspectStr = typeof aspect === 'string' ? aspect.trim() : '';
    if (!aspectStr && isFiniteNumber(width) && isFiniteNumber(height) && height > 0) {
      const ratio = width / height;
      if (isFiniteNumber(ratio) && ratio > 0) {
        aspectStr = `${ratio.toFixed(4)}:1`;
      }
    }

    this.setCropState({ x, y, width, height, aspect: aspectStr });
    this.renderOverlay();
  }

  setSelectedCropId(cropId) {
    this.selectedCropId = cropId || null;

    const input = document.querySelector('[data-cut-selected-crop-id]');
    if (input) {
      input.value = this.selectedCropId || '';
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }
  }

  persistSelectedCrop() {
    if (!this.selectedCropId) return;

    const xInput = document.querySelector('[data-cut-crop-x]');
    const yInput = document.querySelector('[data-cut-crop-y]');
    const wInput = document.querySelector('[data-cut-crop-width]');
    const hInput = document.querySelector('[data-cut-crop-height]');
    const tokenInput = document.querySelector('[data-cut-crop-save-token]');

    if (!xInput || !yInput || !wInput || !hInput || !tokenInput) return;

    xInput.value = this.crop.x;
    yInput.value = this.crop.y;
    wInput.value = this.crop.width;
    hInput.value = this.crop.height;

    xInput.dispatchEvent(new Event('input', { bubbles: true }));
    yInput.dispatchEvent(new Event('input', { bubbles: true }));
    wInput.dispatchEvent(new Event('input', { bubbles: true }));
    hInput.dispatchEvent(new Event('input', { bubbles: true }));

    tokenInput.value = Date.now();
    tokenInput.dispatchEvent(new Event('input', { bubbles: true }));
  }

  updateSurfaceLayout() {
    const video = this.editor.video;
    if (!video || !this.cropSurfaceEl) return;
    const container = video.parentElement;
    if (!container) return;

    const elW = video.clientWidth;
    const elH = video.clientHeight;
    const vidW = video.videoWidth;
    const vidH = video.videoHeight;
    if (!elW || !elH || !vidW || !vidH) return;

    const scale = Math.min(elW / vidW, elH / vidH);
    const dispW = vidW * scale;
    const dispH = vidH * scale;
    const offsetX = (elW - dispW) / 2;
    const offsetY = (elH - dispH) / 2;

    this.cropSurfaceEl.style.left = `${offsetX}px`;
    this.cropSurfaceEl.style.top = `${offsetY}px`;
    this.cropSurfaceEl.style.width = `${dispW}px`;
    this.cropSurfaceEl.style.height = `${dispH}px`;
  }

  renderOverlay() {
    if (!this.cropRectEl || !this.cropLayerEl || !this.cropSurfaceEl) return;

    const isDefaultCrop = this.crop.width >= 0.99 && this.crop.height >= 0.99;
    const showCrop = !!this.selectedCropId && !isDefaultCrop;
    this.cropLayerEl.classList.toggle('hidden', !showCrop);
    if (!showCrop) return;

    const surfaceW = this.cropSurfaceEl.clientWidth;
    const surfaceH = this.cropSurfaceEl.clientHeight;
    if (!surfaceW || !surfaceH) return;

    const left = (this.crop.x - this.crop.width / 2) * surfaceW;
    const top = (this.crop.y - this.crop.height / 2) * surfaceH;
    const w = this.crop.width * surfaceW;
    const h = this.crop.height * surfaceH;

    this.cropRectEl.style.left = `${left}px`;
    this.cropRectEl.style.top = `${top}px`;
    this.cropRectEl.style.width = `${w}px`;
    this.cropRectEl.style.height = `${h}px`;
  }

  beginDrag(mode, evt) {
    if (!this.editor.selectedClipId) return;
    if (!this.cropSurfaceEl) return;
    if (!evt) return;

    evt.preventDefault();
    evt.stopPropagation();

    const rect = this.cropSurfaceEl.getBoundingClientRect();
    const startX = clamp(evt.clientX - rect.left, 0, rect.width);
    const startY = clamp(evt.clientY - rect.top, 0, rect.height);
    const nx = rect.width > 0 ? startX / rect.width : 0.5;
    const ny = rect.height > 0 ? startY / rect.height : 0.5;

    this.draggingCrop = true;
    this.cropDragMode = mode;

    const tlx = this.crop.x - this.crop.width / 2;
    const tly = this.crop.y - this.crop.height / 2;

    this.cropDragStart = {
      pointerId: evt.pointerId,
      startNx: nx,
      startNy: ny,
      orig: { ...this.crop },
      anchorTL: { x: tlx, y: tly }
    };

    const onMove = (e) => this.handlePointerMove(e);
    const onUp = (e) => {
      this.endDrag(e);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onUp);
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  }

  getCropGuides() {
    const phi = 0.61803398875;
    const g = new Set([
      0, 0.5, 1,
      1 / 3, 2 / 3,
      1 - phi, phi,
      1 / 9, 2 / 9, 4 / 9, 5 / 9, 7 / 9, 8 / 9
    ]);
    return Array.from(g).sort((a, b) => a - b);
  }

  snap1D(value, candidates, threshold) {
    let best = value;
    let bestD = threshold;
    for (const c of candidates) {
      const d = Math.abs(c - value);
      if (d <= bestD) {
        bestD = d;
        best = c;
      }
    }
    return best;
  }

  snapCenterOrEdges(center, size, guides, threshold) {
    const left = center - size / 2;
    const right = center + size / 2;

    let bestCenter = center;
    let bestD = threshold;

    for (const g of guides) {
      for (const target of [left, center, right]) {
        const d = Math.abs(target - g);
        if (d <= bestD) {
          bestD = d;
          bestCenter = center + (g - target);
        }
      }
    }

    return bestCenter;
  }

  handlePointerMove(evt) {
    if (!this.draggingCrop || !this.cropDragStart || !this.cropSurfaceEl) return;
    if (evt.pointerId !== this.cropDragStart.pointerId) return;

    evt.preventDefault();

    const rect = this.cropSurfaceEl.getBoundingClientRect();
    const x = clamp(evt.clientX - rect.left, 0, rect.width);
    const y = clamp(evt.clientY - rect.top, 0, rect.height);
    const nx = rect.width > 0 ? x / rect.width : 0.5;
    const ny = rect.height > 0 ? y / rect.height : 0.5;

    const guides = this.getCropGuides();
    const snapPx = 8;
    const snapX = rect.width > 0 ? snapPx / rect.width : 0;
    const snapY = rect.height > 0 ? snapPx / rect.height : 0;

    if (this.cropDragMode === 'move') {
      const dx = nx - this.cropDragStart.startNx;
      const dy = ny - this.cropDragStart.startNy;

      let nextX = this.cropDragStart.orig.x + dx;
      let nextY = this.cropDragStart.orig.y + dy;

      nextX = this.snapCenterOrEdges(nextX, this.cropDragStart.orig.width, guides, snapX);
      nextY = this.snapCenterOrEdges(nextY, this.cropDragStart.orig.height, guides, snapY);

      this.setCropState({ x: nextX, y: nextY });
      this.renderOverlay();
      return;
    }

    if (this.cropDragMode === 'resize') {
      const surfaceAspect = this.getCropSurfaceAspectRatio();
      const ratio = parseAspectRatio(this.crop.aspect);

      const tl = this.cropDragStart.anchorTL;
      let brx = nx;
      let bry = ny;

      brx = this.snap1D(brx, guides, snapX);
      bry = this.snap1D(bry, guides, snapY);

      let w = clampNumber(brx - tl.x, 0.05, 1.0, this.crop.width);
      let h = clampNumber(bry - tl.y, 0.05, 1.0, this.crop.height);

      if (ratio) {
        const sized = this.enforceAspectOnSize(w, h, ratio, surfaceAspect);
        w = sized.width;
        h = sized.height;
      }

      const maxW = 1 - tl.x;
      const maxH = 1 - tl.y;
      const down = Math.min(1, maxW / w, maxH / h);
      w *= down;
      h *= down;

      const nextX = tl.x + w / 2;
      const nextY = tl.y + h / 2;
      this.setCropState({ x: nextX, y: nextY, width: w, height: h });
      this.renderOverlay();
    }
  }

  endDrag(evt) {
    if (!this.draggingCrop) return;
    if (evt) {
      evt.preventDefault?.();
      evt.stopPropagation?.();
    }

    this.draggingCrop = false;
    this.cropDragMode = null;
    this.cropDragStart = null;

    this.persistSelectedCrop();
  }
}
