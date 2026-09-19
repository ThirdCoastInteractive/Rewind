// nebula-material.js — the Perlin-nebula background as a Three.js ShaderMaterial.
// This is a faithful port of the flat-WebGL fragment shader in
// remote-player-background.js, so the Three.js engine renders the same look.
// Aspect comes in as a uniform (u_aspect) instead of u_resolution.

import * as THREE from 'three';

const vertexShader = /* glsl */ `
  varying vec2 v_uv;
  void main() {
    v_uv = uv;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;

const fragmentShader = /* glsl */ `
  precision highp float;

  varying vec2 v_uv;
  uniform float u_time;
  uniform float u_seed;
  uniform float u_aspect;
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
    float aspect = u_aspect;
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

export function createNebulaMaterial() {
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
