#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Installs pit and the starter bots from a pitfight GitHub Release on macOS or
# Linux (amd64, arm64), with no root access needed:
#
#   curl -fsSL https://github.com/squalor-xyz/pitfight/releases/latest/download/install.sh | sh
#
# pit goes to $PREFIX/bin and the bots to $PREFIX/share/pitfight/bots. Environment:
#   PREFIX             install prefix, an absolute path (default $HOME/.local)
#   PITFIGHT_VERSION   release to install, for example v0.1.1 (default: the latest)
#   PITFIGHT_BASE_URL  releases URL (default https://github.com/squalor-xyz/pitfight/releases);
#                      for tests, which serve a fake release from a test server
#
# The archive is checked against the release's SHA256SUMS before anything is
# unpacked or installed; there is no way to skip that check. The script works in a
# temporary directory that it removes on exit and writes nothing outside PREFIX.

set -eu

default_base=https://github.com/squalor-xyz/pitfight/releases
base=${PITFIGHT_BASE_URL:-$default_base}
base=${base%/}

die() {
	printf 'pitfight install: %s\n' "$*" >&2
	exit 1
}

have() {
	command -v "$1" >/dev/null 2>&1
}

# A version is v, a digit, then letters, digits, dots and dashes: safe in a URL and a file name.
valid_version() {
	case $1 in
	v[0-9]*) ;;
	*) return 1 ;;
	esac
	case $1 in
	*[!0-9A-Za-z.-]*) return 1 ;;
	esac
	return 0
}

# fetch URL FILE
fetch() {
	if have curl; then
		curl --fail --silent --show-error --location $curl_opts -o "$2" "$1" </dev/null
	else
		wget -q $wget_opts -O "$2" "$1" </dev/null
	fi
}

# Platform.
case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) die "unsupported OS: $(uname -s) (macOS and Linux only; on Windows download the archive from $default_base)" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

# Prefix.
prefix=${PREFIX:-}
if [ -z "$prefix" ]; then
	[ -n "${HOME:-}" ] || die "set PREFIX or HOME"
	prefix=$HOME/.local
fi
case $prefix in
/*) ;;
*) die "PREFIX must be an absolute path: $prefix" ;;
esac
case $prefix in
?*/) prefix=${prefix%/} ;;
esac
bindir=$prefix/bin
sharedir=$prefix/share/pitfight

# Version.
version=${PITFIGHT_VERSION:-}
if [ -n "$version" ] && ! valid_version "$version"; then
	die "invalid PITFIGHT_VERSION: $version (expected something like v0.1.1)"
fi

# Tools. A hash tool is required: without one nothing can be verified.
if have sha256sum; then
	hasher=sha256sum
elif have shasum; then
	hasher="shasum -a 256"
else
	die "need sha256sum or shasum to verify the download"
fi
if ! have curl && ! have wget; then
	die "need curl or wget to download the release"
fi
curl_opts=
wget_opts=
if [ "$base" = "$default_base" ]; then
	curl_opts="--proto =https --proto-redir =https"
	wget_opts="--https-only"
fi

