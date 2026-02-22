#!/usr/bin/env node

import { readFileSync, mkdirSync, writeFileSync, existsSync } from 'fs';
import { join } from 'path';
import { spawnSync } from 'child_process';

const OUTPUT_DIR = join(process.cwd(), 'screenshots', 'readme');
const CLIPS_DIR = join(OUTPUT_DIR, 'video-clips');
const MANIFEST_PATH = join(CLIPS_DIR, 'manifest.json');
const STITCH_DIR = join(CLIPS_DIR, 'stitch');
const OUTPUT_FILE = join(OUTPUT_DIR, 'readme-demo.mp4');

function ensureDir(p) {
  mkdirSync(p, { recursive: true });
}

function run(cmd, args) {
  console.log(`  → ${cmd} ${args.join(' ').slice(0, 120)}…`);
  const r = spawnSync(cmd, args, { stdio: 'inherit' });
  if (r.status !== 0) throw new Error(`${cmd} failed (exit ${r.status}).`);
}

/** Use ffprobe to get the duration (seconds) of a video file. */
function probeDuration(file) {
  // Try stream-level duration first
  let r = spawnSync(
    'ffprobe',
    ['-v', 'error', '-select_streams', 'v:0', '-show_entries', 'stream=duration', '-of', 'csv=p=0', file],
    { encoding: 'utf8' }
  );
  let d = parseFloat(r.stdout?.trim());
  if (d > 0) return d;

  // Fallback: format-level duration (works for containers without per-stream info)
  r = spawnSync(
    'ffprobe',
    ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', file],
    { encoding: 'utf8' }
  );
  d = parseFloat(r.stdout?.trim());
  if (d > 0) return d;

  // Last resort: count packets (slow but always works)
  r = spawnSync(
    'ffprobe',
    ['-v', 'error', '-count_packets', '-select_streams', 'v:0',
     '-show_entries', 'stream=nb_read_packets,r_frame_rate', '-of', 'csv=p=0', file],
    { encoding: 'utf8' }
  );
  const parts = r.stdout?.trim().split(',') || [];
  if (parts.length >= 2) {
    const [frac, packets] = [parts[0], parseInt(parts[1])];
    const [num, den] = frac.split('/').map(Number);
    if (num && den && packets) return packets / (num / den);
  }
  return 0;
}

function getFontFile() {
  if (process.platform === 'win32') return 'C\\:/Windows/Fonts/arial.ttf';
  if (process.platform === 'darwin') return '/System/Library/Fonts/Supplemental/Arial.ttf';
  return '/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf';
}

/** Escape special characters for ffmpeg drawtext values. */
function esc(text) {
  return text.replace(/:/g, '\\:').replace(/'/g, "\\'");
}

function main() {
  if (!existsSync(MANIFEST_PATH)) {
    throw new Error(`Missing manifest at ${MANIFEST_PATH}. Run pnpm video:readme first.`);
  }

  ensureDir(STITCH_DIR);

  const manifest = JSON.parse(readFileSync(MANIFEST_PATH, 'utf8'));
  const font = getFontFile();
  const fps = manifest.fps || 30;

  // Title timing (seconds)
  const titleFadeIn = 0.4;
  const titleHold = 1.8;
  const titleFadeOut = 0.4;
  const titleEnd = titleFadeIn + titleHold + titleFadeOut; // 2.6 s

  // Clip transition fades (seconds)
  const clipFadeDur = 0.25;

  const parts = [];

  for (const clip of manifest.clips || []) {
    const inPath = join(OUTPUT_DIR, clip.path);
    const outPath = join(STITCH_DIR, `${clip.name}.mp4`);
    const dur = probeDuration(inPath);
    const title = esc(clip.title || clip.name);

    console.log(`\n📎 ${clip.name}  (${dur.toFixed(2)}s)`);

    // Title alpha envelope: fade-in → hold → fade-out → hidden
    const alpha = [
      `if(lt(t,${titleFadeIn}),t/${titleFadeIn}`,
      `if(lt(t,${titleFadeIn + titleHold}),1`,
      `if(lt(t,${titleEnd}),(${titleEnd}-t)/${titleFadeOut}`,
      `0)))`,
    ].join(',');

    // Video-level fades (skip fade-out if we couldn't determine duration)
    const filters = [`fade=t=in:st=0:d=${clipFadeDur}`];
    if (dur > clipFadeDur * 2) {
      const fadeOutStart = Math.max(0, dur - clipFadeDur);
      filters.push(`fade=t=out:st=${fadeOutStart.toFixed(3)}:d=${clipFadeDur}`);
    }
    filters.push(
      `drawtext=fontfile=${font}:text='${title}':x=(w-text_w)/2:y=h*0.08:fontsize=48:fontcolor=white:alpha='${alpha}':box=1:boxcolor=black@0.55:boxborderw=14`
    );

    const vf = filters.join(',');

    run('ffmpeg', [
      '-y',
      '-i', inPath,
      '-vf', vf,
      '-r', String(fps),
      '-c:v', 'libx264',
      '-preset', 'medium',
      '-crf', '20',
      '-pix_fmt', 'yuv420p',
      '-an',
      outPath,
    ]);

    parts.push(outPath);
  }

  // Concat all processed clips
  const list = parts.map((p) => `file '${p.replace(/'/g, "'\\''")}'`).join('\n');
  const listPath = join(STITCH_DIR, 'concat.txt');
  writeFileSync(listPath, list);

  console.log('\n🎞️  Concatenating…');
  run('ffmpeg', ['-y', '-f', 'concat', '-safe', '0', '-i', listPath, '-c', 'copy', OUTPUT_FILE]);

  console.log(`\n✅ Stitched ${parts.length} clips → ${OUTPUT_FILE}`);
}

try {
  main();
} catch (err) {
  console.error(`\n❌ ${err.message}`);
  process.exit(1);
}
