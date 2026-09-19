import { pageFetch, listen, onPageCleanup } from './page-scope.js';
import { SaveQueue } from './save-queue.js';
import { formatTime } from './utils.js';

/**
 * MulticamEngine manages the shot list for multicam crop switching.
 * It handles UI interaction (camera button clicks, timeline rendering)
 * and persists the shot list to the server.
 */
export class MulticamEngine {
  constructor(editor) {
    this.editor = editor;
    this.shots = [];
    this.clipId = null;
    this._bound = false;
    this._history = [];
    this._queues = new Map();
    const video = editor.video;
    if (video) {
      for (const event of ['timeupdate', 'seeked', 'loadeddata', 'emptied']) {
        listen(video, event, () => this.onTimeUpdate());
      }
      // Reuse decoded source frames; no extra video streams or polling.
      const frame = () => {
        if (this._disposed) return;
        this.onTimeUpdate();
        this._frameCallback = video.requestVideoFrameCallback(frame);
      };
      if (video.requestVideoFrameCallback) this._frameCallback = video.requestVideoFrameCallback(frame);
    }
    // Crop/inspector SSE patches can replace the panel's child controls while
    // retaining its root. Rebind after the response has finished morphing.
    listen(document, 'datastar-fetch', event => {
      if (event.detail?.type === 'finished' && this.clipId) this.refreshPanel();
    });
    const flush = () => Promise.all([...this._queues.values()].map(queue => queue.flush()));
    const release = window.RewindNavigation?.beforeLeave(flush);
    listen(window, 'beforeunload', event => {
      if ([...this._queues.values()].some(queue => queue.dirty)) {
        event.preventDefault(); event.returnValue = '';
      }
    });
    onPageCleanup(() => {
      this._disposed = true;
      video?.cancelVideoFrameCallback?.(this._frameCallback);
      release?.(); this._unbind();
      for (const queue of this._queues.values()) queue.dispose();
    });
  }

  /** Bind to the multicam panel DOM after SSE renders it. */
  bind() {
    const panel = document.querySelector('[data-multicam-panel]');
    if (!panel) return;

    // Avoid duplicate event bindings
    if (this._bound) this._unbind();

    this._panel = panel;
    this._timeline = panel.querySelector('[data-multicam-timeline-layer]');
    this._playhead = panel.querySelector('[data-multicam-playhead]');
    this._shotListEl = panel.querySelector('[data-multicam-shot-list]');
    this._shotCount = panel.querySelector('[data-multicam-shot-count]');
    this._program = panel.querySelector('[data-multicam-program]');
    this._programCamera = panel.querySelector('[data-multicam-program-camera]');
    this._programEmpty = panel.querySelector('[data-multicam-program-empty]');
    if (this._program && typeof ResizeObserver !== 'undefined') {
      this._programResize = new ResizeObserver(() => this.renderProgram());
      this._programResize.observe(this._program);
    }

    // Camera buttons
    this._onCameraClick = (e) => {
      const preview = e.target.closest('[data-multicam-preview]');
      if (preview) {
        const row = document.querySelector(`[data-crop-id="${preview.dataset.multicamPreview}"] [data-crop-x]`);
        if (row) window.cropRowSelect?.(row);
        return;
      }
      const btn = e.target.closest('[data-multicam-camera]');
      if (!btn) return;
      this._addShotAtPlayhead(btn.dataset.multicamCamera, btn.dataset.cropName || '');
    };
    panel.addEventListener('click', this._onCameraClick);

    // Clear / Undo / Remove buttons
    this._onActionClick = (e) => {
      if (e.target.closest('[data-multicam-clear]')) {
        if (this.shots.length === 0 || confirm('Clear all shots?')) {
          this._remember();
          this.shots = [];
          this._render();
          this._persist();
        }
        return;
      }
      if (e.target.closest('[data-multicam-undo]')) {
        if (this._history.length > 0) {
          this.shots = this._history.pop();
          this._render();
          this._persist();
        }
        return;
      }
      const removeBtn = e.target.closest('[data-multicam-remove-shot]');
      if (removeBtn) {
        const idx = parseInt(removeBtn.dataset.multicamRemoveShot, 10);
        if (!isNaN(idx) && idx >= 0 && idx < this.shots.length) {
          this._remember();
          this.shots.splice(idx, 1);
          this._recalcBoundaries();
          this._render();
          this._persist();
        }
        return;
      }
      const seek = e.target.closest('[data-multicam-seek]');
      if (seek) {
        const clip = this.editor.clips.find(c => c.id === this.clipId);
        if (clip) this.editor.video.currentTime = clip.start + Number(seek.dataset.multicamSeek);
      }
      if (e.target.closest('[data-multicam-retry-save]')) this._queues.get(this.clipId)?.flush().catch(() => {});
    };
    panel.addEventListener('click', this._onActionClick);

    this._bound = true;
  }

