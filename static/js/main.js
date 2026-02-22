// ============================================================================
// REWIND - Main Application JavaScript
// Black & White Design System - Physical Interaction Model
// ============================================================================

// ============================================================================
// VIEW TRANSITIONS
// ============================================================================

// Enable view transitions for navigation links
document.addEventListener('DOMContentLoaded', () => {
  // View Transitions API support
  if (document.startViewTransition) {
    document.addEventListener('click', (e) => {
      const link = e.target.closest('a[data-transition]');
      if (link && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
        e.preventDefault();
        document.startViewTransition(() => {
          window.location.href = link.href;
        });
      }
    });
  }

  // Table row click handlers
  document.addEventListener('click', (e) => {
    const row = e.target.closest('tr[data-href]');
    if (row && !e.target.closest('a, button')) {
      const href = row.getAttribute('data-href');
      if (href) {
        window.location.href = href;
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
    this.enabled = localStorage.getItem('soundsEnabled') !== 'false';
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

// Escape key handler for closing drawers/modals
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    // Close mobile menu if open
    const mobileMenu = document.getElementById('mobile-menu');
    if (mobileMenu && !mobileMenu.classList.contains('hidden')) {
      mobileMenu.classList.add('hidden');
    }
    
    // Close admin dropdown
    const adminDropdown = document.getElementById('admin-dropdown');
    if (adminDropdown && !adminDropdown.classList.contains('hidden')) {
      adminDropdown.classList.add('hidden');
    }
  }
});
// ============================================================================
// MOBILE GESTURE SUPPORT
// ============================================================================

// Swipe-to-close mobile menu
document.addEventListener('DOMContentLoaded', () => {
  const mobileMenu = document.getElementById('mobile-menu');
  if (!mobileMenu) return;
  
  let touchStartY = 0;
  let touchStartTime = 0;
  
  mobileMenu.addEventListener('touchstart', (e) => {
    touchStartY = e.touches[0].clientY;
    touchStartTime = Date.now();
  }, { passive: true });
  
  mobileMenu.addEventListener('touchend', (e) => {
    const touchEndY = e.changedTouches[0].clientY;
    const touchDuration = Date.now() - touchStartTime;
    const swipeDistance = touchEndY - touchStartY;
    
    // Swipe up to close (>50px in <300ms)
    if (swipeDistance < -50 && touchDuration < 300) {
      mobileMenu.classList.add('hidden');
    }
  }, { passive: true });
});

// Haptic feedback for mobile actions (if supported)
function triggerHaptic(intensity = 'light') {
  if ('vibrate' in navigator) {
    const patterns = {
      light: [10],
      medium: [20],
      heavy: [30]
    };
    navigator.vibrate(patterns[intensity] || patterns.light);
  }
}

// Add haptic feedback to important mobile interactions
document.addEventListener('DOMContentLoaded', () => {
  // Job submission
  const jobForm = document.getElementById('jobForm');
  if (jobForm && window.innerWidth < 1024) {
    jobForm.addEventListener('submit', () => {
      triggerHaptic('medium');
    });
  }
  
  // Mobile menu toggle
  const mobileMenuButton = document.querySelector('[onclick="toggleMobileMenu()"]');
  if (mobileMenuButton) {
    mobileMenuButton.addEventListener('click', () => {
      triggerHaptic('light');
    });
  }
});