#!/usr/bin/env bash
# QA-MCP-01 (PRD-001 section 16.2; p1-mcp-first-party task 10.1): run every test the red-team rows of compliance/qa-mcp-01.json name, in the
# local build (the pinned Hydra runs in Docker through Testcontainers), and fail on any. internal/archtest keeps each row's tests real.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .data/qa-mcp
names=$(python3 - <<'PY'
import json
rows = json.load(open("compliance/qa-mcp-01.json"))["rows"]
print("^(" + "|".join(sorted({t for r in rows for t in r["tests"]})) + ")$")
PY
)
echo "QA-MCP-01: $(python3 -c 'import json;print(len(json.load(open("compliance/qa-mcp-01.json"))["rows"]))') rows"
if ! go test -tags local -count=1 -timeout 30m ./... -run "$names" 2>&1 | grep -v '^{"time"' | tee .data/qa-mcp/report.txt | grep -v "no test files\|no tests to run" | tail -40; then
  :
fi
if grep -q '^--- FAIL\|^FAIL' .data/qa-mcp/report.txt; then
  echo "x QA-MCP-01: a red-team case failed (report in .data/qa-mcp/report.txt)" >&2
  exit 1
fi
echo "ok QA-MCP-01: every red-team case passed (report in .data/qa-mcp/report.txt)"
