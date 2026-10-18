> **Background only.** This is the original project brief, kept as written. The
> project is now named **pitfight** (CLI `pit`, C26), is written in Go with the
> standard library only (C28), and is licensed AGPL-3.0-or-later (C32). Where this
> file and the slicer catalog differ, the catalog wins
> (`slicer catalog list --json --lean`). See [README.md](README.md).

# bot-arena — a programming game where tiny programs fight (working name)

## Summary

A programming game in the spirit of Core War. Players write small programs ("bots") for
a tiny virtual machine. The bots are loaded into a shared circular memory arena, run in
turns, and try to make each other crash. bot-arena provides:
- the instruction set and an assembler
- a deterministic VM and arena
- a tournament runner
- a visualizer that shows the battle in memory as it happens
- (later) a harness where LLM agents write and iterate on bots as competitors

## Motivation

- Core War-style games are a compact, deep design space: simple rules, emergent
  strategy (bombers, replicators, scanners).
- Building one hits fun engineering: ISA design, an assembler, a deterministic VM,
  fair scheduling, a tournament system and visualization.
- LLM agents as competitors are a distinctive angle: an eval of agents' ability to
  write adversarial low-level code and improve it from match results.

## Users

- **Players:** write bots in the assembly language, run matches, and enter tournaments.
- **Spectators:** watch battles in the visualizer.
- **Agent harness (later):** LLM agents that read the rules, write bots, read results,
  and iterate.

## Goals

1. A small, well-specified instruction set with a written spec. The open decision is
   compatibility with the existing Redcode standard vs. a new ISA.
2. An assembler with labels, expressions, and clear error messages with line numbers.
3. A deterministic VM: the same bots, seed and config always give the same result,
   which makes replays and tests possible.
4. An arena with N bots, random non-overlapping start positions from a seed,
   round-robin process scheduling, a cycle limit and a defined tie rule.
5. A tournament runner: round-robin over many seeds, a scoreboard, reproducible results.
6. A visualizer: a terminal view and a web view of memory ownership and execution
   pointers, with step, play, pause and seek on recorded replays.
7. A replay format: compact, versioned, and able to drive the visualizer without
   re-simulating.

## Non-goals (v1)

- Online multiplayer, accounts, or a hosted ladder.
- LLM agent competitors (they're a later tier; v1 designs the CLI so they can plug in).
- Full compatibility with every historic Core War standard.
- A graphical bot editor or IDE.

## Core concepts / data model

- **Core:** a circular array of `CORESIZE` cells. All addresses are taken modulo
  `CORESIZE`.
- **Cell:** an instruction (opcode, modifiers, operand A/B with addressing modes) plus
  the ID of the last bot that wrote it, which drives the visualizer coloring.
- **Process:** an instruction pointer owned by a bot. Each bot has a process queue, up
  to `MAXPROCESSES`.
- **Turn:** each live bot executes one instruction from the front of its queue, then
  requeues it. A bot with no processes is dead.
- **Match:** a config (core size, cycle limit, max processes, min separation, seed)
  plus bots, producing a result (winner, survivors at the cycle limit, cycles run).
- **Replay:** the match config, bot hashes and an event stream (writes, process
  spawn/death per cycle), or a periodic snapshot plus a delta log.

## Feature tiers

### v1

- An ISA spec document plus a reference implementation.
- An assembler (`arena asm bot.s`), with disassembly for debugging.
- A VM and arena (`arena run a.s b.s --seed N`) with a text result.
- A tournament (`arena tourney bots/ --rounds R`) with a scoreboard.
- A terminal visualizer.
- A web visualizer that plays back replay files.
- A starter bot set (an imp, a bomber, a replicator, a scanner) for tests and examples.

### Later

- The LLM agent harness: a rules prompt, a submit-and-iterate loop, agent-vs-agent
  ladders. Opt-in, because it makes paid external API calls.
- A hill: a persistent ranked bot pool where new bots challenge the top N.
- Hosting the web visualizer on squalor.xyz.
- ISA variants and new game modes (teams, king-of-the-hill zones).

## Architecture sketch

```
asm (source → bytecode/cell list) ─┐
                                    ├─> vm core (pure, deterministic) ─> events ─> replay file
config + seed ─────────────────────┘                                             │
                                                         tui viewer  <───────────┤
                                                         web viewer  <───────────┘
tourney runner ── many matches (parallel across processes/threads) ── scoreboard
```

The VM core is pure: no I/O, no clocks, seeded RNG only for placement. Everything else
wraps it.

## Stack recommendation and alternatives

- **Recommended:** Rust for the assembler, VM and tournament runner. Fast enough for
  big tournaments, strong for exact integer semantics, and it compiles to WASM, so the
  web visualizer can run live matches with the identical core. The web viewer is plain
  TypeScript + canvas, without a framework.
- **Alternative A:** all TypeScript (Node for the CLI, browser for the viewer). One
  language and a simpler toolchain; slower tournaments.
- **Alternative B:** Python stdlib core plus a JS viewer. Fastest to prototype; too slow
  for large tournaments without optimization.

## Open decisions (each with a recommendation)

1. **Name.** Placeholder "bot-arena".
2. **ISA.** Options:
   - Implement a subset of the existing Redcode ICWS'94 standard (gives a body of
     existing warriors and literature to learn from).
   - Design a new small ISA (freedom, distinct identity).

   Recommendation: an original ISA strongly inspired by Redcode semantics. It's small
   enough to specify in a few pages, and Redcode compatibility can come later as a mode.
   This needs owner confirmation.
