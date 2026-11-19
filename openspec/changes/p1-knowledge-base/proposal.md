## Why

The assistant answers questions about the books from tools. Its second job, "how does this work and what did we decide",
has no source yet: `kb/` holds 90 compiled pages and an index that the repository's scripts check, but the product
cannot read them, an organisation has no knowledge base of its own, and nothing compiles, publishes or evaluates one
(PRD-001 §9.11, ADR-T6). `p1-ai-assistant` (built) gave the assistant its loop, its read tools, grounding and its
eval harness, and said so in its non-goals: "the knowledge-base tools and how-to answers (`p1-knowledge-base`)". This
change is that. It also closes the control the plan moved here, CTL-AI-08, and the knowledge evals the owner moved here
on 14 Nov 2026 (rev 0.277, QA-AI-06).

## What Changes

- **The platform knowledge base at runtime (AI-50, AI-56, AI-58)**: the compiled pages in `kb/` and the guide sources
  they cite ship in the release, stamped with its version and `kb-schema@N`, and the migrate step loads them into
  installation-scoped tables (the way statutory packs are loaded: §11.4 "Global tables"), with PostgreSQL full-text
  indexes over the pages, their `aliases` and the raw guide text. A Go lint reads the same corpus as `scripts/kb.py`
  and the two must agree on one shared fixture set.
- **Three read tools (AI-49, AI-51, AI-54, AI-58)**: `kb.index`, `kb.read` and `kb.search` join the one registry. All
  three are filtered per reader when the question is asked: a page is open to a reader only if the guide pages it was
  compiled from are open to their roles and their data class is within their rights. The reader-filtered index is
  assembled for layer 1 of the context (AI-48); when it exceeds 8,000 tokens the top-level index lists sections and
  `kb.index(section)` opens one. `kb.search` is PostgreSQL full-text search (`tsvector`, `pg_trgm`): **no embeddings and
  no vector search, ever (AI-58)**.
- **Answers that cite what the reader can open (AI-51, AI-12, AI-53)**: a know-how segment in the answer's shape. It is
  words with the page version it came from; platform code checks every number in it against the cited section of a page
  the turn read, and renders a citation chip that resolves to a guide page, a setting or a record, never to a compiled
  page. A stale or missing page falls back to `kb.search` over raw sources; a statutory rule is re-read with `packs.get`
  and the answer says when the guide is older than the pack in force. The activity log records each page version used.
  `chat@4` carries the answering order.
- **The organisation knowledge base (AI-50, AI-56, AI-60)**: tenant tables for admin-written notes and policies, append-
  only page versions, numbered snapshots and lint findings. Pages are of two kinds: rendered **deterministically** from
  settings (approval policies, team mode, recommended-default decisions and their reasons, no LLM), and compiled from
  notes. A page inherits the strictest data class and the taint of its sources (AI-54). P1 compiles trusted sources
  only; uploaded documents are P2.
- **The `kb-compiler` job (AI-52, AI-57)**: its own identity, with its own database role that can read the sources and
  insert **draft** versions and nothing else (it cannot publish, read the ledger or call domain commands). Its model call
  goes through one internal API operation as the scheduled agents' does (rev 0.281), routed and budgeted as agent
  `kb_compiler` (AI-74). It never overwrites a human-edited version. It runs when a source changes and weekly for the
  semantic lint; findings become Inbox tasks.
- **Publishing is an approval object (AI-56, FR-APR-01, open question 18)**: a `kb_publication` request signs the
  page versions it publishes; in Founder mode the default is self-approval with the weekly review, in Standard and
  Enterprise a second person. A publish makes the next numbered snapshot; a rollback makes a new version that restores
  the old content, then publishes like any other. An edit to a platform page is an overlay for that organisation only.
  A page that contradicts a setting, the pack in force or a newer platform page is flagged to the Owner before it is
  published, and until it is resolved the assistant follows the setting or the pack and says so.
- **Administration (AI-55)**: Settings → AI → Knowledge base (`knowledge-base`) for the Owner and the Administrator
  to browse the index and pages with their sources, diff two versions, edit, roll back, recompile and see lint findings;
  the Auditor can view. Every action is audited. Compiled pages are not shown anywhere else.
- **Knowledge evals (QA-AI-06)** on the harness `p1-ai-assistant` built: at least 100 golden help and policy questions
  in categories (paraphrase and synonym, other languages, negation and exception, conflicting effective dates, a stale
  guide against the pack in force, a revoked reader), measuring citation correctness, the stale-fallback rate and
  retrieval recall per category, in replay mode in `make evals`. The deterministic parts (recall, revoked reader,
  citation resolution) are gated offline; answer accuracy needs recorded model runs and is `[live-verify]`.
- **CTL-AI-08's nightly and event checks (AI-52, AI-54, AI-56)**: a nightly reader-filter test and the QA-AI-06 gate on
  each publish; `compliance/controls/CTL-AI-08.yaml` running in full.
- **No hidden memory, no filing of answers (AI-59, AI-60)**: nothing the assistant says becomes a page by itself, and
  anything it should remember across conversations is a setting or an approved page. (`kb.propose_page` is P2.)

## Capabilities

### New Capabilities

None: the requirements are in the main specs already (`ai/knowledge-base`, `ai/evaluation`,
`compliance/ai-and-api-controls`, `platform/release-and-versioning`). This change builds them (`skip_specs: true`).

### Modified Capabilities

None. The owner's decisions this change asks for (design.md "Decisions needed") become a PRD revision, with the specs
following, when they are taken.

## Impact

- **Requirements built** (PRD-001 §9.11, §12.10, Appendix F.7): AI-48 and AI-49 (their knowledge-base parts), AI-50 to
  AI-58, AI-60, the two unnumbered `ai/knowledge-base` rows (context built fresh, context layers), QA-AI-06, CTL-AI-08
  and the unnumbered "Knowledge base versions" row. Not built: AI-59 and untrusted sources (P2).
- **Screens**: `knowledge-base` (the fragment exists; it is updated locally, no Claude Design publishing).
- **Code**: a `kb` package and loader in the migrate step; global and tenant tables under FORCE RLS with guards (migrations
  000560 to 000579); the storage port and adapter; three tools in `contracts/ai/tools/`, their internal operations, the
  reader filter and the index operation; the grounding shape, `chat@4` and the panel's chips; the notes and page API
  and the publish object; a new database role and the compiler job; the screen; the eval suite; the guide page and its
  compiled `kb/` page.
- **Security**: a new database role with a drafts-only grant (`fyniq_kb_compiler`), a new service-token class, a new
  AI read path filtered per reader, and a new approval object. Threat model `docs/security/threat-models/knowledge-base.md`
  for security review #4. The AI never gains a write path: the never-list (§9.6) and CMP-AI-01 are unchanged and the
  tools are read-only.
- **Depends on** `p1-ai-assistant` and `p1-scheduled-agents` (built), `p1-approval-engine` (built).
