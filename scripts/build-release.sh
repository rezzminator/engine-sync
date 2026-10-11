#!/usr/bin/env bash
# build-release.sh [--write-sums] [--native-only] [--out DIR]
#
# Builds the four release assets, engine-sync_{version}_{os}_{arch} for linux and
# darwin on amd64 and arm64, into DIR (default dist/) and writes DIR/SHA256SUMS,
# one `{sha256}  {asset}` line per asset, sorted by asset. The build is
# reproducible: the same sources and toolchain give the same bytes in any
# directory. --write-sums also copies the sums to
# plugins/engine-sync/libexec/SHA256SUMS, the file the wrapper verifies against.
#
# The version is plugin.json's. The build refuses any Go toolchain but the
# pinned one: a different compiler yields different bytes, so sums committed
# from it would never match CI's.
set -euo pipefail

readonly TOOLCHAIN="go1.27.1"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/dist"
write_sums=""
native_only=""

usage() {
  echo "Usage: build-release.sh [--write-sums] [--native-only] [--out DIR]" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --write-sums) write_sums=1; shift ;;
    --native-only) native_only=1; shift ;;
    --out) [[ $# -ge 2 ]] || usage; out="$2"; shift 2 ;;
    *) usage ;;
  esac
done

if [[ -n "$native_only" && -n "$write_sums" ]]; then
  echo "build-release: --native-only cannot replace the four-platform checksum manifest" >&2
  usage
fi

plugin_json="$root/plugins/engine-sync/.claude-plugin/plugin.json"
if [[ ! -r "$plugin_json" ]]; then
  echo "build-release: cannot read $plugin_json" >&2
  exit 2
fi
version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$plugin_json")"
if [[ -z "$version" ]]; then
  echo "build-release: no version in $plugin_json" >&2
  exit 2
fi

found="$(go env GOVERSION)"
if [[ "$found" != "$TOOLCHAIN" ]]; then
  echo "build-release: $TOOLCHAIN required, found $found" >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
elif command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
else
  echo "build-release: neither shasum nor sha256sum found" >&2
  exit 1
fi

mkdir -p "$out"
out="$(cd "$out" && pwd)"
# A previous build's assets must not linger: the release uploads dist/engine-sync_*.
rm -f "$out"/engine-sync_* "$out/SHA256SUMS"

# Sorted by asset name: the sums file takes this order.
targets="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"
expected=4
if [[ -n "$native_only" ]]; then
  targets="$(go env GOHOSTOS)/$(go env GOHOSTARCH)"
  expected=1
fi

cd "$root"
for target in $targets; do
  goos="${target%/*}"
  goarch="${target#*/}"
  asset="engine-sync_${version}_${goos}_${goarch}"
  # The environment is pinned so an ambient GOFLAGS, GOEXPERIMENT or
  # GOAMD64 cannot change the bytes.
  env -u GOFLAGS -u GOEXPERIMENT GOAMD64=v1 GOARM64=v8.0 \
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid= -X github.com/rezzminator/engine-sync/internal/cli.Version=$version" \
    -o "$out/$asset" ./cmd/engine-sync
  if [[ "$goos" == "$(go env GOHOSTOS)" && "$goarch" == "$(go env GOHOSTARCH)" ]]; then
    reported="$("$out/$asset" version)" || { echo "build-release: cannot execute $asset for version proof" >&2; exit 2; }
    [[ "$reported" == "engine-sync $version" ]] || { echo "build-release: $asset reports $reported" >&2;exit 1; }
    echo "build-release: VERSION PASS $asset reports $reported"
  fi
done

sums="$out/SHA256SUMS"
: > "$sums"
for target in $targets; do
  asset="engine-sync_${version}_${target%/*}_${target#*/}"
  printf '%s  %s\n' "$(sha256 "$out/$asset")" "$asset" >> "$sums"
done
count="$(wc -l < "$sums" | tr -d ' ')"
if [[ "$count" -ne "$expected" ]]; then
  echo "build-release: expected $expected assets in $out, found $count" >&2
  exit 1
fi

if [[ -n "$write_sums" ]]; then
  cp "$sums" "$root/plugins/engine-sync/libexec/SHA256SUMS"
  echo "build-release: wrote plugins/engine-sync/libexec/SHA256SUMS"
fi
echo "build-release: $count assets for $version in $out"
