#!/usr/bin/env bash
# NM-ARC-06, NM-ARC-01 (p1-nemura-platform 5.3): Nemura on its own. Only the `people` profile runs, so every Fyniq service is stopped, and
# a person signs in and takes the first organisation, as a browser would, through Nemura's own origin (Caddyfile.people): the web app, the API's
# version and document, its own OAuth server's discovery, first-run setup with the deployment's one-time code through the sign-in form and the
# local sign-in page, the session that names them the Owner, sign-out, a wrong and a used code refused, and no log holding a code or a token.
#
# Needs the local images (`make images`). Usage: scripts/local-smoke-people.sh        # up, check, down
#                                                KEEP=1 scripts/local-smoke-people.sh  # leave the stack running
set -euo pipefail
cd "$(dirname "$0")/.."
origin="http://localhost:${NEMURA_HTTP_PORT:-8180}"
# A project of its own, so the script never starts, stops or deletes the volumes of a stack someone is running (the default project, `make up`).
compose=(docker compose -p "${NEMURA_SMOKE_PROJECT:-nemura-smoke}" -f deploy/local/compose.yaml)
fail() { echo "✖ NM-ARC-06: $*" >&2; "${compose[@]}" --profile people ps -a >&2; exit 1; }
cleanup() { [ "${KEEP:-0}" = 1 ] || "${compose[@]}" --profile people down -v >/dev/null 2>&1 || true; }
jar=$(mktemp)
trap 'rm -f "$jar"; cleanup' EXIT

for v in GOOGLE_APPLICATION_CREDENTIALS CLOUDSDK_CONFIG GOOGLE_CLOUD_PROJECT; do
  [ -z "${!v:-}" ] || fail "$v is set: local mode must run without cloud credentials (DEV-02)"
done

"${compose[@]}" --profile people down -v >/dev/null 2>&1 || true
"${compose[@]}" --profile people up -d --wait nemura-edge >/dev/null || fail "the people profile did not come up"

# Standalone: nothing of Fyniq runs in this project (the database server is shared infrastructure, not a Fyniq service).
running=$("${compose[@]}" --profile core --profile people ps --services --status running | grep -v -e '^nemura' -e '^postgres$' || true)
[ -z "$running" ] || fail "Fyniq services are running beside Nemura: $running"

body=$(curl -fsS "$origin/") || fail "the web app does not answer at $origin/"
grep -q '<div id="root">' <<<"$body" || fail "the web app's page has no root element"
grep -q 'nemura-web-placeholder' <<<"$body" && fail "the image serves the placeholder, not the web app"
headers=$(curl -fsS -o /dev/null -D - "$origin/sign-in")
grep -qi "^content-security-policy: default-src 'self'" <<<"$headers" || fail "the web app is served without its strict policy: $headers"

version=$(curl -fsS "$origin/api/version") || fail "the API does not answer at $origin/api/version"
python3 -c 'import json,sys; v=json.loads(sys.argv[1]); assert v["flavour"]=="local", v' "$version" || fail "the API is not the local flavour: $version"
printf '%s' "$(curl -fsS "$origin/api/v1/openapi.json")" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "/v1/sign-in" in d["paths"] and "/v1/api-clients" in d["paths"], list(d["paths"])' \
  || fail "the API document lacks Nemura's operations"
discovery=$(curl -fsS "$origin/oauth/.well-known/openid-configuration") || fail "Nemura's OAuth server does not answer at $origin/oauth/"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d["issuer"]==sys.argv[2]+"/oauth/", d["issuer"]' "$discovery" "$origin" \
  || fail "the OAuth server's discovery is wrong: $discovery"

info=$(curl -fsS "$origin/api/v1/sign-in")
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d["setup_done"] is False and [p["id"] for p in d["providers"]]==["local"], d' "$info" \
  || fail "before setup the sign-in page's information is wrong: $info"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$origin/api/v1/session")" = 401 ] || fail "a call with no session was not refused with 401"

