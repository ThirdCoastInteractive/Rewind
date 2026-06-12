// ============================================================================
// SequencePlayback – Multi-clip video player engine
// ============================================================================
//
// Provides gapless back-to-back playback of video segments using dual <video>
// elements with preloading, CSS-based visual transitions, Web Audio crossfading,
// and a virtual timeline.
//
// Usage:
//   const seq = new SequencePlayback(containerEl);
//   seq.load([
//     { src: '/api/videos/abc/stream', startTime: 10, endTime: 30 },
//     { src: '/api/videos/def/stream', startTime: 5, endTime: 20,
//       transition: { type: 'fade', duration: 1 } },
//   ]);
//   seq.play();
//   seq.on('timeupdate', (vt, dur) => { ... });
//

// ── Transition effects ──────────────────────────────────────────────────────
// (outEl, inEl, t, overlay) — t goes 0→1. outEl is on top; hide it to reveal
// inEl underneath. Some effects also style inEl (smooth*) or use the overlay
// div (fadeblack/fadewhite).

const FX = {
  none() {},

  fade(o, _i, t) { o.style.opacity = 1 - t; },

  fadeblack(o, _i, t, ov) {
    if (!ov) { o.style.opacity = 1 - t; return; }
    ov.style.display = 'block'; ov.style.background = 'black';
    if (t < 0.5) { o.style.opacity = 1 - t * 2; ov.style.opacity = t * 2; }
    else { o.style.opacity = 0; ov.style.opacity = 1 - (t - 0.5) * 2; }
  },

  fadewhite(o, _i, t, ov) {
    if (!ov) { o.style.opacity = 1 - t; return; }
    ov.style.display = 'block'; ov.style.background = 'white';
    if (t < 0.5) { o.style.opacity = 1 - t * 2; ov.style.opacity = t * 2; }
    else { o.style.opacity = 0; ov.style.opacity = 1 - (t - 0.5) * 2; }
  },

  dissolve(o, _i, t) {
    o.style.opacity = 1 - t;
    o.style.filter = 'blur(' + (t * 12) + 'px)';
  },

  pixelize(o, _i, t) {
    o.style.filter = 'blur(' + (t * 20) + 'px)';
    o.style.opacity = 1 - t;
  },

  // Directional wipes (clip-path inset)
  wipeleft(o, _i, t) { o.style.clipPath = 'inset(0 0 0 ' + (t * 100) + '%)'; },
  wiperight(o, _i, t) { o.style.clipPath = 'inset(0 ' + (t * 100) + '% 0 0)'; },
  wipeup(o, _i, t) { o.style.clipPath = 'inset(0 0 ' + (t * 100) + '% 0)'; },
  wipedown(o, _i, t) { o.style.clipPath = 'inset(' + (t * 100) + '% 0 0 0)'; },

  // Slides (transform on outgoing only)
  slideleft(o, _i, t)  { o.style.transform = 'translateX(' + (-t * 100) + '%)'; },
  slideright(o, _i, t) { o.style.transform = 'translateX(' + (t * 100) + '%)'; },
  slideup(o, _i, t)    { o.style.transform = 'translateY(' + (-t * 100) + '%)'; },
  slidedown(o, _i, t)  { o.style.transform = 'translateY(' + (t * 100) + '%)'; },

  // Smooth slides (both elements slide in tandem)
  smoothleft(o, i, t) {
    o.style.transform = 'translateX(' + (-t * 100) + '%)';
    i.style.transform = 'translateX(' + ((1 - t) * 100) + '%)';
  },
  smoothright(o, i, t) {
    o.style.transform = 'translateX(' + (t * 100) + '%)';
    i.style.transform = 'translateX(' + (-(1 - t) * 100) + '%)';
  },

  // Circle / Iris
  circlecrop(o, _i, t) { o.style.clipPath = 'circle(' + ((1 - t) * 72) + '% at 50% 50%)'; },
  circleopen(o, _i, t) {
    o.style.maskImage = 'radial-gradient(circle at 50% 50%, transparent ' + (t * 120) + '%, black ' + (t * 120 + 2) + '%)';
    o.style.webkitMaskImage = o.style.maskImage;
  },
  circleclose(o, _i, t) { o.style.clipPath = 'circle(' + (Math.max(0, (1 - t) * 72)) + '% at 50% 50%)'; },

  // Diagonals (mask-image linear gradient)
  diagtl(o, _i, t) {
    var p = t * 150;
    o.style.maskImage = 'linear-gradient(to bottom right, transparent ' + (p - 10) + '%, black ' + p + '%)';
    o.style.webkitMaskImage = o.style.maskImage;
  },
  diagbr(o, _i, t) {
    var p = t * 150;
    o.style.maskImage = 'linear-gradient(to top left, transparent ' + (p - 10) + '%, black ' + p + '%)';
    o.style.webkitMaskImage = o.style.maskImage;
  },
  diagtr(o, _i, t) {
    var p = t * 150;
    o.style.maskImage = 'linear-gradient(to bottom left, transparent ' + (p - 10) + '%, black ' + p + '%)';
    o.style.webkitMaskImage = o.style.maskImage;
  },
  diagbl(o, _i, t) {
    var p = t * 150;
    o.style.maskImage = 'linear-gradient(to top right, transparent ' + (p - 10) + '%, black ' + p + '%)';
    o.style.webkitMaskImage = o.style.maskImage;
  },

  // Slices (multi-strip wipe)
  hlslice(o, _i, t) {
    var n = 8, h = 100 / n, stops = [];
    for (var s = 0; s < n; s++) {
      var base = s * h, vis = base + h * (1 - t);
      stops.push('black ' + base + '%', 'black ' + vis + '%',
                  'transparent ' + vis + '%', 'transparent ' + (base + h) + '%');
    }
    o.style.maskImage = 'linear-gradient(to bottom, ' + stops.join(', ') + ')';
    o.style.webkitMaskImage = o.style.maskImage;
  },
  vuslice(o, _i, t) {
    var n = 8, w = 100 / n, stops = [];
    for (var s = 0; s < n; s++) {
      var base = s * w, vis = base + w * (1 - t);
      stops.push('black ' + base + '%', 'black ' + vis + '%',
                  'transparent ' + vis + '%', 'transparent ' + (base + w) + '%');
    }
    o.style.maskImage = 'linear-gradient(to right, ' + stops.join(', ') + ')';
    o.style.webkitMaskImage = o.style.maskImage;
  },

  // Radial
  radial(o, _i, t) {
    var deg = t * 360;
    o.style.maskImage = 'conic-gradient(from -90deg at 50% 50%, transparent ' + deg + 'deg, black ' + deg + 'deg)';
    o.style.webkitMaskImage = o.style.maskImage;
  },

  // Zoom
  zoomin(o, _i, t) {
    o.style.transform = 'scale(' + (1 + t * 4) + ')';
    o.style.opacity = 1 - t;
  },

  // ── Additional real ffmpeg transitions ──

  fadefast(o, _i, t) { o.style.opacity = Math.max(0, 1 - t * 2); },
  fadeslow(o, _i, t) { o.style.opacity = Math.max(0, Math.pow(1 - t, 0.3)); },
  hblur(o, _i, t) {
    o.style.filter = 'blur(' + (t * 20) + 'px)';
    o.style.opacity = 1 - t;
  },

  coverleft(o, i, t) {
    i.style.transform = 'translateX(' + ((1 - t) * 100) + '%)';
  },
  coverright(o, i, t) {
    i.style.transform = 'translateX(' + (-(1 - t) * 100) + '%)';
  },

  vertopen(o, _i, t) {
    var half = t * 50;
    o.style.clipPath = 'inset(0 ' + half + '%)';
  },
  vertclose(o, _i, t) {
    var half = (1 - t) * 50;
    o.style.clipPath = 'inset(0 ' + half + '%)';
    o.style.opacity = 1 - t;
  },
  horzopen(o, _i, t) {
    var half = t * 50;
    o.style.clipPath = 'inset(' + half + '% 0)';
  },
  horzclose(o, _i, t) {
    var half = (1 - t) * 50;
    o.style.clipPath = 'inset(' + half + '% 0)';
    o.style.opacity = 1 - t;
  },

  squeezeh(o, _i, t) {
    o.style.transform = 'scaleX(' + Math.max(0.01, 1 - t) + ')';
    o.style.opacity = Math.max(0, 1 - t * 1.5);
  },
  squeezev(o, _i, t) {
    o.style.transform = 'scaleY(' + Math.max(0.01, 1 - t) + ')';
    o.style.opacity = Math.max(0, 1 - t * 1.5);
  },
};

