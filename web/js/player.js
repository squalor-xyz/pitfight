// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// The step, play, pause and seek state machine over a Model; the web twin of
// internal/tui/player.go. It has no clock: whoever owns the clock calls tick() once
// per cycle while playing, so a test or a script can drive it deterministically.
// No DOM access.
//
// States: paused or playing. A play is either open-ended (to the last cycle) or
// counted (a number of cycles, or the last cycle if that comes first). step() and
// seek() pause. play() at the last cycle does nothing. tick() advances one cycle
// while playing and pauses at the end of the play.

/** @typedef {import('./model.js').Model} Model */
/** @typedef {import('./model.js').Frame} Frame */

export class Player {
  /** @param {Model} model */
  constructor(model) {
    this.model = model;
    this.playing = false;
    this.remaining = 0; // cycles left in a counted play; 0 when open-ended
  }

  /** The current cycle. */
  get cycle() {
    return this.model.cycle;
  }

  /** The last cycle of the replay. */
  get cycles() {
    return this.model.cycles;
  }

  /** Whether the play in progress is counted (play with n > 0). */
  get counted() {
    return this.playing && this.remaining > 0;
  }

  /** @returns {Frame} the frame at the current cycle */
  frame() {
    return this.model.frame();
  }

  /**
   * Pauses and moves n cycles (back if n is negative), within 0 and the last cycle.
   * @param {number} n
   */
  step(n) {
    this.pause();
    this.model.seek(this.model.cycle + n);
  }

  /**
   * Pauses and moves to a cycle, clamped to 0 and the last cycle.
   * @param {number} cycle
   */
  seek(cycle) {
    this.pause();
    this.model.seek(cycle);
  }

  /**
   * Starts playing: for n cycles if n > 0, otherwise to the last cycle. Playing
   * again while playing replaces the play in progress. At the last cycle there is
   * nothing to play and the player stays paused.
   * @param {number} [n]
   */
  play(n = 0) {
    this.playing = this.model.cycle < this.model.cycles;
    this.remaining = this.playing && n > 0 ? n : 0;
  }

  /** Stops playing. */
  pause() {
    this.playing = false;
  }

  /** Pauses if playing and plays to the end otherwise. */
  toggle() {
    if (this.playing) this.pause();
    else this.play(0);
  }

  /**
   * Advances one cycle if playing and reports whether it did. The play ends, and
   * the player pauses, when the last cycle is reached or the counted cycles are
   * used up.
   * @returns {boolean}
   */
  tick() {
    if (!this.playing) return false;
    this.model.seek(this.model.cycle + 1);
    if (this.remaining > 0) {
      this.remaining--;
      if (this.remaining === 0) this.playing = false;
    }
    if (this.model.cycle >= this.model.cycles) this.pause();
    return true;
  }
}
