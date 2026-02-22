import { clamp, isFiniteNumber, formatTimecode, formatFrameTimecode } from './utils.js';

/**
 * SeekThumbnails – manages spritesheet filmstrip bars and hover tooltips.
 *
 * Owns:
 *  • seek-manifest loading / VTT cache
 *  • filmstrip row rendering (overview + work timeline)
 *  • hover tooltip with spritesheet thumbnail + timecode
 *
 * Reads from editor: videoID, workEl, overviewEl, workStart/workEnd,
 *   overviewStart/overviewEnd, duration, videoFps, dragging flags, snapTime().
 *
 * @param {object} editor  CutPageEditor instance
 */
export class SeekThumbnails {
  constructor(editor) {
    this.editor = editor;

    // Manifest + VTT cache
    this.manifest = null;
    this.vttByLevel = new Map();
    this.loadingVttByLevel = new Map();

    // Tooltip DOM refs
    this.overviewTooltip = null;
    this.workTooltip = null;

    // Tooltip debounce / RAF state
    this._raf = null;
    this._showTimer = null;
    this._pendingKind = null;
    this._pendingEvt = null;

    // Filmstrip render sequence counter (cancels stale async renders)
    this._renderSeq = 0;
  }

  // ---------------------------------------------------------------------------
  // Manifest
  // ---------------------------------------------------------------------------

  async loadManifest() {
    const videoID = this.editor.videoID;
    if (!videoID) return;
    try {
      const res = await fetch(
        `/api/videos/${encodeURIComponent(videoID)}/seek/seek.json`,
        { headers: { Accept: 'application/json' } },
      );
      if (!res.ok) return;
      const manifest = await res.json();
      if (!manifest || !Array.isArray(manifest.levels) || manifest.levels.length === 0) return;
      this.manifest = manifest;
    } catch (_) {
      // Best-effort.
    }
  }

  // ---------------------------------------------------------------------------
  // VTT loading + parsing
  // ---------------------------------------------------------------------------

  async ensureVttLoaded(levelName) {
    if (!levelName || typeof levelName !== 'string') return null;
    if (this.vttByLevel.has(levelName)) return this.vttByLevel.get(levelName);
    if (this.loadingVttByLevel.has(levelName)) return this.loadingVttByLevel.get(levelName);

    const videoID = this.editor.videoID;
    const p = (async () => {
      try {
        const res = await fetch(
          `/api/videos/${encodeURIComponent(videoID)}/seek/levels/${encodeURIComponent(levelName)}/seek.vtt`,
          { headers: { Accept: 'text/vtt' } },
        );
        if (!res.ok) return null;
        const text = await res.text();
        const parsed = SeekThumbnails.parseVTT(text);
        if (parsed) this.vttByLevel.set(levelName, parsed);
        return parsed;
      } catch (_) {
        return null;
      } finally {
        this.loadingVttByLevel.delete(levelName);
      }
    })();

    this.loadingVttByLevel.set(levelName, p);
    return p;
  }

