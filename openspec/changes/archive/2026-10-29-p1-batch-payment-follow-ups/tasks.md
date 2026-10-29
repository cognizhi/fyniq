# Tasks

## 1. A batch's payment matches by itself (FR-BNK-14, rev 0.160)

- [x] 1.1 Auto-reconcile matches a debit line to exactly one unmatched payment of a released batch the line names,
  as design D1 says, with the basis `batch_payment`, within the line cap, and leaves an ambiguous one in the queue.
  - Tests: matched and its advice queued; two payments of the same amount left ambiguous; a payment and an open bill
    left ambiguous; above the cap left; a batch's total left; another account's batch not matched; undone.
    `// req: FR-BNK-14`
  (29 Oct 2026: `banking.decide` takes the money recorded already: one payment of a released batch whose number the
  line names (as written or normalised, as invoice numbers are) is matched with basis `exact` (design D1, revised);
  more than one, or one beside an exact open document, or any other recorded money, stays ambiguous. `AutoReconcile`
  now runs through the caller's `BankTransactions`, so its match queues the remittance advice as a person's does.
  Test: `TestABatchPaymentReconcilesByItself` (two payments of one amount, above the cap, a batch's total under and
  over the cap, another account's batch, lower case, the advice sent, undone), checked by breaking the match.)
- [x] 1.2 The Auto-reconciled list names the basis ("A payment of PB-0001"), and the guide's bank reconciliation page
  says so, compiled into `kb/`.
  (29 Oct 2026: `batch_number` on each auto-reconciliation; the web says "A payment of PB-0003, which the line
  names"; the guide's "What Fyniq reconciled by itself" and kb `banking.auto-reconciled`. Test: `rules.test.tsx`.)

## 2. Send again (FR-BNK-12, rev 0.160)

- [x] 2.1 The API sends a failed advice again (design D2): refused for a payment whose advice is not failed, and for
  a caller who cannot prepare the batch; audited; the matrix row.
  - Tests: a dead job sent again sends once; sent, waiting and not-wanted refused; the worker's fence still holds.
    `// req: FR-BNK-12`
  (29 Oct 2026: `sendRemittanceAgain`, `POST /v1/payment-batches/{batch_id}/payments/{payment_id}/remittance-again`
  with a reason, policy PaymentBatches Create (unqualified). `remitmail.Again` queues a job under a key of its own
  (`remittance:<payment>:again:<n>`), and the batch's state reads the latest job (`remitmail.Latest`), so the dead row
  stays as it was and no outbox state moves back. PB-24 for an advice not failed, or a voided payment; 404 for
  another batch's payment. Audited `remittance.sent_again` with the reason. Tests: `TestAFailedRemittanceIsSentAgain`,
  the matrix row.)
- [x] 2.2 The batch's screen shows **Send again** beside a failed advice, with a reason; help key; the guide's payment
  batch page, compiled into `kb/`.
  (29 Oct 2026: **Send again** beside "Remittance not sent" for whoever prepares batches, a dialog with the reason, and
  "Remittance being sent" while queued; help key `batch.remittance-again`; the guide's "Paid, and remittance advice"
  also says a batch's payment is matched by itself; kb `purchases.payment-batches`. Test: `paymentBatches.test.tsx`.)

## 3. Finish

- [x] 3.1 `make check`, `make test`; archive.
  (29 Oct 2026: green locally and in CI on #343, which merged.)
