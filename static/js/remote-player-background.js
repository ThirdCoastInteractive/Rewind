/* Remote player WebGL background (Perlin Nebula)

   Goals:
   - Zero UI controls (OBS-friendly)
   - Safe fallback if WebGL unsupported
   - Pause when tab is hidden
*/

import { clampNumber, parseAspectRatio } from './lib/utils.js';

(function () {
  const canvasId = 'remote-player-bg-canvas';
  const sceneElId = 'player-scene';
  const videoFrameId = 'remote-player-video-frame';

  // Producer preview and remote composition share the same "stage" aspect.
  // Default stays 16:9 if the scene doesn't specify otherwise.
  const defaultStageAspect = 16 / 9;

  const defaultNebulaConfig = {
    speed: 1.0,
    seed: 0.0,
    tint: [1.0, 1.0, 1.0],
  };

  function getCanvas() {
    return document.getElementById(canvasId);
  }

  function getSceneEl() {
    return document.getElementById(sceneElId);
  }

  function getVideoFrameEl() {
    return document.getElementById(videoFrameId);
  }

  function safeParseScene() {
    const el = getSceneEl();
    if (!el || !el.dataset) return null;
    const b64 = el.dataset.sceneB64;
    if (!b64) return null;
    try {
      const json = atob(b64);
      return JSON.parse(json);
    } catch {
      return null;
    }
  }

  function parseHexTint(str) {
    if (typeof str !== 'string') return null;
    let s = str.trim();
    if (s.startsWith('#')) s = s.slice(1);
    if (s.length === 3) {
      s = s
        .split('')
        .map((c) => c + c)
        .join('');
    }
    if (!/^[0-9a-fA-F]{6}$/.test(s)) return null;
    const r = parseInt(s.slice(0, 2), 16) / 255;
    const g = parseInt(s.slice(2, 4), 16) / 255;
    const b = parseInt(s.slice(4, 6), 16) / 255;
    return [r, g, b];
  }

  function parseTint(value) {
    // Accept: "#RRGGBB" | "RRGGBB" | [r,g,b] (0..1 or 0..255)
    const fromHex = parseHexTint(value);
    if (fromHex) return fromHex;

    if (Array.isArray(value) && value.length >= 3) {
      const r = Number(value[0]);
      const g = Number(value[1]);
      const b = Number(value[2]);
      if (![r, g, b].every(Number.isFinite)) return null;

      // Heuristic: if any channel > 1, treat as 0..255.
      const scale = r > 1 || g > 1 || b > 1 ? 255 : 1;
      return [
        Math.max(0, Math.min(1, r / scale)),
        Math.max(0, Math.min(1, g / scale)),
        Math.max(0, Math.min(1, b / scale)),
      ];
    }

    return null;
  }

  // Prefer the browser's color engine for OKLCH -> sRGB.
  // This avoids hand-rolled math drift and keeps the producer/remote consistent.
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

    const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
    const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
    const s_ = L - 0.0894841775 * a - 1.2914855480 * b;

    const l3 = l_ * l_ * l_;
    const m3 = m_ * m_ * m_;
    const s3 = s_ * s_ * s_;

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

  function extractNebulaConfig(scene) {
    const bg = scene && scene.background ? scene.background : null;
    const speed = clampNumber(bg && bg.speed, 0.0, 10.0, defaultNebulaConfig.speed);
    const seed = clampNumber(bg && bg.seed, -1e6, 1e6, defaultNebulaConfig.seed);

    let tint = null;
    if (bg && bg.tint_oklch && typeof bg.tint_oklch === 'object') {
      const l = bg.tint_oklch.l;
      const c = bg.tint_oklch.c;
      const h = bg.tint_oklch.h;
      tint = oklchToSRGB(l, c, h);
    }
    if (!tint) {
      tint = parseTint(bg && bg.tint);
    }

    const epochMs = clampNumber(bg && bg.epoch_ms, 0, 9e15, 0);
    return {
      speed,
      seed,
      tint: tint || defaultNebulaConfig.tint,
      epochMs,
    };
  }

  const defaultVideoFrameConfig = {
    x: 0.5,
    y: 0.5,
    width: 0.9,
    height: 0.9,
    aspect: '',
    border: {
      enabled: true,
      size: 2,
      opacity: 0.1,
    },
  };

  function extractStageAspect(scene) {
    const s = scene && scene.stage ? scene.stage : null;
    const r = parseAspectRatio(s && s.aspect);
    return r || defaultStageAspect;
  }

  function enforceAspectOnSize(width, height, ratio, stageAspect) {
    const minDim = 0.1;
    const maxDim = 1.0;

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

  function getStageRect(stageAspect) {
    const vw = Math.max(1, window.innerWidth || 1);
    const vh = Math.max(1, window.innerHeight || 1);
    const viewportAspect = vw / vh;

    if (viewportAspect >= stageAspect) {
      // viewport wider than stage -> pillarbox
      const h = vh;
      const w = h * stageAspect;
      const left = (vw - w) / 2;
      return { left, top: 0, width: w, height: h };
    }

    // viewport taller than stage -> letterbox
    const w = vw;
    const h = w / stageAspect;
    const top = (vh - h) / 2;
    return { left: 0, top, width: w, height: h };
  }

  function extractVideoFrameConfig(scene) {
    const v = scene && scene.video ? scene.video : null;
    const border = v && v.border ? v.border : null;
    return {
      x: clampNumber(v && v.x, 0, 1, defaultVideoFrameConfig.x),
      y: clampNumber(v && v.y, 0, 1, defaultVideoFrameConfig.y),
      width: clampNumber(v && (v.width ?? v.w), 0.05, 1.0, defaultVideoFrameConfig.width),
      height: clampNumber(v && (v.height ?? v.h), 0.05, 1.0, defaultVideoFrameConfig.height),
      aspect: typeof (v && v.aspect) === 'string' ? v.aspect : defaultVideoFrameConfig.aspect,
      border: {
        enabled: !!(border && typeof border.enabled !== 'undefined' ? border.enabled : defaultVideoFrameConfig.border.enabled),
        size: clampNumber(border && border.size, 0, 50, defaultVideoFrameConfig.border.size),
        opacity: clampNumber(border && border.opacity, 0, 1, defaultVideoFrameConfig.border.opacity),
      },
    };
  }

  function applyVideoFrame(scene) {
    const el = getVideoFrameEl();
    if (!el) return;

    const cfg = extractVideoFrameConfig(scene);

    const stageAspect = extractStageAspect(scene);

    const stage = getStageRect(stageAspect);
    let w = cfg.width;
    let h = cfg.height;

    const ratio = parseAspectRatio(cfg.aspect);
    if (ratio) {
      const sized = enforceAspectOnSize(w, h, ratio, stageAspect);
      w = sized.width;
      h = sized.height;
    }

    const leftPx = stage.left + cfg.x * stage.width;
    const topPx = stage.top + cfg.y * stage.height;
    const widthPx = w * stage.width;
    const heightPx = h * stage.height;

    el.style.left = `${leftPx.toFixed(1)}px`;
    el.style.top = `${topPx.toFixed(1)}px`;
    el.style.width = `${widthPx.toFixed(1)}px`;
    el.style.height = `${heightPx.toFixed(1)}px`;
    el.style.transform = 'translate(-50%, -50%)';

    if (cfg.border.enabled && cfg.border.size > 0 && cfg.border.opacity > 0) {
      el.style.borderStyle = 'solid';
      el.style.borderWidth = Math.round(cfg.border.size) + 'px';
      el.style.borderColor = `rgba(255,255,255,${cfg.border.opacity})`;
    } else {
      el.style.borderWidth = '0px';
    }
  }

  function createShader(gl, type, source) {
    const shader = gl.createShader(type);
    if (!shader) return null;
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      // Avoid noisy logs in production; fail gracefully.
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

  // Perlin-ish gradient noise + fBm.
  // Intentionally monochrome to match the current remote-player look.
  const fragmentShader = `
    precision highp float;

    varying vec2 v_uv;
    uniform vec2 u_resolution;
    uniform float u_time;
    uniform float u_seed;
    uniform vec3 u_tint;

    // Hash-based gradient noise (2D)
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

      // Quintic fade
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

      // Normalize to preserve aspect
      float aspect = u_resolution.x / max(1.0, u_resolution.y);
      vec2 p = (uv - 0.5) * vec2(aspect, 1.0);

      // Seed sync: shift the domain deterministically.
      // Keep the scale small so large seeds stay well-behaved.
      p += vec2(u_seed * 0.013, u_seed * 0.021);

      // Subtle, slow animation
      float t = u_time * 0.06;
      vec2 drift = vec2(0.18 * t, -0.11 * t);

      // Domain warp for a more "nebula" feel
      float w1 = fbm(p * 2.3 + drift);
      float w2 = fbm(p * 3.7 - drift * 1.3);
      vec2 warp = vec2(w1, w2) * 0.55;

      float n = fbm(p * 3.0 + warp + drift);

      // Contrast curve
      n = 0.5 + 0.5 * n;
      n = smoothstep(0.15, 0.95, n);

      // Vignette
      float r = length(p);
      float vignette = smoothstep(1.1, 0.25, r);

      float intensity = (n * 0.75 * 0.8) * vignette;

      // Output tint on black.
      vec3 col = u_tint * intensity;
      gl_FragColor = vec4(col, 1.0);
    }
  `;

  function createRenderer(initialConfig) {
    const canvas = getCanvas();
    if (!canvas) return null;

    const gl = canvas.getContext('webgl', {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      premultipliedAlpha: false,
      preserveDrawingBuffer: false,
      powerPreference: 'high-performance',
    });
    if (!gl) return null;

    const program = createProgram(gl, vertexShader, fragmentShader);
    if (!program) return null;

    const positionLoc = gl.getAttribLocation(program, 'a_position');
    const resolutionLoc = gl.getUniformLocation(program, 'u_resolution');
    const timeLoc = gl.getUniformLocation(program, 'u_time');
    const seedLoc = gl.getUniformLocation(program, 'u_seed');
    const tintLoc = gl.getUniformLocation(program, 'u_tint');

    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([
        -1, -1,
        1, -1,
        -1, 1,
        1, 1,
      ]),
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

    let rafId = 0;

    // Fallback if producer has not set an epoch_ms yet.
    let localEpochMs = Date.now();

    let config = {
      speed: (initialConfig && initialConfig.speed) || defaultNebulaConfig.speed,
      seed: (initialConfig && initialConfig.seed) || defaultNebulaConfig.seed,
      tint: (initialConfig && initialConfig.tint) || defaultNebulaConfig.tint,
      epochMs: (initialConfig && initialConfig.epochMs) || 0,
    };

    function frame(now) {
      rafId = requestAnimationFrame(frame);
      if (document.visibilityState === 'hidden') return;

      resize();

      gl.useProgram(program);

      gl.enableVertexAttribArray(positionLoc);
      gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
      gl.vertexAttribPointer(positionLoc, 2, gl.FLOAT, false, 0, 0);

      gl.uniform2f(resolutionLoc, canvas.width, canvas.height);
      const epoch = config.epochMs && config.epochMs > 0 ? config.epochMs : localEpochMs;
      gl.uniform1f(timeLoc, ((Date.now() - epoch) / 1000.0) * (config.speed || 0.0));
      gl.uniform1f(seedLoc, config.seed || 0.0);
      gl.uniform3f(tintLoc, config.tint[0], config.tint[1], config.tint[2]);

      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }

    window.addEventListener('resize', resize, { passive: true });
    document.addEventListener('visibilitychange', () => {
      // If the tab becomes visible after a long sleep, reset the fallback epoch.
      if (document.visibilityState === 'visible') {
        localEpochMs = Date.now();
      }
    });

    resize();
    rafId = requestAnimationFrame(frame);

    // Expose a tiny hook for future use (e.g., other shaders).
    return {
      setConfig: (next) => {
        if (!next) return;
        config = {
          speed: clampNumber(next.speed, 0.0, 10.0, defaultNebulaConfig.speed),
          seed: clampNumber(next.seed, -1e6, 1e6, defaultNebulaConfig.seed),
          tint: Array.isArray(next.tint) && next.tint.length >= 3 ? next.tint : defaultNebulaConfig.tint,
          epochMs: clampNumber(next.epochMs, 0, 9e15, 0),
        };
      },
      stop: () => {
        if (rafId) cancelAnimationFrame(rafId);
        rafId = 0;
      },
    };
  }

  let renderer = null;

  function setEnabled(enabled) {
    const canvas = getCanvas();
    if (!canvas) return;

    if (!enabled) {
      canvas.style.display = 'none';
      if (renderer) {
        renderer.stop();
        renderer = null;
      }
      return;
    }

    canvas.style.display = '';
    if (!renderer) {
      renderer = createRenderer(extractNebulaConfig(safeParseScene()));
    }
  }

  function applyScene(scene) {
    const mode = scene && scene.background && scene.background.mode ? String(scene.background.mode) : 'perlin-nebula';
    setEnabled(mode !== 'none');

    if (mode !== 'none' && renderer) {
      renderer.setConfig(extractNebulaConfig(scene));
    }

    applyVideoFrame(scene);
  }

  function init() {
    applyScene(safeParseScene());

    let sceneEl = null;
    let sceneAttrObserver = null;

    function attachSceneAttrObserver() {
      const next = getSceneEl();
      if (next === sceneEl) return;

      if (sceneAttrObserver) sceneAttrObserver.disconnect();
      sceneAttrObserver = null;
      sceneEl = next;

      if (!sceneEl) return;
      sceneAttrObserver = new MutationObserver(() => {
        applyScene(safeParseScene());
      });
      sceneAttrObserver.observe(sceneEl, { attributes: true, attributeFilter: ['data-scene-b64'] });
    }

    attachSceneAttrObserver();

    // DataStar may replace the entire `#player-scene` node on patch.
    // Observe the subtree so we can re-bind when that happens.
    const root = document.body || document.documentElement;
    if (root) {
      const swapObserver = new MutationObserver(() => {
        attachSceneAttrObserver();
        applyScene(safeParseScene());
      });
      swapObserver.observe(root, { childList: true, subtree: true });
    }

    // Minimal debug hook.
    window.__rewindRemotePlayerBg = {
      setEnabled,
    };
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