  /**
   * Parse a seek-thumbnail VTT file into cue objects.
   * Static so it can be tested without instantiation.
   */
  static parseVTT(text) {
    if (typeof text !== 'string') return null;
    const lines = text.replace(/\r/g, '').split('\n');
    const cues = [];
    let i = 0;

    const parseTime = (t) => {
      const m = t.match(/^(\d+):(\d\d):(\d\d)\.(\d\d\d)$/);
      if (!m) return null;
      const hh = Number(m[1]);
      const mm = Number(m[2]);
      const ss = Number(m[3]);
      const ms = Number(m[4]);
      if (![hh, mm, ss, ms].every((v) => isFiniteNumber(v))) return null;
      return hh * 3600 + mm * 60 + ss + ms / 1000;
    };

    while (i < lines.length) {
      const line = lines[i].trim();
      i++;
      if (!line || line.startsWith('WEBVTT') || line.startsWith('NOTE')) continue;
      if (!line.includes('-->')) continue;

      const parts = line.split('-->').map((s) => s.trim());
      const start = parseTime(parts[0]);
      const end = parseTime(parts[1]);
      if (start == null || end == null) continue;

      while (i < lines.length && !lines[i].trim()) i++;
      if (i >= lines.length) break;
      const payload = lines[i].trim();
      i++;

      const m = payload.match(/^(seek-\d{3}\.jpg)#xywh=(\d+),(\d+),(\d+),(\d+)$/);
      if (!m) continue;
      cues.push({
        start,
        end,
        sheet: m[1],
        x: Number(m[2]),
        y: Number(m[3]),
        w: Number(m[4]),
        h: Number(m[5]),
      });
    }

    return cues.length > 0 ? cues : null;
  }

  // ---------------------------------------------------------------------------
  // Level selection
  // ---------------------------------------------------------------------------

  chooseLevelForRange(startTime, endTime, widthPx) {
    const levels = this.manifest?.levels;
    if (!Array.isArray(levels) || levels.length === 0) return null;
    const w = isFiniteNumber(widthPx) && widthPx > 0 ? widthPx : 1;
    const secPerPx = Math.max(0.000001, (endTime - startTime) / w);
    const desiredInterval = secPerPx * 8;

    let best = null;
    let bestScore = Infinity;
    for (const lvl of levels) {
      const iv = Number(lvl?.interval_seconds);
      if (!isFinite(iv) || iv <= 0) continue;
      const score = Math.abs(iv - desiredInterval);
      if (score < bestScore) {
        bestScore = score;
        best = lvl;
      }
    }

    // If medium exists, bias slightly toward it for stability.
    const medium = levels.find((l) => (l?.name || '') === 'medium');
    return best || medium || levels[0];
  }

  // ---------------------------------------------------------------------------
  // Filmstrip rendering
  // ---------------------------------------------------------------------------

  async renderRow(kind, container, startTime, endTime, widthPx) {
    if (!container || !this.manifest) return;

    const rectWidth = isFiniteNumber(widthPx) && widthPx > 0 ? widthPx : 1;
    const lvl = this.chooseLevelForRange(startTime, endTime, rectWidth);
    const levelName = (lvl?.name || '').toString();
    if (!levelName) return;

    const seq = ++this._renderSeq;
    const cues = await this.ensureVttLoaded(levelName);
    if (seq !== this._renderSeq) return;
    if (!cues || cues.length === 0) return;

    const interval = Number(lvl?.interval_seconds);
    if (!isFinite(interval) || interval <= 0) return;

    const rowHeight = kind === 'work' ? 56 : 40;
    const sheetW = Number(lvl?.cols) * Number(lvl?.thumb_width);
    const sheetH = Number(lvl?.rows) * Number(lvl?.thumb_height);
    const thumbW = Number(lvl?.thumb_width);
    const thumbH = Number(lvl?.thumb_height);

    container.innerHTML = '';
    container.style.height = `${rowHeight}px`;

    const timeSpan = endTime - startTime;
    if (!isFiniteNumber(timeSpan) || timeSpan <= 0) return;

    const bgScale = rowHeight / thumbH;
    const bgW = sheetW * bgScale;
    const bgH = sheetH * bgScale;
    const scaledThumbW = thumbW * bgScale;

    const minThumbWidthPx = Math.max(scaledThumbW * 0.5, 20);
    const maxVisibleThumbs = Math.ceil(rectWidth / minThumbWidthPx);

    const startIdx = Math.floor(startTime / interval);
    const endIdx = Math.ceil(endTime / interval);
    const availableThumbs = endIdx - startIdx + 1;

    const step =
      availableThumbs > maxVisibleThumbs
        ? availableThumbs / maxVisibleThumbs
        : 1;

    const thumbWidthPx = ((step * interval) / timeSpan) * rectWidth;
    const videoID = this.editor.videoID;

    let count = 0;
    for (let i = 0; i < maxVisibleThumbs && count < 200; i++) {
      const idx = startIdx + Math.floor(i * step);
      if (idx < 0 || idx >= cues.length) continue;
      const cue = cues[idx];
      if (!cue) continue;

      const cueTime = idx * interval;
      const leftPct = (cueTime - startTime) / timeSpan;
      const leftPx = leftPct * rectWidth;

      if (leftPx + thumbWidthPx < 0 || leftPx > rectWidth) continue;

      const tile = document.createElement('div');
      tile.className = 'absolute top-0 bottom-0 overflow-hidden';
      tile.style.left = `${leftPx}px`;
      tile.style.width = `${thumbWidthPx}px`;
      tile.style.height = `${rowHeight}px`;

      const offsetX = (thumbWidthPx - scaledThumbW) / 2;

      tile.style.backgroundImage = `url(/api/videos/${encodeURIComponent(videoID)}/seek/levels/${encodeURIComponent(levelName)}/${encodeURIComponent(cue.sheet)})`;
      tile.style.backgroundRepeat = 'no-repeat';
      tile.style.backgroundPosition = `${offsetX - cue.x * bgScale}px ${-cue.y * bgScale}px`;
      if (isFinite(bgW) && isFinite(bgH) && bgW > 0 && bgH > 0) {
        tile.style.backgroundSize = `${bgW}px ${bgH}px`;
      }
      tile.style.zIndex = '1';
      tile.style.opacity = '0.9';
      tile.style.pointerEvents = 'none';

      container.appendChild(tile);
      count++;
    }
  }

  // ---------------------------------------------------------------------------
  // Seek tooltip
  // ---------------------------------------------------------------------------

  ensureTooltip(kind) {
    const el = kind === 'work' ? this.editor.workEl : this.editor.overviewEl;
    if (!el) return null;

    if (kind === 'work' && this.workTooltip) return this.workTooltip;
    if (kind !== 'work' && this.overviewTooltip) return this.overviewTooltip;

    const tip = document.createElement('div');
    tip.className = 'fixed z-50 pointer-events-none hidden';
    tip.style.transform = 'translate(-50%, -100%)';
    tip.style.marginTop = '-8px';

    const thumb = document.createElement('div');
    thumb.className = 'border-2 border-white/20 bg-black';
    thumb.style.backgroundRepeat = 'no-repeat';

    const label = document.createElement('div');
    label.className = 'mt-1 text-[10px] text-white/80 font-mono text-center';

    const frame = document.createElement('div');
    frame.className = 'text-[10px] text-white/50 font-mono text-center';

    const box = document.createElement('div');
    box.className = 'px-2 py-1 bg-black/90 border border-white/20';
    box.appendChild(thumb);
    box.appendChild(label);
    box.appendChild(frame);

    tip.appendChild(box);
    document.body.appendChild(tip);

    const obj = { tip, thumb, label, frame };
    if (kind === 'work') this.workTooltip = obj;
    else this.overviewTooltip = obj;
    return obj;
  }

  hideTooltip(kind) {
    if (this._showTimer) {
      clearTimeout(this._showTimer);
      this._showTimer = null;
    }
    this._pendingKind = null;
    this._pendingEvt = null;

    const obj = kind === 'work' ? this.workTooltip : this.overviewTooltip;
    if (!obj || !obj.tip) return;
    obj.tip.classList.add('hidden');
  }

  queueTooltipUpdate(kind, evt) {
    if (!this.manifest) return;
    const ed = this.editor;
    if (!isFiniteNumber(ed.duration) || ed.duration <= 0) return;
    if (ed.drag && ed.drag.type !== 'none' && ed.drag.type !== 'work-pending') {
      this.hideTooltip(kind);
      return;
    }
    if (evt && evt.shiftKey) {
      this.hideTooltip(kind);
      return;
    }

    this._pendingKind = kind;
    this._pendingEvt = evt;

    // If tooltip is already visible, update immediately via RAF
    const obj = kind === 'work' ? this.workTooltip : this.overviewTooltip;
    if (obj && obj.tip && !obj.tip.classList.contains('hidden')) {
      if (this._raf) return;
      this._raf = requestAnimationFrame(() => {
        this._raf = null;
        void this.updateTooltip(kind, evt);
      });
      return;
    }

    // Otherwise, delay showing by 300ms
    if (this._showTimer) return;
    this._showTimer = setTimeout(() => {
      this._showTimer = null;
      if (this._pendingKind === kind && this._pendingEvt) {
        void this.updateTooltip(kind, this._pendingEvt);
      }
    }, 300);
  }

  async updateTooltip(kind, evt) {
    const ed = this.editor;
    const el = kind === 'work' ? ed.workEl : ed.overviewEl;
    if (!el || !evt) return;
    if (!this.manifest) return;

    const tooltip = this.ensureTooltip(kind);
    if (!tooltip) return;

    const rect = el.getBoundingClientRect();
    const x = clamp(evt.clientX - rect.left, 0, rect.width);
    if (!isFiniteNumber(x)) {
      this.hideTooltip(kind);
      return;
    }

    const startTime = kind === 'work' ? ed.workStart : ed.overviewStart;
    const endTime = kind === 'work' ? ed.workEnd : ed.overviewEnd;
    if (!isFiniteNumber(startTime) || !isFiniteNumber(endTime) || endTime <= startTime) {
      this.hideTooltip(kind);
      return;
    }

    const lvl = this.chooseLevelForRange(startTime, endTime, rect.width);
    const levelName = (lvl?.name || '').toString();
    if (!levelName) {
      this.hideTooltip(kind);
      return;
    }

    const cues = await this.ensureVttLoaded(levelName);
    if (!cues || cues.length === 0) {
      this.hideTooltip(kind);
      return;
    }

    const pct = rect.width > 0 ? x / rect.width : 0;
    const rawTime = startTime + pct * (endTime - startTime);
    const t = ed.snapTime(rawTime, evt);

    const interval = Number(lvl?.interval_seconds);
    let idx = isFinite(interval) && interval > 0 ? Math.floor(t / interval) : 0;
    if (!isFinite(idx) || idx < 0) idx = 0;
    if (idx >= cues.length) idx = cues.length - 1;
    const cue = cues[idx];
    if (!cue) {
      this.hideTooltip(kind);
      return;
    }

    const videoID = ed.videoID;
    const sheetURL = `/api/videos/${encodeURIComponent(videoID)}/seek/levels/${encodeURIComponent(levelName)}/${encodeURIComponent(cue.sheet)}`;
    const sheetW = Number(lvl?.cols) * Number(lvl?.thumb_width);
    const sheetH = Number(lvl?.rows) * Number(lvl?.thumb_height);

    tooltip.thumb.style.width = `${cue.w}px`;
    tooltip.thumb.style.height = `${cue.h}px`;
    tooltip.thumb.style.backgroundImage = `url(${sheetURL})`;
    if (isFinite(sheetW) && isFinite(sheetH) && sheetW > 0 && sheetH > 0) {
      tooltip.thumb.style.backgroundSize = `${sheetW}px ${sheetH}px`;
    } else {
      tooltip.thumb.style.backgroundSize = '';
    }
    tooltip.thumb.style.backgroundPosition = `-${cue.x}px -${cue.y}px`;
    const fps = isFiniteNumber(ed.videoFps) && ed.videoFps > 0 ? ed.videoFps : 0;
    tooltip.label.textContent = formatTimecode(t);
    tooltip.frame.textContent =
      fps > 0
        ? `${formatFrameTimecode(t, fps)} • frame ${Math.floor(t * fps)} @ ${fps.toFixed(3)}fps`
        : 'frame -- @ -- fps';

    // Position and clamp.
    tooltip.tip.classList.remove('hidden');
    const tipW = tooltip.tip.offsetWidth || 0;
    let leftPx = rect.left + x;
    if (tipW > 0) {
      leftPx = Math.max(tipW / 2, Math.min(window.innerWidth - tipW / 2, leftPx));
    }
    tooltip.tip.style.left = `${leftPx}px`;
    const y = clamp(evt.clientY - rect.top, 0, rect.height);
    tooltip.tip.style.top = `${rect.top + y}px`;
  }
}
