<img src="assets/pitfight-512.png" alt="pitfight logo" width="128">

# pitfight

A Core War-style programming game in Go: write tiny bots for a small virtual machine
and pit them against each other in a shared circular memory arena.

Players write bots in a small assembly language. The bots are loaded into a circular
core, take turns executing one instruction each, and try to make each other crash.
The same bots, seed and configuration always produce the same match.

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
- `pit`: the CLI. Today it prints usage and has `pit asm bot.s`, which assembles a bot
  and prints the cells, and `pit run a.s b.s --seed 1`, which runs a match and prints
  the result (`--json` for one line of JSON; `--replay FILE` also writes the replay
  and prints its hash; `pit run` with no files lists the flags).
- `bots/`: four original starter bots (`imp.s`, `dwarf.s`, `replicator.s`,
  `scanner.s`); golden tests in `internal/arena` pin their match results. Try
  `pit run bots/imp.s bots/dwarf.s --seed 1`.
- `internal/tourney`: round-robin tournaments over many seeds on parallel workers, with
  a reproducible scoreboard ([docs/spec/tourney.md](docs/spec/tourney.md)). Try
  `pit tourney bots/ --rounds 100`.

Planned: terminal and web replay viewers (`pit view`).

## Build and test

Requires Go 1.27.2. The module uses only the standard library. Run from the repo root
in bash or zsh:

```sh
. scripts/env.sh                   # keeps Go caches and temp files under .cache/
go test ./...
go vet ./...
gofmt -l cmd internal              # must print nothing
go build -o .cache/bin/pit ./cmd/pit
```

## Releases

Pushing a tag such as `v0.1.0` runs the checks and publishes a GitHub Release with
`pit` built for Linux, macOS and Windows on amd64 and arm64 (each archive also holds
the starter bots in `bots/`), plus a `SHA256SUMS` file. To build the same archives
locally, run `scripts/build-release.sh v0.1.0 dist`.

## Layout

- `cmd/pit/`: the `pit` command.
- `internal/`: the packages (`isa`, `asm`, `vm`, `arena`, `replay` and `tourney` so far).
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
