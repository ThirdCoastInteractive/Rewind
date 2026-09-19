import { listen as pageListen, pageInterval, pageTimeout, pageFrame, pageFetch, PageMutationObserver, onPageCleanup } from './lib/page-scope.js';
/**
 * Custom Video Player with YouTube-like controls and keyboard shortcuts
 */
import { DEFAULT_KEYBINDINGS, getKeybindingsFromDOM, buildKeyMap } from './lib/utils.js';
import { FilterPreviewEngine } from './lib/filter-preview-engine.js';
import { AudioPreviewGraph } from './lib/audio-preview-graph.js';
import { AudioToolsEngine } from './lib/audio-tools-engine.js';
import { SequencePlayback } from './lib/sequence-playback.js';
import { isSkipSegment } from './lib/sponsorblock.js';
import { whenVisible } from './lib/page-scope.js';
import { clampSeek, adjacentEntry, entryAtTime, tightestEntryAtTime } from './lib/playback-timeline.js';

const PLAYBACK_HIGHLIGHT_CLASSES = ['bg-white/10', 'border-l-2', 'border-white/40', 'pl-2'];

/** Highlight the range under playhead in a scrollable list, matching transcript cues. */
class RangeListHighlight {
  constructor(player, listEl, itemSelector) {
    this.player = player;
    this.listEl = listEl;
    this.itemSelector = itemSelector;
    this.items = [];
    this.activeEl = null;
    this.userScrolling = false;
    this.scrollTimeout = null;
    if (!listEl || !player?.video) return;
    const observer = new PageMutationObserver(() => this.discover());
    observer.observe(listEl, { childList: true, subtree: true });
    listEl.addEventListener('scroll', () => {
      this.userScrolling = true;
      clearTimeout(this.scrollTimeout);
      this.scrollTimeout = pageTimeout(() => {
        this.userScrolling = false;
      }, 3000);
    }, { passive: true });
    player.video.addEventListener('timeupdate', () => this.sync());
    this.discover();
  }

  discover() {
    if (!this.listEl) return;
    this.items = [...this.listEl.querySelectorAll(this.itemSelector)].map((el) => ({
      el,
      start: parseFloat(el.dataset.rangeStart),
      end: parseFloat(el.dataset.rangeEnd),
    })).filter((item) => Number.isFinite(item.start) && Number.isFinite(item.end) && item.end > item.start);
    this.activeEl = null;
    this.sync();
  }

  sync() {
    const t = this.player.video?.currentTime;
    if (!Number.isFinite(t) || !this.items.length) return;
    const nextEl = tightestEntryAtTime(this.items, t)?.el || null;
    if (nextEl === this.activeEl) return;
    this.activeEl?.classList.remove(...PLAYBACK_HIGHLIGHT_CLASSES);
    this.activeEl = nextEl;
    if (!nextEl) return;
    nextEl.classList.add(...PLAYBACK_HIGHLIGHT_CLASSES);
    if (this.userScrolling) return;
    const elTop = nextEl.offsetTop - this.listEl.offsetTop;
    const target = elTop - (this.listEl.clientHeight / 2) + (nextEl.offsetHeight / 2);
    this.listEl.scrollTo({ top: target, behavior: 'smooth' });
  }
}

class VideoPlayer {
  constructor(container) {
    this.container = container;
    this.video = container.querySelector('video');
    this.videoID = container.dataset.videoId || null;
    this.controlsContainer = null;
    this.progressBar = null;
    this.volumeSlider = null;
    this.playbackRateSelect = null;

    // Seek thumbnails (spritesheets + VTT)
    this.seek = {
      manifest: null,
      vttByLevel: new Map(),
      loadingVttByLevel: new Map(),
    };
    this.seekTooltip = null;
    this.seekTooltipThumb = null;
    this.seekTooltipTime = null;
    this.progressContainer = null;
    this._seekTooltipRAF = null;
    
    // State
    this.isFullscreen = false;
    this.isTheaterMode = false;
    this.userActive = true;
    this.controlsVisible = true;
    this.hideControlsTimeout = null;
    
    // Settings (restored from localStorage)
    this.settings = {
      volume: parseFloat(localStorage.getItem('videoPlayer.volume') || '1'),
      playbackRate: parseFloat(localStorage.getItem('videoPlayer.playbackRate') || '1'),
      muted: localStorage.getItem('videoPlayer.muted') === 'true'
    };
    
    // Sequence playback engine
    this._seq = null;
    
    // Position tracking
    this.positionSaveInterval = null;
    this.lastSavedPosition = 0;
    this.positionSaveThreshold = 2; // Save every 2 seconds of playback change
    
    // Quality switching
    this.qualities = []; // [{label, src, height}]
    this.qualitySelect = null;
    this._switchingQuality = false;

    // Only initialize if there's a video element
    if (this.video) {
      this.init();
    }
  }
  
  init() {
    if (!this.video) {
      console.warn('VideoPlayer: No video element found, skipping initialization');
      return;
    }

    this.buildControls();
    this.attachEventListeners();
    this.restoreSettings();
    if (this.container.closest('[data-watch-page]')) {
      this.theaterBtn?.classList.remove('hidden');
      try { this.setTheaterMode(localStorage.getItem('videoPlayer.theaterMode') === 'true'); } catch (_) {}
    }
    this.keyboardShortcuts = new KeyboardShortcutHandler(this);
    this.initMediaSession();
    this.initQualityPicker();

    if (this.videoID) {
      this.markerManager = new MarkerManager(this);
      this.clipManager = new ClipManager(this);
      this.transcriptManager = new TranscriptManager(this);
      void this.initSeekThumbnails();
      this.initPositionTracking();
      this.contextWindowManager = new ContextWindowManager(this.videoID, this);
      whenVisible(this.container, () => this.contextWindowManager.load());
    }
  }

  buildControls() {
    // Controls are now server-rendered by the VideoPlayerControls templ
    // component. We just bind to the existing DOM elements by class name.
    this.controlsContainer = this.container.querySelector('.video-controls');
    
    this.progressContainer = this.container.querySelector('.progress-container');
    this.progressBar = this.container.querySelector('.progress-bar');
    this.progressFill = this.container.querySelector('.progress-fill');
    this.seekSlider = this.container.querySelector('.seek-slider');
    this.progressBuffer = this.container.querySelector('.progress-buffer');
    this.timelineLane = this.container.querySelector('.timeline-detail-lane');
    this.timelineTitle = this.container.querySelector('.timeline-current-title');
    this.timelinePrevious = this.container.querySelector('.timeline-previous');
    this.timelineNext = this.container.querySelector('.timeline-next');
    this.timelineEntries = [];
    
    this.seekTooltip = this.container.querySelector('.seek-tooltip');
    this.seekTooltipThumb = this.container.querySelector('.seek-tooltip-thumb');
    this.seekTooltipTime = this.container.querySelector('.seek-tooltip-time');
    
    this.playBtn = this.container.querySelector('.play-btn');
    this.timeDisplay = this.container.querySelector('.time-display');
    this.volumeBtn = this.container.querySelector('.volume-btn');
    this.volumeSlider = this.container.querySelector('.volume-slider');
    this.playbackRateSelect = this.container.querySelector('.playback-rate-select');
    this.captionBtn = this.container.querySelector('.caption-btn');
    this.fullscreenBtn = this.container.querySelector('.fullscreen-btn');
    this.theaterBtn = this.container.querySelector('.theater-btn');
    
    // Add container classes
    this.container.classList.add('custom-video-player');
  }
  
