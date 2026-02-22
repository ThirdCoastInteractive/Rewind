import { isFiniteNumber, formatTimecode } from './utils.js';

/**
 * TransportMixin - video transport controls (play, pause, seek, frame-step, loop).
 * Mixed into CutPageEditor via Object.assign(editor, TransportMixin).
 */
export const TransportMixin = {
  seekRelative(seconds) {
    if (!this.video) return;
    if (!isFiniteNumber(this.duration)) return;
    const nextTime = Math.max(0, Math.min(this.duration, this.video.currentTime + seconds));
    this.video.currentTime = nextTime;
    this.workHeadTime = nextTime;
    this.renderPlayheads();
    this.updateTransportTime();
  },

  transportGoToStart() {
    if (!this.video) return;
    this.video.currentTime = 0;
    this.workHeadTime = 0;
    this.renderPlayheads();
    this.updateTransportTime();
  },

  transportGoToEnd() {
    if (!this.video || !isFiniteNumber(this.duration)) return;
    this.video.currentTime = this.duration;
    this.workHeadTime = this.duration;
    this.renderPlayheads();
    this.updateTransportTime();
  },

  transportPrevFrame() {
    if (!this.video) return;
    const fps = isFiniteNumber(this.videoFps) && this.videoFps > 0 ? this.videoFps : 30;
    const frameTime = 1 / fps;
    const newTime = Math.max(0, this.video.currentTime - frameTime);
    this.video.currentTime = newTime;
    this.workHeadTime = newTime;
    this.renderPlayheads();
    this.updateTransportTime();
  },

  transportNextFrame() {
    if (!this.video) return;
    const fps = isFiniteNumber(this.videoFps) && this.videoFps > 0 ? this.videoFps : 30;
    const frameTime = 1 / fps;
    const maxTime = isFiniteNumber(this.duration) ? this.duration : this.video.duration || Infinity;
    const newTime = Math.min(maxTime, this.video.currentTime + frameTime);
    this.video.currentTime = newTime;
    this.workHeadTime = newTime;
    this.renderPlayheads();
    this.updateTransportTime();
  },

  transportStop() {
    if (!this.video) return;
    this.video.pause();
    this.video.currentTime = 0;
    this.workHeadTime = 0;
    this.renderPlayheads();
    this.updateTransportTime();
    this.updateTransportPlayButton();
  },

  transportPlay() {
    if (!this.video) return;
    this.video.play().catch(() => {});
    this.updateTransportPlayButton();
  },

  transportTogglePlay() {
    if (!this.video) return;
    if (this.video.paused) {
      this.video.play().catch(() => {});
    } else {
      this.video.pause();
    }
    this.updateTransportPlayButton();
  },

  transportToggleLoop() {
    this.transportLoopEnabled = !this.transportLoopEnabled;
    this.updateTransportLoopButton();
  },

  updateTransportPlayButton() {
    if (!this.btnTransportPlay) return;
    const icon = this.btnTransportPlay.querySelector('i');
    if (icon) {
      icon.className = this.video?.paused
        ? 'fa-sharp fa-solid fa-play'
        : 'fa-sharp fa-solid fa-pause';
    }
  },

  updateTransportLoopButton() {
    if (!this.btnTransportLoop) return;
    if (this.transportLoopEnabled) {
      this.btnTransportLoop.classList.add('text-primary', 'border-primary');
    } else {
      this.btnTransportLoop.classList.remove('text-primary', 'border-primary');
    }
  },

  updateTransportTime() {
    if (!this.transportTimeEl) return;
    const current = this.video?.currentTime || 0;
    const total = isFiniteNumber(this.duration) ? this.duration : (this.video?.duration || 0);
    this.transportTimeEl.textContent = `${formatTimecode(current)} / ${formatTimecode(total)}`;
  },

  toggleFilmstrip() {
    this.showFilmstrip = !this.showFilmstrip;
    localStorage.setItem('cut-editor-show-filmstrip', this.showFilmstrip ? 'true' : 'false');
    this.render();
  },

  // --- Selection playback ---

  renderLoopButton() {
    if (!this.btnLoop) return;
    this.btnLoop.textContent = this.loopEnabled ? 'LOOP: ON' : 'LOOP: OFF';
  },

  renderPlaySelectionButton() {
    if (!this.btnPlaySelection) return;
    if (!this.video) {
      this.btnPlaySelection.textContent = 'PLAY';
      return;
    }
    this.btnPlaySelection.textContent = this.video.paused ? 'PLAY' : 'PAUSE';
  },

  toggleLoop() {
    this.loopEnabled = !this.loopEnabled;
    this.renderLoopButton();
    if (this.loopEnabled) {
      this.stopAtOut = false;
    }
  },

  togglePlaySelection() {
    if (!this.video) return;

    if (!this.video.paused) {
      this.stopAtOut = false;
      this.video.pause();
      this.renderPlaySelectionButton();
      return;
    }

    void this.playSelection();
    this.renderPlaySelectionButton();
  },

  async playSelection() {
    if (!this.video) return;
    const sel = this.getSelectionRange();
    if (!sel) return;

    this.video.currentTime = sel.start;
    try {
      await this.video.play();
    } catch (_) {
      // Autoplay restrictions.
    }

    this.stopAtOut = !this.loopEnabled;
    this.renderPlaySelectionButton();
  },

  handleSelectionPlaybackTick() {
    if (!this.video) return;
    if (!isFiniteNumber(this.video.currentTime)) return;
    if (this.video.seeking) return;

    const sel = this.getSelectionRange();
    if (!sel) return;

    const epsilon = 0.02;
    const t = this.video.currentTime;

    if (this.loopEnabled) {
      if (t >= sel.end - epsilon) {
        this.video.currentTime = Math.min(sel.start + epsilon, sel.end);
        if (this.video.paused) {
          void this.video.play().catch(() => {});
        }
      }
      return;
    }

    if (this.stopAtOut && t >= sel.end - epsilon) {
      this.stopAtOut = false;
      this.video.pause();
      this.video.currentTime = sel.end;
      this.renderPlaySelectionButton();
    }
  },
};
