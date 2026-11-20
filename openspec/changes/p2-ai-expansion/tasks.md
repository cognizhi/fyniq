# Tasks

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.549 records AI-33's built fields and AI-59's design `[owner to confirm]`. `// req: AI-33`, `// req: AI-59`

## 2. Approval-quality metrics (AI-33)
- [x] 2.1 `MeasureQuality` gains the edited and reversed counts and the per-task-type and per-approver breakdown (D1); the read query joins the undo. `// req: AI-33`
  Built: `internal/proposal/quality.go`, `AIQualityRows` with the undo's time. Test: `TestApprovalQualityCountsEditsAndReversalsPerTaskTypeAndApprover`.
- [x] 2.2 The Approval quality tab and the guide say so. `// req: AI-33`
  Built: `AITasks.tsx` (two rows and a table by task type), `docs/user-guide/ai-proposals.md` and its kb page.

## 3. kb.propose_page (AI-59)
- [ ] 3.1 The task type, its schema and `proposal.NoFigures` (D2). `// req: AI-59`
- [ ] 3.2 The executor's grant on `kb_note` and its attack test (needs-human-review). `// req: AI-59`, `// req: SEC-03`
- [ ] 3.3 Evals and the red-team case: a figure never becomes a page. `// req: AI-59`, `// req: CTL-AI-05`