// List of transition names exposed for UI dropdowns
export const TRANSITION_TYPES = Object.keys(FX).filter(k => k !== 'none');

// Expose FX functions for external transition previews (stitch page)
export { FX as TRANSITION_FX };


// ── SequencePlayback class ──────────────────────────────────────────────────

export class SequencePlayback {
  /**
   * @param {HTMLElement} container - Positioned container for the video elements.
   */
  constructor(container) {
    this.container = container;

    // Grab or create two video elements
    var existing = container.querySelectorAll('video');
    this.els = [
      existing[0] || this._mkVideo(),
      existing[1] || this._mkVideo(),
    ];
    if (!this.els[0].parentElement) container.appendChild(this.els[0]);
    if (!this.els[1].parentElement) container.appendChild(this.els[1]);

    // Ensure stacking context
    for (var vi = 0; vi < 2; vi++) {
      var v = this.els[vi];
      v.style.position = 'absolute';
      v.style.inset = '0';
      v.style.width = '100%';
      v.style.height = '100%';
      v.style.objectFit = 'contain';
      v.playsInline = true;
      v.preload = 'auto';
    }

    // Overlay div for fade-through-color effects
    this.overlay = document.createElement('div');
    this.overlay.style.cssText =
      'position:absolute;inset:0;display:none;pointer-events:none;z-index:10';
    container.appendChild(this.overlay);

    // Active element index (0 or 1)
    this.activeIdx = 0;

    // Timeline
    this.segments = [];     // computed (with vStart/vEnd)
    this.totalDuration = 0;
    this.currentSeg = -1;
    this._preloadedSeg = -1;
    this._playing = false;

    // Transition in-flight
    this._tr = null;

    // Audio (Web Audio API for crossfading)
    this._audioCtx = null;
    this._gains = [null, null];
    this._sources = [null, null];
    this._audioInited = false;

    // Event listeners
    this._cbs = {};

    // Attach handlers
    this._boundTU = this._onTimeUpdate.bind(this);
    this._boundPoll = this._poll.bind(this);
    this._pollRAF = null;
  }

