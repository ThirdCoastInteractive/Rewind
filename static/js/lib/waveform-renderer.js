import { pageFetch } from './page-scope.js';
import { clamp, isFiniteNumber } from './utils.js';

// Lockstep with internal/waveform: DefaultRadiusSeconds, MinValleySeconds,
// QuietPercentile, QuietRangeShare, and score weights 0.55 / 0.25 / 0.20.
const DEFAULT_RADIUS_SECONDS = 5.0;
const MIN_VALLEY_SECONDS = 0.3;
const QUIET_PERCENTILE = 0.20;
const QUIET_RANGE_SHARE = 0.08;

/**
 * WaveformRenderer - loads waveform peaks data and provides
 * canvas rendering + quiet-valley boundary snap.
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
      const res = await pageFetch(`/api/videos/${encodeURIComponent(videoID)}/waveform/waveform.json`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      const manifest = await res.json();
      if (!manifest || typeof manifest !== 'object') return;
      if (!manifest.peaks_path) return;

      const peaksRes = await pageFetch(`/api/videos/${encodeURIComponent(videoID)}/waveform/peaks.i16`, {
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

  /** Return ranked low-amplitude valleys around an edit edge. */
  suggestBoundaryTimes(time, windowSeconds = 5, limit = 3) {
    const peaks = this.peaks;
    const manifest = this.manifest;
    if (!peaks || !manifest) return [];

    const bucketMS = Number(manifest.bucket_ms);
    const bucketSec = isFinite(bucketMS) && bucketMS > 0 ? bucketMS / 1000 : 0;
    if (!bucketSec) return [];

    const idx = Math.round(time / bucketSec);
    if (!isFinite(idx)) return [];
    const radius = windowSeconds > 0 ? windowSeconds : DEFAULT_RADIUS_SECONDS;
    const lo = Math.max(0, Math.floor((time - radius) / bucketSec));
    const hi = Math.min(peaks.length - 1, Math.ceil((time + radius) / bucketSec));
    const local = Array.from(peaks.slice(lo, hi + 1), v => Math.abs(Number(v) || 0)).sort((a, b) => a - b);
    if (!local.length) return [];
    const p20 = local[Math.floor((local.length - 1) * QUIET_PERCENTILE)];
    const threshold = p20 + (local[local.length - 1] - p20) * QUIET_RANGE_SHARE;
    const minBuckets = Math.max(1, Math.ceil(MIN_VALLEY_SECONDS / bucketSec));
    const fps = isFiniteNumber(this.editor.videoFps) && this.editor.videoFps > 0 ? this.editor.videoFps : 30;
    const out = [];
    for (let i = lo; i <= hi;) {
      if (Math.abs(peaks[i] || 0) > threshold) { i++; continue; }
      const start = i;
      let sum = 0;
      while (i <= hi && Math.abs(peaks[i] || 0) <= threshold) { sum += Math.abs(peaks[i] || 0); i++; }
      const count = i - start;
      if (count < minBuckets) continue;
      let candidateTime = ((start + i - 1) / 2) * bucketSec;
      candidateTime = Math.round(candidateTime * fps) / fps;
      const mean = sum / count;
      const duration = count * bucketSec;
      const normAmp = mean / Math.max(1, threshold);
      const distance = Math.abs(candidateTime - time) / radius;
      const score = (1 - normAmp) * 0.55 + Math.min(duration / 2, 1) * 0.25 + (1 - Math.min(distance, 1)) * 0.20;
      out.push({ time_seconds: candidateTime, score, mean_amplitude: mean, valley_duration: duration });
    }
    out.sort((a, b) => (b.score - a.score) || (Math.abs(a.time_seconds - time) - Math.abs(b.time_seconds - time)));
    return out.slice(0, limit);
  }

  /** Best quiet-valley time near an edit edge, or null. */
  suggestBoundaryTime(time, windowSeconds = 5) {
    return this.suggestBoundaryTimes(time, windowSeconds, 1)[0]?.time_seconds ?? null;
  }

  /** Compatibility alias for suggestBoundaryTime. */
  findNearestZeroCrossingTime(time, windowSeconds) {
    return this.suggestBoundaryTime(time, windowSeconds);
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