# First-run setup (NM-ARC-01), as the web app's form does it: the code goes to the API in a POST, the API sends the browser to the provider,
# the local sign-in page signs "smoke-founder" in, and the callback makes the first organisation with them as its Owner.
code=$(make --no-print-directory people-setup-code 2>/dev/null | tail -1)
[ -n "$code" ] || fail "make people-setup-code printed no code"
sign_in() { # sign_in <form-data to /auth/join> <subject>: where the browser lands
  local page
  page=$(curl -fsS -c "$jar" -b "$jar" -o /dev/null -L -w '%{url_effective}' --data "$1" "$origin/api/auth/join") || fail "the join form did not answer"
  case "$page" in *"/auth/dev/authorize?"*) ;; *) fail "joining did not reach the local sign-in page: $page" ;; esac
  case "$page" in *"$code"*) fail "the setup code is in a URL: $page" ;; esac
  curl -fsS -c "$jar" -b "$jar" -o /dev/null -L -w '%{url_effective}' --data "${page#*\?}&subject=$2&name=Smoke+Founder&decision=sign-in&second_factor=yes" "${page%%\?*}"
}
wrong=$(sign_in "provider=local&kind=setup&code=not-the-code&organisation=Smoke+Pte+Ltd&name=Smoke+Founder&return_to=/people" smoke-founder)
[ "$wrong" = "$origin/sign-in?signin_error=NM_SIGNIN_08" ] || fail "a wrong setup code did not end at the sign-in page with NM_SIGNIN_08: $wrong"
[ "$(curl -sS -b "$jar" -o /dev/null -w '%{http_code}' "$origin/api/v1/session")" = 401 ] || fail "a wrong setup code gave a session"
landed=$(sign_in "provider=local&kind=setup&code=$code&organisation=Smoke+Pte+Ltd&name=Smoke+Founder&return_to=/people" smoke-founder)
[ "$landed" = "$origin/people" ] || fail "after setup the browser landed on $landed"
session=$(awk '$6 == "nemura_session" {print $7}' "$jar")
[ -n "$session" ] || fail "setup set no session cookie"
whoami=$(curl -fsS -b "$jar" "$origin/api/v1/session") || fail "the session does not answer"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d["roles"]==["owner"] and d["landing"]=="/people", d' "$whoami" || fail "the session is not the Owner's: $whoami"
[ "$(curl -fsS "$origin/api/v1/sign-in" | python3 -c 'import json,sys; print(json.load(sys.stdin)["setup_done"])')" = True ] || fail "setup completed but the installation does not say so"
used=$(sign_in "provider=local&kind=setup&code=$code&organisation=Other+Pte+Ltd&name=Eve&return_to=/" eve)
[ "$used" = "$origin/sign-in?signin_error=NM_SIGNIN_08" ] || fail "a used setup code was not refused: $used"

# Sign-out ends the session: the cookie is no longer honoured, even when sent again.
curl -fsS -b "$jar" -o /dev/null -X POST -H 'Sec-Fetch-Site: same-origin' "$origin/api/auth/signout" || fail "sign-out did not answer"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Cookie: nemura_session=$session" "$origin/api/v1/session")" = 401 ] || fail "the session works after sign-out"

# SEC-15: no service's log holds the setup code or the session token.
logs=$("${compose[@]}" --profile people logs --no-color 2>&1)
for v in "$code" "$session"; do
  ! grep -qF -- "$v" <<<"$logs" || fail "a log holds the setup code or a session token (SEC-15)"
done
unhealthy=$("${compose[@]}" --profile people ps --format '{{.Service}} {{.State}} {{.Health}}' | awk '$1 !~ /migrate$/ && ($2 != "running" || ($3 != "" && $3 != "healthy"))')
[ -z "$unhealthy" ] || fail "services not running and healthy: $unhealthy"

echo "✔ NM-ARC-06: Nemura runs on its own with every Fyniq service stopped; its web app, API, OAuth server and first-run setup answer through $origin, a person signs in and becomes the Owner, sign-out ends the session, and no log holds a code or a token"
