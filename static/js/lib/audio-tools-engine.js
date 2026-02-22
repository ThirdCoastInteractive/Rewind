// ============================================================================
// AudioToolsEngine - Real-time audio visualisation (VU meters, spectrum, scope)
// ============================================================================

export class AudioToolsEngine {
  constructor(audioGraph) {
    this.audioGraph = audioGraph;
    this.analyserPre = null;   // before filter chain
    this.analyserPost = null;  // after filter chain (what the user hears)
    this.canvases = { meter: null, spectrum: null, scope: null };
    this._raf = null;
    this._running = false;
    this._peakHoldL = -Infinity;
    this._peakHoldR = -Infinity;
    this._peakHoldDecay = 0;
    this._peakClipL = false;
    this._peakClipR = false;
    this._clipClearTimer = null;
    // Smoothing for meter ballistics (IIR)
    this._smoothRmsL = -60;
    this._smoothRmsR = -60;
    this._smoothPeakL = -60;
    this._smoothPeakR = -60;
  }

  /** Bind canvas elements - call once after DOM is ready */
  attach(meterCanvas, spectrumCanvas, scopeCanvas) {
    this.canvases.meter = meterCanvas || null;
    this.canvases.spectrum = spectrumCanvas || null;
    this.canvases.scope = scopeCanvas || null;
  }

  /** Start the rAF render loop */
  start() {
    if (this._running) return;
    this._running = true;
    this._tick();
  }

  /** Stop rendering */
  stop() {
    this._running = false;
    if (this._raf) { cancelAnimationFrame(this._raf); this._raf = null; }
  }

  /** Ensure analysers are tapped into the audio graph */
  ensureAnalysers() {
    const ag = this.audioGraph;
    if (!ag || !ag.ctx) return false;
    const ctx = ag.ctx;

    if (!this.analyserPre) {
      this.analyserPre = ctx.createAnalyser();
      this.analyserPre.fftSize = 2048;
      this.analyserPre.smoothingTimeConstant = 0.8;
    }
    if (!this.analyserPost) {
      this.analyserPost = ctx.createAnalyser();
      this.analyserPost.fftSize = 2048;
      this.analyserPost.smoothingTimeConstant = 0.8;
    }
    return true;
  }

  /**
   * Called by AudioPreviewGraph.rebuild() after it rewires the chain.
   * Taps analysers into the audio graph for visualization.
   * Pre-analyser taps the source; post-analyser taps between last node and destination.
   */
  tap(source, lastNode, destination) {
    if (!this.ensureAnalysers()) return;
    try {
      // Disconnect any prior analyser connections
      try { this.analyserPre.disconnect(); } catch (_) {}
      try { this.analyserPost.disconnect(); } catch (_) {}

      // Pre-filter: parallel tap from source (doesn't interrupt the chain)
      source.connect(this.analyserPre);

      // Post-filter: insert analyser between lastNode and destination
      // When source === lastNode (no filters), source is already connected to destination,
      // so we disconnect that and route through the post-analyser instead.
      try { lastNode.disconnect(destination); } catch (_) {}
      lastNode.connect(this.analyserPost);
      this.analyserPost.connect(destination);
    } catch (_) {
      // Best-effort - may fail if nodes already connected
    }
  }

  /** Main render tick */
  _tick() {
    if (!this._running) return;
    this._raf = requestAnimationFrame(() => this._tick());
    this._drawMeter();
    this._drawSpectrum();
    this._drawScope();
  }

  /** Get CSS pixel dimensions (accounts for DPR scaling on the context) */
  _cssSize(canvas) {
    const dpr = window.devicePixelRatio || 1;
    return { w: canvas.width / dpr, h: canvas.height / dpr };
  }

