// effects.js — background-effect registry for the scene compositor. Each effect
// is a Three.js ShaderMaterial sharing the same uniforms (u_time/u_seed/u_aspect/
// u_tint), so the scene core can swap them and keep driving them identically.
// All animation derives from u_time (epoch-synced) for deterministic remote sync.

import * as THREE from 'three';
import { createNebulaMaterial } from './nebula-material.js';

const vertexShader = /* glsl */ `
  varying vec2 v_uv;
  void main() {
    v_uv = uv;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;

function shaderMaterial(fragmentShader) {
  return new THREE.ShaderMaterial({
    uniforms: {
      u_time: { value: 0 },
      u_seed: { value: 0 },
      u_aspect: { value: 16 / 9 },
      u_tint: { value: new THREE.Color(1, 1, 1) },
    },
    vertexShader,
    fragmentShader,
    depthTest: false,
    depthWrite: false,
  });
}

const starfieldFragment = /* glsl */ `
  precision highp float;
  varying vec2 v_uv;
  uniform float u_time, u_seed, u_aspect;
  uniform vec3 u_tint;
  float hash(vec2 p){ return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
  void main() {
    vec2 p = (v_uv - 0.5) * vec2(u_aspect, 1.0);
    p += vec2(u_time * 0.01, u_time * 0.004);
    float scale = 18.0;
    vec2 g = p * scale;
    vec2 cell = floor(g);
    vec2 f = fract(g);
    float br = 0.0;
    for (int dy = -1; dy <= 1; dy++) {
      for (int dx = -1; dx <= 1; dx++) {
        vec2 o = vec2(float(dx), float(dy));
        vec2 c = cell + o + u_seed;
        vec2 star = vec2(hash(c), hash(c + 7.0));
        float d = length(f - (o + star));
        float twinkle = 0.6 + 0.4 * sin(u_time * 2.0 + hash(c) * 6.2831);
        br += smoothstep(0.09, 0.0, d) * twinkle * step(0.62, hash(c + 3.0));
      }
    }
    float r = length((v_uv - 0.5) * vec2(u_aspect, 1.0));
    br *= smoothstep(1.2, 0.1, r);
    gl_FragColor = vec4(u_tint * br, 1.0);
  }
`;

const plasmaFragment = /* glsl */ `
  precision highp float;
  varying vec2 v_uv;
  uniform float u_time, u_seed, u_aspect;
  uniform vec3 u_tint;
  void main() {
    vec2 p = (v_uv - 0.5) * vec2(u_aspect, 1.0) * 4.0;
    float t = u_time * 0.5 + u_seed;
    float v = sin(p.x + t) + sin(p.y + t) + sin(p.x + p.y + t) + sin(length(p) + t);
    float n = 0.5 + 0.5 * sin(v * 0.7854);
    float r = length((v_uv - 0.5) * vec2(u_aspect, 1.0));
    float vig = smoothstep(1.1, 0.2, r);
    gl_FragColor = vec4(u_tint * (n * 0.7 * vig), 1.0);
  }
`;

// A flat, opaque fill in the tint colour. Handy as a solid backdrop, or stacked
// behind another (future) transparent effect.
const colorFragment = /* glsl */ `
  precision highp float;
  varying vec2 v_uv;
  uniform vec3 u_tint;
  void main() { gl_FragColor = vec4(u_tint, 1.0); }
`;

// "Space": a tinted nebula field (brighter toward the top) with white twinkling
// stars over it — a colour-first backdrop (set the tint hue/chroma). Unlike
// `starfield` (white stars on black), the field itself is the tint colour, so a
// red tint reads as "red space with stars." All motion derives from u_time.
const spaceFragment = /* glsl */ `
  precision highp float;
  varying vec2 v_uv;
  uniform float u_time, u_seed, u_aspect;
  uniform vec3 u_tint;

  float hash12(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * 0.1031);
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.x + p3.y) * p3.z);
  }
  vec2 hash22(vec2 p) { return vec2(hash12(p), hash12(p + 19.19)) * 2.0 - 1.0; }
  float gradNoise(vec2 p) {
    vec2 i = floor(p), f = fract(p);
    vec2 u = f * f * f * (f * (f * 6.0 - 15.0) + 10.0);
    float n00 = dot(normalize(hash22(i)), f);
    float n10 = dot(normalize(hash22(i + vec2(1.0, 0.0))), f - vec2(1.0, 0.0));
    float n01 = dot(normalize(hash22(i + vec2(0.0, 1.0))), f - vec2(0.0, 1.0));
    float n11 = dot(normalize(hash22(i + vec2(1.0, 1.0))), f - vec2(1.0, 1.0));
    return mix(mix(n00, n10, u.x), mix(n01, n11, u.x), u.y);
  }
  float fbm(vec2 p) {
    float s = 0.0, a = 0.55, fr = 1.0;
    for (int i = 0; i < 5; i++) { s += a * gradNoise(p * fr); fr *= 2.0; a *= 0.5; }
    return s;
  }
  float sh(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }

  void main() {
    vec2 uv = v_uv;
    vec2 p = (uv - 0.5) * vec2(u_aspect, 1.0);
    p += vec2(u_seed * 0.013, u_seed * 0.021);
    float t = u_time * 0.05;
    vec2 drift = vec2(0.12 * t, -0.08 * t);

    // Tinted field: cloudy fbm variation, brighter at the top, darker low.
    float n = 0.5 + 0.5 * fbm(p * 2.2 + drift);
    float grad = mix(1.0, 0.35, clamp(uv.y, 0.0, 1.0));
    float field = (0.4 + 0.55 * n) * grad;
    vec3 col = u_tint * field;

    // White twinkling stars over the field.
    vec2 g = (p + drift * 0.5) * 16.0;
    vec2 cell = floor(g), f = fract(g);
    float br = 0.0;
    for (int dy = -1; dy <= 1; dy++) {
      for (int dx = -1; dx <= 1; dx++) {
        vec2 o = vec2(float(dx), float(dy));
        vec2 c = cell + o + u_seed;
        vec2 star = vec2(sh(c), sh(c + 7.0));
        float d = length(f - (o + star));
        float tw = 0.6 + 0.4 * sin(u_time * 2.0 + sh(c) * 6.2831);
        br += smoothstep(0.05, 0.0, d) * tw * step(0.82, sh(c + 3.0));
      }
    }
    col += vec3(1.0) * br;
    gl_FragColor = vec4(col, 1.0);
  }
`;

// createEffectMaterial returns the ShaderMaterial for a background mode.
export function createEffectMaterial(mode) {
  switch (mode) {
    case 'starfield':
      return shaderMaterial(starfieldFragment);
    case 'plasma':
      return shaderMaterial(plasmaFragment);
    case 'space':
      return shaderMaterial(spaceFragment);
    case 'color':
      return shaderMaterial(colorFragment);
    case 'perlin-nebula':
    default:
      return createNebulaMaterial();
  }
}