  attachEventListeners() {
    // Play/pause
    this.playBtn.addEventListener('click', () => this.togglePlayPause());
    this.video.addEventListener('click', () => this.togglePlayPause());
    
    // A native slider handles touch, pointer capture, and accessible focus.
    // Preview while dragging; commit once on release rather than seeking each pixel.
    this.seekSlider.addEventListener('pointerdown', () => { this.seeking = true; this.showControls(); });
    this.seekSlider.addEventListener('input', () => {
      this.seeking = true;
      const time = Number(this.seekSlider.value);
      this.paintProgress(time, this.playbackDuration());
      this.updateTimelineDetails(time);
      const rect = this.container.querySelector('.progress-track').getBoundingClientRect();
      this.queueSeekTooltipUpdate({ clientX: rect.left + time / this.playbackDuration() * rect.width });
    });
    this.seekSlider.addEventListener('change', () => {
      this.seekToTime(Number(this.seekSlider.value));
      this.seeking = false;
      this.hideSeekTooltip();
    });
    pageListen(document, 'pointerup', () => {
      if (!this.seeking) return;
      this.seekToTime(Number(this.seekSlider.value));
      this.seeking = false;
      this.hideSeekTooltip();
    });
    const cancelScrub = () => { this.seeking = false; this.hideSeekTooltip(); this.updateProgress(); };
    this.seekSlider.addEventListener('pointercancel', cancelScrub);
    this.seekSlider.addEventListener('blur', cancelScrub);
    this.seekSlider.addEventListener('keydown', (event) => {
      let time = this._seq?.currentTime ?? this.video.currentTime;
      const delta = event.shiftKey ? 30 : 5;
      if (event.key === 'Home') time = 0;
      else if (event.key === 'End') time = this.playbackDuration();
      else if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') time -= delta;
      else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') time += delta;
      else if (event.key === 'Escape') { cancelScrub(); return; }
      else return;
      event.preventDefault();
      this.seekToTime(time);
    });
    this.progressBar.addEventListener('pointermove', (event) => this.queueSeekTooltipUpdate(event));
    this.progressBar.addEventListener('pointerleave', () => { if (!this.seeking) this.hideSeekTooltip(); });
    this.controlsContainer.addEventListener('focusin', () => this.showControls());
    for (const [button, direction] of [[this.timelinePrevious, -1], [this.timelineNext, 1]]) {
      button.addEventListener('click', () => {
        const entry = adjacentEntry(this.timelineEntries, this.video.currentTime, direction);
        if (entry) this.seekToTime(entry.start);
      });
    }
    
    // Volume
    this.volumeBtn.addEventListener('click', () => this.toggleMute());
    this.volumeSlider.addEventListener('input', (e) => {
      this.setVolume(e.target.value / 100);
    });
    
    // Playback rate
    this.playbackRateSelect.addEventListener('change', (e) => {
      this.setPlaybackRate(parseFloat(e.target.value));
    });
    
    // Captions
    this.captionBtn.addEventListener('click', () => this.toggleCaptions());
    
    // Fullscreen
    this.fullscreenBtn.addEventListener('click', () => this.toggleFullscreen());
    this.theaterBtn?.addEventListener('click', () => this.toggleTheaterMode());
    pageListen(document, 'fullscreenchange', () => this.handleFullscreenChange());
    
    // Video events
    this.video.addEventListener('play', () => this.updatePlayButton());
    this.video.addEventListener('pause', () => this.updatePlayButton());
    this.video.addEventListener('timeupdate', () => {
      this.updateProgress();
      // Check for auto-skip (SponsorBlock)
      if (this.markerManager) {
        this.markerManager.checkAutoSkip();
      }
    });
    this.video.addEventListener('loadedmetadata', () => {
      this.updateProgress();
      this.restoreSavedPosition();
    });
    this.video.addEventListener('progress', () => this.updateBuffered());
    this.video.addEventListener('volumechange', () => this.updateVolumeIcon());
    this.video.addEventListener('pause', () => this.saveCurrentPosition());
    this.video.addEventListener('seeked', () => this.saveCurrentPosition());
    
    // Mouse activity detection
    this.container.addEventListener('mousemove', () => this.showControls());
    this.container.addEventListener('mouseleave', () => this.hideControls());
  }

  initMediaSession() {
    if (!this.video || !('mediaSession' in navigator)) return;

    try {
      navigator.mediaSession.setActionHandler('play', () => this.video.play());
      navigator.mediaSession.setActionHandler('pause', () => this.video.pause());
      navigator.mediaSession.setActionHandler('seekbackward', (details) => {
        const skipTime = details?.seekOffset || 10;
        this.seekRelative(-skipTime);
      });
      navigator.mediaSession.setActionHandler('seekforward', (details) => {
        const skipTime = details?.seekOffset || 10;
        this.seekRelative(skipTime);
      });
      navigator.mediaSession.setActionHandler('previoustrack', () => this.seekRelative(-10));
      navigator.mediaSession.setActionHandler('nexttrack', () => this.seekRelative(10));
    } catch (_) {
      // Best-effort only.
    }
  }

  /**
   * Initialise the quality picker from data-qualities JSON attribute.
   * Parses available qualities and wires up the <select> created in buildControls.
   */
  initQualityPicker() {
    try {
      const raw = this.container.dataset.qualities;
      if (!raw) return;
      this.qualities = JSON.parse(raw); // [{label, src, height}]
    } catch (_) {
      return;
    }
    if (this.qualities.length === 0) return;

    // Build the <select> element (inserted during buildControls)
    this.qualitySelect = this.container.querySelector('.quality-select');
    if (!this.qualitySelect) return;

    // Determine current source – default entry
    const currentSrc = this.video.querySelector('source')?.getAttribute('src') || '';

    // Add "Original" option for the default source
    const origOpt = document.createElement('option');
    origOpt.value = currentSrc;
    origOpt.textContent = 'Original';
    origOpt.selected = true;
    this.qualitySelect.appendChild(origOpt);

    // Add each stream quality (sorted by height descending in the data already)
    for (const q of this.qualities) {
      const opt = document.createElement('option');
      opt.value = q.src;
      opt.textContent = q.label;
      this.qualitySelect.appendChild(opt);
    }

    // Show the select now that it has options
    this.qualitySelect.classList.remove('hidden');

    // Wire up change handler
    this.qualitySelect.addEventListener('change', () => {
      this._switchQuality(this.qualitySelect.value);
    });
  }

  /**
   * Switch video source while preserving playback position and state.
   */
  _switchQuality(newSrc) {
    if (this._switchingQuality) return;
    this._switchingQuality = true;

    const wasPlaying = !this.video.paused;
    const savedTime = this.video.currentTime;
    const savedRate = this.video.playbackRate;

    // Update the <source> element and reload
    const sourceEl = this.video.querySelector('source');
    if (sourceEl) {
      sourceEl.setAttribute('src', newSrc);
    }
    this.video.load();

    // Once enough data is loaded, restore position and play state
    const onCanPlay = () => {
      this.video.removeEventListener('canplay', onCanPlay);
      this.video.currentTime = savedTime;
      this.video.playbackRate = savedRate;
      if (wasPlaying) {
        this.video.play().catch(() => {});
      }
      this._switchingQuality = false;
    };
    this.video.addEventListener('canplay', onCanPlay);
  }

