#!/usr/bin/env bash
set -euo pipefail
python3 "$(dirname "$0")/check-tree_test.py"
python3 "$(dirname "$0")/check-tree_regression_test.py"
python3 "$(dirname "$0")/check-tree_audit_test.py"
python3 "$(dirname "$0")/build-release_test.py"
bash "$(dirname "$0")/leak-check_test.sh"
