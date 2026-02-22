import fs from 'node:fs/promises';
import path from 'node:path';

const repoRoot = path.resolve(process.cwd());

const SRC_DIR = path.join(repoRoot, 'extensions', 'extension-common');
const FILES = ['rewind_common.js', 'popup_impl.js', 'options_impl.js', 'background_impl.js'];

const TARGET_DIRS = [
  path.join(repoRoot, 'extensions', 'chrome-extension-v3', 'common'),
  path.join(repoRoot, 'extensions', 'firefox-extension', 'common')
];

async function ensureDir(filePath) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
}

async function main() {
  for (const fileName of FILES) {
    const srcPath = path.join(SRC_DIR, fileName);
    const src = await fs.readFile(srcPath);

    for (const targetDir of TARGET_DIRS) {
      const dst = path.join(targetDir, fileName);
      await ensureDir(dst);
      await fs.writeFile(dst, src);
      process.stdout.write(`synced ${path.relative(repoRoot, dst)}\n`);
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
