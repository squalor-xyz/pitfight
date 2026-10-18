// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// Wires the page: choose a replay (the start page's lists, a match run by the server,
// the file input, or ?replay=URL), build the model and player, run the play clock and
// draw. This and render.js are the only modules that
// touch the DOM; the logic lives in replay.js, model.js and player.js.

import { parseReplay } from './replay.js';
import { Model } from './model.js';
import { Player } from './player.js';
import { draw, botColor } from './render.js';
import { parseListing, listingOptions, defaultPicks, randomSeed, resolveSeed, buildMatchRequest, describeMatchError } from './home.js';

/**
 * @template {HTMLElement} T
 * @param {string} id
 * @returns {T}
 */
function el(id) {
  const e = document.getElementById(id);
  if (!e) throw new Error(`missing element #${id}`);
  return /** @type {T} */ (e);
}

const fileInput = /** @type {HTMLInputElement} */ (el('file'));
const sourceLabel = el('source');
const errorBox = el('error');
const intro = el('intro');
const viewer = el('viewer');
const playButton = el('play');
const speedSelect = /** @type {HTMLSelectElement} */ (el('speed'));
const cycleLabel = el('cycle');
const seekBar = /** @type {HTMLInputElement} */ (el('seek'));
const canvas = /** @type {HTMLCanvasElement} */ (el('canvas'));
const botList = el('bots');
const resultLine = el('result');
const fightForm = el('fight');
const bot1Select = /** @type {HTMLSelectElement} */ (el('bot1'));
const bot2Select = /** @type {HTMLSelectElement} */ (el('bot2'));
const seedInput = /** @type {HTMLInputElement} */ (el('seed'));
const fightButton = /** @type {HTMLButtonElement} */ (el('fight-button'));
const replaySection = el('replay-section');
const replayList = el('replay-list');
const listNote = el('list-note');

/** @type {Player | null} */
let player = null;
/** @type {Array<HTMLElement>} */
let botRows = [];
let lastTime = 0;
let carry = 0; // fractional cycles owed to the clock
let frameRequest = 0;
let loadToken = 0; // a newer request makes the answer of an older one stale
/** The seed field: `auto` while the page chose it and the user has not typed. */
let seedState = { text: '', auto: true };

/** @param {string} message */
function showError(message) {
  errorBox.textContent = message;
  errorBox.hidden = false;
}

/**
 * Loads replay text: parse, build the player, show the viewer.
 * @param {string} text
 * @param {string} name
 * @param {boolean} [autoplay]  start playing at once
 */
function load(text, name, autoplay = false) {
  errorBox.hidden = true;
  let replay;
  try {
    replay = parseReplay(text);
  } catch (e) {
    showError(`${name}: ${e instanceof Error ? e.message : e}`);
    return;
  }
  loadToken++;
  stopClock();
  player = new Player(new Model(replay));
  sourceLabel.textContent = `${name}: core ${replay.config.coreSize}, ${replay.bots.length} bots, ${player.cycles} cycles`;
  seekBar.max = String(player.cycles);

  botList.replaceChildren();
  botRows = replay.bots.map((bot) => {
    const li = document.createElement('li');
    const swatch = document.createElement('span');
    swatch.className = 'swatch';
    swatch.style.background = botColor(bot.id);
    const label = document.createElement('span');
    const pc = document.createElement('span');
    pc.className = 'pc';
    li.append(swatch, label, pc);
    botList.append(li);
    return li;
  });
  const r = replay.result;
  resultLine.textContent =
    r.outcome === 'win' ? `Result: bot ${r.winner} wins at cycle ${r.cycles}.` : `Result: tie between bots ${r.tied.join(', ')} at cycle ${r.cycles}.`;

  intro.hidden = true;
  viewer.hidden = false;
  render();
  if (autoplay) act(() => player?.play());
}

/** Redraws everything that shows the player's state. */
function render() {
  if (!player) return;
  const frame = player.frame();
  cycleLabel.textContent = `cycle ${frame.cycle} / ${player.cycles}`;
  seekBar.value = String(frame.cycle);
  playButton.textContent = player.playing ? 'Pause' : 'Play';
  frame.live.forEach((live, i) => {
    const row = botRows[i];
    const [label, pc] = /** @type {HTMLElement[]} */ (Array.from(row.children).slice(1));
    label.textContent = `bot ${i + 1}  live ${live}`;
    pc.textContent = frame.executed
      .filter((p) => p.bot === i + 1)
      .map((p) => p.pc)
      .join(',');
  });

  const dpr = window.devicePixelRatio || 1;
  const w = Math.max(1, Math.round(canvas.clientWidth * dpr));
  const h = Math.max(1, Math.round(canvas.clientHeight * dpr));
  if (canvas.width !== w) canvas.width = w;
  if (canvas.height !== h) canvas.height = h;
  const ctx = canvas.getContext('2d');
  if (ctx) draw(ctx, frame, w, h);
}

/** @param {number} time */
function onFrame(time) {
  frameRequest = 0;
  if (!player || !player.playing) return;
  carry += ((time - lastTime) / 1000) * Number(speedSelect.value);
  lastTime = time;
  for (; carry >= 1 && player.playing; carry--) player.tick();
  carry = player.playing ? carry : 0;
  render();
  if (player.playing) frameRequest = requestAnimationFrame(onFrame);
}

function startClock() {
  if (frameRequest) return;
  lastTime = performance.now();
  carry = 0;
  frameRequest = requestAnimationFrame(onFrame);
}

function stopClock() {
  if (frameRequest) cancelAnimationFrame(frameRequest);
  frameRequest = 0;
  player?.pause();
}

/** @param {() => void} action */
function act(action) {
  if (!player) return;
  action();
  render();
  if (player.playing) startClock();
}

