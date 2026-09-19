document.addEventListener('DOMContentLoaded', () => {
  const drawer = document.getElementById('rewind-agent');
  if (!drawer) return;
  let wasOpen = false;
  const sync = () => {
    const open = drawer.getAttribute('aria-hidden') === 'false';
    const modal = open && matchMedia('(max-width: 63.99rem)').matches;
    drawer.setAttribute('role', modal ? 'dialog' : 'complementary');
    if (modal) drawer.setAttribute('aria-modal', 'true'); else drawer.removeAttribute('aria-modal');
    document.getElementById('page-content')?.toggleAttribute('inert', modal);
    document.getElementById('main-nav')?.toggleAttribute('inert', modal);
    if (open !== wasOpen) {
      if (open) drawer.querySelector('textarea')?.focus();
      else document.querySelector('[data-agent-toggle]')?.focus();
      wasOpen = open;
    }
  };
  new MutationObserver(sync).observe(drawer, { attributes: true, attributeFilter: ['aria-hidden'] });
  window.addEventListener('resize', sync);
  window.addEventListener('rewind:page-ready', sync);
  drawer.addEventListener('keydown', event => {
    if (event.key === 'Escape') {
      event.stopPropagation();
      window.__dsAPI?.mergePatch({ agentOpen: false });
    }
    if (event.key === 'Tab' && matchMedia('(max-width: 63.99rem)').matches) {
      const items = [...drawer.querySelectorAll('button, a[href], textarea, input, summary')].filter(el => el.getClientRects().length && !el.disabled);
      const first = items[0], last = items.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
  });
});

