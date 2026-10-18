// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

import test from 'node:test';
import assert from 'node:assert/strict';
import { parseReplay, ReplayError, VersionError } from '../js/replay.js';
import { buildReplay, fixtureText, impDwarfText, DAT } from './support.js';

/** Counts the lines of a replay text by their "t" field, independently of the parser. */
function countLines(text) {
  /** @type {Record<string, number>} */
  const n = {};
  for (const line of text.split('\n')) {
    if (line === '') continue;
    const t = JSON.parse(line).t;
    n[t] = (n[t] ?? 0) + 1;
  }
  return n;
}

test('parses the header of the shared fixture', () => {
  const r = parseReplay(fixtureText());
  assert.equal(r.version, 1);
  assert.deepEqual(r.config, { coreSize: 24, cycleLimit: 40, maxProcesses: 8000, minSeparation: 3, seed: '24' });
  assert.deepEqual(
    r.bots.map((b) => [b.id, b.length, b.start, b.entryAddress, b.cells.length]),
    [
      [1, 1, 3, 3, 1],
      [2, 5, 19, 19, 5],
    ],
  );
  assert.deepEqual(r.bots[1].cells[0], [2, 2, 0, 4, 1, 3]);
  assert.deepEqual(r.result, { cycles: 12, survivors: [2], outcome: 'win', winner: 2, tied: [] });
});

test('the fixture has the event counts of its lines', () => {
  const text = fixtureText();
  const want = countLines(text);
  const r = parseReplay(text);
  const steps = r.cycles.flatMap((c) => c.steps);
  const events = steps.flatMap((s) => s.events);
  assert.equal(r.cycles.length, want.c);
  assert.equal(steps.length, want.x);
  assert.equal(events.filter((e) => e.kind === 'w').length, want.w ?? 0);
  assert.equal(events.filter((e) => e.kind === 's').length, want.s ?? 0);
  assert.equal(events.filter((e) => e.kind === 'd').length, want.d ?? 0);
  assert.deepEqual([r.cycles.length, steps.length, events.length], [12, 24, 20]);
  assert.deepEqual(
    r.cycles.map((c) => c.n),
    Array.from({ length: 12 }, (_, i) => i + 1),
  );
  assert.deepEqual(r.cycles[0].steps[0], {
    bot: 1,
    pc: 3,
    events: [{ kind: 'w', address: 4, cell: [1, 6, 1, 0, 1, 1] }],
  });
});

test('the real imp against dwarf replay parses', () => {
  const text = impDwarfText();
  const want = countLines(text);
  const r = parseReplay(text);
  assert.equal(r.config.coreSize, 8000);
  assert.equal(r.cycles.length, 40);
  assert.equal(r.cycles.flatMap((c) => c.steps).length, want.x);
  assert.equal(r.cycles.flatMap((c) => c.steps.flatMap((s) => s.events)).length, want.w + (want.s ?? 0) + (want.d ?? 0));
  assert.deepEqual(r.result, { cycles: 40, survivors: [1, 2], outcome: 'tie', winner: 0, tied: [1, 2] });
});

test('an unknown version is rejected with an error that names it', () => {
  for (const v of [2, 0, 99]) {
    const text = fixtureText().replace('"version":1', `"version":${v}`);
    assert.throws(
      () => parseReplay(text),
      (e) => e instanceof VersionError && e.version === v && e.message === `replay: unsupported version ${v} (this reader reads version 1)`,
    );
  }
  // The version is read before anything else is interpreted.
  assert.throws(() => parseReplay('{"t":"header","version":3,"junk":true}\n'), VersionError);
  // A version that is not an integer is not a version at all.
  assert.throws(() => parseReplay(fixtureText().replace('"version":1', '"version":"1"')), ReplayError);
});