3. **Core language.** Recommendation: Rust core + WASM, with a TS viewer.
4. **Replay format.** Recommendation: versioned JSON lines for v1 (simple, debuggable),
   with a binary format later if size matters.
5. **Default match parameters** (core size, cycle limit, max processes). Recommendation:
   start from Redcode '94 hill norms (core 8000, cycles 80000, max processes 8000) and
   tune. These numbers are from general knowledge and need checking against a source
   during the spec slice.
6. **Visualizer priority.** Recommendation: terminal viewer first (it's needed for
   debugging), web viewer second.

## Risks

- **ISA balance:** some designs make one strategy dominant. Mitigation: the starter
  bots as a balance test, and tournament stats across seeds.
- **Determinism bugs:** any non-determinism breaks replays. Mitigation: replay-
  equivalence tests and golden match hashes in CI.
- **Scope creep** into the agent harness before the core is solid. Mitigation: the
  harness stays a later tier.
- **Toolchain:** Rust + wasm-pack (or equivalent) adds build dependencies. They need
  owner approval per the dependency rules.

## Milestones

1. The ISA spec document.
2. VM core with unit tests per opcode and addressing mode.
3. Assembler plus disassembler, with round-trip tests.
4. Arena and match runner, plus the starter bots and golden-result tests.
5. Replay format and the terminal viewer.
6. Tournament runner and scoreboard.
7. WASM build and the web viewer.

## v1 acceptance criteria

- The spec documents every opcode, modifier and addressing mode, and each one has
  tests.
- `arena run imp.s dwarf.s --seed 1` gives the same result and replay hash on every run
  and machine.
- The assembler rejects malformed input with line and column errors; asm → disasm → asm
  round-trips.
- A tournament of the starter bots over 100 seeds finishes and prints a stable
  scoreboard.
- The terminal and web viewers both play the same replay file correctly.

## Dependency / licensing notes

- The Rust standard library first. Every crate, wasm toolchain or JS package needs
  owner approval, with a stated reason and a FLOSS license.
- Core War / Redcode: the ICWS standards and existing warriors are public material,
  but their licensing has to be checked before bundling any existing warriors. This is
  not verified yet.
- License for the project itself: not yet chosen.
