---
name: fh-bug-fix
description: Triage, reproduce and fix bug reports in Cognizhi Finance Hub Cloud, then correct any affected books safely and ship the fix. Covers what to ask for, classifying the report (data entry, engine/posting, validation, API, authorisation/RLS, AI, integration, UI/accessibility, performance), the symptom-to-cause table, reproducing on the sample company in local mode with a frozen business date, writing the failing test first, fixing at the right layer, correcting data only through approved journals or reversals (never edits of posted history, never SQL in production), the QA-REL-01 root-cause review for calculation defects, incidents for security or personal-data bugs, and a Conventional Commit fix(scope) with upgrade notes. Use whenever someone says a number is wrong, an invoice is not shown as paid, the books do not balance, a check fires unexpectedly, the AI gave a wrong figure, an API call fails, a page is broken or slow, or forwards a screenshot or user complaint.
---

# Fixing bugs

Work in this order: **understand → classify → reproduce → failing test → fix → verify → correct
the data → ship**. In a ledger, the data correction matters as much as the code fix, and it
follows the same controls as any other posting.

> **Status:** no code exists yet. The commands are the §12.8 contract, arriving in P0. Today you
> can run `make check`, `make deps-up` / `deps-down` and `make hooks`.

## 1. Understand: ask only for what is missing

- **Version:** `/version` or the About dialog (SemVer + git SHA, REL-05).
- **Where:** environment (production, staging, local), organisation, the record's URL or ID, and
  the **request ID** from the error (every RFC 9457 problem response carries one, FR-INT-07).
- **What:** what they did, expected and saw; the exact status text or error `code`; Basic or
  Expert mode (FR-UX-23); a screenshot.
- **When:** time and time zone; the period's lock state.

**Confidentiality.** Production data never goes to a laptop, a non-production project or an
issue tracker (DEV-08, CTL-CHG-05). Reproduce with synthetic data on the sample company. If you
need production facts, read them through audited access (auditor role or break-glass) and copy
only the minimum, masked.

## 2. Classify and set the severity

| Class | Signs | Path |
|---|---|---|
| Data entry / UX | A status or check explains it correctly | Explain the fix to the user; a confusing message is a UX bug: improve the text (ENG-23) or add a hint |
| **Calculation defect** | A wrong figure for valid input; engine ≠ oracle; INV-07/08 nightly failure | **P1 incident** (§4.3 target is 0 per quarter): fix, data correction, QA-REL-01 review |
| Validation / message | A wrong ✖/⚠, or a message that does not say how to fix it | Domain validators + a fixture that triggers it (QA-RPT-02) |
| API contract | Wrong status, shape or error code; breaking change | **fh-api-integrations**; `oasdiff` must stay green |
| Authorisation / tenancy | A user sees or does more than their role allows; cross-tenant data | **Security incident** (SEC-32): contain first, then fix; PDPA breach assessment if personal data was exposed (CMP-PD-05) |
| AI | An unverified number, a wrong proposal, an excluded field in output | **fh-ai-proposals**; add the case to the evals or the red-team suite |
| Integration | Bank feed, Peppol, payment provider, webhook | Check the partner contract mocks; replay through the dead-letter view |
| UI / accessibility | Layout, focus, contrast, keyboard, mode | **fh-design-system**; add a component or a11y test |
| Performance | Over NFR-PERF targets | Profile; check missing indexes (`organisation_id` first), N+1 calls, unvirtualised grids |

## 3. Symptom → first places to look

| Symptom | Check first |
|---|---|
| Invoice or bill still open after payment | Matching (FR-BNK-07): reference normalisation (NFKC, NBSP, case); payment posted to 1100/2010; the difference above tolerance (S$1 or 3% FC) |
| Off by 0.01 | `RND-GST-EXCL` per subtotal vs per line (an organisation setting); `ALLOC-EVEN` not applied cumulatively (PRD rev 0.8); half-even used instead of half-up |
| CPF off by S$1 | `RND-CPF` (EE rounded down, ER = total − EE); the AW ceiling year to date; the age band changes the month **after** the birthday; the pack version in force for the pay month |
| Report ≠ trial balance | Something bypassed the balance service (ENG-04); a report run "as at" a different ledger version (FR-RPT-08) |
| Cannot post / save in a period | The period is locked (INV-03): correct it in an open period, or reopen with dual approval (FR-GL-08) |
| 412 Precondition Failed | A stale `ETag`; someone else changed the record (FR-INT-09). The UI must show "changed by X" (FR-UX-19). |
| 409 / 422 on retry | The `Idempotency-Key` was reused with a different body (FR-INT-08) |
| 403 or empty lists for a valid user | Role scope; RLS tenant context not set in the transaction (§11.4); an expired external user (SEC-06) |
| AI figure marked "unverified" | Numeric grounding (AI-08): the tool did not return that number. Fix the tool or semantic layer, not the prompt. |
| A value "missing" in Basic mode | FR-UX-25: it must show as "Has advanced details"; if it is hidden silently, that is a bug |