test('a replay with a bot longer than the core, overlap or a stray entry still parses', () => {
  const text = buildReplay({
    coreSize: 8,
    bots: [
      { start: 7, length: 12, entry: 3 },
      { start: 6, length: 4 },
    ],
    cycles: [[[1, 0]]],
  });
  const r = parseReplay(text);
  assert.equal(r.bots[0].cells.length, 12);
  assert.equal(r.bots[0].entryAddress, 3);
});

/** @type {Array<[string, (t: string) => string]>} */
const bad = [
  ['empty input', () => ''],
  ['not JSON', () => 'hello\n'],
  ['first line is not a header', (t) => t.split('\n').slice(1).join('\n')],
  ['missing end line', (t) => t.split('\n').slice(0, -2).join('\n') + '\n'],
  ['data after the end line', (t) => t + '{"t":"c","n":13}\n'],
  ['unknown line type', (t) => t.replace('{"t":"end"', '{"t":"zz"}\n{"t":"end"')],
  ['cycle numbers with a gap', (t) => t.replace('"t":"c","n":2', '"t":"c","n":3')],
  ['cells shorter than length', (t) => t.replace('"cells":[[2,2,0,4,1,3],', '"cells":[')],
  ['address outside the core', (t) => t.replace('"a":4,', '"a":24,')],
  ['pc outside the core', (t) => t.replace('"pc":3}', '"pc":-1}')],
  ['step by an unknown bot', (t) => t.replace('"b":1,"pc":3', '"b":3,"pc":3')],
  ['end cycles differ from the last cycle', (t) => t.replace('"cycles":12', '"cycles":13')],
  ['core_size above 1048576', (t) => t.replace('"core_size":24', '"core_size":1048577')],
  ['core_size 0', (t) => t.replace('"core_size":24', '"core_size":0')],
  ['max_processes above 1048576', (t) => t.replace('"max_processes":8000', '"max_processes":1048577')],
  ['max_processes 0', (t) => t.replace('"max_processes":8000', '"max_processes":0')],
  ['min_separation above 1048576', (t) => t.replace('"min_separation":3', '"min_separation":1048577')],
  ['cycle_limit 0', (t) => t.replace('"cycle_limit":40', '"cycle_limit":0')],
  ['cell with five fields', (t) => t.replace('[1,6,1,0,1,1]', '[1,6,1,0,1]')],
  ['event before any step', (t) => t.replace('{"t":"c","n":1}\n{"t":"x","b":1,"pc":3}', '{"t":"c","n":1}\n{"t":"d"}\n{"t":"x","b":1,"pc":3}')],
];
for (const [name, mutate] of bad) {
  test(`rejects: ${name}`, () => {
    assert.throws(() => parseReplay(mutate(fixtureText())), ReplayError);
  });
}

test('errors carry the line number', () => {
  const lines = fixtureText().split('\n');
  lines[4] = '{"t":"x","b":1,"pc":99}';
  assert.throws(() => parseReplay(lines.join('\n')), /line 5/);
});

test('a final line feed is not required and a CRLF file parses', () => {
  const text = buildReplay({ coreSize: 4, bots: [{ start: 0 }, { start: 2 }], cycles: [[[1, 0, { t: 'w', a: 1, c: DAT }], [2, 2]]] });
  assert.equal(parseReplay(text.trimEnd()).cycles.length, 1);
  assert.equal(parseReplay(text.replaceAll('\n', '\r\n')).cycles.length, 1);
});

test('the first line must be the header, whatever else it holds', () => {
  assert.throws(() => parseReplay('{"t":"load","version":1,"bot":1,"cells":[]}\n'), /first line must be the header/);
});

test('the limits of match.md section 1 are accepted: core_size 1048576 parses without allocating a core', () => {
  const t = fixtureText().replace('"core_size":24', '"core_size":1048576');
  assert.equal(parseReplay(t).config.coreSize, 1048576);
});

test('a bot length above 1048576 is rejected as a length, before its cells are read', () => {
  const t = fixtureText().replace('"length":1,', '"length":1048577,');
  assert.throws(() => parseReplay(t), /bots\[0\]\.length/);
});
