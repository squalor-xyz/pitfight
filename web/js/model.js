// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// The frame model of a replay: the state of the match after a given cycle. It is
// the web twin of internal/tui/model.go and must produce the frames of
// testdata/viewer/frames.json for testdata/viewer/fixture.jsonl. No DOM access.

/** @typedef {import('./replay.js').Replay} Replay */

/**
 * One executed address of a cycle: bot `bot` executed the instruction at `pc`. A
 * replay's x record names only the process that stepped, so a bot with several
 * processes has one pointer per cycle, not one per process.
 * @typedef {{bot: number, pc: number}} Pointer
 */

/**
 * The state of the match at the end of one cycle.
 *  - cycle: 0 is the loaded core before any step; c is the core after cycle c.
 *  - owners: one entry per core address, the id of the bot that last wrote the
 *    cell, 0 for a cell nobody wrote. Bot cells count as written by their bot.
 *  - executed: the steps of cycle `cycle` in execution order (ascending bot id): the
 *    bot and the address it executed. Empty at cycle 0 and for a cycle with no steps.
 *  - live: live process count per bot, index i for bot i+1: 1 at the start, plus
 *    spawns, minus deaths, shown as 0 if below 0.
 * @typedef {{cycle: number, owners: number[], executed: Pointer[], live: number[]}} Frame
 */

/**
 * A replay positioned at a cycle. Seeking forward applies the events in between;
 * seeking back rebuilds the core from cycle 0, because replay version 1 has no
 * snapshots. It does not assume the invariants the replay writer keeps but a
 * reader does not check (placement, a step in every cycle for every live bot): it
 * shows what is recorded.
 */
export class Model {
  /** @param {Replay} replay */
  constructor(replay) {
    this.replay = replay;
    /** @type {Uint32Array} */
    this.owners = new Uint32Array(0);
    /** @type {number[]} */
    this.liveCounts = [];
    this.at = 0;
    this.next = 0; // index in replay.cycles of the first cycle not applied yet
    this.#reset();
  }

  /** The last cycle of the replay. */
  get cycles() {
    return Math.max(0, this.replay.result.cycles);
  }

  /** The current cycle. */
  get cycle() {
    return this.at;
  }

  /**
   * Moves to a cycle; before 0 means 0 and after the last means the last.
   * @param {number} cycle
   */
  seek(cycle) {
    const target = Number.isNaN(cycle) ? 0 : Math.min(Math.max(Math.trunc(cycle), 0), this.cycles);
    if (target < this.at) this.#reset();
    const cycles = this.replay.cycles;
    const size = this.owners.length;
    for (; this.next < cycles.length && cycles[this.next].n <= target; this.next++) {
      for (const step of cycles[this.next].steps) {
        for (const ev of step.events) {
          if (ev.kind === 'w') {
            this.owners[ev.address % size] = step.bot;
          } else if (ev.kind === 's') {
            this.#addLive(step.bot, 1);
          } else {
            this.#addLive(step.bot, -1);
          }
        }
      }
    }
    this.at = target;
  }

  /**
   * The frame at the current cycle. It shares nothing with the model.
   * @returns {Frame}
   */
  frame() {
    const frame = {
      cycle: this.at,
      owners: Array.from(this.owners),
      /** @type {Pointer[]} */
      executed: [],
      live: this.liveCounts.map((n) => Math.max(0, n)),
    };
    // Look the cycle up by its number, so a replay with a gap in its cycles does
    // not show the steps of an earlier one.
    const c = this.#cycleNumbered(this.at);
    if (c) {
      for (const st of c.steps) frame.executed.push({ bot: st.bot, pc: st.pc });
    }
    return frame;
  }

  /**
   * The recorded cycle with number n (cycles are in ascending order), if any.
   * @param {number} n
   */
  #cycleNumbered(n) {
    const cycles = this.replay.cycles;
    let lo = 0;
    let hi = cycles.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (cycles[mid].n === n) return cycles[mid];
      if (cycles[mid].n < n) lo = mid + 1;
      else hi = mid - 1;
    }
    return undefined;
  }

  /** Back to cycle 0: the empty core with the bots loaded, in bot order. */
  #reset() {
    const size = this.replay.config.coreSize;
    this.owners = new Uint32Array(size);
    for (const bot of this.replay.bots) {
      for (let k = 0; k < bot.cells.length; k++) this.owners[(bot.start + k) % size] = bot.id;
    }
    this.liveCounts = this.replay.bots.map(() => 1);
    this.at = 0;
    this.next = 0;
  }

  /**
   * @param {number} bot
   * @param {number} d
   */
  #addLive(bot, d) {
    if (bot >= 1 && bot <= this.liveCounts.length) this.liveCounts[bot - 1] += d;
  }
}
