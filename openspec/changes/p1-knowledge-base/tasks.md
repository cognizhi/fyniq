## 1. The platform knowledge base at runtime

Each slice is its own pull request; each passes `make check` and its own tests. Migration numbers are in design.md.

No test is tagged `// req:` with a requirement it proves only in part: AI-49 to AI-58 and QA-AI-06 are each half organisation, half
platform, or half model. Their tags are added when the rest is built, so the trace gate shows them untested until then.

- [x] 1.1 The `kb` package (embeds `kb/pages`, `kb/index.md`, `kb/schema.md`) and the guide's raw sources; a Go reader of
  pages (front matter, sections, headings' anchors) and a Go lint of the same rules as `scripts/kb.py`, with the shared
  fixture set `test/kb/fixtures/` that both run (design D1). Tests `// req: AI-52`, `// req: AI-53`.
  Built: `internal/domain/knowledge` (page, front matter, anchors, lint with 29 rule codes, index generator, search-term words) and the embeds `kb/embed.go` and `docs/user-guide/guide.go`. The Go lint and `scripts/kb.py` agree on 27 fixtures in `test/kb/fixtures/` (`scripts/test_kb_fixtures.py` and `TestTheGoLintAgreesWithKbPyOnTheFixtures`), and on the real corpus (the shipped index byte for byte). `kb.py` takes `KB_ROOT` for the fixtures. The rule of PRD-001 rev 0.8, a number as a whole token of the section its paragraph cites, is in neither lint yet: both keep the substring rule, and the runtime compiler's lint (task 6.3) adds it to both.
- [x] 1.2 Migration 000560: `kb_platform_release`, `kb_platform_page`, `kb_platform_source`, the read views, the reader filter
  function and the full-text and trigram indexes; the storage port and adapter; `cmd/migrate up` loads the release's set
  as the migrator; the migrate image's `.dockerignore` and Dockerfile lines. Tests `// req: AI-50`, `// req: AI-56`, an
  attack test that the app roles cannot write the global tables.
  Built: migration 000560 (four global tables, the release's `kb_platform_load`, six reader-filter functions, full-text and trigram indexes, no vector column); `postgres.LoadPlatformKB` and `cmd/migrate/kb.go` (only the migrate binary embeds the pages; the Dockerfile and `.dockerignore` lines are in); the storage port `KnowledgeBase()`. Attack test: no application role reads or writes the tables or runs the load, and only the API runs the reads.
- [ ] 1.3 The platform index generator in Go, equal to `scripts/kb.py index` on the fixtures, and the 8,000-token split
  into sections (design D3). Tests `// req: AI-58`.
  **Built in part:** the generator (`RenderIndex`, `Entries`) is byte-equal to `kb.py`. **Left:** the 8,000-token split of layer 1's index into sections waits for task 2.3; `kb.index` already lists sections when its result passes the tool's 4,000-token cap.

## 2. The tools

- [x] 2.1 The reader filter (design D2): audience from the guide pages' `roles`, data class to the reader's rights, in the
  query; the Go and TypeScript parsers of the guide's front matter agree on every page. Tests `// req: AI-54`,
  an `// asvs:` tag.
  Built: the filter is in migration 000560's functions; the API's `kbReader` works the reader's data classes out from their live rights (never identity; payroll with AI payroll access and a payroll right; equity, personal by rows of Appendix D); the audience (the guide pages' `roles`) is parsed in Go and in `apps/web/src/help/guide.ts`, equal on every page by `test/kb/guide-audience.json`.
- [ ] 2.2 `kb.index`, `kb.read`, `kb.search` in `contracts/ai/tools/` and `tools.lock.json`; the internal operations through
  `callAITool`; full-text search with `pg_trgm` over pages, aliases and raw guide sources; page versions returned;
  `toolClasses` and AI-83; matrix rows. Tests `// req: AI-49`, `// req: AI-51`, `// req: AI-58`, `// req: AI-83`.
  **Built in part (platform pages):** the three tools, version 1, pinned in `tools.lock.json`; `callAITool` runs them as the person through `runKBTool` (no new operation, so the authorisation matrix has no new row); `toolClasses` adds know-how (AI-83); page versions returned; sections and outline for a long page. **Left:** the organisation's pages and notes join the search and the index with task 4.2.
- [ ] 2.3 The layer-1 index operation for the orchestrator, filtered per reader and cached in the prefix. Tests
  `// req: AI-48`.
  **Left:** the model reads the index with `kb_index` meanwhile (chat@4). Layer 1's cached, reader-filtered index needs an internal operation, a backend method on the AI service and the prompt's cached prefix; the same filter applies, so the cost is tokens, not safety.
- [x] 2.4 Read how recordings are matched in `internal/assistant/evalsuite` and `evals/cassettes` before adding tools or
  bumping the prompt; record the finding in design.md.
  Built: no cassette is recorded yet (`evals/cassettes/assistant` holds only its README, and the secretary-launch suite none), so adding three tools and bumping the prompt to chat@4 replays nothing that exists; a recording made later is made under chat@4 `[live-verify]`. `TestTheReleasesSkillsLoad` now expects the company secretary's skill to be offered the kb tools it already names.
- [x] 2.5 Migration 000561/000562: `ai_tool_call.kb_pages`; the activity log records the page versions each answer used and
  shows them. Tests `// req: AI-12`, `// req: AI-51`.
  Built: migration 000561 (`ai_tool_call.kb_pages`, only the API inserts it) validated in 000562; the AI service keeps the versions on the turn's tool call; the AI activity log lists them in the contract and on the screen; the guide's AI activity page and its compiled page say so.

## 3. Answers

- [x] 3.1 The `know_how` segment, the number check against the cited section, the citation chip resolved by platform code to
  a guide page, a setting or a record, never a compiled page; `chat@4` (answering order, draft and out-of-date wording).
  Tests `// req: AI-51`, `// req: AI-53`, `// req: AI-08`.
  Built: the `know_how` segment (`page`, `section`; the model's words), the checks (the page was read this turn, the section was returned, every number a whole token of the section, one regeneration then withheld as "this part of the guide"), citations resolved by Fyniq from the section's own `(src:)` markers to guide pages, never compiled pages; `chat@4` (the answering order, draft and out-of-date wording); the guide's AI assistant page and its compiled page. The statutory rule of AI-53 rev 0.8 is in the prompt only: the platform's own check is task 3.3.
- [x] 3.2 The panel renders the know-how chip (violet AI content only, no compiled-page link); Vitest with axe. Tests
  `// req: AI-22`.
  Built: `AssistantPanel` shows a chip per guide source (BookOpen icon, "awaiting review" for a draft guide page) linking `/help/<guide>#<anchor>`; Vitest with axe.
- [ ] 3.3 Statutory pages: the pack in force newer than the page's makes the tool result say so, and the answer is from
  `packs.get` (G4-091). Tests `// req: AI-53`.
  **Left:** the platform's check that the pack in force is newer than the page's cited version, and the answer-level proof (G4-091), which needs a recorded model run `[live-verify]`.

## 4. The organisation's knowledge base

- [x] 4.1 Migration 000563: `kb_note`, `kb_page`, `kb_page_version`, `kb_snapshot`, `kb_snapshot_page`, `kb_lint_finding`
  (append-only, guards, FORCE RLS, composite keys, the export catalogue, goldens). Tests `// req: AI-56`.
  Built: migration 000563 (six tables, append-only guards, FORCE RLS, composite keys, the export catalogue) and 000564/000565 (a page may cite up to 200 sources); the storage port `OrganisationKnowledge()` and the adapter; goldens. Tests carry no `// req:` tag (AI-56 is only part proved here).
- [x] 4.2 The organisation's search and index over its newest snapshot, merged with the platform's; overlays win for that
  organisation. Tests `// req: AI-58`, `// req: AI-56`.
  Built: the organisation's search and index over its newest snapshot, merged with the platform's through the reader filter (`mergeIndex`, `mergeHits`); `kb.read` serves an organisation page with its sections and the note that it is the organisation's own.
- [x] 4.3 The notes API (create, edit, list; audited). Tests `// req: AI-50`, `// req: AUD-01`.
  Built: `/v1/knowledge-base/notes` (create, edit with `If-Match`, list, get), audited in the same transaction.

## 5. Settings-rendered pages

- [x] 5.1 `kb.Render` from settings, approval policies, team mode and recommended-default decisions with their reasons, no
  model; written as `rendered` versions when a setting takes effect, per the owner's decision 3. Tests `// req: AI-50`,
  `// req: FR-ORG-08`.
  Built: `knowledge.Render` (team mode, who approves what, one page per settings group; no model, no clock), written as `rendered` versions and published with the setting change that made them (the owner's decision 3); a person's edit is never overwritten and a re-render waits beside it with a finding. Untagged (AI-50 and FR-ORG-08 are only part proved).

## 6. The compiler

- [x] 6.1 Migrations 000564/000565: the role `fyniq_kb_compiler`, its grants (read sources, insert drafts) and the drafts-only
  trigger; every list of roles (`ServiceRoles`, the catalogue and attack tests, `wiring/local.go`, compose,
  `infra/modules/platform-spec`, `db/README.md`, goldens). Attack tests: it cannot publish, read the ledger, change a
  version or write anything but a draft. Tests `// req: AI-57`, `// req: CMP-AI-01`, an `// asvs:` tag.
  **Changed (decision 1): no database role.** The compiler holds no database role and no grant; it reaches the API's four internal operations under its own service-token class `kb_compiler`, which opens nothing else, so "cannot publish, read the ledger or change a version" holds by construction and is tested by the refusals of every other token and operation. No migration 000564/000565 for a role (those numbers went to the sources limit).
- [x] 6.2 The service-token class `kb_compiler`, the API's `compileKBPage` operation (agent `kb_compiler`'s route and budget,
  the AI switch, screening) and `contracts/ai/prompts/kb-compile@1.md`. Tests `// req: AI-57`, `// req: AI-74`.
  Built: the four operations (work, compile, drafts, findings) with the route of agent `kb_compiler` and the AI switch, screening of the notes as untrusted text, the lint again on every draft, and `contracts/ai/prompts/kb_compile@1.md`. `internal/kbcompile` is the job's library and client.
- [ ] 6.3 `jobs kb-compile`: the changed sources, the Go lint with one retry, the drafts, `kb_compile_run`, the human-edit
  conflict finding; the weekly semantic lint as findings and Inbox tasks. Tests `// req: AI-52`, `// req: AI-56`.
  **Built in part:** `jobs kb-compile <org>` (`make kb-compile ORG=…`), the changed notes, the lint with one retry, the drafts, and the human-edit conflict finding. **Left:** the weekly semantic lint as findings and Inbox tasks, the schedule (Cloud Scheduler) and the cloud identity of the job `[live-verify]`.

## 7. Publishing

- [x] 7.1 Migrations 000566/000567 and `kb_publication` as an approval object: the envelope, the policy default per team
  mode (open question 18), the checks before signing (lint, contradictions with settings, the pack and a newer platform
  page), snapshot n+1 in the executing transaction. Tests `// req: AI-56`, `// req: FR-APR-01`, `// req: FM-2`.
  Built: migrations 000566/000567 (`kb_publication` as an approval object), the envelope, the policies (Owner holds A on the row: decision 6; Founder mode publishes alone with a review-log entry, Team mode asks another Owner), the deterministic gate before signing (lint and contradictions with settings), snapshot n+1 in the executing transaction.
- [ ] 7.2 Edit, human-edited versions, rollback as a new version, overlays and their flag when the platform page changes;
  until a contradiction is resolved the assistant follows the setting or the pack and says so. Tests `// req: AI-56`.
  **Built in part:** edit (a human-edited version), rollback as a new version, the draft-beside-human-edit rule, and the overlay flag: an overlay records the platform page's digest, the index says "Fyniq changed this page" when it moved and the assistant is told the overlay may be out of date. **Left:** the pack check of the gate (task 3.3's front-matter field, an owner decision).
- [x] 7.3 The weekly review lists self-approved publications. Tests `// req: FM-3`.
  Built: a Founder-mode self-publication is a review-log entry in the weekly review (`ReviewSelfApproved`).

## 8. Administration

- [ ] 8.1 `/v1/knowledge-base` operations (design D9) in the contract, the authorisation matrix, the never-list for MCP and
  agent clients; each audited. Tests `// req: AI-55`, `// req: AUD-01`.
  **Built in part:** notes, the index, versions, edit, rollback, publish, request, snapshots and findings (list and resolve) are in the contract, the authorisation matrix and the audit trail; the never-list is unchanged (no write tool on MCP or the AI surface). **Left:** the recompile operation (it needs the job's trigger, `[live-verify]`).
- [ ] 8.2 The screen `knowledge-base` (Settings → AI): index, page, diff, versions, lint, recompile, publish; Basic and
  Expert; help keys; `docs/user-guide/knowledge-base.md` compiled into `kb/`; the fragment updated locally. Tests
  `// req: AI-55`, Vitest with axe.
  **Built in part:** the screen `/settings/ai/knowledge-base` (index with filter and search, a page's versions and line comparison, edit, roll back, publish with Team mode's request, findings and their resolution, the notes), its guide page `docs/user-guide/knowledge-base.md` compiled into `kb/` and linked from AI settings; Vitest with axe. The design fragment was already drawn. **Left:** the recompile button, and the P2 proposed-pages state; untagged (AI-55 is part proved).

## 9. Evals, controls and the finish

- [ ] 9.1 The `knowledge` suite kind and its ≥ 100 golden questions in the QA-AI-06 categories: deterministic recall per
  category, the revoked reader, citation resolution and the stale-fallback rate gated offline in `make evals`; answer
  accuracy recorded `[live-verify]`. Tests `// req: QA-AI-06` only for what each proves.
  **Built in part:** `evals/knowledge/knowledge.json` (103 questions in the six categories), `TestTheKnowledgeSuite...` in `make evals`: recall at the top 5 per category against its threshold (the measured baseline, owner and plan on the weak categories), citations of every page, the revoked reader, the fallback. A baseline of 0.75 paraphrase recall became 1.00 by aliases on 17 pages (AI-58). **Left, `[live-verify]`:** answer accuracy, the model's use of the answering order and the stale-guide answer need recorded model runs; QA-AI-06 is not ticked and no test is tagged with it.
- [x] 9.2 The publish gate (the deterministic part, per the owner's decision 2). Tests `// req: QA-AI-06`, `// req: AI-56`.
  Built: `kbGate` (lint, no invented numbers, no contradiction with a setting) runs before a publication is signed or self-approved; the live part of QA-AI-06 stays `[live-verify]`.
- [ ] 9.3 CTL-AI-08's nightly (`verify kb`, the reader-filter test) and event checks running. Tests `// req: CTL-AI-08`.
  **Built in part:** the nightly `verify kb.pages` (each published page still passes the structural lint, its notes are there, and each version was written by who its origin says), running in CTL-AI-08's N row. **Left:** the nightly run of the per-role reader filter against the live database (the worker holds no grant on the platform tables; the filter is a CI test), and the event check.
- [ ] 9.4 `docs/security/threat-models/knowledge-base.md` for security review #4; the build plan's paragraph; `make check`,
  `make test`, `make evals` pass.
  **Built in part:** `docs/security/threat-models/knowledge-base.md` (K1 to K10, K10 planned) and the build plan's paragraph. **Left:** the threat model's planned rows with their attack tests, and the final `make test` and `make evals` on the merged stack.

## Workflow follow-up

- `/opsx:verify p1-knowledge-base`, then `/opsx:archive p1-knowledge-base --skip-specs`.
- `p1-mcp-first-party` then exposes `kb.*` over MCP from the same registry.
