// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// Parser for the pitfight replay format, version 1 (docs/spec/replay.md). No DOM
// access: it runs in the browser and in Node tests.
//
// The parser checks what the viewer needs to be safe: the line grammar and order,
// ranges of addresses and bot ids, consecutive cycles and a consistent end line.
// It does not demand the exact bytes the writer produces (spec section 8), and it
// does not check the placement of the bots or that a bot steps only while it has
// a live process; the model copes with replays that break those.

/**
 * A cell as the six codes [opcode, modifier, a_mode, a_value, b_mode, b_value].
 * @typedef {number[]} Cell
 */

/**
 * One event of a step: a write of `cell` at `address`, a spawn at `address` or a
 * death of the process that stepped.
 * @typedef {{kind: 'w', address: number, cell: Cell} | {kind: 's', address: number} | {kind: 'd'}} ReplayEvent
 */

/**
 * @typedef {object} Step
 * @property {number} bot  the bot that executed
 * @property {number} pc   the address it executed
 * @property {ReplayEvent[]} events
 */

/**
 * @typedef {object} ReplayCycle
 * @property {number} n
 * @property {Step[]} steps
 */

/**
 * @typedef {object} ReplayBot
 * @property {number} id
 * @property {string} hash
 * @property {number} length
 * @property {number} start
 * @property {number} entryAddress
 * @property {Cell[]} cells  the loaded cells, cells[k] at address (start + k) mod core size
 */

/**
 * @typedef {object} Replay
 * @property {number} version
 * @property {{coreSize: number, cycleLimit: number, maxProcesses: number, minSeparation: number, seed: string}} config
 * @property {ReplayBot[]} bots
 * @property {ReplayCycle[]} cycles
 * @property {{cycles: number, survivors: number[], outcome: string, winner: number, tied: number[]}} result
 */

/** The largest core size, bot length, process limit and separation (match.md section 1). */
export const MAX_CORE = 1 << 20;

/** The version this reader reads. */
export const SUPPORTED_VERSION = 1;

/** A replay that cannot be read. The message starts with "replay:". */
export class ReplayError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = 'ReplayError';
  }
}

/** A replay of a version this reader does not implement. */
export class VersionError extends ReplayError {
  /** @param {unknown} version */
  constructor(version) {
    super(`replay: unsupported version ${version} (this reader reads version ${SUPPORTED_VERSION})`);
    this.name = 'VersionError';
    this.version = version;
  }
}

/**
 * @param {unknown} v
 * @returns {v is number}
 */
const isInt = (v) => typeof v === 'number' && Number.isSafeInteger(v);

/**
 * Parses the text of a replay (JSON lines).
 * @param {string} text
 * @returns {Replay}
 * @throws {ReplayError}
 */
