## Why

`p1-ai-proposals-core` built the one path by which an AI changes anything, and the company secretary's task types.
`p1-ai-proposals` is the rest of the plan's row: the other task types of Appendix E, two approvers for tier 3, undo and
the approval-quality metrics. The owner set it after go-live; it is built now, locally, while the owner is away until
21 Nov 2026, on the recommended option (PRD-001 rev 0.370, owner to confirm). Every task type is a file, a domain command,
its dry run, its summary, its suite and its red-team cases, added the way `ar.draft_invoice` was.

## What Changes

- **The task types of Appendix E**, by tier and in the order they are built (design D2):
  - tier 1, drafts and text: `ap.draft_bill`, `exp.draft_claim`, `doc.attach`, `ar.draft_reminders`,
    `plan.save_scenario`, `rpt.draft_commentary`; then the bank's `bank.categorise_lines`, `bank.match_payments`,
    `bank.rule_upsert`;
  - tier 2, posting and master data: `ct.update_contact`, `recur.create_template`, `plan.propose_budget`,
    `gl.post_journal`, `ar.post_invoice`, `ap.post_bill`.
  `kb.propose_page` stays P2 (`p2-ai-expansion`).
- **Undo (AI-31, AI-91)**: a person undoes an executed proposal in one action through the domain's own reversal, in an
  append-only record, audited.
- **Tier 3 (AI-09, AI-92)**: the engine's two-step request with two different signers, proved with a test-only type; no
  tier-3 type loads in a release.
- **Approval quality (CTL-AI-07, AI-92)**: the month's metrics from the proposals' own records and the signed monthly
  review kept as evidence.
- **Evals and the threat model**: each task type's golden, proposal and red-team cases in replay mode; the threat model
  `docs/security/threat-models/ai-proposals.md` grows with each.

## Capabilities

### New Capabilities

None: the requirements are in the main specs (`ai/proposals`, `ai/task-catalogue`,
`compliance/ai-and-api-controls`). This change builds them (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Requirements built** (PRD-001 §9.5, §9.6, Appendix E, Appendix F.7; rev 0.370): AI-09, AI-31, AI-91, AI-92, CTL-AI-07
  and each Appendix E task type's rows in `ai/task-catalogue`.
- **Code**: `contracts/ai/tasks/` files and `tasks.lock.json`; `internal/proposal` commands; the shared domain functions
  they and the API both call; migrations 000950 to 000999 for the executor's grants, the undo record and the metrics.
- **Security boundary**: this is the AI write path (golden rule 5). The never-list is run over every new ID and command
  name first; `ct.update_contact` carries no bank field and `bank.rule_upsert` never sets auto-apply, each with an attack
  test; the executor's ledger grants widen only with the tier-2 posting types, last.
- **Depends on** `p1-ai-proposals-core`. **Needed by** `p1-mcp-first-party`'s proposal tools and `p2-ai-expansion`.
