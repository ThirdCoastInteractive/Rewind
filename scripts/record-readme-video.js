#!/usr/bin/env node
/**
 * record-readme-video.js
 *
 * Records short screencast clips for each major feature, then stitch-readme-video.js
 * concatenates them with title cards into a single demo video.
 *
 * Optimised: 30 fps (was 90), tighter dwell times, pre-warmed caches, covers
 * all major features: home, library, video detail, transcript search, cut editor
 * (timeline + clip creation), filters, settings, admin, and jobs.
 */

import puppeteer from 'puppeteer';
import { mkdirSync, writeFileSync } from 'fs';
import { join } from 'path';

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const BASE_URL = process.env.BASE_URL || 'http://localhost:8080';
const USERNAME = process.env.TEST_USERNAME || 'admin';
const PASSWORD = process.env.TEST_PASSWORD || '';
const TARGET_VIDEO_ID =
  process.env.TARGET_VIDEO_ID || '27666716-eb50-5a31-b90e-55b7a412fdfe';
const OUTPUT_DIR = join(process.cwd(), 'screenshots', 'readme');
const CLIPS_DIR = join(OUTPUT_DIR, 'video-clips');
const MANIFEST_PATH = join(CLIPS_DIR, 'manifest.json');
const FPS = 30;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function ensureDir(p) {
  mkdirSync(p, { recursive: true });
}
const pause = (ms) => new Promise((r) => setTimeout(r, ms));

let globalStart = Date.now();
function logStep(msg) {
  const s = ((Date.now() - globalStart) / 1000).toFixed(1);
  console.log(`  [${s}s] ${msg}`);
}

// ---------------------------------------------------------------------------
// Cursor overlay
// ---------------------------------------------------------------------------

async function enableCursorOverlay(page) {
  await page.evaluateOnNewDocument(() => {
    window.addEventListener('DOMContentLoaded', () => {
      const cursor = document.createElement('div');
      cursor.id = '__demo-cursor';
      Object.assign(cursor.style, {
        position: 'fixed',
        left: '0px',
        top: '0px',
        width: '12px',
        height: '12px',
        border: '2px solid rgba(255,255,255,0.9)',
        borderRadius: '999px',
        transform: 'translate(-50%, -50%)',
        pointerEvents: 'none',
        zIndex: '999999',
        boxShadow: '0 0 6px rgba(0,0,0,0.6)',
        background: 'rgba(0,0,0,0.2)',
        transition: 'left 60ms linear, top 60ms linear',
      });

      const style = document.createElement('style');
      style.textContent = `
        * { cursor: none !important; }
        .__click-ripple {
          position: fixed; width: 14px; height: 14px;
          border: 2px solid rgba(255,255,255,0.8);
          border-radius: 999px;
          transform: translate(-50%, -50%);
          pointer-events: none; z-index: 999998;
          animation: __ripple 350ms ease-out forwards;
        }
        @keyframes __ripple {
          0%   { opacity: 0.9; transform: translate(-50%, -50%) scale(1); }
          100% { opacity: 0;   transform: translate(-50%, -50%) scale(2.4); }
        }
      `;
      document.head.appendChild(style);
      document.body.appendChild(cursor);

      document.addEventListener('mousemove', (e) => {
        cursor.style.left = `${e.clientX}px`;
        cursor.style.top = `${e.clientY}px`;
      });
      document.addEventListener('click', (e) => {
        const r = document.createElement('div');
        r.className = '__click-ripple';
        r.style.left = `${e.clientX}px`;
        r.style.top = `${e.clientY}px`;
        document.body.appendChild(r);
        setTimeout(() => r.remove(), 400);
      });
    });
  });
}

// ---------------------------------------------------------------------------
// Smooth-motion helpers
// ---------------------------------------------------------------------------

function ease(t) {
  return t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t;
}

async function smoothMove(page, tx, ty, ms = 300) {
  const steps = Math.max(6, Math.round(ms / 16));
  const from = await page.evaluate(() => {
    const c = document.getElementById('__demo-cursor');
    if (!c) return { x: 0, y: 0 };
    return { x: parseFloat(c.style.left) || 0, y: parseFloat(c.style.top) || 0 };
  });
  for (let i = 1; i <= steps; i++) {
    const t = ease(i / steps);
    await page.mouse.move(from.x + (tx - from.x) * t, from.y + (ty - from.y) * t);
    await pause(ms / steps);
  }
}

async function smoothClick(page, selector, moveDur = 300) {
  const c = await page.$eval(selector, (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  });
  await smoothMove(page, c.x, c.y, moveDur);
  await pause(80);
  await page.mouse.click(c.x, c.y);
}

async function naturalType(page, text, base = 25) {
  for (const ch of text) {
    await page.keyboard.type(ch);
    await pause(base + Math.random() * 15);
  }
}

async function parkCursor(page, x = 960, y = 400) {
  await page.mouse.move(x, y);
  await pause(30);
}

