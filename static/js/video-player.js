/**
 * Custom Video Player with YouTube-like controls and keyboard shortcuts
 */
import { DEFAULT_KEYBINDINGS, getKeybindingsFromDOM, buildKeyMap } from './lib/utils.js';
import { FilterPreviewEngine } from './lib/filter-preview-engine.js';
import { AudioPreviewGraph } from './lib/audio-preview-graph.js';
import { AudioToolsEngine } from './lib/audio-tools-engine.js';

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
    this.keyboardShortcuts = new KeyboardShortcutHandler(this);
    this.initMediaSession();
    this.initQualityPicker();

    if (this.videoID) {
      this.markerManager = new MarkerManager(this);
      this.clipManager = new ClipManager(this);
      this.transcriptManager = new TranscriptManager(this);
      void this.initSeekThumbnails();
      this.initPositionTracking();
    }
  }
  
  buildControls() {
    // Controls are now server-rendered by the VideoPlayerControls templ
    // component. We just bind to the existing DOM elements by class name.
    this.controlsContainer = this.container.querySelector('.video-controls');
    
    this.progressContainer = this.container.querySelector('.progress-container');
    this.progressBar = this.container.querySelector('.progress-bar');
    this.progressFill = this.container.querySelector('.progress-fill');
    
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
    
    // Add container classes
    this.container.classList.add('custom-video-player');
  }
  
  attachEventListeners() {
    // Play/pause
    this.playBtn.addEventListener('click', () => this.togglePlayPause());
    this.video.addEventListener('click', () => this.togglePlayPause());
    
    // Progress bar
    this.progressBar.addEventListener('click', (e) => this.seekToPosition(e));
    this.progressBar.addEventListener('mousedown', () => {
      this.seeking = true;
    });
    document.addEventListener('mouseup', () => {
      this.seeking = false;
    });
    this.progressBar.addEventListener('mousemove', (e) => {
      if (this.seeking) {
        this.seekToPosition(e);
      }
      this.queueSeekTooltipUpdate(e);
    });

    if (this.progressContainer) {
      this.progressContainer.addEventListener('mouseleave', () => this.hideSeekTooltip());
      this.progressContainer.addEventListener('mousemove', (e) => this.queueSeekTooltipUpdate(e));
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
    document.addEventListener('fullscreenchange', () => this.handleFullscreenChange());
    
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
      const res = await fetch(`/api/videos/${encodeURIComponent(this.videoID)}/seek/seek.json`, {
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
    if (!this.seek.manifest) return;
    if (!this.video || !isFinite(this.video.duration) || this.video.duration <= 0) return;
    if (!this.progressBar) return;

    // Throttle to rAF to avoid excessive DOM work.
    if (this._seekTooltipRAF) return;
    this._seekTooltipRAF = requestAnimationFrame(() => {
      this._seekTooltipRAF = null;
      this.updateSeekTooltip(evt);
    });
  }

  hideSeekTooltip() {
    if (!this.seekTooltip) return;
    this.seekTooltip.classList.add('hidden');
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
        const res = await fetch(
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
    if (!this.seek.manifest || !this.progressBar || !this.video) return;
    if (!isFinite(this.video.duration) || this.video.duration <= 0) return;

    const rect = this.progressBar.getBoundingClientRect();
    const x = Math.max(0, Math.min(rect.width, evt.clientX - rect.left));
    const pct = rect.width > 0 ? x / rect.width : 0;
    const t = pct * this.video.duration;

    const lvl = this.chooseSeekLevel();
    const levelName = (lvl?.name || '').toString();
    if (!levelName) {
      this.hideSeekTooltip();
      return;
    }

    const cues = await this.ensureSeekVttLoaded(levelName);
    if (!cues || cues.length === 0) {
      this.hideSeekTooltip();
      return;
    }

    const interval = Number(lvl?.interval_seconds);
    let idx = isFinite(interval) && interval > 0 ? Math.floor(t / interval) : -1;
    if (!isFinite(idx) || idx < 0) idx = 0;
    if (idx >= cues.length) idx = cues.length - 1;
    const cue = cues[idx];
    if (!cue) {
      this.hideSeekTooltip();
      return;
    }

    const sheetURL = `/api/videos/${encodeURIComponent(this.videoID)}/seek/levels/${encodeURIComponent(levelName)}/${encodeURIComponent(cue.sheet)}`;
    const sheetW = Number(lvl?.cols) * Number(lvl?.thumb_width);
    const sheetH = Number(lvl?.rows) * Number(lvl?.thumb_height);

    this.seekTooltipThumb.style.width = `${cue.w}px`;
    this.seekTooltipThumb.style.height = `${cue.h}px`;
    this.seekTooltipThumb.style.backgroundImage = `url(${sheetURL})`;
    this.seekTooltipThumb.style.backgroundRepeat = 'no-repeat';
    if (isFinite(sheetW) && isFinite(sheetH) && sheetW > 0 && sheetH > 0) {
      this.seekTooltipThumb.style.backgroundSize = `${sheetW}px ${sheetH}px`;
    } else {
      this.seekTooltipThumb.style.backgroundSize = '';
    }
    this.seekTooltipThumb.style.backgroundPosition = `-${cue.x}px -${cue.y}px`;

    this.seekTooltipTime.textContent = this.formatTime(t);

    // Position tooltip.
    const tooltip = this.seekTooltip;
    tooltip.classList.remove('hidden');

    // Clamp X so the tooltip stays within the progress bar.
    const tooltipW = tooltip.offsetWidth || 0;
    let leftPx = x;
    if (tooltipW > 0) {
      leftPx = Math.max(tooltipW / 2, Math.min(rect.width - tooltipW / 2, leftPx));
    }
    tooltip.style.left = `${leftPx}px`;
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
    const rect = this.progressBar.getBoundingClientRect();
    const pos = (e.clientX - rect.left) / rect.width;
    this.video.currentTime = pos * this.video.duration;
  }
  
  updateProgress() {
    if (!this.video.duration) return;
    
    const percent = (this.video.currentTime / this.video.duration) * 100;
    this.progressFill.style.width = percent + '%';
    
    const current = this.formatTime(this.video.currentTime);
    const duration = this.formatTime(this.video.duration);
    this.timeDisplay.textContent = `${current} / ${duration}`;

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
    this.isTheaterMode = !this.isTheaterMode;
    this.container.classList.toggle('theater-mode', this.isTheaterMode);
    
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
      this.hideControlsTimeout = setTimeout(() => {
        this.hideControls();
      }, 3000);
    }
  }
  
  hideControls() {
    if (!this.video.paused) {
      this.controlsVisible = false;
      this.controlsContainer.classList.add('hidden');
    }
  }
  
  seekRelative(seconds) {
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

  // Position tracking methods
  initPositionTracking() {
    // Start interval to save position periodically (every 5 seconds during playback)
    this.positionSaveInterval = setInterval(() => {
      if (!this.video.paused && !this.video.ended) {
        this.saveCurrentPosition();
      }
    }, 5000);

    // Save position when page unloads
    window.addEventListener('beforeunload', () => {
      this.saveCurrentPosition();
    });
  }

  restoreSavedPosition() {
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
    fetch(`/api/videos/${encodeURIComponent(this.videoID)}/position`, {
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
      const res = await fetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/markers`, {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) return;
      this.markers = await res.json();
      
      // Separate skip segments (markers with duration > 0)
      this.skipSegments = this.markers.filter(m => 
        m.duration && m.duration > 0
      );
      
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
      const res = await fetch(`/api/markers/${encodeURIComponent(id)}`, { method: 'DELETE' });
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
    this._skipNotifTimeout = setTimeout(() => {
      toast.classList.add('fade-out');
      setTimeout(() => toast.classList.add('hidden'), 300);
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

  clearTicks() {
    this.player.progressBar.querySelectorAll('.marker-tick, .marker-range').forEach(el => el.remove());
  }

  render() {
    if (!this.player.progressBar) return;
    this.clearTicks();

    const duration = this.player.video.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;

    (this.markers || []).forEach(m => {
      const ts = typeof m.timestamp === 'number' ? m.timestamp : NaN;
      if (!isFinite(ts) || ts < 0 || ts > duration) return;

      // If marker has duration, render as a range
      if (m.duration && m.duration > 0) {
        const start = ts;
        const end = Math.min(ts + m.duration, duration);
        
        const range = document.createElement('div');
        range.className = 'marker-range';
        range.style.left = `${(start / duration) * 100}%`;
        range.style.width = `${((end - start) / duration) * 100}%`;
        if (m.color) {
          range.style.background = m.color;
        }
        if (m.title) {
          range.title = `${m.title} (${m.duration.toFixed(1)}s)`;
        }
        
        // Click to jump to start of segment
        range.addEventListener('click', (e) => {
          e.stopPropagation();
          this.player.video.currentTime = start;
        });
        
        this.player.progressBar.appendChild(range);
      } else {
        // Point marker (existing code)
        const tick = document.createElement('div');
        tick.className = 'marker-tick';
        tick.style.left = `${(ts / duration) * 100}%`;
        if (m.color) {
          tick.style.background = m.color;
        }
        if (m.title) {
          tick.title = m.title;
        }
        tick.addEventListener('click', (e) => {
          e.stopPropagation();
          this.player.video.currentTime = ts;
        });

        this.player.progressBar.appendChild(tick);
      }
    });
  }

  async createMarkerAtCurrentTime() {
    if (!this.player.videoID) return;

    const ts = this.player.video.currentTime;
    if (!isFinite(ts) || ts < 0) return;

    try {
      const res = await fetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/markers`, {
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
      const observer = new MutationObserver(() => this._discoverCues());
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
        this.scrollTimeout = setTimeout(() => {
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
      const res = await fetch(`/api/videos/${encodeURIComponent(this.player.videoID)}/clips`, {
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
    this.player.progressBar?.querySelectorAll('.clip-range').forEach(el => el.remove());
  }

  renderIfNeeded() {
    const duration = this.player.video.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;
    if (this.renderedForDuration === duration) return;
    this.renderedForDuration = duration;
    this.renderTimeline();
  }

  renderTimeline() {
    if (!this.player.progressBar) return;
    const duration = this.player.video.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;

    this.clearTimeline();

    (this.clips || []).forEach(cl => {
      const startTs = cl.StartTs ?? cl.start_ts ?? 0;
      const endTs = cl.EndTs ?? cl.end_ts ?? 0;
      if (!isFinite(startTs) || !isFinite(endTs) || endTs <= startTs) return;
      if (startTs < 0 || startTs > duration) return;

      const start = Math.max(0, Math.min(startTs, duration));
      const end = Math.max(0, Math.min(endTs, duration));
      if (end <= start) return;

      const left = (start / duration) * 100;
      const width = ((end - start) / duration) * 100;

      const range = document.createElement('div');
      range.className = 'clip-range';
      range.style.left = `${left}%`;
      range.style.width = `${width}%`;

      const color = (cl.color || cl.Color || '').toString().trim();
      if (color) {
        range.style.background = color;
      }

      const title = (cl.title || cl.Title || '').toString().trim();
      if (title) {
        range.title = title;
      }

      range.addEventListener('click', (e) => {
        e.stopPropagation();
        const rect = this.player.progressBar.getBoundingClientRect();
        const pos = rect.width > 0 ? (e.clientX - rect.left) / rect.width : 0;
        this.player.video.currentTime = Math.max(0, Math.min(pos, 1)) * duration;
      });

      this.player.progressBar.appendChild(range);
    });
  }

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
      const res = await fetch(`/api/clips/${encodeURIComponent(id)}`, { method: 'DELETE' });
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
    document.addEventListener('keydown', (e) => {
      if (!this.enabled) return;
      
      // Don't trigger if user is typing in an input
      if (
        e.target?.isContentEditable ||
        e.target?.tagName === 'INPUT' ||
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

// Auto-initialize players on page load
document.addEventListener('DOMContentLoaded', () => {
  const playerContainers = document.querySelectorAll('[data-video-player]');
  playerContainers.forEach(container => {
    new VideoPlayer(container);
  });
});