  async initSeekThumbnails() {
    if (!this.videoID) return;
    try {
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/seek/seek.json`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      const manifest = await res.json();
      if (!manifest || !Array.isArray(manifest.levels) || manifest.levels.length === 0) return;
      this.seek.manifest = manifest;
    } catch (_) {
      // Best-effort.
    }
  }

  queueSeekTooltipUpdate(evt) {
    if (!this.seekTooltip || !this.seekTooltipThumb || !this.seekTooltipTime) return;
    if (!this.video || !Number.isFinite(this.playbackDuration()) || this.playbackDuration() <= 0) return;
    if (!this.progressBar) return;

    // Throttle to rAF to avoid excessive DOM work.
    if (this._seekTooltipRAF) return;
    this._seekTooltipRAF = pageFrame(() => {
      this._seekTooltipRAF = null;
      this.updateSeekTooltip(evt);
    });
  }

  hideSeekTooltip() {
    if (!this.seekTooltip) return;
    if (this._seekTooltipRAF) { cancelAnimationFrame(this._seekTooltipRAF); this._seekTooltipRAF = null; }
    this.seekTooltip.classList.add('hidden');
    this._tooltipRequest = (this._tooltipRequest || 0) + 1;
  }

  chooseSeekLevel() {
    const levels = this.seek?.manifest?.levels;
    if (!Array.isArray(levels) || levels.length === 0) return null;

    // Default to medium if present.
    const medium = levels.find((l) => (l?.name || '') === 'medium');
    if (!this.seeking && medium) return medium;

    // While scrubbing, prefer the finest available level (smallest interval).
    let best = null;
    for (const lvl of levels) {
      const iv = Number(lvl?.interval_seconds);
      if (!isFinite(iv) || iv <= 0) continue;
      if (!best || iv < Number(best.interval_seconds)) best = lvl;
    }
    return best || medium || levels[0];
  }

  async ensureSeekVttLoaded(levelName) {
    if (!levelName || typeof levelName !== 'string') return null;
    if (this.seek.vttByLevel.has(levelName)) return this.seek.vttByLevel.get(levelName);
    if (this.seek.loadingVttByLevel.has(levelName)) return this.seek.loadingVttByLevel.get(levelName);

    const p = (async () => {
      try {
        const res = await pageFetch(
          `/api/videos/${encodeURIComponent(this.videoID)}/seek/levels/${encodeURIComponent(levelName)}/seek.vtt`,
          { headers: { 'Accept': 'text/vtt' } }
        );
        if (!res.ok) return null;
        const text = await res.text();
        const parsed = this.parseSeekVTT(text);
        if (parsed) this.seek.vttByLevel.set(levelName, parsed);
        return parsed;
      } catch (_) {
        return null;
      } finally {
        this.seek.loadingVttByLevel.delete(levelName);
      }
    })();

    this.seek.loadingVttByLevel.set(levelName, p);
    return p;
  }

  parseSeekVTT(text) {
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
      if (![hh, mm, ss, ms].every((v) => isFinite(v))) return null;
      return hh * 3600 + mm * 60 + ss + ms / 1000;
    };

    while (i < lines.length) {
      const line = lines[i].trim();
      i++;
      if (!line || line.startsWith('WEBVTT') || line.startsWith('NOTE')) continue;

      // Expect cue timing line.
      if (!line.includes('-->')) continue;
      const parts = line.split('-->').map((s) => s.trim());
      const start = parseTime(parts[0]);
      const end = parseTime(parts[1]);
      if (start == null || end == null) continue;

      // Next non-empty line should be the payload URL.
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
        h: Number(m[5])
      });
    }

    return cues.length > 0 ? cues : null;
  }

  async updateSeekTooltip(evt) {
    if (!this.seekTooltip || !this.seekTooltipThumb || !this.seekTooltipTime) return;
    if (!this.progressBar || !this.video) return;
    const duration = this.playbackDuration();
    if (!Number.isFinite(duration) || duration <= 0) return;
    const request = this._tooltipRequest = (this._tooltipRequest || 0) + 1;

    const rect = this.container.querySelector('.progress-track').getBoundingClientRect();
    const x = Math.max(0, Math.min(rect.width, evt.clientX - rect.left));
    const pct = rect.width > 0 ? x / rect.width : 0;
    const t = pct * duration;
    const tooltip = this.seekTooltip;
    this.seekTooltipTime.textContent = this.formatTime(t);
    const entry = entryAtTime(this.timelineEntries, t);
    tooltip.querySelector('.seek-tooltip-title').textContent = entry?.title || '';
    this.seekTooltipThumb.classList.add('hidden');
    tooltip.classList.remove('hidden');
    const position = () => {
      const width = tooltip.offsetWidth;
      const offset = rect.left - this.progressContainer.getBoundingClientRect().left;
      tooltip.style.left = `${offset + Math.max(width / 2, Math.min(rect.width - width / 2, x))}px`;
    };
    position();
    if (!this.seek.manifest || this._seq) return;

    const lvl = this.chooseSeekLevel();
    const levelName = (lvl?.name || '').toString();
    if (!levelName) {
      return;
    }

    const cues = await this.ensureSeekVttLoaded(levelName);
    if (request !== this._tooltipRequest) return;
    if (!cues || cues.length === 0) {
      return;
    }

    const interval = Number(lvl?.interval_seconds);
    let idx = isFinite(interval) && interval > 0 ? Math.floor(t / interval) : -1;
    if (!isFinite(idx) || idx < 0) idx = 0;
    if (idx >= cues.length) idx = cues.length - 1;
    const cue = cues[idx];
    if (!cue) {
      return;
    }

    const sheetURL = `/api/videos/${encodeURIComponent(this.videoID)}/seek/levels/${encodeURIComponent(levelName)}/${encodeURIComponent(cue.sheet)}`;
    const sheetW = Number(lvl?.cols) * Number(lvl?.thumb_width);
    const sheetH = Number(lvl?.rows) * Number(lvl?.thumb_height);

    const scale = Math.min(1, 192 / cue.w, rect.width * 0.6 / cue.w);
    this.seekTooltipThumb.style.width = `${cue.w * scale}px`;
    this.seekTooltipThumb.style.height = `${cue.h * scale}px`;
    this.seekTooltipThumb.style.backgroundImage = `url(${sheetURL})`;
    this.seekTooltipThumb.style.backgroundRepeat = 'no-repeat';
    if (isFinite(sheetW) && isFinite(sheetH) && sheetW > 0 && sheetH > 0) {
      this.seekTooltipThumb.style.backgroundSize = `${sheetW * scale}px ${sheetH * scale}px`;
    } else {
      this.seekTooltipThumb.style.backgroundSize = '';
    }
    this.seekTooltipThumb.style.backgroundPosition = `-${cue.x * scale}px -${cue.y * scale}px`;
    this.seekTooltipThumb.classList.remove('hidden');
    position();
  }
  
  restoreSettings() {
    this.video.volume = this.settings.volume;
    this.video.muted = this.settings.muted;
    this.video.playbackRate = this.settings.playbackRate;
    
    this.volumeSlider.value = this.settings.volume * 100;
    this.playbackRateSelect.value = this.settings.playbackRate;
    this.updateVolumeIcon();
    this.restoreCaptionSettings();
  }
  
  toggleCaptions() {
    const tracks = Array.from(this.video.textTracks);
    if (tracks.length === 0) return;
    
    // Find first subtitle/caption track
    const track = tracks.find(t => 
      t.kind === 'subtitles' || t.kind === 'captions'
    );
    
    if (!track) return;
    
    // Toggle visibility
    if (track.mode === 'showing') {
      track.mode = 'hidden';
      this.captionBtn.classList.add('text-white/60');
      this.captionBtn.classList.remove('text-white');
      localStorage.setItem('videoPlayer.captionsEnabled', 'false');
    } else {
      track.mode = 'showing';
      this.captionBtn.classList.remove('text-white/60');
      this.captionBtn.classList.add('text-white');
      localStorage.setItem('videoPlayer.captionsEnabled', 'true');
    }
  }
  
  restoreCaptionSettings() {
    const enabled = localStorage.getItem('videoPlayer.captionsEnabled');
    const tracks = Array.from(this.video.textTracks);
    
    if (tracks.length === 0) return;
    
    const track = tracks.find(t => 
      t.kind === 'subtitles' || t.kind === 'captions'
    );
    
    if (!track) return;
    
    if (enabled === 'true') {
      track.mode = 'showing';
      this.captionBtn.classList.remove('text-white/60');
      this.captionBtn.classList.add('text-white');
    } else if (enabled === 'false') {
      track.mode = 'hidden';
      this.captionBtn.classList.add('text-white/60');
    } else {
      // Default: respect the 'default' attribute from HTML
      if (track.mode !== 'showing') {
        this.captionBtn.classList.add('text-white/60');
      } else {
        this.captionBtn.classList.add('text-white');
      }
    }
  }
  
  togglePlayPause() {
    if (this._seq) {
      if (this._seq.paused) this._seq.play(); else this._seq.pause();
      return;
    }
    if (this.video.paused) {
      this.video.play();
    } else {
      this.video.pause();
    }
  }
  
  updatePlayButton() {
    const playIcon = this.playBtn.querySelector('.play-icon');
    const pauseIcon = this.playBtn.querySelector('.pause-icon');
    
    if (this.video.paused) {
      playIcon.classList.remove('hidden');
      pauseIcon.classList.add('hidden');
    } else {
      playIcon.classList.add('hidden');
      pauseIcon.classList.remove('hidden');
    }
  }
  
  seekToPosition(e) {
    const rect = this.container.querySelector('.progress-track').getBoundingClientRect();
    this.seekToTime(rect.width > 0 ? (e.clientX - rect.left) / rect.width * this.playbackDuration() : 0);
  }

  playbackDuration() { return this._seq?.duration ?? this.video.duration; }

  seekToTime(time) {
    const duration = this.playbackDuration();
    if (!Number.isFinite(duration) || duration <= 0) return;
    const target = clampSeek(time, duration);
    if (this._seq) this._seq.seekTo(target);
    else this.video.currentTime = target;
    this.paintProgress(target, duration);
    this.updateTimelineDetails(target);
  }

  paintProgress(time, duration) {
    const ready = Number.isFinite(duration) && duration > 0;
    const target = clampSeek(time, duration);
    this.progressFill.style.width = (ready ? target / duration * 100 : 0) + '%';
    this.seekSlider.disabled = !ready;
    this.seekSlider.max = ready ? String(duration) : '1';
    this.seekSlider.value = String(target);
    this.seekSlider.setAttribute('aria-valuetext', `${this.formatTime(target)} of ${this.formatTime(ready ? duration : 0)}`);
    this.timeDisplay.textContent = `${this.formatTime(target)} / ${this.formatTime(ready ? duration : 0)}`;
  }

  updateBuffered() {
    const duration = this.playbackDuration();
    const ranges = [];
    if (!this._seq && Number.isFinite(duration) && duration > 0) {
      for (let i = 0; i < this.video.buffered.length; i++) {
        const start = this.video.buffered.start(i) / duration * 100;
        const end = this.video.buffered.end(i) / duration * 100;
        ranges.push(`transparent ${start}%, rgba(255,255,255,.3) ${start}% ${end}%, transparent ${end}%`);
      }
    }
    this.progressBuffer.style.background = ranges.length ? `linear-gradient(to right, ${ranges.join(',')})` : 'none';
  }

  renderTimelineDetails() {
    const duration = this.playbackDuration();
    let entries = [];
    if (!this._seq) {
      const matching = [...(this.markerManager?.markers || [])].sort((a, b) => a.timestamp - b.timestamp);
      const unique = new Map();
      for (const marker of matching) {
        const kind = isSkipSegment(marker) ? 'skips' : marker.marker_type === 'chapter' || marker.action_type === 'chapter' ? 'chapters' : 'markers';
        const key = `${kind}:${marker.timestamp}:${marker.title}`;
        if (!unique.has(key) || (marker.duration || 0) > (unique.get(key).duration || 0)) unique.set(key, {...marker, kind});
      }
      const markers = [...unique.values()];
      entries = markers.map(m => ({start: m.timestamp, end: m.duration > 0 ? m.timestamp + m.duration : m.kind === 'chapters' ? (markers.find(next => next.kind === 'chapters' && next.timestamp > m.timestamp)?.timestamp ?? duration) : m.timestamp + .1, title: m.title || 'Marker', point: m.kind === 'markers' && !m.duration, skip: isSkipSegment(m), kind: m.kind}));
      entries.push(...(this.contextWindowManager?.windows || []).map(w => ({...w, kind: 'context'})));
      entries.push(...(this.clipManager?.clips || []).map(c => ({start: Number(c.StartTs ?? c.start_ts), end: Number(c.EndTs ?? c.end_ts), title: c.Title || c.title || 'Clip', kind: 'clips'})));
    }
    this.timelineEntries = entries.filter(e => Number.isFinite(e.start) && Number.isFinite(e.end) && e.start >= 0 && e.start < duration && e.end > e.start).sort((a, b) => a.start - b.start);
    this.timelineLane.replaceChildren();
    this.timelineLane.hidden = !this.timelineEntries.length;
    this.container.querySelector('.timeline-heading').hidden = !this.timelineEntries.length;
    for (const entry of this.timelineEntries) {
      const segment = document.createElement('button');
      segment.type = 'button';
      segment.className = 'timeline-segment';
      segment.tabIndex = -1; // Previous/next controls provide keyboard navigation.
      segment.dataset.kind = entry.kind;
      segment.dataset.skip = String(!!entry.skip);
      segment.dataset.point = String(!!entry.point);
      segment.style.left = `${entry.start / duration * 100}%`;
      segment.style.width = `${(Math.min(entry.end, duration) - entry.start) / duration * 100}%`;
      segment.title = `${this.formatTime(entry.start)} · ${entry.title}${entry.skip ? ' · Auto-skip segment' : ''}`;
      segment.setAttribute('aria-label', segment.title);
      segment.addEventListener('click', () => this.seekToTime(entry.start));
      this.timelineLane.appendChild(segment);
    }
    this.updateTimelineDetails(this.video.currentTime);
  }

  updateTimelineDetails(time) {
    const active = entryAtTime(this.timelineEntries, time);
    this.timelineTitle.textContent = active?.title || (this.timelineEntries.length ? `${this.timelineEntries.length} timeline entries` : '');
    this.timelineTitle.title = active?.title || '';
    this.timelinePrevious.disabled = !this.timelineEntries.length || time <= (this.timelineEntries[0]?.start ?? 0);
    this.timelineNext.disabled = !adjacentEntry(this.timelineEntries, time, 1);
    for (const [i, element] of [...this.timelineLane.children].entries()) element.classList.toggle('is-current', this.timelineEntries[i] === active);
  }
  
  updateProgress() {
    if (this.seeking) return;
    this.paintProgress(this._seq?.currentTime ?? this.video.currentTime, this.playbackDuration());
    this.updateTimelineDetails(this.video.currentTime);

    if (this.markerManager) {
      this.markerManager.renderIfNeeded();
    }

    if (this.clipManager) {
      this.clipManager.renderIfNeeded();
    }
  }
  
  formatTime(seconds) {
    if (isNaN(seconds)) return '0:00';
    
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    
    if (h > 0) {
      return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
    }
    return `${m}:${s.toString().padStart(2, '0')}`;
  }
  
  toggleMute() {
    this.video.muted = !this.video.muted;
    this.settings.muted = this.video.muted;
    localStorage.setItem('videoPlayer.muted', this.video.muted);
    this.updateVolumeIcon();
  }
  
  setVolume(volume) {
    this.video.volume = volume;
    this.settings.volume = volume;
    localStorage.setItem('videoPlayer.volume', volume);
    
    if (volume > 0 && this.video.muted) {
      this.video.muted = false;
      this.settings.muted = false;
      localStorage.setItem('videoPlayer.muted', 'false');
    }
    
    this.updateVolumeIcon();
  }
  
  updateVolumeIcon() {
    const highIcon = this.volumeBtn.querySelector('.volume-high-icon');
    const mutedIcon = this.volumeBtn.querySelector('.volume-muted-icon');
    
    if (this.video.muted || this.video.volume === 0) {
      highIcon.classList.add('hidden');
      mutedIcon.classList.remove('hidden');
    } else {
      highIcon.classList.remove('hidden');
      mutedIcon.classList.add('hidden');
    }
  }
  
  setPlaybackRate(rate) {
    this.video.playbackRate = rate;
    this.settings.playbackRate = rate;
    localStorage.setItem('videoPlayer.playbackRate', rate);
  }
  
  toggleFullscreen() {
    if (!this.isFullscreen) {
      if (this.container.requestFullscreen) {
        this.container.requestFullscreen();
      } else if (this.container.webkitRequestFullscreen) {
        this.container.webkitRequestFullscreen();
      }
    } else {
      if (document.exitFullscreen) {
        document.exitFullscreen();
      } else if (document.webkitExitFullscreen) {
        document.webkitExitFullscreen();
      }
    }
  }
  
  handleFullscreenChange() {
    this.isFullscreen = !!document.fullscreenElement;
    
    const enterIcon = this.fullscreenBtn.querySelector('.fullscreen-enter-icon');
    const exitIcon = this.fullscreenBtn.querySelector('.fullscreen-exit-icon');
    
    if (this.isFullscreen) {
      enterIcon.classList.add('hidden');
      exitIcon.classList.remove('hidden');
      this.container.classList.add('fullscreen');
    } else {
      enterIcon.classList.remove('hidden');
      exitIcon.classList.add('hidden');
      this.container.classList.remove('fullscreen');
    }
  }
  
  toggleTheaterMode() {
    this.setTheaterMode(!this.isTheaterMode);
  }

  setTheaterMode(enabled) {
    const page = this.container.closest('[data-watch-page]');
    if (!page) return;
    this.isTheaterMode = enabled;
    page.classList.toggle('is-theater', enabled);
    this.container.classList.toggle('theater-mode', this.isTheaterMode);
    this.theaterBtn?.setAttribute('aria-pressed', String(enabled));
    this.theaterBtn?.setAttribute('aria-label', enabled ? 'Exit theater mode' : 'Theater mode');
    if (this.theaterBtn) this.theaterBtn.title = enabled ? 'Exit theater mode (T)' : 'Theater mode (T)';
    try { localStorage.setItem('videoPlayer.theaterMode', String(enabled)); } catch (_) {}
    
    // Dispatch event for parent page to adjust layout
    this.container.dispatchEvent(new CustomEvent('theatermodechange', {
      detail: { enabled: this.isTheaterMode }
    }));
  }
  
  togglePictureInPicture() {
    if (document.pictureInPictureElement) {
      document.exitPictureInPicture();
    } else if (document.pictureInPictureEnabled) {
      this.video.requestPictureInPicture();
    }
  }
  
  showControls() {
    this.controlsVisible = true;
    this.controlsContainer.classList.remove('hidden');
    
    clearTimeout(this.hideControlsTimeout);
    
    // Auto-hide after 3 seconds if playing
    if (!this.video.paused) {
      this.hideControlsTimeout = pageTimeout(() => {
        this.hideControls();
      }, 3000);
    }
  }
  
  hideControls() {
    if (!this.video.paused && !this.seeking && !this.controlsContainer.querySelector(':focus-visible')) {
      this.controlsVisible = false;
      this.controlsContainer.classList.add('hidden');
    }
  }
  
  seekRelative(seconds) {
    if (this._seq) {
      this._seq.seekTo(this._seq.currentTime + seconds);
      return;
    }
    this.video.currentTime = Math.max(0, Math.min(
      this.video.duration,
      this.video.currentTime + seconds
    ));
  }
  
  changeVolume(delta) {
    const newVolume = Math.max(0, Math.min(1, this.video.volume + delta));
    this.setVolume(newVolume);
    this.volumeSlider.value = newVolume * 100;
  }
  
  changePlaybackRate(delta) {
    const rates = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];
    const currentIndex = rates.indexOf(this.video.playbackRate);
    const newIndex = Math.max(0, Math.min(rates.length - 1, currentIndex + Math.sign(delta)));
    
    this.setPlaybackRate(rates[newIndex]);
    this.playbackRateSelect.value = rates[newIndex];
  }
  
  seekToPercent(percent) {
    this.video.currentTime = (percent / 100) * this.video.duration;
  }

  // ── Sequence Playback ──────────────────────────────────────────────────
  
  /**
   * Load a sequence of clips for back-to-back playback with transitions.
   * Extends the player to act as a multi-clip sequence player.
   *
   * @param {Array<Object>} segments
   * @param {string} segments[].src      - Video source URL
   * @param {number} segments[].startTime - Start time in source video (seconds)
   * @param {number} segments[].endTime   - End time in source video (seconds)
   * @param {string} [segments[].label]   - Display label
   * @param {Object|null} [segments[].transition] - Transition into this segment
   * @param {string} segments[].transition.type     - Transition type name
   * @param {number} segments[].transition.duration  - Duration in seconds
   * @param {Object} [segments[].transition.behavior]
   * @param {string} segments[].transition.behavior.outgoing - 'play'|'freeze'|'play-past'
   * @param {string} segments[].transition.behavior.audio    - 'crossfade'|'cut'|'fade-out-in'
   */
  loadSequence(segments) {
    this.clearSequence();
    if (!segments || segments.length === 0) return;
    
    // The video element's parent must be the positioned container
    var seqContainer = this.video.parentElement;
    this._seq = new SequencePlayback(seqContainer);
    
    // Wire events to player UI
    var self = this;
    this._seq.on('timeupdate', function(vt, dur) {
      if (!self.progressFill) return;
      if (!self.seeking) self.paintProgress(vt, dur);
    });
    this._seq.on('play', function() { self.updatePlayButton(); });
    this._seq.on('pause', function() { self.updatePlayButton(); });
    this._seq.on('ended', function() { self.updatePlayButton(); });
    this._seq.on('segmentchange', function(idx) {
      self.container.dispatchEvent(new CustomEvent('sequencesegmentchange', { detail: { index: idx } }));
    });
    
    this._seq.load(segments);
    this.renderTimelineDetails();
    this.updateBuffered();
    this._seq.setVolume(this.video.volume);
    this._seq.setMuted(this.video.muted);
  }
  
  clearSequence() {
    if (!this._seq) return;
    this._seq.destroy();
    this._seq = null;
    this.renderTimelineDetails();
    this.updateProgress();
  }
  
  /** @returns {boolean} Whether the player is in sequence playback mode. */
  isSequenceMode() { return !!this._seq; }
  
  /** @returns {SequencePlayback|null} The active sequence engine, if any. */
  getSequence() { return this._seq; }
  
  // Position tracking methods
  initPositionTracking() {
    // Start interval to save position periodically (every 5 seconds during playback)
    this.positionSaveInterval = pageInterval(() => {
      if (!this.video.paused && !this.video.ended) {
        this.saveCurrentPosition();
      }
    }, 5000);

    // Save position when page unloads
    pageListen(window, 'beforeunload', () => {
      this.saveCurrentPosition();
    });
  }

  restoreSavedPosition() {
    const urlT = parseFloat(new URLSearchParams(window.location.search).get('t') || '');
    if (Number.isFinite(urlT) && urlT >= 0 && Number.isFinite(this.video.duration) && urlT < this.video.duration) {
      this.video.currentTime = urlT;
      return;
    }

    const savedPosition = parseFloat(this.container.dataset.savedPosition || '0');
    
    // Only restore if we have a valid saved position and it's not at the very beginning or end
    if (savedPosition > 1 && savedPosition < this.video.duration - 5) {
      this.video.currentTime = savedPosition;
      console.log(`Restored playback position: ${savedPosition.toFixed(2)}s`);
    }
  }

  saveCurrentPosition() {
    if (!this.videoID || !this.video) return;

    const currentPos = this.video.currentTime;
    
    // Only save if position has changed significantly (threshold)
    if (Math.abs(currentPos - this.lastSavedPosition) < this.positionSaveThreshold) {
      return;
    }

    this.lastSavedPosition = currentPos;

    // Send position to server (fire and forget, no need to wait for response)
    pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/position`, {
      keepalive: true,
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        position: currentPos
      })
    }).catch(err => {
      // Silent fail - position tracking is not critical
      console.debug('Failed to save playback position:', err);
    });
  }
}

