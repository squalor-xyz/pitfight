<img src="assets/pitfight-512.png" alt="pitfight logo" width="128">

# pitfight

A Core War-style programming game in Go: write tiny bots for a small virtual machine
and pit them against each other in a shared circular memory arena.

Players write bots in a small assembly language. The bots are loaded into a circular
core, take turns executing one instruction each, and try to make each other crash.
The same bots, seed and configuration always produce the same match.

## Quick start

Build `pit` (see [Build and test](#build-and-test)) and, from the repo root, run

```sh
.cache/bin/pit play bots/imp.s bots/dwarf.s
```

It runs the match, serves the web viewer on 127.0.0.1 with the match loaded, opens your
browser on it and plays it; press Ctrl-C to stop. The seed is random and printed, so
`--seed N` repeats a match; `--terminal` plays it in the terminal instead (the seed is then printed when the
viewer ends with `q`, so pass `--seed N` for a match you may stop with Ctrl-C),
`--no-open` only prints the address, and `--replay FILE` also saves the replay.

## Status

Early development. What exists today:

- The instruction set spec, [docs/spec/isa.md](docs/spec/isa.md), the assembly
  language spec, [docs/spec/asm.md](docs/spec/asm.md), the match spec,
  [docs/spec/match.md](docs/spec/match.md), and the replay file spec,
  [docs/spec/replay.md](docs/spec/replay.md).
- `internal/isa`: instruction types.
- `internal/asm`: the assembler, with labels, expressions and errors that carry a line
  and column, and the disassembler (`pit asm` prints its output).
- `internal/vm`: core memory, operand evaluation, opcode execution, process queues
  and events.
- `internal/arena`: seeded placement, round-robin scheduling, the cycle limit and the
  result and tie rule of a match.
- `internal/replay`: the replay file (`docs/spec/replay.md`): versioned JSON lines with
  a stable SHA-256 hash, a strict reader, and a player that rebuilds the core and the
  executed addresses from the file without running the match.
- `internal/tui`: the terminal viewer: a frame model of a replay (cell owners, executed
  addresses, live processes per cycle), a step/play/pause/seek player and an ANSI
  renderer, standard library only. `testdata/viewer/` holds the shared viewer fixtures
  and the frames every viewer must produce from them.
- `internal/acceptance`: test-only. It builds `pit` and checks the five v1 acceptance
  criteria (spec coverage, the imp against dwarf result and replay hash, assembler
  errors and round trips, a stable 100-round tournament scoreboard, viewer parity).
  The web half of viewer parity runs under `node --test web/test/*.test.js`, not
  under `go test`, and canvas drawing is checked by hand in a browser.
- `pit`: the CLI. Today it prints usage and has `pit asm bot.s`, which assembles a bot
  and prints the cells, `pit run a.s b.s --seed 1`, which runs a match and prints
  the result (`--json` for one line of JSON; `--replay FILE` also writes the replay
  and prints its hash; `pit run` with no files lists the flags), and `pit view
  replay.jsonl`, which plays a replay in the terminal (commands typed as lines:
  `s` step, `b` back, `g N` seek, `p` play/pause, `p N` play N cycles, `q` quit;
  `pit view --help` lists them), and `pit play a.s b.s`, which runs a match and shows
  it in the browser (or with `--terminal` in the terminal viewer) in one command.
- `bots/`: four original starter bots (`imp.s`, `dwarf.s`, `replicator.s`,
  `scanner.s`); golden tests in `internal/arena` pin their match results. Try
  `pit run bots/imp.s bots/dwarf.s --seed 1`.
- `internal/tourney`: round-robin tournaments over many seeds on parallel workers, with
  a reproducible scoreboard ([docs/spec/tourney.md](docs/spec/tourney.md)). Try
  `pit tourney bots/ --rounds 100`.

- `web/`: the web viewer: plain JavaScript ES modules and canvas, no framework and
  no build step. It plays a replay with step, play, pause and seek, and shows the
  same frames as the terminal viewer. `pit serve` serves it on 127.0.0.1 (a browser
  cannot read files from `file://`). Its start page lists the bots (`*.s`) of
  `--bots DIR` (default `bots`, if it exists) and the replays (`*.jsonl`) of
  `--replays DIR`: pick two bots, optionally set the seed, press Fight and the
  server runs the match and the page plays it; or click a replay. Try
  `pit serve --replays testdata/viewer` from the repo root. A replay file can still
  be chosen with the file picker, and `/?replay=/replays/NAME` loads one by address.
  A match runs with the defaults of `pit run` (cycle limit 80000), one at a time,
  and only for a page of this server. The viewer is built into the `pit` binary.

## Build and test

Requires Go 1.27.2. The module uses only the standard library. Run from the repo root
in bash or zsh:

```sh
. scripts/env.sh                   # keeps Go caches and temp files under .cache/
go test ./...
go vet ./...
gofmt -l cmd internal web          # must print nothing
go build -o .cache/bin/pit ./cmd/pit
node --test web/test/*.test.js     # web viewer tests; needs a Node that runs .js ES modules without package.json (26.12.0 verified)
```

## Releases

Pushing a tag such as `v0.1.0` runs the checks and publishes a GitHub Release with
`pit` built for Linux, macOS and Windows on amd64 and arm64 (each archive also holds
the starter bots in `bots/`), plus a `SHA256SUMS` file. To build the same archives
locally, run `scripts/build-release.sh v0.1.0 dist`.

## Layout

- `cmd/pit/`: the `pit` command.
- `internal/`: the packages (`isa`, `asm`, `vm`, `arena`, `replay`, `tourney` and `tui` so far), and
  `acceptance`, the test-only v1 acceptance checks that run the built `pit`.
- `testdata/viewer/`: the shared viewer fixtures (replays) and their expected frames.
- `bots/`: the starter bots.
- `docs/spec/`: the ISA, assembly, match, replay and tournament specs.
- `scripts/env.sh`: Go cache and temp setup. `scripts/build-release.sh`: release
  archives.
- `.github/workflows/`: CI and tagged releases.
- `assets/`: the logo (not AGPL; see License).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). [PROJECT.md](PROJECT.md) holds the original
project brief and is background reading only.

## License

Copyright (C) 2026 Squalor LLC. pitfight is free software under the GNU Affero General
Public License, version 3 or (at your option) any later version. See [LICENSE](LICENSE)
and [NOTICE](NOTICE).

The pitfight logo (`assets/pitfight-512.png`) is not covered by the AGPL. It is
Copyright 2026 Squalor LLC, all rights reserved, and is included here under a narrow
grant from Squalor LLC to use it unmodified in this repository's `assets/` and README.
