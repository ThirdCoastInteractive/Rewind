// ============================================================================
// STITCH PAGE — NLE sequence editor with global playhead, canvas timeline,
// trim handles, and continuous scrubbing preview.
// ============================================================================

import { SequencePlayback, TRANSITION_FX } from './lib/sequence-playback.js';
import { NLETimeline } from './lib/nle-timeline.js';
import { clamp } from './lib/utils.js';

class StitchEditor {
  constructor(root) {
    this.root = root;

    // DOM refs
    this.previewContainer = root.querySelector('.stitch-preview-container');
    this.video = document.getElementById('stitch-preview-video');
    this.titlePreview = document.getElementById('stitch-preview-title');
    this.titleText = document.getElementById('stitch-preview-title-text');
    this.titleSubtitle = document.getElementById('stitch-preview-title-subtitle');
    this.emptyPreview = document.getElementById('stitch-preview-empty');
    this.trPreview = document.getElementById('stitch-preview-transition');
    this.trFrom = document.getElementById('stitch-tr-from');
    this.trTo = document.getElementById('stitch-tr-to');
    this.trOverlay = document.getElementById('stitch-tr-overlay');
    this.trLabel = document.getElementById('stitch-tr-label');
    this.progressEl = document.getElementById('stitch-preview-progress');
    this.transportPlayBtn = document.getElementById('stitch-transport-play-btn');
    this.audioBtn = document.getElementById('stitch-audio-btn');
    this.timeEl = document.getElementById('stitch-transport-time');
    this.overviewCanvas = document.getElementById('stitch-overview-canvas');
    this.overviewEl = document.getElementById('stitch-overview');
    this.workCanvas = document.getElementById('stitch-work-canvas');
    this.workWrapper = document.getElementById('stitch-timeline-wrapper');

    // NLE state
    this.playheadTime = 0;
    this.workStart = 0;
    this.workEnd = 10;
    this._timeline = [];
    this._totalDuration = 0;
    this._hoveredSeg = -1;

    // Playback
    this.muted = false;
    this._playing = false;
    this._playRAF = null;
    this._playTitleTimer = null;
    this._seqPlayer = null;

    // Drag state
    this._drag = { type: 'none' };

    // Transition preview
    this._trRAF = null;
    this._trSegIdx = -1;
    this._trPopupCleanup = null;

    // Render scheduling
    this._renderRAF = null;

    // Auto-save
    this._saveTimer = null;
    this._saving = false;

    // Subsystems
    this.timeline = new NLETimeline(this);

    // Init
    this._updateAudioIcon();
    this._attachCanvasHandlers();
    this._attachKeyboard();
    this._exposeWindowFunctions();

    var self = this;
    requestAnimationFrame(function() {
      self._computeTimeline();
      self._initWorkWindow();
      self._updateTotalDuration();
      self.render();
    });
  }

  // ── DataStar helpers ──────────────────────────────────────────────────

  _ds() { return window.__dsAPI || null; }

  getSegments() {
    var api = this._ds();
    return api ? (api.getPath('_stitchSegments') || []) : [];
  }

  setSegments(segs) {
    var api = this._ds();
    if (api) api.mergePatch({ _stitchSegments: segs, _stitchDirty: true });
    this._computeTimeline();
    if (this.playheadTime > this._totalDuration) {
      this.playheadTime = Math.max(0, this._totalDuration);
    }
    this._updateTotalDuration();
    this.render();
  }

  getSelectedIdx() {
    var api = this._ds();
    return api ? (api.getPath('_stitchSelectedIdx') ?? -1) : -1;
  }

  setSelectedIdx(idx) {
    var api = this._ds();
    if (api) api.mergePatch({ _stitchSelectedIdx: idx });
  }

  // ── Virtual timeline ──────────────────────────────────────────────────

  _computeTimeline() {
    var segs = this.getSegments();
    var computed = [];
    var vOff = 0;

    for (var i = 0; i < segs.length; i++) {
      var s = segs[i];
      var dur = this._calcSegDuration(s);
      var tr = (i > 0 && s.transition && s.transition.type) ? s.transition : null;
      var trDur = (tr && tr.duration > 0) ? tr.duration : 0;

      if (i > 0 && trDur > 0) vOff -= trDur;

      var entry = {
        raw: s,
        index: i,
        type: s.type || 'clip',
        vStart: vOff,
        vEnd: vOff + dur,
        duration: dur,
        startTime: s.start_ts || 0,
        endTime: s.end_ts || ((s.start_ts || 0) + dur),
        label: s.title || s.text || '(clip)',
        transition: null,
      };

      if (trDur > 0) {
        entry.transition = {
          type: tr.type,
          duration: trDur,
          vTrStart: vOff,
          vTrEnd: vOff + trDur,
        };
      }

      computed.push(entry);
      vOff += dur;
    }

    this._timeline = computed;
    this._totalDuration = Math.max(0, vOff);
  }

  _findSegAtTime(vt) {
    var tl = this._timeline;
    if (!tl || tl.length === 0) return null;

    for (var i = tl.length - 1; i >= 0; i--) {
      if (vt >= tl[i].vStart && vt < tl[i].vEnd) {
        var seg = tl[i];
        var localTime = seg.raw.type === 'title'
          ? vt - seg.vStart
          : (seg.raw.start_ts || 0) + (vt - seg.vStart);
        return { index: i, seg: seg, localTime: localTime };
      }
    }

    if (vt >= this._totalDuration && tl.length > 0) {
      var last = tl[tl.length - 1];
      var lt = last.raw.type === 'title' ? last.duration : last.endTime;
      return { index: tl.length - 1, seg: last, localTime: lt };
    }

    if (tl.length > 0) return { index: 0, seg: tl[0], localTime: tl[0].startTime };
    return null;
  }

  _calcSegDuration(seg) {
    if (seg.type === 'title') return Math.max(0.1, seg.duration || 3);
    var start = seg.start_ts || 0;
    var end = seg.end_ts || (start + (seg.duration || 0));
    return Math.max(0.1, end - start);
  }

  // ── Work window ───────────────────────────────────────────────────────

  _initWorkWindow() {
    var dur = this._totalDuration;
    if (dur <= 0) { this.workStart = 0; this.workEnd = 10; return; }
    this.workStart = 0;
    this.workEnd = dur;
  }

