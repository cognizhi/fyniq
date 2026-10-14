#!/usr/bin/env bash
# `make dev`: run the Go services and the web app on the host with hot reload (NFR-DEVX-03).
# The dependencies run in Docker (`make deps-up`). Each Go service is rebuilt and restarted on a
# change to a .go file through watchexec; Vite applies web changes by hot module reload. Logs go
# to .data/dev/. Without watchexec (mise installs it) the services run without reload.
set -euo pipefail
set -m   # each background job gets its own process group, so stopping it stops its children
cd "$(dirname "$0")/.."
[ -f .env ] && set -a && . ./.env && set +a
export FYNIQ_ENV="${FYNIQ_ENV:-local}"
mkdir -p .data/dev/bin

# One host port per service; Vite proxies /api to the api.
declare -A PORTS=([api]="${FYNIQ_API_PORT:-8081}" [ai]=8082 [mcp]=8083 [worker]=8084 [executor]=8085 [render]=8086)
pids=()
cleanup() {
  trap - INT TERM EXIT
  for pid in "${pids[@]}"; do kill -TERM -- "-$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

if command -v watchexec >/dev/null 2>&1; then
  reload=on
else
  reload=off
  echo "⚠ watchexec is not installed (mise installs it): Go services will not restart on changes" >&2
fi

for svc in "${!PORTS[@]}"; do
  # Build, then exec the binary, so a stop signal reaches the service itself.
  run="go build -tags local -o .data/dev/bin/$svc ./cmd/$svc && exec .data/dev/bin/$svc"
  if [ "$reload" = on ]; then
    PORT="${PORTS[$svc]}" watchexec --restart --quiet --exts go --watch cmd --watch internal \
      -- sh -c "$run" >>".data/dev/$svc.log" 2>&1 &
  else
    PORT="${PORTS[$svc]}" sh -c "$run" >>".data/dev/$svc.log" 2>&1 &
  fi
  pids+=($!)
done

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