  _mkVideo() {
    var v = document.createElement('video');
    v.playsInline = true;
    v.preload = 'auto';
    return v;
  }

  // ── Events ──────────────────────────────────────────────────────────────

  /** Register an event callback. Events: timeupdate, play, pause, ended, segmentchange */
  on(evt, fn) { (this._cbs[evt] ||= []).push(fn); return this; }
  off(evt, fn) { var a = this._cbs[evt]; if (a) this._cbs[evt] = a.filter(f => f !== fn); }
  _emit(evt) {
    var args = [].slice.call(arguments, 1);
    var a = this._cbs[evt];
    if (a) for (var k = 0; k < a.length; k++) a[k].apply(null, args);
  }

  // ── Getters ─────────────────────────────────────────────────────────────

  get active()   { return this.els[this.activeIdx]; }
  get preload()  { return this.els[1 - this.activeIdx]; }
  get paused()   { return !this._playing; }
  get duration() { return this.totalDuration; }
  get currentTime() { return this._getVT(); }

  // ── Public API ──────────────────────────────────────────────────────────

  /**
   * Load a sequence of segments for playback.
   * @param {Array} segs - Segment descriptors. Each:
   *   { src, startTime, endTime, label?,
   *     transition?: { type, duration, behavior?: { outgoing, audio } } }
   *
   * transition.behavior.outgoing: 'play' (default) | 'freeze' | 'play-past'
   *   'play'      – outgoing keeps playing but stops at its endTime
   *   'freeze'    – outgoing pauses at its last frame
   *   'play-past' – outgoing keeps playing past endTime during transition
   *
   * transition.behavior.audio: 'crossfade' (default) | 'cut' | 'fade-out-in'
   */
  load(segs) {
    this.stop();
    this.segments = this._buildTimeline(segs);
    this.totalDuration = this.segments.length > 0
      ? this.segments[this.segments.length - 1].vEnd
      : 0;
    this.currentSeg = -1;
    this._preloadedSeg = -1;

    if (this.segments.length > 0) {
      this._loadInto(0, this.activeIdx);
      this.currentSeg = 0;
      this._showEl(this.activeIdx);
      this._hideEl(1 - this.activeIdx);
    }

    this._emit('load', this.totalDuration);
  }

  play() {
    if (this.currentSeg < 0 || this.segments.length === 0) return;
    this._playing = true;
    this._initAudio();
    this.active.play().catch(function(){});
    this._startPoll();
    this._emit('play');
  }

