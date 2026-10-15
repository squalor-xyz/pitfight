# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Source this from bash or zsh, by path: . scripts/env.sh (from the repo root).
#
# Keeps Go's caches and temp files inside this checkout under .cache/ (C34).
# The repo root is derived from this script's own path, so each worktree uses
# its own .cache/. Nothing is exported or created unless that root holds go.mod
# and scripts/env.sh; otherwise the script prints an error and returns 1 (exits 1
# if it was executed instead of sourced).
# Dash and other shells that are not bash or zsh cannot report the sourced
# path, so they fail that way.
# Go is told to use only the local toolchain and no module proxy.
# Never use `go env -w`: it writes outside the repo.

if [ -n "${BASH_VERSION-}" ]; then
  _pit_self=${BASH_SOURCE[0]}
elif [ -n "${ZSH_VERSION-}" ]; then
  eval '_pit_self=${(%):-%x}'
else
  _pit_self=
fi

if [ -z "$_pit_self" ]; then
  echo "env.sh: cannot tell where this script is; source it from bash or zsh" >&2
  unset _pit_self
  return 1 2>/dev/null || exit 1
fi

_pit_root=$(CDPATH= cd -- "$(dirname -- "$_pit_self")/.." 2>/dev/null && pwd -P)

if [ -z "$_pit_root" ] || [ ! -f "$_pit_root/go.mod" ] || [ ! -f "$_pit_root/scripts/env.sh" ]; then
  echo "env.sh: repo root not found from '$_pit_self' (no go.mod there); nothing was set" >&2
  unset _pit_self _pit_root
  return 1 2>/dev/null || exit 1
fi

_pit_cache="$_pit_root/.cache"

if ! mkdir -p "$_pit_cache/go-build" "$_pit_cache/gomod" "$_pit_cache/gopath" "$_pit_cache/tmp"; then
  echo "env.sh: cannot create the .cache directories under $_pit_root" >&2
  unset _pit_self _pit_root _pit_cache
  return 1 2>/dev/null || exit 1
fi

export GOCACHE="$_pit_cache/go-build"
export GOMODCACHE="$_pit_cache/gomod"
export GOPATH="$_pit_cache/gopath"
export GOTMPDIR="$_pit_cache/tmp"
export TMPDIR="$_pit_cache/tmp"
export GOTOOLCHAIN=local
export GOPROXY=off

unset _pit_self _pit_root _pit_cache
