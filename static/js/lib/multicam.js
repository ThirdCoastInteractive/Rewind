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
    this._saving = false;
    this._bound = false;
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

    // Camera buttons
    this._onCameraClick = (e) => {
      const btn = e.target.closest('[data-multicam-camera]');
      if (!btn) return;
      this._addShotAtPlayhead(btn.dataset.multicamCamera, btn.dataset.cropName || '');
    };
    panel.addEventListener('click', this._onCameraClick);

    // Clear / Undo / Remove buttons
    this._onActionClick = (e) => {
      if (e.target.closest('[data-multicam-clear]')) {
        if (this.shots.length === 0 || confirm('Clear all shots?')) {
          this.shots = [];
          this._render();
          this._persist();
        }
        return;
      }
      if (e.target.closest('[data-multicam-undo]')) {
        if (this.shots.length > 0) {
          this.shots.pop();
          this._render();
          this._persist();
        }
        return;
      }
      const removeBtn = e.target.closest('[data-multicam-remove-shot]');
      if (removeBtn) {
        const idx = parseInt(removeBtn.dataset.multicamRemoveShot, 10);
        if (!isNaN(idx) && idx >= 0 && idx < this.shots.length) {
          this.shots.splice(idx, 1);
          this._recalcBoundaries();
          this._render();
          this._persist();
        }
        return;
      }
      // Export button
      if (e.target.closest('[data-multicam-export]')) {
        this._exportMulticam();
        return;
      }
    };
    panel.addEventListener('click', this._onActionClick);

    this._bound = true;
  }

  _unbind() {
    if (this._panel) {
      if (this._onCameraClick) this._panel.removeEventListener('click', this._onCameraClick);
      if (this._onActionClick) this._panel.removeEventListener('click', this._onActionClick);
    }
    this._bound = false;
  }

  /** Called when a clip is selected — load its shot list. */
  loadForClip(clipId, shots) {
    this.clipId = clipId;
    this.shots = Array.isArray(shots) ? [...shots] : [];
    this.bind();
    this._render();
  }

  /** Called when clip is deselected. */
  clear() {
    this.clipId = null;
    this.shots = [];
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

    if (clipRelTime < 0 || clipRelTime > clip.duration) return;

    const trTypeEl = this._panel?.querySelector('[data-multicam-transition-type]');
    const trDurEl = this._panel?.querySelector('[data-multicam-transition-dur]');
    const trType = trTypeEl?.value || 'fade';
    const trDur = parseFloat(trDurEl?.value || '0.5') || 0.5;

    if (this.shots.length === 0) {
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
      const lastShot = this.shots[this.shots.length - 1];

      // Can't split if playhead is at or after the last shot's end
      if (clipRelTime >= lastShot.end - 0.1) return;
      // Can't split if playhead is at or before the last shot's start
      if (clipRelTime <= lastShot.start + 0.1) return;

      // Set transition on the outgoing shot
      const transition = trType === 'cut' ? null : { type: trType, duration: trDur };
      lastShot.transition_out = transition;
      lastShot.end = clipRelTime;

      // New shot from playhead to the old end
      this.shots.push({
        crop_id: cropId,
        start: clipRelTime,
        end: clip.duration,
        transition_out: null,
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
        + 'Position the playhead and click a camera to add shots.'
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
        + `<span class="text-white/80 flex-1 truncate">${name}</span>`
        + `<span class="text-white/40">${shot.start.toFixed(1)}–${shot.end.toFixed(1)}s</span>`
        + trIcon
        + `<button type="button" class="text-white/20 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity" data-multicam-remove-shot="${i}" title="Remove shot">`
        + `<i class="fa-sharp fa-solid fa-xmark"></i></button>`
        + `</div>`;
    }
    this._shotListEl.innerHTML = html;
  }

  /** Update playhead indicator position on the timeline. */
  _updatePlayhead() {
    if (!this._playhead) return;
    const clip = this.editor.clips.find(c => c.id === this.editor.selectedClipId);
    if (!clip || clip.duration <= 0 || !this.editor.video) {
      this._playhead.style.left = '0%';
      return;
    }
    const rel = (this.editor.video.currentTime - clip.start) / clip.duration;
    this._playhead.style.left = `${(Math.max(0, Math.min(1, rel)) * 100).toFixed(2)}%`;
  }

  /** Called on video timeupdate to animate the playhead. */
  onTimeUpdate() {
    this._updatePlayhead();
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
        return row.dataset.cropName || cropId.slice(0, 8);
      }
    }
    return cropId.slice(0, 8);
  }

  /** Persist the shot list to the server. */
  async _persist() {
    if (!this.clipId || this._saving) return;
    this._saving = true;
    try {
      await fetch(`/api/clips/${this.clipId}/shot-list`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ shots: this.shots }),
      });
    } catch (e) {
      console.error('Failed to save shot list:', e);
    } finally {
      this._saving = false;
    }
  }

  /** Trigger multicam export via DataStar SSE post. */
  _exportMulticam() {
    if (!this.clipId || this.shots.length < 2) return;

    const formatEl = this._panel?.querySelector('[data-multicam-format]');
    const qualityEl = this._panel?.querySelector('[data-multicam-quality]');
    const format = formatEl?.value || 'mp4';
    const quality = qualityEl?.value || 'high';

    // Use DataStar's @post to get SSE streaming status updates
    const api = window.__dsAPI;
    if (api) {
      api.mergePatch({});
      // Trigger via synthetic DataStar post
      const url = `/api/clips/${this.clipId}/multicam-export`;
      const payload = JSON.stringify({ format, quality });
      // Execute as a DataStar action expression
      const script = `@post('${url}', {payload: ${payload}})`;
      try {
        api.actions(script);
      } catch (_) {
        // Fallback: direct script execution
        const el = document.createElement('div');
        el.setAttribute('data-on:click', script);
        document.body.appendChild(el);
        el.click();
        el.remove();
      }
    }
  }
}
