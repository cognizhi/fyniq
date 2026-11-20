# Tasks

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.563 records AI-33's built fields and AI-59's design `[owner to confirm]`. `// req: AI-33`, `// req: AI-59`

## 2. Approval-quality metrics (AI-33)
- [x] 2.1 `MeasureQuality` gains the edited and reversed counts and the per-task-type and per-approver breakdown (D1); the read query joins the undo. `// req: AI-33`
  Built: `internal/proposal/quality.go`, `AIQualityRows` with the undo's time. Test: `TestApprovalQualityCountsEditsAndReversalsPerTaskTypeAndApprover`.
- [x] 2.2 The Approval quality tab and the guide say so. `// req: AI-33`
  Built: `AITasks.tsx` (two rows and a table by task type), `docs/user-guide/ai-proposals.md` and its kb page.

## 3. kb.propose_page (AI-59)
- [x] 3.1 The task type, its schema and `proposal.NoFigures` (D2). `// req: AI-59`
  Built: `kb.propose_page` v1 (tier 1; approver: the Owner, since an Administrator holds no right to sign an approval), `internal/proposal/kb_page.go`. Test: `TestAProposedPageHoldsNoFigure`.
- [x] 3.2 The executor's grant on `kb_note` and its attack test (needs-human-review). `// req: AI-59`, `// req: SEC-03`
  Built: migration 001105 (SELECT, one column-limited INSERT, and a trigger holding the executor to a general note under `proposed.`), listed in `docs/security/p2-grants-for-human-review.md`. Test: `TestAttackTheExecutorAddsAProposedNoteAndNothingMore`.
- [x] 3.3 Evals and the red-team case: a figure never becomes a page. `// req: AI-59`, `// req: CTL-AI-05`
  Built: `evals/kb-propose-page`, `evals/kb-propose-page-redteam` and the API test `TestTheAIProposesAKnowHowPageWithNoFigureAndAPersonApprovesIt`, whose red-team cases propose an amount, a percentage, a decimal, a long number and an ID.
