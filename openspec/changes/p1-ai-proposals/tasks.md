## 1. Tier-1 drafts the capture and the chat need

- [ ] 1.1 `ap.draft_bill` (DraftBill): the file, the command and its dry run, the shared bill-draft function, the
  executor's grants, the suite and a red-team case (design D1, D3). Tests `// req: ai/task-catalogue ap.draft_bill Draft a bill`,
  `// req: AI-11`, `// req: AI-30`.
- [ ] 1.2 `exp.draft_claim` (DraftClaim): own claims only. Tests `// req: ai/task-catalogue exp.draft_claim Draft an expense claim`.
- [ ] 1.3 `doc.attach` (AttachDocument): an existing document to a record the person may attach to. Tests
  `// req: ai/task-catalogue doc.attach Attach a document to a record`.

## 2. Undo, tier 3 and approval quality

- [ ] 2.1 Undo (design D4): `proposal.Undoer`, `undoAIProposal`, `ai_proposal_undo`; the draft types implement it. Tests
  `// req: AI-31`, `// req: AI-91`.
- [ ] 2.2 Tier 3 in the engine (design D5): two steps, two different people, with a test-only type. Tests `// req: AI-09`,
  `// req: AI-92`.
- [ ] 2.3 Approval quality (design D6): the first-view record, the month's report, the signed review. Tests
  `// req: CTL-AI-07`, `// req: AI-92`.

## 3. The other tier-1 types

- [ ] 3.1 `ar.draft_reminders`, `plan.save_scenario`, `rpt.draft_commentary` (design D2).
- [ ] 3.2 `bank.categorise_lines`, `bank.match_payments`, `bank.rule_upsert` (design D7, D8).

## 4. Tier 2

- [ ] 4.1 `ct.update_contact`, `recur.create_template`, `plan.propose_budget` (design D8).
- [ ] 4.2 `gl.post_journal`, `ar.post_invoice`, `ap.post_bill` (design D2, D3, D7).

## 5. The finish

- [ ] 5.1 The threat model's additions, the guide for what each task type does, the knowledge base, `make evals`.
