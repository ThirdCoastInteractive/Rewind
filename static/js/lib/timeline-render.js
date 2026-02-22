import {
  clamp,
  isFiniteNumber,
  formatTime,
  timeFromEvent,
  computeContrastBorder,
} from './utils.js';

/**
 * Timeline – overview/work timeline rendering + playheads.
 *
 * Instantiated in CutPageEditor constructor: `this.timeline = new Timeline(this);`
 * Accessed via `this.timeline.renderOverview()` etc.
 */
export class Timeline {
  constructor(editor) {
    this.editor = editor;
  }

  renderOverview() {
    const ed = this.editor;
    if (!ed.overviewLayer || !isFiniteNumber(ed.duration) || ed.duration <= 0) return;
    ed.ensureOverviewWindow();
    const layer = ed.overviewLayer;
    layer.innerHTML = '';

    const mkDiv = (cls) => {
      const d = document.createElement('div');
      d.className = cls;
      return d;
    };

    const ovStart = ed.overviewStart;
    const ovEnd = ed.overviewEnd;
    const ovSpan = ovEnd - ovStart;
    if (!isFiniteNumber(ovSpan) || ovSpan <= 0) return;

    /** Map a time value to a percentage within the overview window */
    const pctOf = (t) => ((t - ovStart) / ovSpan) * 100;

    const thumbHeight = ed.seek?.manifest && ed.showFilmstrip ? 40 : 0;

    // Waveform underlay - render FIRST so it's behind everything
    if (ed.waveform?.peaks && ed.waveform?.manifest) {
      const canvas = document.createElement('canvas');
      canvas.className = 'absolute inset-0 pointer-events-none';
      canvas.style.width = '100%';
      canvas.style.height = '100%';
      canvas.style.opacity = '0.7';
      canvas.style.zIndex = '0';
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.floor(layer.clientWidth * dpr));
      canvas.height = Math.max(1, Math.floor(layer.clientHeight * dpr));
      ed.drawWaveformToCanvas(canvas, ovStart, ovEnd);
      layer.appendChild(canvas);
    }

    if (thumbHeight > 0) {
      const thumbRow = mkDiv('absolute left-0 right-0 top-0 pointer-events-none overflow-hidden');
      thumbRow.style.height = `${thumbHeight}px`;
      thumbRow.style.zIndex = '2';
      layer.appendChild(thumbRow);
      void ed.seekThumbs.renderRow('overview', thumbRow, ovStart, ovEnd, layer.clientWidth);
    }

    // Content layer fills entire height
    const content = mkDiv('absolute inset-0');
    content.style.zIndex = '1';
    layer.appendChild(content);

    // Clips ranges
    (ed.clips || []).forEach((cl) => {
      if (!isFiniteNumber(cl.startTs) || !isFiniteNumber(cl.endTs) || cl.endTs <= cl.startTs) return;
      if (cl.endTs <= ovStart || cl.startTs >= ovEnd) return;
      const left = pctOf(cl.startTs);
      const width = ((cl.endTs - cl.startTs) / ovSpan) * 100;
      const clipId = cl.id;
      const isSelected = ed.selectedClipId && clipId === ed.selectedClipId;

      const bar = mkDiv('absolute top-0 bottom-0');
      bar.style.left = `${left}%`;
      bar.style.width = `${width}%`;
      bar.dataset.clipBar = 'overview';
      if (clipId) bar.dataset.clipId = clipId;

      const clipColor = (isSelected ? ed.getLiveClipColor() : null) || cl.color.trim();
      if (clipColor) {
        bar.style.background = clipColor;
        bar.style.opacity = isSelected ? '0.6' : '0.4';
        if (isSelected) {
          bar.style.borderLeft = `2px solid ${clipColor}`;
          bar.style.borderRight = `2px solid ${clipColor}`;
          bar.style.boxSizing = 'border-box';
        }
      } else {
        bar.style.background = 'rgba(255,255,255,0.15)';
        if (isSelected) {
          bar.style.borderLeft = '2px solid rgba(255,255,255,0.6)';
          bar.style.borderRight = '2px solid rgba(255,255,255,0.6)';
          bar.style.boxSizing = 'border-box';
        }
      }

      if (cl.title) {
        bar.title = cl.title;
      }

      bar.addEventListener('click', (e) => {
        e.stopPropagation();
        const seekT = timeFromEvent(ed.overviewEl, e, ed.overviewStart, ed.overviewEnd);
        ed.workHeadTime = clamp(seekT, 0, ed.duration);
        ed.selectClip(cl, seekT);
      });

      content.appendChild(bar);
    });

