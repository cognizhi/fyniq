#!/usr/bin/env bash
# `make dev`: run the Go services and the web app on the host with hot reload (NFR-DEVX-03).
# The dependencies run in Docker (`make deps-up`). One watchexec rebuilds every Go service in a
# single `go build` (shared packages compile once) and restarts them on a change to a .go file;
# Vite applies web changes by hot module reload. Logs go to .data/dev/. Without watchexec (mise
# installs it) the services run without reload.
#   scripts/dev.sh             start the stack
#   scripts/dev.sh --services  (used by watchexec) start the built services and wait
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f .env ] && set -a && . ./.env && set +a
export FYNIQ_ENV="${FYNIQ_ENV:-local}"
# The browser is on Vite's origin, which proxies /api: sign-in sends it back there.
export FYNIQ_PUBLIC_ORIGIN="${FYNIQ_PUBLIC_ORIGIN:-http://localhost:${FYNIQ_WEB_PORT:-5173}}"
mkdir -p .data/dev/bin

SERVICES=(api ai mcp worker executor render)
declare -A PORTS=([api]="${FYNIQ_API_PORT:-8081}" [ai]=8082 [mcp]=8083 [worker]=8084 [executor]=8085 [render]=8086)

if [ "${1:-}" = --services ]; then
  trap 'kill $(jobs -p) 2>/dev/null || true; wait' TERM INT EXIT
  for svc in "${SERVICES[@]}"; do
    PORT="${PORTS[$svc]}" ".data/dev/bin/$svc" >>".data/dev/$svc.log" 2>&1 &
  done
  wait
  exit 0
fi

set -m   # each background job gets its own process group, so stopping it stops its children
pids=()
cleanup() {
  trap - INT TERM EXIT
  for pid in "${pids[@]}"; do kill -TERM -- "-$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

# No VCS stamp (nothing reads it; the release build has none either) and no DWARF: a change relinks every
# service, and these two make that about a third faster (NFR-DEVX-03). Stack traces are unaffected; to attach a
# debugger, run the service with `dlv debug ./cmd/<service>`.
build="go build -tags local -buildvcs=false -ldflags=-w -o .data/dev/bin/ $(printf './cmd/%s ' "${SERVICES[@]}")"
if command -v watchexec >/dev/null 2>&1; then
  reload=on
  # watchexec runs the command string through sh itself and restarts it on every change.
  watchexec --restart --quiet --exts go --watch cmd --watch internal \
    -- "$build && exec scripts/dev.sh --services" >>.data/dev/build.log 2>&1 &
else
  reload=off
  echo "⚠ watchexec is not installed (mise installs it): Go services will not restart on changes" >&2
  sh -c "$build && exec scripts/dev.sh --services" >>.data/dev/build.log 2>&1 &
fi
pids+=($!)

FYNIQ_API_PORT="${PORTS[api]}" pnpm --filter @fyniq/web dev >>.data/dev/web.log 2>&1 &
pids+=($!)

cat <<MSG
Fyniq dev stack (FYNIQ_ENV=$FYNIQ_ENV, hot reload: $reload)
  Web        http://localhost:${FYNIQ_WEB_PORT:-5173}
  API        http://localhost:${PORTS[api]}/version
  Logs       .data/dev/*.log
  Sample     .data/seed/company.json
Press Ctrl-C to stop the services (the dependencies keep running: make deps-down).
MSG
wait -n "${pids[@]}"
