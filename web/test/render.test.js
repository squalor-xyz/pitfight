// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// The pure parts of render.js: layout, block owner and colors. Canvas drawing
// itself is checked by hand in a browser.

import test from 'node:test';
import assert from 'node:assert/strict';
import { layout, blockOwner, botColor, draw, EMPTY_COLOR, POINTER_COLOR } from '../js/render.js';
import { parseReplay } from '../js/replay.js';
import { Model } from '../js/model.js';
import { fixtureText } from './support.js';

test('layout: a core that fits gets one tile per cell, as large as possible', () => {
  const l = layout(24, 100, 50);
  assert.equal(l.block, 1);
  assert.ok(l.cols * Math.ceil(24 / l.cols) >= 24);
  assert.ok(Math.ceil(24 / l.cols) * l.size <= 50, 'rows fit the height');
  assert.ok(l.cols * l.size <= 100, 'cols fit the width');
  // One pixel larger would not fit.
  const bigger = l.size + 1;
  const cols = Math.floor(100 / bigger);
  assert.ok(cols === 0 || Math.ceil(24 / cols) * bigger > 50);
});

test('layout: a core bigger than the canvas area shares tiles between blocks of cells', () => {
  const l = layout(8010, 40, 20); // 800 pixels for 8010 cells: 11 cells per tile
  assert.equal(l.size, 1);
  assert.equal(l.block, 11);
  assert.ok(l.cols * l.rows >= Math.ceil(8010 / l.block));
  assert.ok(l.cols <= 40 && l.rows <= 20);
});

test('layout: 8000 cells on 800x600 use every tile of a grid that fits', () => {
  const l = layout(8000, 800, 600);
  assert.equal(l.block, 1);
  assert.ok(l.size >= 7);
  assert.ok(l.cols * l.size <= 800 && l.rows * l.size <= 600);
  assert.equal(l.rows, Math.ceil(8000 / l.cols));
});

test('layout: degenerate sizes do not throw or divide by zero', () => {
  for (const [n, w, h] of [[0, 100, 100], [1, 100, 100], [5, 0, 0], [5, 1, 1]]) {
    const l = layout(n, w, h);
    assert.ok(l.size >= 1 && l.cols >= 1 && l.rows >= 0 && l.block >= 1, `${n} ${w} ${h}: ${JSON.stringify(l)}`);
  }
});

test('blockOwner: the written owner with most cells, lowest id on a tie, 0 if none', () => {
  assert.equal(blockOwner([3]), 3);
  assert.equal(blockOwner([0]), 0);
  assert.equal(blockOwner([0, 0, 0]), 0);
  assert.equal(blockOwner([0, 2, 2, 1, 0]), 2);
  assert.equal(blockOwner([2, 1, 2, 1]), 1);
  assert.equal(blockOwner([3, 0, 0, 0, 0, 0, 0, 0]), 3);
});

test('botColor: no owner is the empty-cell color and bot colors are distinct', () => {
  assert.notEqual(botColor(0), botColor(1));
  const colors = new Set(Array.from({ length: 12 }, (_, i) => botColor(i + 1)));
  assert.equal(colors.size, 12);
  assert.equal(botColor(1), botColor(1));
});

/** A canvas context that records the rectangles it is asked to draw. */
function recorder() {
  /** @type {Array<{op: string, x: number, y: number, w: number, h: number, style: string}>} */
  const calls = [];
  const ctx = {
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 1,
    /** @param {number} x @param {number} y @param {number} w @param {number} h */
    fillRect(x, y, w, h) {
      calls.push({ op: 'fill', x, y, w, h, style: this.fillStyle });
    },
    /** @param {number} x @param {number} y @param {number} w @param {number} h */
    strokeRect(x, y, w, h) {
      calls.push({ op: 'stroke', x, y, w, h, style: this.strokeStyle });
    },
  };
  return { ctx: /** @type {any} */ (ctx), calls };
}

test('draw: tiles are colored by owner and executed cells get an outline', () => {
  const m = new Model(parseReplay(fixtureText()));
  m.seek(1);
  const frame = m.frame();
  const { ctx, calls } = recorder();
  const [w, h] = [240, 120];
  draw(ctx, frame, w, h);

  const l = layout(24, w, h);
  const x0 = Math.floor((w - l.cols * l.size) / 2);
  const y0 = Math.floor((h - l.rows * l.size) / 2);
  const tileAt = (/** @type {number} */ a) => [x0 + (a % l.cols) * l.size, y0 + Math.floor(a / l.cols) * l.size];

  // One filled tile per cell after the background, each in its owner's color.
  const tiles = calls.filter((c) => c.op === 'fill').slice(1);
  assert.equal(tiles.length, 24);
  frame.owners.forEach((owner, a) => {
    const [x, y] = tileAt(a);
    const t = tiles.find((c) => c.x === x && c.y === y);
    assert.equal(t?.style, botColor(owner), `cell ${a}`);
  });
  assert.equal(tiles.find((c) => c.style === EMPTY_COLOR) !== undefined, true);
  assert.notEqual(botColor(frame.owners[4]), botColor(frame.owners[0]));

  // Cycle 1 executed cells 3 (bot 1) and 19 (bot 2): two outlines, on those tiles.
  const outlines = calls.filter((c) => c.op === 'stroke');
  assert.deepEqual(
    outlines.map((c) => [c.x - 1, c.y - 1, c.style]),
    [3, 19].map((a) => [...tileAt(a), POINTER_COLOR]),
  );
});

test('draw: an empty frame at cycle 0 has no outlines, and a tiny core does not throw', () => {
  const m = new Model(parseReplay(fixtureText()));
  const { ctx, calls } = recorder();
  draw(ctx, m.frame(), 240, 120);
  assert.equal(calls.filter((c) => c.op === 'stroke').length, 0);
  draw(ctx, m.frame(), 1, 1);
  draw(ctx, { cycle: 0, owners: [], executed: [], live: [] }, 100, 100);
});
