// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

import test from 'node:test';
import assert from 'node:assert/strict';
import { parseReplay } from '../js/replay.js';
import { Model } from '../js/model.js';
import { buildReplay, fixtureText, framesJSON, impDwarfText, splFixtureText, splFramesJSON, DAT } from './support.js';

/** @param {string} text */
const modelOf = (text) => new Model(parseReplay(text));

const w = (a, c = DAT) => ({ t: 'w', a, c });
const s = (a) => ({ t: 's', a });
const d = { t: 'd' };

// The Go terminal viewer (internal/tui TestFrames) checks the same two pairs, so a
// frame that differs between the viewers fails one of the two suites.
for (const [label, fixture, frames, fixtureName] of [
  ['testdata/viewer/frames.json', fixtureText, framesJSON, 'fixture.jsonl'],
  ['testdata/viewer/spl_frames.json (spawns and deaths)', splFixtureText, splFramesJSON, 'spl_fixture.jsonl'],
]) {
  test(`the frame model at each cycle equals ${label}`, () => {
    const ff = frames();
    assert.equal(ff.fixture, fixtureName);
    assert.equal(ff.frames.length, ff.cycles + 1, 'one frame per cycle 0..cycles');
    const m = modelOf(fixture());
    assert.equal(m.cycles, ff.cycles);
    for (const want of ff.frames) {
      m.seek(want.cycle);
      assert.deepEqual(m.frame(), want, `cycle ${want.cycle}`);
    }
  });
}

test('frames.json matches when visiting cycles in reverse (backward seeks)', () => {
  const ff = framesJSON();
  const m = modelOf(fixtureText());
  for (const want of [...ff.frames].reverse()) {
    m.seek(want.cycle);
    assert.deepEqual(m.frame(), want, `cycle ${want.cycle}`);
  }
});

test('seek clamps to 0 and the last cycle', () => {
  const m = modelOf(fixtureText());
  for (const [seek, want] of [[-5, 0], [7, 7], [1000, 12], [-1, 0], [12, 12], [Infinity, 12], [-Infinity, 0], [5, 5], [NaN, 0], [3.9, 3]]) {
    m.seek(seek);
    assert.equal(m.cycle, want, `seek(${seek})`);
    assert.equal(m.frame().cycle, want);
  }
});

test('a frame shares nothing with the model', () => {
  const m = modelOf(fixtureText());
  m.seek(3);
  const f = m.frame();
  f.owners.fill(9);
  f.executed.length = 0;
  f.live[0] = 99;
  assert.deepEqual(m.frame(), framesJSON().frames[3]);
});

test('a spawn makes live 2, only the executed address of the cycle is drawn, a death returns to 1', () => {
  const m = modelOf(
    buildReplay({
      coreSize: 16,
      bots: [{ start: 0 }, { start: 8 }],
      cycles: [
        [[1, 0, s(5)], [2, 8]],
        [[1, 5], [2, 9]],
        [[1, 1, d], [2, 10]],
      ],
    }),
  );
  const want = [
    { live: [1, 1], executed: [] },
    { live: [2, 1], executed: [{ bot: 1, pc: 0 }, { bot: 2, pc: 8 }] },
    { live: [2, 1], executed: [{ bot: 1, pc: 5 }, { bot: 2, pc: 9 }] },
    { live: [1, 1], executed: [{ bot: 1, pc: 1 }, { bot: 2, pc: 10 }] },
  ];
  want.forEach((wnt, c) => {
    m.seek(c);
    const f = m.frame();
    assert.deepEqual({ live: f.live, executed: f.executed }, wnt, `cycle ${c}`);
  });
});

test('live counts are 1 + spawns - deaths; only the shown value is clamped, never the count', () => {
  // Bot 1 dies twice (count -1) and then spawns twice. Clamping the count at each
  // step would give 2 at the end; the true count is 1. The replay is one the Go
  // reader refuses (a dead bot steps) but the viewer must not assume otherwise.
  const m = modelOf(
    buildReplay({
      coreSize: 16,
      bots: [{ start: 0 }, { start: 8 }],
      cycles: [
        [[1, 0, d], [2, 8]],
        [[1, 1, d], [2, 9]],
        [[1, 2, s(4)], [2, 10]],
        [[1, 3, s(5)], [2, 11]],
      ],
    }),
  );
  const live1 = [0, 1, 2, 3, 4].map((c) => {
    m.seek(c);
    return m.frame().live[0];
  });
  assert.deepEqual(live1, [1, 0, 0, 0, 1]);
});

test('a final live count below 0 is shown as 0', () => {
  const m = modelOf(
    buildReplay({
      coreSize: 16,
      bots: [{ start: 0 }, { start: 8 }],
      cycles: [
        [[1, 0, d], [2, 8]],
        [[1, 1, d], [2, 9]],
        [[1, 2, d], [2, 10]],
      ],
    }),
  );
  m.seek(m.cycles);
  assert.deepEqual(m.frame().live, [0, 1]);
});

