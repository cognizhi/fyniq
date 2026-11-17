#!/usr/bin/env bash
# The visual snapshots (QA-UI-07, QA-RPT-04; p1-launch-readiness design D5): a stack of their own with the business date
# fixed, so every run draws the same days, then Playwright in its pinned Linux container hashes each page at the five
# frames in light and dark against apps/web/visual/baselines.json. A changed or missing hash fails the run; the pictures
# and every hash it saw are left in apps/web/test-results/visual/, and `python3 scripts/visual.py approve` takes the hashes
# into the baselines (a pull request labelled visual-baseline).
#
# Usage: make test-visual                  # the key pages (every pull request)
#        VISUAL_ALL=1 make test-visual     # every route of the registry (nightly and before a release)
#        KEEP=1 make test-visual           # leave the stack running
# The baselines are linux/amd64's, as CI draws them; elsewhere VISUAL_PLATFORM=linux/amd64 draws them under emulation.
set -euo pipefail
cd "$(dirname "$0")/.."

# A stack of its own, on ports of its own, beside any other running here.
export COMPOSE_PROJECT_NAME="${VISUAL_PROJECT:-fyniq-visual}"
export FYNIQ_BUSINESS_DATE="${VISUAL_DATE:-2026-12-31}"
export FYNIQ_HTTP_PORT="${VISUAL_HTTP_PORT:-8095}" FYNIQ_API_PORT=8096 FYNIQ_PG_PORT="${VISUAL_PG_PORT:-25433}" FYNIQ_SMTP_PORT=11026 \
  FYNIQ_MAIL_UI_PORT=18026 FYNIQ_GCS_PORT=14444 FYNIQ_CLAMD_PORT=13311 FYNIQ_PARTNER_PORT=14010 FYNIQ_PARTNER_WISE_PORT=14011 \
  FYNIQ_PARTNER_AIRWALLEX_PORT=14012
image="${PLAYWRIGHT_IMAGE:?run it with make test-visual, which names the pinned image}"
out=apps/web/test-results/visual
cleanup() { [ "${KEEP:-0}" = 1 ] || make --no-print-directory down >/dev/null 2>&1 || true; }
trap cleanup EXIT

# The stack, set up for its first run (FR-ADM-06), by the local smoke test.
KEEP=1 scripts/local-smoke.sh >/dev/null

# The organisation the pictures are of: the sample company, its Owner and Accountant, under a name that never changes.
mkdir -p .data/visual "$out"
go run -tags local ./cmd/seed --new-organisation "Visual Pte Ltd" --invite "Ann Owner:owner,accountant" > .data/visual/organisation.json

# Playwright in its pinned container, on the host's network to reach the stack; it reads the repository as it is.
platform=()
[ -z "${VISUAL_PLATFORM:-}" ] || platform=(--platform "$VISUAL_PLATFORM")
docker run --rm "${platform[@]}" --network host --ipc host --user "$(id -u):$(id -g)" -e HOME=/tmp -e CI \
  -e FYNIQ_HTTP_PORT -e VISUAL_ALL -e VISUAL_ORGANISATION=/work/.data/visual/organisation.json \
  -v "$PWD:/work" -w /work/apps/web "$image" npx --no-install playwright test -c visual/visual.config.ts
