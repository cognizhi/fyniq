## 1. The platform knowledge base at runtime

Each slice is its own pull request; each passes `make check` and its own tests. Migration numbers are in design.md.

- [ ] 1.1 The `kb` package (embeds `kb/pages`, `kb/index.md`, `kb/schema.md`) and the guide's raw sources; a Go reader of
  pages (front matter, sections, headings' anchors) and a Go lint of the same rules as `scripts/kb.py`, with the shared
  fixture set `test/kb/fixtures/` that both run (design D1). Tests `// req: AI-52`, `// req: AI-53`.
- [ ] 1.2 Migration 000560: `kb_platform_release`, `kb_platform_page`, `kb_platform_source`, the read views, the reader filter
  function and the full-text and trigram indexes; the storage port and adapter; `cmd/migrate up` loads the release's set
  as the migrator; the migrate image's `.dockerignore` and Dockerfile lines. Tests `// req: AI-50`, `// req: AI-56`, an
  attack test that the app roles cannot write the global tables.
- [ ] 1.3 The platform index generator in Go, equal to `scripts/kb.py index` on the fixtures, and the 8,000-token split
  into sections (design D3). Tests `// req: AI-58`.

## 2. The tools

- [ ] 2.1 The reader filter (design D2): audience from the guide pages' `roles`, data class to the reader's rights, in the
  query; the Go and TypeScript parsers of the guide's front matter agree on every page. Tests `// req: AI-54`,
  an `// asvs:` tag.
- [ ] 2.2 `kb.index`, `kb.read`, `kb.search` in `contracts/ai/tools/` and `tools.lock.json`; the internal operations through
  `callAITool`; full-text search with `pg_trgm` over pages, aliases and raw guide sources; page versions returned;
  `toolClasses` and AI-83; matrix rows. Tests `// req: AI-49`, `// req: AI-51`, `// req: AI-58`, `// req: AI-83`.
- [ ] 2.3 The layer-1 index operation for the orchestrator, filtered per reader and cached in the prefix. Tests
  `// req: AI-48`.
- [ ] 2.4 Read how recordings are matched in `internal/assistant/evalsuite` and `evals/cassettes` before adding tools or
  bumping the prompt; record the finding in design.md.
- [ ] 2.5 Migration 000561/000562: `ai_tool_call.kb_pages`; the activity log records the page versions each answer used and
  shows them. Tests `// req: AI-12`, `// req: AI-51`.

## 3. Answers

- [ ] 3.1 The `know_how` segment, the number check against the cited section, the citation chip resolved by platform code to
  a guide page, a setting or a record, never a compiled page; `chat@4` (answering order, draft and out-of-date wording).
  Tests `// req: AI-51`, `// req: AI-53`, `// req: AI-08`.
- [ ] 3.2 The panel renders the know-how chip (violet AI content only, no compiled-page link); Vitest with axe. Tests
  `// req: AI-22`.
- [ ] 3.3 Statutory pages: the pack in force newer than the page's makes the tool result say so, and the answer is from
  `packs.get` (G4-091). Tests `// req: AI-53`.

## 4. The organisation's knowledge base

- [ ] 4.1 Migration 000563: `kb_note`, `kb_page`, `kb_page_version`, `kb_snapshot`, `kb_snapshot_page`, `kb_lint_finding`
  (append-only, guards, FORCE RLS, composite keys, the export catalogue, goldens). Tests `// req: AI-56`.
- [ ] 4.2 The organisation's search and index over its newest snapshot, merged with the platform's; overlays win for that
  organisation. Tests `// req: AI-58`, `// req: AI-56`.
- [ ] 4.3 The notes API (create, edit, list; audited). Tests `// req: AI-50`, `// req: AUD-01`.

## 5. Settings-rendered pages

- [ ] 5.1 `kb.Render` from settings, approval policies, team mode and recommended-default decisions with their reasons, no
  model; written as `rendered` versions when a setting takes effect, per the owner's decision 3. Tests `// req: AI-50`,
  `// req: FR-ORG-08`.

## 6. The compiler

- [ ] 6.1 Migrations 000564/000565: the role `fyniq_kb_compiler`, its grants (read sources, insert drafts) and the drafts-only
  trigger; every list of roles (`ServiceRoles`, the catalogue and attack tests, `wiring/local.go`, compose,
  `infra/modules/platform-spec`, `db/README.md`, goldens). Attack tests: it cannot publish, read the ledger, change a
  version or write anything but a draft. Tests `// req: AI-57`, `// req: CMP-AI-01`, an `// asvs:` tag.
- [ ] 6.2 The service-token class `kb_compiler`, the API's `compileKBPage` operation (agent `kb_compiler`'s route and budget,
  the AI switch, screening) and `contracts/ai/prompts/kb-compile@1.md`. Tests `// req: AI-57`, `// req: AI-74`.
- [ ] 6.3 `jobs kb-compile`: the changed sources, the Go lint with one retry, the drafts, `kb_compile_run`, the human-edit
  conflict finding; the weekly semantic lint as findings and Inbox tasks. Tests `// req: AI-52`, `// req: AI-56`.

## 7. Publishing

- [ ] 7.1 Migrations 000566/000567 and `kb_publication` as an approval object: the envelope, the policy default per team
  mode (open question 18), the checks before signing (lint, contradictions with settings, the pack and a newer platform
  page), snapshot n+1 in the executing transaction. Tests `// req: AI-56`, `// req: FR-APR-01`, `// req: FM-2`.
- [ ] 7.2 Edit, human-edited versions, rollback as a new version, overlays and their flag when the platform page changes;
  until a contradiction is resolved the assistant follows the setting or the pack and says so. Tests `// req: AI-56`.
- [ ] 7.3 The weekly review lists self-approved publications. Tests `// req: FM-3`.

## 8. Administration

- [ ] 8.1 `/v1/knowledge-base` operations (design D9) in the contract, the authorisation matrix, the never-list for MCP and
  agent clients; each audited. Tests `// req: AI-55`, `// req: AUD-01`.
- [ ] 8.2 The screen `knowledge-base` (Settings → AI): index, page, diff, versions, lint, recompile, publish; Basic and
  Expert; help keys; `docs/user-guide/knowledge-base.md` compiled into `kb/`; the fragment updated locally. Tests
  `// req: AI-55`, Vitest with axe.

## 9. Evals, controls and the finish

- [ ] 9.1 The `knowledge` suite kind and its ≥ 100 golden questions in the QA-AI-06 categories: deterministic recall per
  category, the revoked reader, citation resolution and the stale-fallback rate gated offline in `make evals`; answer
  accuracy recorded `[live-verify]`. Tests `// req: QA-AI-06` only for what each proves.
- [ ] 9.2 The publish gate (the deterministic part, per the owner's decision 2). Tests `// req: QA-AI-06`, `// req: AI-56`.
- [ ] 9.3 CTL-AI-08's nightly (`verify kb`, the reader-filter test) and event checks running. Tests `// req: CTL-AI-08`.
- [ ] 9.4 `docs/security/threat-models/knowledge-base.md` for security review #4; the build plan's paragraph; `make check`,
  `make test`, `make evals` pass.

## Workflow follow-up

- `/opsx:verify p1-knowledge-base`, then `/opsx:archive p1-knowledge-base --skip-specs`.
- `p1-mcp-first-party` then exposes `kb.*` over MCP from the same registry.
