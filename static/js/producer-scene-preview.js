/* Producer scene live preview (WebGL)

   - Shows a local preview of the current scene controls
   - Uses OKLCH sliders and converts to sRGB for the shader
   - Does NOT broadcast; broadcast happens via POST handlers
*/

import { clampNumber, parseAspectRatio } from './lib/utils.js';

(function () {
  const canvasId = 'producer-scene-preview-canvas';
  const currentSceneElId = 'producer-current-scene';
  const stageElId = 'producer-preview-stage';
  const videoRectId = 'producer-video-frame-rect';
  const videoHandleId = 'producer-video-frame-handle';
  const videoReadoutId = 'producer-video-frame-readout';

  // Producer preview and remote composition share the same "stage" aspect.
  // Stored as a string ("16:9"), interpreted as width:height.
  let stageAspectState = '16:9';

  function getCanvas() {
    return document.getElementById(canvasId);
  }

  function getEl(id) {
    return document.getElementById(id);
  }

  function getStageEl() {
    return getEl(stageElId);
  }

  function getCurrentSceneEl() {
    return getEl(currentSceneElId);
  }

  let videoFrameState = {
    x: 0.5,
    y: 0.5,
    width: 0.9,
    height: 0.9,
  };

  // "" means free; otherwise a string like "16:9".
  let videoAspectState = '';

  // Shared epoch for preview animation + remote sync (when live applying).
  let epochMs = Date.now();

  function getStageAspectRatio() {
    return parseAspectRatio(stageAspectState) || 16 / 9;
  }

  function applyStageAspectToPreview() {
    const el = getStageEl();
    if (!el) return;

    const m = typeof stageAspectState === 'string' ? stageAspectState.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/) : null;
    if (!m) {
      el.style.aspectRatio = '16 / 9';
      return;
    }
    const a = clampNumber(m[1], 0.001, 1000, 16);
    const b = clampNumber(m[2], 0.001, 1000, 9);
    el.style.aspectRatio = `${a} / ${b}`;
  }

  function enforceAspectOnSize(width, height, ratio) {
    const minDim = 0.1;
    const maxDim = 1.0;

    const stageAspect = getStageAspectRatio();

    // Want pixel AR == ratio. In stage fractions:
    // (width/height)*stageAspect == ratio.
    const hFromW = (width * stageAspect) / ratio;
    const wFromH = (height * ratio) / stageAspect;

    let w = width;
    let h = hFromW;
    if (Number.isFinite(hFromW) && Number.isFinite(wFromH)) {
      const dKeepW = Math.abs(hFromW - height);
      const dKeepH = Math.abs(wFromH - width);
      if (dKeepH < dKeepW) {
        w = wFromH;
        h = height;
      }
    }

    if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) {
      return { width, height };
    }

    // Fit to bounds while preserving aspect.
    const down = Math.min(1, maxDim / w, maxDim / h);
    w *= down;
    h *= down;

    const up = Math.max(1, minDim / w, minDim / h);
    w *= up;
    h *= up;

    const down2 = Math.min(1, maxDim / w, maxDim / h);
    w *= down2;
    h *= down2;

    return { width: w, height: h };
  }

  function clampVideoFrameToBounds(next) {
    let w = clampNumber(next.width, 0.1, 1, 0.9);
    let h = clampNumber(next.height, 0.1, 1, 0.9);

    const ratio = parseAspectRatio(videoAspectState);
    if (ratio) {
      const sized = enforceAspectOnSize(w, h, ratio);
      w = sized.width;
      h = sized.height;
    }

    const x = clampNumber(next.x, w / 2, 1 - w / 2, 0.5);
    const y = clampNumber(next.y, h / 2, 1 - h / 2, 0.5);
    return { x, y, width: w, height: h };
  }

  function setVideoFrameState(next) {
    videoFrameState = clampVideoFrameToBounds({
      x: typeof next.x !== 'undefined' ? next.x : videoFrameState.x,
      y: typeof next.y !== 'undefined' ? next.y : videoFrameState.y,
      width: typeof next.width !== 'undefined' ? next.width : videoFrameState.width,
      height: typeof next.height !== 'undefined' ? next.height : videoFrameState.height,
    });
  }

  function updateVideoReadout(cfg) {
    const el = getEl(videoReadoutId);
    if (!el) return;

    const xPct = (cfg.video.x * 100).toFixed(1);
    const yPct = (cfg.video.y * 100).toFixed(1);
    const wPct = (cfg.video.width * 100).toFixed(1);
    const hPct = (cfg.video.height * 100).toFixed(1);
    const ar = cfg.video.height > 0 ? cfg.video.width / cfg.video.height : 0;
    const arText = Number.isFinite(ar) && ar > 0 ? ar.toFixed(3) : '—';

    const fixed = typeof cfg.video.aspect === 'string' && cfg.video.aspect.trim() !== '' ? cfg.video.aspect.trim() : '';
    el.textContent = fixed
      ? `X ${xPct}%  Y ${yPct}%  W ${wPct}%  H ${hPct}%  FIX ${fixed}  AR ${arText}`
      : `X ${xPct}%  Y ${yPct}%  W ${wPct}%  H ${hPct}%  AR ${arText}`;
  }

  let __oklchProbeEl = null;

  function parseComputedColorToRGB01(colorStr) {
    if (typeof colorStr !== 'string') return null;

    const rgbMatch = colorStr.match(/rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)/i);
    if (rgbMatch) {
      const r = clampNumber(rgbMatch[1], 0, 255, 255) / 255;
      const g = clampNumber(rgbMatch[2], 0, 255, 255) / 255;
      const b = clampNumber(rgbMatch[3], 0, 255, 255) / 255;
      return [r, g, b];
    }

    const srgbMatch = colorStr.match(/color\(\s*srgb\s+([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)/i);
    if (srgbMatch) {
      const r = clampNumber(srgbMatch[1], 0, 1, 1);
      const g = clampNumber(srgbMatch[2], 0, 1, 1);
      const b = clampNumber(srgbMatch[3], 0, 1, 1);
      return [r, g, b];
    }

    return null;
  }

  function ensureOKLCHProbeEl() {
    if (__oklchProbeEl) return __oklchProbeEl;
    if (!document.body) return null;
    const el = document.createElement('span');
    el.style.position = 'absolute';
    el.style.left = '-9999px';
    el.style.top = '-9999px';
    el.style.visibility = 'hidden';
    document.body.appendChild(el);
    __oklchProbeEl = el;
    return el;
  }

  function oklchToSRGBViaCSS(l, c, hDeg) {
    try {
      if (!window.CSS || typeof CSS.supports !== 'function') return null;
      if (!CSS.supports('color', 'oklch(50% 0.1 120)')) return null;

      const probe = ensureOKLCHProbeEl();
      if (!probe) return null;

      const Lpct = clampNumber(l, 0, 1, 1) * 100;
      const C = clampNumber(c, 0, 1, 0);
      const H = clampNumber(hDeg, 0, 360, 0);
      probe.style.color = `oklch(${Lpct}% ${C} ${H})`;
      return parseComputedColorToRGB01(getComputedStyle(probe).color);
    } catch {
      return null;
    }
  }

  function oklchToSRGBFallback(l, c, hDeg) {
    const L = clampNumber(l, 0, 1, 1);
    const C = clampNumber(c, 0, 1, 0);
    const h = (clampNumber(hDeg, 0, 360, 0) * Math.PI) / 180;

    const a = C * Math.cos(h);
    const b = C * Math.sin(h);

    // OKLab -> LMS
    const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
    const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
    const s_ = L - 0.0894841775 * a - 1.2914855480 * b;

    const l3 = l_ * l_ * l_;
    const m3 = m_ * m_ * m_;
    const s3 = s_ * s_ * s_;

    // LMS -> linear sRGB
    let rLin = +4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3;
    let gLin = -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3;
    let bLin = -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3;

    const toSrgb = (v) => {
      v = Math.max(0, Math.min(1, v));
      if (v <= 0.0031308) return 12.92 * v;
      return 1.055 * Math.pow(v, 1 / 2.4) - 0.055;
    };

    return [toSrgb(rLin), toSrgb(gLin), toSrgb(bLin)];
  }

  function oklchToSRGB(l, c, hDeg) {
    return oklchToSRGBViaCSS(l, c, hDeg) || oklchToSRGBFallback(l, c, hDeg);
  }

  function readControls() {
    const mode = (getEl('scene-background-mode')?.value || 'perlin-nebula').trim();

    const speed = clampNumber(getEl('scene-speed')?.value, 0, 10, 1);
    const seed = clampNumber(getEl('scene-seed')?.value, -1000000, 1000000, 0);

    const borderEnabled = !!getEl('scene-video-border-enabled')?.checked;
    const borderSize = clampNumber(getEl('scene-video-border-size')?.value, 0, 50, 2);
    const borderOpacity = clampNumber(getEl('scene-video-border-opacity')?.value, 0, 1, 0.1);

    const l = clampNumber(getEl('scene-oklch-l')?.value, 0, 1, 1);
    const c = clampNumber(getEl('scene-oklch-c')?.value, 0, 1, 0);
    const h = clampNumber(getEl('scene-oklch-h')?.value, 0, 360, 0);

    return {
      stage: {
        aspect: stageAspectState,
      },
      mode,
      speed,
      seed,
      oklch: { l, c, h },
      video: {
        x: videoFrameState.x,
        y: videoFrameState.y,
        width: videoFrameState.width,
        height: videoFrameState.height,
        aspect: videoAspectState,
        border: { enabled: borderEnabled, size: borderSize, opacity: borderOpacity },
      },
    };
  }

  function safeParseSceneB64(b64) {
    if (!b64 || typeof b64 !== 'string') return null;
    try {
      return JSON.parse(atob(b64));
    } catch {
      return null;
    }
  }

  function setControlValue(id, value) {
    const el = getEl(id);
    if (!el) return;
    el.value = String(value);
    // Nudge any listeners (if added later).
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  }

  function loadSceneIntoControls(scene, presetName) {
    const stage = scene && scene.stage ? scene.stage : null;
    if (stage && typeof stage.aspect === 'string' && stage.aspect.trim() !== '') {
      stageAspectState = stage.aspect.trim();
    } else {
      stageAspectState = '16:9';
    }
    applyStageAspectToPreview();

    const bg = scene && scene.background ? scene.background : null;
    const mode = bg && bg.mode ? String(bg.mode) : 'perlin-nebula';
    const speed = bg && typeof bg.speed !== 'undefined' ? bg.speed : 1;
    const seed = bg && typeof bg.seed !== 'undefined' ? bg.seed : 0;

    setControlValue('scene-background-mode', mode);
    setControlValue('scene-speed', clampNumber(speed, 0, 10, 1));
    setControlValue('scene-seed', clampNumber(seed, -1000000, 1000000, 0));

    if (bg && bg.tint_oklch && typeof bg.tint_oklch === 'object') {
      const l = clampNumber(bg.tint_oklch.l, 0, 1, 1);
      const c = clampNumber(bg.tint_oklch.c, 0, 1, 0);
      const h = clampNumber(bg.tint_oklch.h, 0, 360, 0);
      setControlValue('scene-oklch-l', l);
      setControlValue('scene-oklch-c', c);
      setControlValue('scene-oklch-h', h);
    }

    const v = scene && scene.video ? scene.video : null;
    const vb = v && v.border ? v.border : null;
    if (v) {
      videoAspectState = typeof v.aspect === 'string' ? v.aspect : '';
      setVideoFrameState({
        x: clampNumber(v.x, 0, 1, 0.5),
        y: clampNumber(v.y, 0, 1, 0.5),
        width: clampNumber(v.width ?? v.w, 0.1, 1, 0.9),
        height: clampNumber(v.height ?? v.h, 0.1, 1, 0.9),
      });

      const chk = getEl('scene-video-border-enabled');
      if (chk) {
        chk.checked = vb && typeof vb.enabled !== 'undefined' ? !!vb.enabled : true;
      }
      setControlValue('scene-video-border-size', clampNumber(vb && vb.size, 0, 50, 2));
      setControlValue('scene-video-border-opacity', clampNumber(vb && vb.opacity, 0, 1, 0.1));
    }

    if (typeof presetName === 'string' && presetName.trim() !== '') {
      setControlValue('scene-preset-name', presetName.trim());
    }
  }

  function renderVideoRect(cfg) {
    const rect = getEl(videoRectId);
    if (!rect) return;

    rect.style.left = (cfg.video.x * 100).toFixed(3) + '%';
    rect.style.top = (cfg.video.y * 100).toFixed(3) + '%';
    rect.style.width = (cfg.video.width * 100).toFixed(3) + '%';
    rect.style.height = (cfg.video.height * 100).toFixed(3) + '%';
    rect.style.transform = 'translate(-50%, -50%)';

    updateVideoReadout(cfg);

    const b = cfg.video.border;
    if (b && b.enabled && b.size > 0 && b.opacity > 0) {
      rect.style.borderStyle = 'solid';
      rect.style.borderWidth = Math.round(b.size) + 'px';
      rect.style.borderColor = `rgba(255,255,255,${b.opacity})`;
    } else {
      rect.style.borderWidth = '0px';
    }
  }

  function syncForms(cfg) {
    const set = (id, v) => {
      const el = getEl(id);
      if (!el) return;
      el.value = String(v);
    };

    set('scene-apply-background-mode', cfg.mode);
    set('scene-apply-stage-aspect', cfg.stage && typeof cfg.stage.aspect === 'string' ? cfg.stage.aspect : stageAspectState);
    set('scene-apply-speed', cfg.speed);
    set('scene-apply-seed', cfg.seed);
    set('scene-apply-oklch-l', cfg.oklch.l);
    set('scene-apply-oklch-c', cfg.oklch.c);
    set('scene-apply-oklch-h', cfg.oklch.h);
    set('scene-apply-epoch-ms', epochMs);

    if (cfg.video) {
      // Apply form hidden inputs
      set('scene-apply-video-x', cfg.video.x);
      set('scene-apply-video-y', cfg.video.y);
      set('scene-apply-video-w', cfg.video.width);
      set('scene-apply-video-h', cfg.video.height);
      set('scene-apply-video-aspect', cfg.video.aspect || '');
      set('scene-apply-video-border-enabled', cfg.video.border && cfg.video.border.enabled ? 1 : 0);
      set('scene-apply-video-border-size', cfg.video.border ? cfg.video.border.size : 0);
      set('scene-apply-video-border-opacity', cfg.video.border ? cfg.video.border.opacity : 0);

      // Save preset form hidden inputs
      set('scene-save-stage-aspect', cfg.stage && typeof cfg.stage.aspect === 'string' ? cfg.stage.aspect : stageAspectState);
      set('scene-save-video-x', cfg.video.x);
      set('scene-save-video-y', cfg.video.y);
      set('scene-save-video-w', cfg.video.width);
      set('scene-save-video-h', cfg.video.height);
      set('scene-save-video-aspect', cfg.video.aspect || '');
    }
  }

  function createShader(gl, type, source) {
    const shader = gl.createShader(type);
    if (!shader) return null;
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      return null;
    }
    return shader;
  }

  function createProgram(gl, vertexSource, fragmentSource) {
    const vs = createShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fs = createShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vs || !fs) return null;

    const program = gl.createProgram();
    if (!program) return null;

    gl.attachShader(program, vs);
    gl.attachShader(program, fs);
    gl.linkProgram(program);

    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      return null;
    }

    return program;
  }

  const vertexShader = `
    attribute vec2 a_position;
    varying vec2 v_uv;
    void main() {
      v_uv = a_position * 0.5 + 0.5;
      gl_Position = vec4(a_position, 0.0, 1.0);
    }
  `;

  const fragmentShader = `
    precision highp float;

    varying vec2 v_uv;
    uniform vec2 u_resolution;
    uniform float u_time;
    uniform float u_seed;
    uniform vec3 u_tint;

    float hash12(vec2 p) {
      vec3 p3 = fract(vec3(p.xyx) * 0.1031);
      p3 += dot(p3, p3.yzx + 33.33);
      return fract((p3.x + p3.y) * p3.z);
    }

    vec2 hash22(vec2 p) {
      float n = hash12(p);
      float m = hash12(p + 19.19);
      return vec2(n, m) * 2.0 - 1.0;
    }

    float gradNoise(vec2 p) {
      vec2 i = floor(p);
      vec2 f = fract(p);
      vec2 u = f * f * f * (f * (f * 6.0 - 15.0) + 10.0);

      vec2 g00 = normalize(hash22(i + vec2(0.0, 0.0)));
      vec2 g10 = normalize(hash22(i + vec2(1.0, 0.0)));
      vec2 g01 = normalize(hash22(i + vec2(0.0, 1.0)));
      vec2 g11 = normalize(hash22(i + vec2(1.0, 1.0)));

      float n00 = dot(g00, f - vec2(0.0, 0.0));
      float n10 = dot(g10, f - vec2(1.0, 0.0));
      float n01 = dot(g01, f - vec2(0.0, 1.0));
      float n11 = dot(g11, f - vec2(1.0, 1.0));

      float nx0 = mix(n00, n10, u.x);
      float nx1 = mix(n01, n11, u.x);
      return mix(nx0, nx1, u.y);
    }

    float fbm(vec2 p) {
      float sum = 0.0;
      float amp = 0.55;
      float freq = 1.0;
      for (int i = 0; i < 6; i++) {
        sum += amp * gradNoise(p * freq);
        freq *= 2.0;
        amp *= 0.5;
      }
      return sum;
    }

    void main() {
      vec2 uv = v_uv;

      float aspect = u_resolution.x / max(1.0, u_resolution.y);
      vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
      p += vec2(u_seed * 0.013, u_seed * 0.021);

      float t = u_time * 0.06;
      vec2 drift = vec2(0.18 * t, -0.11 * t);

      float w1 = fbm(p * 2.3 + drift);
      float w2 = fbm(p * 3.7 - drift * 1.3);
      vec2 warp = vec2(w1, w2) * 0.55;

      float n = fbm(p * 3.0 + warp + drift);

      n = 0.5 + 0.5 * n;
      n = smoothstep(0.15, 0.95, n);

      float r = length(p);
      float vignette = smoothstep(1.1, 0.25, r);

      float intensity = (n * 0.75 * 0.8) * vignette;

      vec3 col = u_tint * intensity;
      gl_FragColor = vec4(col, 1.0);
    }
  `;

  function init() {
    const canvas = getCanvas();
    if (!canvas) return;

    const rectEl = getEl(videoRectId);
    const handleEl = getEl(videoHandleId);

    const gl = canvas.getContext('webgl', {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      premultipliedAlpha: false,
      preserveDrawingBuffer: false,
      powerPreference: 'high-performance',
    });
    if (!gl) return;

    const program = createProgram(gl, vertexShader, fragmentShader);
    if (!program) return;

    const positionLoc = gl.getAttribLocation(program, 'a_position');
    const resolutionLoc = gl.getUniformLocation(program, 'u_resolution');
    const timeLoc = gl.getUniformLocation(program, 'u_time');
    const seedLoc = gl.getUniformLocation(program, 'u_seed');
    const tintLoc = gl.getUniformLocation(program, 'u_tint');

    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]),
      gl.STATIC_DRAW
    );

    function resize() {
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      const w = Math.max(1, Math.floor(canvas.clientWidth * dpr));
      const h = Math.max(1, Math.floor(canvas.clientHeight * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
        gl.viewport(0, 0, w, h);
      }
    }

    epochMs = Date.now();

    const applyForm = getEl('scene-apply-form');
    let liveApplyTimer = 0;
    let liveApplyInFlight = false;
    let liveApplyQueued = false;

    const queueLiveApply = () => {
      if (!applyForm) return;
      if (liveApplyTimer) window.clearTimeout(liveApplyTimer);
      liveApplyTimer = window.setTimeout(() => {
        liveApplyTimer = 0;
        void submitLiveApply();
      }, 150);
    };

    const submitLiveApply = async () => {
      if (!applyForm) return;
      if (liveApplyInFlight) {
        liveApplyQueued = true;
        return;
      }

      liveApplyInFlight = true;
      liveApplyQueued = false;
      try {
        const body = new FormData(applyForm);
        await fetch(applyForm.action, {
          method: 'POST',
          body,
          credentials: 'same-origin',
          redirect: 'manual',
        });
      } catch {
        // ignore network errors for live updates
      } finally {
        liveApplyInFlight = false;
        if (liveApplyQueued) {
          queueLiveApply();
        }
      }
    };

    // Load the currently-applied session scene into controls (if server provided it).
    // This makes the preview immediately reflect reality.
    applyStageAspectToPreview();
    const currentEl = getCurrentSceneEl();
    if (currentEl && currentEl.dataset && currentEl.dataset.sceneB64) {
      const scene = safeParseSceneB64(currentEl.dataset.sceneB64);
      if (scene) {
        loadSceneIntoControls(scene, '');
        epochMs = Date.now();
      }
    }

    function apply() {
      const cfg = readControls();
      const rgb = oklchToSRGB(cfg.oklch.l, cfg.oklch.c, cfg.oklch.h);

      syncForms(cfg);
      renderVideoRect(cfg);

      // Show/hide based on mode.
      canvas.style.display = cfg.mode === 'none' ? 'none' : '';

      return { cfg, rgb };
    }

    let rafId = 0;

    function frame() {
      rafId = requestAnimationFrame(frame);
      if (document.visibilityState === 'hidden') return;

      resize();

      const { cfg, rgb } = apply();
      if (cfg.mode === 'none') return;

      gl.useProgram(program);

      gl.enableVertexAttribArray(positionLoc);
      gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
      gl.vertexAttribPointer(positionLoc, 2, gl.FLOAT, false, 0, 0);

      gl.uniform2f(resolutionLoc, canvas.width, canvas.height);
      gl.uniform1f(timeLoc, ((Date.now() - epochMs) / 1000.0) * cfg.speed);
      gl.uniform1f(seedLoc, cfg.seed);
      gl.uniform3f(tintLoc, rgb[0], rgb[1], rgb[2]);

      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }

    // Reset epoch when user clicks the preview (nice for lining up motion while tuning)
    canvas.addEventListener(
      'click',
      () => {
        epochMs = Date.now();
        queueLiveApply();
      },
      { passive: true }
    );

    // Allow loading an existing preset into the slider controls for editing.
    document.addEventListener('click', (e) => {
      const t = e.target;
      if (!(t instanceof Element)) return;
      const btn = t.closest('button[data-scene-b64]');
      if (!btn) return;

      const scene = safeParseSceneB64(btn.getAttribute('data-scene-b64'));
      const name = btn.getAttribute('data-preset-name') || '';
      if (!scene) return;

      loadSceneIntoControls(scene, name);
      epochMs = Date.now();
      queueLiveApply();
    });

    // Aspect ratio shortcuts for the preview stage (this controls remote letterbox/pillarbox).
    document.addEventListener('click', (e) => {
      const target = e.target;
      if (!(target instanceof Element)) return;
      const btn = target.closest('button[data-stage-aspect]');
      if (!btn) return;

      const aspectStr = (btn.getAttribute('data-stage-aspect') || '').trim();
      if (!parseAspectRatio(aspectStr)) return;

      stageAspectState = aspectStr;
      applyStageAspectToPreview();

      // If a fixed frame aspect is locked, re-enforce it under the new stage aspect.
      if (parseAspectRatio(videoAspectState)) {
        setVideoFrameState({ width: videoFrameState.width, height: videoFrameState.height });
      }

      queueLiveApply();
      e.preventDefault();
    });

    // Aspect ratio shortcuts for the video frame rectangle.
    document.addEventListener('click', (e) => {
      const target = e.target;
      if (!(target instanceof Element)) return;
      const btn = target.closest('button[data-video-aspect]');
      if (!btn) return;

      const aspectStr = btn.getAttribute('data-video-aspect') || '';
      const m = aspectStr.match(/^\s*(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)\s*$/);
      if (!m) return;
      const a = clampNumber(m[1], 0.001, 1000, 16);
      const b = clampNumber(m[2], 0.001, 1000, 9);
      const ratio = a / b;
      if (!Number.isFinite(ratio) || ratio <= 0) return;

      // Lock an absolute aspect ratio and re-fit the current size to match.
      videoAspectState = aspectStr;
      setVideoFrameState({ width: videoFrameState.width, height: videoFrameState.height });

      queueLiveApply();
      e.preventDefault();
    });

    // Visual editor: drag to move, drag corner handle to resize.
    if (rectEl) {
      const getEditorRect = () => {
        const editor = rectEl.parentElement;
        if (!editor) return null;
        return editor.getBoundingClientRect();
      };

      const capturePointer = (el, e) => {
        try {
          el.setPointerCapture(e.pointerId);
        } catch {
          // ignore
        }
      };

      let mode = null; // 'move' | 'resize'
      let start = null;

      const onPointerDownMove = (e) => {
        if (!(e instanceof PointerEvent)) return;
        if (handleEl && (e.target === handleEl || (e.target instanceof Element && e.target.closest('#' + videoHandleId)))) return;
        const r = getEditorRect();
        if (!r) return;
        mode = 'move';
        start = { x: e.clientX, y: e.clientY, state: { ...videoFrameState }, rect: r };
        capturePointer(rectEl, e);
        e.preventDefault();
      };

      const onPointerDownResize = (e) => {
        if (!(e instanceof PointerEvent)) return;
        const r = getEditorRect();
        if (!r) return;
        mode = 'resize';
        start = { x: e.clientX, y: e.clientY, state: { ...videoFrameState }, rect: r };
        capturePointer(handleEl || rectEl, e);
        e.preventDefault();
      };

      const onPointerMove = (e) => {
        if (!mode || !start) return;
        const r = start.rect;
        const dx = (e.clientX - start.x) / Math.max(1, r.width);
        const dy = (e.clientY - start.y) / Math.max(1, r.height);

        if (mode === 'move') {
          let nextX = start.state.x + dx;
          let nextY = start.state.y + dy;

          // Snap center to stage center lines.
          const snapPx = 10;
          const snapX = snapPx / Math.max(1, r.width);
          const snapY = snapPx / Math.max(1, r.height);
          if (Math.abs(nextX - 0.5) <= snapX) nextX = 0.5;
          if (Math.abs(nextY - 0.5) <= snapY) nextY = 0.5;

          setVideoFrameState({ x: nextX, y: nextY });
          queueLiveApply();
        } else if (mode === 'resize') {
          // Resize symmetrically around center.
          setVideoFrameState({
            width: start.state.width + dx * 2,
            height: start.state.height + dy * 2,
          });
          queueLiveApply();
        }
      };

      const onPointerUp = () => {
        mode = null;
        start = null;
      };

      rectEl.addEventListener('pointerdown', onPointerDownMove);
      rectEl.addEventListener('pointermove', onPointerMove);
      rectEl.addEventListener('pointerup', onPointerUp);
      rectEl.addEventListener('pointercancel', onPointerUp);
      if (handleEl) {
        handleEl.addEventListener('pointerdown', onPointerDownResize);
      }
    }

    const controlIds = [
      'scene-background-mode',
      'scene-speed',
      'scene-seed',
      'scene-oklch-l',
      'scene-oklch-c',
      'scene-oklch-h',
      'scene-video-border-size',
      'scene-video-border-opacity',
      'scene-video-border-enabled',
    ];

    for (const id of controlIds) {
      const el = getEl(id);
      if (!el) continue;
      el.addEventListener('input', () => {
        queueLiveApply();
      });
      el.addEventListener('change', () => {
        queueLiveApply();
      });
    }

    window.addEventListener('resize', resize, { passive: true });
    resize();
    rafId = requestAnimationFrame(frame);

    window.__rewindProducerScenePreview = {
      resetEpoch: () => {
        epochMs = Date.now();
      },
      stop: () => {
        if (rafId) cancelAnimationFrame(rafId);
        rafId = 0;
      },
    };
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