## 4. Reproduce, then write the failing test first

1. `make dev`, then `make seed` (or `make reset`) for a clean sample company (arrives in P0).
2. Set the **business date** override (DEV-05) to the user's date. Never rely on today.
3. Shrink to the **smallest** data set that shows the bug: a fixture built with the sample
   generator (DEV-04), not a copy of user data.
4. Write the test at the lowest layer that shows the symptom, tagged `// req: <ID>`:

| Bug in | Test lives in (planned) |
|---|---|
| Posting rule, rounding, FX | `internal/engine/**/_test.go` + a differential scenario in `test/differential/` + the oracle |
| Database invariant or RLS | `test/invariants/` (attack as the application role) |
| Report figure | `test/reports/` with the `verify` helper (QA-RPT-01) |
| Validation message | `test/messages/` fixture (QA-RPT-02) |
| API | `test/contract/` |
| Web | `apps/web/**/*.test.tsx`, or `e2e/` for journeys |
| AI | `evals/` golden question, proposal eval or red-team case |

5. **Expected values come from the rule, worked by hand or from the oracle**, never from the
   current (possibly wrong) output. If the oracle agrees with the bug, the oracle has the same
   misunderstanding: fix it **from the PRD rule**, independently.
6. The test must fail before the fix and pass after it.

## 5. Fix and verify

- Fix the cause at the right layer, with the smallest change. A posting-rule change gets a **new
  rule version** and an independently updated oracle (**fh-accounting-domain**).
- Migrations already merged are never edited; add a new migration (expand and contract).
- A statutory value is fixed with a new pack version, not code (**fh-sg-statutory**).
- Run `make test`; for UI or journey bugs also `make test-e2e`.

## 6. Correct the data (never edit history)

Posted entries are immutable (INV-02) and nobody has standing production write access (SEC-03).
Corrections are **postings with approvals**:

| Situation | Correction |
|---|---|
| A document posted wrongly, period open | Edit the document: the engine posts a reversal plus a replacement (FR-GL-12); normal approvals apply |
| A wrong amount in a closed period | A correcting journal in the **current** open period with a reason that links the bug, **or** a dual-approved reopen (FR-GL-08) when the accountant requires restatement; record the before/after close snapshots (AUD-04) |
| Many organisations affected (P3) | A reviewed correction job that **proposes** approved journals per organisation under a named system principal (FR-AUT-02); each organisation approves; audited |
| A wrong statutory pack version used | Activate the corrected version; re-run affected pay runs or returns as new versions, with approvals |
| AI-executed proposal was wrong | Undo through AI-31 while the period is open; then fix the task type or prompt with evals |

Every correction is visible in the audit trail with the bug reference in its reason. If filed
returns (GST F5, CPF, AIS) are affected, tell the user: an F7 or a CPF adjustment may be needed,
and the accountant decides.

## 7. Root-cause review (QA-REL-01, calculation defects and incidents)

Write `docs/postmortems/YYYY-MM-DD-<slug>.md` within 5 working days: timeline; impact (which
organisations, periods, figures); root cause; **why the oracle, the properties and the invariants
did not catch it**, and what now does (a new property, invariant, fixture or check); the data
correction; follow-ups with owners. Blameless; facts only.

## 8. Ship

- Commit: `fix(<scope>): <what users see now>`, e.g.
  `fix(engine): settle payments whose reference contains a non-breaking space`.
  Scopes: ledger, engine, api, web, ui, ai, mcp, oauth, worker, executor, jobs, db, infra,
  deploy, contracts, oracle, packs, compliance, security, design, docs, prd, skills, ci, release,
  deps.
- Body: the cause in one or two sentences, and `Refs: <issue>`.
- **Upgrade notes:** a paragraph that release-please copies to the CHANGELOG (REL-03): what users
  or the accountant must do, and **which figures will differ** (e.g. "AR falls for invoices paid
  with a spaced reference; the September F5 is unchanged").
- release-please makes it a patch release. Never edit `VERSION` or `CHANGELOG.md` by hand.
- Security fixes: coordinate disclosure; keep exploit detail out of the public commit message.

## Definition of done

- [ ] Reproduced on synthetic data with a frozen business date; no production data copied.
- [ ] A failing test first, at the right layer, tagged `req:`; it now passes.
- [ ] Oracle and invariants extended so this class of defect is caught next time.
- [ ] Data corrected through approved postings only; filed returns flagged to the accountant.
- [ ] Postmortem written for calculation defects and incidents.
- [ ] `fix(scope)` commit with upgrade notes.
