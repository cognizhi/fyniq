# Threat model: flux analysis and variance explanations

| | |
|---|---|
| Feature and requirement IDs | Flux analysis in the close: FR-GL-14, Appendix C.2 #57, `RND-PERCENT` (PRD-001 rev 0.63, the founders' decisions of 21 Oct 2026; built in rev 0.65) |
| Author, reviewer, date | Claude Code for the founders, 21 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Each income or expense account's totals for two months (internal, from the balance service); the explanations' words (internal); who wrote each explanation; no personal data beyond member IDs, no bank numbers or secrets |
| Who acts | The Accountant and the Controller (C on "Period close / reopen") write explanations; everyone with V on the row reads the variances; the API's database role `fyniq_api` |

## 1. What we are building

At the close, each income or expense account's total for the month is compared with its total for the previous month.
A variance above both thresholds (two organisation settings changed under FM-1) needs an explanation, and the close
checks refuse the close until each such variance has one that is of its totals as they are (Appendix C.2 #57). An
explanation records the two totals it explains; it is append-only, and once either total changes it is stale.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02); **API ↔ database** (`fyniq_api`, RLS per organisation).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: an explanation written for one set of figures is left standing after the figures change, so the attester reads an explanation of numbers that are no longer there | A month closed on a variance nobody explained | Medium |
| T2 | person ↔ API | Tampering: an explanation is filed against totals its writer never saw (a posting landed between reading and writing) | As T1 | Low |
| T3 | API ↔ database | Tampering: an explanation is rewritten or removed after the close, or written into a closed month | The close's record no longer says what the attester read | Low |
| T4 | person ↔ API | Repudiation: nobody can tell who explained a variance | No evidence for the auditor | Low |
| T5 | person ↔ API | Elevation of privilege: the AI or a member who does not prepare the close writes explanations | Explanations no preparer stands behind | Low |
| T6 | person ↔ API | Tampering: the thresholds are raised quietly so nothing needs explaining | Flux analysis that never asks | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | An explanation records the two totals; the close and the list compare them with the totals now, and a stale one counts as none (C.2 #57) | `TestFluxAnalysisInTheClose` (FR-GL-14) |
| T2 | The request names the totals its writer read, and the domain refuses others (CLOSE-10) | `TestFluxAnalysisInTheClose` |
| T3 | `fyniq_api` holds INSERT only; `period_variance_explanation_guard` refuses an update, a delete and a truncate even for the table's owner (`FY163`), and an explanation in a period that is not open, reading the period FOR SHARE so it never interleaves with the close, or of a balance-sheet account (`FY162`) | `TestAttackVarianceExplanationsAreAppendOnlyAndOnlyInAnOpenPeriod` |
| T4 | Every explanation is audited with its actor in the same transaction (AUD-01), and keeps its writer | `TestFluxAnalysisInTheClose`, `TestAttackVarianceExplanationsAreAppendOnlyAndOnlyInAnOpenPeriod` |
| T5 | Writing needs C on the row (the Accountant and the Controller); the AI column holds V at most, and the AI's draft waits for the AI catalogue (§9.6) | `TestAuthorisationMatrix` |
| T6 | Both thresholds are settings under dual control (FM-1): a change waits for a second approver, is effective-dated and audited | `TestTheCatalogueShipsEverySettingAtAValidRecommendedValue` and the settings' approval tests |

## 4. What remains

- The staleness rule is provisional (rev 0.65): any change to either total makes the explanation stale, even a cent.
  The founders confirm it at sign-off.
- The close checks record what they found when they ran; an explanation written afterwards counts only once the checks
  run again. A posting after the checks makes them stale anyway (FR-GL-07).
- The database does not recompute the totals an explanation names; the domain does, from the balance service, in the
  same transaction.
