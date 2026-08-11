#!/usr/bin/env bash
# kg benchmark collector — thin wrapper around lib/bench.py.
#
# Usage:
#   ./run.sh [ROOT] [options...]
#
# All options are forwarded to bench.py (see `lib/bench.py --help`).
set -euo pipefail

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ROOT="${1:-$(pwd)}"
[[ $# -gt 1 ]] && shift

exec python3 "$BENCH_DIR/lib/bench.py" "$ROOT" "$@"
