# AGENTS.md

pitfight (CLI `pit`) is a programming game in the spirit of Core War: players
write small programs ("bots") for a tiny virtual machine, and the bots fight in
a shared circular memory arena. Go, standard library only.

## Source of truth

- Slicer catalog decisions override PROJECT.md where they differ. PROJECT.md
  still calls the project bot-arena, names a CLI `arena` and mentions Rust; read
  it as background. Decided: name `pitfight`, CLI `pit` (C26); module
  `github.com/squalor-xyz/pitfight`; Go (go1.27.2) standard library only, plain
  JavaScript plus canvas for the web viewer (C28); AGPL-3.0-or-later (C32); no
  database in v1 (C39).
- Read the catalog with `slicer catalog list --json --lean`.

## Stack

- Go 1.27.2, standard library only, for the assembler, VM, arena, tournament
  runner and terminal viewer. The module has no `require` lines and no `go.sum`.
- Web viewer: plain JavaScript ES modules with JSDoc types and `// @ts-check`,
  canvas, no framework, no build step. No `tsc` without owner approval. JS tests
  use `node:test` from the installed Node, with no npm packages (C28).

## Layout

- `cmd/pit/`: `main`. `run(args, stdout, stderr)` returns the exit code; today
  it handles usage only.
- `internal/isa`, `asm`, `vm`, `arena`, `replay`, `tourney`, `tui`: the
  packages, one slice at a time. Only `internal/isa` (instruction types) and
  `internal/vm` (core memory, operand evaluation, opcode execution, process queues and events)
  exist so far.
- `web/`: the web viewer. `docs/spec/`: the ISA and format specs (`isa.md`,
  `match.md`). `bots/`: the starter bots. `web/` and `bots/` do not exist yet.
- `scripts/env.sh`: Go cache and temp setup (see below).
  `scripts/build-release.sh`: release archives. `.github/workflows/`: CI and
  release.
- `assets/pitfight-512.png`: the logo, an unmodified copy of the master in the
  private Squalor branding repo. All rights reserved, not AGPL; never edit,
  resize or recolor it here. Changes go through branding.
- `README.md`: project overview for readers. `CONTRIBUTING.md`: contributor
  summary of these rules, including slicer use. Keep both in step with this file.

## Setup and checks

Run from the repo root:

```sh
. scripts/env.sh                   # Go caches and temp files stay under .cache/ (C34)
go test ./...
go vet ./...
gofmt -l cmd internal              # must print nothing
go build -o .cache/bin/pit ./cmd/pit
```

`scripts/env.sh` must be sourced by path from bash or zsh. Dash and other shells
that cannot report the sourced path make it print an error and return 1, setting
and creating nothing. It also refuses to run unless its repo root holds `go.mod`
and `scripts/env.sh`. Run every Go command with it sourced. Never run
`go env -w`; it writes outside the repo. Do not add a Makefile.

CI runs on GitHub Actions (C63): `.github/workflows/ci.yml` runs the checks above
on pushes to `main` and on pull requests; still run them locally before handoff.
Pushing a tag `v*` runs `release.yml`, which reruns the checks, builds archives
with `scripts/build-release.sh VERSION OUTDIR` and publishes a GitHub Release.
Workflows use only `actions/checkout` and `actions/setup-go`, pinned by commit
SHA, plus the runner's `gh`; any other action needs owner approval (C13).

## Rules

- VM core is pure (C12): no I/O, no clocks; seeded randomness for placement
  only. Determinism (C36): no map iteration in the VM or arena core; randomness
  comes only from an in-repo seeded PRNG with a fixed, documented algorithm,
  never `math/rand` or `math/rand/v2`.
- Every dependency needs owner approval (C13): Go modules, toolchains, wasm
  toolchains, JS packages. The Go toolchain and its stock `GOOS=js GOARCH=wasm`
  target are approved (C28).
- Do not bundle existing warriors until their licensing is checked (C14).
- Test-first: write the failing test, watch it fail for the stated reason, then
  write the code. Use Go `testing`, table-driven where natural, `t.TempDir()`
  for files, and no `os.Chdir`.
- Use Go's built-in fuzzing where natural (C37). A fuzz target keeps a seed
  corpus, so plain `go test ./...` runs it as a regular test. Failing fuzz inputs
  under `testdata/fuzz/` are committed; do not ignore them.
- Interface details that PROJECT.md and the catalog leave open (package APIs,
  CLI flags and output formats, syntax details, game rules) are decided by the
  slice implementer (C35, C38). Record each with
  `slicer catalog add --kind decision` and a one-line reason. The reviewer
  checks it and the owner may overrule it.
- Every Go and shell file starts with an `SPDX-License-Identifier:
  AGPL-3.0-or-later` comment.
- Work is tracked in slicer. Read and change tracking state only through the
  `slicer` CLI; never open, search or edit `.slicer/`.

## License

AGPL-3.0-or-later, copyright Squalor LLC (C32). `NOTICE` holds the copyright line
and the "or any later version" notice. `LICENSE` is the verbatim AGPL-3.0 text,
taken from github/choosealicense.com with only its YAML front matter removed;
C32 records the source and checksum. Never edit, rewrap or paraphrase it.

## Git

- Work in a worktree under `.worktrees/<name>` on a branch per slice; keep
  `.worktrees/` out of version control.
- No commits unless the owner asks. No AI attribution in commits or PR text.
