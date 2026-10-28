# Tasks

## 1. A batch's payment matches by itself (FR-BNK-14, rev 0.160)

- [ ] 1.1 Auto-reconcile matches a debit line to exactly one unmatched payment of a released batch the line names,
  as design D1 says, with the basis `batch_payment`, within the line cap, and leaves an ambiguous one in the queue.
  - Tests: matched and its advice queued; two payments of the same amount left ambiguous; a payment and an open bill
    left ambiguous; above the cap left; a batch's total left; another account's batch not matched; undone.
    `// req: FR-BNK-14`
- [ ] 1.2 The Auto-reconciled list names the basis ("A payment of PB-0001"), and the guide's bank reconciliation page
  says so, compiled into `kb/`.

## 2. Send again (FR-BNK-12, rev 0.160)

- [ ] 2.1 The API sends a failed advice again (design D2): refused for a payment whose advice is not failed, and for
  a caller who cannot prepare the batch; audited; the matrix row.
  - Tests: a dead job sent again sends once; sent, waiting and not-wanted refused; the worker's fence still holds.
    `// req: FR-BNK-12`
- [ ] 2.2 The batch's screen shows **Send again** beside a failed advice, with a reason; help key; the guide's payment
  batch page, compiled into `kb/`.

## 3. Finish

- [ ] 3.1 `make check`, `make test`; archive.
