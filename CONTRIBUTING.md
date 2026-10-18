# Contributing to pitfight

[AGENTS.md](AGENTS.md) holds the full working rules for people and coding agents.
This page summarizes them.

## Requirements

- Go 1.27.2, standard library only. The module has no `require` lines and no `go.sum`.
- Every new dependency needs owner approval first: Go modules, toolchains, wasm
  toolchains, JS packages. A proposal states why the standard library is not enough
  and confirms a FLOSS license.
- The web viewer (planned) is plain JavaScript ES modules with canvas, no framework
  and no build step. Its tests use `node:test`.
- Do not run `go env -w`, and do not add a Makefile.

## Checks

Run from the repo root, in bash or zsh, with `scripts/env.sh` sourced:

```sh
. scripts/env.sh
go test ./...
go vet ./...
gofmt -l cmd internal              # must print nothing
go build -o .cache/bin/pit ./cmd/pit
```

There is no CI yet, so run these locally before handing off a change.

## Rules

- Test first: write the failing test, watch it fail for the stated reason, then write
  the code. Use Go `testing`, table-driven where natural, `t.TempDir()` for files, and
  no `os.Chdir`.
- Use Go's built-in fuzzing where it fits. Commit failing fuzz inputs under
  `testdata/fuzz/`.
- The VM core is pure and deterministic: no I/O, no clocks, no map iteration in the VM
  or arena core, and randomness only from the in-repo seeded PRNG (never `math/rand`).
- Every Go and shell file starts with an `SPDX-License-Identifier: AGPL-3.0-or-later`
  comment.
- Do not bundle existing Core War warriors until their licensing has been checked.

## Tracking work with slicer

Work is planned and tracked with the `slicer` CLI. Read and change tracking state only
through it. Never open, search or edit `.slicer/` by hand.

- Once per clone, run `slicer setup-git` and run the `git config` lines it prints. They
  set up the merge drivers for the tracking files.
- Project decisions live in the catalog: `slicer catalog list --json --lean`. Catalog
  decisions override PROJECT.md where they differ.
- To pick up a slice:
  1. `slicer sections` lists the section names `next` needs.
  2. `slicer next --ready --section NAME ...` shows the next startable item.
  3. Read its scope, dependencies and checks. Resolve any gaps before you start.
  4. `slicer start ID --render --strict`.
- When an interface detail is left open (package API, CLI flag, output format, syntax,
  game rule), the implementer decides it and records it with
  `slicer catalog add --kind decision` and a one-line reason.
- Run `slicer check` before handoff. If generated render files conflict in a merge, run
  `slicer render`, then `slicer check`. Do not merge them by hand.
- `slicer recommended-workflow` prints the full workflow.

## Git

- Work in a worktree under `.worktrees/<name>`, on its own branch, one branch per
  slice. `.worktrees/` stays out of version control.
- Commit messages carry no AI attribution trailers.

## License

pitfight is licensed under AGPL-3.0-or-later (see [LICENSE](LICENSE) and
[NOTICE](NOTICE)). Contributions are accepted under the same license.
