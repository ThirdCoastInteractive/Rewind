import test from 'node:test';
import assert from 'node:assert/strict';
import { SaveQueue } from './save-queue.js';

test('leaving before debounce saves the latest edit', async () => {
  const writes = [];
  const queue = new SaveQueue(async value => writes.push(value));
  queue.schedule({ title: 'first' });
  queue.schedule({ title: 'last' });
  await queue.flush();
  assert.deepEqual(writes, [{ title: 'last' }]);
  assert.equal(queue.dirty, false);
});

test('edits during a slow save are serialized and included in the flush', async () => {
  let release;
  const writes = [];
  const queue = new SaveQueue(async value => {
    writes.push(value);
    if (writes.length === 1) await new Promise(resolve => { release = resolve; });
  });
  queue.schedule({ title: 'first' });
  const flush = queue.flush();
  await Promise.resolve();
  queue.schedule({ title: 'second' });
  release();
  await flush;
  assert.deepEqual(writes, [{ title: 'first' }, { title: 'second' }]);
  assert.equal(queue.dirty, false);
  queue.dispose();
});

test('failed saves keep edits for retry and reject navigation flush', async () => {
  let fail = true;
  const queue = new SaveQueue(async () => { if (fail) throw new Error('offline'); });
  queue.schedule({ title: 'keep me' });
  await assert.rejects(queue.flush(), /offline/);
  assert.equal(queue.dirty, true);
  fail = false;
  await queue.flush();
  assert.equal(queue.dirty, false);
});
