import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const code = fs.readFileSync(path.join(here, 'rewind_common.js'), 'utf8');

function element(tag, opts = {}) {
  const children = opts.children || [];
  return {
    tag,
    className: opts.className || '',
    attrs: opts.attrs || {},
    src: opts.src || '',
    duration: opts.duration,
    children,
    childElementCount: children.length,
    textContent: opts.text != null ? opts.text : children.map((child) => child.textContent || '').join(''),
    getAttribute(name) {
      return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null;
    },
    querySelectorAll(sel) {
      if (sel === 'source') return children.filter((child) => child.tag === 'source');
      return [];
    }
  };
}

function flatten(nodes, out = []) {
  for (const node of nodes) {
    out.push(node);
    if (node.children) flatten(node.children, out);
  }
  return out;
}

function hasClass(node, name) {
  return String(node.className || '').split(/\s+/).includes(name);
}

function documentFor(nodes, title) {
  const all = flatten(nodes);
  return {
    title,
    querySelector(sel) {
      const found = this.querySelectorAll(sel);
      return found[0] || null;
    },
    querySelectorAll(sel) {
      if (sel === 'video') return all.filter((node) => node.tag === 'video');
      if (sel === 'span, div, p') {
        return all.filter((node) => node.tag === 'span' || node.tag === 'div' || node.tag === 'p');
      }
      if (sel === '[aria-label], [data-testid]') {
        return all.filter((node) => node.attrs && (node.attrs['aria-label'] || node.attrs['data-testid']));
      }
      if (sel === '.ytp-offline-slate') return all.filter((node) => hasClass(node, 'ytp-offline-slate'));
      if (sel === '.ytp-live-badge, [class*="ytp-live"], [class*="live-badge"]') {
        return all.filter((node) => {
          const className = String(node.className || '');
          return hasClass(node, 'ytp-live-badge') || className.includes('ytp-live') || className.includes('live-badge');
        });
      }
      return [];
    }
  };
}

function inspect(href, nodes, { title = '', entries = [] } = {}) {
  const context = {
    document: documentFor(nodes, title),
    location: new URL(href),
    performance: { getEntriesByType: () => entries },
    crypto: { getRandomValues(buf) { return buf; } },
    URL,
    fetch() {
      throw new Error('unexpected fetch');
    }
  };
  context.window = context;
  vm.runInNewContext(code, context);
  return JSON.parse(JSON.stringify(context.RewindCommon.inspectPageLive()));
}

const playlist = 'https://video.pscp.tv/live/playlist.m3u8';

test('X broadcast with a LIVE chip is a live archive and keeps the page URL', () => {
  const result = inspect('https://x.com/i/broadcasts/1qxvvenMPMQxB', [
    element('video', { duration: 7200 }),
    element('span', { text: 'LIVE' }),
    element('span', { text: 'Started 2 hours ago' }),
    element('div', { text: 'Limited by Broadcaster' })
  ], {
    title: 'Starship Flight 14 / X',
    entries: [{ name: playlist }]
  });

  assert.deepEqual(result, {
    live: true,
    title: 'Starship Flight 14 / X',
    mediaUrl: ''
  });
});

test('twitter.com and event URLs use the same on-air signals', () => {
  const fromChip = inspect('https://mobile.twitter.com/i/broadcasts/1ZkKzeyrPbaxv', [
    element('div', { attrs: { 'aria-label': 'Live' }, children: [element('span', { text: 'ignored' })] })
  ]);
  const fromStarted = inspect('https://www.x.com/i/events/starship', [
    element('p', { text: 'Started 5 minutes ago' })
  ]);
  const fromOpenVideo = inspect('https://twitter.com/i/broadcasts/1yNGaQLWpejGj', [
    element('video', { duration: Infinity })
  ]);

  assert.equal(fromChip.live, true);
  assert.equal(fromStarted.live, true);
  assert.equal(fromOpenVideo.live, true);
});

test('an ended X broadcast stays a normal archive', () => {
  const result = inspect('https://x.com/i/broadcasts/1qxvvenMPMQxB', [
    element('video', { duration: 5400, src: playlist }),
    element('span', { text: 'Replay' })
  ], { title: 'Starship Flight 14 / X' });

  assert.equal(result.live, false);
  assert.equal(result.mediaUrl, '');
});

test('an X post is not a broadcast', () => {
  const result = inspect('https://x.com/SpaceX/status/123', [
    element('span', { text: 'LIVE' }),
    element('video', { duration: Infinity })
  ]);
  assert.equal(result.live, false);
});

test('Kick and YouTube live detection is unchanged', () => {
  const kick = inspect('https://kick.com/someuser', [
    element('video', { children: [element('source', { src: playlist })] })
  ]);
  const youtube = inspect('https://www.youtube.com/watch?v=abc', [
    element('div', { className: 'ytp-live-badge' })
  ], { title: 'Live now' });
  const offline = inspect('https://www.youtube.com/@Handle/live', [
    element('div', { className: 'ytp-offline-slate' }),
    element('video')
  ]);

  assert.equal(kick.live, true);
  assert.equal(kick.mediaUrl, playlist);
  assert.equal(youtube.live, true);
  assert.equal(offline.live, false);
});