test('placement is not validated: overlap, a bot longer than the core, an entry outside the bot', () => {
  // Core of 8. Bot 1 (12 cells) starts at 7 and wraps onto itself; bot 2 (4 cells)
  // starts at 6 and overwrites addresses 6, 7, 0, 1 of it, because loads apply in
  // bot order. The entry of bot 1 (3) is not a cell of the bot.
  const m = modelOf(
    buildReplay({
      coreSize: 8,
      bots: [{ start: 7, length: 12, entry: 3 }, { start: 6, length: 4, entry: 0 }],
      cycles: [[[1, 3, w(4)], [2, 0]]],
    }),
  );
  const f0 = m.frame();
  assert.deepEqual(f0.owners, [2, 2, 1, 1, 1, 1, 2, 2]);
  assert.deepEqual(f0.live, [1, 1]);
  m.seek(1);
  const f1 = m.frame();
  assert.deepEqual(f1.owners, [2, 2, 1, 1, 1, 1, 2, 2]);
  assert.deepEqual(f1.executed, [{ bot: 1, pc: 3 }, { bot: 2, pc: 0 }]);
});

test('a live bot with no step in a cycle, and cycles after a bot is gone, are shown as recorded', () => {
  const m = modelOf(
    buildReplay({
      coreSize: 8,
      bots: [{ start: 0 }, { start: 4 }],
      cycles: [
        [[2, 4]], // bot 1 is live but has no step
        [[1, 0, d], [2, 5]],
        [[2, 6]], // bot 1 is gone
        [[2, 7]],
      ],
    }),
  );
  const seen = [];
  for (let c = 0; c <= 4; c++) {
    m.seek(c);
    const f = m.frame();
    seen.push([f.live, f.executed]);
  }
  assert.deepEqual(seen, [
    [[1, 1], []],
    [[1, 1], [{ bot: 2, pc: 4 }]],
    [[0, 1], [{ bot: 1, pc: 0 }, { bot: 2, pc: 5 }]],
    [[0, 1], [{ bot: 2, pc: 6 }]],
    [[0, 1], [{ bot: 2, pc: 7 }]],
  ]);
});

test('spawns beyond max_processes just count', () => {
  const steps = Array.from({ length: 5 }, (_, i) => [[1, i, s(i + 1)], [2, 8]]);
  const text = buildReplay({ coreSize: 16, bots: [{ start: 0 }, { start: 8 }], cycles: steps }).replace('"max_processes":8000', '"max_processes":2');
  const m = modelOf(text);
  m.seek(5);
  assert.deepEqual(m.frame().live, [6, 1]);
});

test('a write sets the owner to the stepping bot, also with an unchanged value', () => {
  const m = modelOf(
    buildReplay({
      coreSize: 8,
      bots: [{ start: 0 }, { start: 4 }],
      cycles: [[[1, 0, w(4)], [2, 4, w(0)]]],
    }),
  );
  m.seek(1);
  assert.deepEqual(m.frame().owners, [2, 0, 0, 0, 1, 0, 0, 0]);
});

test('the real imp against dwarf replay models without error at every cycle', () => {
  const r = parseReplay(impDwarfText());
  const m = new Model(r);
  assert.equal(m.cycles, 40);
  for (let c = 0; c <= 40; c++) {
    m.seek(c);
    const f = m.frame();
    assert.equal(f.cycle, c);
    assert.equal(f.owners.length, 8000);
    assert.deepEqual(f.live, [1, 1]);
    assert.equal(f.executed.length, c === 0 ? 0 : 2);
  }
  // docs/spec/replay.md section 9: in cycle 1 the imp at 4519 copies itself to
  // 4520 and the dwarf at 2555 runs.
  m.seek(1);
  const f = m.frame();
  assert.deepEqual(f.executed, [{ bot: 1, pc: 4519 }, { bot: 2, pc: 2555 }]);
  assert.equal(f.owners[4519], 1);
  assert.equal(f.owners[4520], 1);
  assert.equal(f.owners[2558], 2);
  assert.equal(f.owners[0], 0);
});

test('a cycle the replay has no steps for shows no executed addresses (replay object with a gap)', () => {
  // The parser refuses gaps, but the model is built from objects, and must not show
  // the steps of an earlier cycle at a cycle it has none for.
  /** @type {import('../js/replay.js').Replay} */
  const replay = {
    version: 1,
    config: { coreSize: 8, cycleLimit: 10, maxProcesses: 8, minSeparation: 1, seed: '1' },
    bots: [{ id: 1, hash: '', length: 1, start: 0, entryAddress: 0, cells: [DAT] }],
    cycles: [
      { n: 1, steps: [{ bot: 1, pc: 0, events: [] }] },
      { n: 3, steps: [{ bot: 1, pc: 2, events: [] }] },
    ],
    result: { cycles: 3, survivors: [1], outcome: 'win', winner: 1, tied: [] },
  };
  const m = new Model(replay);
  const executed = [0, 1, 2, 3].map((c) => {
    m.seek(c);
    return m.frame().executed;
  });
  assert.deepEqual(executed, [[], [{ bot: 1, pc: 0 }], [], [{ bot: 1, pc: 2 }]]);
});
