// ============================================================================
// FilterPreviewEngine - real-time CSS + overlay preview of the filter stack
// ============================================================================

import { AudioPreviewGraph } from './audio-preview-graph.js';

export class FilterPreviewEngine {
  constructor(videoElement, container) {
    this.video = videoElement;
    this.container = container;
    this.audioGraph = new AudioPreviewGraph(videoElement);
    this.overlayContainer = null;
    this.currentStack = [];
    this._savedPlaybackRate = null;
  }

  apply(filterStack) {
    this.currentStack = filterStack || [];
    const preview = this.compile(this.currentStack);

    const hasVisualFilter = preview.filter !== 'none' || preview.transform !== 'none';
    this.video.style.filter = preview.filter;
    this.video.style.transform = preview.transform;

    // CSS filter on <video> also affects browser-rendered captions.
    // Hide native track rendering while visual filters are active.
    this._setCaptionsHidden(hasVisualFilter);

    if (preview.playbackRate !== 1.0) {
      if (this._savedPlaybackRate === null) {
        this._savedPlaybackRate = this.video.playbackRate;
      }
      this.video.playbackRate = preview.playbackRate;
    } else if (this._savedPlaybackRate !== null) {
      this.video.playbackRate = this._savedPlaybackRate;
      this._savedPlaybackRate = null;
    }

    this.video.muted = preview.muted;
    this.updateOverlays(preview.overlays);
    this.audioGraph.rebuild(preview.audioNodes, preview.muted);
  }

  clear() {
    this.currentStack = [];
    this.video.style.filter = '';
    this.video.style.transform = '';
    this._setCaptionsHidden(false);
    if (this._savedPlaybackRate !== null) {
      this.video.playbackRate = this._savedPlaybackRate;
      this._savedPlaybackRate = null;
    }
    this.video.muted = false;
    this.clearOverlays();
    this.audioGraph.rebuild([], false);
  }

  updateParam(filterIndex, paramName, value) {
    // Don't call audioGraph.updateParam directly - filter stack indices don't
    // map 1:1 to audio node indices. Instead, update the local stack and
    // call apply() which rebuilds the entire audio graph correctly.
    if (this.currentStack[filterIndex]) {
      if (!this.currentStack[filterIndex].params) this.currentStack[filterIndex].params = {};
      this.currentStack[filterIndex].params[paramName] = value;
      this.apply(this.currentStack);
    }
  }

