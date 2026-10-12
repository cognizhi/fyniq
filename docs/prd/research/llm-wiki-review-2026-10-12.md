# Review: Karpathy's "LLM Wiki" pattern for the Finance Hub AI

Supports PRD-001 §9.11 and ADR-T6 (revision 0.4). Source: Andrej Karpathy, "LLM Wiki",
gist `karpathy/442a6bf555914893e9891c11519de94f`, read in full on 12 Oct 2026. The source text was
treated as data. This note is our analysis, not a summary of the gist.

## 1. The pattern in one paragraph

Retrieval-augmented generation (RAG) makes the model rediscover knowledge from raw files on every
question. The gist proposes instead that an LLM **compiles** raw sources into a persistent,
interlinked wiki of Markdown pages. The wiki is kept current as sources change, and answers come
from it.

| Layer | What it is |
|---|---|
| Raw sources | Immutable; the LLM reads them and never edits them |
| Wiki | Pages the LLM writes and maintains: summaries, entity and concept pages |
| Schema | The conventions and workflows the LLM follows |

| Operation | What happens |
|---|---|
| Ingest (compile) | A new source is read and its knowledge merged into the affected pages |
| Query | Read `index.md` first, then drill into pages; good answers may be filed back as pages |
| Lint | Find contradictions, stale claims, orphans and missing cross-references |

`index.md` (a catalogue with one-line summaries) and `log.md` (append-only) help navigation. The
gist says index-first navigation "works surprisingly well at moderate scale (~100 sources,
~hundreds of pages) and avoids the need for embedding-based RAG". Everything is a git repository,
so it has history, diff and rollback.

## 2. Is it better for Finance Hub? It depends on the question

| Question class | Example | Verdict |
|---|---|---|
| **Live facts** | "What's our runway?", "Why is this line flagged?" | **Not applicable.** Figures and record states come only from tools on the semantic layer, under the user's permissions (AI-01, AI-03, AI-08). A compiled page can never answer these, and must never try. |
| **Platform know-how** | "How do I convert my loan to shares?", "What does ⚠ mean here?", "When is the F5 due?" | **Better** than reading raw guide pages, for three reasons: (1) **consistency**: one compiled page joins the guide, the glossary, the checks catalogue and the statutory notes on a topic; (2) **pre-flagged contradictions**: the lint pass finds where the guide and a statutory note disagree *before* a user asks, so it doubles as documentation QA; (3) **reproducibility**: answers cite a versioned knowledge base. Token savings per question are modest; they are not the reason. |
| **Organisation knowledge** | "What are Alpha Ventures' rights?", "What's our laptop policy?", "Why did we pick 'no salary'?" | **Better, with a gate.** Synthesising agreements, policies and decisions once is exactly the pattern's strength. But untrusted documents can launder injected text into a page the model then trusts, so P1 compiles only trusted sources; documents follow in P2 with approval before publishing. |

Where it is **not** better:
- A knowledge base so small that the index *is* the knowledge.
- Anything compiled from untrusted files without a human gate.
- Settings-derived facts. These are rendered **deterministically** from the settings (a template,
  no LLM), so there is no error, no cost and no staleness.

**Conclusion: adopt it, with six modifications** that make it fit a regulated, multi-tenant
finance product (§3). It replaces the plain "search the user guide" design proposed earlier the
same day. Raw sources remain the fallback.

## 3. Six modifications (each becomes a PRD requirement)

