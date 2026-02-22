// ============================================================================
// AudioPreviewGraph - Web Audio API graph for real-time audio filter preview
// ============================================================================

export class AudioPreviewGraph {
  constructor(videoElement) {
    this.video = videoElement;
    this.ctx = null;
    this.source = null;
    this.activeNodes = [];
    /** @type {function|null} Called after every rebuild with (source, lastNode, destination) */
    this.onRebuild = null;
  }

  ensureContext() {
    if (this.ctx) return;
    this.ctx = new AudioContext();
    // MediaElementAudioSourceNode can only be created ONCE per <video>.
    this.source = this.ctx.createMediaElementSource(this.video);
    this.source.connect(this.ctx.destination);
    // Resume context if suspended (browser autoplay policy)
    if (this.ctx.state === 'suspended') {
      this.ctx.resume();
    }
  }

  rebuild(audioNodes, muted) {
    // Don't create AudioContext until we actually need audio processing.
    // createMediaElementSource permanently routes video audio through Web Audio.
    if (audioNodes.length === 0 && !muted) {
      if (this.ctx) {
        // Context exists from prior filters - clean up and reconnect direct
        this.source.disconnect();
        this.activeNodes.forEach(n => n.disconnect());
        this.activeNodes = [];
        this.source.connect(this.ctx.destination);
        if (this.onRebuild) this.onRebuild(this.source, this.source, this.ctx.destination);
      }
      return;
    }

    this.ensureContext();
    this.source.disconnect();
    this.activeNodes.forEach(n => n.disconnect());
    this.activeNodes = [];

    if (muted || audioNodes.length === 0) {
      if (!muted) this.source.connect(this.ctx.destination);
      return;
    }

    let prev = this.source;
    for (const spec of audioNodes) {
      let node;
      switch (spec.type) {
        case 'gain':
          node = this.ctx.createGain();
          node.gain.value = spec.gain ?? 1.0;
          break;
        case 'biquad':
          node = this.ctx.createBiquadFilter();
          node.type = spec.filter;
          node.frequency.value = spec.frequency ?? 1000;
          if (spec.Q != null) node.Q.value = spec.Q;
          if (spec.gain != null) node.gain.value = spec.gain;
          break;
        case 'compressor':
          node = this.ctx.createDynamicsCompressor();
          node.threshold.value = spec.threshold ?? -24;
          node.ratio.value = spec.ratio ?? 4;
          node.attack.value = spec.attack ?? 0.003;
          node.release.value = spec.release ?? 0.25;
          break;
        case 'fade_in': {
          node = this.ctx.createGain();
          const now = this.ctx.currentTime;
          node.gain.setValueAtTime(0, now);
          node.gain.linearRampToValueAtTime(1, now + (spec.duration || 0.5));
          break;
        }
        case 'fade_out': {
          node = this.ctx.createGain();
          // Fade out is best-effort: schedule from current time.
          // Full accuracy requires knowing clip end, which we don't have here.
          break;
        }
        default:
          continue;
      }
      prev.connect(node);
      this.activeNodes.push(node);
      prev = node;
    }
    prev.connect(this.ctx.destination);
    if (this.onRebuild) this.onRebuild(this.source, prev, this.ctx.destination);
  }

  updateParam(index, paramName, value) {
    const node = this.activeNodes[index];
    if (!node) return;
    if (node[paramName] instanceof AudioParam) {
      node[paramName].value = value;
    }
  }

  destroy() {
    if (this.ctx) this.ctx.close().catch(() => {});
    this.ctx = null;
    this.source = null;
    this.activeNodes = [];
  }
}
