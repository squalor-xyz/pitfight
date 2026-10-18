// @ts-check
// SPDX-License-Identifier: AGPL-3.0-or-later

// Wires the page: choose a replay (file input, or ?replay=URL), build the model and
// player, run the play clock and draw. This and render.js are the only modules that
// touch the DOM; the logic lives in replay.js, model.js and player.js.

import { parseReplay } from './replay.js';
import { Model } from './model.js';
import { Player } from './player.js';
import { draw, botColor } from './render.js';

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

/** @type {Player | null} */
let player = null;
/** @type {Array<HTMLElement>} */
let botRows = [];
let lastTime = 0;
let carry = 0; // fractional cycles owed to the clock
let frameRequest = 0;

/** @param {string} message */
function showError(message) {
  errorBox.textContent = message;
  errorBox.hidden = false;
}

/**
 * Loads replay text: parse, build the player, show the viewer.
 * @param {string} text
 * @param {string} name
 */
function load(text, name) {
  errorBox.hidden = true;
  let replay;
  try {
    replay = parseReplay(text);
  } catch (e) {
    showError(`${name}: ${e instanceof Error ? e.message : e}`);
    return;
  }
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
// --replays DIR" at /replays/NAME.
const wanted = new URLSearchParams(location.search).get('replay');
if (wanted) {
  fetch(wanted)
    .then((r) => (r.ok ? r.text() : Promise.reject(new Error(`${r.status} ${r.statusText}`))))
    .then((text) => load(text, wanted))
    .catch((e) => showError(`${wanted}: ${e instanceof Error ? e.message : e}`));
}