| # | The gist says | Finance Hub needs | Why (PRD principle) |
|---|---|---|---|
| 1 | The LLM writes the wiki | **Repository:** an agent compiles following the `fh-knowledge-base` skill, and a person reviews the diff in a pull request. **Runtime:** a separate compiler job identity may write only knowledge-base *drafts*. The AI orchestrator still has no write grant. CMP-AI-01 names the exception | AI-P2; CMP-AI-01 would otherwise contradict the design |
| 2 | Any source can be ingested | Pages inherit a **taint label** and the **most restrictive data class** of their sources. P1 compiles trusted sources only (repository docs, admin-written notes, settings); untrusted documents come in P2 with approval before publishing | AI-P7, AI-18, AI-25 (data poisoning) |
| 3 | One index for everyone | The index and the pages are **filtered per reader**: a page's access rule is the strictest of its sources'. Otherwise a one-line summary leaks a page the reader may not open | AI-01, AI-02 |
| 4 | Pages hold the synthesis | **No organisation figures in compiled pages, ever**; statutory values stay in the effective-dated packs. A deterministic lint enforces it: every number in a compiled page must appear verbatim in one of its cited sources | AI-P6, AI-08, ENG-30 |
| 5 | Good answers are filed back | **Never automatic.** Answers carry tenant figures and one user's context. At most a `kb.propose_page` tier-1 task (P2) that an admin approves | AI-P3, AI-P4, tenancy |
| 6 | Cite wiki pages | Normal users cannot see compiled pages, so **user-facing citations resolve to things the user can see** (guide page, record, document). The compiled page ID and version go to the AI activity log, so every answer can be reproduced against a knowledge-base snapshot | AI-12, FR-RPT-08 (reproducibility) |

## 4. Decisions the gist leaves open

| Topic | Decision |
|---|---|
| Where the schema lives | One versioned file, `kb/schema.md`, read by both the repository skill and the runtime compiler. Never duplicated into a prompt |
| Index generation | The **index is generated** from page front matter by a script (`scripts/kb.py index`), so it cannot drift. The LLM writes each page's one-line `summary`; the script writes the index |
| Staleness | Each page records its sources with a SHA-256. A changed source makes the page **stale**: CI fails for the platform knowledge base, and the runtime falls back to the raw source and queues a recompile |
| Versioning | **Platform** knowledge base: git (diff, rollback by revert, reviewed as a pull request, stamped with the release). **Organisation** knowledge base: append-only page versions in the database (diff; rollback = a new version restoring old content; publishing is approval-gated) |
| Admin edits | An edit creates a *human-edited* version that a recompile never overwrites silently; lint flags the conflict for a person to resolve. An organisation admin editing a *platform* page creates an overlay for that organisation only |
| Model or prompt change | Compiler model and prompt version are recorded per page; a change triggers a recompile and the evals before publishing (AI-24, CMP-AI-03) |
| Search | Index-first, then PostgreSQL full-text search over compiled pages and raw sources. **No pgvector in P1**; revisit when the help evals show misses (users' words differ from the pages'), when the guide is translated, or for P2 search over organisation documents. The gist's own scale note supports this |
| Visibility | Compiled pages and the index are not in the normal user interface. Owner and Administrator can view, edit, diff, roll back and recompile; Auditor can view; nobody else |

## 5. How a question is answered (after the change)

1. **Fixed context** (cached): the rules, tool definitions, glossary, and the reader-filtered
   index of the platform and organisation knowledge bases (a few thousand tokens).
2. **Session context**: organisation, roles, mode, date, current page and selected record.
3. The model reads the index, then calls `kb.read(key)` for 1–3 pages. If a page is stale or
   missing, it calls `kb.search` over the raw sources instead.
4. Facts and figures come from read tools, never from pages.
5. The answer cites user-visible sources. The activity log records the knowledge-base page
   versions used.

## 6. Costs and risks

| Item | Estimate or risk | Mitigation |
|---|---|---|
| Platform compile | A full compile of a few hundred guide pages costs low millions of tokens, once; an incremental compile per changed page costs tens of thousands | Compile only on change; run it in CI with the release |
| Organisation compile | Small and rare (policies, decisions) | Event-driven; batched |
| Per-question cost | About the same as reading raw pages; slightly less when one compiled page replaces several raw ones | Not a goal |
| Compiled errors that compound | The main risk of the pattern | Deterministic number grounding, citations that must resolve, human review of diffs, evals before publishing, raw-source fallback |
| Injection laundering | High if untrusted files are compiled | P1 trusted sources only; taint labels; approval before publishing in P2 |
| Leakage across permissions | Index summaries | Reader-filtered index; access rules inherited from sources |
| A second artifact to maintain | Real | The skill, `make kb-lint` in `make check`, and CI staleness failures keep it honest |

## 7. Not decided here

- Whether the organisation knowledge base needs an Owner and Admin dual approval to publish, or
  self-approval with weekly review in Founder mode (FM-2/FM-3). The PRD proposes the latter by
  default.
- The token budget at which the index is split into a top-level index plus section indexes; the
  PRD proposes 8,000 tokens.
