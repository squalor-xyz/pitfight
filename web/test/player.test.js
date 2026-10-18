// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

import test from 'node:test';
import assert from 'node:assert/strict';
import { parseReplay } from '../js/replay.js';
import { Model } from '../js/model.js';
import { Player } from '../js/player.js';
import { fixtureText, framesJSON } from './support.js';

const newPlayer = () => new Player(new Model(parseReplay(fixtureText())));

test('seeking to cycle N equals stepping N times, forward and backward', () => {
  const stepper = newPlayer();
  const stepped = [stepper.frame()];
  for (let i = 0; i < stepper.cycles; i++) {
    stepper.step(1);
    stepped.push(stepper.frame());
  }
  assert.equal(stepped.length, 13);
  assert.deepEqual(stepped, framesJSON().frames);
  for (let n = 0; n <= stepper.cycles; n++) {
    const fresh = newPlayer();
    fresh.seek(n);
    assert.deepEqual(fresh.frame(), stepped[n], `fresh seek to ${n}`);
    const back = newPlayer();
    back.seek(back.cycles);
    back.seek(n);
    assert.deepEqual(back.frame(), stepped[n], `backward seek to ${n}`);
  }
});

/**
 * @typedef {object} Op
 * @property {string} do  step, seek, play, pause, toggle or tick
 * @property {number} [n]
 * @property {number} cycle
 * @property {boolean} [playing]
 * @property {boolean} [counted]  a counted play (play N) is running
 * @property {boolean} [changed]  tick only: the cycle advanced
 */

/** @type {Array<[string, Op[]]>} */
const scripts = [
  ['starts paused at 0', [{ do: 'pause', cycle: 0 }]],
  ['step forward', [{ do: 'step', n: 1, cycle: 1 }, { do: 'step', n: 1, cycle: 2 }, { do: 'step', n: 3, cycle: 5 }]],
  ['step back', [{ do: 'seek', n: 6, cycle: 6 }, { do: 'step', n: -1, cycle: 5 }, { do: 'step', n: -2, cycle: 3 }]],
  ['step back before 0', [{ do: 'seek', n: 2, cycle: 2 }, { do: 'step', n: -9, cycle: 0 }, { do: 'step', n: -1, cycle: 0 }]],
  ['step past the end', [{ do: 'seek', n: 10, cycle: 10 }, { do: 'step', n: 100, cycle: 12 }, { do: 'step', n: 1, cycle: 12 }]],
  ['step zero stays', [{ do: 'seek', n: 4, cycle: 4 }, { do: 'step', n: 0, cycle: 4 }]],
  ['seek', [{ do: 'seek', n: 7, cycle: 7 }, { do: 'seek', n: 3, cycle: 3 }, { do: 'seek', n: 0, cycle: 0 }, { do: 'seek', n: 12, cycle: 12 }]],
  ['seek past the end', [{ do: 'seek', n: 13, cycle: 12 }, { do: 'seek', n: 2 ** 30, cycle: 12 }]],
  ['seek before 0', [{ do: 'seek', n: 5, cycle: 5 }, { do: 'seek', n: -1, cycle: 0 }, { do: 'seek', n: -(2 ** 30), cycle: 0 }]],
  [
    'play to the end',
    [
      { do: 'play', cycle: 0, playing: true },
      { do: 'tick', cycle: 1, playing: true, changed: true },
      { do: 'seek', n: 10, cycle: 10 }, // a seek pauses
      { do: 'play', cycle: 10, playing: true },
      { do: 'tick', cycle: 11, playing: true, changed: true },
      { do: 'tick', cycle: 12, playing: false, changed: true },
      { do: 'tick', cycle: 12, playing: false },
    ],
  ],
  [
    'play N cycles',
    [
      { do: 'play', n: 2, cycle: 0, playing: true, counted: true },
      { do: 'tick', cycle: 1, playing: true, counted: true, changed: true },
      { do: 'tick', cycle: 2, playing: false, changed: true },
      { do: 'tick', cycle: 2, playing: false },
    ],
  ],
  [
    'play N stops at the end',
    [
      { do: 'seek', n: 11, cycle: 11 },
      { do: 'play', n: 5, cycle: 11, playing: true, counted: true },
      { do: 'tick', cycle: 12, playing: false, changed: true },
    ],
  ],
  [
    'play at the end does nothing',
    [
      { do: 'seek', n: 12, cycle: 12 },
      { do: 'play', cycle: 12 },
      { do: 'play', n: 3, cycle: 12 },
      { do: 'toggle', cycle: 12 },
    ],
  ],
  [
    'pause',
    [
      { do: 'play', cycle: 0, playing: true },
      { do: 'tick', cycle: 1, playing: true, changed: true },
      { do: 'pause', cycle: 1 },
      { do: 'tick', cycle: 1 },
      { do: 'pause', cycle: 1 },
    ],
  ],
  [
    'toggle',
    [
      { do: 'toggle', cycle: 0, playing: true },
      { do: 'tick', cycle: 1, playing: true, changed: true },
      { do: 'toggle', cycle: 1 },
      { do: 'toggle', cycle: 1, playing: true },
    ],
  ],
  [
    'play again while playing restarts the count',
    [
      { do: 'play', n: 3, cycle: 0, playing: true, counted: true },
      { do: 'tick', cycle: 1, playing: true, counted: true, changed: true },
      { do: 'play', cycle: 1, playing: true },
      { do: 'tick', cycle: 2, playing: true, changed: true },
      { do: 'tick', cycle: 3, playing: true, changed: true },
    ],
  ],
  ['step pauses', [{ do: 'play', cycle: 0, playing: true }, { do: 'step', n: 1, cycle: 1 }, { do: 'tick', cycle: 1 }]],
  [
    'seek pauses',
    [
      { do: 'play', n: 4, cycle: 0, playing: true, counted: true },
      { do: 'seek', n: 6, cycle: 6 },
      { do: 'tick', cycle: 6 },
    ],
  ],
  ['paused tick is a no-op', [{ do: 'tick', cycle: 0 }, { do: 'step', n: 2, cycle: 2 }, { do: 'tick', cycle: 2 }]],
];

for (const [name, ops] of scripts) {
  test(`state machine: ${name}`, () => {
    const p = newPlayer();
    assert.deepEqual([p.cycle, p.playing, p.cycles], [0, false, 12]);
    ops.forEach((o, i) => {
      let changed = false;
      switch (o.do) {
        case 'step': p.step(o.n ?? 0); break;
        case 'seek': p.seek(o.n ?? 0); break;
        case 'play': p.play(o.n ?? 0); break;
        case 'pause': p.pause(); break;
        case 'toggle': p.toggle(); break;
        case 'tick': changed = p.tick(); break;
        default: assert.fail(`bad op ${o.do}`);
      }
      const where = `op ${i} (${o.do} ${o.n ?? ''})`;
      assert.deepEqual(
        [p.cycle, p.playing, p.counted],
        [o.cycle, o.playing ?? false, o.counted ?? false],
        where,
      );
      if (o.do === 'tick') assert.equal(changed, o.changed ?? false, `${where}: changed`);
      assert.equal(p.frame().cycle, p.cycle, `${where}: frame cycle`);
    });
  });
}
