#!/usr/bin/env bash
# Tests for leak-check.sh's report when no terms file is supplied. A scratch copy of
# the script sits where no scripts/leak-terms.txt exists, so the default is absent.
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/leak-check.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/leak-check-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir "$work/scripts"
cp "$script" "$work/scripts/leak-check.sh"
printf 'clean text\n' > "$work/subject.txt"
printf 'fixture-private-token\n' > "$work/terms.txt"
: > "$work/empty-terms.txt"

failures=0
# expect NAME RC WORDS -- ENV...: leak-check on the subject with ENV; its exit code is RC and its output holds WORDS.
expect() {
  local name="$1" want_rc="$2" words="$3" out rc=0
  shift 4
  out="$(env -u LEAK_TERMS "$@" bash "$work/scripts/leak-check.sh" --files "$work/subject.txt" 2>&1)" || rc=$?
  if [[ "$rc" != "$want_rc" || "$out" != *"$words"* ]]; then
    printf 'leak-check_test: %s: want rc=%s with "%s", got rc=%s output: %s\n' "$name" "$want_rc" "$words" "$rc" "$out" >&2
    failures=$((failures + 1))
  fi
}

expect "unset LEAK_TERMS and no default file is a gap" 3 "leak-check: GAP — no terms file supplied" -- UNUSED=1
expect "empty LEAK_TERMS is not supplied" 3 "leak-check: GAP — no terms file supplied" -- LEAK_TERMS=
expect "LEAK_TERMS naming an absent file fails" 1 "leak-check: FAILED — terms file not found" -- LEAK_TERMS="$work/absent.txt"
expect "LEAK_TERMS naming an empty file fails" 1 "leak-check: FAILED — terms file has zero usable terms" -- LEAK_TERMS="$work/empty-terms.txt"
expect "supplied terms scan clean" 0 "leak-check: clean: 1 files, 1 terms" -- LEAK_TERMS="$work/terms.txt"
printf 'fixture-private-token\n' > "$work/scripts/leak-terms.txt"
expect "the default terms file beside the script is used" 0 "leak-check: clean: 1 files, 1 terms" -- UNUSED=1

if (( failures > 0 )); then
  echo "LEAK CHECK TESTS FAIL — $failures case(s)" >&2
  exit 1
fi
echo "LEAK CHECK TESTS PASS — unsupplied terms gap, unusable terms failure, supplied and default terms"