class ContextWindowManager {
  constructor(videoID, player = null) {
    this.videoID = videoID;
    this.player = player;
    this.windows = [];
    this.activeID = null;
    this.player?.video?.addEventListener('loadedmetadata', () => this.renderRail());
    const contextList = document.querySelector('[data-context-list]');
    if (player && contextList) {
      this.highlight = new RangeListHighlight(player, contextList, '[data-range-start]');
    }
  }

  async load() {
    if (this.loading) return;
    this.loading = true;
    try {
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.videoID)}/context-windows`, { headers: { Accept: 'application/json' } });
      if (!res.ok) return;
      this.windows = ((await res.json()) || []).map(raw => ({
        id: String(raw.ID ?? raw.id ?? ''),
        start: Number(raw.StartTs ?? raw.start_ts ?? 0),
        end: Number(raw.EndTs ?? raw.end_ts ?? 0),
        title: String(raw.Title ?? raw.title ?? 'Context'),
        summary: String(raw.Summary ?? raw.summary ?? ''),
        topics: asStringArray(raw.Topics ?? raw.topics),
        entities: asStringArray(raw.Entities ?? raw.entities),
        stale: Boolean(raw.EvidenceStale ?? raw.evidence_stale ?? false),
        kind: String(raw.Kind ?? raw.kind ?? 'window'),
        parentId: String(raw.ParentID ?? raw.parent_id ?? ''),
        hook: String(raw.Hook ?? raw.hook ?? ''),
      })).filter(w => w.id && w.end > w.start);

      this.loaded = true;
      this.renderRail();
    } catch (_) {
      // Context Windows are additive; playback remains available on failure.
    } finally {
      this.loading = false;
    }
  }

  renderRail() { this.player?.renderTimelineDetails(); }

  seek(w) { if (this.player?.video) { this.player.video.currentTime = w.start; void this.player.video.play(); } else { window.RewindNavigation.navigate(`/videos/${encodeURIComponent(this.videoID)}?t=${w.start}`); } }
  updateActive() { this.player?.updateTimelineDetails(this.player.video.currentTime); }

}

function formatContextTime(seconds) {
  const s = Math.max(0, Math.floor(Number(seconds) || 0));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  return h ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`;
}