  compile(filterStack) {
    const filters = [];
    const transforms = [];
    const audioNodes = [];
    let playbackRate = 1.0;
    let muted = false;
    const overlays = [];

    for (const f of filterStack) {
      const p = f.params || {};
      switch (f.type) {
        case 'brightness':
          filters.push(`brightness(${1 + (p.value || 0)})`);
          break;
        case 'contrast':
          filters.push(`contrast(${p.value ?? 1})`);
          break;
        case 'saturation':
          filters.push(`saturate(${p.value ?? 1})`);
          break;
        case 'gamma':
          // CSS doesn't have a gamma filter; approximate with brightness
          if (p.value && p.value !== 1) {
            filters.push(`brightness(${Math.pow(0.5, 1 / p.value) * 2})`);
          }
          break;
        case 'grayscale':
          filters.push('grayscale(1)');
          break;
        case 'sepia':
          filters.push('sepia(1)');
          break;
        case 'color_temp': {
          // Approximate color temperature with sepia + hue-rotate
          const temp = p.temperature ?? 6500;
          if (temp < 6500) {
            // Warmer - add orange tint
            const warmth = (6500 - temp) / 5500;
            filters.push(`sepia(${warmth * 0.3}) saturate(${1 + warmth * 0.3})`);
          } else if (temp > 6500) {
            // Cooler - add blue tint via hue rotation
            const coolness = (temp - 6500) / 5500;
            filters.push(`hue-rotate(${coolness * 30}deg) saturate(${1 - coolness * 0.15})`);
          }
          const tint = p.tint ?? 0;
          if (tint !== 0) {
            filters.push(`hue-rotate(${tint * 20}deg)`);
          }
          break;
        }
        case 'lift_gamma_gain': {
          const lift = p.lift ?? 0;
          const gamma = p.gamma ?? 1;
          const gain = p.gain ?? 1;
          if (lift !== 0) filters.push(`brightness(${1 + lift})`);
          if (gamma !== 1) filters.push(`brightness(${Math.pow(0.5, 1 / gamma) * 2})`);
          if (gain !== 1) filters.push(`contrast(${gain})`);
          break;
        }
        case 'exposure': {
          const ev = p.exposure ?? 0;
          if (ev !== 0) filters.push(`brightness(${Math.pow(2, ev)})`);
          break;
        }
        case 'lut': {
          // Approximate LUT presets with CSS filters
          const lutMap = {
            'cinematic_warm':  'sepia(0.2) contrast(1.1) saturate(0.9)',
            'cinematic_cool':  'hue-rotate(10deg) contrast(1.15) saturate(0.85)',
            'film_noir':       'grayscale(1) contrast(1.4) brightness(1.05)',
            'bleach_bypass':   'saturate(0.4) contrast(1.3) brightness(1.05)',
            'orange_teal':     'sepia(0.15) saturate(1.2) contrast(1.1)',
            'vintage_fade':    'sepia(0.3) contrast(0.9) brightness(1.05) saturate(0.7)',
            'high_contrast':   'grayscale(1) contrast(1.6)',
            'pastel':          'saturate(0.6) brightness(1.1)',
            'golden_hour':     'sepia(0.25) saturate(1.15) brightness(1.03)',
            'moonlit':         'hue-rotate(20deg) saturate(0.6) brightness(0.95)',
          };
          if (p.preset && lutMap[p.preset]) filters.push(lutMap[p.preset]);
          break;
        }
        case 'sharpen':
          // No CSS equivalent - skip for preview
          break;
        case 'denoise':
          // No CSS equivalent - skip for preview
          break;
        case 'hflip':
          transforms.push('scaleX(-1)');
          break;
        case 'vflip':
          transforms.push('scaleY(-1)');
          break;
        case 'transpose': {
          const dir = String(p.direction || 'cw');
          if (dir === 'cw')        transforms.push('rotate(90deg)');
          else if (dir === 'ccw')   transforms.push('rotate(-90deg)');
          else if (dir === 'ccw_flip') transforms.push('rotate(-90deg) scaleX(-1)');
          else if (dir === 'cw_flip')  transforms.push('rotate(90deg) scaleX(-1)');
          break;
        }
        case 'speed':
          playbackRate = p.factor || 1.0;
          break;
        case 'mute':
          muted = true;
          break;
        case 'vignette':
          overlays.push({ type: 'vignette', angle: p.angle ?? 0.5 });
          break;
        case 'text':
          overlays.push({ type: 'text', text: p.text || '', position: p.position || 'bottom-right', size: p.size || 24 });
          break;
        case 'curves': {
          const presetMap = {
            'lighter':            'brightness(1.1)',
            'darker':             'brightness(0.85)',
            'increase_contrast':  'contrast(1.3)',
            'negative':           'invert(1)',
            'cross_process':      'hue-rotate(20deg) saturate(1.3)',
            'vintage':            'sepia(0.3) contrast(1.1) brightness(1.05)',
          };
          if (presetMap[p.preset]) filters.push(presetMap[p.preset]);
          break;
        }
        // Audio filters → Web Audio nodes
        case 'volume':
          audioNodes.push({ type: 'gain', gain: p.gain ?? 1.0 });
          break;
        case 'bass':
          audioNodes.push({ type: 'biquad', filter: 'lowshelf', frequency: 200, gain: p.gain ?? 0 });
          break;
        case 'treble':
          audioNodes.push({ type: 'biquad', filter: 'highshelf', frequency: 4000, gain: p.gain ?? 0 });
          break;
        case 'equalizer':
          audioNodes.push({
            type: 'biquad', filter: 'peaking',
            frequency: p.frequency ?? 1000,
            Q: (p.width ?? 200) > 0 ? (p.frequency ?? 1000) / (p.width ?? 200) : 5,
            gain: p.gain ?? 0,
          });
          break;
        case 'highpass':
          audioNodes.push({ type: 'biquad', filter: 'highpass', frequency: p.frequency ?? 200 });
          break;
        case 'lowpass':
          audioNodes.push({ type: 'biquad', filter: 'lowpass', frequency: p.frequency ?? 8000 });
          break;
        case 'compressor':
          audioNodes.push({
            type: 'compressor',
            threshold: p.threshold ?? -24,
            ratio: p.ratio ?? 4,
            attack: (p.attack ?? 20) / 1000, // ms → seconds for Web Audio
            release: (p.release ?? 250) / 1000,
          });
          break;
        case 'audio_fade_in':
          audioNodes.push({ type: 'fade_in', duration: p.duration ?? 0.5 });
          break;
        case 'audio_fade_out':
          audioNodes.push({ type: 'fade_out', duration: p.duration ?? 0.5 });
          break;
        case 'noise_gate':
          audioNodes.push({ type: 'gate', threshold: p.threshold ?? -40 });
          break;
      }
    }

    return {
      filter: filters.join(' ') || 'none',
      transform: transforms.join(' ') || 'none',
      playbackRate,
      muted,
      overlays,
      audioNodes,
    };
  }

