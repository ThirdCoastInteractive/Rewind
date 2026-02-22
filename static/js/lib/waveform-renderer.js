import { clamp, isFiniteNumber } from './utils.js';

/**
 * WaveformRenderer - loads waveform peaks data and provides
 * canvas rendering + zero-crossing snap.
 *
 * @param {object} editor  CutPageEditor instance (for videoID access)
 */
export class WaveformRenderer {
  constructor(editor) {
    this.editor = editor;
    this.manifest = null;
    this.peaks = null;
  }

  /** Fetch waveform manifest + binary peaks for the current video. */
  async loadAssets() {
    const videoID = this.editor.videoID;
    if (!videoID) return;
    try {
      const res = await fetch(`/api/videos/${encodeURIComponent(videoID)}/waveform/waveform.json`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      const manifest = await res.json();
      if (!manifest || typeof manifest !== 'object') return;
      if (!manifest.peaks_path) return;

      const peaksRes = await fetch(`/api/videos/${encodeURIComponent(videoID)}/waveform/peaks.i16`, {
        headers: { 'Accept': 'application/octet-stream' }
      });
      if (!peaksRes.ok) return;
      const buf = await peaksRes.arrayBuffer();
      const peaks = new Int16Array(buf);
      if (!peaks || peaks.length === 0) return;

      this.manifest = manifest;
      this.peaks = peaks;
    } catch (_) {
      // Best-effort.
    }
  }

  /**
   * Find the nearest zero-crossing in the peaks data around a given time.
   * Returns the time of the crossing, or null if none found.
   */
  findNearestZeroCrossingTime(time, windowSeconds) {
    const peaks = this.peaks;
    const manifest = this.manifest;
    if (!peaks || !manifest) return null;

    const bucketMS = Number(manifest.bucket_ms);
    const bucketSec = isFinite(bucketMS) && bucketMS > 0 ? bucketMS / 1000 : 0;
    if (!bucketSec) return null;

    const idx = Math.round(time / bucketSec);
    if (!isFinite(idx)) return null;

    const windowBuckets = Math.max(1, Math.floor((windowSeconds || 0.25) / bucketSec));
    const maxIdx = peaks.length - 2;
    const clampIdx = (i) => Math.max(0, Math.min(maxIdx, i));

    let bestIdx = null;
    let bestDist = Infinity;
    for (let offset = 0; offset <= windowBuckets; offset++) {
      const i = clampIdx(idx - offset);
      const j = clampIdx(idx + offset);
      for (const k of [i, j]) {
        const a = peaks[k];
        const b = peaks[k + 1];
        if (!isFinite(a) || !isFinite(b)) continue;
        if (a === 0 || b === 0) continue;
        if ((a > 0 && b < 0) || (a < 0 && b > 0)) {
          const dist = Math.abs(k - idx);
          if (dist < bestDist) {
            bestDist = dist;
            bestIdx = k;
          }
        }
      }
      if (bestIdx != null) break;
    }

    if (bestIdx == null) return null;
    return bestIdx * bucketSec;
  }

  /**
   * Draw the waveform onto a canvas for the given time range.
   */
  drawToCanvas(canvas, startTime, endTime) {
    const peaks = this.peaks;
    const manifest = this.manifest;
    if (!peaks || !manifest) return;

    const bucketMS = Number(manifest.bucket_ms);
    const bucketSec = isFinite(bucketMS) && bucketMS > 0 ? bucketMS / 1000 : 0;
    if (!bucketSec) return;

    const ctx = canvas?.getContext?.('2d');
    if (!ctx) return;

    const w = canvas.width;
    const h = canvas.height;
    if (!w || !h) return;

    ctx.clearRect(0, 0, w, h);
    ctx.strokeStyle = 'rgba(255,255,255,0.85)';
    ctx.lineWidth = 1;

    const midY = h / 2;
    const dur = endTime - startTime;

    ctx.beginPath();
    for (let x = 0; x < w; x++) {
      const t0 = startTime + (x / w) * dur;
      const t1 = startTime + ((x + 1) / w) * dur;
      let i0 = Math.floor(t0 / bucketSec);
      let i1 = Math.floor(t1 / bucketSec);
      if (!isFinite(i0)) i0 = 0;
      if (!isFinite(i1)) i1 = i0;
      if (i0 < 0) i0 = 0;
      if (i1 < i0) i1 = i0;
      if (i0 >= peaks.length) break;
      if (i1 >= peaks.length) i1 = peaks.length - 1;

      let max = 0;
      for (let i = i0; i <= i1; i++) {
        const v = Math.abs(peaks[i] || 0);
        if (v > max) max = v;
      }

      const amp = clamp(max / 32767, 0, 1);
      const y = amp * (h * 0.45);
      ctx.moveTo(x + 0.5, midY - y);
      ctx.lineTo(x + 0.5, midY + y);
    }
    ctx.stroke();
  }
}