function asStringArray(value) {
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item ?? '').trim()).filter(Boolean);
}

function contextWindowTooltip(w) {
  const lines = [w?.title || 'Context'];
  if (Array.isArray(w?.topics) && w.topics.length) lines.push(`Topics: ${w.topics.join(', ')}`);
  if (Array.isArray(w?.entities) && w.entities.length) lines.push(`Entities: ${w.entities.join(', ')}`);
  return lines.join('\n');
}

class MarkerManager {
  constructor(player) {
    this.player = player;
    this.markers = [];
    this.skipSegments = [];
    this.lastSkipCheck = 0;
    this.renderedForDuration = null;
    this.loading = false;
    this._initialLoadDone = false;

	  this.panel = this.findPanel();
	  this.listEl = this.panel?.querySelector('[data-markers-list]') || null;

    this.load();
  }

	findPanel() {
		if (!this.player.videoID) return null;
		return document.querySelector(`[data-video-panel][data-video-id="${CSS.escape(this.player.videoID)}"]`);
	}

  async load() {
    if (!this.player.videoID) return;
    this.loading = true;
    try {
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/markers`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      this.markers = await res.json();
      
      this.skipSegments = this.markers.filter(isSkipSegment);
      
      this.renderedForDuration = null;
      this.renderIfNeeded();
      // On initial load, data-init SSE on [data-markers-list] handles the list render.
      // Only trigger a manual refresh on subsequent loads (after marker create/delete).
      if (this._initialLoadDone) {
        this.renderList();
      } else {
        this._initialLoadDone = true;
      }
    } catch (_) {
      // Best-effort; no UI error surface yet.
    } finally {
      this.loading = false;
    }
  }

  formatTime(seconds) {
    if (!isFinite(seconds) || seconds < 0) return '0:00';
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    if (h > 0) {
      return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
    }
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  renderList() {
    // Marker list is now server-rendered via SSE (templ MarkerList component).
    // The initial render is triggered by data-init on [data-markers-list].
    // After mutations, we click the hidden refresh button.
    this._triggerRefresh();
  }

  _triggerRefresh() {
    const btn = document.querySelector('[data-markers-refresh]');
    if (btn) btn.click();
  }

  async deleteMarker(id) {
    if (!id || typeof id !== 'string') return;
    if (id.startsWith('sb:')) return;
    try {
      const res = await pageFetch(`/api/markers/${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (!res.ok) return;
      await this.load();
    } catch (_) {
      // Best-effort.
    }
  }
  
  checkAutoSkip() {
    const currentTime = this.player.video.currentTime;
    
    // Avoid rapid re-checks (debounce to 0.5s)
    if (Math.abs(currentTime - this.lastSkipCheck) < 0.5) {
      return;
    }
    this.lastSkipCheck = currentTime;
    
    // Check if we're inside a skip segment
    for (const seg of this.skipSegments) {
      const start = seg.timestamp;
      const end = start + seg.duration;
      
      // If we're in the segment and haven't passed the end
      if (currentTime >= start && currentTime < end) {
        // Check user preference
        const autoSkip = localStorage.getItem('videoPlayer.autoSkipSponsors');
        if (autoSkip !== 'false') { // Default enabled
          this.skipSegment(seg, end);
        } else {
          this.showSkipButton(seg, end);
        }
        break; // Only handle one segment at a time
      }
    }
  }
  
  skipSegment(segment, endTime) {
    console.log(`[SponsorBlock] Auto-skipping: ${segment.title}`);
    this.player.video.currentTime = endTime + 0.1; // Add small buffer
    this.showSkipNotification(segment);
  }
  
  showSkipNotification(segment) {
    // Use the pre-rendered toast element from the VideoPlayerControls templ
    const toast = this.player.container.querySelector('[data-skip-notification]');
    if (!toast) return;
    const textEl = toast.querySelector('[data-skip-notification-text]');
    if (textEl) textEl.textContent = `Skipped: ${segment.title}`;
    toast.classList.remove('hidden', 'fade-out');

    // Auto-hide after 2 seconds
    clearTimeout(this._skipNotifTimeout);
    this._skipNotifTimeout = pageTimeout(() => {
      toast.classList.add('fade-out');
      pageTimeout(() => toast.classList.add('hidden'), 300);
    }, 2000);
  }
  
  showSkipButton(segment, endTime) {
    // Show skip button overlay (if auto-skip disabled)
    // For now, just log - can be enhanced later
    console.log(`[SponsorBlock] Segment available to skip: ${segment.title}`);
  }

  renderIfNeeded() {
    const duration = this.player.video.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;
    if (this.renderedForDuration === duration) return;
    this.renderedForDuration = duration;
    this.render();
  }

  clearTicks() { this.player.timelineLane?.replaceChildren(); }

  render() { this.player.renderTimelineDetails(); }

  async createMarkerAtCurrentTime() {
    if (!this.player.videoID) return;

    const ts = this.player.video.currentTime;
    if (!isFinite(ts) || ts < 0) return;

    try {
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/markers`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Accept': 'application/json'
        },
        body: JSON.stringify({
          timestamp: ts,
          title: '',
          description: '',
          color: '',
          marker_type: 'point'
        })
      });
      if (!res.ok) return;

      await this.load();
    } catch (_) {
      // Best-effort.
    }
  }
}

class TranscriptManager {
  constructor(player) {
    this.player = player;
    this.panel = this.findPanel();
    this.listEl = this.panel?.querySelector('[data-transcript-list]') || null;
    this.searchEl = this.panel?.querySelector('[data-transcript-search]') || null;
    this.cueElements = [];
    this.activeCueIndex = -1;
    this.userScrolling = false;
    this.scrollTimeout = null;

    if (this.panel && this.listEl) {
      this.attach();
      // Cues are loaded via data-init SSE on [data-transcript-list].
      // Watch for SSE patches to discover rendered cue rows.
      const observer = new PageMutationObserver(() => this._discoverCues());
      observer.observe(this.listEl, { childList: true, subtree: true });
    }
  }

  findPanel() {
    if (!this.player.videoID) return null;
    return document.querySelector(`[data-transcript-panel][data-video-id="${CSS.escape(this.player.videoID)}"]`);
  }

  attach() {
    if (this.searchEl) {
      this.searchEl.addEventListener('input', () => this.applyFilter());
    }

    // Listen for video time updates to auto-scroll
    if (this.player.video) {
      this.player.video.addEventListener('timeupdate', () => this.onTimeUpdate());
    }

    // Detect user scrolling to pause auto-scroll temporarily
    if (this.listEl) {
      this.listEl.addEventListener('scroll', () => {
        this.userScrolling = true;
        clearTimeout(this.scrollTimeout);
        this.scrollTimeout = pageTimeout(() => {
          this.userScrolling = false;
        }, 3000); // Resume auto-scroll after 3 seconds of no manual scrolling
      }, { passive: true });
    }
  }

  /** Re-read cue elements from the server-rendered DOM after SSE patch. */
  _discoverCues() {
    if (!this.listEl) return;
    this.cueElements = Array.from(this.listEl.querySelectorAll('[data-cue-start]'));
    this.activeCueIndex = -1;
    // Re-apply search filter if active
    if (this.searchEl?.value?.trim()) {
      this.applyFilter();
    }
    this.onTimeUpdate();
  }

  onTimeUpdate() {
    if (!this.player.video || !this.cueElements.length) return;
    const currentTime = this.player.video.currentTime;

    // Find the current visible cue (last cue whose start time <= currentTime)
    let newIndex = -1;
    const visible = this.cueElements.filter((el) => !el.classList.contains('hidden'));
    for (let i = 0; i < visible.length; i++) {
      if (parseFloat(visible[i].dataset.cueStart) <= currentTime) {
        newIndex = i;
      } else {
        break;
      }
    }

    // Map back to cueElements index for highlight
    const targetEl = newIndex >= 0 ? visible[newIndex] : null;
    const absIndex = targetEl ? this.cueElements.indexOf(targetEl) : -1;

    if (absIndex !== this.activeCueIndex) {
      this.setActiveCue(absIndex);
    }
  }

  setActiveCue(index) {
    // Remove highlight from previous active cue
    if (this.activeCueIndex >= 0 && this.cueElements[this.activeCueIndex]) {
      this.cueElements[this.activeCueIndex].classList.remove('bg-white/10', 'border-l-2', 'border-white/40', 'pl-2');
    }

    this.activeCueIndex = index;

    // Highlight new active cue
    if (index >= 0 && this.cueElements[index]) {
      const el = this.cueElements[index];
      el.classList.add('bg-white/10', 'border-l-2', 'border-white/40', 'pl-2');

      // Auto-scroll within the list container only (not the page)
      if (!this.userScrolling && this.listEl) {
        const containerHeight = this.listEl.clientHeight;
        const elTop = el.offsetTop - this.listEl.offsetTop;
        const elHeight = el.offsetHeight;
        const targetScroll = elTop - (containerHeight / 2) + (elHeight / 2);
        this.listEl.scrollTo({ top: targetScroll, behavior: 'smooth' });
      }
    }
  }

  /** Filter server-rendered cue rows by search query (toggle hidden class). */
  applyFilter() {
    const q = (this.searchEl?.value || '').trim().toLowerCase();
    this.activeCueIndex = -1;
    this.cueElements.forEach((el) => {
      const text = (el.dataset.cueText || '').toLowerCase();
      el.classList.toggle('hidden', q !== '' && !text.includes(q));
    });
    this.onTimeUpdate();
  }
}

class ClipManager {
  constructor(player) {
    this.player = player;
    this.clips = [];
    this.inPoint = null;
    this.outPoint = null;

    this.renderedForDuration = null;

    this.panel = this.findPanel();
    this.listEl = this.panel?.querySelector('[data-clips-list]') || null;
    this.rangeEl = this.panel?.querySelector('[data-clip-range]') || null;
    this.btnSetIn = this.panel?.querySelector('[data-clip-set-in]') || null;
    this.btnSetOut = this.panel?.querySelector('[data-clip-set-out]') || null;
    this.btnCreate = this.panel?.querySelector('[data-clip-create]') || null;

    this.attachPanelListeners();
    if (this.listEl) {
      this.highlight = new RangeListHighlight(player, this.listEl, '[data-clip-row][data-range-start]');
    }

    // Timeline imperative API for backend SSE control
    this.timeline = {
      addClip: (clip) => this.timelineAddClip(clip),
      removeClip: (clipId) => this.timelineRemoveClip(clipId),
      updateClip: (clipId, updates) => this.timelineUpdateClip(clipId, updates),
      clear: () => this.timelineClear()
    };
    
    // Load clips data for timeline overlay
    this.loadClipsForTimeline();
  }

  findPanel() {
    if (!this.player.videoID) return null;
    return document.querySelector(`[data-video-panel][data-video-id="${CSS.escape(this.player.videoID)}"]`);
  }

  attachPanelListeners() {
    if (this.btnSetIn) {
      this.btnSetIn.addEventListener('click', () => this.setInPoint());
    }
    if (this.btnSetOut) {
      this.btnSetOut.addEventListener('click', () => this.setOutPoint());
    }
    if (this.btnCreate) {
      this.btnCreate.addEventListener('click', () => this.createClipFromRange());
    }
    this.renderRange();
  }

  renderRange() {
    if (!this.rangeEl) return;
    const fmt = (v) => (typeof v === 'number' && isFinite(v)) ? v.toFixed(2) : '--';
    this.rangeEl.textContent = `In: ${fmt(this.inPoint)}  Out: ${fmt(this.outPoint)}`;
  }

  setInPoint() {
    const t = this.player.video.currentTime;
    if (!isFinite(t) || t < 0) return;
    this.inPoint = t;
    this.renderRange();
  }

  setOutPoint() {
    const t = this.player.video.currentTime;
    if (!isFinite(t) || t < 0) return;
    this.outPoint = t;
    this.renderRange();
  }

  async loadClipsForTimeline() {
    if (!this.player.videoID) return;
    try {
      const res = await pageFetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/clips`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      this.clips = await res.json();
      this.renderTimeline();
    } catch (_) {
      // Best-effort.
    }
  }

  // Timeline imperative API methods (called by backend SSE)
  timelineAddClip(clip) {
    // clip: {id, startTime, endTime, color, title}
    const existing = this.clips.find(c => c.ID === clip.id || c.id === clip.id);
    if (existing) {
      // Update existing
      Object.assign(existing, {
        ID: clip.id,
        StartTs: clip.startTime,
        EndTs: clip.endTime,
        Color: clip.color,
        Title: clip.title
      });
    } else {
      // Add new
      this.clips.push({
        ID: clip.id,
        StartTs: clip.startTime,
        EndTs: clip.endTime,
        Color: clip.color,
        Title: clip.title
      });
    }
    this.renderTimeline();
  }

  timelineRemoveClip(clipId) {
    this.clips = this.clips.filter(c => 
      (c.ID !== clipId && c.id !== clipId)
    );
    this.renderTimeline();
  }

  timelineUpdateClip(clipId, updates) {
    const clip = this.clips.find(c => c.ID === clipId || c.id === clipId);
    if (clip) {
      if (updates.startTime !== undefined) clip.StartTs = updates.startTime;
      if (updates.endTime !== undefined) clip.EndTs = updates.endTime;
      if (updates.color !== undefined) clip.Color = updates.color;
      if (updates.title !== undefined) clip.Title = updates.title;
      this.renderTimeline();
    }
  }

  timelineClear() {
    this.clips = [];
    this.clearTimeline();
  }

  clearTimeline() {
    this.player.renderTimelineDetails();
  }

  renderIfNeeded() {
    const duration = this.player.video.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;
    if (this.renderedForDuration === duration) return;
    this.renderedForDuration = duration;
    this.renderTimeline();
  }

  renderTimeline() { this.player.renderTimelineDetails(); }

  // Clips are now server-rendered via components.ClipListContainer
  // This class only handles timeline overlay
  // Exports are now handled by Datastar + encoder service (see handlers_api_clip_exports.go)

  // DEPRECATED: exportClip method removed - exports are now handled by Datastar actions
  // The encoder service processes exports asynchronously and UI is updated via SSE patching

  async createClipFromRange() {
    if (!this.player.videoID) return;
    if (typeof this.inPoint !== 'number' || typeof this.outPoint !== 'number') return;

    const start = Math.min(this.inPoint, this.outPoint);
    const end = Math.max(this.inPoint, this.outPoint);
    if (!isFinite(start) || !isFinite(end) || end <= start) return;

    const startInput = this.panel?.querySelector('[data-clip-create-start]');
    const endInput = this.panel?.querySelector('[data-clip-create-end]');
    const submitBtn = this.panel?.querySelector('[data-clip-create-submit]');

    if (!startInput || !endInput || !submitBtn) return;

    startInput.value = start;
    endInput.value = end;
    startInput.dispatchEvent(new Event('input', { bubbles: true }));
    endInput.dispatchEvent(new Event('input', { bubbles: true }));
    submitBtn.click();
  }

  async deleteClip(id) {
    // Delete is now handled by DataStar @delete() action in template
    // This legacy method kept for backwards compatibility but unused
    if (!id || typeof id !== 'string') return;
    try {
      const res = await pageFetch(`/api/clips/${encodeURIComponent(id)}`, { method: 'DELETE' });
      // Backend returns SSE that updates DOM + timeline automatically via DataStar
    } catch (_) {
      // Best-effort.
    }
  }
}


/**
 * Keyboard Shortcut Handler with YouTube parity
 */
class KeyboardShortcutHandler {
  constructor(player) {
    this.player = player;
    this.enabled = true;
    this.keybindings = { ...DEFAULT_KEYBINDINGS, ...getKeybindingsFromDOM() };
    this.keyMap = buildKeyMap(this.keybindings);
    this.attachListeners();
  }
  