  // ── VU Meters ──────────────────────────────────────────────────────────────
  _drawMeter() {
    const canvas = this.canvases.meter;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const { w: W, h: H } = this._cssSize(canvas);

    ctx.fillStyle = '#0a0a0a';
    ctx.fillRect(0, 0, W, H);

    const analyser = this.analyserPost || this.analyserPre;
    if (!analyser) { this._drawMeterOff(ctx, W, H); return; }

    const buf = new Float32Array(analyser.fftSize);
    analyser.getFloatTimeDomainData(buf);

    // Compute RMS + peak (treat as mono - stereo requires channel splitter which is complex)
    const half = buf.length >> 1;
    let sumSqL = 0, peakL = 0, sumSqR = 0, peakR = 0;
    for (let i = 0; i < half; i++) {
      const s = buf[i];
      sumSqL += s * s;
      if (Math.abs(s) > peakL) peakL = Math.abs(s);
    }
    for (let i = half; i < buf.length; i++) {
      const s = buf[i];
      sumSqR += s * s;
      if (Math.abs(s) > peakR) peakR = Math.abs(s);
    }
    const rmsL = Math.sqrt(sumSqL / half) || 1e-10;
    const rmsR = Math.sqrt(sumSqR / (buf.length - half)) || 1e-10;

    const dbRmsL = 20 * Math.log10(rmsL);
    const dbRmsR = 20 * Math.log10(rmsR);
    const dbPeakL = 20 * Math.log10(peakL || 1e-10);
    const dbPeakR = 20 * Math.log10(peakR || 1e-10);

    // IIR smoothing (attack fast, release slow)
    const aFast = 0.4, aSlow = 0.05;
    this._smoothRmsL += (dbRmsL - this._smoothRmsL) * (dbRmsL > this._smoothRmsL ? aFast : aSlow);
    this._smoothRmsR += (dbRmsR - this._smoothRmsR) * (dbRmsR > this._smoothRmsR ? aFast : aSlow);
    this._smoothPeakL += (dbPeakL - this._smoothPeakL) * (dbPeakL > this._smoothPeakL ? aFast : aSlow);
    this._smoothPeakR += (dbPeakR - this._smoothPeakR) * (dbPeakR > this._smoothPeakR ? aFast : aSlow);

    // Peak hold
    if (this._smoothPeakL > this._peakHoldL) { this._peakHoldL = this._smoothPeakL; this._peakHoldDecay = 30; }
    if (this._smoothPeakR > this._peakHoldR) { this._peakHoldR = this._smoothPeakR; this._peakHoldDecay = 30; }
    if (this._peakHoldDecay > 0) {
      this._peakHoldDecay--;
    } else {
      this._peakHoldL -= 0.5;
      this._peakHoldR -= 0.5;
    }

    // Clip indicators
    if (dbPeakL > -0.5) this._peakClipL = true;
    if (dbPeakR > -0.5) this._peakClipR = true;
    clearTimeout(this._clipClearTimer);
    this._clipClearTimer = setTimeout(() => { this._peakClipL = false; this._peakClipR = false; }, 2000);

    // Draw two vertical bars
    const barW = Math.floor((W - 16) / 2);
    const barX1 = 4;
    const barX2 = barX1 + barW + 8;
    const topY = 14;
    const botY = H - 4;
    const barH = botY - topY;

    // dB scale: -60 to 0
    const dbToY = (db) => topY + (1 - (Math.max(-60, Math.min(0, db)) + 60) / 60) * barH;
    const dbToH = (db) => botY - dbToY(db);

    this._drawSingleMeter(ctx, barX1, topY, barW, barH, botY, dbToY, dbToH,
      this._smoothRmsL, this._smoothPeakL, this._peakHoldL, this._peakClipL);
    this._drawSingleMeter(ctx, barX2, topY, barW, barH, botY, dbToY, dbToH,
      this._smoothRmsR, this._smoothPeakR, this._peakHoldR, this._peakClipR);

    // Labels
    ctx.fillStyle = '#666';
    ctx.font = '9px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('L', barX1 + barW / 2, 10);
    ctx.fillText('R', barX2 + barW / 2, 10);

    // dB scale ticks
    ctx.textAlign = 'right';
    ctx.fillStyle = '#444';
    for (const db of [0, -6, -12, -18, -24, -36, -48, -60]) {
      const y = dbToY(db);
      ctx.fillText(String(db), W - 1, y + 3);
      ctx.fillRect(barX1, y, barW, 1);
      ctx.fillRect(barX2, y, barW, 1);
    }
  }

