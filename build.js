import esbuild from 'esbuild';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import https from 'https';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Ensure dist directory exists
const distDir = path.join(__dirname, 'static', 'dist');
if (!fs.existsSync(distDir)) {
  fs.mkdirSync(distDir, { recursive: true });
}

// Bundle video player
await esbuild.build({
  entryPoints: ['static/js/video-player.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/video-player.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built video-player.js');

// Bundle main.js
await esbuild.build({
  entryPoints: ['static/js/main.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/main.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built main.js');

// Bundle cut-page.js
await esbuild.build({
  entryPoints: ['static/js/cut-page.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/cut-page.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built cut-page.js');

// Bundle remote-player-background.js
await esbuild.build({
  entryPoints: ['static/js/remote-player-background.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/remote-player-background.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built remote-player-background.js');

// Bundle producer-scene-preview.js
await esbuild.build({
  entryPoints: ['static/js/producer-scene-preview.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/producer-scene-preview.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built producer-scene-preview.js');

// Bundle admin-dashboard.js (D3 charts for admin metrics)
await esbuild.build({
  entryPoints: ['static/js/admin-dashboard.js'],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/admin-dashboard.js',
  target: ['es2020'],
  format: 'iife'
});

console.log('✓ Built admin-dashboard.js');

// Minify video-player.css to dist
await esbuild.build({
  entryPoints: ['static/css/video-player.css'],
  minify: true,
  sourcemap: false,
  outfile: 'static/dist/video-player.css',
  loader: { '.css': 'css' }
});

console.log('✓ Minified video-player.css');

function downloadToFile(url, destPath) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(destPath);
    https
      .get(url, (res) => {
        // Follow redirects
        if (res.statusCode && res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          file.close();
          fs.unlink(destPath, () => {
            downloadToFile(res.headers.location, destPath).then(resolve).catch(reject);
          });
          return;
        }

        if (res.statusCode !== 200) {
          file.close();
          fs.unlink(destPath, () => {
            reject(new Error(`Download failed (${res.statusCode}) for ${url}`));
          });
          return;
        }

        res.pipe(file);
        file.on('finish', () => {
          file.close(resolve);
        });
      })
      .on('error', (err) => {
        file.close();
        fs.unlink(destPath, () => reject(err));
      });
  });
}

async function ensureDatastarBundle() {
  const dest = path.join(distDir, 'datastar.js');
  if (fs.existsSync(dest) && fs.statSync(dest).size > 0) {
    console.log('✓ Datastar bundle already present');
    return;
  }

  const url = 'https://cdn.jsdelivr.net/gh/starfederation/datastar@1.0.0-RC.7/bundles/datastar.js';
  await downloadToFile(url, dest);
  console.log('✓ Downloaded datastar.js');
}

function copyRecursiveSync(src, dest) {
  const stat = fs.statSync(src);
  if (stat.isDirectory()) {
    if (!fs.existsSync(dest)) {
      fs.mkdirSync(dest, { recursive: true });
    }
    for (const entry of fs.readdirSync(src)) {
      copyRecursiveSync(path.join(src, entry), path.join(dest, entry));
    }
    return;
  }
  fs.copyFileSync(src, dest);
}

function walkFilesSync(rootDir, onFile) {
  const entries = fs.readdirSync(rootDir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(rootDir, entry.name);
    if (entry.isDirectory()) {
      walkFilesSync(fullPath, onFile);
    } else if (entry.isFile()) {
      onFile(fullPath);
    }
  }
}

async function tryResolvePackageDir(pkgName) {
  try {
    const { createRequire } = await import('module');
    const require = createRequire(import.meta.url);
    const pkgJsonPath = require.resolve(`${pkgName}/package.json`, { paths: [__dirname] });
    return path.dirname(pkgJsonPath);
  } catch {
    return null;
  }
}

function findFirstMatchingFile(packageDir, matchers) {
  let found = null;
  walkFilesSync(packageDir, (fullPath) => {
    if (found) return;
    const normalized = fullPath.replace(/\\/g, '/');
    for (const matcher of matchers) {
      if (matcher.test(normalized)) {
        found = fullPath;
        return;
      }
    }
  });
  return found;
}

async function copyFontAwesomeAssets() {
  const distWebfontsDir = path.join(distDir, 'webfonts');
  const distFaCssDir = path.join(distDir, 'fontawesome');
  fs.mkdirSync(distFaCssDir, { recursive: true });

  function resolveWebfontsDir(packageDir) {
    const direct = path.join(packageDir, 'webfonts');
    if (fs.existsSync(direct)) return direct;

    const kitLayout = path.join(packageDir, 'icons', 'webfonts');
    if (fs.existsSync(kitLayout)) return kitLayout;

    return null;
  }

  const candidates = [
    {
      name: '@fortawesome/fontawesome-free',
      cssMatchers: [/\/css\/all\.min\.css$/, /\/css\/all\.css$/]
    }
  ];

  for (const candidate of candidates) {
    const packageDir = await tryResolvePackageDir(candidate.name);
    if (!packageDir) continue;

    const cssPath = findFirstMatchingFile(packageDir, candidate.cssMatchers);
    const webfontsPath = resolveWebfontsDir(packageDir);
    if (!cssPath || !webfontsPath) {
      continue;
    }

    fs.copyFileSync(cssPath, path.join(distFaCssDir, 'all.min.css'));
    copyRecursiveSync(webfontsPath, distWebfontsDir);
    console.log(`✓ Bundled Font Awesome assets from ${candidate.name}`);
    return;
  }

  console.warn('! Font Awesome assets not bundled (install @fortawesome/fontawesome-free)');
}

await copyFontAwesomeAssets();

await ensureDatastarBundle();
