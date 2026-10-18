// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// The logic of the start page: what "pit serve" lists (GET /api/list), the choice of
// two bots and a seed, and the request that runs the match (POST /api/match). No DOM
// and no network access: main.js wires these functions to the page and to fetch, and
// the node tests call them directly.

/**
 * The listing of GET /api/list: base names of the bots (.s) and replays (.jsonl).
 * @typedef {object} Listing
 * @property {string[]} bots
 * @property {string[]} replays
 * @property {boolean} truncated  the server cut a list that was too long
 */

/**
 * @typedef {object} BotOption
 * @property {string} value  the file name, as the server lists it
 * @property {string} label
 */

/**
 * @typedef {object} ReplayOption
 * @property {string} name   the file name
 * @property {string} label
 * @property {string} url    where "pit serve" serves it
 */

/** The largest seed: the seed of a match is an unsigned 64-bit integer. */
const MAX_SEED = 2n ** 64n - 1n;

/** The starter bots that the pickers start with. */
const STARTERS = ['imp.s', 'dwarf.s'];

/**
 * Checks the decoded JSON of GET /api/list.
 * @param {unknown} value
 * @returns {Listing}
 */
export function parseListing(value) {
  const v = /** @type {any} */ (value);
  const strings = (/** @type {unknown} */ x) => Array.isArray(x) && x.every((s) => typeof s === 'string');
  if (v === null || typeof v !== 'object' || !strings(v.bots) || !strings(v.replays)) {
    throw new Error('the server sent a listing that is not {"bots": [...], "replays": [...]}');
  }
  return { bots: v.bots, replays: v.replays, truncated: v.truncated === true };
}

/**
 * Where "pit serve --replays" serves a replay.
 * @param {string} name
 * @returns {string}
 */
export function replayUrl(name) {
  return '/replays/' + encodeURIComponent(name);
}

/**
 * Turns a listing into the options of the bot pickers and the replay list.
 * @param {Listing} listing
 * @returns {{bots: BotOption[], replays: ReplayOption[], truncated: boolean}}
 */
export function listingOptions(listing) {
  return {
    bots: listing.bots.map((value) => ({ value, label: value.replace(/\.s$/, '') })),
    replays: listing.replays.map((name) => ({ name, label: name.replace(/\.jsonl$/, ''), url: replayUrl(name) })),
    truncated: listing.truncated,
  };
}

/**
 * The bots the two pickers start with: the starter bots imp and dwarf if they are
 * listed, else the first two (one bot twice if there is only one, none if none).
 * @param {string[]} names  the listed bot names
 * @returns {[string, string]}
 */
export function defaultPicks(names) {
  const [imp, dwarf] = STARTERS;
  if (names.includes(imp) && names.includes(dwarf)) return [imp, dwarf];
  // One starter goes at its own end of the list, so that it is picked next to another bot.
  const ordered = [...names.filter((n) => n === imp), ...names.filter((n) => !STARTERS.includes(n)), ...names.filter((n) => n === dwarf)];
  const first = ordered[0] ?? '';
  return [first, ordered[1] ?? first];
}

/**
 * Parses the text of the seed field: a decimal from 0 to 2^64-1.
 * @param {string} text
 * @returns {string | null}  the seed without leading zeros, or null if it is not one
 */
export function parseSeed(text) {
  const t = text.trim();
  if (!/^[0-9]+$/.test(t)) return null;
  const n = BigInt(t);
  return n <= MAX_SEED ? n.toString() : null;
}

/**
 * A random seed, as a decimal string.
 * @param {(a: BigUint64Array) => BigUint64Array} [fill]  fills the array with random 64-bit words
 * @returns {string}
 */
export function randomSeed(fill = (a) => /** @type {BigUint64Array} */ (crypto.getRandomValues(a))) {
  const words = new BigUint64Array(1);
  fill(words);
  return words[0].toString();
}

/**
 * The seed to run the next match with. A seed that the page chose (auto) or an empty
 * one is replaced by a new random seed each time, so that Fight twice gives two
 * matches; a seed the user typed stays.
 * @param {{text: string, auto: boolean}} seed
 * @param {() => string} roll  returns a new random seed
 * @returns {{text: string, auto: boolean}}
 */
export function resolveSeed(seed, roll) {
  if (seed.auto || seed.text.trim() === '') return { text: roll(), auto: true };
  return seed;
}

/**
 * The request that runs a match, from the state of the pickers.
 * @param {{first: string, second: string, seedText: string}} state
 * @returns {{url: string, init: RequestInit & {headers: Record<string, string>}, seed: string, label: string}}
 * @throws {Error} if a bot is not chosen or the seed is not valid
 */
export function buildMatchRequest(state) {
  if (!state.first || !state.second) throw new Error('Choose two bots.');
  const seed = parseSeed(state.seedText);
  if (seed === null) throw new Error('The seed must be a whole number from 0 to 18446744073709551615.');
  return {
    url: '/api/match',
    init: {
      method: 'POST',
      // JSON is also what keeps a form on another site from sending this request.
      headers: { 'Content-Type': 'application/json' },
      cache: 'no-store',
      // The seed is a string: a JSON number would lose precision above 2^53.
      body: JSON.stringify({ bots: [state.first, state.second], seed }),
    },
    seed,
    label: `${state.first} vs ${state.second}, seed ${seed}`,
  };
}

/**
 * The text to show for a refused match request. The server's message names the file
 * and the position of an error in a bot ("bad.s:2:3: unknown opcode ...").
 * @param {number} status
 * @param {string} body
 * @returns {string}
 */
export function describeMatchError(status, body) {
  try {
    const e = JSON.parse(body)?.error;
    if (typeof e === 'string' && e !== '') return e;
  } catch {
    // Not JSON: a proxy or another server.
  }
  return `The server answered ${status}.`;
}
