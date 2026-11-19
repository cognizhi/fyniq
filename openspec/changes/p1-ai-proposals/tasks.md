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
  **Left:** there is no operation that attaches an existing stored file to another record (evidence is attached by an upload, or a claim's
  forwarded mail), so the command would need a new domain function and the evidence rules' own checks first; it follows the document
  inbox, whose captured files are the existing documents it attaches.

## 2. Undo, tier 3 and approval quality

- [x] 2.1 Undo's machinery (design D4): `proposal.Undoer`, `undoAIProposal`, the append-only record. Not tagged `AI-31`/`AI-91`: no real
  task type reverses anything yet (see 2.1a).
  As built (migration 000952): `POST /v1/ai/proposal-undos` runs as the person (the command's right, as to do it themselves) through the command's
  own `Undo`, in one transaction, once, only for an Executed proposal whose command implements `Undoer` (AI-UNDO-01 to 04), and keeps who, when,
  why and what was reversed in `ai_proposal_undo` (append-only, FY951), audited; the proposal stays Executed. No real task type implements
  `Undoer` yet: the drafts are not undoable by design, and the posting and master-data types add it as they are built (tests use a test-only
  undoable type). **Left:** the Undo button on the proposal review (web) and its guide text: nothing to undo until a real type implements `Undoer`, so the screen waits for the first.
- [ ] 2.1a Each posting or master-data task type's own `Undo`, with its attack test (a reversing entry, a restored master-data version); the
  requirements AI-31 and AI-91 are proved when the first of them is built.
- [x] 2.2 Tier 3 in the engine (design D5): `proposalSteps` gives a tier-3 request two steps; the engine already refuses a second signature by
  the same person and the requester's own above tier 1. Proved on a request built with two steps (no catalogue loads a tier-3 type, and none
  is added for the test). Tests `// req: AI-09`, `// req: AI-92`.
- [x] 2.3 Approval quality (design D6): `ai_proposal_view` records each person's first look at a review (when they open `getAIProposal`);
  `proposal.MeasureQuality` computes the month's metrics; `GET /v1/ai/approval-quality?month=` (Owner, DPO) and `POST /v1/ai/approval-reviews`
  (Owner, DPO, step-up, after the month, once, with the metrics as signed) keep the review as CTL-AI-07's evidence (control file running).
  Tests `// req: CTL-AI-07`, `// req: AI-92`. The report and the sign button are the Approval quality tab of the AI tasks page (Owner and DPO).

## 3. The other tier-1 types

- [ ] 3.1 `ar.draft_reminders`, `plan.save_scenario`, `rpt.draft_commentary` (design D2).
  **Left:** `rpt.draft_commentary` has no domain to draft into (board-pack commentary is not built); `plan.save_scenario` saves a simulated
  forecast whose inputs (a cash projection's assumptions) the proposal would have to carry in full; `ar.draft_reminders` waits for the
  sales reminder drafts to be a record a person approves. Each needs its record first.
- [ ] 3.2 `bank.categorise_lines`, `bank.match_payments`, `bank.rule_upsert` (design D7, D8).
  **Left:** these post to the ledger (bank categorisation and matching, Appendix B.1), so they widen the executor's ledger grants and need
  their undo (a reversing entry) with their attack tests; they are built together with the tier-2 posting types in a slice of their own.

## 4. Tier 2

- [ ] 4.1 `ct.update_contact`, `recur.create_template`, `plan.propose_budget` (design D8).
  **Left:** `ct.update_contact` needs a narrow executor update of the contact's allowed fields (the existing update writes every column, so
  the executor would need grants on all of them, bank-adjacent remittance email included) and its undo (the previous values); the other two
  need their domain commands checked against the executor's grants.
- [ ] 4.2 `gl.post_journal`, `ar.post_invoice`, `ap.post_bill` (design D2, D3, D7).
  **Left (the riskiest, last by design D2):** they post to the ledger as the approving person, so each widens the executor's ledger grants,
  carries its limit and its undo, and its attack tests; not started while the owner is away.

## 5. The finish

- [ ] 5.1 The threat model's additions, the guide for what each task type does, the knowledge base, `make evals`.

## Needs human review (owner, before any of this is built)

- **Tier-2 and bank task types** (`bank.categorise_lines`, `bank.match_payments`, `bank.rule_upsert`, `ct.update_contact`, `recur.create_template`,
  `plan.propose_budget`, `gl.post_journal`, `ar.post_invoice`, `ap.post_bill`) each need executor grants the executor does not hold. The tightest
  design, to review as one pull request labelled `needs-human-review`:
  - one migration per task type, granting the executor only the columns its one domain command writes, through a narrow command-specific query (never
    the API's all-column update: `ct.update_contact` would otherwise hold UPDATE on remittance email and tax residence);
  - no grant on `journal_entry` beyond what the shared posting function needs, and the posting types last, each with its limit in the file, its approver
    rule (AI-10) and an `Undoer` (a reversing entry, a restored master-data version) whose attack test proves the undo cannot be run by anyone without the
    command's right or on a closed period;
  - the never-list tests run over every new ID and command first, and `bank.rule_upsert` has no auto-apply field.
