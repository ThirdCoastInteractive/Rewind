/** Clamp a requested seek time, including unknown/live durations. */
export function clampSeek(time, duration) {
  return Number.isFinite(duration) && duration > 0 && Number.isFinite(time)
    ? Math.max(0, Math.min(duration, time)) : 0;
}

/** Find the next entry, or restart the current entry before going back. */
export function adjacentEntry(entries, time, direction) {
  if (direction > 0) return entries.find(entry => entry.start > time + 0.1) || null;
  return [...entries].reverse().find(entry => entry.start < time - 2) || entries[0] || null;
}

/** Resolve the entry at a time without filling gaps between explicit ranges. */
export function entryAtTime(entries, time) {
  return [...entries].reverse().find(entry => time >= entry.start && time < entry.end) || null;
}

/** Prefer a nested/short range over a parent window or full-episode clip. */
export function tightestEntryAtTime(entries, time) {
  let best = null;
  let bestDur = Infinity;
  for (const entry of entries) {
    if (time >= entry.start && time < entry.end) {
      const dur = entry.end - entry.start;
      if (dur < bestDur) {
        best = entry;
        bestDur = dur;
      }
    }
  }
  return best;
}
