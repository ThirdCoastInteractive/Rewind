#!/usr/bin/env node
/**
 * capture-readme-screenshots.js
 *
 * Fast, comprehensive screenshot capture for the README.
 * Covers: home, library, video detail (transcript, markers, comments),
 * clip editor (timeline, filters, crops, export), jobs, settings,
 * keybindings, admin dashboard, admin users, producer.
 *
 * Optimisations over previous version:
 * - Uses `load` instead of `networkidle2` (assets are pre-warmed)
 * - Minimal sleep times (just enough for SSE patches to land)
 * - Single browser session, no redundant navigations
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
const TARGET_VIDEO_ID = process.env.TARGET_VIDEO_ID || '27666716-eb50-5a31-b90e-55b7a412fdfe';
const TARGET_JOB_ID = process.env.TARGET_JOB_ID || '';
const OUTPUT_DIR = join(process.cwd(), 'screenshots', 'readme');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function ensureDir(p) {
  mkdirSync(p, { recursive: true });
}

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

let globalStart = Date.now();

function logStep(msg) {
  const elapsed = ((Date.now() - globalStart) / 1000).toFixed(1);
  console.log(`  [${elapsed}s] 📸 ${msg}`);
}

async function login(page) {
  if (!PASSWORD) throw new Error('Set TEST_PASSWORD env var.');
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle2' });
  await page.waitForSelector('input[name="username"]');
  await page.type('input[name="username"]', USERNAME, { delay: 8 });
  await page.type('input[name="password"]', PASSWORD, { delay: 8 });
  await Promise.all([
    page.waitForNavigation({ waitUntil: 'load' }),
    page.click('button[type="submit"]'),
  ]);
  if (page.url().includes('/login')) throw new Error('Login failed.');
}

/** Pre-warm browser caches so subsequent navigations are fast. */
async function warmUp(page) {
  const paths = [
    '/',
    '/videos',
    `/videos/${TARGET_VIDEO_ID}`,
    `/videos/${TARGET_VIDEO_ID}/cut`,
    '/jobs',
    '/settings',
    '/settings/keybindings',
    '/admin',
    '/producer',
  ];
  for (const p of paths) {
    await page.goto(`${BASE_URL}${p}`, { waitUntil: 'load' }).catch(() => {});
  }
}

async function go(page, path, selector, timeout = 10000) {
  await page.goto(`${BASE_URL}${path}`, { waitUntil: 'load' });
  if (selector) await page.waitForSelector(selector, { timeout });
  await pause(300);
}

async function capture(page, name) {
  const filepath = join(OUTPUT_DIR, `${name}.png`);
  await page.screenshot({ path: filepath, fullPage: false });
  return filepath;
}

async function trySetVideosPageSize(page, size) {
  await page.evaluate((s) => {
    const btns = Array.from(document.querySelectorAll('[data-on\\:click]'));
    const btn = btns.find((b) => b.textContent?.trim() === String(s));
    if (btn) btn.click();
  }, size);
  await pause(500);
}

async function clickVideosNextPage(page) {
  return page.evaluate(() => {
    const p = document.getElementById('videos-pagination');
    if (!p) return false;
    const btn = Array.from(p.querySelectorAll('button')).find((b) =>
      b.textContent?.toLowerCase().includes('next')
    );
    if (btn) { btn.click(); return true; }
    return false;
  });
}