export function parseReplay(text) {
  const lines = text.split('\n');
  if (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();
  if (lines.length === 0) throw new ReplayError('replay: empty file');

  let lineNo = 0;
  /** @param {string} msg @returns {never} */
  const fail = (msg) => {
    throw new ReplayError(`replay: line ${lineNo}: ${msg}`);
  };
  /**
   * @param {string} name
   * @param {unknown} v
   * @param {number} lo
   * @param {number} hi
   * @returns {number}
   */
  const int = (name, v, lo, hi) => {
    if (!isInt(v) || v < lo || v > hi) fail(`${name} ${JSON.stringify(v)} is not an integer in ${lo}..${hi}`);
    return /** @type {number} */ (v);
  };
  /** @param {number} i @returns {any} */
  const record = (i) => {
    lineNo = i + 1;
    let v;
    try {
      v = JSON.parse(lines[i]);
    } catch (e) {
      return fail(`not JSON: ${e instanceof Error ? e.message : e}`);
    }
    if (v === null || typeof v !== 'object' || Array.isArray(v)) fail('not a JSON object');
    return v;
  };

  // The header, and its version first (spec section 7).
  const h = record(0);
  if (h.t !== 'header') fail('the first line must be the header');
  if (!isInt(h.version)) fail(`version ${JSON.stringify(h.version)} is not an integer`);
  if (h.version !== SUPPORTED_VERSION) throw new VersionError(h.version);
  const cfg = h.config;
  if (cfg === null || typeof cfg !== 'object') fail('config is missing');
  // The ranges of match.md section 1, so a hostile file cannot make the model
  // allocate a core of billions of cells.
  const coreSize = int('core_size', cfg.core_size, 1, MAX_CORE);
  const config = {
    coreSize,
    cycleLimit: int('cycle_limit', cfg.cycle_limit, 1, 2 ** 31 - 1),
    maxProcesses: int('max_processes', cfg.max_processes, 1, MAX_CORE),
    minSeparation: int('min_separation', cfg.min_separation, 0, MAX_CORE),
    seed: String(cfg.seed),
  };
  if (!Array.isArray(h.bots) || h.bots.length === 0) fail('bots must be a non-empty array');
  const nbots = h.bots.length;
  /** @type {ReplayBot[]} */
  const bots = h.bots.map((/** @type {any} */ b, /** @type {number} */ i) => {
    if (b === null || typeof b !== 'object') fail(`bots[${i}] is not an object`);
    if (b.id !== i + 1) fail(`bots[${i}].id is ${JSON.stringify(b.id)}, want ${i + 1}`);
    return {
      id: b.id,
      hash: String(b.hash),
      length: int(`bots[${i}].length`, b.length, 1, MAX_CORE),
      start: int(`bots[${i}].start`, b.start, 0, coreSize - 1),
      entryAddress: int(`bots[${i}].entry_address`, b.entry_address, 0, coreSize - 1),
      cells: [],
    };
  });

  /** @param {unknown} c @returns {Cell} */
  const cell = (c) => {
    if (!Array.isArray(c) || c.length !== 6) return fail('a cell must be an array of 6 integers');
    for (const v of c) if (!isInt(v) || v < 0) fail('a cell must be an array of 6 integers');
    return c;
  };

  let i = 1;
  // One load line per bot, in bot id order.
  for (const bot of bots) {
    if (i >= lines.length) throw new ReplayError('replay: the file ends before the load lines are complete');
    const l = record(i);
    if (l.t !== 'load') fail(`expected the load line of bot ${bot.id}`);
    if (l.bot !== bot.id) fail(`load line of bot ${JSON.stringify(l.bot)}, want bot ${bot.id}`);
    if (!Array.isArray(l.cells) || l.cells.length !== bot.length) fail(`cells must have exactly ${bot.length} entries`);
    bot.cells = l.cells.map(cell);
    i++;
  }

  /** @type {ReplayCycle[]} */
  const cycles = [];
  /** @type {Step | null} */
  let step = null;
  /** @type {Replay['result'] | null} */
  let result = null;
  for (; i < lines.length; i++) {
    const l = record(i);
    if (result) fail('data after the end line');
    switch (l.t) {
      case 'c':
        if (l.n !== cycles.length + 1) fail(`cycle ${JSON.stringify(l.n)} after cycle ${cycles.length}: cycles must be consecutive from 1`);
        cycles.push({ n: l.n, steps: [] });
        step = null;
        break;
      case 'x': {
        if (cycles.length === 0) fail('a step before the first cycle');
        step = {
          bot: int('b', l.b, 1, nbots),
          pc: int('pc', l.pc, 0, coreSize - 1),
          events: [],
        };
        cycles[cycles.length - 1].steps.push(step);
        break;
      }
      case 'w':
        if (!step) fail('a write before any step');
        step.events.push({ kind: 'w', address: int('a', l.a, 0, coreSize - 1), cell: cell(l.c) });
        break;
      case 's':
        if (!step) fail('a spawn before any step');
        step.events.push({ kind: 's', address: int('a', l.a, 0, coreSize - 1) });
        break;
      case 'd':
        if (!step) fail('a death before any step');
        step.events.push({ kind: 'd' });
        break;
      case 'end': {
        const list = (/** @type {string} */ name, /** @type {unknown} */ v) => {
          if (!Array.isArray(v)) return fail(`${name} must be an array`);
          return v.map((x) => int(`${name} entry`, x, 1, nbots));
        };
        const n = int('cycles', l.cycles, 0, 2 ** 31 - 1);
        if (n !== cycles.length) fail(`end says ${n} cycles but the file has ${cycles.length}`);
        if (l.outcome !== 'win' && l.outcome !== 'tie') fail(`outcome ${JSON.stringify(l.outcome)} is not win or tie`);
        result = {
          cycles: n,
          survivors: list('survivors', l.survivors),
          outcome: l.outcome,
          winner: int('winner', l.winner, 0, nbots),
          tied: list('tied', l.tied),
        };
        step = null;
        break;
      }
      default:
        fail(`unknown line type ${JSON.stringify(l.t)}`);
    }
  }
  if (!result) throw new ReplayError('replay: the file ends without an end line');
  return { version: h.version, config, bots, cycles, result };
}
