#!/usr/bin/env bash
# leak-check.sh [--files f1 [f2 ...]]
#
# The leak gate: nothing that identifies a person or a machine may reach this
# public repo. It scans every tracked or untracked-but-not-ignored file
# (`git ls-files --cached --others --exclude-standard`), or the named files,
# for:
#   - a machine-absolute path: a person's home under the Users or home root, or the macOS private temp tree;
#   - an email address, except noreply@anthropic.com and @example.com or
#     @example.org addresses, and the invented fixture homes BENIGN_PATH names;
#   - a private term from the terms file.
# Case-insensitive. A hit prints `path:line`, never the line: the terms are
# secret, and this output reaches public CI logs.
#
# THE PRIVATE TERMS ARE NOT IN THIS FILE. A gate that spells out the names it
# hunts publishes them itself. They live in scripts/leak-terms.txt (untracked,
# gitignored) or the file the LEAK_TERMS environment variable names: one
# lowercase regex per line, `#` comments, and
#   !ignore-token <lowercase regex>   removed from a line before it is judged, so
#                                     a known-public string a term matches inside
#                                     stops failing while a real leak on the same
#                                     line still does.
# A MISSING OR EMPTY TERMS FILE IS A FAILURE, never a pass, and so is a scan of
# zero files: "no denylist" and "found no leak" must not print the same word.
#
# The structural patterns below are bracket-classed so they cannot match their
# own source text; this script passes the scan it runs.
set -euo pipefail
export LC_ALL=C

# Machine-absolute paths and an email address. They name nobody.
STRUCTURAL_PATTERN='/Users/[A-Za-z0-9]|/home/[A-Za-z0-9]|/private/va[r]|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}'
# Emails that match the pattern and name nobody, removed from a line before it
# is judged, so a real address on the same line still fails.
BENIGN_EMAIL='noreply@anthropic[.]com|[A-Za-z0-9._%+-]+@example[.](com|org)'
# The invented homes the moved compiler tests use as fixture paths; removed
# like the benign emails, so a real home on the same line still fails.
BENIGN_PATH='/home/(test|x)/'

usage() {
  echo "Usage: leak-check.sh [--files f1 [f2 ...]]" >&2
  exit 2
}

mode="repo"
files=()
if [[ $# -gt 0 ]]; then
  [[ "$1" == "--files" && $# -ge 2 ]] || usage
  mode="files"
  shift
  files=("$@")
fi

script_dir="$(cd "$(dirname "$0")" && pwd)"
terms_file="${LEAK_TERMS:-$script_dir/leak-terms.txt}"

if [[ ! -f "$terms_file" ]]; then
  echo "leak-check: FAILED — terms file not found: $terms_file" >&2
  echo "leak-check: the private denylist is untracked by design (see .gitignore)." >&2
  echo "leak-check: create it as one lowercase regex per line ('#' comments)," >&2
  echo "leak-check: or point LEAK_TERMS at one. Refusing to report clean with no denylist." >&2
  exit 1
fi

terms=()
ignore_tokens=()
while IFS= read -r term || [[ -n "$term" ]]; do
  term="${term%$'\r'}"
  [[ "$term" =~ ^[[:space:]]*# ]] && continue
  [[ -z "${term//[[:space:]]/}" ]] && continue
  case "$term" in
    '!ignore-token '*) ignore_tokens+=("${term#!ignore-token }"); continue ;;
    '!'*) echo "leak-check: FAILED — unknown directive in $terms_file: $term" >&2; exit 1 ;;
  esac
  terms+=("$term")
done < "$terms_file"
if (( ${#terms[@]} == 0 )); then
  echo "leak-check: FAILED — terms file has zero usable terms: $terms_file" >&2
  echo "leak-check: an empty denylist finds nothing, which is not the same as finding no leak." >&2
  exit 1
fi
# An ignore token sed cannot apply (a `#`, the delimiter, or a bad regex) would
# empty every line it is tried on and hide the leak there: refuse it up front.
for tok in ${ignore_tokens[@]+"${ignore_tokens[@]}"}; do
  if ! printf 'x' | sed -E "s#${tok}#<ignored>#g" >/dev/null 2>&1; then
    echo "leak-check: FAILED — ignore token sed cannot apply in $terms_file (no '#', a valid ERE); the token is not printed" >&2
    exit 1
  fi
done
terms_alt="$(IFS='|'; printf '%s' "${terms[*]}")"
PATTERN="(${terms_alt}|${STRUCTURAL_PATTERN})"

if [[ "$mode" == "repo" ]]; then
  if ! repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
    echo "leak-check: FAILED — not inside a git work tree and no --files given: nothing to scan" >&2
    exit 1
  fi
  cd "$repo_root"
  while IFS= read -r -d '' path; do
    files+=("$path")
  done < <(git ls-files -z --cached --others --exclude-standard)
fi

# The terms file holds the private terms by design and is never scanned.
terms_abs="$(cd "$(dirname "$terms_file")" && pwd)/$(basename "$terms_file")"

scan=()
skipped=0
for f in ${files[@]+"${files[@]}"}; do
  if [[ ! -f "$f" ]]; then
    skipped=$((skipped + 1))
    printf 'leak-check: NOT-SCANNED %s: not a regular file\n' "$f" >&2
    continue
  fi
  f_abs="$(cd "$(dirname "$f")" && pwd)/$(basename "$f")"
  [[ "$f_abs" == "$terms_abs" ]] && continue
  scan+=("$f")
done

if (( ${#scan[@]} == 0 )); then
  echo "leak-check: FAILED — nothing to scan (${#files[@]} candidate(s), $skipped not a regular file): refusing to report clean" >&2
  exit 1
fi

# line_is_real_hit LINE: true when the line still matches after the benign
# emails and the configured ignore tokens are removed. Ignore tokens are
# lowercase regexes, so they apply to the lowercased line.
line_is_real_hit() {
  local line tok
  line="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E "s#${BENIGN_EMAIL}#<benign>#g; s#${BENIGN_PATH}#<benign>/#g")"
  for tok in ${ignore_tokens[@]+"${ignore_tokens[@]}"}; do
    line="$(printf '%s' "$line" | sed -E "s#${tok}#<ignored>#g")"
  done
  printf '%s' "$line" | grep -qiE "$PATTERN"
}

hits="$(mktemp)"
trap 'rm -f "$hits"' EXIT

matches="$(grep -HniIE "$PATTERN" -- "${scan[@]}")" && rc=0 || rc=$?
if (( rc >= 2 )); then
  echo "leak-check: FAILED — grep could not read part of the scan set (rc=$rc); refusing to report clean" >&2
  exit 1
fi
if [[ -n "$matches" ]]; then
  while IFS= read -r match; do
    # path:line:content — the path never holds a colon in this repo.
    path="${match%%:*}"
    rest="${match#*:}"
    lnum="${rest%%:*}"
    content="${rest#*:}"
    if line_is_real_hit "$content"; then
      printf '%s:%s\n' "$path" "$lnum" >> "$hits"
    fi
  done <<< "$matches"
fi

n="$(wc -l < "$hits" | tr -d ' ')"
if (( n > 0 )); then
  cat "$hits"
  echo "leak-check: FAILED — ${n} hit(s)" >&2
  exit 1
fi

echo "leak-check: clean: ${#scan[@]} files, ${#terms[@]} terms"