  pause() {
    this._playing = false;
    this.active.pause();
    if (this._tr) this.preload.pause();
    this._stopPoll();
    this._emit('pause');
  }

  stop() {
    this.pause();
    this._cancelTr();
    this._clearFX();
    for (var k = 0; k < 2; k++) {
      this.els[k].pause();
      this.els[k].removeAttribute('src');
      this.els[k].load();
      this._hideEl(k);
    }
    this.currentSeg = -1;
    this._preloadedSeg = -1;
  }

  /** Seek to a virtual time across the full sequence. */
  seekTo(vt) {
    vt = Math.max(0, Math.min(vt, this.totalDuration));
    var info = this._findSeg(vt);
    if (info.index < 0) return;

    this._cancelTr();
    this._clearFX();

    if (info.index !== this.currentSeg) {
      this._loadInto(info.index, this.activeIdx);
      this.currentSeg = info.index;
      this._preloadedSeg = -1;
      this._showEl(this.activeIdx);
      this._hideEl(1 - this.activeIdx);
    }

    this.active.currentTime = info.localTime;
    if (this._playing) this.active.play().catch(function(){});
    this._preloadNext();
    this._onTimeUpdate();
  }

  setVolume(v) { for (var k = 0; k < 2; k++) this.els[k].volume = v; }
  setMuted(m)  { for (var k = 0; k < 2; k++) this.els[k].muted = m; }

  destroy() {
    this.stop();
    this._stopPoll();
    // Remove secondary video if we created it
    if (this.els[1] && this.els[1].parentElement === this.container) {
      this.els[1].remove();
    }
    this.overlay.remove();
    if (this._audioCtx) {
      this._audioCtx.close().catch(function(){});
      this._audioCtx = null;
    }
    this._cbs = {};
  }

  // ── Timeline ────────────────────────────────────────────────────────────

  _buildTimeline(raw) {
    var segs = [];
    var vOff = 0;
    for (var i = 0; i < raw.length; i++) {
      var s = raw[i];
      var dur = s.endTime - s.startTime;
      var tr = (i > 0 && s.transition) ? s.transition : null;
      var trDur = tr ? (tr.duration || 0) : 0;

      // Transition overlap
      if (i > 0 && trDur > 0) vOff -= trDur;

      var seg = {
        src: s.src,
        startTime: s.startTime,
        endTime: s.endTime,
        clipDuration: dur,
        label: s.label || '',
        vStart: vOff,
        vEnd: vOff + dur,
        transition: null,
      };

      if (tr && trDur > 0) {
        seg.transition = {
          type: tr.type || 'fade',
          duration: trDur,
          behavior: {
            outgoing: (tr.behavior && tr.behavior.outgoing) || 'play',
            audio:    (tr.behavior && tr.behavior.audio) || 'crossfade',
          },
          vTrStart: vOff,       // virtual time transition begins
          vTrEnd: vOff + trDur, // virtual time transition ends
        };
      }

      segs.push(seg);
      vOff += dur;
    }
    return segs;
  }

  _findSeg(vt) {
    for (var i = this.segments.length - 1; i >= 0; i--) {
      if (vt >= this.segments[i].vStart) {
        var seg = this.segments[i];
        return { index: i, localTime: seg.startTime + (vt - seg.vStart) };
      }
    }
    if (this.segments.length > 0) {
      return { index: 0, localTime: this.segments[0].startTime };
    }
    return { index: -1, localTime: 0 };
  }

  // ── Segment loading ─────────────────────────────────────────────────────

  _loadInto(segIndex, elIndex) {
    var seg = this.segments[segIndex];
    if (!seg) return;
    var el = this.els[elIndex];
    // Avoid reloading same source
    if (el.getAttribute('data-seq-src') !== seg.src) {
      el.setAttribute('data-seq-src', seg.src);
      el.src = seg.src;
    }
    el.currentTime = seg.startTime;
  }

  _preloadNext() {
    var nextIdx = this.currentSeg + 1;
    if (nextIdx >= this.segments.length) return;
    if (this._preloadedSeg === nextIdx) return;
    var elIdx = 1 - this.activeIdx;
    this._loadInto(nextIdx, elIdx);
    this._hideEl(elIdx);
    this._preloadedSeg = nextIdx;
  }

  _showEl(idx) {
    this.els[idx].style.display = '';
    this.els[idx].classList.remove('hidden');
  }
  _hideEl(idx) {
    this.els[idx].style.display = 'none';
  }