  _unbind() {
    this._programResize?.disconnect();
    if (this._panel) {
      if (this._onCameraClick) this._panel.removeEventListener('click', this._onCameraClick);
      if (this._onActionClick) this._panel.removeEventListener('click', this._onActionClick);
    }
    this._bound = false;
  }

  /** Rebind camera controls after the server patches the panel. */
  refreshPanel() {
    this.bind();
    if (this._panel?.dataset.clipId !== this.clipId) return;
    this._render();
    window.__dsAPI?.mergePatch({ _multicamSaving: !!this._queues.get(this.clipId)?.dirty });
  }

  /** Called when a clip is selected — load its shot list. */
  loadForClip(clipId, shots) {
    if (this.clipId !== clipId) this._history = [];
    this.clipId = clipId;
    const pending = this._queues.get(clipId);
    this.shots = pending?.dirty && pending.latest ? JSON.parse(pending.latest) : structuredClone(Array.isArray(shots) ? shots : []);
    this.bind();
    this._render();
    window.__dsAPI?.mergePatch({ _multicamSaving: !!pending?.dirty, _multicamSaveFailed: false });
  }

  /** Called when clip is deselected. */
  clear() {
    this.clipId = null;
    this.shots = [];
    this._history = [];
    this._render();
  }

  /** Add a shot at the current playhead position using the given crop. */
  _addShotAtPlayhead(cropId, cropName) {
    const editor = this.editor;
    if (!editor.selectedClipId || !editor.video) return;

    const clip = editor.clips.find(c => c.id === editor.selectedClipId);
    if (!clip) return;

    const videoTime = editor.video.currentTime;
    const clipRelTime = videoTime - clip.start;

    if (clipRelTime < 0 || clipRelTime >= clip.duration) {
      this._notice('Move the playhead inside the selected clip to switch cameras.');
      return;
    }

    const trTypeEl = this._panel?.querySelector('[data-multicam-transition-type]');
    const trDurEl = this._panel?.querySelector('[data-multicam-transition-dur]');
    const trType = trTypeEl?.value || 'fade';
    const trDur = parseFloat(trDurEl?.value || '0.5') || 0.5;

    if (this.shots.length === 0) {
      this._remember();
      // First shot: from clip start to playhead? No — from start to end, we'll split later.
      // Actually: first shot starts at 0 and goes to the current time (or clip end).
      // Better: first camera covers clip start to playhead.
      // But if playhead is at start, it covers the full clip.
      this.shots.push({
        crop_id: cropId,
        start: 0,
        end: clip.duration,
        transition_out: null,
      });
    } else {
      // Split the current shot at the playhead and insert the new crop.
      const shotIndex = this.shots.findIndex(shot => clipRelTime >= shot.start && clipRelTime < shot.end);
      if (shotIndex < 0) return;
      const lastShot = this.shots[shotIndex];
      if (lastShot.crop_id === cropId) return;

      // Can't split if playhead is at or after the last shot's end
      if (clipRelTime >= lastShot.end - 0.1) return;
      // Can't split if playhead is at or before the last shot's start
      this._remember();
      if (clipRelTime <= lastShot.start + 0.1) {
        lastShot.crop_id = cropId;
        this._render(); this._persist(); return;
      }

      // Set transition on the outgoing shot
      const transition = trType === 'cut' ? null : { type: trType, duration: Math.min(trDur, (clipRelTime - lastShot.start) / 2) };
      const previousEnd = lastShot.end;
      const previousTransition = lastShot.transition_out;
      lastShot.transition_out = transition;
      lastShot.end = clipRelTime;

      // New shot from playhead to the old end
      this.shots.splice(shotIndex + 1, 0, {
        crop_id: cropId,
        start: clipRelTime,
        end: previousEnd,
        transition_out: previousTransition ? { ...previousTransition, duration: Math.min(previousTransition.duration, (previousEnd - clipRelTime) / 2) } : null,
      });
    }

    this._render();
    this._persist();
  }

  /** Recalculate shot boundaries after a removal to close gaps. */
  _recalcBoundaries() {
    if (this.shots.length === 0) return;

    const clip = this.editor.clips.find(c => c.id === this.editor.selectedClipId);
    if (!clip) return;

    // Ensure first shot starts at 0
    this.shots[0].start = 0;

    // Make each shot's start equal to the previous shot's end
    for (let i = 1; i < this.shots.length; i++) {
      this.shots[i].start = this.shots[i - 1].end;
    }

    // Last shot extends to clip end
    this.shots[this.shots.length - 1].end = clip.duration;
  }