  setWorkWindow(a, b) {
    var dur = this._totalDuration;
    if (dur <= 0) return;
    var start = clamp(Math.min(a, b), 0, dur);
    var end = clamp(Math.max(a, b), 0, dur);
    var minSize = Math.min(1, dur);
    this.workStart = start;
    this.workEnd = Math.max(end, start + minSize);
    if (this.workEnd > dur) {
      this.workEnd = dur;
      this.workStart = Math.max(0, this.workEnd - minSize);
    }
    this.render();
  }

  zoomWorkWindow(scale) {
    var dur = this._totalDuration;
    if (dur <= 0) return;
    var center = (this.workStart + this.workEnd) / 2;
    var size = clamp((this.workEnd - this.workStart) * scale, 0.5, dur);
    var start = clamp(center - size / 2, 0, Math.max(0, dur - size));
    this.workStart = start;
    this.workEnd = start + size;
    this.render();
  }

  panWorkWindow(fraction) {
    var dur = this._totalDuration;
    if (dur <= 0) return;
    var size = this.workEnd - this.workStart;
    var delta = size * fraction;
    var start = clamp(this.workStart + delta, 0, Math.max(0, dur - size));
    this.workStart = start;
    this.workEnd = start + size;
    this.render();
  }

  zoomIn() { this.zoomWorkWindow(0.7); }
  zoomOut() { this.zoomWorkWindow(1.4); }
  zoomReset() { this._initWorkWindow(); this.render(); }

  // ── Rendering ─────────────────────────────────────────────────────────

  render() {
    this.timeline.renderOverview();
    this.timeline.renderWork();
  }

  _scheduleRender() {
    if (this._renderRAF) return;
    var self = this;
    this._renderRAF = requestAnimationFrame(function() {
      self._renderRAF = null;
      self.render();
    });
  }

  _updateTimeDisplay() {
    if (this.timeEl) {
      this.timeEl.textContent = this._formatTime(this.playheadTime) + ' / ' + this._formatTime(this._totalDuration);
    }
    if (this.progressEl) {
      var pct = this._totalDuration > 0 ? (this.playheadTime / this._totalDuration * 100) : 0;
      this.progressEl.style.width = pct + '%';
    }
  }

  _updateTotalDuration() {
    var el = document.getElementById('stitch-total-duration');
    if (el) el.textContent = this._totalDuration > 0 ? '~' + this._formatTime(this._totalDuration) : '';
  }

  // ── Preview / Scrub ───────────────────────────────────────────────────

  _scrubTo(vt) {
    vt = clamp(vt, 0, this._totalDuration);
    this.playheadTime = vt;

    var segInfo = this._findSegAtTime(vt);
    if (!segInfo) { this._showEmpty(); this._updateTimeDisplay(); this._scheduleRender(); return; }

    if (this.getSelectedIdx() !== segInfo.index) this.setSelectedIdx(segInfo.index);

    if (segInfo.seg.raw.type === 'title') {
      this._showTitleForScrub(segInfo.seg.raw);
    } else {
      this._seekVideoForScrub(segInfo.seg.raw, segInfo.localTime);
    }

    this._updateTimeDisplay();
    this._scheduleRender();
  }

  _seekVideoForScrub(rawSeg, localTime) {
    if (this.titlePreview) { this.titlePreview.classList.add('hidden'); this.titlePreview.style.display = 'none'; }
    if (this.emptyPreview) this.emptyPreview.classList.add('hidden');
    if (this.trPreview) this.trPreview.classList.add('hidden');
    if (!this.video) return;
    this.video.classList.remove('hidden');
    var src = this._streamUrl(rawSeg);
    if (this.video.getAttribute('data-current-src') !== src) {
      this.video.setAttribute('data-current-src', src);
      this.video.src = src;
    }
    this.video.currentTime = localTime;
    this.video.muted = this.muted;
  }

  _showTitleForScrub(rawSeg) {
    if (this.video) { this.video.classList.add('hidden'); this.video.pause(); }
    if (this.emptyPreview) this.emptyPreview.classList.add('hidden');
    if (this.trPreview) this.trPreview.classList.add('hidden');
    if (this.titlePreview) {
      this.titlePreview.classList.remove('hidden');
      this.titlePreview.style.display = 'flex';
    }
    this._renderTitlePreview(rawSeg);
  }

  _showEmpty() {
    if (this.video) { this.video.classList.add('hidden'); this.video.pause(); this.video.removeAttribute('src'); this.video.removeAttribute('data-current-src'); }
    if (this.titlePreview) { this.titlePreview.classList.add('hidden'); this.titlePreview.style.display = 'none'; }
    if (this.emptyPreview) this.emptyPreview.classList.remove('hidden');
    if (this.trPreview) this.trPreview.classList.add('hidden');
  }

  _renderTitlePreview(seg) {
    var el = this.titlePreview;
    if (!el || !this.titleText) return;
    el.style.backgroundColor = seg.bg_color || '#000000';
    if (seg.position === 'top-center') { el.style.justifyContent = 'flex-start'; el.style.paddingTop = '20%'; el.style.paddingBottom = ''; }
    else if (seg.position === 'bottom-center') { el.style.justifyContent = 'flex-end'; el.style.paddingBottom = '20%'; el.style.paddingTop = ''; }
    else { el.style.justifyContent = 'center'; el.style.paddingTop = ''; el.style.paddingBottom = ''; }

    this.titleText.textContent = seg.text || '';
    this.titleText.style.color = seg.text_color || '#ffffff';
    var scale = el.parentElement ? el.parentElement.clientHeight / 1080 : 0.26;
    this.titleText.style.fontSize = Math.round((seg.font_size || 72) * scale) + 'px';

    if (this.titleSubtitle) {
      if (seg.subtitle) {
        this.titleSubtitle.textContent = seg.subtitle;
        this.titleSubtitle.style.color = seg.text_color || '#ffffff';
        this.titleSubtitle.style.fontSize = Math.round(((seg.font_size || 72) * 0.5) * scale) + 'px';
        this.titleSubtitle.style.opacity = '0.6';
        this.titleSubtitle.style.display = '';
      } else {
        this.titleSubtitle.style.display = 'none';
      }
    }
  }

  // ── Playback ──────────────────────────────────────────────────────────