// ---------------------------------------------------------------------------
// Navigation helpers
// ---------------------------------------------------------------------------

async function login(page) {
  if (!PASSWORD) throw new Error('Set TEST_PASSWORD env var.');
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle2' });
  await page.waitForSelector('input[name="username"]');
  await page.type('input[name="username"]', USERNAME, { delay: 10 });
  await page.type('input[name="password"]', PASSWORD, { delay: 10 });
  await Promise.all([
    page.waitForNavigation({ waitUntil: 'load' }),
    page.click('button[type="submit"]'),
  ]);
  if (page.url().includes('/login')) throw new Error('Login failed.');
}

async function go(page, path, selector) {
  await page.goto(`${BASE_URL}${path}`, { waitUntil: 'load' });
  if (selector) await page.waitForSelector(selector, { timeout: 15000 });
  await parkCursor(page);
}

async function trySetVideosPageSize(page, size) {
  await page.evaluate((s) => {
    const b = Array.from(document.querySelectorAll('[data-on\\:click]'));
    const t = b.find((el) => el.textContent?.trim() === String(s));
    if (t) t.click();
  }, size);
  await pause(350);
}

async function findAndOpenTargetVideo(page, videoId, maxPages = 6) {
  const sel = `a[href="/videos/${videoId}"]`;
  for (let i = 0; i < maxPages; i++) {
    const el = await page.$(sel);
    if (el) {
      const box = await el.boundingBox();
      if (box) {
        await smoothMove(page, box.x + box.width / 2, box.y + box.height / 2, 350);
        await pause(200);
      }
      await Promise.all([
        page.waitForNavigation({ waitUntil: 'load' }),
        page.click(sel),
      ]);
      return true;
    }
    const prev = await page.evaluate(() =>
      document.querySelector('#videos-grid a')?.getAttribute('href')
    );
    const clicked = await page.evaluate(() => {
      const p = document.getElementById('videos-pagination');
      if (!p) return false;
      const btn = Array.from(p.querySelectorAll('button')).find((b) =>
        b.textContent?.toLowerCase().includes('next')
      );
      if (btn) { btn.click(); return true; }
      return false;
    });
    if (!clicked) break;
    await page
      .waitForFunction(
        (p) => document.querySelector('#videos-grid a')?.getAttribute('href') !== p,
        { timeout: 8000 },
        prev
      )
      .catch(() => {});
  }
  return false;
}

async function waitForCutEditorReady(page) {
  await page.waitForFunction(
    () =>
      window.cutEditor &&
      Number.isFinite(window.cutEditor.duration) &&
      window.cutEditor.duration > 0,
    { timeout: 20000 }
  );
}

// ---------------------------------------------------------------------------
// Timeline helpers (smooth)
// ---------------------------------------------------------------------------

async function dragTimeline(page, selector, startPct, endPct, steps = 30) {
  const box = await page.$eval(selector, (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  });
  const y = box.y + box.h / 2;
  const sx = box.x + box.w * startPct;
  const ex = box.x + box.w * endPct;
  await smoothMove(page, sx, y, 250);
  await pause(80);
  await page.mouse.down();
  for (let i = 1; i <= steps; i++) {
    const t = ease(i / steps);
    await page.mouse.move(sx + (ex - sx) * t, y);
    await pause(14);
  }
  await page.mouse.up();
}

async function clickTimeline(page, selector, pct) {
  const box = await page.$eval(selector, (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  });
  const x = box.x + box.w * pct;
  const y = box.y + box.h / 2;
  await smoothMove(page, x, y, 250);
  await pause(60);
  await page.mouse.click(x, y);
}

// ---------------------------------------------------------------------------
// Pre-warm
// ---------------------------------------------------------------------------

