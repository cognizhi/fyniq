# Design

## D1. The metrics (AI-33)

The rows the month's report reads already carry the decision and its payload hashes; the approval's edit is "approved whole" turned round
(`approved_sha256` differs from `payload_sha256`), and an undo is a row of `ai_proposal_undo`. Quality gains, from the same rows:

- `edited`, `edited_share`: approvals where an item was changed or removed;
- `reversed`, `reversed_share`: approved proposals a person undid afterwards (AI-91);
- per task type: `approved`, `edited`, `reversed`, `median_decision_seconds`;
- per approver and task type: `approved`, `quick`, `edited`, `reversed`.

Shares are display figures between 0 and 1 (the existing ones), never an input to a rule. Nothing is written: the report stays computed.

## D2. kb.propose_page (AI-59), recorded decisions [owner to confirm]

- **A page is a note**: the proposal creates a knowledge-base note (the existing `kb_note` record), which the compiler or an Owner or
  Administrator turns into a page through the existing publication step; the AI never publishes a page.
- **Without figures**: the proposal's text is refused when it holds an amount of money, a percentage, an ID, an account number or a date outside
  prose (a deterministic check, `proposal.NoFigures`), and the evals include a conversation whose answer carries the organisation's numbers, which the
  task type must not turn into a page (the red-team case).
- **Tier 1**: it is approved by an Owner or Administrator, one person; the undo withdraws the note while nobody has edited it.
- **The executor's grant** is one column-limited INSERT on `kb_note`, listed in the needs-human-review table; no update, no delete, no grant on pages.

## D3. What waits

`kb.propose_page` is not built in this change's first slice: it adds a new write path from the AI to the organisation's knowledge, which the
fyniq-ai-proposals skill puts through its own review. D2 records the design the owner is asked to confirm.