async function findAndOpenTargetVideo(page, videoId, maxPages = 6) {
  const sel = `a[href="/videos/${videoId}"]`;
  for (let i = 0; i < maxPages; i++) {
    if (await page.$(sel)) {
      await Promise.all([
        page.waitForNavigation({ waitUntil: 'load' }),
        page.click(sel),
      ]);
      return true;
    }
    const prev = await page.evaluate(() =>
      document.querySelector('#videos-grid a')?.getAttribute('href')
    );
    if (!(await clickVideosNextPage(page))) break;
    await page.waitForFunction(
      (p) => document.querySelector('#videos-grid a')?.getAttribute('href') !== p,
      { timeout: 8000 },
      prev
    );
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

async function dragTimeline(page, selector, startPct, endPct) {
  const box = await page.$eval(selector, (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  });
  const y = box.y + box.h / 2;
  const sx = box.x + box.w * startPct;
  const ex = box.x + box.w * endPct;
  await page.mouse.move(sx, y);
  await page.mouse.down();
  await page.mouse.move(ex, y, { steps: 12 });
  await page.mouse.up();
}

async function clickTimeline(page, selector, pct) {
  const box = await page.$eval(selector, (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  });
  await page.mouse.click(box.x + box.w * pct, box.y + box.h / 2);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function run() {
  ensureDir(OUTPUT_DIR);
  globalStart = Date.now();

  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });

  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080, deviceScaleFactor: 1 });

  try {
    logStep('Login + warm-up');
    await login(page);
    await warmUp(page);

    // ------------------------------------------------------------------ HOME
    logStep('Home');
    await go(page, '/', '#jobForm');
    await capture(page, 'home');

    // ------------------------------------------------------------------ JOBS
    logStep('Jobs dashboard');
    await go(page, '/jobs', '#jobs-list');
    await capture(page, 'jobs');

    // Job detail
    const jobId = await page.evaluate(async (preferredId) => {
      const cards = Array.from(document.querySelectorAll('.job-card'));
      const ids = cards
        .map((c) => c.getAttribute('href'))
        .filter(Boolean)
        .map((h) => h.split('/').pop())
        .filter(Boolean);
      if (preferredId && ids.includes(preferredId)) return preferredId;
      for (const id of ids) {
        try {
          const r = await fetch(`/api/jobs/${id}/logs?limit=1&offset=0`);
          if (!r.ok) continue;
          const d = await r.json();
          if ((d.total || 0) > 0) return id;
        } catch {}
      }
      return ids[0] || null;
    }, TARGET_JOB_ID);

    if (jobId) {
      await Promise.all([
        page.waitForNavigation({ waitUntil: 'load' }),
        page.click(`a[href="/jobs/${jobId}"]`),
      ]);
      await page.waitForSelector('#job-detail-card', { timeout: 10000 });
      await pause(400);
      await capture(page, 'job-detail');
    }

    // ------------------------------------------------------------------ LIBRARY
    logStep('Video library');
    await go(page, '/videos', '#videos-grid a');
    await trySetVideosPageSize(page, 96);
    await capture(page, 'videos');

    await page.evaluate(() => window.scrollTo({ top: 800, behavior: 'instant' }));
    await pause(200);
    await capture(page, 'videos-scrolled');

    // ------------------------------------------------------------------ VIDEO DETAIL
    logStep('Video detail');
    const opened = await findAndOpenTargetVideo(page, TARGET_VIDEO_ID);
    if (!opened) {
      await go(page, `/videos/${TARGET_VIDEO_ID}`, '#videoPlayer');
    }
    await pause(500);
    await capture(page, 'video-detail');

    // Transcript search
    logStep('Transcript search');
    await page.waitForSelector('[data-transcript-search]');
    await page.click('[data-transcript-search]');
    await page.type('[data-transcript-search]', 'george floyd', { delay: 15 });
    await pause(500);
    await capture(page, 'video-detail-search');

    // ------------------------------------------------------------------ CUT EDITOR
    logStep('Cut editor');
    await Promise.all([
      page.waitForNavigation({ waitUntil: 'load' }),
      page.click(`a[href="/videos/${TARGET_VIDEO_ID}/cut"]`),
    ]);
    await page.waitForSelector('[data-cut-page]');
    await waitForCutEditorReady(page);
    await pause(300);

    await clickTimeline(page, '[data-cut-overview]', 0.2);
    await dragTimeline(page, '[data-cut-work]', 0.25, 0.65);
    await pause(300);
    await capture(page, 'cut-editor');

    // Create a clip
    logStep('Create clip');
    const initialClipIds = await page.$$eval(
      '[data-clip-row][data-clip-id]',
      (rows) => rows.map((r) => r.dataset.clipId)
    );

    await page.evaluate(() => {
      if (!window.cutEditor) throw new Error('Cut editor not available.');
      window.cutEditor.inPoint = 5;
      window.cutEditor.outPoint = 8;
      window.cutEditor.renderRange();
      window.cutEditor.render();
      window.cutEditor.createClipFromRange();
    });

    await page.waitForFunction(
      (known) => {
        const rows = Array.from(document.querySelectorAll('[data-clip-row][data-clip-id]'));
        return rows.some((r) => r.dataset.clipId && !known.includes(r.dataset.clipId));
      },
      { timeout: 30000 },
      initialClipIds
    );
    await pause(300);
    await capture(page, 'cut-editor-clip');

    // Select clip + show export panel
    logStep('Export panel');
    const newClipId = await page.evaluate((known) => {
      const rows = Array.from(document.querySelectorAll('[data-clip-row][data-clip-id]'));
      return (
        rows.find((r) => r.dataset.clipId && !known.includes(r.dataset.clipId))?.dataset.clipId ||
        null
      );
    }, initialClipIds);

    if (newClipId) {
      await page.click(`[data-clip-row][data-clip-id="${newClipId}"]`);
      await pause(1000);

      const exportBtn = await page.$('#cut-export-panel button[data-on\\:click*="exports"]');
      if (exportBtn) await exportBtn.click();
      await pause(800);
    }
    await capture(page, 'cut-editor-export');

    // Filters panel
    logStep('Filters panel');
    // Click the COLOR / FILTERS panel header to open it
    await page.evaluate(() => {
      const buttons = Array.from(document.querySelectorAll('button'));
      const filterBtn = buttons.find(
        (b) => b.textContent?.includes('COLOR') && b.textContent?.includes('FILTERS')
      );
      if (filterBtn) filterBtn.click();
    });
    await pause(400);

    // Open the "Add Filter" dropdown
    const filterDropdown = await page.$('#filter-stack details summary');
    if (filterDropdown) {
      await filterDropdown.click();
      await pause(300);
    }
    await capture(page, 'cut-editor-filters');

    // ------------------------------------------------------------------ SETTINGS
    logStep('Settings');
    await go(page, '/settings', 'form');
    await capture(page, 'settings');

    // Keybindings
    logStep('Keybindings');
    await go(page, '/settings/keybindings', '#keybinding-settings');
    await capture(page, 'keybindings');

    // ------------------------------------------------------------------ ADMIN
    logStep('Admin dashboard');
    await go(page, '/admin');
    await pause(800); // Let D3 charts render
    await capture(page, 'admin');

    // ------------------------------------------------------------------ PRODUCER
    logStep('Producer');
    await go(page, '/producer');
    await pause(300);
    await capture(page, 'producer');

    // ------------------------------------------------------------------ DONE
    const manifest = {
      baseUrl: BASE_URL,
      capturedAt: new Date().toISOString(),
      elapsedMs: Date.now() - globalStart,
      images: [
        'home.png',
        'jobs.png',
        'job-detail.png',
        'videos.png',
        'videos-scrolled.png',
        'video-detail.png',
        'video-detail-search.png',
        'cut-editor.png',
        'cut-editor-clip.png',
        'cut-editor-export.png',
        'cut-editor-filters.png',
        'settings.png',
        'keybindings.png',
        'admin.png',
        'producer.png',
      ],
    };
    writeFileSync(join(OUTPUT_DIR, 'manifest.json'), JSON.stringify(manifest, null, 2));

    const totalSec = ((Date.now() - globalStart) / 1000).toFixed(1);
    console.log(`\n✅ ${manifest.images.length} screenshots in ${totalSec}s → ${OUTPUT_DIR}`);
  } finally {
    await browser.close();
  }
}

run().catch((err) => {
  console.error(`\n❌ ${err.message}`);
  process.exit(1);
});
