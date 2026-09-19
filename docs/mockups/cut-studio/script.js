document.addEventListener('DOMContentLoaded', () => {
  const body = document.body;
  const all = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const first = (selector, root = document) => root.querySelector(selector);

  const showToast = (message) => {
    const toast = first('.toast');
    if (!toast) return;
    const text = String(message || 'Concept only — no edit performed');
    toast.textContent = /save|export|remove|delete|added|queued|applied/i.test(text)
      ? 'Concept only — no edit performed'
      : text;
    toast.classList.add('show');
    window.clearTimeout(showToast.timer);
    showToast.timer = window.setTimeout(() => toast.classList.remove('show'), 1800);
  };

  const modal = first('.modal');
  let opener = null;
  const closeModal = () => {
    if (!modal) return;
    modal.classList.remove('open');
    if (opener && typeof opener.focus === 'function') opener.focus();
  };
  all('[data-export]').forEach((button) => button.addEventListener('click', () => {
    if (!modal) return;
    opener = button;
    modal.classList.add('open');
    const dialog = first('.dialog', modal);
    if (dialog) { dialog.tabIndex = -1; dialog.focus(); }
  }));
  all('[data-close]').forEach((button) => button.addEventListener('click', closeModal));
  if (modal) modal.addEventListener('click', (event) => {
    if (event.target === modal) closeModal();
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && modal && modal.classList.contains('open')) closeModal();
  });

  const captionTargets = all('[data-caption-target]');
  let captionText = captionTargets[0]?.textContent.trim() || '';
  let highlightEnabled = true;
  const renderCaption = (text, activeIndex = -1) => {
    captionTargets.forEach((target) => {
      target.textContent = '';
      const parts = String(text).split(/(\s+)/);
      let word = 0;
      parts.forEach((part) => {
        if (/^\s+$/.test(part)) target.appendChild(document.createTextNode(part));
        else if (part) {
          const span = document.createElement('span');
          span.textContent = part;
          if (word === activeIndex) span.classList.add('active-word');
          target.appendChild(span);
          word += 1;
        }
      });
    });
  };
  all('[data-live-caption]').forEach((field) => field.addEventListener('input', () => {
    const value = field.value;
    captionText = value;
    renderCaption(value);
    const id = field.getAttribute('data-layer-text');
    if (id) {
      const target = document.getElementById(id);
      if (target && !target.hasAttribute('data-caption-target')) target.textContent = value;
    }
  }));
  all('[data-layer-text]:not([data-live-caption])').forEach((field) => field.addEventListener('input', () => {
    const target = document.getElementById(field.dataset.layerText);
    if (target) target.textContent = field.value;
  }));
  if (captionTargets.length) renderCaption(captionText);

  all('[data-select]').forEach((button) => button.addEventListener('click', () => {
    const style = (button.dataset.value || button.textContent || 'clear').trim().toLowerCase();
    const value = /karaoke|broadcast|clear/.exec(style)?.[0] || 'clear';
    body.dataset.captionStyle = value;
    all('[data-select]').forEach((item) => item.classList.toggle('active', item === button));
  }));
  const initialStyle = first('[data-select].active');
  if (initialStyle) initialStyle.click();

  const start = 110, end = 135;
  let current = 117, playing = false, timer = 0;
  const format = (seconds) => {
    const safe = Math.max(0, Number(seconds) || 0);
    const centiseconds = Math.round(safe * 100);
    const minutes = Math.floor(centiseconds / 6000);
    const secondsPart = Math.floor(centiseconds / 100) % 60;
    const fraction = centiseconds % 100;
    return `${String(minutes).padStart(2, '0')}:${String(secondsPart).padStart(2, '0')}.${String(fraction).padStart(2, '0')}`;
  };
  const updatePosition = () => {
    const ratio = Math.max(0, Math.min(1, (current - start) / (end - start)));
    all('.playhead').forEach((head) => {
      const tracks = head.closest('.tracks');
      const lane = tracks && first('.lane', tracks);
      if (tracks && lane) {
        const tracksRect = tracks.getBoundingClientRect();
        const laneRect = lane.getBoundingClientRect();
        head.style.left = `${laneRect.left - tracksRect.left + ratio * laneRect.width}px`;
      } else head.style.left = `${ratio * 100}%`;
    });
    all('[data-scrub]').forEach((scrub) => {
      const fill = first('span', scrub);
      if (fill) fill.style.width = `${ratio * 100}%`;
    });
    all('.playbar .time').forEach((time, index) => { time.textContent = index === 0 ? format(current) : '23:47.00'; });
    all('.stage-toolbar .mono').forEach((time) => {
      time.textContent = `${format(current)}${time.textContent.includes('/') ? ' / 23:47.00' : ''}`;
    });
  };
  const setPlaying = (value) => {
    if (value && current >= end) current = start;
    playing = value;
    all('[data-play]').forEach((button) => { button.textContent = playing ? '❚❚' : '▶'; button.setAttribute('aria-label', playing ? 'Pause' : 'Play'); });
    if (playing && !timer) timer = window.setInterval(() => {
      current = Math.min(end, current + 0.25);
      const wordCount = captionText.match(/\S+/g)?.length || 0;
      const index = wordCount ? Math.min(wordCount - 1, Math.floor(((current - start) / (end - start)) * wordCount)) : -1;
      renderCaption(captionText, highlightEnabled ? index : -1);
      updatePosition();
      if (current >= end) { window.clearInterval(timer); timer = 0; setPlaying(false); renderCaption(captionText); }
    }, 250);
    if (!playing && timer) { window.clearInterval(timer); timer = 0; }
  };
  all('[data-play]').forEach((button) => button.addEventListener('click', () => setPlaying(!playing)));
  all('[data-scrub]').forEach((scrub) => scrub.addEventListener('click', (event) => {
    const rect = scrub.getBoundingClientRect();
    if (!rect.width) return;
    current = start + Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) * (end - start);
    const wordCount = captionText.match(/\S+/g)?.length || 0;
    const index = wordCount ? Math.min(wordCount - 1, Math.floor(((current - start) / (end - start)) * wordCount)) : -1;
    renderCaption(captionText, highlightEnabled ? index : -1);
    updatePosition();
  }));
  window.addEventListener('resize', updatePosition);
  updatePosition();

  all('[data-eye]').forEach((button) => {
    const label = button.closest('.stack-item')?.querySelector('strong')?.textContent.trim() || 'layer';
    button.setAttribute('aria-label', `Hide ${label}`);
    button.setAttribute('aria-pressed', 'true');
    button.addEventListener('click', () => {
    const target = document.getElementById(button.dataset.eye);
    if (!target) return;
    const hidden = !target.hidden;
    target.hidden = hidden;
    button.setAttribute('aria-pressed', String(!hidden));
    button.textContent = hidden ? '○' : '◉';
    button.dataset.eyeState = hidden ? 'hidden' : 'visible';
    button.setAttribute('aria-label', `${hidden ? 'Show' : 'Hide'} ${label}`);
    });
  });
  all('[data-safe]').forEach((checkbox) => checkbox.addEventListener('change', () => {
    all('.safe').forEach((safe) => { safe.hidden = !checkbox.checked; });
  }));
  all('[data-highlight]').forEach((checkbox) => checkbox.addEventListener('change', () => {
    highlightEnabled = checkbox.checked;
    renderCaption(captionText);
  }));
  all('[data-safe-button]').forEach((button) => button.addEventListener('click', () => {
    const checkbox = first('[data-safe]');
    if (checkbox) { checkbox.checked = !checkbox.checked; checkbox.dispatchEvent(new Event('change')); }
    else all('.safe').forEach((safe) => { safe.hidden = !safe.hidden; });
  }));
  all('[data-concept]').forEach((button) => {
    button.disabled = true;
    button.title = 'Concept only — no edit performed';
  });
  all('[data-toast]').forEach((button) => button.addEventListener('click', () => showToast(button.dataset.toast)));
});
