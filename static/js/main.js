import './lib/navigation.js';
import './lib/agent-drawer.js';

function bindWikiPlayers(root) {
  const scope = root || document;
  scope.querySelectorAll('.wiki-player video').forEach((video) => {
    if (video.dataset.wikiBound) return;
    video.dataset.wikiBound = '1';
    const start = parseFloat(video.dataset.start);
    const end = parseFloat(video.dataset.end);
    const hasStart = Number.isFinite(start);
    const hasEnd = Number.isFinite(end) && end > (hasStart ? start : 0);
    const seekStart = () => {
      if (hasStart && Math.abs(video.currentTime - start) > 0.3) {
        video.currentTime = start;
      }
    };
    video.addEventListener('loadedmetadata', seekStart);
    video.addEventListener('play', () => {
      if (hasStart && video.currentTime < start - 0.25) video.currentTime = start;
    });
    if (hasEnd) {
      video.addEventListener('timeupdate', () => {
        if (video.currentTime >= end) {
          video.pause();
          video.currentTime = hasStart ? start : 0;
        }
      });
    }
  });
}

document.addEventListener('DOMContentLoaded', () => bindWikiPlayers(document));
function bootStitchWorkspace() {
  if (!document.querySelector('[data-stitch-workspace]')) return;
  if (typeof window.mountStitchWorkspaces === 'function') {
    window.mountStitchWorkspaces();
    return;
  }
  const existing = document.querySelector('script[src*="stitch-workspace.js"]');
  const src = existing?.getAttribute('src') || '/static/dist/stitch-workspace.js';
  if (document.querySelector(`script[data-stitch-boot="${src}"]`)) return;
  const script = document.createElement('script');
  script.src = src;
  script.dataset.stitchBoot = src;
  script.onload = () => window.mountStitchWorkspaces?.();
  document.head.appendChild(script);
}

window.addEventListener('rewind:page-ready', () => {
  bindWikiPlayers(document.getElementById('page-content') || document);
  bootStitchWorkspace();
});
// ============================================================================
// REWIND - Main Application JavaScript
// Black & White Design System - Physical Interaction Model
// ============================================================================

// ============================================================================
// VIEW TRANSITIONS
// ============================================================================

// Enable view transitions for navigation links
document.addEventListener('DOMContentLoaded', () => {
  // Table row click handlers
  document.addEventListener('click', (e) => {
    const row = e.target.closest('tr[data-href]');
    if (row && !e.target.closest('a, button')) {
      const href = row.getAttribute('data-href');
      if (href) {
        window.RewindNavigation.navigate(href);
      }
    }
  });
});

// ============================================================================
// VIDEO LIST HOVER PREVIEWS
// ============================================================================

document.addEventListener('DOMContentLoaded', () => {
  const activeCard = new WeakMap();

  function getPreviewVideo(card) {
    return card.querySelector('video[data-preview-src]');
  }

  function startPreview(card) {
    const video = getPreviewVideo(card);
    if (!video) return;

    if (!video.getAttribute('src')) {
      const src = video.dataset.previewSrc;
      if (src) video.setAttribute('src', src);
    }

    try {
      video.currentTime = 0;
    } catch {
      // ignore
    }

    const p = video.play();
    if (p && typeof p.catch === 'function') {
      p.catch(() => {});
    }
  }

  function stopPreview(card) {
    const video = getPreviewVideo(card);
    if (!video) return;

    video.pause();
    try {
      video.currentTime = 0;
    } catch {
      // ignore
    }
  }

  // Use bubbling events so we don't need per-card listeners (and it survives DOM morphs)
  document.addEventListener('mouseover', (e) => {
    const card = e.target.closest('[data-video-hover-preview]');
    if (!card) return;
    if (activeCard.get(card)) return;

    activeCard.set(card, true);
    startPreview(card);
  });

  document.addEventListener('mouseout', (e) => {
    const card = e.target.closest('[data-video-hover-preview]');
    if (!card) return;

    const related = e.relatedTarget;
    if (related && card.contains(related)) return;

    activeCard.set(card, false);
    stopPreview(card);
  });
});

// ============================================================================
// AUDIO SERVICE (Sound Design)
// ============================================================================

class AudioService {
  constructor() {
    this.sounds = {};
    this.enabled = document.body?.dataset.soundsEnabled !== 'false';
  }
  
  load(name, path) {
    this.sounds[name] = new Audio(path);
    this.sounds[name].preload = 'auto';
  }
  
  play(name) {
    if (!this.enabled || !this.sounds[name]) return;
    
    const sound = this.sounds[name].cloneNode();
    sound.volume = 0.3; // Subtle volume
    sound.play().catch(() => {}); // Ignore autoplay policy errors
  }
  
  toggle() {
    this.enabled = !this.enabled;
    localStorage.setItem('soundsEnabled', this.enabled);
    return this.enabled;
  }
}

// Global audio service instance
window.audio = new AudioService();

// Preload sound effects (when implemented)
// audio.load('drawer-open', '/static/audio/drawer-open.wav');
// audio.load('job-submit', '/static/audio/job-submit.wav');
// etc.

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// Preferences and bookmarklets belong to the persistent shell and each new page.
document.addEventListener('DOMContentLoaded', () => {
  window.audio.enabled = document.body.dataset.soundsEnabled !== 'false';
});
function preparePageLinks() {
  const bookmarklet = 'javascript:(function(){window.open(' + JSON.stringify(location.origin + '/bookmarklet?url=') + '+encodeURIComponent(location.href),"rewind","width=500,height=600")})()';
  document.querySelectorAll('.bookmarklet-link').forEach(link => link.href = bookmarklet);
  document.querySelectorAll('.bookmarklet-code').forEach(code => code.textContent = bookmarklet);
}
document.addEventListener('DOMContentLoaded', preparePageLinks);
window.addEventListener('rewind:page-ready', preparePageLinks);
document.addEventListener('submit', event => {
  if (event.target.id === 'jobForm' && window.innerWidth < 1024) navigator.vibrate?.(20);
});
