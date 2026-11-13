#!/usr/bin/env bash
# `make evals`: run every AI eval suite under evals/ with cmd/evals (PRD-001 §16.2, AI-78, CTL-AI-05). Replay mode by
# default: each model's recorded answers in evals/cassettes, offline and with no credential (DEV-02). FYNIQ_AI_LIVE=1
# with --model runs one model live on the suites' synthetic text and records its answers. Report: .data/evals/report.json.
set -euo pipefail
cd "$(dirname "$0")/.."
mise=""
command -v mise >/dev/null 2>&1 && mise="mise exec --"
exec $mise go run -tags local ./cmd/evals "$@"