    // Markers
    (ed.markers || []).forEach((m) => {
      if (!isFiniteNumber(m.timestamp) || m.timestamp < 0 || m.timestamp > ed.duration) return;
      const ts = m.timestamp;
      if (ts >= ovEnd) return;

      const markerColor = m.color.trim();

      const dur = m.duration;
      if (isFiniteNumber(dur) && dur > 0) {
        const end = Math.min(ts + dur, ed.duration);
        if (end <= ovStart) return;
        const left = pctOf(ts);
        const width = ((end - ts) / ovSpan) * 100;
        const range = mkDiv('absolute top-0 bottom-0 bg-white/20');
        range.style.left = `${left}%`;
        range.style.width = `${width}%`;
        if (markerColor) {
          range.style.background = markerColor;
          range.style.opacity = '0.35';
        }

        range.dataset.markerEl = '';
        range.addEventListener('click', (e) => {
          e.stopPropagation();
          ed.workHeadTime = clamp(ts, 0, ed.duration);
          if (ed.video) ed.video.currentTime = ts;
        });
        content.appendChild(range);
      } else {
        if (ts < ovStart) return;
        const left = pctOf(ts);
        const tick = mkDiv('absolute top-0 bottom-0 w-[2px] bg-white/40');
        tick.style.left = `${left}%`;
        if (markerColor) {
          tick.style.background = markerColor;
          tick.style.opacity = '0.8';
        }
        tick.dataset.markerEl = '';
        tick.addEventListener('click', (e) => {
          e.stopPropagation();
          ed.workHeadTime = clamp(ts, 0, ed.duration);
          if (ed.video) ed.video.currentTime = ts;
        });
        content.appendChild(tick);
      }
    });

    // Selection overlay (in/out points)
    if (isFiniteNumber(ed.inPoint) && isFiniteNumber(ed.outPoint)) {
      const selStart = clamp(Math.min(ed.inPoint, ed.outPoint), 0, ed.duration);
      const selEnd = clamp(Math.max(ed.inPoint, ed.outPoint), 0, ed.duration);
      if (selEnd > selStart && selEnd > ovStart && selStart < ovEnd) {
        const left = pctOf(selStart);
        const width = ((selEnd - selStart) / ovSpan) * 100;
        const sel = mkDiv('absolute top-0 bottom-0 bg-white/20 border-y-2 border-white/40');
        sel.style.left = `${left}%`;
        sel.style.width = `${width}%`;

        if (ed.selectedClipId) {
          const clip = this.findClipByID(ed.selectedClipId);
          const clipColor =
            ed.getLiveClipColor() || (clip?.color ?? clip?.Color ?? '').toString().trim();
          if (clipColor) {
            sel.style.background = clipColor;
            sel.style.opacity = '0.2';
            const bg = getComputedStyle(layer).backgroundColor || 'rgb(10,10,10)';
            sel.style.borderColor = computeContrastBorder(clipColor, bg, 0.2);
          }
        }
        sel.style.pointerEvents = 'none';
        content.appendChild(sel);
      }
    }

    // Work window overlay
    const winLeft = pctOf(ed.workStart);
    const winWidth = ((ed.workEnd - ed.workStart) / ovSpan) * 100;
    const win = mkDiv('absolute top-0 bottom-0 border-2 border-white/40 bg-white/5');
    win.style.left = `${winLeft}%`;
    win.style.width = `${winWidth}%`;
    win.style.pointerEvents = 'none';

    const winHandleLeft = mkDiv(
      'absolute top-0 bottom-0 w-2 bg-white/10 border-r-2 border-white/20',
    );
    winHandleLeft.style.left = '0';
    winHandleLeft.style.transform = 'translateX(-50%)';
    winHandleLeft.style.pointerEvents = 'none';
    const winHandleRight = mkDiv(
      'absolute top-0 bottom-0 w-2 bg-white/10 border-l-2 border-white/20',
    );
    winHandleRight.style.right = '0';
    winHandleRight.style.transform = 'translateX(50%)';
    winHandleRight.style.pointerEvents = 'none';
    win.appendChild(winHandleLeft);
    win.appendChild(winHandleRight);

