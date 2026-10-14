#!/usr/bin/env bash
# Measure the local-mode budgets (PRD-001 §15): `make setup && make dev` on a clean machine within
# 10 minutes and later runs within 60 seconds (NFR-DEVX-01, DEV-01), and a Go change restarting its
# service within 3 seconds (NFR-DEVX-03). CI runs it on a fresh runner (the local-mode job);
# it writes the results to $GITHUB_STEP_SUMMARY when set and fails when a budget is missed.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .data

now() { date +%s.%N; }
since() { awk -v a="$1" -v b="$(now)" 'BEGIN { printf "%.1f", b - a }'; }
over() { awk -v x="$1" -v max="$2" 'BEGIN { exit !(x > max) }'; }
ready() { curl -fsS localhost:8081/readyz >/dev/null 2>&1 && curl -fsS localhost:5173/ >/dev/null 2>&1; }
wait_for() { # wait_for <seconds> <command…>
  local deadline=$(( $(date +%s) + $1 )); shift
  until "$@"; do sleep 0.2; [ "$(date +%s)" -lt "$deadline" ] || return 1; done
}
start_dev() { setsid make dev >>.data/devx-dev.log 2>&1 & echo $!; }
stop_dev() { kill -TERM -- "-$1" 2>/dev/null || true; wait_for 30 sh -c "! kill -0 $1 2>/dev/null" || true; }
restarts() { grep -c "listening on" .data/dev/api.log 2>/dev/null || echo 0; }

t=$(now)
make setup >.data/devx-setup.log 2>&1
pid=$(start_dev)
wait_for 600 ready || { echo "✖ the stack was not ready within 10 minutes; see .data/devx-*.log" >&2; stop_dev "$pid"; exit 1; }
first=$(since "$t")
stop_dev "$pid"

t=$(now)
pid=$(start_dev)
wait_for 120 ready || { echo "✖ the stack was not ready within 2 minutes on the second run" >&2; stop_dev "$pid"; exit 1; }
later=$(since "$t")

before=$(restarts)
t=$(now)
touch internal/version/version.go
wait_for 60 sh -c "[ \$(grep -c 'listening on' .data/dev/api.log) -gt $before ] && curl -fsS localhost:8081/healthz >/dev/null" \
  || { echo "✖ the api did not restart after a Go change; is watchexec installed?" >&2; stop_dev "$pid"; exit 1; }
reload=$(since "$t")
stop_dev "$pid"
make deps-down >/dev/null 2>&1 || true

report=$(cat <<MD
| Measure | Budget | Result |
|---|---|---|
| \`make setup && make dev\`, first run (NFR-DEVX-01, DEV-01) | ≤ 600 s | ${first} s |
| \`make dev\`, later run (NFR-DEVX-01) | ≤ 60 s | ${later} s |
| Go change to restarted api (NFR-DEVX-03) | ≤ 3 s | ${reload} s |
MD
)
echo "$report"
[ -n "${GITHUB_STEP_SUMMARY:-}" ] && printf '### Local-mode timings\n\n%s\n' "$report" >>"$GITHUB_STEP_SUMMARY"

fail=0
over "$first" 600 && { echo "✖ first run over budget" >&2; fail=1; }
over "$later" 60 && { echo "✖ later run over budget" >&2; fail=1; }
over "$reload" 3 && { echo "✖ Go reload over budget" >&2; fail=1; }
exit "$fail"
