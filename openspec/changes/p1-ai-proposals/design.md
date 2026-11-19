## Context

`p1-ai-proposals-core` (design D1 to D13) is the path: a catalogue of hash-pinned files, a proposal store under its guard,
an approval request signing the payload hash, the executor running the command as the approving person, expiry, the
evals' harness and the controls. Its commands are `proposal.Command` (policy, measure, dry run, execute), registered in
`proposal.Release`. `ar.draft_invoice` (one command, one file, one test, one migration for the executor's reads) is the
pattern for a task type. This change adds task types and the three pieces the core left (AI-09, AI-31, CTL-AI-07).
The owner is away: the decisions below are on the recommended option, owner to confirm (PRD-001 rev 0.370).

## Goals / Non-Goals

**Goals:**
- Every task type of Appendix E but `kb.propose_page` (P2), each closed by default, each checked against the never-list.
- Undo, tier 3's second approver and the approval-quality metrics, as the specs say.
- Built so a later task type is one more file and command.

**Non-Goals:**
- A tier-3 task type (reserved), `kb.propose_page` (P2), proposals from agents beyond the company secretary
  (`p1-scheduled-agents` owns them) and anything on the never-list.

## Decisions

**D1. The never-list runs first.**
- Each new ID and command name goes through `proposal.Refuse` before a file is written. If one trips (for example
  `ap.post_bill` for "post" being read as a payment), the finding is reported and the list is not loosened. Posting a bill
  is not a payment and not an approval: the human who approves the proposal approves the bill (Appendix E rev 0.11).
- A test asserts every Appendix E ID of this change is either accepted by `Refuse` or named in a report.

**D2. Order, so that each step is usable and the riskier last.**
1. Tier-1 drafts that the capture and the chat need: `ap.draft_bill`, `exp.draft_claim`, then `doc.attach`.
2. Undo, tier 3 and approval quality (the machinery that tier-2 posting needs before it exists).
3. The other tier-1 types: `ar.draft_reminders`, `plan.save_scenario`, `rpt.draft_commentary`, then the bank's three.
4. Tier 2 master data: `ct.update_contact`, `recur.create_template`, `plan.propose_budget`.
5. Tier 2 posting: `gl.post_journal`, `ar.post_invoice`, `ap.post_bill`. These widen the executor's ledger grants, so
   they are last, each with the grants of its own command and no more; any left unbuilt carries a "Left:" note.

**D3. One shared domain function per effect (AI-11).**
- A task type's command calls the same function the API calls for a person (for example `journal.CreateBillIn`, as
  `CreateInvoiceIn` serves `ar.draft_invoice`), so a draft made by the AI, by a capture and by hand are the same record
  with the same checks and the same audit, the AI as originator.
- The executor's role gains only the grants that function's tables need, per task type, in the migration of that task
  type (range 000950 to 000999).

**D4. Undo is a person's act through the domain's own reversal (AI-31, AI-91).**
- An optional interface `proposal.Undoer` (`Undo(ctx, u, execution, base, as)`), so no existing command or fake changes.
  A command that does not implement it is not undoable, and the catalogue says so.
- (As built: `POST /v1/ai/proposal-undos` with the proposal's ID in the body, because the contract lint ties a path under a record to its ETag.) The operation `undoAIProposal` runs as the person (never the executor), who holds the command's right; it refuses
  unless the proposal is Executed, the records are as it left them (the same base-version check as AI-P5) and, for
  ledger entries, the period is open. It runs in one transaction through the domain function a person would use: a draft
  is not undone (see below), an entry is reversed by a reversing entry, master data restored to its previous version.
- A draft the AI made (`ap.draft_bill`, `exp.draft_claim`, texts) is **not undoable**: it posts nothing, no document draft is ever
  deleted (the lifecycle is draft, posted, reversed), and a person edits or ignores it. The undoable task types are those that post
  or change master data: the bank's, the tier-2 ones.
- "Under the same approval policy as the original": the reversal functions carry their own policies (a void in Founder
  mode needs a second person, a posted bill's reversal needs approval where a person's would), so the undo needs the
  approval a person's own would. No new approval object type: two engines would drift (as core D4 decided).
- The record is `ai_proposal_undo`, append-only (who, when, why, what was reversed), audited; the proposal stays
  Executed. A task type whose effect cannot be reversed safely (a message queued to send) is not undoable.
- Alternative: a second `ai_proposal_undo` approval request. Rejected for the P1 scope: it doubles the lifecycle for an
  action each domain already guards; revisit if the owner wants a second approver on every undo.

**D5. Tier 3 is the engine's, not the catalogue's (AI-09, AI-92).**
- A tier-3 type still does not load (the catalogue refuses it, "reserved"). The approval engine's `ai_proposal` policy
  builder gains two steps when a request's tier is 3, each of the task type's approver roles, signed by two different
  people in every team mode, with the same person unable to sign both. A test-only tier-3 type in the engine's tests
  proves it; the release never loads one.

**D6. Approval quality is computed from the proposals (CTL-AI-07, AI-92).**
- A read model over `ai_proposal`, its approval request and the review's first view: counts by outcome, the share
  approved in under 10 seconds of the first view with no item changed, the median time to a decision, approvals per
  approver, the share rejected with a reason, each task type's share.
- The first view is recorded when the review is first opened (an append-only `ai_proposal_view`). The report is an API
  operation read by the Owner and the DPO; the monthly review is a record signed by one of them (a step-up), kept as
  evidence for CTL-AI-07.

**D7. Limits and approvers are Appendix E's, as defaults.**
- `bank.categorise_lines`: at most 100 lines, S$10,000 each, S$50,000 a proposal. `bank.match_payments`: at most 100
  matches, the same totals, settlement differences only within the B.1 tolerances. `gl.post_journal`: S$5,000 an entry,
  above it the Controller signs. `ar.post_invoice`: S$20,000. `ap.post_bill`: S$10,000. `recur.create_template`: S$5,000
  a month. These are the recommended defaults of FR-ORG-08 and stay data in the file.

**D8. Bank fields and auto-apply stay out.**
- `ct.update_contact`'s schema has no bank field and its command refuses a payload that carries one, with an attack test.
- `bank.rule_upsert` requires the rule's back-test result and cannot set auto-apply: the payload has no such field and
  the command refuses a rule that would apply itself.

**D9. Evals.**
- Each task type adds a `proposal` suite (cases in replay mode through the real orchestrator) and red-team cases; a
  suite's recordings are `[live-verify]` for real models.

## Risks / Trade-offs

- [Posting types widen the executor's grants] -> last, per type, never broader; their attack tests prove the executor
  cannot do what is not its task type's.
- [Undo of a posted effect] -> only by the domain's own reversal, only while the period is open and nothing moved after.
- [Many task types mean many approvals] -> bulk approval stays tier 1 only; approval quality is measured (D6).

## Migration Plan

Additive: files, commands, grants, two tables. Nothing is enabled (AI-32). Rolling back removes the files and commands;
proposals already made stay as records.

## Open Questions

- None for the owner beyond those tagged owner-to-confirm in rev 0.370: who signs the monthly review; whether an undo
  needs its own second approver.
