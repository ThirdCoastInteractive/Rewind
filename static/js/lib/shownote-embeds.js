const YOUTUBE_HOSTS = new Set(['youtube.com', 'www.youtube.com', 'm.youtube.com', 'youtu.be']);
const MAX_TIMESTAMP = 24 * 60 * 60;

/** Parse a show-note timestamp into seconds. Ranges return both endpoints. */
export function parseTimestampSpec(value) {
  const text = String(value || '').trim().replace(/[–—]/g, '-');
  if (!text) return null;
  const match = text.match(/^(\d+(?::\d{1,2}){0,2})(?:\s*[-/]\s*(\d+(?::\d{1,2}){0,2}))?$/);
  if (!match) return null;
  const parse = (part) => {
    const fields = part.split(':').map(Number);
    if (fields.some((field) => !Number.isInteger(field) || field < 0) || fields.slice(1).some((field) => field > 59)) return null;
    const seconds = fields.reduce((total, field) => total * 60 + field, 0);
    return Number.isFinite(seconds) && seconds <= MAX_TIMESTAMP ? seconds : null;
  };
  const start = parse(match[1]);
  const end = match[2] ? parse(match[2]) : null;
  if (start === null || (match[2] && end === null) || (end !== null && end < start)) return null;
  return { start, end };
}

function queryTime(url) {
  const value = url.searchParams.get('t') || url.searchParams.get('start');
  if (!value) return null;
  if (/^\d+(?:\.\d+)?s?$/.test(value)) {
    const seconds = Number.parseFloat(value);
    return Number.isFinite(seconds) && seconds <= MAX_TIMESTAMP ? seconds : null;
  }
  const units = value.match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/i);
  if (units && units[0] && units[1] + units[2] + units[3]) {
    const seconds = Number(units[1] || 0) * 3600 + Number(units[2] || 0) * 60 + Number(units[3] || 0);
    return Number.isFinite(seconds) && seconds <= MAX_TIMESTAMP ? seconds : null;
  }
  return parseTimestampSpec(value)?.start ?? null;
}

/** Validate a YouTube URL and return its canonical video id and start time. */
export function parseYouTubeURL(rawURL) {
  let url;
  try { url = new URL(rawURL); } catch (_) { return null; }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !YOUTUBE_HOSTS.has(url.hostname.toLowerCase())) return null;
  let videoId = '';
  const path = url.pathname.replace(/^\/+|\/+$/g, '').split('/');
  if (url.hostname.toLowerCase() === 'youtu.be' && path.length === 1) videoId = path[0] || '';
  else if (path[0] === 'watch' && path.length === 1) videoId = url.searchParams.get('v') || '';
  else if (['shorts', 'live'].includes(path[0]) && path.length === 2) videoId = path[1] || '';
  if (!/^[A-Za-z0-9_-]{11}$/.test(videoId)) return null;
  const fragment = url.hash.match(/^#t=(.+)$/i)?.[1];
  const start = queryTime(url) ?? (fragment ? parseTimestampSpec(fragment)?.start : null) ?? 0;
  return { videoId, start, url: url.href };
}

/** Parse a Markdown/plain show-note reference, including provider timestamps. */
export function parseShowNoteReference(line) {
  const match = line.match(/^\s*(?:(?:\d+[.)]|[-+*])\s+)?\[([^\]]*)\]\(([^)\s]+)\)(.*)$/) ||
    line.match(/^\s*(?:(?:\d+[.)]|[-+*])\s+)?(https?:\/\/\S+)(.*)$/);
  if (!match) return null;
  const markdownLink = match.length === 4;
  const label = match[1];
  const uri = markdownLink ? match[2] : match[1];
  const suffix = markdownLink ? match[3] : match[2];
  const internal = uri.match(/^rewind:\/\/(video|clip|marker)\/([^/]+)$/);
  const relativeVideo = uri.match(/^\/(?:videos?|watch)\/([^/]+)/);
  const relativeClip = uri.match(/^\/(?:clips?|videos\/[^/]+\/clips?)\/([^/]+)/);
  const relativeMarker = uri.match(/^\/(?:markers?|videos\/[^/]+\/markers?)\/([^/]+)/);
  let kind = 'external';
  let id = '';
  if (internal) [kind, id] = [internal[1], internal[2]];
  else if (relativeClip) [kind, id] = ['clip', relativeClip[1]];
  else if (relativeMarker) [kind, id] = ['marker', relativeMarker[1]];
  else if (relativeVideo) [kind, id] = ['video', relativeVideo[1]];
  const explicit = (suffix.match(/@\s*([^\n]+)/) || [])[1] || '';
  const youtube = parseYouTubeURL(uri);
  const timestamp = explicit ? parseTimestampSpec(explicit) : null;
  if (youtube) {
    return { label, uri, time: explicit, kind: 'youtube', id: youtube.videoId, external: true,
      provider: 'youtube', providerURL: youtube.url, start: timestamp?.start ?? youtube.start,
      end: timestamp?.end ?? null, videoId: youtube.videoId };
  }
  return { label, uri, time: explicit, kind, id, external: kind === 'external' };
}