  _play() {
    if (this._playing) return;
    if (this._totalDuration <= 0) return;
    if (this.playheadTime >= this._totalDuration - 0.05) this.playheadTime = 0;

    this._playing = true;
    this._updatePlayIcon(true);
    this._stopTransitionPreview();
    this.closeTransitionPopup();

    var segInfo = this._findSegAtTime(this.playheadTime);
    if (!segInfo) { this._stop(); return; }

    this.setSelectedIdx(segInfo.index);

    if (segInfo.seg.raw.type === 'title') {
      this._showTitleForScrub(segInfo.seg.raw);
      this._playTitleTimer = { segIdx: segInfo.index, startWall: performance.now(), startVt: this.playheadTime };
    } else {
      this._seekVideoForScrub(segInfo.seg.raw, segInfo.localTime);
      this.video.play().catch(function() {});
    }

    this._startPlaybackLoop();
  }

  _stop() {
    this._playing = false;
    if (this._playRAF) { cancelAnimationFrame(this._playRAF); this._playRAF = null; }
    this._playTitleTimer = null;
    if (this.video && !this.video.paused) this.video.pause();
    this._updatePlayIcon(false);
    this._updateTimeDisplay();
    this._scheduleRender();
  }

  _startPlaybackLoop() {
    if (this._playRAF) cancelAnimationFrame(this._playRAF);
    var self = this;

    function tick() {
      if (!self._playing) return;

      var segInfo = self._findSegAtTime(self.playheadTime);
      if (!segInfo) { self._stop(); return; }
      var seg = segInfo.seg;

      if (seg.raw.type === 'title') {
        if (self._playTitleTimer && self._playTitleTimer.segIdx === seg.index) {
          var elapsed = (performance.now() - self._playTitleTimer.startWall) / 1000;
          self.playheadTime = self._playTitleTimer.startVt + elapsed;
        }
      } else {
        if (self.video && !self.video.paused) {
          self.playheadTime = seg.vStart + (self.video.currentTime - seg.startTime);
        }
      }

      self.playheadTime = Math.min(self.playheadTime, self._totalDuration);

      if (self.playheadTime >= seg.vEnd - 0.03) {
        var nextIdx = seg.index + 1;
        if (nextIdx >= self._timeline.length) {
          self.playheadTime = self._totalDuration;
          self._stop();
          return;
        }
        self._advanceToNextSegment(nextIdx);
      }

      self._updateTimeDisplay();
      self._scheduleRender();
      self._playRAF = requestAnimationFrame(tick);
    }

    this._playRAF = requestAnimationFrame(tick);
  }

  _advanceToNextSegment(nextIdx) {
    var seg = this._timeline[nextIdx];
    if (!seg) { this._stop(); return; }

    this.playheadTime = seg.vStart;
    this.setSelectedIdx(seg.index);

    if (seg.raw.type === 'title') {
      if (this.video) this.video.pause();
      this._showTitleForScrub(seg.raw);
      this._playTitleTimer = { segIdx: seg.index, startWall: performance.now(), startVt: seg.vStart };
    } else {
      this._seekVideoForScrub(seg.raw, seg.startTime);
      this.video.muted = this.muted;
      this.video.play().catch(function() {});
      this._playTitleTimer = null;
    }
  }

  previewToggle() {
    if (this._seqPlayer && !this._seqPlayer.paused) {
      this._seqPlayer.pause();
      this._playing = false;
      this._updatePlayIcon(false);
      return;
    }
    if (this._seqPlayer && this._seqPlayer.paused && this._seqPlayer.currentSeg >= 0) {
      this._seqPlayer.play();
      this._playing = true;
      this._updatePlayIcon(true);
      return;
    }
    if (this._playing) this._stop();
    else this._play();
  }

  // ── Sequence playback (Play All with transitions) ─────────────────────

  _buildSeqSegments() {
    var segs = this.getSegments();
    var result = [];
    for (var i = 0; i < segs.length; i++) {
      var s = segs[i];
      var url = this._streamUrl(s);
      if (!url) continue;
      var entry = {
        src: url,
        startTime: s.start_ts || 0,
        endTime: s.end_ts || ((s.start_ts || 0) + (s.duration || 0)),
        label: s.title || '',
        transition: null,
      };
      if (s.transition && s.transition.type && result.length > 0) {
        entry.transition = {
          type: s.transition.type,
          duration: s.transition.duration || 0.5,
          behavior: { outgoing: s.transition.outgoing || 'play', audio: s.transition.audio || 'crossfade' },
        };
      }
      result.push(entry);
    }
    return result;
  }

  playAll() {
    this._stop();
    this.playheadTime = 0;

    var rawSegs = this.getSegments();
    if (rawSegs.length === 0) return;

    if (rawSegs.some(function(s) { return s && s.type === 'title'; })) {
      this._play();
      return;
    }

    var segs = this._buildSeqSegments();
    if (segs.length === 0) return;

    this._stopTransitionPreview();
    this.closeTransitionPopup();

    if (this.emptyPreview) this.emptyPreview.classList.add('hidden');
    if (this.titlePreview) { this.titlePreview.classList.add('hidden'); this.titlePreview.style.display = 'none'; }
    if (this.trPreview) this.trPreview.classList.add('hidden');
    if (this.video) { this.video.classList.remove('hidden'); this.video.style.display = ''; }

    var self = this;
    if (!this._seqPlayer) {
      this._seqPlayer = new SequencePlayback(this.previewContainer);
      this._seqPlayer.on('timeupdate', function(vt, dur) {
        self.playheadTime = vt;
        self._updateTimeDisplay();
        self._scheduleRender();
      });
      this._seqPlayer.on('ended', function() {
        self._playing = false;
        self._updatePlayIcon(false);
      });
      this._seqPlayer.on('segmentchange', function(idx) {
        self.setSelectedIdx(idx);
      });
    }

    this._seqPlayer.load(segs);
    this._seqPlayer.setMuted(this.muted);
    this._seqPlayer.play();
    this._playing = true;
    this._updatePlayIcon(true);
  }

  stopAll() {
    if (this._seqPlayer) {
      this._seqPlayer.stop();
      if (this.video) this.video.removeAttribute('data-current-src');
    }
    this._stop();
    this._scrubTo(this.playheadTime);
  }

  toggleAudio() {
    this.muted = !this.muted;
    if (this._seqPlayer) this._seqPlayer.setMuted(this.muted);
    if (this.video) this.video.muted = this.muted;
    this._updateAudioIcon();
  }

  // ── Transport ─────────────────────────────────────────────────────────

  transportStart() { this._stop(); this._scrubTo(0); }
  transportEnd() { this._stop(); this._scrubTo(this._totalDuration); }

  transportPrevFrame() {
    this._stop();
    this._scrubTo(Math.max(0, this.playheadTime - 1 / 30));
  }

