// The document shell owns navigation. DataStar owns fetching and applying SSE.
const pageRoutes = /^\/(?:$|(?:videos|channels|creators|follows|visual|people|network|wiki|compilations|stitch|show-notes|jobs|upload|settings|admin|producer)(?:\/|$))/;
let pending = null;
let scope = makeScope();
const leaveGuards = new Set();
let preparing = false;

function makeScope() {
  const cleanups = new Set();
  const controller = new AbortController();
  return {
    signal: controller.signal,
    cleanup(fn) {
      if (controller.signal.aborted) { fn(); return () => {}; }
      cleanups.add(fn);
      return () => cleanups.delete(fn);
    },
    dispose() {
      for (const fn of [...cleanups].reverse()) { try { fn(); } catch (error) { console.error('Page cleanup failed', error); } }
      cleanups.clear();
      controller.abort();
    },
  };
}

window.RewindPage = {
  get scope() { return scope; },
  ready(fn) {
    if (document.readyState === 'loading') this.listen(document, 'DOMContentLoaded', fn, { once: true });
    else fn();
  },
  listen(target, type, fn, options) {
    target.addEventListener(type, fn, options);
    scope.cleanup(() => target.removeEventListener(type, fn, options));
  },
  eventSource(url) {
    const source = new EventSource(url);
    scope.cleanup(() => source.close());
    return source;
  },
};

function eligible(url) { return url.origin === location.origin && pageRoutes.test(url.pathname); }

function status(text) {
  const el = document.getElementById('navigation-status');
  if (el) { el.textContent = text; el.hidden = !text; }
}

function navigate(href, { history: historyMode = 'push', scroll = [0, 0] } = {}) {
  if (preparing || pending) return;
  const url = new URL(href, location.href);
  if (leaveGuards.size) {
    preparing = true;
    const root = document.getElementById('page-content');
    if (root) root.inert = true;
    status('Saving changes…');
    Promise.resolve().then(async () => {
      for (const guard of leaveGuards) await guard();
      preparing = false;
      startNavigation(url, historyMode, scroll);
    }).catch(() => {
      preparing = false;
      if (root) root.inert = false;
      status('Changes could not be saved. Stay on this page and try again.');
    });
    return;
  }
  startNavigation(url, historyMode, scroll);
}

function startNavigation(url, historyMode, scroll) {
  if (!eligible(url) || !window.__dsAPI) { location.assign(url.href); return; }
  if (!pending && historyMode === 'push') history.replaceState({ ...history.state, rewindScroll: [scrollX, scrollY] }, '');
  pending = { url, historyMode, scroll };
  status('Loading page…');
  window.dispatchEvent(new CustomEvent('rewind-navigate', { detail: { url: url.pathname + url.search } }));
}

window.RewindNavigation = {
  navigate,
  beforeLeave(fn) { leaveGuards.add(fn); return () => leaveGuards.delete(fn); },
  beforeSwap() {
    if (!pending) return;
    scope.dispose();
    document.querySelectorAll('#page-content video, #page-content audio').forEach(media => { media.pause(); media.removeAttribute('src'); media.querySelectorAll('source').forEach(s => s.removeAttribute('src')); media.load(); });
    scope = makeScope();
    const { url, historyMode } = pending;
    if (historyMode === 'push') history.pushState({ rewindScroll: [0, 0] }, '', url);
    else if (historyMode === 'replace') history.replaceState({ rewindScroll: [0, 0] }, '', url);
    window.__dsAPI?.mergePatch({ agentPage: url.pathname, _navOpen: false });
    document.querySelectorAll('#main-nav details[open]').forEach(d => d.open = false);
  },
  afterSwap() {
    const root = document.getElementById('page-content');
    if (!root) return;
    document.title = root.dataset.pageTitle;
    document.body.classList.toggle('page-fullscreen', root.dataset.pageFullscreen === 'true');
    document.querySelectorAll('#main-nav a[href]').forEach(link => {
      const path = new URL(link.href).pathname;
      const active = location.pathname === path || (path !== '/' && path !== '/admin' && location.pathname.startsWith(path + '/'));
      if (active) link.setAttribute('aria-current', 'page'); else link.removeAttribute('aria-current');
    });
    if (pending) {
      const { scroll, url } = pending;
      requestAnimationFrame(() => {
        if (url.hash) document.getElementById(decodeURIComponent(url.hash.slice(1)))?.scrollIntoView();
        else window.scrollTo(...scroll);
        if (!document.getElementById('rewind-agent')?.contains(document.activeElement)) root.focus({ preventScroll: true });
      });
    }
    pending = null;
    status('');
    window.dispatchEvent(new CustomEvent('rewind:page-ready'));
  },
  fail(message) { pending = null; const root = document.getElementById('page-content'); if (root) root.inert = false; status(message); },
  redirect(href) { pending = null; navigate(href, { history: 'replace' }); },
  hardLoad() { if (pending) location.assign(pending.url.href); },
};

document.addEventListener('click', event => {
  if (event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  const link = event.target.closest('a[href]');
  if (!link || link.target || link.hasAttribute('download') || link.closest('[data-native-navigation]') || [...link.attributes].some(a => a.name.startsWith('data-on:click')) || link.hasAttribute('onclick')) return;
  const url = new URL(link.href, location.href);
  if (!eligible(url) || !window.__dsAPI || (url.pathname === location.pathname && url.search === location.search && url.hash)) return;
  event.preventDefault();
  navigate(url.href);
});

window.addEventListener('popstate', event => navigate(location.href, { history: 'pop', scroll: event.state?.rewindScroll || [0, 0] }));
window.addEventListener('pagehide', () => scope.dispose());
window.addEventListener('pageshow', event => { if (event.persisted) location.reload(); });
document.addEventListener('datastar-fetch', event => {
  if (event.detail?.type === 'error' && event.detail?.el?.id === 'navigation-bridge') window.RewindNavigation.fail('Navigation failed. Try the link again.');
});

