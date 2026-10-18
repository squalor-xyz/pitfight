#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Cross-compiles pit for the release platforms and writes one archive per
# platform plus SHA256SUMS into OUTDIR. Run from anywhere, by path:
#   scripts/build-release.sh VERSION OUTDIR
# VERSION (for example v0.1.0) only names the archives. Each archive holds the
# pit binary, LICENSE, NOTICE and README.md. Used by .github/workflows/release.yml.

set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "usage: scripts/build-release.sh VERSION OUTDIR" >&2
	exit 2
fi
version=$1
outdir=$2

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
. "$root/scripts/env.sh"

mkdir -p "$outdir"
outdir=$(cd "$outdir" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	goos=${target%/*}
	goarch=${target#*/}
	name="pit_${version}_${goos}_${goarch}"
	bin=pit
	if [ "$goos" = windows ]; then
		bin=pit.exe
	fi
	mkdir -p "$stage/$name"
	(cd "$root" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
		go build -trimpath -ldflags='-s -w' -o "$stage/$name/$bin" ./cmd/pit)
	cp "$root/LICENSE" "$root/NOTICE" "$root/README.md" "$stage/$name/"
	if [ "$goos" = windows ]; then
		(cd "$stage" && zip -qr "$outdir/$name.zip" "$name")
	else
		tar -C "$stage" -czf "$outdir/$name.tar.gz" "$name"
	fi
done

(cd "$outdir" && shasum -a 256 pit_"${version}"_* > SHA256SUMS)