    content.appendChild(win);

    // Label
    const label = mkDiv('absolute right-2 top-1 text-[10px] text-white/40 font-mono');
    label.textContent = `${formatTime(ovStart)}–${formatTime(ovEnd)}`;
    content.appendChild(label);

    // Minimap bar (visible when overview is zoomed)
    if (ed.isOverviewZoomed()) {
      const minimap = mkDiv('absolute left-0 right-0 bottom-0 h-1 bg-white/5');
      minimap.style.zIndex = '10';
      const viewLeft = (ovStart / ed.duration) * 100;
      const viewWidth = (ovSpan / ed.duration) * 100;
      const viewBar = mkDiv('absolute top-0 bottom-0 bg-white/30 rounded-sm');
      viewBar.style.left = `${viewLeft}%`;
      viewBar.style.width = `${viewWidth}%`;
      minimap.appendChild(viewBar);
      layer.appendChild(minimap);
    }
  }

  getOverviewWorkWindowHit(evt) {
    const ed = this.editor;
    if (!ed.overviewEl || !isFiniteNumber(ed.duration) || ed.duration <= 0) return 'seek';
    const ovSpan = ed.overviewEnd - ed.overviewStart;
    if (!isFiniteNumber(ovSpan) || ovSpan <= 0) return 'seek';

    const rect = ed.overviewEl.getBoundingClientRect();
    const x = clamp(evt.clientX - rect.left, 0, rect.width);

    const leftX = ((ed.workStart - ed.overviewStart) / ovSpan) * rect.width;
    const rightX = ((ed.workEnd - ed.overviewStart) / ovSpan) * rect.width;

    const edgePx = 8;
    const nearLeft = Math.abs(x - leftX) <= edgePx;
    const nearRight = Math.abs(x - rightX) <= edgePx;
    const inside = x > leftX + edgePx && x < rightX - edgePx;

    if (nearLeft) return 'resize-left';
    if (nearRight) return 'resize-right';
    if (inside) return 'move';
    return 'seek';
  }

  renderWork() {
    const ed = this.editor;
    if (!ed.workLayer || !isFiniteNumber(ed.duration) || ed.duration <= 0) return;
    const layer = ed.workLayer;
    layer.innerHTML = '';

    const windowSize = ed.workEnd - ed.workStart;
    if (!isFiniteNumber(windowSize) || windowSize <= 0) return;

    const mkDiv = (cls) => {
      const d = document.createElement('div');
      d.className = cls;
      return d;
    };

    const thumbHeight = ed.seek?.manifest && ed.showFilmstrip ? 56 : 0;
    if (thumbHeight > 0) {
      const thumbRow = mkDiv('absolute left-0 right-0 top-0 pointer-events-none overflow-hidden');
      thumbRow.style.height = `${thumbHeight}px`;
      layer.appendChild(thumbRow);
      void ed.seekThumbs.renderRow('work', thumbRow, ed.workStart, ed.workEnd, layer.clientWidth);
    }

    const content = mkDiv('absolute left-0 right-0 bottom-0');
    content.style.top = `${thumbHeight}px`;
    layer.appendChild(content);

    // Waveform underlay
    if (ed.waveform?.peaks && ed.waveform?.manifest) {
      const canvas = document.createElement('canvas');
      canvas.className = 'absolute inset-0 w-full h-full pointer-events-none';
      canvas.style.opacity = '0.5';
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.floor(layer.clientWidth * dpr));
      canvas.height = Math.max(1, Math.floor((layer.clientHeight - thumbHeight) * dpr));
      ed.drawWaveformToCanvas(canvas, ed.workStart, ed.workEnd);
      content.appendChild(canvas);
    }

    // Clips within window
    (ed.clips || []).forEach((cl) => {
      if (!isFiniteNumber(cl.startTs) || !isFiniteNumber(cl.endTs) || cl.endTs <= cl.startTs) return;
      if (cl.endTs < ed.workStart || cl.startTs > ed.workEnd) return;

      const a = clamp(cl.startTs, ed.workStart, ed.workEnd);
      const b = clamp(cl.endTs, ed.workStart, ed.workEnd);
      if (b <= a) return;

      const left = ((a - ed.workStart) / windowSize) * 100;
      const width = ((b - a) / windowSize) * 100;
      const isSelected = !!(cl.id && cl.id === ed.selectedClipId);

      const bar = mkDiv('absolute top-0 bottom-0');
      bar.style.left = `${left}%`;
      bar.style.width = `${width}%`;
      bar.dataset.clipBar = 'work';
      if (cl.id) bar.dataset.clipId = cl.id;

      const clipColor = (isSelected ? ed.getLiveClipColor() : null) || cl.color.trim();
      if (clipColor) {
        bar.style.background = clipColor;
        bar.style.opacity = isSelected ? '0.6' : '0.4';
      } else {
        bar.style.background = 'rgba(255,255,255,0.15)';
      }

      if (isSelected) {
        bar.style.zIndex = '1';
        bar.style.borderTop = '2px solid rgba(255,255,255,0.6)';
        bar.style.borderBottom = '2px solid rgba(255,255,255,0.6)';
        if (ed.editMode) {
          bar.style.borderTop = '2px solid rgba(255,200,50,0.8)';
          bar.style.borderBottom = '2px solid rgba(255,200,50,0.8)';
        }
      }

      bar.addEventListener('click', (e) => {
        e.stopPropagation();
        if (ed.suppressNextWorkClick) {
          ed.suppressNextWorkClick = false;
          return;
        }
        if (isSelected) return;
        const seekT = timeFromEvent(ed.workEl, e, ed.workStart, ed.workEnd);
        ed.selectClip(cl, seekT);
      });

      content.appendChild(bar);
    });

    // Markers within window
    (ed.markers || []).forEach((m) => {
      if (!isFiniteNumber(m.timestamp)) return;
      const ts = m.timestamp;

      const markerColor = m.color.trim();

      const dur = m.duration;
      if (isFiniteNumber(dur) && dur > 0) {
        const segStart = ts;
        const segEnd = ts + dur;
        if (segEnd < ed.workStart || segStart > ed.workEnd) return;

        const a = clamp(segStart, ed.workStart, ed.workEnd);
        const b = clamp(segEnd, ed.workStart, ed.workEnd);
        if (b <= a) return;

        const left = ((a - ed.workStart) / windowSize) * 100;
        const width = ((b - a) / windowSize) * 100;
        const range = mkDiv('absolute top-0 bottom-0 bg-white/20');
        range.style.left = `${left}%`;
        range.style.width = `${width}%`;
        if (markerColor) {
          range.style.background = markerColor;
          range.style.opacity = '0.35';
        }
        range.dataset.markerEl = '';
        range.addEventListener('click', (e) => {
          e.stopPropagation();
          ed.workHeadTime = clamp(ts, 0, ed.duration);
          if (ed.video) ed.video.currentTime = ts;
        });
        content.appendChild(range);
        return;
      }

      if (ts < ed.workStart || ts > ed.workEnd) return;

      const left = ((ts - ed.workStart) / windowSize) * 100;
      const tick = mkDiv('absolute top-0 bottom-0 w-[2px] bg-white/30');
      tick.style.left = `${left}%`;
      if (markerColor) {
        tick.style.background = markerColor;
        tick.style.opacity = '0.8';
      }
      tick.dataset.markerEl = '';
      tick.addEventListener('click', (e) => {
        e.stopPropagation();
        ed.workHeadTime = clamp(ts, 0, ed.duration);
        if (ed.video) ed.video.currentTime = ts;
      });
      content.appendChild(tick);
    });

    // Selection overlay
    if (isFiniteNumber(ed.inPoint) && isFiniteNumber(ed.outPoint)) {
      const a = clamp(Math.min(ed.inPoint, ed.outPoint), ed.workStart, ed.workEnd);
      const b = clamp(Math.max(ed.inPoint, ed.outPoint), ed.workStart, ed.workEnd);
      if (b > a) {
        const left = ((a - ed.workStart) / windowSize) * 100;
        const width = ((b - a) / windowSize) * 100;
        const sel = mkDiv('absolute top-0 bottom-0 bg-white/15 border-2 border-white/30');
        sel.style.left = `${left}%`;
        sel.style.width = `${width}%`;
        sel.style.pointerEvents = 'none';

        if (ed.selectedClipId) {
          const clip = this.findClipByID(ed.selectedClipId);
          const clipColor =
            ed.getLiveClipColor() || (clip?.color ?? clip?.Color ?? '').toString().trim();
          if (clipColor) {
            sel.style.background = clipColor;
            sel.style.opacity = '0.25';
            const bg = getComputedStyle(layer).backgroundColor || 'rgb(10,10,10)';
            sel.style.borderColor = computeContrastBorder(clipColor, bg, 0.25);
          }
        }

        const sL = mkDiv('absolute top-0 bottom-0 w-2 bg-white/10 border-r-2 border-white/20');
        sL.style.left = '0';
        sL.style.transform = 'translateX(-50%)';
        sL.style.pointerEvents = 'none';
        const sR = mkDiv('absolute top-0 bottom-0 w-2 bg-white/10 border-l-2 border-white/20');
        sR.style.right = '0';
        sR.style.transform = 'translateX(50%)';
        sR.style.pointerEvents = 'none';
        sel.appendChild(sL);
        sel.appendChild(sR);

        content.appendChild(sel);
      }
    }

    // Window label
    const label = mkDiv('absolute right-2 top-1 text-[10px] text-white/40 font-mono');
    label.textContent = `${formatTime(ed.workStart)}–${formatTime(ed.workEnd)}`;
    content.appendChild(label);
  }

  findClipByID(id) {
    if (!id || typeof id !== 'string') return null;
    return (this.editor.clips || []).find((c) => c?.id === id) || null;
  }

  getTrimHitForBar(barEl, evt) {
    if (!barEl) return 'none';
    const rect = barEl.getBoundingClientRect();
    const x = clamp(evt.clientX - rect.left, 0, rect.width);
    const edgePx = 8;
    if (x <= edgePx) return 'start';
    if (x >= rect.width - edgePx) return 'end';
    return 'none';
  }

  renderPlayheads() {
    const ed = this.editor;
    if (!ed.video || !isFiniteNumber(ed.duration) || ed.duration <= 0) return;
    const t = ed.video.currentTime;
    if (!isFiniteNumber(t) || t < 0) return;

    const updateLine = (layer, pct, dataAttr, className) => {
      if (!layer) return;
      let line = layer.querySelector(`[${dataAttr}]`);
      if (!line) {
        line = document.createElement('div');
        if (dataAttr === 'data-cut-playhead') {
          line.dataset.cutPlayhead = '1';
        } else if (dataAttr === 'data-cut-workhead') {
          line.dataset.cutWorkhead = '1';
        }
        line.className = className;
        layer.appendChild(line);
      }
      line.style.left = `${clamp(pct, 0, 100)}%`;
      line.style.display = 'block';
    };

    const ovSpan = ed.overviewEnd - ed.overviewStart;
    if (isFiniteNumber(ovSpan) && ovSpan > 0) {
      updateLine(
        ed.overviewLayer,
        ((t - ed.overviewStart) / ovSpan) * 100,
        'data-cut-playhead',
        'absolute top-0 bottom-0 w-[2px] bg-white',
      );

      if (isFiniteNumber(ed.workHeadTime)) {
        updateLine(
          ed.overviewLayer,
          ((ed.workHeadTime - ed.overviewStart) / ovSpan) * 100,
          'data-cut-workhead',
          'absolute top-0 bottom-0 w-[2px] bg-white/60',
        );
      }
    }

    const windowSize = ed.workEnd - ed.workStart;
    if (isFiniteNumber(windowSize) && windowSize > 0) {
      updateLine(
        ed.workLayer,
        ((t - ed.workStart) / windowSize) * 100,
        'data-cut-playhead',
        'absolute top-0 bottom-0 w-[2px] bg-white',
      );

      if (isFiniteNumber(ed.workHeadTime)) {
        const pct = ((ed.workHeadTime - ed.workStart) / windowSize) * 100;
        const clampedPct = clamp(pct, 0, 100);
        updateLine(
          ed.workLayer,
          clampedPct,
          'data-cut-workhead',
          'absolute top-0 bottom-0 w-[2px] bg-white/60',
        );
        const wh = ed.workLayer?.querySelector('[data-cut-workhead]');
        if (wh && (pct < 0 || pct > 100)) {
          wh.style.display = 'none';
        }
      }
    }
  }
}