  ensureOverlayContainer() {
    if (this.overlayContainer) return;
    // Use the pre-rendered overlay container from templ (VideoPlayerControls)
    this.overlayContainer = this.video.parentElement.querySelector('[data-filter-preview-overlays]');
    if (!this.overlayContainer) {
      // Fallback for cut page where the templ component may not be present
      this.overlayContainer = document.querySelector('[data-filter-preview-overlays]');
    }
    if (this.overlayContainer) {
      this.overlayContainer.style.display = '';
      this._vignetteSlot = this.overlayContainer.querySelector('[data-overlay-vignette]');
      this._textSlot = this.overlayContainer.querySelector('[data-overlay-text]');
    }
  }

  updateOverlays(overlays) {
    if (!overlays.length) { this.clearOverlays(); return; }
    this.ensureOverlayContainer();
    if (!this.overlayContainer) return;

    // Reset both pre-rendered slots
    if (this._vignetteSlot) {
      this._vignetteSlot.style.display = 'none';
      this._vignetteSlot.style.background = '';
    }
    if (this._textSlot) {
      this._textSlot.style.display = 'none';
      this._textSlot.textContent = '';
      this._textSlot.style.fontSize = '';
    }

    for (const o of overlays) {
      switch (o.type) {
        case 'vignette': {
          if (!this._vignetteSlot) break;
          const a = Math.min(1, Math.max(0, o.angle ?? 0.5));
          this._vignetteSlot.style.display = '';
          this._vignetteSlot.style.background = `radial-gradient(ellipse at center, transparent 40%, rgba(0,0,0,${a}) 100%)`;
          break;
        }
        case 'text': {
          if (!this._textSlot) break;
          this._textSlot.textContent = o.text || '';
          this._textSlot.style.fontSize = (o.size || 24) + 'px';
          this._textSlot.style.display = '';
          this._positionOverlay(this._textSlot, o.position || 'bottom-center');
          break;
        }
      }
    }
  }

  _positionOverlay(el, position) {
    const map = {
      'top-left':      'top:0;left:0;bottom:auto;right:auto;transform:none;',
      'top-center':    'top:0;left:50%;bottom:auto;right:auto;transform:translateX(-50%);',
      'top-right':     'top:0;right:0;bottom:auto;left:auto;transform:none;',
      'center':        'top:50%;left:50%;bottom:auto;right:auto;transform:translate(-50%,-50%);',
      'bottom-left':   'bottom:0;left:0;top:auto;right:auto;transform:none;',
      'bottom-center': 'bottom:0;left:50%;top:auto;right:auto;transform:translateX(-50%);',
      'bottom-right':  'bottom:0;right:0;top:auto;left:auto;transform:none;',
    };
    el.style.cssText += (map[position] || map['bottom-center']);
  }

  clearOverlays() {
    if (this._vignetteSlot) {
      this._vignetteSlot.style.display = 'none';
    }
    if (this._textSlot) {
      this._textSlot.style.display = 'none';
    }
  }

  destroy() {
    this.clear();
    this.audioGraph.destroy();
    if (this.overlayContainer) {
      this.overlayContainer.style.display = 'none';
      this.overlayContainer = null;
    }
  }

  _setCaptionsHidden(hidden) {
    for (const track of this.video.textTracks) {
      if (track.kind === 'subtitles' || track.kind === 'captions') {
        track.mode = hidden ? 'hidden' : 'showing';
      }
    }
  }
}