/** Parse an indented timestamp cue that belongs to the immediately preceding provider reference. */
export function parseTimestampCue(line) {
  const match = String(line || '').match(/^\s{2,}(?:[-*]\s*)?((?:\d+:)?\d{1,2}:\d{2}(?:\s*[-–—/]\s*(?:\d+:)?\d{1,2}:\d{2})?)\b(.*)$/);
  if (!match) return null;
  const timestamp = parseTimestampSpec(match[1]);
  return timestamp ? { ...timestamp, time: match[1], label: match[2].trim() } : null;
}

/** Render a seek-only cue bound to its preceding YouTube reference. */
export function renderYouTubeCue(reference, cue) {
  const card = document.createElement('div'); card.className = 'sn-youtube-cue';
  const button = document.createElement('button'); button.type = 'button'; button.textContent = `${cue.time}${cue.label ? ` · ${cue.label}` : ''}`;
  button.addEventListener('click', () => window.dispatchEvent(new CustomEvent('rewind:youtube-seek', { detail: { sourceKey: reference.sourceKey, start: cue.start, end: cue.end } })));
  card.appendChild(button); return card;
}

let apiPromise;
function youtubeAPI() {
  if (window.YT?.Player) return Promise.resolve(window.YT);
  if (apiPromise) return apiPromise;
  apiPromise = new Promise((resolve, reject) => {
    const previous = window.onYouTubeIframeAPIReady;
    window.onYouTubeIframeAPIReady = () => { previous?.(); resolve(window.YT); };
    const script = document.createElement('script');
    script.src = 'https://www.youtube.com/iframe_api';
    script.async = true;
    const fail = () => { apiPromise = null; reject(new Error('YouTube player unavailable')); };
    script.onerror = fail;
    window.setTimeout(() => { if (!window.YT?.Player) fail(); }, 8000);
    document.head.appendChild(script);
  });
  return apiPromise;
}

