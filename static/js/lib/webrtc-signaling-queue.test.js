import test from 'node:test';
import assert from 'node:assert/strict';
import { createSignalingQueue } from './webrtc-signaling-queue.js';

test('holds candidates received before the first offer', async () => {
  const events = [];
  const queue = createSignalingQueue({
    onOffer: async () => events.push('offer'),
    onCandidate: async (message) => events.push(`candidate:${message.data}`),
  });

  queue.push(JSON.stringify({ event: 'candidate', data: 'early' }));
  queue.push(JSON.stringify({ event: 'offer', data: '{}' }));
  await queue.whenIdle();

  assert.deepEqual(events, ['offer', 'candidate:early']);
});

test('serializes rapid offers and answers in order', async () => {
  const events = [];
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const queue = createSignalingQueue({
    onOffer: async (message) => {
      events.push(`start:${message.data}`);
      if (message.data === 'one') await gate;
      events.push(`done:${message.data}`);
    },
  });

  queue.push({ event: 'offer', data: 'one' });
  queue.push({ event: 'offer', data: 'two' });
  await Promise.resolve();
  assert.deepEqual(events, ['start:one']);
  release();
  await queue.whenIdle();

  assert.deepEqual(events, ['start:one', 'done:one', 'start:two', 'done:two']);
});
