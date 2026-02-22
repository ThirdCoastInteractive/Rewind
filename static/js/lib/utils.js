// ============================================================================
// Shared utility functions used across video-player.js and cut-page.js
// ============================================================================

export function clamp(v, min, max) {
  return Math.max(min, Math.min(max, v));
}

export function isFiniteNumber(v) {
  return typeof v === 'number' && isFinite(v);
}

export function clampNumber(value, min, max, fallback) {
  const n = Number(value);
  if (!Number.isFinite(n)) return fallback;
  return Math.max(min, Math.min(max, n));
}

export function parseAspectRatio(aspectStr) {
  if (typeof aspectStr !== 'string') return null;
  const m = aspectStr.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/);
  if (!m) return null;
  const a = clampNumber(m[1], 0.001, 1000, 0);
  const b = clampNumber(m[2], 0.001, 1000, 0);
  const r = a / b;
  if (!Number.isFinite(r) || r <= 0) return null;
  return r;
}

export function formatTime(seconds) {
  if (!isFiniteNumber(seconds) || seconds < 0) return '0:00';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export function formatTimecode(seconds) {
  if (!isFiniteNumber(seconds) || seconds < 0) return '00:00:00.000';
  const totalMs = Math.floor(seconds * 1000);
  const ms = totalMs % 1000;
  const totalSec = Math.floor(totalMs / 1000);
  const s = totalSec % 60;
  const totalMin = Math.floor(totalSec / 60);
  const m = totalMin % 60;
  const h = Math.floor(totalMin / 60);
  return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}.${ms.toString().padStart(3, '0')}`;
}

export function formatFrameTimecode(seconds, fps) {
  if (!isFiniteNumber(seconds) || seconds < 0 || !isFiniteNumber(fps) || fps <= 0) return '--:--:--:--';
  const frames = Math.floor(seconds * fps);
  const fpsInt = Math.max(1, Math.round(fps));
  const ff = frames % fpsInt;
  const totalSec = Math.floor(frames / fpsInt);
  const s = totalSec % 60;
  const totalMin = Math.floor(totalSec / 60);
  const m = totalMin % 60;
  const h = Math.floor(totalMin / 60);
  return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}:${ff.toString().padStart(2, '0')}`;
}

// ── Keybindings ──────────────────────────────────────────────────────────────

export const DEFAULT_KEYBINDINGS = {
  set_in_point: 'F14',
  set_out_point: 'F15',
  create_clip: 'F16',
  play_pause: 'F17',
  seek_back: 'F18',
  seek_forward: 'F19',
  prev_frame: 'F20',
  next_frame: 'F21',
  create_marker: 'F22',
};

export function getKeybindingsFromDOM() {
  const el = document.getElementById('rewind-keybindings');
  if (!el) return {};
  const raw = el.dataset?.keybindings;
  if (!raw) return {};
  try {
    return JSON.parse(raw) || {};
  } catch (err) {
    console.warn('Failed to parse keybindings:', err);
    return {};
  }
}

export function buildKeyMap(bindings) {
  const map = {};
  Object.entries(bindings || {}).forEach(([action, key]) => {
    if (key) {
      map[key] = action;
    }
  });
  return map;
}

// ── Color utilities ──────────────────────────────────────────────────────────

const _colorProbe = (() => {
  let el = null;
  return () => {
    if (el) return el;
    el = document.createElement('div');
    el.style.position = 'fixed';
    el.style.left = '-9999px';
    el.style.top = '-9999px';
    el.style.width = '1px';
    el.style.height = '1px';
    el.style.pointerEvents = 'none';
    document.body?.appendChild(el);
    return el;
  };
})();

export function parseRGBString(rgb) {
  const m = rgb.match(/rgba?\(([^)]+)\)/i);
  if (!m) return null;
  const parts = m[1].split(',').map((p) => Number(p.trim()));
  if (parts.length < 3 || parts.some((v) => !isFiniteNumber(v))) return null;
  return { r: parts[0], g: parts[1], b: parts[2] };
}

export function resolveColorToRGB(color) {
  if (!color || typeof color !== 'string') return null;
  const probe = _colorProbe();
  if (!probe) return null;
  probe.style.color = '';
  probe.style.color = color;
  const resolved = getComputedStyle(probe).color || '';
  return parseRGBString(resolved);
}