/** Render a safe YouTube embed and expose timestamp seek/copy controls. */
export function renderYouTubeEmbed(reference) {
  const card = document.createElement('div');
  card.className = 'sn-youtube-embed';
  card.dataset.videoId = reference.videoId;
  const frame = document.createElement('div');
  frame.className = 'sn-youtube-frame';
  const mount = document.createElement('div');
  mount.className = 'sn-youtube-mount';
  frame.appendChild(mount);
  card.appendChild(frame);
  const controls = document.createElement('div');
  controls.className = 'sn-youtube-actions';
  const jump = document.createElement('input');
  jump.type = 'text'; jump.inputMode = 'numeric'; jump.placeholder = 'M:SS'; jump.value = formatTimestamp(reference.start); jump.disabled = true; jump.setAttribute('aria-label', 'Jump to time');
  const seek = document.createElement('button'); seek.type = 'button'; seek.textContent = 'Jump'; seek.disabled = true;
  const copy = document.createElement('button'); copy.type = 'button'; copy.textContent = 'Copy current time'; copy.disabled = true;
  const copyLink = document.createElement('button'); copyLink.type = 'button'; copyLink.textContent = 'Copy timestamp link'; copyLink.disabled = true;
  controls.append(jump, seek, copy, copyLink);
  const status = document.createElement('span'); status.className = 'sn-youtube-status'; status.setAttribute('role', 'status'); controls.appendChild(status); card.appendChild(controls);
  const fallback = document.createElement('a');
  fallback.href = reference.providerURL; fallback.target = '_blank'; fallback.rel = 'noopener noreferrer';
  fallback.textContent = 'Open on YouTube'; card.appendChild(fallback);
  let player; let rangeTimer; let readyTimer; let disposed = false; let pendingSeek = null; let activeEnd = reference.end;
  const cueReference = () => player?.cueVideoById?.({ videoId: reference.videoId, startSeconds: reference.start, endSeconds: reference.end || undefined });
  const seekToInput = () => { const parsed = parseTimestampSpec(jump.value); if (!parsed) { status.textContent = 'Enter a valid M:SS timestamp'; return; } activeEnd = parsed.end; if (!player) { pendingSeek = parsed.start; status.textContent = 'Player is still loading'; return; } cueReference(); player.seekTo?.(parsed.start, true); status.textContent = ''; };
  seek.addEventListener('click', seekToInput);
  jump.addEventListener('keydown', (event) => { if (event.key === 'Enter') { event.preventDefault(); seekToInput(); } });
  const clipboard = async (text, button, label) => { try { if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable'); await navigator.clipboard.writeText(text); button.textContent = 'Copied'; } catch (_) { button.textContent = 'Copy unavailable'; } window.setTimeout(() => { button.textContent = label; }, 1400); };
  const currentTime = () => Math.max(0, Math.floor(player?.getCurrentTime?.() ?? reference.start));
  copy.addEventListener('click', () => { const value = formatTimestamp(currentTime()); jump.value = value; clipboard(value, copy, 'Copy current time'); });
  copyLink.addEventListener('click', () => { const url = new URL(reference.providerURL); url.searchParams.set('t', `${currentTime()}s`); clipboard(url.href, copyLink, 'Copy timestamp link'); });
  const stopAtEnd = () => { if (player && activeEnd !== null && activeEnd !== undefined && player.getCurrentTime?.() >= activeEnd) { player.pauseVideo?.(); cueReference(); window.clearInterval(rangeTimer); } };
  const unavailable = () => { window.clearInterval(rangeTimer); fallback.hidden = false; frame.hidden = true; player?.getIframe?.()?.setAttribute('hidden', ''); jump.disabled = true; seek.disabled = true; copy.disabled = true; copyLink.disabled = true; status.textContent = 'YouTube embed unavailable'; };
  youtubeAPI().then((YT) => {
    if (disposed) return;
    player = new YT.Player(mount, { videoId: reference.videoId, playerVars: { controls: 1, rel: 0, autoplay: 0, origin: window.location.origin },
      events: { onReady: () => { window.clearTimeout(readyTimer); jump.disabled = false; seek.disabled = false; copy.disabled = false; copyLink.disabled = false; cueReference(); if (pendingSeek !== null) { player.seekTo?.(pendingSeek, true); pendingSeek = null; } }, onStateChange: (event) => { window.clearInterval(rangeTimer); if (event.data === YT.PlayerState.PLAYING) rangeTimer = window.setInterval(stopAtEnd, 200); }, onError: unavailable } });
    readyTimer = window.setTimeout(() => { if (!disposed && jump.disabled) unavailable(); }, 10000);
  }).catch(unavailable);
  const seekListener = (event) => { if (event.detail?.sourceKey !== reference.sourceKey) return; activeEnd = Object.prototype.hasOwnProperty.call(event.detail, 'end') ? event.detail.end : reference.end; if (player) player.seekTo?.(event.detail.start, true); else { pendingSeek = event.detail.start; status.textContent = 'Player is still loading'; } };
  window.addEventListener('rewind:youtube-seek', seekListener);
  card.__rewindDestroy = () => { disposed = true; window.clearInterval(rangeTimer); window.clearTimeout(readyTimer); window.removeEventListener('rewind:youtube-seek', seekListener); player?.destroy?.(); };
  return card;
}

export function formatTimestamp(seconds) {
  const value = Math.max(0, Math.floor(Number(seconds) || 0));
  const h = Math.floor(value / 3600); const m = Math.floor((value % 3600) / 60); const s = value % 60;
  return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`;
}
