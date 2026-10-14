#!/usr/bin/env bash
# `make evals`: run every AI eval suite under evals/ (PRD-001 §16.2). Replay mode by default;
# FYNIQ_AI_LIVE=1 for live runs on synthetic data. The suites arrive in P1.
set -euo pipefail
cd "$(dirname "$0")/.."
mode=replay
[ "${FYNIQ_AI_LIVE:-0}" = 1 ] && mode=live
suites=$(find evals -mindepth 1 -maxdepth 1 -type d | sort)
if [ -z "$suites" ]; then
  echo "✔ evals ($mode): no suites yet; the golden, proposal and red-team suites arrive in P1"
  exit 0
fi
echo "✖ evals: suites exist under evals/ but no runner is wired yet; add it with the first suite" >&2
exit 1
