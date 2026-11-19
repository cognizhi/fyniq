#!/usr/bin/env bash
# NM-ARC-01 (p1-nemura-platform 7.3): Nemura's browser journeys against a stack of its own: only the `people` profile in a compose project of its
# own (so no Fyniq service runs and no other stack is touched), the one-time setup code from `make people-setup-code`, then Playwright in the
# installed Chrome (apps/nemura-web/journeys/). Needs the local images (`make images`).
# Usage: scripts/test-journeys-people.sh        KEEP=1 leaves the stack running
set -euo pipefail
cd "$(dirname "$0")/.."
origin="http://localhost:${NEMURA_HTTP_PORT:-8180}"
compose=(docker compose -p "${NEMURA_JOURNEYS_PROJECT:-nemura-journeys}" -f deploy/local/compose.yaml)
cleanup() { [ "${KEEP:-0}" = 1 ] || "${compose[@]}" --profile people down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT
"${compose[@]}" --profile people down -v >/dev/null 2>&1 || true
"${compose[@]}" --profile people up -d --wait nemura-edge >/dev/null
code=$(make --no-print-directory people-setup-code 2>/dev/null | tail -1)
[ -n "$code" ] || { echo "✖ make people-setup-code printed no code" >&2; exit 1; }
NEMURA_ORIGIN="$origin" NEMURA_SETUP_CODE="$code" pnpm --filter @fyniq/nemura-web test:journeys
