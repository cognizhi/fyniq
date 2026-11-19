## Context

What exists (read from the repository on 18 Nov 2026):

- **The repository side is done.** `kb/` has `schema.md` (`kb-schema@1`), 90 compiled pages (12 `concept`, 78 `task`), the
  generated `index.md` (about 20 KB, roughly 5,000 tokens), and `log.md`. `scripts/kb.py` generates the index and lints
  (front matter, source hashes, coverage, links, citations, number grounding); `make kb-lint` is in `make check`. 59 of
  the 90 pages are `status: draft`, because a guide page they cite is a draft awaiting an accountant's review.
- **The assistant side is built** (`p1-ai-assistant`): `cmd/ai` and `internal/assistant` run the tool loop; the registry
  `contracts/ai/tools/*.json` (`internal/aitool`, pinned in `tools.lock.json`) has eight tools; `callAITool` in
  `internal/api/ai_tool_calls.go` runs each as the turn's person through the public read operations; answers are
  structured segments (`text`, `fact`, `missing`) whose `text` may hold no number other than a date or ordinal
  (`internal/assistant/grounding.go`, `stray`); prompts are `contracts/ai/prompts/chat@N.md`; the eval harness is in
  `internal/assistant/evalsuite` and `make evals`.
- **Nothing reads `kb/` at runtime.** No Go package embeds it, no `kb.*` tool exists, the organisation has no tables
  for it. `agents/company-secretary/SKILL.md` and `internal/agents/skill.go` already say the `kb.*` tools are not
  offered "until `p1-knowledge-base`". The AI routes already list the agent `kb_compiler` (000356). Appendix D already
  has the row "Knowledge base: compiled pages and index" (`authz.KnowledgeBase`: Owner and Admin VC, Auditor V; the AI's
  cell is "Reads, filtered to the user (AI-54)"). `compliance/controls/CTL-AI-08.yaml` has its CI check running and its
  nightly and event checks planned for this change.