  // ── Polling loop ────────────────────────────────────────────────────────
  // We use rAF instead of timeupdate events for smoother transition tracking.

  _startPoll() {
    if (this._pollRAF) return;
    this._pollRAF = requestAnimationFrame(this._boundPoll);
  }
  _stopPoll() {
    if (this._pollRAF) { cancelAnimationFrame(this._pollRAF); this._pollRAF = null; }
  }
  _poll() {
    this._pollRAF = null;
    this._onTimeUpdate();
    if (this._playing || this._tr) {
      this._pollRAF = requestAnimationFrame(this._boundPoll);
    }
  }

  // ── Playback core ───────────────────────────────────────────────────────

  _getVT() {
    if (this.currentSeg < 0) return 0;
    var seg = this.segments[this.currentSeg];
    if (!seg) return 0;
    return seg.vStart + (this.active.currentTime - seg.startTime);
  }

  _onTimeUpdate() {
    if (this.currentSeg < 0) return;
    var seg = this.segments[this.currentSeg];
    var vt = this._getVT();

    this._emit('timeupdate', vt, this.totalDuration);

    // Transition tick
    if (this._tr) {
      this._tickTr();
      return;
    }

    // Check if approaching end → start transition or advance
    var localTime = this.active.currentTime;
    var nextIdx = this.currentSeg + 1;

    if (nextIdx < this.segments.length) {
      var next = this.segments[nextIdx];
      if (next.transition && next.transition.duration > 0) {
        var timeToEnd = seg.endTime - localTime;
        if (timeToEnd <= next.transition.duration && timeToEnd > 0) {
          this._beginTr(nextIdx);
          return;
        }
      }
    }

    // Past segment end without transition → advance
    if (localTime >= seg.endTime - 0.03) {
      this._advance();
    }

    // Ensure next is preloaded
    this._preloadNext();
  }

  _advance() {
    var nextIdx = this.currentSeg + 1;
    if (nextIdx >= this.segments.length) {
      // Sequence ended
      this.active.pause();
      this._playing = false;
      this._stopPoll();
      this._emit('ended');
      return;
    }
    this._hardCut(nextIdx);
  }

  _hardCut(nextIdx) {
    var oldIdx = this.activeIdx;
    this.activeIdx = 1 - this.activeIdx;
    this.currentSeg = nextIdx;

    if (this._preloadedSeg === nextIdx) {
      this._showEl(this.activeIdx);
      this.active.currentTime = this.segments[nextIdx].startTime;
      if (this._playing) this.active.play().catch(function(){});
    } else {
      this._loadInto(nextIdx, this.activeIdx);
      this._showEl(this.activeIdx);
      if (this._playing) this.active.play().catch(function(){});
    }

    this.els[oldIdx].pause();
    this._hideEl(oldIdx);

    this._preloadedSeg = -1;
    this._restoreAudioGains();
    this._preloadNext();
    this._emit('segmentchange', nextIdx);
  }

  // ── Transitions ─────────────────────────────────────────────────────────

  _beginTr(nextIdx) {
    var next = this.segments[nextIdx];
    var tr = next.transition;
    var preloadElIdx = 1 - this.activeIdx;

    // Ensure loaded
    if (this._preloadedSeg !== nextIdx) {
      this._loadInto(nextIdx, preloadElIdx);
      this._preloadedSeg = nextIdx;
    }

    var outEl = this.active;
    var inEl = this.els[preloadElIdx];

    // Show incoming underneath
    this._showEl(preloadElIdx);
    outEl.style.zIndex = '2';
    inEl.style.zIndex = '1';

    // Start incoming from its startTime
    inEl.currentTime = next.startTime;
    if (this._playing) inEl.play().catch(function(){});

    this._tr = {
      type: tr.type,
      duration: tr.duration,
      behavior: tr.behavior,
      outElIdx: this.activeIdx,
      inElIdx: preloadElIdx,
      nextSegIdx: nextIdx,
      startWall: performance.now(),
      frozenOut: false,
    };
  }

