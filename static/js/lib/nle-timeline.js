// nle-timeline.js — Canvas-based NLE timeline for stitch sequences
import { clamp } from './utils.js';

var COLORS = {
  clip:    { fill: '#3b82f6', dim: '#1e3a8a', bright: '#60a5fa' },
  video:   { fill: '#22c55e', dim: '#166534', bright: '#4ade80' },
  title:   { fill: '#f43f5e', dim: '#9f1239', bright: '#fb7185' },
  stitch:  { fill: '#f59e0b', dim: '#92400e', bright: '#fbbf24' },
  compose: { fill: '#a855f7', dim: '#6b21a8', bright: '#c084fc' },
};

function col(type) { return COLORS[type] || COLORS.clip; }

export class NLETimeline {
  constructor(editor) { this.ed = editor; }

  // ── Overview ───────────────────────────────────────────────────────────

  renderOverview() {
    var ed = this.ed;
    var canvas = ed.overviewCanvas;
    if (!canvas) return;
    var par = canvas.parentElement;
    var w = par.clientWidth, h = par.clientHeight;
    if (w <= 0 || h <= 0) return;

    var dpr = window.devicePixelRatio || 1;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    var ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);

    var tl = ed._timeline;
    if (!tl || tl.length === 0) return;
    var total = ed._totalDuration;
    if (total <= 0) return;

    var ovS = 0, ovE = total, ovSpan = total;
    var sel = ed.getSelectedIdx();

    for (var i = 0; i < tl.length; i++) {
      var seg = tl[i];
      var x1 = (seg.vStart / ovSpan) * w;
      var x2 = (seg.vEnd / ovSpan) * w;
      var sw = Math.max(2, x2 - x1 - 0.5);
      var c = col(seg.type);

      ctx.fillStyle = (i === sel) ? c.fill : c.dim;
      ctx.globalAlpha = (i === sel) ? 0.8 : 0.5;
      ctx.fillRect(x1 + 0.5, 1, sw, h - 2);
      ctx.globalAlpha = 1;

      if (i === sel) {
        ctx.strokeStyle = '#ffffff';
        ctx.lineWidth = 1.5;
        ctx.strokeRect(x1 + 1, 1, Math.max(1, sw), h - 2);
      }

      if (seg.transition) {
        var tx = (seg.transition.vTrStart / ovSpan) * w;
        ctx.fillStyle = '#eab308';
        ctx.globalAlpha = 0.8;
        ctx.fillRect(tx - 1, 0, 2, h);
        ctx.globalAlpha = 1;
      }
    }

    // Dim areas outside work window
    var wL = clamp((ed.workStart / ovSpan) * w, 0, w);
    var wR = clamp((ed.workEnd / ovSpan) * w, 0, w);
    ctx.fillStyle = 'rgba(0,0,0,0.45)';
    if (wL > 0) ctx.fillRect(0, 0, wL, h);
    if (wR < w) ctx.fillRect(wR, 0, w - wR, h);

    // Work window border
    ctx.strokeStyle = 'rgba(255,255,255,0.5)';
    ctx.lineWidth = 1;
    ctx.strokeRect(wL + 0.5, 0.5, wR - wL - 1, h - 1);

    // Resize handles
    ctx.fillStyle = 'rgba(255,255,255,0.3)';
    ctx.fillRect(wL, 0, 4, h);
    ctx.fillRect(wR - 4, 0, 4, h);

