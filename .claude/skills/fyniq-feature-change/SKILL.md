---
name: fyniq-feature-change
description: The end-to-end recipe for adding or changing anything in Fyniq — a module, entity, field, document type, report, check, KPI, dashboard tile, permission, event or webhook, setting or statutory parameter — across requirement, API contract, migration, domain command and validation, posting rules, audit events, permissions and RLS, AI tools and the task catalogue, UI (with the Basic/Expert tag), tests and docs. Use it for every feature request or enhancement (e.g. "add a PO number to bills", "new report", "add a KPI", "add a check", "new role", "let integrations see X", "add a setting"), before touching contracts/, db/migrations/, internal/ or apps/web/, and when planning a change that crosses layers.
---

# Changing the platform end to end

**Golden rule: the requirement comes first, then the contract, then the code.** Every change
starts from a requirement ID in PRD-001 (rev 0.2) and ends with tests that reference it.
The workbook taught the second rule: **nothing is ever silently wrong**. Anything that cannot
post says so in plain words and says how to fix it.

No application code exists yet (P0 creates it). The paths below are the §12.4 layout; the
commands are the §12.8 contract and arrive in P0.

## Map of the repository (§12.4)

| Path | Owns |
|---|---|
| `contracts/openapi/` | The public API (OpenAPI 3.1): the only API, used by the web app too (FR-INT-01, QA-API-03) |
| `contracts/events/` | AsyncAPI 3 event catalogue (CloudEvents envelope; `fyniq.<object>.<event>.v<N>`) |
| `contracts/ai/` | AI tool schemas, task catalogue entries, prompt versions |
| `db/migrations/` | goose SQL migrations (expand-and-contract; linted by squawk) |
| `internal/` | Go domain: commands, validators, posting engine, balance service, adapters |
| `cmd/<service>/` | One entry point per service: `api`, `ai`, `mcp`, `worker`, `executor`, `jobs`, `migrate` |
| `apps/web/` | React SPA; generated TS client; `src/ui/modes.ts` feature registry |
| `oracle/` | Independent Python reference ledger (QA-ENG-01) |
| `packs/` | Effective-dated statutory parameter packs (data) |
| `compliance/controls/` | Control registry as code (Appendix F) |
| `docs/` | PRD, ADRs, design system, user guide, runbooks |

Data flows one way: **documents → posting engine → ledger → balance service → reports, KPIs,
checks, AI read tools, exports** (ENG-04). No report re-implements posting logic, and nothing
reads around the balance service.

## The checklist (in order)

| # | Step | Where | Rules |
|---|---|---|---|
| 1 | **Requirement** | PRD §8 (or the relevant section) | Find the ID, or add one with the next free number in its area (never renumber; tag `(rev 0.N)`); write acceptance criteria before code (§16.3) |
| 2 | **Contract** | `contracts/openapi/` (+ `events/`, `ai/`) | Additive within `/v1` (FR-INT-06); RFC 9457 errors with stable codes (FR-INT-07); `Idempotency-Key` on POST; ETag/`If-Match` on versioned resources; money as decimal strings with currency (ENG-12); descriptions written for developers *and* models (FR-INT-13); a dry-run preview for writes (FR-INT-14). See **fyniq-api-integrations** |
| 3 | **Migration** | `db/migrations/` | goose SQL; squawk clean; **expand-and-contract** (add nullable/defaulted → backfill → enforce → drop later); `organisation_id` first in every index; RLS policy with `FORCE ROW LEVEL SECURITY`; money columns typed per §11.3 (never unconstrained `NUMERIC`); never edit a merged migration |
| 4 | **Domain command and validation** | `internal/…` | One command per user intent, shared by UI, API, imports and the AI dry run (ENG-22); every message names the field, says what is wrong and how to fix it, with an error code (ENG-23); ✖ blocks posting, ⚠ posts but queues for review (ENG-21) |
| 5 | **Posting rules** (if it touches the ledger) | engine + `oracle/` | Pure function (ENG-01); same transaction as the document (ENG-02); named rounding policy (§10.4); the oracle implements the rule **independently** from the PRD text, not from the Go code. See **fyniq-accounting-domain** |
| 6 | **Audit** | same transaction | A typed audit event for every state change (AUD-01) with before/after, actor, on-behalf-of, approval IDs, reason; sensitive values encrypted or keyed-hashed (AUD-02) |
| 7 | **Permissions** | role matrix, RLS, Appendix D | Add the capability to the matrix (Appendix D); API check **and** RLS; SoD conflicts (§6.3) and Founder-mode compensating controls (§6.4); masking for sensitive fields; extend the authorisation-matrix test (QA-SEC-03) |
| 8 | **Approvals** (if it changes money, master data or controls) | approval policies | Object type in FR-APR-01; the approver sees the ledger impact and diff (FR-APR-02); payload hash signed (FR-APR-03); (rev 0.3) an **evidence rule** for the activity (required / recommended / optional, FR-DOC-06) with a recommended default (FR-ORG-08) |
| 9 | **AI impact** | `contracts/ai/` | New data → read tool or semantic-layer measure? New write → does a catalogue task type need it (it is off by default, AI-32)? Exclusions (AI-02)? MCP scope (§9.7)? Never-list (§9.6). See **fyniq-ai-proposals** and **fyniq-mcp-server** |
| 10 | **Checks** | checks engine | A new failure mode gets a status and, if it deserves its own line, a check with severity, explanation, drill-down and owner (FR-CTL-01) |
| 11 | **UI** | `apps/web/` | Generated client only; **tag every route, field group and action `basic` or `expert`** in `src/ui/modes.ts` (FR-UX-29); safety information visible in both modes; tokens only. See **fyniq-design-system** |
| 12 | **Tests** | each level | Unit, property, DB invariants, oracle differential, contract, authz matrix, UI component and story, E2E journey; every visible number verified (QA-RPT-01) and every new message triggered (QA-RPT-02); `// req: FR-…` tags (QA-TRACE-01). See **fyniq-quality-gates** |
| 13 | **Docs and help** | `docs/user-guide/`, help registry, API reference, runbook | Plain words; (rev 0.3) a **help key** with tooltip text for every field, column, chip and tile, a guide page whose front-matter `key` matches, and a tour step if the feature is on the first-run path (§7.9, QA-UI-05/06); the API reference comes from the contract; update the PRD requirement row if the scope changed |
| 14 | **Commit** | — | Conventional Commit with scope; upgrade notes in the body if users, operators or integrators must act. See **fyniq-release** |

