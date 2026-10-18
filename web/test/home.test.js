// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

import test from 'node:test';
import assert from 'node:assert/strict';
import {
  parseListing,
  listingOptions,
  defaultPicks,
  parseSeed,
  randomSeed,
  resolveSeed,
  buildMatchRequest,
  describeMatchError,
  replayUrl,
} from '../js/home.js';

test('parseListing accepts the listing and rejects other shapes', () => {
  assert.deepEqual(parseListing({ bots: ['a.s'], replays: ['m.jsonl'] }), { bots: ['a.s'], replays: ['m.jsonl'], truncated: false });
  assert.deepEqual(parseListing({ bots: [], replays: [], truncated: true }), { bots: [], replays: [], truncated: true });
  for (const bad of [null, [], 'x', {}, { bots: [] }, { bots: 'a.s', replays: [] }, { bots: [1], replays: [] }, { bots: [], replays: [null] }]) {
    assert.throws(() => parseListing(bad), /listing/, JSON.stringify(bad));
  }
});

test('listingOptions turns a listing into picker options and replay links', () => {
  const opts = listingOptions({ bots: ['dwarf.s', 'imp.s', 'my bot.s'], replays: ['a b.jsonl', 'm#1.jsonl'], truncated: false });
  assert.deepEqual(opts.bots, [
    { value: 'dwarf.s', label: 'dwarf' },
    { value: 'imp.s', label: 'imp' },
    { value: 'my bot.s', label: 'my bot' },
  ]);
  assert.deepEqual(opts.replays, [
    { name: 'a b.jsonl', label: 'a b', url: '/replays/a%20b.jsonl' },
    { name: 'm#1.jsonl', label: 'm#1', url: '/replays/m%231.jsonl' },
  ]);
  assert.deepEqual(listingOptions({ bots: [], replays: [], truncated: false }), { bots: [], replays: [], truncated: false });
});

test('replayUrl encodes the name as one path segment', () => {
  assert.equal(replayUrl('m.jsonl'), '/replays/m.jsonl');
  assert.equal(replayUrl('../x?y.jsonl'), '/replays/..%2Fx%3Fy.jsonl');
});

test('defaultPicks preselects the starter bots imp and dwarf', () => {
  assert.deepEqual(defaultPicks(['dwarf.s', 'imp.s', 'replicator.s', 'scanner.s']), ['imp.s', 'dwarf.s']);
  assert.deepEqual(defaultPicks(['a.s', 'b.s', 'c.s']), ['a.s', 'b.s']);
  assert.deepEqual(defaultPicks(['imp.s', 'z.s']), ['imp.s', 'z.s']);
  assert.deepEqual(defaultPicks(['dwarf.s', 'z.s']), ['z.s', 'dwarf.s']);
  assert.deepEqual(defaultPicks(['only.s']), ['only.s', 'only.s']);
  assert.deepEqual(defaultPicks([]), ['', '']);
});

test('parseSeed takes a decimal from 0 to 2^64-1', () => {
  assert.equal(parseSeed('0'), '0');
  assert.equal(parseSeed(' 42 '), '42');
  assert.equal(parseSeed('007'), '7');
  assert.equal(parseSeed('18446744073709551615'), '18446744073709551615');
  for (const bad of ['', '  ', '-1', '+1', '1.5', '1e3', 'abc', '0x10', '18446744073709551616', '١٢']) {
    assert.equal(parseSeed(bad), null, JSON.stringify(bad));
  }
});

test('randomSeed is a decimal string from the 64 bits it is given', () => {
  /** @param {number[]} words */
  const fill = (words) => (/** @type {BigUint64Array} */ a) => {
    words.forEach((w, i) => (a[i] = BigInt(w)));
    return a;
  };
  assert.equal(randomSeed(fill([0])), '0');
  assert.equal(randomSeed(fill([123456789])), '123456789');
  const max = (/** @type {BigUint64Array} */ a) => {
    a[0] = 2n ** 64n - 1n;
    return a;
  };
  assert.equal(randomSeed(max), '18446744073709551615');
  const real = randomSeed();
  assert.match(real, /^[0-9]+$/);
  assert.notEqual(parseSeed(real), null);
});

test('resolveSeed keeps a typed seed and rolls an automatic or empty one', () => {
  let rolls = 0;
  const roll = () => String(++rolls * 100);
  assert.deepEqual(resolveSeed({ text: '5', auto: false }, roll), { text: '5', auto: false });
  assert.equal(rolls, 0);
  assert.deepEqual(resolveSeed({ text: '99', auto: true }, roll), { text: '100', auto: true });
  assert.deepEqual(resolveSeed({ text: '  ', auto: false }, roll), { text: '200', auto: true });
  assert.deepEqual(resolveSeed({ text: '', auto: true }, roll), { text: '300', auto: true });
});

test('buildMatchRequest builds the POST for the picker state', () => {
  const req = buildMatchRequest({ first: 'imp.s', second: 'dwarf.s', seedText: '18446744073709551615' });
  assert.equal(req.url, '/api/match');
  assert.equal(req.init.method, 'POST');
  assert.deepEqual(req.init.headers, { 'Content-Type': 'application/json' });
  assert.equal(req.init.cache, 'no-store');
  // The seed goes as a string: a JSON number would lose precision above 2^53.
  assert.equal(req.init.body, '{"bots":["imp.s","dwarf.s"],"seed":"18446744073709551615"}');
  assert.equal(req.seed, '18446744073709551615');
  assert.equal(req.label, 'imp.s vs dwarf.s, seed 18446744073709551615');

  const same = buildMatchRequest({ first: 'a.s', second: 'a.s', seedText: ' 007 ' });
  assert.equal(same.init.body, '{"bots":["a.s","a.s"],"seed":"7"}');
});

test('buildMatchRequest refuses an incomplete or invalid picker state', () => {
  assert.throws(() => buildMatchRequest({ first: '', second: 'a.s', seedText: '1' }), /bot/);
  assert.throws(() => buildMatchRequest({ first: 'a.s', second: '', seedText: '1' }), /bot/);
  assert.throws(() => buildMatchRequest({ first: 'a.s', second: 'b.s', seedText: 'x' }), /seed/);
  assert.throws(() => buildMatchRequest({ first: 'a.s', second: 'b.s', seedText: '' }), /seed/);
  assert.throws(() => buildMatchRequest({ first: 'a.s', second: 'b.s', seedText: '18446744073709551616' }), /seed/);
});

test('describeMatchError shows the server message, and the position of a source error', () => {
  assert.equal(describeMatchError(422, '{"error":"bad.s:2:3: unknown opcode \\"BOGUS\\"","bot":"bad.s","line":2,"col":3,"message":"unknown opcode \\"BOGUS\\""}'), 'bad.s:2:3: unknown opcode "BOGUS"');
  assert.equal(describeMatchError(503, '{"error":"a match is already running; try again in a moment"}'), 'a match is already running; try again in a moment');
  assert.equal(describeMatchError(404, '{"error":"no such bot in the list"}'), 'no such bot in the list');
  assert.equal(describeMatchError(502, 'Bad Gateway'), 'The server answered 502.');
  assert.equal(describeMatchError(500, '{"error":42}'), 'The server answered 500.');
  assert.equal(describeMatchError(500, ''), 'The server answered 500.');
});
