import { pageInterval, PageMutationObserver, onPageCleanup } from './lib/page-scope.js';
// three-scene.js — Three.js scene engine entry.
//
// Renders the same scene JSON
// (from #player-scene) but as a Three.js compositor, and composites the webcam
// streams published by webrtc-room.js (via window.__rewindCams) as VideoTexture
// meshes instead of DOM tiles. Used by both the producer preview and the viewer.

import { SceneCore } from './lib/scene/scene-core.js';

(function () {
  const canvas = document.getElementById('remote-player-bg-canvas');
  if (!canvas) return;

  const params = new URLSearchParams(location.search);
  const transparent = params.get('transparent') === '1';
  const fixedWidth = parseInt(params.get('w') || '0', 10) || 0;
  const fixedHeight = parseInt(params.get('h') || '0', 10) || 0;

  // OBS browser sources composite over other layers — make the page transparent.
  if (transparent && document.body) document.body.style.background = 'transparent';

  const core = new SceneCore(canvas, { transparent, fixedWidth, fixedHeight });
  onPageCleanup(() => core.dispose());

  // The MutationObserver below watches the whole body subtree (so it still fires
  // when #player-scene is replaced wholesale by an SSE patch). Dedupe on the
  // actual base64 so unrelated DOM mutations — e.g. the producer editor
  // re-rendering its panels — are cheap no-ops and never loop.
  let lastB64 = '';
  function readScene() {
    const el = document.getElementById('player-scene');
    if (!el || !el.dataset || !el.dataset.sceneB64) return;
    if (el.dataset.sceneB64 === lastB64) return;
    lastB64 = el.dataset.sceneB64;
    let json;
    try {
      // Decode base64 as UTF-8 (atob alone is Latin-1 and mangles non-ASCII text).
      const bin = atob(el.dataset.sceneB64);
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      json = JSON.parse(new TextDecoder().decode(bytes));
    } catch (_) {
      return;
    }
    // On the producer, the scene editor owns the local model and suppresses its
    // own broadcast echo; the viewer applies scenes directly.
    if (window.__sceneEditor) window.__sceneEditor.onRemoteScene(json);
    else core.applyScene(json);
  }
  readScene();

  // The DOM video frame and cam grid are replaced by 3D meshes.
  const frame = document.getElementById('remote-player-video-frame');
  if (frame) frame.style.display = 'none';
  const grid = document.getElementById('cam-grid');
  if (grid) grid.style.display = 'none';

  // Re-read the scene whenever DataStar patches #player-scene (it may replace the node).
  const root = document.body || document.documentElement;
  if (root) {
    new PageMutationObserver(readScene).observe(root, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['data-scene-b64'],
    });
  }

  // Surface content load/play state: toggle the on-stage loading overlay and a
  // DataStar signal the producer panel reflects.
  core.setStatusCallback((state) => {
    const ov = document.getElementById('scene-loading');
    if (ov) ov.style.display = state === 'loading' ? 'flex' : 'none';
    const ds = window.__dsAPI;
    if (ds && ds.mergePatch) ds.mergePatch({ _contentState: state });
  });

  // Poll: sync webcam meshes + preload the show note's content videos (from the
  // producer's content list) so switching between them is instant.
  pageInterval(() => {
    const reg = window.__rewindCams;
    if (reg && reg.videos) core.setCamStreams(reg);
    const urls = Array.from(document.querySelectorAll('[data-content-src]'))
      .map((e) => e.dataset.contentSrc)
      .filter(Boolean);
    if (urls.length) core.preload(urls);
  }, 800);

  window.__rewindScene = core;
})();