  _drawSingleMeter(ctx, x, topY, w, barH, botY, dbToY, dbToH, rms, peak, peakHold, clip) {
    // RMS fill (gradient green→yellow→red)
    const grad = ctx.createLinearGradient(0, botY, 0, topY);
    grad.addColorStop(0, '#22c55e');   // green at bottom (-60)
    grad.addColorStop(0.6, '#eab308'); // yellow around -24
    grad.addColorStop(0.85, '#f97316'); // orange around -9
    grad.addColorStop(1, '#ef4444');   // red at top (0)
    ctx.fillStyle = '#1a1a1a';
    ctx.fillRect(x, topY, w, barH);
    ctx.fillStyle = grad;
    const rmsH = dbToH(rms);
    ctx.fillRect(x, botY - rmsH, w, rmsH);

    // Peak line (bright)
    const peakY = dbToY(peak);
    ctx.fillStyle = peak > -6 ? '#ef4444' : '#fff';
    ctx.fillRect(x, peakY, w, 2);

    // Peak hold line
    const holdY = dbToY(peakHold);
    ctx.fillStyle = '#fff8';
    ctx.fillRect(x, holdY, w, 1);

    // Clip indicator
    if (clip) {
      ctx.fillStyle = '#ef4444';
      ctx.fillRect(x, topY - 2, w, 3);
    }
  }

  _drawMeterOff(ctx, W, H) {
    ctx.fillStyle = '#333';
    ctx.font = '10px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('NO SIGNAL', W / 2, H / 2 + 3);
  }

  // ── Spectrum Analyser ──────────────────────────────────────────────────────
  _drawSpectrum() {
    const canvas = this.canvases.spectrum;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const { w: W, h: H } = this._cssSize(canvas);

    ctx.fillStyle = '#0a0a0a';
    ctx.fillRect(0, 0, W, H);

    const analyser = this.analyserPost || this.analyserPre;
    if (!analyser) return;

    const freqData = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(freqData);

    const sampleRate = analyser.context.sampleRate;
    const binCount = freqData.length;
    const padL = 28;
    const padR = 4;
    const padT = 4;
    const padB = 16;
    const plotW = W - padL - padR;
    const plotH = H - padT - padB;

    // Logarithmic frequency scale: 20Hz → 20kHz
    const minFreq = 20;
    const maxFreq = Math.min(20000, sampleRate / 2);
    const logMin = Math.log10(minFreq);
    const logMax = Math.log10(maxFreq);

    // Draw frequency grid lines
    ctx.strokeStyle = '#222';
    ctx.lineWidth = 1;
    ctx.font = '8px monospace';
    ctx.fillStyle = '#555';
    ctx.textAlign = 'center';
    for (const f of [50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000]) {
      if (f > maxFreq) continue;
      const x = padL + ((Math.log10(f) - logMin) / (logMax - logMin)) * plotW;
      ctx.beginPath(); ctx.moveTo(x, padT); ctx.lineTo(x, padT + plotH); ctx.stroke();
      const label = f >= 1000 ? (f / 1000) + 'k' : String(f);
      ctx.fillText(label, x, H - 2);
    }

    // dB grid
    ctx.textAlign = 'right';
    for (const db of [0, -12, -24, -36, -48]) {
      const y = padT + (1 - (db + 60) / 60) * plotH;
      ctx.beginPath(); ctx.moveTo(padL, y); ctx.lineTo(padL + plotW, y); ctx.stroke();
      ctx.fillText(String(db), padL - 2, y + 3);
    }

    // Draw spectrum as filled path
    const grad = ctx.createLinearGradient(0, padT + plotH, 0, padT);
    grad.addColorStop(0, 'rgba(34, 197, 94, 0.6)');
    grad.addColorStop(0.5, 'rgba(234, 179, 8, 0.6)');
    grad.addColorStop(1, 'rgba(239, 68, 68, 0.6)');

    ctx.beginPath();
    ctx.moveTo(padL, padT + plotH);

    // Walk across pixels, sample logarithmically
    for (let px = 0; px < plotW; px++) {
      const logF = logMin + (px / plotW) * (logMax - logMin);
      const freq = Math.pow(10, logF);
      const bin = Math.round(freq / (sampleRate / 2) * binCount);
      const clamped = Math.max(0, Math.min(binCount - 1, bin));
      const val = freqData[clamped] / 255;
      const y = padT + (1 - val) * plotH;
      ctx.lineTo(padL + px, y);
    }

    ctx.lineTo(padL + plotW, padT + plotH);
    ctx.closePath();
    ctx.fillStyle = grad;
    ctx.fill();

    // Stroke the top edge
    ctx.beginPath();
    for (let px = 0; px < plotW; px++) {
      const logF = logMin + (px / plotW) * (logMax - logMin);
      const freq = Math.pow(10, logF);
      const bin = Math.round(freq / (sampleRate / 2) * binCount);
      const clamped = Math.max(0, Math.min(binCount - 1, bin));
      const val = freqData[clamped] / 255;
      const y = padT + (1 - val) * plotH;
      if (px === 0) ctx.moveTo(padL + px, y);
      else ctx.lineTo(padL + px, y);
    }
    ctx.strokeStyle = '#22c55e';
    ctx.lineWidth = 1.5;
    ctx.stroke();
  }