  _tickTr() {
    if (!this._tr) return;
    var elapsed = (performance.now() - this._tr.startWall) / 1000;
    var t = Math.min(1, elapsed / this._tr.duration);
    // Smoothstep easing for visual comfort
    t = t * t * (3 - 2 * t);

    var outEl = this.els[this._tr.outElIdx];
    var inEl = this.els[this._tr.inElIdx];
    var outSeg = this.segments[this.currentSeg];
    var behavior = this._tr.behavior || {};

    // Handle outgoing behavior
    if (!this._tr.frozenOut) {
      var outgoing = behavior.outgoing || 'play';
      if (outgoing === 'freeze') {
        if (outEl.currentTime >= outSeg.endTime - 0.03) {
          outEl.pause();
          outEl.currentTime = outSeg.endTime;
          this._tr.frozenOut = true;
        }
      } else if (outgoing === 'play') {
        if (outEl.currentTime >= outSeg.endTime - 0.03) {
          outEl.pause();
          this._tr.frozenOut = true;
        }
      }
      // 'play-past': don't stop, let it keep playing
    }

    // Apply visual effect
    var fx = FX[this._tr.type] || FX.fade;
    // Raw linear t for effect (smoothstep already applied)
    fx(outEl, inEl, t, this.overlay);

    // Audio crossfade
    this._updateAudioCrossfade(t);

    if (t >= 1) {
      this._endTr();
    }
  }

  _endTr() {
    if (!this._tr) return;
    var nextIdx = this._tr.nextSegIdx;
    var oldActiveIdx = this._tr.outElIdx;

    // Clean up
    this._clearFX();

    // Swap roles
    this.activeIdx = this._tr.inElIdx;
    this.currentSeg = nextIdx;

    this.els[oldActiveIdx].pause();
    this._hideEl(oldActiveIdx);
    this.active.style.zIndex = '1';
    this.preload.style.zIndex = '0';

    this._restoreAudioGains();
    this._tr = null;
    this._preloadedSeg = -1;
    this._preloadNext();
    this._emit('segmentchange', nextIdx);
  }

  _cancelTr() {
    if (!this._tr) return;
    this._tr = null;
    this._clearFX();
    this._restoreAudioGains();
  }

  _clearFX() {
    for (var k = 0; k < 2; k++) {
      var el = this.els[k];
      el.style.opacity = '';
      el.style.transform = '';
      el.style.filter = '';
      el.style.clipPath = '';
      el.style.maskImage = '';
      el.style.webkitMaskImage = '';
      el.style.zIndex = '';
    }
    this.overlay.style.display = 'none';
    this.overlay.style.opacity = '';
    this.overlay.style.background = '';
  }

  // ── Audio crossfade (Web Audio API) ─────────────────────────────────────

  _initAudio() {
    if (this._audioInited) return;
    // Only init audio when both elements have non-muted sources
    if (this.els[0].muted && this.els[1].muted) return;
    try {
      this._audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      for (var k = 0; k < 2; k++) {
        var src = this._audioCtx.createMediaElementSource(this.els[k]);
        var gain = this._audioCtx.createGain();
        src.connect(gain);
        gain.connect(this._audioCtx.destination);
        this._sources[k] = src;
        this._gains[k] = gain;
      }
      this._audioInited = true;
      if (this._audioCtx.state === 'suspended') this._audioCtx.resume();
      this._restoreAudioGains();
    } catch (e) {
      console.warn('SequencePlayback: Web Audio init failed', e);
    }
  }

  _restoreAudioGains() {
    if (!this._gains[0] || !this._gains[1]) return;
    this._gains[this.activeIdx].gain.value = 1;
    this._gains[1 - this.activeIdx].gain.value = 0;
  }

  _updateAudioCrossfade(t) {
    if (!this._gains[0] || !this._gains[1] || !this._tr) return;

    var outGain = this._gains[this._tr.outElIdx];
    var inGain = this._gains[this._tr.inElIdx];
    var mode = (this._tr.behavior && this._tr.behavior.audio) || 'crossfade';

    switch (mode) {
      case 'crossfade':
        // Equal-power crossfade
        outGain.gain.value = Math.cos(t * Math.PI / 2);
        inGain.gain.value = Math.sin(t * Math.PI / 2);
        break;
      case 'cut':
        outGain.gain.value = t < 0.5 ? 1 : 0;
        inGain.gain.value = t < 0.5 ? 0 : 1;
        break;
      case 'fade-out-in':
        if (t < 0.5) {
          outGain.gain.value = 1 - t * 2;
          inGain.gain.value = 0;
        } else {
          outGain.gain.value = 0;
          inGain.gain.value = (t - 0.5) * 2;
        }
        break;
    }
  }
}
