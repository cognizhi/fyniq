#!/usr/bin/env bash
# `make evals`: run every AI eval suite under evals/ with cmd/evals (PRD-001 §16.2, AI-78, CTL-AI-05). Replay mode by
# default: each model's recorded answers in evals/cassettes, offline and with no credential (DEV-02). FYNIQ_AI_LIVE=1
# with --model runs one model live on the suites' synthetic text and records its answers. Report: .data/evals/report.json,
# with the assistant's suites in it.
set -euo pipefail
cd "$(dirname "$0")/.."
mise=""
command -v mise >/dev/null 2>&1 && mise="mise exec --"
# First the assistant's own suites (evals/<suite>/assistant.json, QA-AI-01 and QA-AI-03), through the API and the real
# AI service on the synthetic sample company, in PostgreSQL by Testcontainers, so Docker must run. Their report goes
# beside cmd/evals's; FYNIQ_EVALS_RELEASE=1 fails a case with no recording (the release gate).
# Then the scheduled agents' suites (evals/<suite>/agent.json, QA-AI-07): each case a run of the agent on its business
# date, through the worker, the API's model step and the AI service.
# Then the document capture's suite (evals/<suite>/capture.json, QA-AI-02, AI-18): synthetic documents captured and read through the inbox's
# reading path, each reading judged field by field.
# Then the knowledge suite (evals/knowledge/knowledge.json, QA-AI-06's retrieval half): the release's own knowledge base asked
# 100 or more golden questions through the tools, recall per category, citations and the revoked reader.
mkdir -p .data/evals
rm -f .data/evals/assistant.json .data/evals/agent.json .data/evals/capture.json .data/evals/knowledge.json
FYNIQ_EVALS_OUT="$PWD/.data/evals" $mise go test -tags local ./internal/api/ -run '^(TestTheAssistantSuites|TestTheAgentSuites|TestTheCaptureSuites|TestTheKnowledgeSuite.*)$' -count=1
exec $mise go run -tags local ./cmd/evals "$@"