    // Playhead
    if (ed.playheadTime >= 0 && ed.playheadTime <= total) {
      var px = (ed.playheadTime / ovSpan) * w;
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(px - 1, 0, 2, h);
    }
  }

  // ── Work area ──────────────────────────────────────────────────────────

  renderWork() {
    var ed = this.ed;
    var canvas = ed.workCanvas;
    if (!canvas) return;
    var par = canvas.parentElement;
    var w = par.clientWidth, h = par.clientHeight;
    if (w <= 0 || h <= 0) return;

    var dpr = window.devicePixelRatio || 1;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    var ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);

    var tl = ed._timeline;
    if (!tl || tl.length === 0) {
      ctx.fillStyle = 'rgba(255,255,255,0.08)';
      ctx.font = '11px monospace';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText('Add clips to build your sequence', w / 2, h / 2);
      return;
    }

    var wS = ed.workStart, wE = ed.workEnd, wSpan = wE - wS;
    if (wSpan <= 0) return;

    var sel = ed.getSelectedIdx();
    var rulerH = 16;
    var bTop = rulerH + 2;
    var bH = Math.max(12, h - bTop - 2);

    this._drawRuler(ctx, w, rulerH, wS, wE);
    ctx.fillStyle = 'rgba(255,255,255,0.08)';
    ctx.fillRect(0, rulerH, w, 1);

    for (var i = 0; i < tl.length; i++) {
      var seg = tl[i];
      if (seg.vEnd <= wS || seg.vStart >= wE) continue;

      var x1 = clamp(((seg.vStart - wS) / wSpan) * w, -2, w + 2);
      var x2 = clamp(((seg.vEnd - wS) / wSpan) * w, -2, w + 2);
      var sw = Math.max(1, x2 - x1);
      var c = col(seg.type);
      var isSel = i === sel;

      // Transition overlap zone
      if (seg.transition) {
        var txS = clamp(((seg.transition.vTrStart - wS) / wSpan) * w, 0, w);
        var txE = clamp(((seg.transition.vTrEnd - wS) / wSpan) * w, 0, w);
        ctx.fillStyle = 'rgba(234,179,8,0.06)';
        ctx.fillRect(txS, bTop, txE - txS, bH);
      }

      // Block background
      ctx.fillStyle = isSel ? c.fill : c.dim;
      ctx.globalAlpha = isSel ? 0.5 : 0.3;
      ctx.fillRect(x1, bTop, sw, bH);
      ctx.globalAlpha = 1;

      // Left type bar
      ctx.fillStyle = c.fill;
      ctx.fillRect(x1, bTop, 3, bH);

      // Border
      ctx.strokeStyle = isSel ? 'rgba(255,255,255,0.6)' : 'rgba(255,255,255,0.12)';
      ctx.lineWidth = isSel ? 1.5 : 0.5;
      ctx.strokeRect(x1, bTop, sw, bH);

      // Trim handles
      if (isSel || i === ed._hoveredSeg) {
        ctx.fillStyle = isSel ? 'rgba(255,255,255,0.5)' : 'rgba(255,255,255,0.25)';
        ctx.fillRect(x1, bTop, 4, bH);
        ctx.fillRect(x2 - 4, bTop, 4, bH);
      }

      // Label
      if (sw > 28) {
        ctx.fillStyle = isSel ? 'rgba(255,255,255,0.9)' : 'rgba(255,255,255,0.5)';
        ctx.font = '10px monospace';
        ctx.textBaseline = 'top';
        ctx.textAlign = 'left';
        var lbl = seg.label || '(clip)';
        var maxW = sw - 18;
        while (ctx.measureText(lbl).width > maxW && lbl.length > 3) lbl = lbl.slice(0, -2) + '…';
        ctx.fillText(lbl, x1 + 6, bTop + 3);
      }

      // Duration
      if (sw > 44) {
        ctx.fillStyle = 'rgba(255,255,255,0.25)';
        ctx.font = '9px monospace';
        ctx.textBaseline = 'bottom';
        ctx.textAlign = 'left';
        ctx.fillText(seg.duration.toFixed(1) + 's', x1 + 6, bTop + bH - 2);
      }

      // Type badge
      if (sw > 18) {
        var badge = seg.type === 'title' ? 'T' : seg.type === 'video' ? 'V' : seg.type === 'compose' ? 'C' : seg.type === 'stitch' ? 'S' : 'P';
        ctx.fillStyle = c.bright;
        ctx.globalAlpha = 0.4;
        ctx.font = 'bold 9px monospace';
        ctx.textBaseline = 'top';
        ctx.textAlign = 'right';
        ctx.fillText(badge, x2 - 5, bTop + 3);
        ctx.globalAlpha = 1;
      }

      // Transition diamond
      if (seg.transition) {
        var tx = clamp(((seg.transition.vTrStart - wS) / wSpan) * w, 0, w);
        var cy = bTop + bH / 2;
        var d = 5;
        ctx.fillStyle = '#eab308';
        ctx.beginPath();
        ctx.moveTo(tx, cy - d);
        ctx.lineTo(tx + d, cy);
        ctx.lineTo(tx, cy + d);
        ctx.lineTo(tx - d, cy);
        ctx.closePath();
        ctx.fill();
      }
    }

    // Playhead
    var ph = ed.playheadTime;
    if (ph >= wS && ph <= wE) {
      var px = ((ph - wS) / wSpan) * w;
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(px - 1, 0, 2, h);
      ctx.beginPath();
      ctx.moveTo(px - 4, 0);
      ctx.lineTo(px + 4, 0);
      ctx.lineTo(px, 5);
      ctx.closePath();
      ctx.fill();
    }
  }

  _drawRuler(ctx, w, h, tStart, tEnd) {
    var span = tEnd - tStart;
    if (span <= 0) return;
    var target = Math.max(3, Math.floor(w / 80));
    var raw = span / target;
    var intervals = [0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300];
    var iv = intervals[0];
    for (var k = 0; k < intervals.length; k++) { if (intervals[k] >= raw) { iv = intervals[k]; break; } }

    ctx.fillStyle = 'rgba(255,255,255,0.35)';
    ctx.font = '9px monospace';
    ctx.textBaseline = 'bottom';
    ctx.textAlign = 'left';

    var t = Math.ceil(tStart / iv) * iv;
    while (t <= tEnd) {
      var x = ((t - tStart) / span) * w;
      ctx.fillRect(x, h - 4, 1, 4);
      var m = Math.floor(t / 60), s = Math.floor(t % 60);
      var lbl = iv < 1 ? t.toFixed(1) + 's' : m + ':' + (s < 10 ? '0' : '') + s;
      ctx.fillText(lbl, x + 2, h - 1);
      t += iv;
    }
  }

  // ── Hit testing ────────────────────────────────────────────────────────

  hitTestWork(e) {
    var ed = this.ed;
    var canvas = ed.workCanvas;
    if (!canvas) return { type: 'none' };
    var rect = canvas.getBoundingClientRect();
    var mx = e.clientX - rect.left, my = e.clientY - rect.top;
    var w = rect.width, h = rect.height;

    var tl = ed._timeline;
    if (!tl || tl.length === 0) return { type: 'seek', time: this._xToTime(mx, w) };

    var wS = ed.workStart, wE = ed.workEnd, wSpan = wE - wS;
    if (wSpan <= 0) return { type: 'none' };

    var rulerH = 16, bTop = rulerH + 2, bH = Math.max(12, h - bTop - 2);

    // Ruler click → seek
    if (my < rulerH) return { type: 'seek', time: this._xToTime(mx, w) };

    // Segment blocks
    if (my >= bTop && my <= bTop + bH) {
      for (var i = tl.length - 1; i >= 0; i--) {
        var seg = tl[i];
        if (seg.vEnd <= wS || seg.vStart >= wE) continue;
        var x1 = ((seg.vStart - wS) / wSpan) * w;
        var x2 = ((seg.vEnd - wS) / wSpan) * w;
        if (mx < x1 || mx > x2) continue;

        var edge = 6;
        if (mx <= x1 + edge) return { type: 'trim-start', segIdx: i, time: seg.vStart };
        if (mx >= x2 - edge) return { type: 'trim-end', segIdx: i, time: seg.vEnd };

        if (seg.transition) {
          var tx = ((seg.transition.vTrStart - wS) / wSpan) * w;
          if (Math.abs(mx - tx) < 8) return { type: 'transition', segIdx: i, time: seg.transition.vTrStart };
        }

        return { type: 'segment', segIdx: i, time: this._xToTime(mx, w) };
      }
    }

    return { type: 'seek', time: this._xToTime(mx, w) };
  }

  hitTestOverview(e) {
    var ed = this.ed;
    var canvas = ed.overviewCanvas;
    if (!canvas) return { type: 'seek', time: 0 };
    var rect = canvas.getBoundingClientRect();
    var mx = e.clientX - rect.left;
    var w = rect.width;

    var total = ed._totalDuration;
    if (total <= 0) return { type: 'seek', time: 0 };

    var time = (mx / w) * total;
    var wLx = (ed.workStart / total) * w;
    var wRx = (ed.workEnd / total) * w;
    var edge = 6;

    if (Math.abs(mx - wLx) <= edge) return { type: 'window-resize-left', time: time };
    if (Math.abs(mx - wRx) <= edge) return { type: 'window-resize-right', time: time };
    if (mx > wLx + edge && mx < wRx - edge) return { type: 'window-move', time: time };

    return { type: 'seek', time: time };
  }

  cursorForWorkHit(hit) {
    switch (hit.type) {
      case 'trim-start': case 'trim-end': return 'ew-resize';
      case 'transition': return 'pointer';
      case 'segment': return 'pointer';
      default: return 'crosshair';
    }
  }

  cursorForOverviewHit(hit) {
    switch (hit.type) {
      case 'window-resize-left': case 'window-resize-right': return 'ew-resize';
      case 'window-move': return 'grab';
      default: return 'crosshair';
    }
  }

  _xToTime(x, w) {
    var ed = this.ed;
    return ed.workStart + (x / w) * (ed.workEnd - ed.workStart);
  }
}