  transportNextFrame() {
    this._stop();
    this._scrubTo(Math.min(this._totalDuration, this.playheadTime + 1 / 30));
  }

  // ── Canvas event handlers ─────────────────────────────────────────────

  _attachCanvasHandlers() {
    var self = this;

    if (this.workCanvas) {
      this.workCanvas.addEventListener('mousedown', function(e) { self._onWorkMouseDown(e); });
      this.workCanvas.addEventListener('mousemove', function(e) { self._onWorkHover(e); });
      this.workCanvas.addEventListener('wheel', function(e) {
        if (!e.ctrlKey && !e.metaKey) return;
        e.preventDefault();
        self.zoomWorkWindow(e.deltaY < 0 ? 0.8 : 1.25);
      }, { passive: false });
      this.workCanvas.addEventListener('contextmenu', function(e) { e.preventDefault(); });
    }

    if (this.overviewCanvas) {
      this.overviewCanvas.addEventListener('mousedown', function(e) { self._onOverviewMouseDown(e); });
    }

    document.addEventListener('mousemove', function(e) { self._onDocMouseMove(e); });
    document.addEventListener('mouseup', function(e) { self._onDocMouseUp(e); });

    if (this.workCanvas && this.workCanvas.parentElement) {
      new ResizeObserver(function() { self._scheduleRender(); }).observe(this.workCanvas.parentElement);
    }
    if (this.overviewCanvas && this.overviewCanvas.parentElement) {
      new ResizeObserver(function() { self._scheduleRender(); }).observe(this.overviewCanvas.parentElement);
    }
  }

  _onWorkMouseDown(e) {
    if (e.button === 1 || (e.button === 0 && e.altKey)) {
      this._drag = { type: 'work-pan', startX: e.clientX, origStart: this.workStart, origEnd: this.workEnd };
      e.preventDefault();
      return;
    }
    if (e.button !== 0) return;

    var hit = this.timeline.hitTestWork(e);

    if (hit.type === 'seek') {
      this._stop();
      this._drag = { type: 'work-seek' };
      this._scrubTo(clamp(hit.time, 0, this._totalDuration));
      return;
    }

    if (hit.type === 'segment') {
      this._stop();
      this.setSelectedIdx(hit.segIdx);
      this._drag = { type: 'work-seek' };
      this._scrubTo(clamp(hit.time, 0, this._totalDuration));
      return;
    }

    if (hit.type === 'trim-start' || hit.type === 'trim-end') {
      this._stop();
      var seg = this.getSegments()[hit.segIdx];
      if (!seg) return;
      this._drag = {
        type: hit.type,
        segIdx: hit.segIdx,
        startX: e.clientX,
        origStartTs: seg.start_ts || 0,
        origEndTs: seg.end_ts || ((seg.start_ts || 0) + (seg.duration || 0)),
        origDuration: this._calcSegDuration(seg),
        didMove: false,
      };
      return;
    }

    if (hit.type === 'transition') {
      this.openTransitionPopup(hit.segIdx, e);
      return;
    }
  }

  _onOverviewMouseDown(e) {
    if (e.button !== 0) return;
    var hit = this.timeline.hitTestOverview(e);

    if (hit.type === 'seek') {
      this._stop();
      this._drag = { type: 'overview-seek' };
      this._scrubTo(clamp(hit.time, 0, this._totalDuration));
      return;
    }

    if (hit.type === 'window-move') {
      this._drag = {
        type: 'overview-window-move',
        startX: e.clientX,
        origStart: this.workStart,
        origEnd: this.workEnd,
        anchorTime: hit.time,
      };
      return;
    }

    if (hit.type === 'window-resize-left' || hit.type === 'window-resize-right') {
      this._drag = {
        type: hit.type === 'window-resize-left' ? 'overview-resize-left' : 'overview-resize-right',
        origStart: this.workStart,
        origEnd: this.workEnd,
      };
      return;
    }
  }

  _onWorkHover(e) {
    if (this._drag.type !== 'none') return;
    var hit = this.timeline.hitTestWork(e);
    var newHovered = (hit.type === 'segment' || hit.type === 'trim-start' || hit.type === 'trim-end') ? hit.segIdx : -1;

    if (newHovered !== this._hoveredSeg) {
      this._hoveredSeg = newHovered;
      this._scheduleRender();
    }

    this.workCanvas.style.cursor = this.timeline.cursorForWorkHit(hit);
  }

  _onDocMouseMove(e) {
    var d = this._drag;

    if (d.type === 'work-seek') {
      var rect = this.workCanvas.getBoundingClientRect();
      var mx = e.clientX - rect.left;
      var t = this.workStart + (mx / rect.width) * (this.workEnd - this.workStart);
      this._scrubTo(clamp(t, 0, this._totalDuration));
      return;
    }

    if (d.type === 'overview-seek') {
      var rect2 = this.overviewCanvas.getBoundingClientRect();
      var mx2 = e.clientX - rect2.left;
      var t2 = (mx2 / rect2.width) * this._totalDuration;
      this._scrubTo(clamp(t2, 0, this._totalDuration));
      return;
    }

    if (d.type === 'work-pan') {
      var rect3 = this.workCanvas.getBoundingClientRect();
      var dx = e.clientX - d.startX;
      if (rect3.width > 0) {
        var wSpan = d.origEnd - d.origStart;
        var timeDelta = -(dx / rect3.width) * wSpan;
        var maxStart = Math.max(0, this._totalDuration - wSpan);
        var newStart = clamp(d.origStart + timeDelta, 0, maxStart);
        this.workStart = newStart;
        this.workEnd = newStart + wSpan;
        this.render();
      }
      return;
    }

    if (d.type === 'overview-window-move') {
      var rect4 = this.overviewCanvas.getBoundingClientRect();
      var dx2 = e.clientX - d.startX;
      var total = this._totalDuration;
      if (rect4.width > 0 && total > 0) {
        var timeDelta2 = (dx2 / rect4.width) * total;
        var size = d.origEnd - d.origStart;
        var newStart2 = clamp(d.origStart + timeDelta2, 0, Math.max(0, total - size));
        this.workStart = newStart2;
        this.workEnd = newStart2 + size;
        this.render();
      }
      return;
    }

    if (d.type === 'overview-resize-left' || d.type === 'overview-resize-right') {
      var rect5 = this.overviewCanvas.getBoundingClientRect();
      var mx5 = e.clientX - rect5.left;
      var total2 = this._totalDuration;
      if (rect5.width > 0 && total2 > 0) {
        var t5 = clamp((mx5 / rect5.width) * total2, 0, total2);
        var minSize = Math.min(0.5, total2);
        if (d.type === 'overview-resize-left') {
          this.workStart = clamp(t5, 0, Math.max(0, d.origEnd - minSize));
          this.workEnd = d.origEnd;
        } else {
          this.workStart = d.origStart;
          this.workEnd = clamp(t5, Math.min(total2, d.origStart + minSize), total2);
        }
        this.render();
      }
      return;
    }

    if (d.type === 'trim-start' || d.type === 'trim-end') {
      var rect6 = this.workCanvas.getBoundingClientRect();
      var dx6 = e.clientX - d.startX;
      if (rect6.width <= 0) return;
      var wSpan6 = this.workEnd - this.workStart;
      var timeDelta6 = (dx6 / rect6.width) * wSpan6;

      var segs = this.getSegments();
      var seg = segs[d.segIdx];
      if (!seg) return;

      if (d.type === 'trim-start') {
        if (seg.type === 'title') {
          seg.duration = Math.max(0.1, d.origDuration - timeDelta6);
        } else {
          seg.start_ts = clamp(d.origStartTs + timeDelta6, 0, d.origEndTs - 0.1);
        }
      } else {
        if (seg.type === 'title') {
          seg.duration = Math.max(0.1, d.origDuration + timeDelta6);
        } else {
          seg.end_ts = Math.max(d.origStartTs + 0.1, d.origEndTs + timeDelta6);
        }
      }

      d.didMove = true;
      this._computeTimeline();
      if (this.playheadTime > this._totalDuration) this.playheadTime = this._totalDuration;
      this._updateTotalDuration();
      this.render();
      return;
    }
  }

