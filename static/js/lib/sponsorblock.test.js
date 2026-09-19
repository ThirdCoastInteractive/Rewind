import test from 'node:test';
import assert from 'node:assert/strict';
import { isSkipSegment } from './sponsorblock.js';

test('chapter ranges and user markers never become automatic skips', () => {
  const chapter = { timestamp: 0, duration: 219, marker_type: 'chapter' };
  assert.equal(isSkipSegment(chapter), false);
  assert.equal(isSkipSegment({ ...chapter, source: 'sponsorblock', action_type: 'chapter' }), false);
  assert.equal(isSkipSegment({ ...chapter, source: 'sponsorblock', action_type: 'skip' }), false);
  assert.equal(isSkipSegment({ timestamp: 30, duration: 20, title: '[Skip] Sponsor', source: 'archive' }), false);
});

test('only valid, explicit SponsorBlock skip actions qualify', () => {
  const segment = { source: 'sponsorblock', action_type: 'skip', category: 'sponsor', timestamp: 30, duration: 20 };
  assert.equal(isSkipSegment(segment), true);
  for (const change of [
    { action_type: 'mute' }, { action_type: '' }, { category: 'chapter' },
    { duration: 0 }, { duration: -1 }, { duration: Infinity },
    { timestamp: -1 }, { timestamp: NaN }, { duration: '20' }, { source: undefined },
  ]) assert.equal(isSkipSegment({ ...segment, ...change }), false, JSON.stringify(change));
});
