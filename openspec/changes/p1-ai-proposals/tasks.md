## 1. Tier-1 drafts the capture and the chat need

- [x] 1.1 `ap.draft_bill` (DraftBill): the file, the command and its dry run, the shared bill-draft function, the
  executor's grants, the suite and a red-team case (design D1, D3). Tests `// req: ai/task-catalogue ap.draft_bill Draft a bill`,
  `// req: AI-11`, `// req: AI-30`.
  As built: `proposal.BillDraftOf` reads the payload (supplier by contact ID, accounts by code, purchase GST codes) and
  `journal.CreateBillIn` makes the draft; the dry run creates it in the rolled-back unit of work and shows the total, the input
  tax as decided, what stops posting and the warnings (a duplicate included). `document` (tax invoice, simplified, none) is
  the fact the document states and defaults to none: an unknown fact claims no input tax (G4-029). Migration-free: the
  executor already held what a bill needs. A catalogue lock entry is regenerated only before a file is released.
- [x] 1.2 `exp.draft_claim` (DraftClaim): own claims only. Tests `// req: ai/task-catalogue exp.draft_claim Draft an expense claim`.
  As built: the claimant is always the requester (`Actor.Requester`): the payload has no claimant, and the executor refuses an
  approval by anyone else. Migration 000950 lets the executor read the claim categories. **Found:** an Employee alone holds no
  approval right on the approvals gate (`(approvals)` A), so cannot sign their own proposal; a member who also holds an
  approval role (Approver, Owner) can. Reported to the owner (widening the gate is security posture). **Left:** the proposal
  suites and red-team cases for both (the eval harness's person is a founder, who may not enter bills); their task-type files
  are tested through the API suites.
- [x] 1.2a (design D3) `Actor.Requester`: the person who asked, beside the approver, for a command that acts for one person.
- [ ] 1.3 `doc.attach` (AttachDocument): an existing document to a record the person may attach to. Tests
  `// req: ai/task-catalogue doc.attach Attach a document to a record`.

## 2. Undo, tier 3 and approval quality

- [ ] 2.1 Undo (design D4): `proposal.Undoer`, `undoAIProposal`, `ai_proposal_undo`; the posting and master-data types implement it as they arrive, the drafts are not undoable. Tests
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
