# Threat model: the close checklist

| | |
|---|---|
| Feature and requirement IDs | The close checklist: FR-GL-07, FM-4, Appendix C.2 #58 (PRD-001 rev 0.68, the founders' decisions of 22 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 22 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The checklist template's tasks and each month's marks with their notes (internal); who saved and marked them; no personal data beyond member IDs, no bank numbers or secrets |
| Who acts | The Accountant and the Controller (C on "Period close / reopen") change the template and mark tasks; everyone with V on the row reads them; the API's database role `fyniq_api` |

## 1. What we are building

Each month's close has a checklist from the organisation's template. A task is done, or skipped for the month with a
reason; the close checks refuse the close while one is neither (Appendix C.2 #58). The template is saved as versions;
marks are an append-only log.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02); **API ↔ database** (`fyniq_api`, RLS per organisation).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: the preparer drops a task from the template so it never has to be done | A step of the close silently disappears | Medium |
| T2 | person ↔ API | Tampering: a task is skipped to get past the check | The close goes ahead without the step | Medium |
| T3 | API ↔ database | Tampering: a mark is rewritten or removed after the close, or written into a closed month | The record no longer says what was done when the month closed | Low |
| T4 | person ↔ API | Repudiation: nobody can tell who marked a task or changed the template | No evidence for the auditor | Low |
| T5 | person ↔ API | Elevation of privilege: the AI or a member who does not prepare the close marks tasks | Marks no preparer stands behind | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Every template change is a new version, audited with its tasks; the close check names the version it used. The founders accepted that the preparer may change the template (22 Oct 2026) | `TestTheCloseChecklist` (FR-GL-07) |
| T2 | A skip needs a reason, which the API and the database both require; the attester, who did not run the checks (FM-4), reads it | `TestTheCloseChecklist`, `TestAttackTheCloseChecklistIsAppendOnlyAndOnlyInAnOpenMonth` |
| T3 | `fyniq_api` holds INSERT only; `close_checklist_guard` refuses an update, a delete and a truncate even for the tables' owner (`FY165`), and a mark in a month that is not open, reading it FOR SHARE (`FY164`) | `TestAttackTheCloseChecklistIsAppendOnlyAndOnlyInAnOpenMonth` |
| T4 | Every mark and template version is audited with its actor in the same transaction (AUD-01) | `TestTheCloseChecklist` |
| T5 | Marking and changing need C on the row; the AI holds V at most | `TestAuthorisationMatrix` |

## 4. What remains

- The preparer can weaken the template (T1); the founders chose this over a second approval. Revisit before a finance
  hire joins (Standard mode).
- A task's evidence is its note until the evidence rules of FR-DOC-06 arrive.
