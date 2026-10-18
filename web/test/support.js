// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// Helpers shared by the node:test files: the shared fixtures and a builder for
// small inline replays. Not a test file (its name does not end in .test.js).

import { readFileSync } from 'node:fs';

const repo = new URL('../../', import.meta.url);

/**
 * Reads a text file relative to the repository root.
 * @param {string} rel
 * @returns {string}
 */
export function readText(rel) {
  return readFileSync(new URL(rel, repo), 'utf8');
}

/** The shared viewer fixture (testdata/viewer/fixture.jsonl). */
export const fixtureText = () => readText('testdata/viewer/fixture.jsonl');

/** The expected frames of the fixture (testdata/viewer/frames.json). */
export const framesJSON = () => JSON.parse(readText('testdata/viewer/frames.json'));

/** The real replay of imp against dwarf (internal/replay/testdata). */
export const impDwarfText = () => readText('internal/replay/testdata/imp_dwarf_seed1.jsonl');

/** A DAT.F $0, $0 cell as the six codes of a replay cell. */
export const DAT = [0, 5, 1, 0, 1, 0];

/**
 * @typedef {object} BotSpec
 * @property {number} start      start address
 * @property {number} [length]   cells loaded (default 1)
 * @property {number} [entry]    entry_address (default start)
 */

/**
 * Builds the text of a replay from small descriptions. A step is
 * [bot, pc, ...events] where an event is a replay event object
 * ({t:'w',a,c}, {t:'s',a} or {t:'d'}). Nothing is validated here, so tests can
 * build replays the Go reader would refuse as long as the line grammar holds.
 * @param {object} o
 * @param {number} o.coreSize
 * @param {BotSpec[]} o.bots
 * @param {Array<Array<Array<any>>>} o.cycles  steps per cycle
 * @param {number} [o.resultCycles]  the cycles field of the end line
 * @returns {string}
 */
export function buildReplay({ coreSize, bots, cycles, resultCycles }) {
  const lines = [];
  lines.push(
    JSON.stringify({
      t: 'header',
      version: 1,
      config: { core_size: coreSize, cycle_limit: 100, max_processes: 8000, min_separation: 1, seed: '1' },
      bots: bots.map((b, i) => ({
        id: i + 1,
        hash: '0'.repeat(64),
        length: b.length ?? 1,
        start: b.start,
        entry_address: b.entry ?? b.start,
      })),
    }),
  );
  bots.forEach((b, i) => {
    lines.push(JSON.stringify({ t: 'load', bot: i + 1, cells: Array.from({ length: b.length ?? 1 }, () => DAT) }));
  });
  cycles.forEach((steps, i) => {
    lines.push(JSON.stringify({ t: 'c', n: i + 1 }));
    for (const [bot, pc, ...events] of steps) {
      lines.push(JSON.stringify({ t: 'x', b: bot, pc }));
      for (const ev of events) lines.push(JSON.stringify(ev));
    }
  });
  lines.push(
    JSON.stringify({
      t: 'end',
      cycles: resultCycles ?? cycles.length,
      survivors: [],
      outcome: 'tie',
      winner: 0,
      tied: [],
    }),
  );
  return lines.join('\n') + '\n';
}
