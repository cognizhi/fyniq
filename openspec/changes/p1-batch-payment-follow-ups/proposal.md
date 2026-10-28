# Proposal

## Why

`p1-payments-out` releases payment batches, matches each payment to its bank line by hand, and emails a supplier
its remittance advice when the match is made (FR-BNK-12). Two gaps showed once it was built:

- **Bank auto-reconcile skips batch payments.** FR-BNK-14 reconciles a line by itself to one open invoice or bill, a
  transfer or an approved rule, but a released batch's payment is already posted, so it is not an open document. Every
  batch payment waits for a person in the queue, and its supplier's advice waits with it.
- **A remittance advice that gave up stays failed.** Design D8 shows it as failed on the batch and queues nothing
  again, so a render or email outage leaves each affected supplier to be told by hand.

## What Changes

- **The owner's decisions of 29 Oct 2026, taken interactively (PRD-001 rev 0.160):**
  - **A batch's payment matches by itself (FR-BNK-14):** a line that names a released batch's number, whose amount and
    currency are those of exactly one unmatched payment of that batch, and that nothing else could be, is
    reconciled to that payment by the system principal, within the same limits (S$500 a line). A line for a whole
    batch's total is not matched by itself. The alternatives were also matching a batch's total, offering the payment
    as the queue's top suggestion only, and no change.
  - **Send again (FR-BNK-12):** a person who can prepare the batch sends a failed advice again: one new attempt,
    audited, and still at most one advice a payment. The alternatives were retrying by itself once a day for a week,
    and no resend.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None: the rev 0.160 scenarios are already in the main spec `banking/bank-transactions`, converted with the PRD
revision. This change builds them (`skip_specs: true`).

## Impact

- `internal/domain/banking` (auto-reconcile), `internal/remitmail`, the payment batch API and its screen, the guide's
  bank reconciliation and payment batch pages, and their kb pages.
- No migration is expected: the match and the outbox already record what these need. If the worker's fence on
  remittance advice must name a resend, that is a migration with an attack test.