  /** Render the shot timeline and shot list. */
  _render() {
    this._renderTimeline();
    this._renderShotList();
    this._updatePlayhead();

    const countEl = this._panel?.querySelector('[data-multicam-shot-count]');
    if (countEl) countEl.textContent = `(${this.shots.length} shots)`;
    const undo = this._panel?.querySelector('[data-multicam-undo]');
    if (undo) undo.disabled = this._history.length === 0;
    const clear = this._panel?.querySelector('[data-multicam-clear]');
    if (clear) clear.disabled = this.shots.length === 0;

    // Update shot count signal so DataStar can enable/disable the export button
    const api = window.__dsAPI;
    if (api) {
      api.mergePatch({ _multicamShotCount: this.shots.length });
    }
  }

  _renderTimeline() {
    if (!this._timeline) return;

    const clip = this.editor.clips.find(c => c.id === this.editor.selectedClipId);
    if (!clip || clip.duration <= 0) {
      this._timeline.innerHTML = '';
      return;
    }

    // Assign colors to crop IDs for visual distinction
    const cropColors = this._getCropColors();
    const dur = clip.duration;

    let html = '';
    for (const shot of this.shots) {
      const left = (shot.start / dur) * 100;
      const width = ((shot.end - shot.start) / dur) * 100;
      const color = cropColors[shot.crop_id] || 'rgba(255,255,255,0.15)';

      html += `<div class="absolute top-0 bottom-0 border-r border-black/40" `
        + `style="left:${left.toFixed(3)}%;width:${width.toFixed(3)}%;background:${color}" `
        + `title="${this._getCropName(shot.crop_id)}: ${shot.start.toFixed(1)}s – ${shot.end.toFixed(1)}s">`
        + `</div>`;
    }
    this._timeline.innerHTML = html;
  }

  _renderShotList() {
    if (!this._shotListEl) return;

    if (this.shots.length === 0) {
      this._shotListEl.innerHTML =
        '<div class="text-xs text-white/30 font-mono text-center py-2">'
        + 'Choose your opening camera. Then move the playhead and switch cameras to build the sequence.'
        + '</div>';
      return;
    }

    let html = '';
    for (let i = 0; i < this.shots.length; i++) {
      const shot = this.shots[i];
      const name = this._getCropName(shot.crop_id);
      const trIcon = shot.transition_out
        ? `<span class="text-white/30" title="${shot.transition_out.type} ${shot.transition_out.duration}s"><i class="fa-sharp fa-solid fa-shuffle"></i></span>`
        : '';

      html += `<div class="flex items-center gap-1 px-2 py-1 bg-neutral-900/50 border border-white/5 text-xs font-mono group" data-multicam-shot-index="${i}">`
        + `<span class="text-amber-400/60 w-4">${i + 1}</span>`
        + `<button type="button" class="text-left text-white/80 flex-1 truncate hover:underline" data-multicam-seek="${shot.start}" title="Seek to shot">${name}</button>`
        + `<span class="text-white/40">${shot.start.toFixed(1)}–${shot.end.toFixed(1)}s</span>`
        + trIcon
        + `<button type="button" class="text-white/60 hover:text-red-400 p-1" data-multicam-remove-shot="${i}" title="Remove shot">`
        + `<i class="fa-sharp fa-solid fa-xmark"></i></button>`
        + `</div>`;
    }
    this._shotListEl.innerHTML = html;
  }

  /** Update playhead indicator position on the timeline. */
  _updatePlayhead() {
    this.renderProgram();
    if (!this._playhead) return;
    const clip = this.editor.clips.find(c => c.id === this.editor.selectedClipId);
    if (!clip || clip.duration <= 0 || !this.editor.video) {
      this._playhead.style.left = '0%';
      return;
    }
    const rel = (this.editor.video.currentTime - clip.start) / clip.duration;
    this._playhead.style.left = `${(Math.max(0, Math.min(1, rel)) * 100).toFixed(2)}%`;
    const active = this.shots.find(shot => rel * clip.duration >= shot.start && rel * clip.duration < shot.end);
    for (const button of this._panel.querySelectorAll('[data-multicam-camera]')) {
      const current = active?.crop_id === button.dataset.multicamCamera;
      button.setAttribute('aria-pressed', String(current));
      button.textContent = current ? 'On camera' : this.shots.length ? 'Switch here' : 'Start with camera';
      button.classList.toggle('border-amber-400', current);
    }
  }

  /** Called on video timeupdate to animate the playhead. */
  onTimeUpdate() {
    this._updatePlayhead();
  }

