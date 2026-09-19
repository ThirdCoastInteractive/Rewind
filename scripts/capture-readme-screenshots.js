#!/usr/bin/env node
/**
 * capture-readme-screenshots.js
 *
 * Capture README screenshots from a running Rewind instance.
 * Read-only: does not create clips, jobs, wiki pages, or show notes.
 */

import puppeteer from 'puppeteer';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'fs';
import { join } from 'path';

function loadDotEnv() {
  if (!existsSync('.env')) return;
  for (const line of readFileSync('.env', 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const eq = trimmed.indexOf('=');
    if (eq < 1) continue;
    const key = trimmed.slice(0, eq);
    let value = trimmed.slice(eq + 1);
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    if (process.env[key] === undefined) process.env[key] = value;
  }
}

loadDotEnv();

const BASE_URL =
  process.env.BASE_URL ||
  `http://localhost:${process.env.WEBSERVER_PORT || '9115'}`;
const USERNAME =
  process.env.TEST_USERNAME ||
  process.env.SITESPEED_TEST_USER ||
  process.env.SITESPEED_ADMIN_USER ||
  'admin';
const PASSWORD =
  process.env.TEST_PASSWORD ||
  process.env.SITESPEED_TEST_PASSWORD ||
  process.env.SITESPEED_ADMIN_PASSWORD ||
  '';
const TARGET_VIDEO_ID =
  process.env.TARGET_VIDEO_ID || 'fbbd9b8e-a8ff-5348-aa2a-04b4c741b26d';
const TARGET_STITCH_ID =
  process.env.TARGET_STITCH_ID || 'd26cb8f8-8f98-5e06-973e-ebecb715dc7b';
const TARGET_CREATOR_ID =
  process.env.TARGET_CREATOR_ID || '2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8';
const TARGET_SHOW_NOTE_ID =
  process.env.TARGET_SHOW_NOTE_ID || 'ea188acc-4e7a-42e1-8c86-d04f4b04d11b';
const TARGET_CHANNEL_NAME = process.env.TARGET_CHANNEL_NAME || 'Ben Avery';
const TARGET_WIKI_PAGE = process.env.TARGET_WIKI_PAGE || '/wiki/clipping/ben-avery';
const TRANSCRIPT_QUERY = process.env.TRANSCRIPT_QUERY || 'Jeff';
const OUTPUT_DIR = join(process.cwd(), 'screenshots', 'readme');

function ensureDir(p) {
  mkdirSync(p, { recursive: true });
}

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

let globalStart = Date.now();
const captured = [];

function logStep(msg) {
  const elapsed = ((Date.now() - globalStart) / 1000).toFixed(1);
  console.log(`  [${elapsed}s] ${msg}`);
}

async function login(page) {
  if (!PASSWORD) throw new Error('Set TEST_PASSWORD or SITESPEED_TEST_PASSWORD.');
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('input[name="username"]');
  await page.type('input[name="username"]', USERNAME, { delay: 8 });
  await page.type('input[name="password"]', PASSWORD, { delay: 8 });
  await Promise.all([
    page.waitForNavigation({ waitUntil: 'domcontentloaded' }),
    page.click('button[type="submit"]'),
  ]);
  if (page.url().includes('/login')) throw new Error('Login failed.');
}

async function waitForImages(page, timeout = 8000) {
  await page.evaluate(async (ms) => {
    const deadline = Date.now() + ms;
    document.querySelectorAll('img[loading="lazy"]').forEach((img) => {
      img.loading = 'eager';
    });
    await Promise.all(
      Array.from(document.images).map((img) => {
        if (img.complete) return Promise.resolve();
        return new Promise((resolve) => {
          const done = () => resolve();
          img.addEventListener('load', done, { once: true });
          img.addEventListener('error', done, { once: true });
          setTimeout(done, Math.max(0, deadline - Date.now()));
        });
      }),
    );
  }, timeout);
}

async function prepare(page) {
  await page.evaluate(() => {
    document.querySelectorAll('details.site-nav-group[open]').forEach((d) => {
      d.open = false;
    });
    const menu = document.getElementById('site-nav-menu');
    if (menu) menu.classList.remove('is-open');
    const agent = document.getElementById('rewind-agent');
    if (agent) agent.setAttribute('hidden', '');
  });
  await waitForImages(page);
}

async function go(page, path, selector, timeout = 15000) {
  await page.goto(`${BASE_URL}${path}`, { waitUntil: 'domcontentloaded', timeout });
  if (selector) await page.waitForSelector(selector, { timeout });
  await pause(400);
  await prepare(page);
}

async function capture(page, name) {
  await prepare(page);
  const filepath = join(OUTPUT_DIR, `${name}.png`);
  await page.screenshot({ path: filepath, fullPage: false });
  captured.push(`${name}.png`);
  logStep(`saved ${name}.png`);
  return filepath;
}

async function clickTabByText(page, text) {
  await page.evaluate((label) => {
    const btn = Array.from(document.querySelectorAll('button')).find((b) =>
      (b.textContent || '').trim().includes(label),
    );
    btn?.click();
  }, text);
  await pause(400);
}

async function runShot(name, fn) {
  try {
    logStep(name);
    await fn();
  } catch (err) {
    console.warn(`  skip ${name}: ${err.message}`);
  }
}

async function run() {
  ensureDir(OUTPUT_DIR);
  globalStart = Date.now();

  const chromePath =
    process.env.CHROME_PATH ||
    (process.platform === 'win32'
      ? 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'
      : undefined);

  const browser = await puppeteer.launch({
    headless: true,
    executablePath: chromePath,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-gpu'],
  });

  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
  page.setDefaultTimeout(20000);

  try {
    logStep(`login ${BASE_URL}`);
    await login(page);

    await runShot('home', async () => {
      await go(page, '/', 'form[action="/archive"]');
      await page.waitForFunction(
        () => {
          const stats = document.getElementById('home-stats');
          return stats && /VIDEOS/i.test(stats.textContent || '');
        },
        { timeout: 12000 },
      );
      await pause(800);
      await capture(page, 'home');
    });

    await runShot('jobs', async () => {
      await go(page, '/jobs', '#jobs-list');
      await pause(400);
      await capture(page, 'jobs');
      const href = await page.evaluate(() => {
        const a = document.querySelector('#jobs-list a[href^="/jobs/"]');
        return a?.getAttribute('href') || '';
      });
      if (href) {
        await go(page, href, '#job-detail-card');
        await capture(page, 'job-detail');
      }
    });

    await runShot('videos', async () => {
      await go(page, '/videos', '#videos-grid a');
      await page.evaluate(() => {
        const btns = Array.from(document.querySelectorAll('[data-on\\:click], button, a'));
        const btn = btns.find((b) => b.textContent?.trim() === '48');
        btn?.click();
      });
      await pause(800);
      await waitForImages(page, 10000);
      await capture(page, 'videos');
      await page.evaluate(() => window.scrollTo({ top: 720, behavior: 'instant' }));
      await pause(300);
      await waitForImages(page, 6000);
      await capture(page, 'videos-scrolled');
    });

    await runShot('channels', async () => {
      await go(page, '/channels', '#channels-list', 20000);
      await page.waitForFunction(
        () => (document.querySelectorAll('#channels-list a').length || 0) > 3,
        { timeout: 15000 },
      );
      await waitForImages(page, 12000);
      await page.waitForFunction(
        () => {
          const imgs = Array.from(document.querySelectorAll('#channels-list img[src]'));
          const ready = imgs.filter((img) => img.complete && img.naturalWidth > 0);
          return ready.length >= Math.min(6, imgs.length);
        },
        { timeout: 15000 },
      );
      await pause(400);
      await capture(page, 'channels');
    });

    await runShot('channel-view', async () => {
      const path = `/channels/view?name=${encodeURIComponent(TARGET_CHANNEL_NAME)}`;
      await go(page, path, 'h1');
      await pause(800);
      await waitForImages(page, 8000);
      await capture(page, 'channel-view');
    });

    await runShot('creators', async () => {
      await go(page, '/creators', 'h1');
      await pause(600);
      await waitForImages(page, 8000);
      await capture(page, 'creators');
    });

    await runShot('creator-view', async () => {
      await go(page, `/creators/${TARGET_CREATOR_ID}`, 'h1');
      await pause(800);
      await waitForImages(page, 8000);
      await capture(page, 'creator-view');
    });

    await runShot('follows', async () => {
      await go(page, '/follows');
      await pause(500);
      await capture(page, 'follows');
    });

    await runShot('network', async () => {
      await page.goto(`${BASE_URL}/network`, { waitUntil: 'domcontentloaded', timeout: 90000 });
      await page.waitForSelector('#network-graph svg circle', { timeout: 45000 });
      await pause(4000);
      await capture(page, 'network');
    });

    await runShot('wiki', async () => {
      await go(page, '/wiki', 'h1');
      await pause(600);
      await capture(page, 'wiki');
    });

    await runShot('wiki-page', async () => {
      await go(page, TARGET_WIKI_PAGE, 'h1');
      await pause(600);
      await capture(page, 'wiki-page');
    });

    await runShot('stitch', async () => {
      await go(page, '/stitch?user=all', 'a[href^="/stitch/"]');
      await pause(700);
      await waitForImages(page, 8000);
      await capture(page, 'stitch');
    });

    await runShot('stitch-editor', async () => {
      await go(page, `/stitch/${TARGET_STITCH_ID}`, '[data-stitch-workspace]');
      await page.waitForFunction(
        () => {
          const el = document.querySelector('[data-sync-state]');
          const text = (el?.textContent || '').trim();
          return text && !/loading/i.test(text);
        },
        { timeout: 20000 },
      );
      await pause(1200);
      await capture(page, 'stitch-editor');
    });

    await runShot('show-notes', async () => {
      await go(page, '/show-notes', 'h1');
      await pause(500);
      await capture(page, 'show-notes');
    });

    await runShot('show-notes-editor', async () => {
      await go(page, `/show-notes/${TARGET_SHOW_NOTE_ID}`);
      await pause(1500);
      await capture(page, 'show-notes-editor');
    });

    await runShot('visual', async () => {
      await go(page, '/visual', 'h1');
      await pause(400);
      await capture(page, 'visual');
    });

    await runShot('video-detail', async () => {
      await go(page, `/videos/${TARGET_VIDEO_ID}`, '#videoPlayer, [data-watch-page]');
      await pause(800);
      await clickTabByText(page, 'Transcript');
      await pause(800);
      await waitForImages(page, 8000);
      await capture(page, 'video-detail');
    });

    await runShot('video-detail-search', async () => {
      await page.waitForSelector('[data-transcript-search]');
      await page.click('[data-transcript-search]', { clickCount: 3 });
      await page.type('[data-transcript-search]', TRANSCRIPT_QUERY, { delay: 20 });
      await pause(700);
      await capture(page, 'video-detail-search');
    });

    await runShot('video-detail-context', async () => {
      await clickTabByText(page, 'Context');
      await pause(1200);
      await capture(page, 'video-detail-context');
    });

    await runShot('cut-editor', async () => {
      await go(page, `/videos/${TARGET_VIDEO_ID}/cut`, '[data-cut-page]');
      await page.waitForFunction(
        () =>
          window.cutEditor &&
          Number.isFinite(window.cutEditor.duration) &&
          window.cutEditor.duration > 0,
        { timeout: 20000 },
      );
      await pause(600);
      await capture(page, 'cut-editor');

      const clipSel = await page.$('[data-clip-row][data-clip-id]');
      if (clipSel) {
        await clipSel.click();
        await pause(800);
        await capture(page, 'cut-editor-clip');
        const exportBtn = await page.$('#cut-export-panel button[data-on\\:click*="exports"]');
        if (exportBtn) {
          await exportBtn.click();
          await pause(600);
        }
        await capture(page, 'cut-editor-export');
      }

      await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('button'));
        const filterBtn = buttons.find(
          (b) => b.textContent?.includes('COLOR') && b.textContent?.includes('FILTERS'),
        );
        filterBtn?.click();
      });
      await pause(400);
      const filterDropdown = await page.$('#filter-stack details summary');
      if (filterDropdown) {
        await filterDropdown.click();
        await pause(250);
      }
      await capture(page, 'cut-editor-filters');
    });

    await runShot('settings', async () => {
      await go(page, '/settings', 'form');
      await page.evaluate(() => {
        document.querySelectorAll('input, textarea, code, pre, span').forEach((el) => {
          const val = el.value || el.textContent || '';
          if (/rw_[A-Za-z0-9_-]{8,}/.test(val)) {
            if ('value' in el) el.value = 'rw_••••••••';
            if (el.textContent) el.textContent = el.textContent.replace(/rw_[A-Za-z0-9_-]+/g, 'rw_••••••••');
          }
        });
        const headings = Array.from(document.querySelectorAll('h2, h3, legend, .card-header, p, span, div'));
        const mcp = headings.find((el) => /^\s*MCP\s*$/.test((el.textContent || '').trim()));
        mcp?.scrollIntoView({ block: 'center' });
      });
      await pause(250);
      await capture(page, 'settings');
    });

    await runShot('keybindings', async () => {
      await go(page, '/settings/keybindings', '#keybinding-settings');
      await capture(page, 'keybindings');
    });

    await runShot('admin', async () => {
      await go(page, '/admin');
      await pause(1000);
      await capture(page, 'admin');
    });

    const manifest = {
      baseUrl: BASE_URL,
      capturedAt: new Date().toISOString(),
      elapsedMs: Date.now() - globalStart,
      images: captured,
    };
    writeFileSync(join(OUTPUT_DIR, 'manifest.json'), JSON.stringify(manifest, null, 2));
    const totalSec = ((Date.now() - globalStart) / 1000).toFixed(1);
    console.log(`\n${captured.length} screenshots in ${totalSec}s -> ${OUTPUT_DIR}`);
  } finally {
    await browser.close();
  }
}

run().catch((err) => {
  console.error(`\n${err.message}`);
  process.exit(1);
});