  attachListeners() {
    pageListen(document, 'keydown', (e) => {
      if (!this.enabled) return;
      
      // Don't trigger if user is typing in an input
      if (
        e.target?.isContentEditable ||
          e.target?.tagName === 'INPUT' ||
          e.target?.tagName === 'BUTTON' ||
          e.target?.tagName === 'A' ||
        e.target?.tagName === 'TEXTAREA' ||
        e.target?.tagName === 'SELECT'
      ) {
        return;
      }
      
      this.handleKeyPress(e);
    });
  }
  
  handleKeyPress(e) {
    const key = e.key;
    const lowerKey = key.toLowerCase();
    const hasModifier = e.ctrlKey || e.metaKey || e.altKey;

    // Media keys
    if (key === 'MediaPlayPause') {
      e.preventDefault();
      this.player.togglePlayPause();
      return;
    }
    if (key === 'MediaTrackPrevious') {
      e.preventDefault();
      this.player.seekRelative(-10);
      return;
    }
    if (key === 'MediaTrackNext') {
      e.preventDefault();
      this.player.seekRelative(10);
      return;
    }

    // User-configurable keybindings (no modifiers)
    if (!hasModifier) {
      const action = this.keyMap[key];
      if (action) {
        e.preventDefault();
        this.executeKeybindingAction(action);
        return;
      }
    }

    // Playback controls
    if ((lowerKey === 'k' || lowerKey === ' ') && !hasModifier) {
      e.preventDefault();
      this.player.togglePlayPause();
      return;
    }
    if (lowerKey === 'arrowleft' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.seekRelative(-5);
      return;
    }
    if (lowerKey === 'arrowright' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.seekRelative(5);
      return;
    }

    // Frame navigation (when paused)
    if (lowerKey === ',' && this.player.video.paused && !hasModifier) {
      e.preventDefault();
      this.previousFrame();
      return;
    }
    if (lowerKey === '.' && this.player.video.paused && !hasModifier) {
      e.preventDefault();
      this.nextFrame();
      return;
    }

    // Playback rate
    if (lowerKey === '<' || (lowerKey === ',' && e.shiftKey)) {
      e.preventDefault();
      this.player.changePlaybackRate(-1);
      return;
    }
    if (lowerKey === '>' || (lowerKey === '.' && e.shiftKey)) {
      e.preventDefault();
      this.player.changePlaybackRate(1);
      return;
    }

    // Percentage seeking (0-9)
    if (/^[0-9]$/.test(lowerKey) && !hasModifier) {
      e.preventDefault();
      this.player.seekToPercent(parseInt(lowerKey) * 10);
      return;
    }

    // View modes
    if (lowerKey === 'f' && !hasModifier) {
      e.preventDefault();
      this.player.toggleFullscreen();
      return;
    }
    if (lowerKey === 't' && !hasModifier) {
      e.preventDefault();
      this.player.toggleTheaterMode();
      return;
    }
    if (lowerKey === 'i' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.togglePictureInPicture();
      return;
    }
    if (lowerKey === 'escape') {
      if (this.player.isFullscreen) {
        this.player.toggleFullscreen();
      }
      return;
    }

    // Clips (Shift variants - check before non-shift)
    if (lowerKey === 'i' && e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.clipManager?.setInPoint();
      return;
    }
    if (lowerKey === 'o' && e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.clipManager?.setOutPoint();
      return;
    }
    if (lowerKey === 'c' && e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.clipManager?.createClipFromRange();
      return;
    }
    if (lowerKey === 'm' && e.shiftKey && !hasModifier) {
      e.preventDefault();
      this.player.markerManager?.createMarkerAtCurrentTime();
      return;
    }

    // Audio
    if (lowerKey === 'm' && !hasModifier) {
      e.preventDefault();
      this.player.toggleMute();
      return;
    }
    if (lowerKey === 'c' && !hasModifier) {
      e.preventDefault();
      this.player.toggleCaptions();
      return;
    }

    if (lowerKey === 'arrowup' && !hasModifier) {
      e.preventDefault();
      this.player.changeVolume(0.05);
      return;
    }
    if (lowerKey === 'arrowdown' && !hasModifier) {
      e.preventDefault();
      this.player.changeVolume(-0.05);
    }
  }