  _onDocMouseUp(e) {
    var d = this._drag;
    if (d.type === 'none') return;

    if ((d.type === 'trim-start' || d.type === 'trim-end') && d.didMove) {
      this.setSegments(this.getSegments());
    }

    this._drag = { type: 'none' };
    if (this.workCanvas) this.workCanvas.style.cursor = '';
    document.body.style.cursor = '';
  }

  // ── Segment CRUD ──────────────────────────────────────────────────────

  addSource(el) {
    var sourceType = el.dataset.sourceType || 'clip';
    var segs = this.getSegments();
    var base = { duration: parseFloat(el.dataset.duration) || 0, title: el.dataset.title || '', transition: null, filters: [] };
    var seg;
    switch (sourceType) {
      case 'clip':
        seg = Object.assign({}, base, { type: 'clip', clip_id: el.dataset.sourceId, video_id: el.dataset.videoId, start_ts: parseFloat(el.dataset.startTs) || 0, end_ts: parseFloat(el.dataset.endTs) || 0 });
        break;
      case 'video':
        seg = Object.assign({}, base, { type: 'video', video_id: el.dataset.sourceId, start_ts: parseFloat(el.dataset.startTs) || 0, end_ts: parseFloat(el.dataset.endTs) || 0 });
        break;
      case 'compose': case 'stitch':
        seg = Object.assign({}, base, { type: sourceType, export_job_id: el.dataset.sourceId });
        break;
      default: return;
    }
    var newSegs = segs.concat([seg]);
    this.setSegments(newSegs);
    this.setSelectedIdx(newSegs.length - 1);
    this._scrubTo(this._timeline.length > 0 ? this._timeline[this._timeline.length - 1].vStart : 0);
  }

  addClip(el) {
    var segs = this.getSegments();
    var newSegs = segs.concat([{
      type: 'clip', clip_id: el.dataset.clipId, video_id: el.dataset.videoId,
      duration: parseFloat(el.dataset.duration) || 0,
      start_ts: parseFloat(el.dataset.startTs) || 0, end_ts: parseFloat(el.dataset.endTs) || 0,
      title: el.dataset.title || '', transition: null, filters: [],
    }]);
    this.setSegments(newSegs);
    this.setSelectedIdx(newSegs.length - 1);
  }

  addTitleCard() {
    var textEl = document.getElementById('stitch-title-card-text');
    var durEl = document.getElementById('stitch-title-card-duration');
    var text = textEl ? textEl.value.trim() : '';
    if (!text) return;
    var duration = durEl ? (parseFloat(durEl.value) || 3) : 3;
    var segs = this.getSegments();
    var newIdx = segs.length;
    this.setSegments(segs.concat([{
      type: 'title', text: text, duration: duration, subtitle: '',
      bg_color: '#000000', text_color: '#ffffff', font_size: 72, position: 'center', transition: null,
    }]));
    if (textEl) textEl.value = '';
    this.setSelectedIdx(newIdx);
    if (this._timeline[newIdx]) this._scrubTo(this._timeline[newIdx].vStart);
  }

  removeSegment(idx) {
    var segs = this.getSegments();
    var sel = this.getSelectedIdx();
    this.closeTransitionPopup();
    this._stop();
    this.setSegments(segs.filter(function(_, i) { return i !== idx; }));
    if (sel === idx) this.setSelectedIdx(-1);
    else if (sel > idx) this.setSelectedIdx(sel - 1);
    if (this.playheadTime > this._totalDuration) this.playheadTime = Math.max(0, this._totalDuration);
    this._scrubTo(this.playheadTime);
  }

  moveSegment(idx, direction) {
    var segs = this.getSegments();
    var target = idx + direction;
    if (target < 0 || target >= segs.length) return;
    var copy = segs.slice();
    var tmp = copy[idx]; copy[idx] = copy[target]; copy[target] = tmp;
    this.setSegments(copy);
    var sel = this.getSelectedIdx();
    if (sel === idx) this.setSelectedIdx(target);
    else if (sel === target) this.setSelectedIdx(idx);
  }

  selectSegment(idx) {
    this.closeTransitionPopup();
    this._stop();
    this.setSelectedIdx(idx);
    if (this._timeline[idx]) this._scrubTo(this._timeline[idx].vStart);
    else this._showEmpty();
  }

  // ── Field updates ─────────────────────────────────────────────────────

  updateSegField(idx, field, value) {
    var segs = this.getSegments();
    if (idx < 0 || idx >= segs.length) return;
    var copy = segs.map(function(s, i) { return i === idx ? Object.assign({}, s, { [field]: value }) : s; });
    this.setSegments(copy);
    if (copy[idx].type === 'title' && this.getSelectedIdx() === idx) this._renderTitlePreview(copy[idx]);
  }

