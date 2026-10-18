# pitfight

A Core War-style programming game in Go: write tiny bots for a small virtual machine
and pit them against each other in a shared circular memory arena.

Players write bots in a small assembly language. The bots are loaded into a circular
core, take turns executing one instruction each, and try to make each other crash.
The same bots, seed and configuration always produce the same match.

## Status

Early development. What exists today:

- The instruction set spec, [docs/spec/isa.md](docs/spec/isa.md), and the match spec,
  [docs/spec/match.md](docs/spec/match.md).
- `internal/isa`: instruction types.
- `internal/vm`: core memory, operand evaluation, opcode execution, process queues
  and events.
- `pit`: the CLI. Today it only prints usage.

Planned: the assembler (`pit asm`), matches (`pit run`), round-robin tournaments
(`pit tourney`), a replay format, and terminal and web replay viewers (`pit view`).

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

## Layout

- `cmd/pit/`: the `pit` command.
- `internal/`: the packages (`isa` and `vm` so far).
- `docs/spec/`: the ISA and match specs.
- `scripts/env.sh`: Go cache and temp setup.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). [PROJECT.md](PROJECT.md) holds the original
project brief and is background reading only.

## License

Copyright (C) 2026 Squalor LLC. pitfight is free software under the GNU Affero General
Public License, version 3 or (at your option) any later version. See [LICENSE](LICENSE)
and [NOTICE](NOTICE).
