// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// Canvas drawing of a frame. The functions layout, blockOwner and botColor are
// pure (and tested in Node); only draw() touches the canvas. Nothing here runs at
// import time that needs a DOM.
//
// Look: the core is a grid of square tiles in address order, left to right, top to
// bottom. A tile is the color of the bot that last wrote its cell, dark grey if
// nobody did. The cells executed in the current frame get a bright outline (or a
// white tile when tiles are too small for an outline). When the core has more cells
// than the canvas has pixels, one tile stands for a block of cells and shows the
// owner with most cells in the block, as the terminal viewer does.

/** @typedef {import('./model.js').Frame} Frame */

/** Color of a cell nobody has written. */
export const EMPTY_COLOR = '#23272e';
/** Color of the executed-cell marks. */
export const POINTER_COLOR = '#ffffff';

/**
 * The CSS color of a bot id: golden-angle hues, so any number of bots get colors
 * that differ from their neighbours'. 0 (no owner) is the empty-cell color.
 * @param {number} id
 * @returns {string}
 */
export function botColor(id) {
  if (id === 0) return EMPTY_COLOR;
  const hue = Math.round(((id - 1) * 137.508) % 360);
  return `hsl(${hue} 70% ${id % 2 === 0 ? 62 : 52}%)`;
}

/**
 * Tile layout for a core of n cells on a canvas of w by h pixels.
 * @param {number} n
 * @param {number} w
 * @param {number} h
 * @returns {{size: number, cols: number, rows: number, block: number}}
 *   size: tile edge in pixels (at least 1); cols, rows: tiles per row and column;
 *   block: cells per tile (1 unless the core has more cells than the canvas pixels)
 */
export function layout(n, w, h) {
  w = Math.max(1, Math.floor(w));
  h = Math.max(1, Math.floor(h));
  if (n <= 0) return { size: 1, cols: w, rows: 0, block: 1 };
  // Largest edge s such that ceil(n / floor(w / s)) rows of s pixels fit. No edge
  // can exceed sqrt(w * h / n), so start there and shrink.
  let size = Math.max(1, Math.floor(Math.sqrt((w * h) / n)));
  for (; size > 1; size--) {
    const cols = Math.floor(w / size);
    if (cols >= 1 && Math.ceil(n / cols) * size <= h) break;
  }
  if (size === 1 && w * h < n) {
    const block = Math.ceil(n / (w * h));
    const tiles = Math.ceil(n / block);
    return { size: 1, cols: w, rows: Math.ceil(tiles / w), block };
  }
  const cols = Math.floor(w / size);
  return { size, cols, rows: Math.ceil(n / cols), block: 1 };
}

/**
 * The owner shown for a block of cells: the written owner with most cells, the
 * lowest id on a tie, 0 if no cell is written.
 * @param {ArrayLike<number>} owners  the owners of the block's cells
 * @returns {number}
 */
export function blockOwner(owners) {
  if (owners.length === 1) return owners[0];
  /** @type {Map<number, number>} */
  const counts = new Map();
  for (let i = 0; i < owners.length; i++) {
    const o = owners[i];
    if (o !== 0) counts.set(o, (counts.get(o) ?? 0) + 1);
  }
  let best = 0;
  let bestN = 0;
  for (const [id, n] of counts) {
    if (n > bestN || (n === bestN && id < best)) {
      best = id;
      bestN = n;
    }
  }
  return best;
}

/**
 * Draws a frame on the whole canvas.
 * @param {CanvasRenderingContext2D} ctx
 * @param {Frame} frame
 * @param {number} width   canvas width in drawing units
 * @param {number} height  canvas height in drawing units
 */
export function draw(ctx, frame, width, height) {
  ctx.fillStyle = '#14161a';
  ctx.fillRect(0, 0, width, height);
  const n = frame.owners.length;
  const { size, cols, rows, block } = layout(n, width, height);
  if (rows === 0) return;
  const tiles = Math.ceil(n / block);
  const gap = size >= 6 ? 1 : 0;
  const x0 = Math.floor((width - cols * size) / 2);
  const y0 = Math.floor((height - rows * size) / 2);

  /** @param {number} t */
  const rect = (t) => [x0 + (t % cols) * size, y0 + Math.floor(t / cols) * size];
  for (let t = 0; t < tiles; t++) {
    const owner = blockOwner(frame.owners.slice(t * block, Math.min((t + 1) * block, n)));
    ctx.fillStyle = botColor(owner);
    const [x, y] = rect(t);
    ctx.fillRect(x, y, size - gap, size - gap);
  }

  ctx.strokeStyle = POINTER_COLOR;
  ctx.fillStyle = POINTER_COLOR;
  for (const p of frame.executed) {
    if (p.pc < 0 || p.pc >= n) continue;
    const [x, y] = rect(Math.floor(p.pc / block));
    if (size >= 5) {
      ctx.lineWidth = 2;
      ctx.strokeRect(x + 1, y + 1, size - gap - 2, size - gap - 2);
    } else {
      ctx.fillRect(x, y, size, size);
    }
  }
}