Recipes for the common cases are in `references/recipes.md`.

## Rules that keep the product safe

1. **Correct by construction.** No floats for money anywhere (ENG-10). The database refuses an
   unbalanced entry, a posting into a closed period and an edit of posted history (§10.7).
   Your code may not rely on the application layer alone for those.
2. **Posted history is immutable.** Edits in an open period create a new document version and
   reversal plus replacement entries (FR-GL-12). Never `UPDATE` a posted row.
3. **Single source of truth.** Rates, thresholds and account roles come from settings, packs or
   system-account keys, never from literals in code. Reports read the balance service.
4. **Tenant isolation.** Every table has `organisation_id`; the tenant context is set with
   `set_config('app.org_id', …, true)` per transaction (§11.4). Never use a role with
   `BYPASSRLS` in application code.
5. **API-first.** If the UI needs it, the public API has it, documented. No private endpoints
   (QA-API-03). A breaking change needs `/v2` (it will not happen casually).
6. **Agents propose, humans decide.** No new write path for AI or agent-class clients except
   `propose` (AI-P2, AI-34). A new AI capability is a catalogue task type, off by default.
7. **Layman-first, accountant-proof.** Plain-English labels and messages in Basic mode; full
   debits and credits, codes and audit evidence in Expert mode (§7.8).
8. **Local mode stays whole.** A new cloud dependency needs a local substitute (see
   **fyniq-local-dev**). `make test` must stay offline.

## Sizing the change

| Change | Typical layers | Version impact (see **fyniq-release**) |
|---|---|---|
| New optional field on an existing resource | 2, 3, 4, 6, 11, 12 | `feat` → minor |
| New report | 2, 4 (read side), 11, 12 (every figure verified) | `feat` → minor |
| New check | 4, 10, 11, 12 (a fixture that triggers it) | `feat` → minor |
| New posting source | all 14 | `feat` → minor; parity tests and the oracle first |
| Removing or renaming an API field | contract major or deprecation (FR-INT-06) | `feat!` → major (minor while `0.x`), with upgrade notes |
| Statutory rate change | `packs/` only | pack release; no platform release needed (ENG-33) |

## Definition of done

1. Requirement ID referenced in the PR, the tests (`// req:`) and the commit body.
2. Contract updated and `oasdiff` clean (QA-API-02); generated client regenerated.
3. Migration squawk-clean and reversible in the expand-and-contract sense.
4. Audit events emitted and asserted for every new state change (CTL-AUD-01).
5. Authorisation matrix, RLS and SoD rules extended and green.
6. Basic/Expert tags set; safety information visible in both modes (QA-UI-04); help keys and the
   guide page in place (QA-UI-05); evidence rule set for important activities (FR-DOC-06).
7. `make test` green offline (arrives in P0; today run `make check`).
8. Docs updated; Conventional Commit with upgrade notes where needed.