  executeKeybindingAction(action) {
    switch (action) {
      case 'set_in_point':
        this.player.clipManager?.setInPoint();
        break;
      case 'set_out_point':
        this.player.clipManager?.setOutPoint();
        break;
      case 'create_clip':
        this.player.clipManager?.createClipFromRange();
        break;
      case 'play_pause':
        this.player.togglePlayPause();
        break;
      case 'seek_back':
        this.player.seekRelative(-10);
        break;
      case 'seek_forward':
        this.player.seekRelative(10);
        break;
      case 'prev_frame':
        this.previousFrame();
        break;
      case 'next_frame':
        this.nextFrame();
        break;
      case 'create_marker':
        this.player.markerManager?.createMarkerAtCurrentTime();
        break;
      default:
        break;
    }
  }
  
  previousFrame() {
    if (!this.player.video.paused) return;
    
    // Estimate frame duration (assuming 30fps if unknown)
    const fps = 30;
    const frameDuration = 1 / fps;
    
    this.player.video.currentTime = Math.max(
      0,
      this.player.video.currentTime - frameDuration
    );
  }
  
  nextFrame() {
    if (!this.player.video.paused) return;
    
    const fps = 30;
    const frameDuration = 1 / fps;
    
    this.player.video.currentTime = Math.min(
      this.player.video.duration,
      this.player.video.currentTime + frameDuration
    );
  }
}

// Global function for DataStar clip row buttons to call
window.seekToTime = function(seconds) {
  const video = document.getElementById('videoPlayer');
  if (video && isFinite(seconds) && seconds >= 0) {
    video.currentTime = seconds;
  }
};

// Initialize both direct MPA loads and scripts mounted by DataStar navigation.
function initVideoPlayers() {
  pageListen(document, 'click', event => {
    const button = event.target.closest('[data-context-seek]');
    if (!button) return;
    const video = document.querySelector('[data-video-player] video');
    if (video) { video.currentTime = Number(button.dataset.contextSeek); }
  });

  const playerContainers = document.querySelectorAll('[data-video-player]');
  playerContainers.forEach(container => {
    if (container.dataset.playerInitialized) return;
    container.dataset.playerInitialized = 'true';
    const player = new VideoPlayer(container);
    onPageCleanup(() => {
      player.saveCurrentPosition();
      player.video.pause();
      player._seq?.destroy();
      clearInterval(player.positionSaveInterval);
      if (navigator.mediaSession) navigator.mediaSession.metadata = null;
    });
  });
  document.querySelectorAll('[data-context-window-list][data-video-id]').forEach(list => {
    if (!document.querySelector(`[data-video-player][data-video-id="${CSS.escape(list.dataset.videoId)}"]`)) {
      void new ContextWindowManager(list.dataset.videoId).load();
    }
  });
}
if (document.readyState === 'loading') pageListen(document, 'DOMContentLoaded', initVideoPlayers);
else initVideoPlayers();
