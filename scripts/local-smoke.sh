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

discovery=$(curl -fsS "$origin/oauth/.well-known/openid-configuration") || fail "the OAuth server does not answer at $origin/oauth/"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d["issuer"]==sys.argv[2]+"/oauth/", d["issuer"]; assert "S256" in d["code_challenge_methods_supported"], d' \
  "$discovery" "$origin" || fail "the OAuth server's discovery is wrong: $discovery"

# First-run setup (FR-ADM-06) through the one origin: a setup code from the deployment step, then the
# setup as a person signed in with the local issuer's dev token. A database kept from an earlier run is
# set up already, and says so.
if [ "$(curl -fsS "$origin/api/v1/setup")" = '{"done":false}' ]; then
  code=$(make --no-print-directory setup-code 2>/dev/null | tail -1)
  [ -n "$code" ] || fail "make setup-code printed no code"
  python3 - "$origin" "$code" <<'PY' || fail "first-run setup did not complete"
import base64, hashlib, hmac, json, sys, time, urllib.request
origin, code = sys.argv[1], sys.argv[2]
claims = {"sub": "smoke-founder", "client": "web", "class": "user", "org": "00000000-0000-0000-0000-000000000000", "roles": [], "exp": int(time.time()) + 600}
msg = "fyniq-dev." + base64.urlsafe_b64encode(json.dumps(claims).encode()).rstrip(b"=").decode()
token = msg + "." + base64.urlsafe_b64encode(hmac.new(b"fyniq-local-only-dev-token-key", msg.encode(), hashlib.sha256).digest()).rstrip(b"=").decode()
body = json.dumps({"setup_code": code, "display_name": "Smoke Founder", "organisation": {"name": "Smoke Pte Ltd", "base_currency": "SGD"}}).encode()
req = urllib.request.Request(origin + "/api/v1/setup", data=body, method="POST", headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=10) as r:
    assert r.status == 201, r.status
    out = json.load(r)
    assert out["organisation_id"] and len(out["recovery_codes"]) == 10  # shown once, never printed here
PY
  [ "$(curl -fsS "$origin/api/v1/setup")" = '{"done":true}' ] || fail "setup completed but the installation does not say so"
else
  echo "  (the database was set up in an earlier run: setup not repeated)"
fi

headers=$(curl -sS -o /dev/null -D - "$origin/api/v1/organisation")
grep -q "^HTTP/1.1 401" <<<"$headers" || fail "an unauthenticated call was not refused with 401: $headers"
grep -qi "^content-type: application/problem+json" <<<"$headers" || fail "the refusal is not problem details: $headers"

# SEC-15 across services: synthetic NRIC/FIN numbers, a bank account number and bearer tokens go to the
# stack in headers, the query and bodies, including a refusal the API logs with the caller's subject;
# then no service's log may hold any of them in full. The dev token is minted with the local issuer's
# key, which is valid nowhere else.
# req: SEC-15
python3 - "$origin" <<'PY' || fail "a log holds a secret, a token, an NRIC or a bank number (SEC-15)"
import base64, hashlib, hmac, json, subprocess, sys, time, urllib.request, uuid
origin = sys.argv[1]
nric, fin, bank, secret = "S1234567D", "F7654321N", "123-456789-001", "sk-live_local9f8e7d6c5b4a"
claims = {"sub": f"user-{nric}", "client": "web", "class": "user", "org": str(uuid.uuid4()), "roles": ["employee"], "exp": int(time.time()) + 600}
msg = "fyniq-dev." + base64.urlsafe_b64encode(json.dumps(claims).encode()).rstrip(b"=").decode()
token = msg + "." + base64.urlsafe_b64encode(hmac.new(b"fyniq-local-only-dev-token-key", msg.encode(), hashlib.sha256).digest()).rstrip(b"=").decode()
body = json.dumps({"memo": f"refund {nric} to {bank} with {secret}", "lines": []}).encode()
for auth in (f"Bearer {token}", f"Bearer {secret}", f"Bearer x.{nric}.y"):
    req = urllib.request.Request(f"{origin}/api/v1/journals?note={fin}&account={bank}", data=body, method="POST",
                                 headers={"Authorization": auth, "Content-Type": "application/json", "X-Customer": nric})
    try:
        urllib.request.urlopen(req, timeout=10)
    except urllib.error.HTTPError as e:
        assert e.code in (401, 403), e.code
logs = subprocess.run(["docker", "compose", "-f", "deploy/local/compose.yaml", "--profile", "core", "logs", "--no-color"],
                      capture_output=True, text=True, check=True).stdout
assert "api.refused" in logs, "the refusal was not logged, so the masking was not exercised"
leaks = [v for v in (nric, fin, bank, secret, token) if v in logs]
if leaks:
    print("leaked:", [v[:6] + "…" for v in leaks], file=sys.stderr)
    sys.exit(1)
PY

unhealthy=$(docker compose -f deploy/local/compose.yaml --profile core ps --format '{{.Service}} {{.State}} {{.Health}}' \
  | awk '$1 != "migrate" && ($2 != "running" || ($3 != "" && $3 != "healthy"))')
[ -z "$unhealthy" ] || fail "services not running and healthy: $unhealthy"
migrated=$(docker compose -f deploy/local/compose.yaml --profile core ps -a --format '{{.Service}} {{.ExitCode}}' | awk '$1 == "migrate" {print $2}')
[ "$migrated" = 0 ] || fail "the migrate job exited with ${migrated:-nothing}"

echo "✔ QA-LOCAL-01: the stack runs from the local images with no cloud credentials; web, API, document and refusals answer through $origin, and no log holds a secret, token, NRIC or bank number (SEC-15)"
