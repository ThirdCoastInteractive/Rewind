#!/usr/bin/env node
/**
 * Stills + short clips for the 0.0.3 tweet thread.
 * Read-only: no follows, no index jobs, no tokens, no suggestion accept.
 *
 *   BASE_URL=http://localhost:9115 node scripts/capture-tweet-media.js
 *
 * Writes screenshots/tweet/*.png and *.webm
 */
import puppeteer from 'puppeteer';
import { readFileSync, mkdirSync } from 'fs';
import { join } from 'path';

function loadEnv() {
  try {
    for (const line of readFileSync('.env', 'utf8').split(/\r?\n/)) {
      const m = line.match(/^([A-Z0-9_]+)=(.*)$/);
      if (m && process.env[m[1]] === undefined) process.env[m[1]] = m[2];
    }
  } catch {
    /* optional */
  }
}
loadEnv();

const BASE_URL = process.env.BASE_URL || 'http://localhost:9115';
const USERNAME =
  process.env.TEST_USERNAME || process.env.SITESPEED_TEST_USER || 'admin';
const PASSWORD =
  process.env.TEST_PASSWORD || process.env.SITESPEED_TEST_PASSWORD || '';
const VIDEO_ID =
  process.env.TARGET_VIDEO_ID || '27666716-eb50-5a31-b90e-55b7a412fdfe';
const OUT = join(process.cwd(), 'screenshots', 'tweet');
const CHROME =
  process.env.CHROME_PATH ||
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';

const pause = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (m) => console.log(`  ${m}`);

async function login(page) {
  if (!PASSWORD) throw new Error('Need TEST_PASSWORD or SITESPEED_TEST_PASSWORD');
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle2' });
  await page.waitForSelector('input[name="username"]');
  await page.type('input[name="username"]', USERNAME, { delay: 8 });
  await page.type('input[name="password"]', PASSWORD, { delay: 8 });
  await Promise.all([
    page.waitForNavigation({ waitUntil: 'load' }),
    page.click('button[type="submit"]'),
  ]);
  if (page.url().includes('/login')) throw new Error('Login failed');
}

async function go(page, path, selector, timeout = 15000) {
  await page.goto(`${BASE_URL}${path}`, { waitUntil: 'load' });
  if (selector) await page.waitForSelector(selector, { timeout });
  await pause(400);
}

async function shot(page, name) {
  const path = join(OUT, `${name}.png`);
  await page.screenshot({ path, fullPage: false });
  log(`png ${name}`);
  return path;
}

async function record(page, name, fn) {
  const path = join(OUT, `${name}.webm`);
  const rec = await page.screencast({ path, fps: 30 });
  await fn();
  await rec.stop();
  log(`webm ${name}`);
  return path;
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const browser = await puppeteer.launch({
    headless: true,
    executablePath: CHROME,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-gpu'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080, deviceScaleFactor: 1 });

  try {
    log('login');
    await login(page);

    log('channels');
    await go(page, '/channels', '#channels-list a, #channels-list .font-mono', 20000);
    await pause(900);
    await shot(page, 'channels');

    const channelHref = await page.evaluate(() => {
      const a = document.querySelector('a[href^="/channels/view"]');
      return a ? a.getAttribute('href') : null;
    });
    if (channelHref) {
      log('channel view');
      await go(page, channelHref, 'h1');
      await pause(600);
      await shot(page, 'channel-view');
    }

    log('creators');
    await go(page, '/creators', 'h1');
    await pause(500);
    await shot(page, 'creators');

    const creatorHref = await page.evaluate(() => {
      const links = Array.from(document.querySelectorAll('a[href^="/creators/"]'));
      const a = links.find((el) => {
        const href = el.getAttribute('href') || '';
        return href !== '/creators/new' && /^\/creators\/[0-9a-f-]+$/i.test(href);
      });
      return a ? a.getAttribute('href') : null;
    });
    if (creatorHref) {
      log('creator detail');
      await go(page, creatorHref, 'h1');
      await pause(500);
      await shot(page, 'creator-detail');
    }

    log('follows');
    await go(page, '/follows', 'h1');
    await pause(500);
    await shot(page, 'follows');

    log('network');
    await go(page, '/network', '#network-graph svg circle', 25000);
    // Still before fitGraph (tick 80) packs labels too small for a tweet.
    await pause(2200);
    await shot(page, 'network');
    await pause(2500);

    await record(page, 'network', async () => {
      await pause(5000);
      const box = await page.$eval('#network-graph svg', (svg) => {
        const circles = Array.from(svg.querySelectorAll('circle'));
        const mid = svg.getBoundingClientRect();
        const cx = mid.x + mid.width / 2;
        const cy = mid.y + mid.height / 2;
        let best = circles[0];
        let bestD = Infinity;
        for (const c of circles) {
          const r = c.getBoundingClientRect();
          const d =
            (r.x + r.width / 2 - cx) ** 2 + (r.y + r.height / 2 - cy) ** 2;
          if (d < bestD) {
            bestD = d;
            best = c;
          }
        }
        const r = best.getBoundingClientRect();
        return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
      });
      await page.mouse.click(box.x, box.y);
      await pause(3500);
    });
    await pause(400);
    await shot(page, 'network-inspector');

    log('mcp');
    await go(page, '/settings', 'form');
    await page.evaluate(() => {
      const el = Array.from(document.querySelectorAll('h2, h3, p, span, div, legend')).find(
        (n) => /^\s*MCP\s*$/.test((n.textContent || '').trim())
      );
      el?.scrollIntoView({ block: 'center' });
    });
    await pause(300);
    const mcp = await page.evaluateHandle(() => {
      const h = Array.from(document.querySelectorAll('*')).find(
        (n) => (n.textContent || '').trim() === 'MCP'
      );
      let n = h;
      for (let i = 0; i < 10 && n; i++) {
        if ((n.innerText || '').includes('CREATE TOKEN')) return n;
        n = n.parentElement;
      }
      return h;
    });
    const mcpEl = mcp.asElement();
    if (mcpEl) {
      await mcpEl.screenshot({ path: join(OUT, 'mcp.png') });
      log('png mcp');
    } else {
      await shot(page, 'mcp');
    }

    log('comment search');
    await go(page, `/videos/${VIDEO_ID}`, '#comments-section-inner, [data-bind="commentSearch"]', 20000);
    await page.evaluate(() => {
      document.querySelector('#comments-section-inner, [data-bind="commentSearch"]')
        ?.scrollIntoView({ block: 'center' });
    });
    await pause(400);
    const searchSel = '[data-bind="commentSearch"]';
    if (await page.$(searchSel)) {
      await record(page, 'comments', async () => {
        await page.click(searchSel);
        await pause(200);
        await page.type(searchSel, 'the', { delay: 60 });
        await pause(2200);
      });
      await shot(page, 'comments-search');
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
