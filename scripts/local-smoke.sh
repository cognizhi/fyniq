#!/usr/bin/env bash
# QA-LOCAL-01: local mode from the built images, with no cloud credentials. `make up` starts the
# stack from the local flavour of the images (same commit, same Dockerfile and hardening), then this
# checks it through Caddy's one origin, as a browser would: the web app, the API's version, its
# document, a call refused as problem details, and every service healthy. The sample company is
# written by `make seed`; loading it into the database arrives with the first-run setup
# (p0-identity-oauth-first-run). The golden and parity suites run in `make test` and in P1.
#
# Usage: scripts/local-smoke.sh            # make up, check, make down
#        KEEP=1 scripts/local-smoke.sh     # leave the stack running
set -euo pipefail
cd "$(dirname "$0")/.."
origin="http://localhost:${FYNIQ_HTTP_PORT:-8080}"
fail() { echo "✖ QA-LOCAL-01: $*" >&2; docker compose -f deploy/local/compose.yaml --profile core ps -a >&2; exit 1; }
cleanup() { [ "${KEEP:-0}" = 1 ] || make --no-print-directory down >/dev/null 2>&1 || true; }
trap cleanup EXIT

for v in GOOGLE_APPLICATION_CREDENTIALS CLOUDSDK_CONFIG GOOGLE_CLOUD_PROJECT; do
  [ -z "${!v:-}" ] || fail "$v is set: local mode must run without cloud credentials (DEV-02)"
done

make --no-print-directory up
make --no-print-directory seed >/dev/null

body=$(curl -fsS "$origin/") || fail "the web app does not answer at $origin/"
grep -q "<div id=\"root\">" <<<"$body" || fail "the web app's page has no root element"

version=$(curl -fsS "$origin/api/version") || fail "the API does not answer at $origin/api/version"
python3 -c 'import json,sys; v=json.loads(sys.argv[1]); assert v["flavour"]=="local", v' "$version" \
  || fail "the API is not the local flavour: $version"

doc=$(curl -fsS "$origin/api/v1/openapi.json") || fail "no API document at $origin/api/v1/openapi.json"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert "/v1/journals" in d["paths"], list(d["paths"])' "$doc" \
  || fail "the API document lacks the journals"

headers=$(curl -sS -o /dev/null -D - "$origin/api/v1/organisation")
grep -q "^HTTP/1.1 401" <<<"$headers" || fail "an unauthenticated call was not refused with 401: $headers"
grep -qi "^content-type: application/problem+json" <<<"$headers" || fail "the refusal is not problem details: $headers"

unhealthy=$(docker compose -f deploy/local/compose.yaml --profile core ps --format '{{.Service}} {{.State}} {{.Health}}' \
  | awk '$1 != "migrate" && ($2 != "running" || ($3 != "" && $3 != "healthy"))')
[ -z "$unhealthy" ] || fail "services not running and healthy: $unhealthy"
migrated=$(docker compose -f deploy/local/compose.yaml --profile core ps -a --format '{{.Service}} {{.ExitCode}}' | awk '$1 == "migrate" {print $2}')
[ "$migrated" = 0 ] || fail "the migrate job exited with ${migrated:-nothing}"

echo "✔ QA-LOCAL-01: the stack runs from the local images with no cloud credentials; web, API, document and refusals answer through $origin"
