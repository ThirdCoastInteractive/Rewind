import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

test('navigation waits for saves and keeps the editor usable after failure', async () => {
  const root = { inert: false };
  const document = new EventTarget();
  document.getElementById = id => id === 'page-content' ? root : null;
  const window = new EventTarget();
  const context = vm.createContext({ window, document, history: { state: {}, replaceState() {} },
    location: new URL('http://localhost/stitch'), URL, AbortController, CustomEvent, console, scrollX: 0, scrollY: 0 });
  vm.runInContext(await readFile(new URL('./navigation.js', import.meta.url), 'utf8'), context);
  window.__dsAPI = {};
  let navigations = 0;
  window.addEventListener('rewind-navigate', () => navigations++);
  let reject;
  const remove = window.RewindNavigation.beforeLeave(() => new Promise((_, no) => { reject = no; }));
  window.RewindNavigation.navigate('/jobs');
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(navigations, 0);
  assert.equal(root.inert, true);
  reject(new Error('save failed'));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(root.inert, false);
  assert.equal(window.RewindPage.scope.signal.aborted, false);
  remove();
  window.RewindNavigation.beforeLeave(async () => {});
  window.RewindNavigation.navigate('/jobs');
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(navigations, 1);
});

test('page swaps close streams and listeners without disturbing shell state', async () => {
  const document = new EventTarget();
  document.getElementById = () => null;
  document.querySelectorAll = () => [];
  const window = new EventTarget();
  const history = { state: {}, replaceState() {}, pushState() {} };
  let closed = 0;
  const context = vm.createContext({
    window, document, history, location: new URL('http://localhost/videos'),
    URL, AbortController, EventTarget, CustomEvent, console, scrollX: 0, scrollY: 0,
    EventSource: class { close() { closed++; } },
  });
  vm.runInContext(await readFile(new URL('./navigation.js', import.meta.url), 'utf8'), context);
  const oldScope = window.RewindPage.scope;
  let events = 0;
  window.RewindPage.listen(document, 'page-action', () => events++);
  window.RewindPage.eventSource('/api/jobs/stream');
  window.__dsAPI = { mergePatch() {} };
  window.RewindNavigation.navigate('/jobs');
  window.RewindNavigation.beforeSwap();
  document.dispatchEvent(new Event('page-action'));
  assert.equal(events, 0);
  assert.equal(closed, 1);
  assert.equal(oldScope.signal.aborted, true);
  assert.notEqual(window.RewindPage.scope, oldScope);
  assert.equal(window.RewindPage.scope.signal.aborted, false);
  let lateCleanup = false;
  oldScope.cleanup(() => { lateCleanup = true; });
  assert.equal(lateCleanup, true, 'late async completions cannot retain resources on a disposed page');
});

test('failed navigation leaves the current page resources alive', async () => {
  const document = new EventTarget();
  document.getElementById = () => null;
  const window = new EventTarget();
  const context = vm.createContext({
    window, document, history: { state: {}, replaceState() {} },
    location: new URL('http://localhost/videos'), URL, AbortController,
    CustomEvent, console, scrollX: 0, scrollY: 0,
  });
  vm.runInContext(await readFile(new URL('./navigation.js', import.meta.url), 'utf8'), context);
  window.__dsAPI = {};
  const scope = window.RewindPage.scope;
  window.RewindNavigation.navigate('/jobs');
  window.RewindNavigation.fail('Failed');
  assert.equal(scope.signal.aborted, false);
  assert.equal(window.RewindPage.scope, scope);
});
