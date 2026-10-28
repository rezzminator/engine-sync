#!/usr/bin/env bash
# release-check.sh [--release] [BASE_VERSION]
#
# Passes only when every place that carries engine-sync's version says the same
# one:
#   - plugins/engine-sync/.claude-plugin/plugin.json  "version"   (the reference)
#   - README.md                                     the badge/version-{X}- badge
#   - CHANGELOG.md                                  a `## [{X}]` heading
# --release also requires the CHANGELOG heading to carry a date,
# `## [{X}] - YYYY-MM-DD`, and the committed SHA256SUMS to equal what a fresh
# build (build-release.sh) produces:
#   - plugins/engine-sync/libexec/SHA256SUMS          equal to build-release.sh's
# Only a release commit refreshes SHA256SUMS (build-release.sh --write-sums),
# since the sums bake in the sources; on develop between releases they differ
# from a fresh build, so a plain check leaves them uncompared and says so.
# BASE_VERSION requires the version to sort after it.
#
# Exit 0 passed; 1 a stale file or a disagreeing place, named; 2 a file that
# cannot be read, or a usage error.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
release=""
base=""

usage() {
  echo "Usage: release-check.sh [--release] [BASE_VERSION]" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release) release=1; shift ;;
    -*) usage ;;
    *) [[ -z "$base" ]] || usage; base="$1"; shift ;;
  esac
done

plugin_json="plugins/engine-sync/.claude-plugin/plugin.json"
readme="README.md"
changelog="CHANGELOG.md"
sums="plugins/engine-sync/libexec/SHA256SUMS"

for f in "$plugin_json" "$readme" "$changelog" "$sums"; do
  if [[ ! -r "$root/$f" ]]; then
    echo "release-check: cannot read $f" >&2
    exit 2
  fi
done

json_version() {
  python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); print(value["version"] if "version" in value else next(p["version"] for p in value["plugins"] if p["name"]=="engine-sync"))' "$root/$1"
}

version="$(json_version "$plugin_json")"
if [[ -z "$version" ]]; then
  echo "release-check: no \"version\" in $plugin_json" >&2
  exit 1
fi

bad=0
disagree() {
  echo "release-check: $1 says '$2', $plugin_json says '$version'" >&2
  bad=1
}

badge_version="$(sed -n 's/.*badge\/version-\([0-9][0-9A-Za-z.+_]*\)-.*/\1/p' "$root/$readme" | head -n 1)"
[[ "$badge_version" == "$version" ]] || disagree "$readme badge" "${badge_version:-nothing}"

heading_re="^## \\[$(printf '%s' "$version" | sed 's/[.[\*^$]/\\&/g')\\]"
if ! grep -q "$heading_re" "$root/$changelog"; then
  echo "release-check: $changelog has no '## [$version]' heading ($plugin_json says '$version')" >&2
  bad=1
elif [[ -n "$release" ]] && ! grep -Eq "${heading_re} (-|—) [0-9]{4}-[0-9]{2}-[0-9]{2}\$" "$root/$changelog"; then
  echo "release-check: $changelog has no '## [$version] - YYYY-MM-DD' heading; a release carries its date" >&2
  bad=1
fi

if [[ -n "$base" ]]; then
  if [[ "$base" == "$version" ]] || [[ "$(printf '%s\n%s\n' "$base" "$version" | sort -V | tail -n 1)" != "$version" ]]; then
    echo "release-check: version $version does not sort after $base" >&2
    bad=1
  fi
fi

if [[ "$bad" -ne 0 ]]; then
  exit 1
fi

if [[ -z "$release" ]]; then
  echo "release-check: ok: $version (places agree; $sums not compared, only --release compares it with a fresh build)"
  exit 0
fi

# The sums bake in the version, so they are compared only once the places agree.
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if ! "$root/scripts/build-release.sh" --out "$tmp" >"$tmp/build.log" 2>&1; then
  echo "release-check: build-release.sh failed, so $sums cannot be compared:" >&2
  tail -30 "$tmp/build.log" >&2
  exit 1
fi
if ! diff -u --label "committed $sums" --label "fresh build" "$root/$sums" "$tmp/SHA256SUMS" >"$tmp/sums.diff"; then
  echo "release-check: $sums is stale: it differs from a fresh build (scripts/build-release.sh --write-sums):" >&2
  cat "$tmp/sums.diff" >&2
  exit 1
fi

echo "release-check: ok: $version (release)"
