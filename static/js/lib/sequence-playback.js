import { pageFrame } from './page-scope.js';
import { seekVideo } from './utils.js';
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

    // Ensure stacking context. pointer-events none so the stitch seek bar stays clickable.
    for (var vi = 0; vi < 2; vi++) {
      var v = this.els[vi];
      v.style.position = 'absolute';
      v.style.inset = '0';
      v.style.width = '100%';
      v.style.height = '100%';
      v.style.objectFit = 'contain';
      v.style.pointerEvents = 'none';
      v.playsInline = true;
      v.preload = 'auto';
    }

    // Dual title cards so title↔title and title↔video transitions can run the same FX as videos.
    this.titleEls = [this._mkTitle(), this._mkTitle()];

    // Overlay div for fade-through-color effects. Keep videos/overlay under title + seek UI.
    this.overlay = document.createElement('div');
    this.overlay.style.cssText =
      'position:absolute;inset:0;display:none;pointer-events:none;z-index:4';
    if (!this.els[0].parentElement) container.appendChild(this.els[0]);
    if (!this.els[1].parentElement) this.els[0].insertAdjacentElement('afterend', this.els[1]);
    if (!this.titleEls[0].parentElement) this.els[1].insertAdjacentElement('afterend', this.titleEls[0]);
    if (!this.titleEls[1].parentElement) this.titleEls[0].insertAdjacentElement('afterend', this.titleEls[1]);
    if (!this.overlay.parentElement) this.titleEls[1].insertAdjacentElement('afterend', this.overlay);

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

    // Audio (Web Audio API for crossfading + loudness match)
    this._audioCtx = null;
    this._gains = [null, null];
    this._sources = [null, null];
    this._analysers = [null, null];
    this._compressors = [null, null];
    this._autoGain = [1, 1];
    this._userGain = [1, 1];
    this._autoGainCache = {};
    this._autoGainAcc = {};
    this._xfadeLin = [1, 0];
    this._outMuted = false;
    this.matchLoudness = true;
    this._audioInited = false;
    this._rmsBuf = null;

    // Event listeners
    this._cbs = {};

    // Attach handlers
    this._boundTU = this._onTimeUpdate.bind(this);
    this._boundPoll = this._poll.bind(this);
    this._pollRAF = null;
    this._playGen = 0;
    this._cutLock = false;
    this._titleWall = null;
    this._titleVT = 0;

    // Clear coalesced seeks once they actually land so play() does not freeze on them.
    // timeupdate/ended are a backup when rAF is throttled so we still cut at the out point.
    for (var si = 0; si < 2; si++) {
      this.els[si].addEventListener('seeked', function() {
        var p = this._pendingSeek;
        if (Number.isFinite(p) && Math.abs((this.currentTime || 0) - p) < 0.5) {
          this._pendingSeek = NaN;
        }
      });
      this.els[si].addEventListener('timeupdate', this._boundTU);
      this.els[si].addEventListener('ended', this._boundTU);
    }
  }

  _mkVideo() {
    var v = document.createElement('video');
    v.playsInline = true;
    v.preload = 'auto';
    return v;
  }

  _mkTitle() {
    var d = document.createElement('div');
    d.className = 'seq-title hidden';
    d.style.cssText = 'position:absolute;inset:0;display:none;flex-direction:column;align-items:center;justify-content:center;pointer-events:none;z-index:3;text-align:center;padding:8%';
    var t = document.createElement('div');
    t.className = 'seq-title-text';
    t.style.cssText = 'font-weight:700;line-height:1.15';
    var s = document.createElement('div');
    s.className = 'seq-title-sub';
    s.style.cssText = 'opacity:0.6;margin-top:0.35em';
    d.append(t, s);
    return d;
  }

  _paintTitle(idx, seg) {
    var d = this.titleEls[idx];
    if (!d || !seg) return;
    var raw = seg.title || {};
    var scale = (d.parentElement && d.parentElement.clientHeight ? d.parentElement.clientHeight : 280) / 1080;
    var fs = Math.round((raw.font_size || 72) * scale);
    d.style.backgroundColor = raw.bg_color || '#000000';
    d.style.color = raw.text_color || '#ffffff';
    var justify = raw.position === 'top-center' ? 'flex-start' : raw.position === 'bottom-center' ? 'flex-end' : 'center';
    d.style.justifyContent = justify;
    d.style.paddingTop = raw.position === 'top-center' ? '12%' : '8%';
    d.style.paddingBottom = raw.position === 'bottom-center' ? '12%' : '8%';
    var text = d.querySelector('.seq-title-text');
    var sub = d.querySelector('.seq-title-sub');
    if (text) {
      text.textContent = raw.text || seg.label || '';
      text.style.fontSize = fs + 'px';
      text.style.fontFamily = (raw.font || 'UnifrakturCook') + ', serif';
      text.style.color = raw.text_color || '#ffffff';
    }
    if (sub) {
      if (raw.subtitle) {
        sub.textContent = raw.subtitle;
        sub.style.display = '';
        sub.style.fontSize = Math.round(fs * 0.5) + 'px';
        sub.style.fontFamily = 'Tomorrow, system-ui, sans-serif';
        sub.style.color = raw.text_color || '#ffffff';
      } else {
        sub.textContent = '';
        sub.style.display = 'none';
      }
    }
  }

  _layerEl(idx, seg) {
    return this._isTitle(seg) ? this.titleEls[idx] : this.els[idx];
  }

  _present(idx, seg) {
    if (this._isTitle(seg)) {
      this._hideEl(idx);
      this._paintTitle(idx, seg);
      this.titleEls[idx].classList.remove('hidden');
      this.titleEls[idx].style.display = 'flex';
      this._applyLook(idx);
    } else {
      this._hideTitle(idx);
      this._showEl(idx);
    }
  }

  _hideTitle(idx) {
    var d = this.titleEls[idx];
    if (!d) return;
    d.style.display = 'none';
    d.classList.add('hidden');
  }

  _hideLayer(idx) {
    this._hideEl(idx);
    this._hideTitle(idx);
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
    this._autoGainAcc = {};

    if (this.segments.length > 0) {
      this.currentSeg = 0;
      var first = this.segments[0];
      this._hideLayer(1 - this.activeIdx);
      if (this._isTitle(first)) {
        this._titleVT = first.vStart;
        this._titleWall = null;
        this._present(this.activeIdx, first);
      } else {
        this._loadInto(0, this.activeIdx);
        this._present(this.activeIdx, first);
      }
      this._preloadNext();
    }

    this._emit('load', this.totalDuration);
    if (this.currentSeg >= 0) this._emit('segmentchange', this.currentSeg);
  }

  play() {
    if (this.currentSeg < 0 || this.segments.length === 0) return;
    this._playing = true;
    this._initAudio();
    this._syncMixForCurrent();
    var seg = this.segments[this.currentSeg];
    if (this._isTitle(seg)) {
      this._hideLayer(1 - this.activeIdx);
      this._present(this.activeIdx, seg);
      var local = Math.max(0, (this._titleVT || seg.vStart) - seg.vStart);
      this._titleWall = performance.now() - local * 1000;
    } else {
      this._titleWall = null;
      this._hideLayer(1 - this.activeIdx);
      this._present(this.activeIdx, seg);
      this._playWhenReady(this.active, seg);
    }
    this._preloadNext();
    this._startPoll();
    this._emit('play');
    this._emit('segmentchange', this.currentSeg);
  }

  pause() {
    this._playing = false;
    this._playGen++;
    this._clearPlayWait(this.els[0]);
    this._clearPlayWait(this.els[1]);
    this.els[0].pause();
    this.els[1].pause();
    this._stopPoll();
    this._emit('pause');
  }

  stop() {
    this.pause();
    this._cancelTr();
    this._clearFX();
    this._playGen++;
    this._clearPlayWait(this.els[0]);
    this._clearPlayWait(this.els[1]);
    for (var k = 0; k < 2; k++) {
      this.els[k].pause();
      this.els[k].removeAttribute('src');
      this.els[k].load();
      this._hideLayer(k);
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
      if (!this._isTitle(this.segments[info.index])) {
        this._loadInto(info.index, this.activeIdx);
      }
      this.currentSeg = info.index;
      this._preloadedSeg = -1;
    }

    var seg = this.segments[info.index];
    if (this._isTitle(seg)) {
      this._hideLayer(1 - this.activeIdx);
      this._present(this.activeIdx, seg);
      this._titleVT = vt;
      if (this._playing) this._titleWall = performance.now() - (vt - seg.vStart) * 1000;
      else this._titleWall = null;
      this._preloadNext();
      this._emit('segmentchange', info.index);
      this._onTimeUpdate();
      return;
    }

    this._titleWall = null;
    this._hideLayer(1 - this.activeIdx);
    this._present(this.activeIdx, seg);

    seekVideo(this.active, info.localTime);
    if (this._playing) this._playWhenReady(this.active, seg);
    this._preloadNext();
    this._emit('segmentchange', info.index);
    this._onTimeUpdate();
  }

  /** Pause and hide both videos without unloading sources (title overlay / empty preview). */
  hide() {
    this.pause();
    this._cancelTr();
    this._clearFX();
    this._hideLayer(0);
    this._hideLayer(1);
  }

  setVolume(v) { for (var k = 0; k < 2; k++) this.els[k].volume = v; }
  setMuted(m)  {
    this._outMuted = !!m;
    for (var k = 0; k < 2; k++) this.els[k].muted = !!m && !this._audioInited;
    this._applyMixGains();
  }
  setMatchLoudness(on) {
    this.matchLoudness = !!on;
    if (!on) {
      this._autoGain[0] = 1;
      this._autoGain[1] = 1;
    }
    this._configureCompressor(this.matchLoudness);
    this._applyMixGains();
  }

  destroy() {
    this.stop();
    this._stopPoll();
    // Remove secondary video if we created it
    if (this.els[1] && this.els[1].parentElement === this.container) {
      this.els[1].remove();
    }
    for (var ti = 0; ti < 2; ti++) {
      if (this.titleEls[ti] && this.titleEls[ti].parentElement === this.container) this.titleEls[ti].remove();
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
      var isTitle = s.kind === 'title' || (!s.src && (s.duration > 0 || s.kind === 'title'));
      var dur = isTitle
        ? Math.max(0.1, s.duration || (s.endTime - s.startTime) || 3)
        : (s.endTime - s.startTime);
      var tr = (i > 0 && s.transition) ? s.transition : null;
      var trDur = tr ? (tr.duration || 0) : 0;

      // Transition overlap
      if (i > 0 && trDur > 0) vOff -= trDur;

      var seg = {
        kind: isTitle ? 'title' : 'video',
        src: isTitle ? '' : s.src,
        startTime: isTitle ? 0 : s.startTime,
        endTime: isTitle ? dur : s.endTime,
        clipDuration: dur,
        label: s.label || '',
        title: s.title || null,
        gainDb: Number(s.gainDb) || 0,
        look: s.look || '',
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

  _isTitle(seg) { return !!(seg && seg.kind === 'title'); }

  _nextVideoIndex(fromIdx) {
    var i = fromIdx + 1;
    while (i < this.segments.length && this._isTitle(this.segments[i])) i++;
    return i < this.segments.length ? i : -1;
  }

  _elReadyFor(el, seg) {
    if (!el || !seg || this._isTitle(seg)) return false;
    if (el.getAttribute('data-seq-src') !== seg.src) return false;
    if (el.readyState < 2) return false;
    if (el.seeking) return false;
    var t = el.currentTime || 0;
    return Math.abs(t - seg.startTime) < 0.18;
  }

  _loadInto(segIndex, elIndex) {
    var seg = this.segments[segIndex];
    if (!seg || this._isTitle(seg) || !seg.src) return;
    var el = this.els[elIndex];
    if (el.getAttribute('data-seq-src') !== seg.src) {
      this._clearPrime(el);
      el.setAttribute('data-seq-src', seg.src);
      el.src = seg.src;
    }
    if (!this._elReadyFor(el, seg)) {
      this._clearPrime(el);
      seekVideo(el, seg.startTime);
    }
    this._primeEl(el);
  }

  _clearPrime(el) {
    if (!el) return;
    el._seqPrimed = false;
    if (el._seqPrimeFn) {
      el.removeEventListener('seeked', el._seqPrimeFn);
      el.removeEventListener('canplay', el._seqPrimeFn);
      el.removeEventListener('loadeddata', el._seqPrimeFn);
      el._seqPrimeFn = null;
    }
  }

  /** Decode a frame on the hidden buffer so the cut is not a black seek. */
  _primeEl(el) {
    if (!el || el._seqPrimed || el._seqPrimeFn) return;
    var self = this;
    var onReady = function() {
      if (el._seqPrimed) return;
      if (el.readyState < 2) return;
      var fn = el._seqPrimeFn;
      if (fn) {
        el.removeEventListener('seeked', fn);
        el.removeEventListener('canplay', fn);
        el.removeEventListener('loadeddata', fn);
        el._seqPrimeFn = null;
      }
      var wasMuted = el.muted;
      el.muted = true;
      var done = function() {
        el._seqPrimed = true;
        var cur = self.segments[self.currentSeg];
        var isActiveVideo = el === self.active && cur && !self._isTitle(cur) && self._playing;
        if (!isActiveVideo) el.pause();
        el.muted = wasMuted;
      };
      var p = el.play();
      if (p && p.then) p.then(done).catch(done);
      else done();
    };
    el._seqPrimeFn = onReady;
    el.addEventListener('seeked', onReady);
    el.addEventListener('canplay', onReady);
    el.addEventListener('loadeddata', onReady);
    onReady();
  }

  _preloadNext() {
    var incomingIdx = this.currentSeg + 1;
    var elIdx = 1 - this.activeIdx;
    if (incomingIdx < this.segments.length && this._isTitle(this.segments[incomingIdx])) {
      if (this._preloadedSeg !== incomingIdx) this._paintTitle(elIdx, this.segments[incomingIdx]);
      this._preloadedSeg = incomingIdx;
      return;
    }
    var nextIdx = this._nextVideoIndex(this.currentSeg);
    if (nextIdx < 0) return;
    if (this._preloadedSeg === nextIdx) {
      this._primeEl(this.els[elIdx]);
      return;
    }
    this._loadInto(nextIdx, elIdx);
    this._hideEl(elIdx);
    this._preloadedSeg = nextIdx;
  }

  _showEl(idx) {
    this.els[idx].classList.remove('hidden');
    this.els[idx].style.display = '';
    this._applyLook(idx);
  }
  _hideEl(idx) {
    this.els[idx].style.display = 'none';
    this.els[idx].classList.add('hidden');
  }

  _segLocalRange(seg, t) {
    return t >= seg.startTime - 0.5 && t <= seg.endTime + 0.5;
  }

  /**
   * Classify the element's currentTime against the active segment.
   * 'before'/'seeking' = in-point has not landed (do not move the playhead back to 0).
   * 'past' = played through the out-point (cut / advance).
   * 'in' = inside the clip.
   */
  _localState(el, seg) {
    var t = (el && el.currentTime) || 0;
    var pending = el && el._pendingSeek;
    if (el && el.seeking && Number.isFinite(pending)) {
      return { t: pending, kind: 'seeking' };
    }
    if (t < seg.startTime - 0.5) {
      return { t: Number.isFinite(pending) ? pending : seg.startTime, kind: 'before' };
    }
    if (t >= seg.endTime - 0.03) {
      return { t: Math.max(t, seg.endTime), kind: 'past' };
    }
    return { t: t, kind: 'in' };
  }

  /** True when currentTime is the in-segment frame we asked for, not t=0 of a huge file. */
  _seekHasLanded(el, seg) {
    if (!el || el.seeking) return false;
    var t = el.currentTime || 0;
    if (seg && !this._segLocalRange(seg, t)) return false;
    var pending = el._pendingSeek;
    if (Number.isFinite(pending) && Math.abs(t - pending) >= 0.5) return false;
    if (Number.isFinite(pending) && Math.abs(t - pending) < 0.5) el._pendingSeek = NaN;
    return true;
  }

  _clearPlayWait(el) {
    if (!el || !el._seqPlayOnReady) return;
    el.removeEventListener('seeked', el._seqPlayOnReady);
    el.removeEventListener('canplay', el._seqPlayOnReady);
    el.removeEventListener('loadeddata', el._seqPlayOnReady);
    el._seqPlayOnReady = null;
  }

  /** Start playback only after the pending seek has landed in the current segment. */
  _playWhenReady(el, seg) {
    if (!this._playing || !el) return;
    var self = this;
    var gen = ++this._playGen;
    this._clearPlayWait(this.els[0]);
    this._clearPlayWait(this.els[1]);

    var tryStart = function() {
      if (!self._playing || self._playGen !== gen) {
        self._clearPlayWait(el);
        return true;
      }
      if (!self._seekHasLanded(el, seg)) return false;
      if (el.readyState < 2) return false;
      self._clearPlayWait(el);
      el.play().catch(function() {});
      return true;
    };

    if (tryStart()) return;
    var onReady = function() { tryStart(); };
    el._seqPlayOnReady = onReady;
    el.addEventListener('seeked', onReady);
    el.addEventListener('canplay', onReady);
    el.addEventListener('loadeddata', onReady);
  }

  // ── Polling loop ────────────────────────────────────────────────────────
  // We use rAF instead of timeupdate events for smoother transition tracking.

  _startPoll() {
    if (this._pollRAF) return;
    this._pollRAF = pageFrame(this._boundPoll);
  }
  _stopPoll() {
    if (this._pollRAF) { cancelAnimationFrame(this._pollRAF); this._pollRAF = null; }
  }
  _poll() {
    this._pollRAF = null;
    this._onTimeUpdate();
    if (this._playing || this._tr) {
      this._pollRAF = pageFrame(this._boundPoll);
    }
  }

  // ── Playback core ───────────────────────────────────────────────────────

  _getVT() {
    if (this.currentSeg < 0) return 0;
    var seg = this.segments[this.currentSeg];
    if (!seg) return 0;
    if (this._isTitle(seg)) {
      var tvt;
      if (this._playing && this._titleWall != null) {
        tvt = seg.vStart + (performance.now() - this._titleWall) / 1000;
      } else {
        tvt = Number.isFinite(this._titleVT) ? this._titleVT : seg.vStart;
      }
      if (this.totalDuration > 0) return Math.max(0, Math.min(tvt, this.totalDuration));
      return Math.max(0, tvt);
    }
    var st = this._localState(this.active, seg);
    var local = st.kind === 'past' ? seg.endTime : st.t;
    if (!this._playing && st.kind === 'in') {
      var pending = this.active._pendingSeek;
      if (Number.isFinite(pending)) local = pending;
    }

    var vt = seg.vStart + (local - seg.startTime);
    if (this.totalDuration > 0) return Math.max(0, Math.min(vt, this.totalDuration));
    return Math.max(0, vt);
  }

  _onTimeUpdate() {
    if (this.currentSeg < 0 || this._cutLock) return;
    var seg = this.segments[this.currentSeg];
    var vt = this._getVT();

    this._emit('timeupdate', vt, this.totalDuration);

    // Transition tick
    if (this._tr) {
      this._tickTr();
      return;
    }

    if (this._isTitle(seg)) {
      this._preloadNext();
      var nextTitleIdx = this.currentSeg + 1;
      if (this._playing && nextTitleIdx < this.segments.length) {
        var nextTitle = this.segments[nextTitleIdx];
        if (nextTitle.transition && nextTitle.transition.duration > 0) {
          var titleTimeToEnd = seg.vEnd - vt;
          if (titleTimeToEnd <= nextTitle.transition.duration && titleTimeToEnd > 0) {
            this._beginTr(nextTitleIdx);
            return;
          }
        }
      }
      if (this._playing && vt >= seg.vEnd - 0.03) this._advance();
      return;
    }
    if (this._playing) this._sampleAutoGain();

    var st = this._localState(this.active, seg);
    // Seek still in flight (currentTime at 0 of a long file) — hold playhead, do not cut.
    if (st.kind === 'before' || st.kind === 'seeking') {
      this._preloadNext();
      return;
    }

    var localTime = st.t;
    var nextIdx = this.currentSeg + 1;

    if (st.kind === 'past') {
      this.active.pause();
      if (this._playing) this._advance();
      return;
    }

    if (nextIdx < this.segments.length) {
      var next = this.segments[nextIdx];
      // Title cards are wall-clock; do not start an xfade early and clip the outgoing shot.
      if (next.transition && next.transition.duration > 0) {
        var timeToEnd = seg.endTime - localTime;
        if (timeToEnd <= next.transition.duration && timeToEnd > 0) {
          this._beginTr(nextIdx);
          return;
        }
      }
    }

    if (localTime >= seg.endTime - 0.03) {
      this.active.pause();
      this._advance();
    }

    this._preloadNext();
  }

  _advance() {
    var nextIdx = this.currentSeg + 1;
    if (nextIdx >= this.segments.length) {
      this.active.pause();
      this._playing = false;
      this._stopPoll();
      this._emit('ended');
      return;
    }
    this._hardCut(nextIdx);
  }

  _hardCut(nextIdx) {
    if (this._cutLock) return;
    this._cutLock = true;
    var next = this.segments[nextIdx];
    var hadPreload = this._preloadedSeg === nextIdx;
    var savedPreload = this._preloadedSeg;
    var oldIdx = this.activeIdx;
    this.currentSeg = nextIdx;
    this._playGen++;
    this._cancelTr();
    this._clearFX();

    if (this._isTitle(next)) {
      this.els[0].pause(); this.els[1].pause();
      this.activeIdx = 1 - oldIdx;
      this._hideLayer(oldIdx);
      this._present(this.activeIdx, next);
      this._titleVT = next.vStart;
      if (this._playing) this._titleWall = performance.now();
      var nextVid = this._nextVideoIndex(nextIdx);
      this._preloadedSeg = (savedPreload === nextVid) ? savedPreload : -1;
      this._preloadNext();
      this._cutLock = false;
      this._emit('segmentchange', nextIdx);
      return;
    }

    this._preloadedSeg = -1;
    this._titleWall = null;
    this.activeIdx = 1 - oldIdx;

    this.active.pause();
    this._hideTitle(this.activeIdx);
    if (hadPreload && this._elReadyFor(this.active, next)) {
      this._present(this.activeIdx, next);
      if (this._playing) this._playWhenReady(this.active, next);
    } else if (hadPreload) {
      this._present(this.activeIdx, next);
      seekVideo(this.active, next.startTime);
      if (this._playing) this._playWhenReady(this.active, next);
    } else {
      this._loadInto(nextIdx, this.activeIdx);
      this._present(this.activeIdx, next);
      if (this._playing) this._playWhenReady(this.active, next);
    }

    this.els[oldIdx].pause();
    this._hideLayer(oldIdx);

    this._restoreAudioGains();
    this._preloadNext();
    this._cutLock = false;
    this._emit('segmentchange', nextIdx);
  }

  // ── Transitions ─────────────────────────────────────────────────────────

  _beginTr(nextIdx) {
    var next = this.segments[nextIdx];
    var outSeg = this.segments[this.currentSeg];
    var tr = next.transition;
    if (!tr) { this._advance(); return; }
    var preloadElIdx = 1 - this.activeIdx;

    if (this._isTitle(next)) {
      this._paintTitle(preloadElIdx, next);
    } else if (this._preloadedSeg !== nextIdx) {
      this._loadInto(nextIdx, preloadElIdx);
      this._preloadedSeg = nextIdx;
    }

    var outEl = this._layerEl(this.activeIdx, outSeg);
    var inEl = this._layerEl(preloadElIdx, next);

    this._present(preloadElIdx, next);
    outEl.style.zIndex = '4';
    inEl.style.zIndex = '3';

    if (!this._isTitle(next)) {
      if (!this._elReadyFor(this.els[preloadElIdx], next)) seekVideo(this.els[preloadElIdx], next.startTime);
      if (this._playing) this._playWhenReady(this.els[preloadElIdx], next);
    }

    this._tr = {
      type: tr.type,
      duration: tr.duration,
      behavior: tr.behavior,
      outElIdx: this.activeIdx,
      inElIdx: preloadElIdx,
      nextSegIdx: nextIdx,
      startWall: performance.now(),
      frozenOut: this._isTitle(outSeg),
    };
  }

  _tickTr() {
    if (!this._tr) return;
    var elapsed = (performance.now() - this._tr.startWall) / 1000;
    var t = Math.min(1, elapsed / this._tr.duration);
    // Smoothstep easing for visual comfort
    t = t * t * (3 - 2 * t);

    var outSeg = this.segments[this.currentSeg];
    var inSeg = this.segments[this._tr.nextSegIdx];
    var outEl = this._layerEl(this._tr.outElIdx, outSeg);
    var inEl = this._layerEl(this._tr.inElIdx, inSeg);
    var behavior = this._tr.behavior || {};

    // Handle outgoing behavior
    if (!this._tr.frozenOut && !this._isTitle(outSeg)) {
      var outgoing = behavior.outgoing || 'play';
      var outVideo = this.els[this._tr.outElIdx];
      if (outgoing === 'freeze') {
        if (outVideo.currentTime >= outSeg.endTime - 0.03) {
          outVideo.pause();
          outVideo.currentTime = outSeg.endTime;
          this._tr.frozenOut = true;
        }
      } else if (outgoing === 'play') {
        if (outVideo.currentTime >= outSeg.endTime - 0.03) {
          outVideo.pause();
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
    var incoming = this.segments[this.currentSeg];
    if (this._isTitle(incoming)) {
      var into = Math.min(incoming.clipDuration || incoming.endTime || 0, Number(this._tr.duration) || 0);
      this._titleVT = incoming.vStart + into;
      this._titleWall = this._playing ? performance.now() - into * 1000 : null;
    } else {
      this._titleWall = null;
    }

    this.els[oldActiveIdx].pause();
    this._hideLayer(oldActiveIdx);
    var shown = this._layerEl(this.activeIdx, incoming);
    if (shown) shown.style.zIndex = '3';
    this.els[oldActiveIdx].style.zIndex = '';
    this.titleEls[oldActiveIdx].style.zIndex = '';

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
    var reset = function(el) {
      if (!el) return;
      el.style.opacity = '';
      el.style.transform = '';
      el.style.filter = '';
      el.style.clipPath = '';
      el.style.maskImage = '';
      el.style.webkitMaskImage = '';
      el.style.zIndex = '';
    };
    for (var k = 0; k < 2; k++) {
      reset(this.els[k]);
      reset(this.titleEls[k]);
    }
    this.overlay.style.display = 'none';
    this.overlay.style.opacity = '';
    this.overlay.style.background = '';
  }

  // ── Audio crossfade (Web Audio API) ─────────────────────────────────────

  _initAudio() {
    if (this._audioInited) return;
    try {
      this._audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      for (var k = 0; k < 2; k++) {
        var src = this._audioCtx.createMediaElementSource(this.els[k]);
        var analyser = this._audioCtx.createAnalyser();
        analyser.fftSize = 2048;
        analyser.smoothingTimeConstant = 0.6;
        var gain = this._audioCtx.createGain();
        var comp = this._audioCtx.createDynamicsCompressor();
        src.connect(analyser);
        src.connect(gain);
        gain.connect(comp);
        comp.connect(this._audioCtx.destination);
        this._sources[k] = src;
        this._analysers[k] = analyser;
        this._gains[k] = gain;
        this._compressors[k] = comp;
      }
      this._audioInited = true;
      this._configureCompressor(this.matchLoudness);
      for (var u = 0; u < 2; u++) this.els[u].muted = false;
      if (this._audioCtx.state === 'suspended') this._audioCtx.resume();
      this._restoreAudioGains();
    } catch (e) {
      console.warn('SequencePlayback: Web Audio init failed', e);
    }
  }

  _lookCSS(look) {
    switch (look) {
      case 'punch': return 'contrast(1.12) saturate(1.1)';
      case 'warm': return 'sepia(0.18) saturate(1.12)';
      case 'cool': return 'hue-rotate(12deg) saturate(0.92) brightness(1.02)';
      case 'noir': return 'grayscale(1) contrast(1.22)';
      default: return '';
    }
  }

  _applyLook(idx) {
    var el = this.els[idx];
    if (!el) return;
    var segIdx = idx === this.activeIdx ? this.currentSeg : this._preloadedSeg;
    var seg = (segIdx >= 0) ? this.segments[segIdx] : null;
    el.style.filter = this._lookCSS(seg && seg.look);
  }

  _syncMixForCurrent() {
    var seg = this.segments[this.currentSeg];
    var user = (seg && Number.isFinite(seg.gainDb)) ? Math.pow(10, seg.gainDb / 20) : 1;
    this._userGain[this.activeIdx] = user;
    this._applyLook(this.activeIdx);
    this._applyLook(1 - this.activeIdx);
    if (seg && !this._isTitle(seg) && this.matchLoudness) {
      var cached = this._autoGainCache[this._mixKey(seg)];
      if (Number.isFinite(cached)) this._autoGain[this.activeIdx] = Math.pow(10, cached / 20);
    }
    this._applyMixGains();
  }

  _mixKey(seg) {
    return (seg.src || '') + ':' + (seg.startTime || 0);
  }

  _configureCompressor(on) {
    // Bypass always. DynamicsCompressor on speech is the MATCH LEVELS crunch;
    // preview matching is gain-only. Export still uses ffmpeg loudnorm.
    for (var k = 0; k < 2; k++) {
      var comp = this._compressors[k];
      if (!comp) continue;
      comp.threshold.value = 0;
      comp.knee.value = 0;
      comp.ratio.value = 1;
      comp.attack.value = 0.003;
      comp.release.value = 0.05;
    }
  }

  _sampleAutoGain() {
    if (!this.matchLoudness || !this._audioInited) return;
    var seg = this.segments[this.currentSeg];
    if (!seg || this._isTitle(seg)) return;
    var key = this._mixKey(seg);
    if (Number.isFinite(this._autoGainCache[key])) return;
    var an = this._analysers[this.activeIdx];
    if (!an) return;
    if (!this._rmsBuf || this._rmsBuf.length !== an.fftSize) {
      this._rmsBuf = new Float32Array(an.fftSize);
    }
    try { an.getFloatTimeDomainData(this._rmsBuf); } catch (e) { return; }
    var sum = 0;
    var peak = 0;
    for (var i = 0; i < this._rmsBuf.length; i++) {
      var s = this._rmsBuf[i];
      var a = s < 0 ? -s : s;
      if (a > peak) peak = a;
      sum += s * s;
    }
    var rms = Math.sqrt(sum / this._rmsBuf.length);
    // Skip silence / breaths so one quiet window cannot lock a huge boost.
    if (rms < 0.012) return;
    var acc = this._autoGainAcc[key] || { sumSq: 0, n: 0, peak: 0 };
    acc.sumSq += rms * rms;
    acc.n++;
    if (peak > acc.peak) acc.peak = peak;
    this._autoGainAcc[key] = acc;
    if (acc.n < 18) return;
    var meanRms = Math.sqrt(acc.sumSq / acc.n);
    if (meanRms < 1e-6) return;
    var db = 20 * Math.log10(meanRms);
    var gainDb = Math.max(-6, Math.min(6, -18 - db));
    // Already-hot clips clip if we boost; only attenuate those.
    if (acc.peak > 0.45 && gainDb > 0) gainDb = 0;
    this._autoGainCache[key] = gainDb;
    this._autoGain[this.activeIdx] = Math.pow(10, gainDb / 20);
    this._applyMixGains();
  }

  _applyMixGains() {
    if (!this._gains[0] || !this._gains[1]) return;
    var mute = this._outMuted ? 0 : 1;
    for (var k = 0; k < 2; k++) {
      var xfade = this._xfadeLin[k];
      if (!Number.isFinite(xfade)) xfade = (k === this.activeIdx) ? 1 : 0;
      var auto = this.matchLoudness ? (this._autoGain[k] || 1) : 1;
      var user = this._userGain[k] || 1;
      this._gains[k].gain.value = xfade * auto * user * mute;
    }
  }

  levelDb() {
    var an = this._analysers[this.activeIdx];
    if (!an) return -60;
    if (!this._rmsBuf || this._rmsBuf.length !== an.fftSize) {
      this._rmsBuf = new Float32Array(an.fftSize);
    }
    try { an.getFloatTimeDomainData(this._rmsBuf); } catch (e) { return -60; }
    var sum = 0;
    for (var i = 0; i < this._rmsBuf.length; i++) sum += this._rmsBuf[i] * this._rmsBuf[i];
    var rms = Math.sqrt(sum / this._rmsBuf.length);
    if (rms < 1e-6) return -60;
    return 20 * Math.log10(rms);
  }

  _restoreAudioGains() {
    this._xfadeLin[this.activeIdx] = 1;
    this._xfadeLin[1 - this.activeIdx] = 0;
    this._syncMixForCurrent();
  }

  _updateAudioCrossfade(t) {
    if (!this._gains[0] || !this._gains[1] || !this._tr) return;

    var outIdx = this._tr.outElIdx;
    var inIdx = this._tr.inElIdx;
    var mode = (this._tr.behavior && this._tr.behavior.audio) || 'crossfade';
    var outX = 0;
    var inX = 0;

    switch (mode) {
      case 'crossfade':
        outX = Math.cos(t * Math.PI / 2);
        inX = Math.sin(t * Math.PI / 2);
        break;
      case 'cut':
        outX = t < 0.5 ? 1 : 0;
        inX = t < 0.5 ? 0 : 1;
        break;
      case 'fade-out-in':
        if (t < 0.5) { outX = 1 - t * 2; inX = 0; }
        else { outX = 0; inX = (t - 0.5) * 2; }
        break;
      default:
        outX = 1 - t;
        inX = t;
    }
    this._xfadeLin[outIdx] = outX;
    this._xfadeLin[inIdx] = inX;
    this._applyMixGains();
  }
}

