import test from 'node:test';
import assert from 'node:assert/strict';
import { MangaWindow } from './mangaWindow.ts';

test('mixed page heights keep page-relative scroll coordinates', () => {
  const pages = new MangaWindow(200);
  pages.measure(0, 800, new Set()); pages.measure(1, 1200, new Set());
  assert.deepEqual(pages.locate(1400), { row: 1, ratio: 0.5 });
  const anchor = pages.locate(2500);
  pages.measure(0, 1600, new Set());
  assert.deepEqual(pages.locate(pages.offset(anchor)), anchor);
});

test('long books keep geometry and rendered windows bounded', () => {
  const pages = new MangaWindow(20000);
  for (let index = 0; index < 500; index++) pages.measure(index, 1000000, new Set());
  assert.ok(pages.total() < 12000000);
  assert.equal(pages.height(0), 320);
  for (const row of [0, 50, 19999]) { const window = pages.window(row); assert.ok(window.end - window.start <= 12); assert.ok(window.start <= row && window.end > row); }
  assert.deepEqual(pages.locate(Infinity), { row: 0, ratio: 0 });
});

test('active rows survive eviction and invalid dimensions do not corrupt sums', () => {
  const pages = new MangaWindow(200);
  pages.measure(0, 800, new Set([0]));
  for (let index = 1; index < 100; index++) pages.measure(index, 900, new Set([0]));
  assert.equal(pages.height(0), 800);
  const before = pages.total();
  pages.measure(3, NaN, new Set());
  assert.equal(pages.total(), before);
  assert.throws(() => new MangaWindow(20001));
});