  /** Show the active shot's framing while keeping the source viewer untouched. */
  renderProgram() {
    const canvas = this._program;
    if (!canvas || !canvas.clientWidth) return;
    const video = this.editor.video;
    const clip = this.editor.clips.find(c => c.id === this.clipId);
    const time = (video?.currentTime ?? 0) - (clip?.start ?? 0);
    const shot = clip && time >= 0 && time < clip.duration
      ? this.shots.find(s => time >= s.start && time < s.end) : null;
    const row = shot && [...document.querySelectorAll('[data-crop-id]')].find(el => el.dataset.cropId === shot.crop_id);
    const data = row?.querySelector('[data-crop-x]')?.dataset;
    const overlay = this.editor.cropOverlay;
    const crop = shot && overlay?.selectedCropId === shot.crop_id ? overlay.crop : data ? {
      x: Number(data.cropX), y: Number(data.cropY), width: Number(data.cropW), height: Number(data.cropH),
    } : null;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.fillStyle = '#000';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    let message = !this.shots.length ? 'Choose an opening camera to preview the sequence.'
      : !shot ? 'Move the playhead inside the camera sequence.'
      : !crop ? 'This camera is unavailable. Choose another camera.'
      : video.readyState < 2 ? 'Waiting for video…' : '';
    if (!message) {
      const w = Math.min(1, Math.max(0, crop.width));
      const h = Math.min(1, Math.max(0, crop.height));
      const sx = Math.max(0, Math.min(1 - w, crop.x - w / 2)) * video.videoWidth;
      const sy = Math.max(0, Math.min(1 - h, crop.y - h / 2)) * video.videoHeight;
      const sw = w * video.videoWidth, sh = h * video.videoHeight;
      if (Number.isFinite(sx + sy + sw + sh) && sw > 0 && sh > 0) {
        const scale = Math.min(canvas.width / sw, canvas.height / sh);
        ctx.drawImage(video, sx, sy, sw, sh, (canvas.width - sw * scale) / 2, (canvas.height - sh * scale) / 2, sw * scale, sh * scale);
      } else message = 'Camera framing is unavailable.';
    }
    this._programCamera.textContent = shot ? (data?.cropName || shot.crop_id.slice(0, 8)) : 'No camera';
    this._programEmpty.textContent = message;
    this._programEmpty.hidden = !message;
    this._programEmpty.style.display = message ? '' : 'none';
  }

  /** Assign distinct colors to crop IDs. */
  _getCropColors() {
    const palette = [
      'rgba(245,158,11,0.35)',   // amber
      'rgba(59,130,246,0.35)',   // blue
      'rgba(16,185,129,0.35)',   // emerald
      'rgba(168,85,247,0.35)',   // purple
      'rgba(239,68,68,0.35)',    // red
      'rgba(236,72,153,0.35)',   // pink
      'rgba(14,165,233,0.35)',   // sky
      'rgba(234,179,8,0.35)',    // yellow
    ];
    const colors = {};
    const cropIds = [...new Set(this.shots.map(s => s.crop_id))];
    cropIds.forEach((id, i) => {
      colors[id] = palette[i % palette.length];
    });
    return colors;
  }

  _getCropName(cropId) {
    const rows = this._panel?.querySelectorAll('[data-multicam-camera]') || [];
    for (const row of rows) {
      if (row.dataset.multicamCamera === cropId) {
        return (row.dataset.cropName || cropId.slice(0, 8)).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
      }
    }
    return cropId.slice(0, 8);
  }

  _remember() {
    this._history.push(structuredClone(this.shots));
    if (this._history.length > 50) this._history.shift();
  }

  _notice(text) {
    const el = this._panel?.querySelector('[data-multicam-notice]');
    if (el) el.textContent = text;
  }

  /** Serialize saves per clip, retaining edits made during an in-flight request. */
  _persist() {
    const id = this.clipId;
    if (!id) return;
    let queue = this._queues.get(id);
    if (!queue) {
      queue = new SaveQueue(async shots => {
        const response = await pageFetch('/api/clips/' + id + '/shot-list', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ shots }),
        });
        if (!response.ok) throw new Error('Could not save camera sequence.');
        const clip = this.editor.clips.find(c => c.id === id);
        if (clip) clip.shotList = structuredClone(shots);
      }, (saving, dirty) => {
        if (this.clipId !== id) return;
        window.__dsAPI?.mergePatch({ _multicamSaving: saving || dirty, _multicamSaveFailed: !saving && dirty });
        this._notice(saving ? 'Saving sequence…' : dirty ? 'Save failed. Retry before exporting or leaving.' : 'Sequence saved');
      }, 150);
      this._queues.set(id, queue);
    }
    window.__dsAPI?.mergePatch({ _multicamSaving: true, _multicamSaveFailed: false });
    this._notice('Saving sequence…');
    queue.schedule(this.shots);
    // Scheduling the same already-saved state does not start another write.
    if (!queue.dirty) {
      window.__dsAPI?.mergePatch({ _multicamSaving: false });
      this._notice('Sequence saved');
    }
  }
}
