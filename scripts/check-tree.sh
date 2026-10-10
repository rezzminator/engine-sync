#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
command -v python3 >/dev/null 2>&1 || { echo "CHECK enumeration ERROR python3 unavailable"; exit 2; }
exec python3 "$root/scripts/check-tree.py" "$@"