  updateTransition(idx, field, value) {
    var segs = this.getSegments();
    if (idx < 0 || idx >= segs.length) return;
    var seg = Object.assign({}, segs[idx]);
    if (!seg.transition) seg.transition = { type: '', duration: 0.5 };
    seg.transition = Object.assign({}, seg.transition, { [field]: value });
    if (field === 'type' && !value) seg.transition = null;
    if (field === 'type' && value && seg.transition && (!seg.transition.duration || seg.transition.duration <= 0)) seg.transition.duration = 0.5;
    this.setSegments(segs.map(function(s, i) { return i === idx ? seg : s; }));
  }

  // ── Transition popup ──────────────────────────────────────────────────

  openTransitionPopup(segIdx, anchorOrEvent) {
    this.closeTransitionPopup();
    var popup = document.getElementById('stitch-transition-popup');
    if (!popup) return;
    var segs = this.getSegments();
    if (!segs[segIdx]) return;

    var top, left;
    if (anchorOrEvent instanceof Element) {
      var rect = anchorOrEvent.getBoundingClientRect();
      top = rect.top;
      left = rect.left + rect.width / 2 - 110;
    } else if (anchorOrEvent && typeof anchorOrEvent.clientX === 'number') {
      top = anchorOrEvent.clientY;
      left = anchorOrEvent.clientX - 110;
    } else {
      return;
    }

    popup.className = 'fixed z-50 bg-neutral-900 border-2 border-white/20 p-3 shadow-xl';
    popup.style.cssText = 'min-width:220px; top:' + top + 'px; left:' + Math.max(8, left) + 'px; transform:translateY(-100%) translateY(-8px);';

    var api = this._ds();
    if (api) api.mergePatch({ _stitchTrIdx: segIdx });

    var self = this;
    var closeHandler = function(e) {
      if (!popup.contains(e.target)) self.closeTransitionPopup();
    };
    setTimeout(function() { document.addEventListener('mousedown', closeHandler); }, 0);
    this._trPopupCleanup = function() { document.removeEventListener('mousedown', closeHandler); };

    this._showTransitionPreview(segIdx);
  }

  setTransPopup(idx, field, value) {
    this.updateTransition(idx, field, value);
    var segs = this.getSegments();
    var seg = segs[idx];
    if (!seg) return;
    var durDiv = document.getElementById('stitch-tr-popup-dur');
    if (durDiv) durDiv.className = (seg.transition && seg.transition.type) ? 'mt-2' : 'mt-2 opacity-30 pointer-events-none';
    this._restartTransitionAnimation();
  }

  closeTransitionPopup() {
    var popup = document.getElementById('stitch-transition-popup');
    if (popup) { popup.className = 'hidden'; popup.innerHTML = ''; }
    if (this._trPopupCleanup) { this._trPopupCleanup(); this._trPopupCleanup = null; }
    var api = this._ds();
    if (api) api.mergePatch({ _stitchTrIdx: -1 });
    this._stopTransitionPreview();
    this._trSegIdx = -1;
  }

  // ── Transition preview ────────────────────────────────────────────────

  _stopTransitionPreview() {
    if (this._trRAF) { cancelAnimationFrame(this._trRAF); this._trRAF = null; }
    if (this.trPreview) this.trPreview.classList.add('hidden');
    this._clearTransitionFX();
  }

  _clearTransitionFX() {
    if (this.trFrom) { this.trFrom.style.clipPath = ''; this.trFrom.style.transform = ''; this.trFrom.style.opacity = '1'; this.trFrom.style.filter = ''; this.trFrom.style.maskImage = ''; this.trFrom.style.webkitMaskImage = ''; }
    if (this.trTo) { this.trTo.style.transform = ''; this.trTo.style.maskImage = ''; this.trTo.style.webkitMaskImage = ''; }
    if (this.trOverlay) { this.trOverlay.style.display = 'none'; this.trOverlay.style.opacity = ''; }
  }

  _captureFrame(seg, time) {
    if (seg.type === 'title') {
      var c = document.createElement('canvas');
      c.width = 640; c.height = 360;
      var ctx = c.getContext('2d');
      ctx.fillStyle = seg.bg_color || '#000000';
      ctx.fillRect(0, 0, 640, 360);
      ctx.fillStyle = seg.text_color || '#ffffff';
      var fs = Math.round((seg.font_size || 72) * (360 / 1080));
      ctx.font = 'bold ' + fs + 'px monospace';
      ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
      var y = seg.position === 'top-center' ? 108 : seg.position === 'bottom-center' ? 252 : 180;
      ctx.fillText(seg.text || '', 320, y);
      if (seg.subtitle) { ctx.font = Math.round(fs * 0.5) + 'px monospace'; ctx.globalAlpha = 0.6; ctx.fillText(seg.subtitle, 320, y + fs + 4); }
      return Promise.resolve(c);
    }
    var url = this._streamUrl(seg);
    if (!url) { var c2 = document.createElement('canvas'); c2.width = 640; c2.height = 360; c2.getContext('2d').fillRect(0, 0, 640, 360); return Promise.resolve(c2); }
    return new Promise(function(resolve) {
      var v = document.createElement('video'); v.muted = true; v.preload = 'auto'; v.src = url;
      var done = false;
      function finish() { if (done) return; done = true; var c3 = document.createElement('canvas'); c3.width = v.videoWidth || 640; c3.height = v.videoHeight || 360; c3.getContext('2d').drawImage(v, 0, 0, c3.width, c3.height); v.removeAttribute('src'); v.load(); resolve(c3); }
      function fail() { if (done) return; done = true; var c4 = document.createElement('canvas'); c4.width = 640; c4.height = 360; c4.getContext('2d').fillStyle = '#1a1a1a'; c4.getContext('2d').fillRect(0, 0, 640, 360); resolve(c4); }
      v.addEventListener('loadeddata', function() { if (time > 0.1) v.currentTime = time; else finish(); }, { once: true });
      v.addEventListener('seeked', finish, { once: true });
      v.addEventListener('error', fail, { once: true });
      setTimeout(function() { if (!done) fail(); }, 8000);
      v.load();
    });
  }