fileInput.addEventListener('change', async () => {
  const file = fileInput.files?.[0];
  if (!file) return;
  try {
    load(await file.text(), file.name);
  } catch (e) {
    showError(`${file.name}: ${e instanceof Error ? e.message : e}`);
  } finally {
    // Give the keys back to the page (the input is ignored by the key handler while
    // it has focus), and let choosing the same file again fire a change event.
    fileInput.value = '';
    fileInput.blur();
  }
});

/** @param {{text: string, auto: boolean}} seed */
function setSeed(seed) {
  seedState = seed;
  seedInput.value = seed.text;
}

seedInput.addEventListener('input', () => {
  seedState = { text: seedInput.value, auto: false };
});
el('reroll').addEventListener('click', () => setSeed({ text: randomSeed(), auto: true }));

// Fight: ask the server to run the match of the two chosen bots and play its replay.
fightForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  if (fightButton.disabled) return;
  setSeed(resolveSeed(seedState, randomSeed));
  let request;
  try {
    request = buildMatchRequest({ first: bot1Select.value, second: bot2Select.value, seedText: seedState.text });
  } catch (err) {
    showError(err instanceof Error ? err.message : String(err));
    return;
  }
  const token = ++loadToken;
  fightButton.disabled = true;
  fightButton.textContent = 'Running...';
  try {
    const response = await fetch(request.url, request.init);
    const text = await response.text();
    if (token !== loadToken) return;
    if (response.ok) load(text, request.label, true);
    else showError(describeMatchError(response.status, text));
  } catch (err) {
    if (token === loadToken) showError(`Could not run the match: ${err instanceof Error ? err.message : err}`);
  } finally {
    fightButton.disabled = false;
    fightButton.textContent = 'Fight';
    // Space plays and pauses the replay, so the button must not keep the keys.
    fightButton.blur();
  }
});

/**
 * Fetches a replay from the server and plays it or, for ?replay= without &autoplay=1,
 * shows it paused.
 * @param {string} url
 * @param {string} name
 * @param {boolean} autoplay
 */
async function fetchReplay(url, name, autoplay) {
  const token = ++loadToken;
  try {
    const r = await fetch(url);
    if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
    const text = await r.text();
    if (token === loadToken) load(text, name, autoplay);
  } catch (e) {
    if (token === loadToken) showError(`${name}: ${e instanceof Error ? e.message : e}`);
  }
}

/**
 * Fills a bot picker.
 * @param {HTMLSelectElement} select
 * @param {Array<{value: string, label: string}>} options
 * @param {string} chosen
 */
function fillPicker(select, options, chosen) {
  select.replaceChildren(
    ...options.map((o) => {
      const option = document.createElement('option');
      option.value = o.value;
      option.textContent = o.label;
      return option;
    }),
  );
  select.value = chosen;
}

/** Shows the bots and replays that "pit serve" lists. Without a listing the page is the file picker only. */
async function showListing() {
  let options;
  try {
    const r = await fetch('/api/list', { cache: 'no-store' });
    if (!r.ok) return;
    options = listingOptions(parseListing(await r.json()));
  } catch {
    return; // not served by pit serve, or an older one: the file picker remains
  }
  if (options.bots.length > 0) {
    const [first, second] = defaultPicks(options.bots.map((b) => b.value));
    fillPicker(bot1Select, options.bots, first);
    fillPicker(bot2Select, options.bots, second);
    setSeed({ text: randomSeed(), auto: true });
    fightForm.hidden = false;
  }
  if (options.replays.length > 0) {
    replayList.replaceChildren(
      ...options.replays.map((replay) => {
        const li = document.createElement('li');
        const button = document.createElement('button');
        button.type = 'button';
        button.textContent = replay.label;
        button.title = replay.name;
        button.addEventListener('click', () => {
          button.blur();
          fetchReplay(replay.url, replay.name, true);
        });
        li.append(button);
        return li;
      }),
    );
    replaySection.hidden = false;
  }
  listNote.hidden = !options.truncated;
}

el('first').addEventListener('click', () => act(() => player?.seek(0)));
el('back').addEventListener('click', () => act(() => player?.step(-1)));
el('forward').addEventListener('click', () => act(() => player?.step(1)));
el('last').addEventListener('click', () => act(() => player?.seek(player.cycles)));
playButton.addEventListener('click', () => act(() => player?.toggle()));
seekBar.addEventListener('input', () => act(() => player?.seek(Number(seekBar.value))));
window.addEventListener('resize', render);

document.addEventListener('keydown', (e) => {
  // The slider, the file input and the speed menu have keys of their own.
  if (!player || e.target instanceof HTMLSelectElement || e.target instanceof HTMLInputElement || e.ctrlKey || e.metaKey || e.altKey) return;
  const keys = /** @type {Record<string, () => void>} */ ({
    ' ': () => player?.toggle(),
    ArrowLeft: () => player?.step(e.shiftKey ? -10 : -1),
    ArrowRight: () => player?.step(e.shiftKey ? 10 : 1),
    Home: () => player?.seek(0),
    End: () => player?.seek(player.cycles),
  });
  const key = keys[e.key];
  if (!key) return;
  // A button has its own Space handling; leave it alone.
  if (e.key === ' ' && e.target instanceof HTMLButtonElement) return;
  e.preventDefault();
  act(key);
});

// ?replay=URL loads a replay from a URL, for example one served by "pit serve
// --replays DIR" at /replays/NAME, and shows it paused; with &autoplay=1 (the address
// that "pit play" opens) it starts playing.
const params = new URLSearchParams(location.search);
const wanted = params.get('replay');
if (wanted) fetchReplay(wanted, wanted, params.get('autoplay') === '1');
showListing();