- **Precedents to copy.** Statutory packs: shipped content is validated and loaded into global tables by `cmd/migrate`
  as the migrator (`postgres.LoadShippedPacks`), read through a view (PRD §11.4 "Global tables" already names "platform
  knowledge-base pages"). Document templates: publishing a template is an approval object (`document_template.publish`).
  Scheduled agents: a job's model step goes through one internal API operation under the job's own service-token class
  (rev 0.281, `runAgentModel`), so no job holds a provider credential or the router's tables.
- **Where code collides with the requirements** (so the design has to resolve each):
  1. `stray()` refuses any number in an answer's words, and how-to answers carry numbers ("within 30 days").
  2. The registry caps a tool result at 4,000 tokens, but the largest compiled page is about 7,000 and AI-58's index is up
     to 8,000 tokens and belongs to layer 1.
  3. Appendix D gives the assistant no right on the Knowledge base row, only the note "filtered to the user", so `kb.*`
     cannot ride a Settings read operation on an Employee's token.
  4. A compiled page has no `roles` of its own; the guide pages it cites do (`docs/user-guide/*.md` front matter `roles`,
     parsed today by `apps/web/src/help/guide.ts`).

## Goals / Non-Goals

**Goals:**
- The assistant answers know-how from the knowledge base, with citations the reader can open, and never from a page the
  reader may not open.
- An organisation has its own knowledge base that a person controls: nothing the AI wrote is live until a named human
  approves it, and a human edit is never overwritten.
- The compiler can be compromised without reaching the ledger, a publish or a credential.
- Retrieval misses are fixed in the compile step, measured per category.

**Non-Goals:**
- Embeddings, a vector index, `pgvector` (AI-58, ADR-T6), in any phase.
- Uploaded agreements and minutes, `taint: untrusted` pages and `kb.propose_page` (P2: AI-54 second half, AI-59).
- Changing the repository's compile process (`fyniq-knowledge-base` skill) beyond what the shared fixture needs.
- Nemura's knowledge base (PRD-002).

## Decisions

**D1. Platform pages: shipped in the release, loaded as global tables (AI-50, AI-56).**
- A top-level Go package `kb` embeds `kb/pages`, `kb/index.md` and `kb/schema.md`; a second small package embeds
  `docs/user-guide/*.md` (the raw sources, which `kb.search` falls back to). Both need `.dockerignore` and Dockerfile lines
  for the migrate image only: the API and AI images do not embed them.
- `cmd/migrate up` loads them after the migrations, as the migrator, like `LoadShippedPacks`: `kb_platform_release`
  (release version, `kb-schema@N`, digest), `kb_platform_page` (key, type, title, summary, aliases, body, sources with
  SHA-256, data class, taint, status, audience roles, compiler, `tsvector`) and `kb_platform_source` (guide page key, file,
  title, roles, status, body, `tsvector`). A loaded page set with another digest than the release's is replaced in one
  transaction (these are not packs: the set is a snapshot of the release, and the previous release's rows go with it);
  the API's and the AI's roles read through views only.
- The validation before loading is a Go lint over the same corpus as `scripts/kb.py`. They agree by a shared fixture set,
  `test/kb/fixtures/` (valid pages and one broken page per rule, each with the findings both must give); both test suites
  run it, so a rule changed in one fails the other. The Go lint is the only one the product ships.
- Alternative: embed in the API and search in memory. Rejected: AI-58 names PostgreSQL full-text search and `pg_trgm`
  over pages, aliases and raw sources, and the same query must serve platform and organisation pages together.
- Alternative: copy the pages into every organisation. Rejected: 90 pages times every tenant for content that cannot
  differ; an edit is an overlay (D7).

**D2. The reader filter (AI-54, AI-01, AI-02).**
- A page is open to a reader when (a) every source's audience admits one of the reader's roles (a guide page's `roles`
  front matter: `all`, or role keys, the same rule `forPerson` applies, the Owner reading all) and (b) the page's data
  class is within the reader's rights: `general` for everyone, `personal` and `identity` need a right on Contacts or
  Members, `payroll` the Payroll right (and `ai.payroll_access`, AI-02), `equity` a view of the cap table. The audience
  rule's source of truth is the guide page front matter; the web guide keeps its own parser, and a contract test reads
  every guide page through both the Go and the TypeScript parser (the TypeScript one through the Vitest suite, over a JSON
  list the Go test writes to `test/kb/`) so they cannot drift.
- The filter runs in the database query (a SQL function taking the reader's roles and classes), not after it, so a count,
  a snippet, a summary and the index never reveal a page the reader may not open. It applies to the raw-source fallback of
  `kb.search` on the guide text too (rev 0.8).
- Appendix D: the AI's cell on the Knowledge base row stays "Reads, filtered to the user". The tools do not call a
  Settings operation: they run through new internal read operations (`kbIndex`, `kbRead`, `kbSearch`) that take the turn's
  delegated token, resolve the person's live roles as the other tools do, and apply this filter. The authorisation matrix
  gets rows for them (Employee: allowed, filtered; revoked reader: refused). `aiReadable` is for public operations and is
  not widened.
- `kb.*` calls add the know-how class to `toolClasses` (AI-83); an organisation page whose data class is `personal` adds
  personal.

**D3. The tools and the index (AI-49, AI-48, AI-58).**
- `contracts/ai/tools/kb.index.json`, `kb.read.json`, `kb.search.json`, version 1, `runs: api`, scope `ai.read`, read-only,
  each with a strict bounded input: `kb.index` takes an optional `section`; `kb.read` takes a `key` and an optional
  `section` (a heading anchor); `kb.search` takes a `query` (at most 200 characters), an optional `scope`
  (`pages`, `sources`, both by default) and a `limit` of at most 10. Pinned in `tools.lock.json`.
- A page is read by section: the registry's 4,000-token cap stays, and a page over it returns its outline (the headings and
  the first line of each) and the requested section, never silently cut. The result names the page version (platform:
  release version and the page's digest; organisation: the number) so the answer can cite it and the log can record it.
- Layer 1's index is not a tool result: the orchestrator asks one more internal operation (`kbIndex`, same filter) when it
  builds the turn's context, and puts the reader-filtered index into the cached prefix (AI-27). While it fits in 8,000
  tokens it is the full index; above that, it is a top-level list of sections (the page key's first segment) and the model
  calls `kb.index(section)`. The index is rebuilt from published pages' front matter by the same generator as
  `scripts/kb.py index` (Go port, same fixture), never stored by hand.
- `kb.search`: `websearch_to_tsquery` ranked with `ts_rank_cd` over the page's key, title, summary, aliases and body
  (weights A to D), plus `pg_trgm` similarity on titles and aliases for misspellings, over `kb_platform_page`,
  `kb_page_version` (published) and, as the fallback, `kb_platform_source` and the organisation's notes. A hit is a key, a
  title, the summary and a snippet, with its source kind (page or raw source); the model reads the page next. The
  text search configuration is `simple` (no stemming), so Chinese and Malay terms match as stored tokens and the aliases
  carry the user's own words; questions in other languages are measured, not assumed (D10).
- Publication state in answers: a `status: draft` platform page is served with `draft: true` and the assistant is told to
  say the guide page it comes from is awaiting review (the same banner the guide shows, FR-UX-41). See Decisions needed 4.

**D4. Answers (AI-51, AI-53, AI-12; `chat@4`).**
- The answer's segments gain **`know_how`**: `{page, section, text}`. `page` is the key and version a `kb.read` returned in
  this turn, `section` a heading anchor of it, `text` the sentence in the assistant's words. Platform code checks that every
  number in `text` appears as a whole token in that section (the lint's rule), that `page` was read in this turn, and that
  its figures are not an organisation figure: figures about the books are `fact` segments and nothing else.
  A failing segment goes through the same one regeneration and then `missing` as a failing figure (G4-089/090).
  `stray()` keeps refusing numbers in plain `text`.
- A `know_how` segment is rendered with a **citation chip** resolved by platform code, never by the model: a platform page
  resolves to the guide page it was compiled from and the cited heading (`/help/<key>#<anchor>`), an organisation page
  rendered from settings to the setting's page, an organisation page from a note to the note in Settings (visible to those
  who may see the note; for others the chip says "the organisation's policy" with no link). No chip ever links a compiled
  page: the page carries a `cite` list in its front matter resolved at load time, and a page with none cannot be cited.
- Statutory guides (AI-53 rev 0.8): a page whose section carries a pack key and version is cross-checked: when the pack in
  force on the question's date is newer than the page's, the platform sends the model that fact in the tool result and
  the answer must be from `packs.get`, say the guide may be out of date, and state no rule it cannot confirm. This is
  evaluated by platform code in the tool result, not left to the model; the eval (G4-091) checks the answer.
- The AI activity log records, for every `kb.*` call, the page versions returned (`ai_tool_call` gains a `kb_pages`
  column: key and version per page), and the turn's page versions are the union; the activity log screen shows them.
- `chat@4` adds the answering order (index, `kb.read`, then `kb.search` over raw sources when a page is stale or missing),
  "figures only from read tools", the know-how segment's shape and the draft and out-of-date wording. The existing golden,
  red-team and proposal suites replay recordings made under `chat@3` and with eight tools. Whether a cassette is matched on
  the tools offered or the prompt, and so whether adding three tools and bumping the prompt breaks the replays or only
  needs a re-recording `[live-verify]`, is read from `internal/assistant/evalsuite` first (task 2.4), before either change
  is made; the same for the company secretary's run, which is offered a skill's tools the registry has.

**D5. The organisation knowledge base's tables (AI-50, AI-56; PRD data model `kb_page`, `kb_page_version`, `kb_snapshot`,
`kb_lint_finding`).**
- `kb_note` (admin-written note or policy: key, kind `note|policy|decision`, title, body, data class, a version counter,
  audited like any record; its SHA-256 is a source's hash).
- `kb_page` (key, type) and `kb_page_version` (append-only: key, number, title, summary, aliases, body, sources with
  SHA-256, data class, taint, origin `rendered|compiled|human_edited|overlay|rollback`, `human_edited`, compiler model and
  prompt version, base platform page and digest for an overlay, author person or `kb-compiler`, created at, `tsvector`).
  There is no status column: a version is **published** when the live snapshot lists it, **a draft** when it is newer than
  the published one for its key.
- `kb_snapshot` and `kb_snapshot_page` (append-only: number, the approval that made it, key and version per page). The live
  knowledge base is the newest snapshot; the index and the search read only it (plus the platform's).
- `kb_lint_finding` (kind, page key and version, detail, open or resolved with who and why, the Inbox task raised).
- Every table: `organisation_id`, composite foreign keys, `FORCE ROW LEVEL SECURITY`, the tenant export and the data
  inventory, goldens, guards (FY-numbered), attack tests.
- Cross-organisation reading of the platform tables is by the global views alone.

**D6. Settings-rendered pages (AI-50).**
- A pure function `kb.Render(settings, policies, mode)` turns the organisation's effective settings, approval policies,
  team mode and recommended-default decisions (each setting's value, its recommended value and its one-line reason from the
  catalogue, and whether the organisation changed it) into pages (`org.approval-policy`, `org.team-mode`, `org.settings.<group>`).
  No model, no network, byte-identical for the same input, with `taint: trusted`, data class `general` unless a setting
  names a person. The numbers in them are the settings' own values and appear in the rendered text; they are structured
  from the catalogue, not free text from users.
- The render runs on a setting's effective change (in the transaction that applies it) and writes the page versions as
  origin `rendered`; see Decisions needed 3 for whether they publish with it.

**D7. The compiler (AI-52, AI-57; rev 0.281's pattern).**
- `kb-compiler` is its own identity: a Cloud Run job (`jobs kb-compile`, run-to-completion, started by the tick or by
  "Recompile") with the database role `fyniq_kb_compiler` (migration 000565; see Decisions needed 1) and the service-token
  class `kb_compiler`. The role holds `SELECT` on `kb_note`, the rendered inputs it compiles from (a view of what it
  may read: settings and policies, not the ledger), `kb_page_version` and `kb_snapshot`, and `INSERT` on `kb_page_version`
  and `kb_compile_run`, and nothing else. A trigger refuses an insert from it that is not origin `compiled`, a draft, with
  `human_edited` false and no author person, and no insert supersedes a version with `human_edited`.
- The job, per source that changed (a note's or setting's hash differs from the page's recorded source hash): reads the
  source as data; calls the API's `compileKBPage` (service token class `kb_compiler`, which opens that operation alone;
  routed and budgeted as agent `kb_compiler`, so the data-class policy and the AI switch apply; the source is screened
  like any untrusted text, AI-18); receives the page text; runs the Go lint (front matter, every number in a cited section
  of its sources, citations resolve, `[[links]]`); on any failure retries once with the findings, then writes a lint
  finding instead of a draft; on success inserts a draft version. It never publishes, so nothing it writes is read by the
  assistant (D5: only published versions are live).
- The weekly semantic lint is the same job with a second prompt (`contradictions`, `superseded`, `orphans`, `missing`,
  `cross-references`), whose output is lint findings only, each an Inbox task for the Owner and the Administrator.
- A human-edited version is never overwritten: a recompile is written beside it as a draft with `origin: compiled` and
  a `human_edit_conflict` finding that a person resolves.
- The model call's prompt is versioned in `contracts/ai/prompts/kb-compile@1.md` and hashed in the version; a change to it
  or to the compiler model needs the QA-AI-06 gate (D10).

**D8. Publishing (AI-56, FR-APR-01, FM-2, FM-3, open question 18 decided).**
- `kb_publication` is an approval object (`kb_publication.publish`), like `document_template.publish`: the envelope lists
  each page key with the version and its digest, the base snapshot number, the lint result and the contradictions
  accepted; it is signed under the organisation's approval policy for "knowledge-base change", whose default in Founder mode
  is self-approval with the weekly review (open question 18, decided 16 Oct 2026) and in Standard and Enterprise mode a
  second person. A signed publication makes snapshot n+1 = the previous snapshot with those versions swapped in, in one
  transaction, audited. The widening migration redeclares the whole object list (see the migration plan).
- **Before the request can be signed**, the API runs the checks: deterministic lint clean for each version; each page
  compared with the settings (a page that contradicts a setting by a rendered value, FR-ORG-08), the pack in force, and the
  newer platform page for an overlay; a contradiction blocks signing until the Owner resolves it or accepts it with a
  reason (recorded in the envelope), and until resolved the assistant follows the setting or the pack (the platform marks
  the page `conflicts` in the tool result and the prompt says so).
- Edit: an Owner or Administrator's edit writes a `human_edited` version (a draft), which the compiler never overwrites.
  Rollback writes a new version restoring the chosen old content, then publishes like any change. An edit to a platform
  page is an `overlay` version for that organisation (the base page's digest recorded); when the platform's page changes,
  the overlay is flagged for the Owner.
- The weekly review of FM-3 already lists self-approved high-risk actions; a `kb_publication` signed by its own requester
  is one.

**D9. The administration surface (AI-55).**
- `/v1/knowledge-base/...` operations (list and search the index with each page's state, read a page with its sources and
  versions, diff two versions, save a note, edit, request a publish, roll back, recompile, list and resolve lint
  findings), all under the Knowledge base row: View for Owner, Admin and Auditor; Create for Owner and Admin (edit,
  publish request); signing under the approval policy. Each is in the OpenAPI contract and on the never-list for MCP and
  agent clients: nothing here is offered to the AI, which only reads (D3).
- The screen `knowledge-base` in Settings → AI, built from the fragment: the summary cards, the index with platform and
  organisation tabs, a page's detail with the diff, versions and sources, lint findings, and the plain statement of what
  the assistant may use. Help keys, a guide page `docs/user-guide/knowledge-base.md` compiled into `kb/`.

**D10. Evals (QA-AI-06, CTL-AI-08).**
- A suite kind `knowledge` in the harness: at least 100 golden questions over the sample company's guide and a seeded
  organisation knowledge base, each with the expected page keys, the key facts and the guide pages a citation must resolve
  to, in the categories of QA-AI-06 (paraphrase and synonym, other languages, negation and exception, conflicting
  effective dates, a stale guide against the pack in force, a revoked reader). It is split in two:
  - **Deterministic, gated offline in `make test` and `make evals`**: retrieval recall per category (the expected page is in
    the top results of `kb.search` or listed in the filtered index), the revoked reader (the page, its summary, its
    snippet and its index entry are absent), citation resolution (every chip resolves to a guide page, a setting or a
    record and none to a compiled page), and the stale-fallback rate. A category below its threshold for two releases needs
    a named owner and a fix plan (a line in the report).
  - **Needs recorded model runs, `[live-verify]`**: answer accuracy and the model's use of the answering order, replayed
    from `evals/cassettes/assistant` once recorded.
- A test names `// req: QA-AI-06` only for the part it proves; the requirement is ticked when the live part is recorded.
- The publish gate (Decisions needed 2) uses the deterministic part.
- CTL-AI-08: the nightly check runs the reader-filter test against the live database (every page and the index, for each
  role) and `verify kb` (every published version lints, no human-edited version overwritten); the event check is the
  publish gate's result recorded as evidence.

## As built (18 Nov 2026)

What the first five slices settled, where the build differed from the text above:

- **Citations come from the body, not front matter.** D4 said a page carries a `cite` list resolved at load. The pages
  already cite `(src: <guide path>#<anchor>)` at each section, which `kb.py` and the Go lint check, so the tool resolves a
  section's citations from those markers to the guide page's own key, title and anchor; no front-matter field was added and
  `kb-schema@1` is unchanged. A compiled page and its guide page may share a key (`approvals.policies` is both), so the
  citation is the guide's file, resolved by the loader, never a lookup by key in the compiled set.
- **Search terms.** A question is searched by its words, not its sentence: `knowledge.QueryTerms` drops English filler words
  and one-letter words, keeps any script whole, and passes at most 12 terms ORed, ranked by `ts_rank_cd` with trigram
  similarity of the title and aliases. Retrieval misses were fixed by aliases (AI-58); the other-language baseline is 1 in 10.
- **The load is one call.** `kb_platform_load(jsonb)`, run only by the migrator, replaces the whole set, because the
  architecture test allows only the unit of work to begin a transaction.
- **The reader's classes.** `kbReader` takes the roles as the guide names them and works the data classes out from Appendix D:
  personal from Contacts or Users and roles, equity from the cap table, payroll only with AI payroll access and the person's
  payroll right, identity never.
- **No operation of its own.** The tools run inside `callAITool`, so the authorisation matrix has no new row; the page
  versions travel in the tool result and the turn's tool call.
- **Mode is not an access matter.** The guide's Basic and Expert split is a display preference; the assistant answers from a
  page whatever mode the reader is in.
- **The compiler has no database role (decision 1, outcome).** It is a job with a service-token class and the API's four
  compiler operations; the API holds the only database access, runs the lint again on every draft and writes the draft as
  the compiler. The stronger boundary (no grant to widen) replaced the planned role and its attack tests.
- **The Owner holds A on the knowledge-base row (decision 6).** Appendix D's row gives the Owner view, create and approve;
  the Administrator edits and asks only, so Founder mode lets an Owner publish alone and Team mode asks another Owner.
- **Rendered pages publish with the setting change (decision 3).** They are the setting's own words, so no second person
  is asked; a person's edit of one is never overwritten.
- **Recompile and the weekly lint (20 Nov 2026, PRD-001 rev 0.330; decided while the owner was away, small).** A person's Recompile
  is an outbox row (`kb.compile`) the worker runs as the compiler through `kbcompile.APIClient` with the compiler's own token, so the
  API never runs the model loop and the person's token is never the compiler's. The worker holds that token only in the local build (as
  it holds the agents' model-step token); the release build has none, so Recompile answers `KB-COMPILER-03` and the weekly job logs
  that it did nothing, until the compiler's own Cloud Run job and service account exist `[live-verify]`. The weekly pass's deterministic
  half is a fifth compiler operation, `kbCompilerLint`, which raises findings and nothing else, so the worker needs no grant on
  `kb_lint_finding` (the alternative was rejected: a wider boundary for a weekly job). The advice kinds (stale, source without page,
  superseded, link to no page, orphan) never block a publication (`knowledge.BlocksPublication`): the new version is their answer, and an
  open advice finding that blocked the page it is about would stop the fix. An "Inbox task" is the findings list of Settings → AI →
  Knowledge base, not a Home row. A nightly per-role reader-filter run against the live database stays a CI and local test: the worker
  holds no grant on the platform tables and none is widened for a check.
- **Left from this design:** layer 1's cached index (task 2.3), the platform's check against the pack in force (task 3.3),
  the recompile operation (8.1, 8.2), the weekly semantic lint and the schedule (6.3), the answer-level evals (`[live-verify]`) and CTL-AI-08's
  nightly and event checks (9.3).

## Migration plan

Range 000560 to 000579, allocated per slice and re-checked against the highest object list on `origin/main` after every merge
(the widening migration redeclares the whole `approval_request_object_known` list):

| Number | What | Slice |
|---|---|---|
| 000560 | `kb_platform_release`, `kb_platform_page`, `kb_platform_source`, the reader views, the search functions and indexes | 1 |
| 000561 | `ai_tool_call.kb_pages` (the page versions used), `NOT VALID` | 3 |
| 000562 | its validation | 3 |
| 000563 | `kb_note`, `kb_page`, `kb_page_version`, `kb_snapshot`, `kb_snapshot_page`, `kb_lint_finding`, guards, RLS | 4 |
| 000564 | `kb_compile_run` and the rendered-input view | 6 |
| 000565 | the role `fyniq_kb_compiler`, its grants and the drafts-only trigger | 6 |
| 000566 | `kb_publication`: the approval object widened `NOT VALID` | 7 |
| 000567 | its validation | 7 |
| 000568 to 000579 | spare (corrections, the organisation's search function, lint finding states) | |

## Risks / Trade-offs

- [A compiled page leaks to a reader who may not open it, through a snippet, a count or the index] → the filter is in the
  query and applies to every derived output (SEC-34); the nightly reader-filter test and the revoked-reader cases fail the
  build, with a matrix row per operation.
- [The model states an invented number as know-how] → the `know_how` segment's numbers are checked against the cited
  section; plain text still refuses numbers; the lint checks the page's own numbers against its sources (it proves the
  number exists, not that the claim is right, which the evals and the weekly semantic lint cover).
- [A guide is older than the pack in force] → fail closed in platform code (D4), evaluated by G4-091.
- [The compiler, once compromised, writes a poisoned page] → it writes drafts only, nothing is live until a named human
  publishes, the publish gate checks and shows the diff; a page's text is passed to the model as data (AI-18) and is
  screened on its way out.
- [A new database role touches many lists] → it is its own task with the catalogue, attack and golden tests (task 6.1);
  the alternative is Decisions needed 1.
- [A migration collides with another agent's object list] → build the widening from the highest list on `origin/main` and
  re-check after each merge.
- [59 of 90 platform pages are drafts] → Decisions needed 4; the flag is a column, so a later decision is a one-line change.

## Decisions needed

Each has a recommended default, which the build uses until the owner decides; a different decision changes only the slice
named.

1. **The compiler's database identity** (slice 6).
   - **A. A new role `fyniq_kb_compiler`** (recommended): AI-57 read literally, "its own service identity", and the only
     way "cannot read the ledger" is a grant and not a promise. Costs: the role lists in `bootstrap_local.go`, the catalogue
     and attack-test loops, `wiring/local.go`, compose, `infra/modules/platform-spec` and `db/README.md`, one task.
   - B. Run as `jobs kb-compile` under `fyniq_worker`, fenced by a trigger on `session_user`. Cheaper, but the worker can
     read the ledger, so the fence is a trigger and a convention, not a grant, which AI-57 and CMP-AI-01 do not promise.
   - C. Run inside the API. Rejected: the API holds the write credentials.
2. **What gates an organisation publish at runtime** (slice 7).
   - **A. The deterministic gate on every publish, the live evals on a compiler model or prompt change and on each platform
     release** (recommended): lint clean, the retrieval and revoked-reader and citation cases of QA-AI-06 over the
     candidate snapshot, with no model call, so a publish stays free and instant. QA-AI-06's "a knowledge-base publish
     must pass" is read as this deterministic part, with accuracy proved at each model or prompt change.
   - B. A live eval run per publish: the most literal, but 100 model calls per publish, and a Founder edits a policy line.
   - C. Lint only. Cheapest; misses a page that now hides another from search.
3. **Do settings-rendered pages publish with the setting change or through the publish approval?** (slice 5)
   - **A. With the setting change, automatically** (recommended): the setting's own change is already an approval object
     (dual control, FM-1) and the page is a byte-for-byte render of it, so a second approval signs the same facts twice. It
     writes snapshot n+1 in the same transaction, audited, and the diff is in the audit event.
   - B. Through the publish approval like every page: uniform, but a founder approves the same change twice and the
     assistant would describe an old setting between the two.
4. **Are draft platform pages served to the assistant?** (slice 2)
   - **A. Yes, flagged** (recommended): 59 of 90 are drafts only because a guide page awaits its accountant's review
     (README), the guide shows them with the "Needs additional verification" banner (FR-UX-41), and the assistant says so.
   - B. Published pages only: the assistant would know a third of the product's how-to; the guide would answer and it would not.
5. **Platform page audience** (slice 2). Recommended default, decided here as small: the strictest of the cited guide pages'
   `roles`, with the Owner reading all, mirroring `forPerson`. Alternative: any single source's roles (more permissive).

## Open questions

None blocking. Statutory content in pages stays tagged `[verify]` where the guide source is; none is restated here.