  _showTransitionPreview(segIdx) {
    var segs = this.getSegments();
    if (segIdx < 1 || segIdx >= segs.length) return;
    this._trSegIdx = segIdx;
    var outgoing = segs[segIdx - 1], incoming = segs[segIdx];
    var tr = incoming.transition || { type: 'fade', duration: 0.5 };

    if (this.video) { this.video.classList.add('hidden'); this.video.pause(); }
    if (this.titlePreview) { this.titlePreview.classList.add('hidden'); this.titlePreview.style.display = 'none'; }
    if (this.emptyPreview) this.emptyPreview.classList.add('hidden');
    if (!this.trPreview) return;
    this.trPreview.classList.remove('hidden');

    if (this.trLabel) this.trLabel.textContent = this._transitionLabel(tr.type || 'fade') + ' · ' + (tr.duration || 0.5).toFixed(1) + 's';

    var outTime = outgoing.type !== 'title' ? Math.max(0, (outgoing.end_ts || ((outgoing.start_ts || 0) + (outgoing.duration || 0))) - 0.2) : 0;
    var inTime = incoming.type !== 'title' ? (incoming.start_ts || 0) : 0;

    var self = this;
    Promise.all([this._captureFrame(outgoing, outTime), this._captureFrame(incoming, inTime)]).then(function(r) {
      if (self._trSegIdx !== segIdx) return;
      if (!self.trFrom || !self.trTo) return;
      self.trFrom.width = r[0].width; self.trFrom.height = r[0].height;
      self.trTo.width = r[1].width; self.trTo.height = r[1].height;
      self.trFrom.getContext('2d').drawImage(r[0], 0, 0);
      self.trTo.getContext('2d').drawImage(r[1], 0, 0);
      self._startTransitionAnimation(tr.type || 'fade', tr.duration || 0.5);
    });
  }

  _transitionLabel(type) {
    var labels = { '': 'Hard Cut', fade: 'Fade', fadeblack: 'Fade Black', fadewhite: 'Fade White', dissolve: 'Dissolve', wipeleft: 'Wipe Left', wiperight: 'Wipe Right', wipeup: 'Wipe Up', wipedown: 'Wipe Down', slideleft: 'Slide Left', slideright: 'Slide Right', circlecrop: 'Circle Crop', circleopen: 'Circle Open', circleclose: 'Circle Close', radial: 'Radial', zoomin: 'Zoom In' };
    return labels[type] || type;
  }

  _startTransitionAnimation(type, duration) {
    if (this._trRAF) cancelAnimationFrame(this._trRAF);
    if (!this.trFrom) return;
    var fx = TRANSITION_FX[type] || TRANSITION_FX.fade;
    var durMs = Math.max(duration, 0.2) * 1000;
    var holdMs = 600;
    var cycleMs = holdMs + durMs + holdMs + durMs + holdMs;
    var startTime = null;
    var self = this;

    function frame(ts) {
      if (!startTime) startTime = ts;
      if (!self.trPreview || self.trPreview.classList.contains('hidden')) return;
      var elapsed = (ts - startTime) % cycleMs;
      var progress;
      if (elapsed < holdMs) progress = 0;
      else if (elapsed < holdMs + durMs) progress = (elapsed - holdMs) / durMs;
      else if (elapsed < holdMs + durMs + holdMs) progress = 1;
      else if (elapsed < holdMs + durMs + holdMs + durMs) progress = 1 - (elapsed - holdMs - durMs - holdMs) / durMs;
      else progress = 0;
      progress = progress * progress * (3 - 2 * progress);

      self.trFrom.style.opacity = ''; self.trFrom.style.transform = ''; self.trFrom.style.clipPath = ''; self.trFrom.style.filter = '';
      self.trFrom.style.maskImage = ''; self.trFrom.style.webkitMaskImage = '';
      if (self.trTo) { self.trTo.style.transform = ''; self.trTo.style.maskImage = ''; self.trTo.style.webkitMaskImage = ''; }
      if (self.trOverlay) { self.trOverlay.style.display = 'none'; self.trOverlay.style.opacity = ''; }
      fx(self.trFrom, self.trTo, progress, self.trOverlay);
      self._trRAF = requestAnimationFrame(frame);
    }
    this._trRAF = requestAnimationFrame(frame);
  }

  _restartTransitionAnimation() {
    if (this._trSegIdx < 0) return;
    var segs = this.getSegments();
    var seg = segs[this._trSegIdx];
    if (!seg) return;
    var tr = seg.transition || { type: 'fade', duration: 0.5 };
    if (this.trLabel) this.trLabel.textContent = this._transitionLabel(tr.type || 'fade') + ' · ' + (tr.duration || 0.5).toFixed(1) + 's';
    this._startTransitionAnimation(tr.type || 'fade', tr.duration || 0.5);
  }

  // ── Auto-save ─────────────────────────────────────────────────────────

