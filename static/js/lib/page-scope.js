// Page-owned resources are disposed before the SSE page replacement.
const scope = window.RewindPage?.scope;
export function onPageCleanup(fn) { return scope?.cleanup(fn) || (() => {}); }
export function listen(target, type, callback, options) {
  target.addEventListener(type, callback, options);
  onPageCleanup(() => target.removeEventListener(type, callback, options));
}
export function observe(observer) { onPageCleanup(() => observer.disconnect()); return observer; }
export function pageInterval(callback, delay) {
  const id = setInterval(callback, delay);
  onPageCleanup(() => clearInterval(id));
  return id;
}
export function pageTimeout(callback, delay) {
  if (scope?.signal.aborted) return 0;
  const id = setTimeout(() => { release(); callback(); }, delay);
  const release = onPageCleanup(() => clearTimeout(id));
  return id;
}
export function whenVisible(el, callback) {
  if (!el) return;
  const observer = observe(new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) { observer.disconnect(); callback(); }
  }));
  observer.observe(el);
}
export class PageMutationObserver extends MutationObserver {
  constructor(callback) { super(callback); onPageCleanup(() => this.disconnect()); }
}
export class PageResizeObserver extends ResizeObserver {
  constructor(callback) { super(callback); onPageCleanup(() => this.disconnect()); }
}
export function pageFrame(callback) {
  if (scope?.signal.aborted) return 0;
  const id = requestAnimationFrame(time => { release(); callback(time); });
  const release = onPageCleanup(() => cancelAnimationFrame(id));
  return id;
}
export function pageFetch(url, options = {}) {
  const signal = scope && !options.keepalive ? (options.signal ? AbortSignal.any([scope.signal, options.signal]) : scope.signal) : options.signal;
  return fetch(url, { ...options, signal });
}