# Work directory, removed on exit; staged files in PREFIX are removed too.
staged_pit=
staged_bots=
work=
cleanup() {
	[ -z "$staged_pit" ] || rm -rf "$staged_pit"
	[ -z "$staged_bots" ] || rm -rf "$staged_bots"
	[ -z "$work" ] || rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
tmpbase=${TMPDIR:-/tmp}
work=$(mktemp -d "${tmpbase%/}/pitfight-install.XXXXXX") || die "cannot create a temporary directory"

# SHA256SUMS of the release: it names the archives and their hashes.
if [ -n "$version" ]; then
	sums_url=$base/download/$version/SHA256SUMS
else
	sums_url=$base/latest/download/SHA256SUMS
fi
sums=$work/SHA256SUMS
fetch "$sums_url" "$sums" || die "cannot download SHA256SUMS from $sums_url"

# Latest: the archive name in SHA256SUMS carries the version.
if [ -z "$version" ]; then
	names=$(awk -v os="$os" -v arch="$arch" 'NF == 2 { f = $2; sub(/^\*/, "", f); if (f ~ ("^pit_v[0-9A-Za-z.-]+_" os "_" arch "[.]tar[.]gz$")) print f }' "$sums")
	set -f # the unquoted expansion below must split, never glob
	set -- $names
	set +f
	[ "$#" -eq 1 ] || die "SHA256SUMS has no single archive for $os/$arch"
	version=${1#pit_}
	version=${version%_"$os"_"$arch".tar.gz}
	valid_version "$version" || die "invalid version in SHA256SUMS: $version"
fi
top=pit_${version}_${os}_${arch}
archive=$top.tar.gz

# The hash for exactly this archive name: one line, no other.
expected=$(awk -v n="$archive" 'NF == 2 { f = $2; sub(/^\*/, "", f); if (f == n) print $1 }' "$sums")
set -f # split, never glob
set -- $expected
set +f
[ "$#" -eq 1 ] || die "SHA256SUMS has no single entry for $archive"
expected=$1
case $expected in
*[!0-9a-f]*) die "SHA256SUMS entry for $archive is not a SHA-256 hash" ;;
esac
[ "${#expected}" -eq 64 ] || die "SHA256SUMS entry for $archive is not a SHA-256 hash"

fetch "$base/download/$version/$archive" "$work/$archive" || die "cannot download $archive"
actual=$($hasher "$work/$archive" </dev/null | awk '{ print $1 }')
[ "$actual" = "$expected" ] || die "checksum mismatch for $archive: expected $expected, got ${actual:-nothing}"

# The archive holds one directory, $top, and nothing else.
tar -tzf "$work/$archive" </dev/null >"$work/list" || die "cannot read archive $archive"
while IFS= read -r entry; do
	case $entry in
	"$top" | "$top"/*) ;;
	*) die "unexpected path in archive: $entry" ;;
	esac
	case $entry in
	.. | ../* | */../* | */..) die "unexpected path in archive: $entry" ;;
	esac
done <"$work/list"
mkdir "$work/x"
tar -xzf "$work/$archive" -C "$work/x" </dev/null || die "cannot unpack archive $archive"
src=$work/x/$top
[ -z "$(find "$work/x" ! -type d ! -type f)" ] || die "archive contains a link or special file"
[ -f "$src/pit" ] || die "archive has no pit binary"
[ -d "$src/bots" ] || die "archive has no bots directory"
nbots=0
for f in "$src"/bots/*; do
	[ -f "$f" ] || die "unexpected entry in the bots directory of the archive"
	nbots=$((nbots + 1))
done
[ "$nbots" -gt 0 ] || die "archive has no bots"

# Install: stage next to the destination (same file system), then rename into place.
mkdir -p "$bindir" "$sharedir/bots" || die "cannot create directories under $prefix"
staged_pit=$bindir/.pit.new.$$
staged_bots=$sharedir/.bots.new.$$
cp "$src/pit" "$staged_pit"
chmod 755 "$staged_pit"
mkdir "$staged_bots"
cp "$src"/bots/* "$staged_bots"/
chmod 644 "$staged_bots"/*
for f in "$staged_bots"/*; do
	mv -f "$f" "$sharedir/bots/${f##*/}"
done
mv -f "$staged_pit" "$bindir/pit"
rm -rf "$staged_bots"
staged_pit=
staged_bots=

printf 'installed pit %s to %s/pit\n' "$version" "$bindir"
printf 'installed %s bots to %s/bots\n' "$nbots" "$sharedir"
case :${PATH:-}: in
*:"$bindir":*) ;;
*)
	printf 'warning: %s is not on your PATH. Add it, for example:\n  export PATH="%s:$PATH"\n' "$bindir" "$bindir" >&2
	;;
esac