  autoSave(dirty, projectId, title, format, quality, segments) {
    if (!dirty || !projectId || this._saving) return;
    if (this._saveTimer) clearTimeout(this._saveTimer);
    var self = this;
    this._saveTimer = setTimeout(function() {
      self._saving = true;
      var api = self._ds();
      if (api) api.mergePatch({ _stitchSaving: true });
      fetch('/api/stitch/projects/' + projectId, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: title, format: format, quality: quality, segments: segments || [] }),
      }).then(function(r) {
        var api2 = self._ds();
        if (r.ok) { if (api2) api2.mergePatch({ _stitchDirty: false, _stitchSaving: false }); }
        else { if (api2) api2.mergePatch({ _stitchSaving: false }); }
        self._saving = false;
      }).catch(function() {
        var api3 = self._ds();
        if (api3) api3.mergePatch({ _stitchSaving: false });
        self._saving = false;
      });
    }, 800);
  }

  deleteProject() {
    var api = this._ds();
    var id = api ? api.getPath('_stitchProjectId') : null;
    if (!id) return;
    fetch('/api/stitch/projects/' + id, { method: 'DELETE' })
      .then(function(r) { return r.json(); })
      .then(function(d) { if (d.redirect) window.location.href = d.redirect; })
      .catch(function(e) { console.error('delete error', e); });
  }

  // ── Keyboard shortcuts ────────────────────────────────────────────────

  _attachKeyboard() {
    var self = this;
    document.addEventListener('keydown', function(e) {
      var t = document.activeElement;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;

      var key = e.key.toLowerCase();

      if (key === ' ' && !e.ctrlKey && !e.metaKey) { e.preventDefault(); self.previewToggle(); return; }
      if ((key === 'delete' || key === 'backspace') && !e.ctrlKey && !e.metaKey) {
        var sel = self.getSelectedIdx();
        if (sel >= 0) { e.preventDefault(); self.removeSegment(sel); }
        return;
      }
      if (key === 'd' && !e.ctrlKey && !e.metaKey) {
        var sel2 = self.getSelectedIdx(), segs = self.getSegments();
        if (sel2 >= 0 && sel2 < segs.length) {
          e.preventDefault();
          var copy = JSON.parse(JSON.stringify(segs));
          copy.splice(sel2 + 1, 0, JSON.parse(JSON.stringify(segs[sel2])));
          self.setSegments(copy);
          self.setSelectedIdx(sel2 + 1);
        }
        return;
      }
      if (key === 'arrowup' && !e.ctrlKey) { var s = self.getSelectedIdx(); if (s > 0) { e.preventDefault(); self.moveSegment(s, -1); } return; }
      if (key === 'arrowdown' && !e.ctrlKey) { var s2 = self.getSelectedIdx(); if (s2 >= 0 && s2 < self.getSegments().length - 1) { e.preventDefault(); self.moveSegment(s2, 1); } return; }
      if (key === 'arrowleft' && !e.ctrlKey) {
        e.preventDefault();
        if (e.shiftKey) { self.transportPrevFrame(); }
        else {
          var cur = self.getSelectedIdx();
          if (cur > 0) self.selectSegment(cur - 1);
          else if (cur < 0 && self._timeline.length > 0) self.selectSegment(0);
        }
        return;
      }
      if (key === 'arrowright' && !e.ctrlKey) {
        e.preventDefault();
        if (e.shiftKey) { self.transportNextFrame(); }
        else {
          var cur2 = self.getSelectedIdx();
          if (cur2 < self.getSegments().length - 1) self.selectSegment(cur2 + 1);
        }
        return;
      }
      if (key === 'home') { e.preventDefault(); self.transportStart(); return; }
      if (key === 'end') { e.preventDefault(); self.transportEnd(); return; }
      if (key === 'j' && !e.ctrlKey) { e.preventDefault(); self.transportPrevFrame(); return; }
      if (key === 'l' && !e.ctrlKey) { e.preventDefault(); self.transportNextFrame(); return; }
      if (key === 'm' && !e.ctrlKey) { e.preventDefault(); self.toggleAudio(); return; }
      if (key === 's' && !e.ctrlKey) { e.preventDefault(); var api = self._ds(); if (api) api.mergePatch({ _stitchDirty: true }); return; }
      if (key === 'escape') { self.closeTransitionPopup(); self.stopAll(); return; }
      if (key === '=' || key === '+') { e.preventDefault(); self.zoomIn(); return; }
      if (key === '-') { e.preventDefault(); self.zoomOut(); return; }
      if (key === '0' && !e.ctrlKey) { e.preventDefault(); self.zoomReset(); return; }
    });
  }

  // ── Signal change handler (called by data-effect) ─────────────────────

  _onSignalsChanged() {
    this._computeTimeline();
    this._updateTotalDuration();
    this._scheduleRender();
  }

  // ── Helpers ───────────────────────────────────────────────────────────

  _streamUrl(seg) {
    if (seg.type === 'clip' || seg.type === 'video') return '/api/videos/' + seg.video_id + '/stream';
    if (seg.type === 'compose') return '/api/compose/' + seg.export_job_id + '/stream';
    if (seg.type === 'stitch') return '/api/stitch/' + seg.export_job_id + '/stream';
    return '';
  }

  _formatTime(s) {
    if (s < 0) s = 0;
    var m = Math.floor(s / 60);
    var sec = Math.floor(s % 60);
    return m + ':' + (sec < 10 ? '0' : '') + sec;
  }

  _updatePlayIcon(playing) {
    if (!this.transportPlayBtn) return;
    var icon = this.transportPlayBtn.querySelector('i');
    if (icon) icon.className = playing ? 'fa-sharp fa-solid fa-pause' : 'fa-sharp fa-solid fa-play';
  }

  _updateAudioIcon() {
    if (!this.audioBtn) return;
    var icon = this.audioBtn.querySelector('i');
    if (icon) icon.className = this.muted ? 'fa-sharp fa-solid fa-volume-xmark' : 'fa-sharp fa-solid fa-volume-high';
  }

  // ── Window function wrappers ──────────────────────────────────────────

  _exposeWindowFunctions() {
    var ed = this;
    window.addStitchSource = function(el) { ed.addSource(el); };
    window.addStitchClip = function(el) { ed.addClip(el); };
    window.addStitchTitleCard = function() { ed.addTitleCard(); };
    window.removeStitchSegment = function(idx) { ed.removeSegment(idx); };
    window.moveStitchSegment = function(idx, dir) { ed.moveSegment(idx, dir); };
    window.selectStitchSegment = function(idx) { ed.selectSegment(idx); };
    window.openTransitionPopup = function(idx, el) { ed.openTransitionPopup(idx, el); };
    window._setTransPopup = function(idx, field, value) { ed.setTransPopup(idx, field, value); };
    window.updateStitchSegField = function(idx, field, value) { ed.updateSegField(idx, field, value); };
    window.updateStitchTransition = function(idx, field, value) { ed.updateTransition(idx, field, value); };
    window.stitchAutoSave = function(dirty, pid, title, fmt, quality, segs) { ed.autoSave(dirty, pid, title, fmt, quality, segs); };
    window.stitchDeleteProject = function() { ed.deleteProject(); };
    window.stitchPreviewToggle = function() { ed.previewToggle(); };
    window.stitchPlayAll = function() { ed.playAll(); };
    window.stitchStopAll = function() { ed.stopAll(); };
    window.stitchToggleAudio = function() { ed.toggleAudio(); };
    window.stitchZoomIn = function() { ed.zoomIn(); };
    window.stitchZoomOut = function() { ed.zoomOut(); };
    window.stitchZoomReset = function() { ed.zoomReset(); };
    window.stitchTransportStart = function() { ed.transportStart(); };
    window.stitchTransportEnd = function() { ed.transportEnd(); };
    window.stitchTransportPrevFrame = function() { ed.transportPrevFrame(); };
    window.stitchTransportNextFrame = function() { ed.transportNextFrame(); };
    window.stitchEditor = ed;
  }
}

// ── Init ──────────────────────────────────────────────────────────────────

function init() {
  var root = document.querySelector('[data-stitch-page]');
  if (!root) return;
  new StitchEditor(root);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
