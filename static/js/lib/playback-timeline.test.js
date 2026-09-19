import { test } from 'node:test';
import assert from 'node:assert/strict';
import { clampSeek, adjacentEntry, entryAtTime, tightestEntryAtTime } from './playback-timeline.js';

test('seeking clamps ends and rejects unknown duration', () => {
  assert.equal(clampSeek(-20, 120), 0);
  assert.equal(clampSeek(200, 120), 120);
  assert.equal(clampSeek(45.5, 120), 45.5);
  assert.equal(clampSeek(20, Infinity), 0);
  assert.equal(clampSeek(NaN, 120), 0);
});

const entries = [{start: 10, end: 20}, {start: 30, end: 60}, {start: 60, end: 90}];
test('navigation restarts an entry or moves to its neighbor at the boundary', () => {
  assert.equal(adjacentEntry(entries, 42, -1), entries[1]);
  assert.equal(adjacentEntry(entries, 30, -1), entries[0]);
  assert.equal(adjacentEntry(entries, 30, 1), entries[2]);
  assert.equal(adjacentEntry(entries, 80, 1), null);
  assert.equal(adjacentEntry([], 0, -1), null);
});
test('details preserve gaps and choose the following entry at shared boundaries', () => {
  assert.equal(entryAtTime(entries, 25), null);
  assert.equal(entryAtTime(entries, 60), entries[2]);
  assert.equal(entryAtTime(entries, 90), null);
  const overlapping = [{start: 0, end: 31}, {start: 30, end: 60}];
  assert.equal(entryAtTime(overlapping, 30), overlapping[1]);
});
test('list highlight prefers the nested range inside a parent window', () => {
  const parent = {start: 0, end: 600};
  const nested = {start: 90, end: 140};
  const other = {start: 200, end: 280};
  assert.equal(tightestEntryAtTime([parent, nested, other], 100), nested);
  assert.equal(tightestEntryAtTime([parent, nested, other], 10), parent);
  assert.equal(tightestEntryAtTime([parent, nested, other], 250), other);
  assert.equal(tightestEntryAtTime([parent, nested, other], 900), null);
});