  // ── Waveform Scope ─────────────────────────────────────────────────────────
  _drawScope() {
    const canvas = this.canvases.scope;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const { w: W, h: H } = this._cssSize(canvas);

    ctx.fillStyle = '#0a0a0a';
    ctx.fillRect(0, 0, W, H);

    const analyser = this.analyserPost || this.analyserPre;
    if (!analyser) return;

    const buf = new Float32Array(analyser.fftSize);
    analyser.getFloatTimeDomainData(buf);

    const padL = 4;
    const padR = 4;
    const plotW = W - padL - padR;
    const midY = H / 2;
    const amp = (H / 2) - 4;

    // Center line
    ctx.strokeStyle = '#333';
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(padL, midY);
    ctx.lineTo(padL + plotW, midY);
    ctx.stroke();

    // ±0.5 lines
    ctx.strokeStyle = '#222';
    ctx.beginPath();
    ctx.moveTo(padL, midY - amp * 0.5);
    ctx.lineTo(padL + plotW, midY - amp * 0.5);
    ctx.moveTo(padL, midY + amp * 0.5);
    ctx.lineTo(padL + plotW, midY + amp * 0.5);
    ctx.stroke();

    // Waveform
    ctx.beginPath();
    const step = buf.length / plotW;
    for (let px = 0; px < plotW; px++) {
      const i = Math.floor(px * step);
      const y = midY - buf[i] * amp;
      if (px === 0) ctx.moveTo(padL + px, y);
      else ctx.lineTo(padL + px, y);
    }

    // Color based on peak level
    let peak = 0;
    for (let i = 0; i < buf.length; i++) {
      if (Math.abs(buf[i]) > peak) peak = Math.abs(buf[i]);
    }
    ctx.strokeStyle = peak > 0.95 ? '#ef4444' : peak > 0.5 ? '#eab308' : '#22c55e';
    ctx.lineWidth = 1.5;
    ctx.stroke();

    // Labels
    ctx.fillStyle = '#444';
    ctx.font = '8px monospace';
    ctx.textAlign = 'left';
    ctx.fillText('+1', padL + 1, 10);
    ctx.fillText('-1', padL + 1, H - 3);
    ctx.fillText('0', padL + 1, midY - 2);
  }

  /** Clean up */
  destroy() {
    this.stop();
    if (this.analyserPre) { try { this.analyserPre.disconnect(); } catch (_) {} }
    if (this.analyserPost) { try { this.analyserPost.disconnect(); } catch (_) {} }
    this.analyserPre = null;
    this.analyserPost = null;
    this.canvases = { meter: null, spectrum: null, scope: null };
  }
}