async function warmUp(page) {
  logStep('⏳ Pre-warming caches…');
  const paths = [
    '/',
    '/videos',
    `/videos/${TARGET_VIDEO_ID}`,
    `/videos/${TARGET_VIDEO_ID}/cut`,
    '/jobs',
    '/settings',
    '/admin',
  ];
  for (const p of paths) {
    await page.goto(`${BASE_URL}${p}`, { waitUntil: 'load' }).catch(() => {});
  }
  logStep('✅ Warm-up done');
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function run() {
  ensureDir(OUTPUT_DIR);
  ensureDir(CLIPS_DIR);
  globalStart = Date.now();

  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080, deviceScaleFactor: 1 });
  await enableCursorOverlay(page);

  try {
    await login(page);
    await warmUp(page);
    await page.mouse.move(960, 540);

    const clips = [];

    const recordSegment = async (name, title, fn) => {
      logStep(`🎬 Recording ${name}…`);
      const p = join(CLIPS_DIR, `${name}.webm`);
      const rec = await page.screencast({ path: p, fps: FPS });
      await fn();
      await rec.stop();
      clips.push({ name, title, path: `video-clips/${name}.webm` });
    };

    // ---- Home: paste a URL ----
    await recordSegment('home', 'Add a Video', async () => {
      await go(page, '/', '#jobForm');
      await pause(1200);
      await smoothClick(page, 'input[name="url"], #jobForm input[type="text"]', 400);
      await pause(300);
      await naturalType(page, 'https://www.youtube.com/watch?v=dQw4w9WgXcQ', 35);
      await pause(800);
    });

    // ---- Library: browse thumbnails ----
    await recordSegment('library', 'Browse the Library', async () => {
      await go(page, '/videos', '#videos-grid a');
      await trySetVideosPageSize(page, 96);
      await pause(1200);
      const thumbs = await page.$$('#videos-grid a');
      for (const idx of [0, 3, 7]) {
        if (thumbs[idx]) {
          const box = await thumbs[idx].boundingBox();
          if (box) {
            await smoothMove(page, box.x + box.width / 2, box.y + box.height / 2, 300);
            await pause(300);
          }
        }
      }
      await pause(600);
    });

    // ---- Video detail: transcript search ----
    await recordSegment('video-detail', 'Watch + Search Transcripts', async () => {
      const opened = await findAndOpenTargetVideo(page, TARGET_VIDEO_ID);
      if (!opened) await go(page, `/videos/${TARGET_VIDEO_ID}`, '#videoPlayer');
      await parkCursor(page);
      await pause(1500);
      await page.waitForSelector('[data-transcript-search]');
      await smoothClick(page, '[data-transcript-search]', 350);
      await pause(300);
      await naturalType(page, 'the', 60);
      await pause(1200);
    });

    // ---- Cut editor: timeline + clip creation ----
    await recordSegment('cut-editor', 'Create Clips', async () => {
      const cutLink = `a[href="/videos/${TARGET_VIDEO_ID}/cut"]`;
      await Promise.all([
        page.waitForNavigation({ waitUntil: 'load' }),
        smoothClick(page, cutLink, 300),
      ]);
      await page.waitForSelector('[data-cut-page]');
      await waitForCutEditorReady(page);
      await parkCursor(page);
      await pause(1200);

      await clickTimeline(page, '[data-cut-overview]', 0.2);
      await pause(500);
      await dragTimeline(page, '[data-cut-work]', 0.25, 0.65);
      await pause(800);

      // Create clip
      await page.evaluate(() => {
        window.cutEditor.inPoint = 5;
        window.cutEditor.outPoint = 8;
        window.cutEditor.renderRange();
        window.cutEditor.render();
        window.cutEditor.createClipFromRange();
      });
      await pause(1200);

      // Select the new clip
      const clipRow = await page.$('[data-clip-row][data-clip-id]');
      if (clipRow) {
        await clipRow.click();
        await pause(800);
      }
    });

    // ---- Filters panel ----
    await recordSegment('filters', 'Color Filters', async () => {
      // Open the COLOR / FILTERS panel
      await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('button'));
        const btn = buttons.find(
          (b) => b.textContent?.includes('COLOR') && b.textContent?.includes('FILTERS')
        );
        if (btn) btn.click();
      });
      await pause(400);

      // Open "Add Filter" dropdown
      const filterSummary = await page.$('#filter-stack details summary');
      if (filterSummary) {
        await smoothClick(page, '#filter-stack details summary', 300);
        await pause(600);
      }
      await pause(800);
    });

    // ---- Settings ----
    await recordSegment('settings', 'Settings & Keybindings', async () => {
      await go(page, '/settings', 'form');
      await pause(1200);
      await page.evaluate(() => window.scrollTo({ top: 300, behavior: 'smooth' }));
      await pause(800);
    });

    // ---- Admin dashboard ----
    await recordSegment('admin', 'Admin Dashboard', async () => {
      await go(page, '/admin');
      await pause(1200);
      await page.evaluate(() => window.scrollTo({ top: 300, behavior: 'smooth' }));
      await pause(800);
    });

    // ---- Jobs ----
    await recordSegment('jobs', 'Download Jobs', async () => {
      await go(page, '/jobs', '#jobs-list');
      await pause(1000);

      // Click into a job for detail
      const jobLink = await page.$('.job-card');
      if (jobLink) {
        await smoothClick(page, '.job-card', 300);
        await pause(1200);
      }
    });

    // ---- Write manifest ----
    writeFileSync(
      MANIFEST_PATH,
      JSON.stringify(
        { baseUrl: BASE_URL, capturedAt: new Date().toISOString(), fps: FPS, clips },
        null,
        2
      )
    );

    const totalSec = ((Date.now() - globalStart) / 1000).toFixed(1);
    logStep(`✅ ${clips.length} clips saved in ${totalSec}s → ${CLIPS_DIR}`);
  } finally {
    await browser.close();
  }
}

run().catch((err) => {
  console.error(`\n❌ ${err.message}`);
  process.exit(1);
});