export function rgbToLin(v) {
  const c = v / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

export function luminance(rgb) {
  const r = rgbToLin(rgb.r);
  const g = rgbToLin(rgb.g);
  const b = rgbToLin(rgb.b);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function mixRGB(a, b, t) {
  const clampT = Math.max(0, Math.min(1, t));
  return {
    r: a.r + (b.r - a.r) * clampT,
    g: a.g + (b.g - a.g) * clampT,
    b: a.b + (b.b - a.b) * clampT
  };
}

export function rgbToString(rgb, alpha = 1) {
  const r = Math.round(rgb.r);
  const g = Math.round(rgb.g);
  const b = Math.round(rgb.b);
  if (alpha >= 1) return `rgb(${r}, ${g}, ${b})`;
  return `rgba(${r}, ${g}, ${b}, ${alpha.toFixed(3)})`;
}

export function computeContrastBorder(colorStr, bgStr, overlayOpacity) {
  const base = resolveColorToRGB(colorStr);
  if (!base) return 'rgba(255,255,255,0.35)';
  const bg = resolveColorToRGB(bgStr) || { r: 10, g: 10, b: 10 };
  const op = Math.max(0, Math.min(1, overlayOpacity ?? 0.25));
  const effective = mixRGB(bg, base, op);
  const l = luminance(effective);
  const target = l < 0.35 ? { r: 255, g: 255, b: 255 } : { r: 0, g: 0, b: 0 };
  const border = mixRGB(base, target, 0.65);
  return rgbToString(border, 0.9);
}

export function rgbToOklch(rgb) {
  const r = rgbToLin(rgb.r);
  const g = rgbToLin(rgb.g);
  const b = rgbToLin(rgb.b);

  const l = 0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b;
  const m = 0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b;
  const s = 0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b;

  const l_ = Math.cbrt(l);
  const m_ = Math.cbrt(m);
  const s_ = Math.cbrt(s);

  const L = 0.2104542553 * l_ + 0.7936177850 * m_ - 0.0040720468 * s_;
  const A = 1.9779984951 * l_ - 2.4285922050 * m_ + 0.4505937099 * s_;
  const B = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.8086757660 * s_;

  const C = Math.sqrt(A * A + B * B);
  let H = Math.atan2(B, A) * (180 / Math.PI);
  if (H < 0) H += 360;

  return { L: L * 100, C, H };
}

// ── Timeline helpers ─────────────────────────────────────────────────────────

export function timeFromEvent(el, evt, startTime, endTime) {
  const rect = el.getBoundingClientRect();
  const x = clamp(evt.clientX - rect.left, 0, rect.width);
  const pct = rect.width > 0 ? x / rect.width : 0;
  return startTime + pct * (endTime - startTime);
}

// ── Data normalization ───────────────────────────────────────────────────────
// The Go JSON API returns PascalCase properties (ID, StartTs, EndTs, Color, Title).
// Normalize once on load so all JS code can use consistent camelCase.

export function normalizeClip(raw) {
  if (!raw) return raw;
  return {
    id:      (raw.id || raw.ID || '').toString(),
    startTs: raw.StartTs ?? raw.start_ts ?? 0,
    endTs:   raw.EndTs ?? raw.end_ts ?? 0,
    color:   (raw.Color ?? raw.color ?? '').toString(),
    title:   (raw.Title ?? raw.title ?? '').toString(),
    // Preserve full object for any extra fields (crops, filter_stack, etc.)
    _raw: raw,
  };
}

/** Force cursor globally during a drag (overrides all inline cursors). */
export function setDragCursor(name) {
  document.documentElement.dataset.dragCursor = name;
}

/** Clear the forced drag cursor. */
export function clearDragCursor() {
  delete document.documentElement.dataset.dragCursor;
}

export function normalizeMarker(raw) {
  if (!raw) return raw;
  return {
    id:        (raw.id || raw.ID || '').toString(),
    timestamp: raw.Timestamp ?? raw.timestamp ?? raw.time ?? 0,
    color:     (raw.Color ?? raw.color ?? '').toString(),
    title:     (raw.Title ?? raw.title ?? '').toString(),
    duration:  raw.Duration ?? raw.duration ?? null,
    _raw: raw,
  };
}
