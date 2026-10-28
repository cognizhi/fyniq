# Tasks

## 1. Claimants' bank details (FR-AP-22)

- [x] 1.1 Migrations (design D9):
  - `payee_bank_account` gains `member_user_id`, `contact_id` becomes optional, and the exactly-one check is added
    NOT VALID, then validated;
  - the document type `claimant_bank_change`;
  - FY237 and FY238 extended to a person's change (one in force per person and currency, retired only by another
    approved change, approved only by someone other than the person);
  - the approval object `claimant_bank_change` with `claimant_bank_change.approve`;
  - squawk clean; goldens updated.
  - Tests: a person's account in force without approval refused; a second in force refused; the person signing their
    own refused, for every role (QA-ENG-05). `// req: FR-AP-22`
- [x] 1.2 `internal/domain/payee` takes a person as payee: a Singapore account only, sealed, shown by its last four
  characters; the change's checks and evidence (FR-DOC-06's payee default); the freeze of their claims while a change
  waits.
  - Tests: an overseas account refused; a waiting change freezes the person; the number never in a response, log or
    audit event (AUD-02). `// req: FR-AP-22`, `// req: SEC-11`
- [x] 1.3 The API: the caller's own bank account (read, prepare, change, ask, reveal, evidence), approved through the
  engine under "Payment batches" (A), never by the person; the matrix rows; `make openapi` first.
  - Added in building: `withdrawMyBankChange`, since a person reaches their change's request only through it (as a
    claimant their claim's), never through the approvals; `getClaimantBankChange`, the approver's read; a reimbursement
    recorded by hand while a change waits is flagged ⚠ (PAYEE-12), as a vendor's payment is.
  - Tests: a person cannot read or change another's; the Controller approves and the account comes into force; the
    person's approval refused (FM-1). `// req: FR-AP-22`, `// req: FM-1`
- [x] 1.4 The bank account card on `profile-preferences` (`docs/design/screens/src/ProfilePreferences.html`, updated):
  masked with Show number, the waiting change with Withdraw, Change my bank account with the evidence panel. Help
  keys; the guide page `settings.profile`, compiled into `kb/`.
  - Built with the guide page `preferences.security` (the profile page's own) and a section of `approvals.decide` for
    the approver; the approval page shows a person's change with its screenshot and number.
  - Tests: Vitest with axe on the card's states. `// req: FR-AP-22`

## 2. Payment batches

- [x] 2.1 Migrations (design D1):
  - the document type `payment_batch` and its sequence (PB-);
  - `payment_batch_release`, one per batch, never changed or deleted (FY239);
  - `payment_batch_payment`, linking a release to the payments it posted, each a posted bill payment or reimbursement
    of the batch's money account and date (FY240);
  - the approval object `payment_batch` with `payment_batch.approve` and `payment_batch.reapprove`;
  - recording a payment of a bill in an approved, unreleased batch refused;
  - RLS, grants, squawk, goldens.
  - Added in building: a batch is approved, by posting, only under an approved request that neither its requester nor
    its preparer signed (FY242), as FY237 does for bank details; a second release is refused by the guard (FY239).
  - Tests: a second release refused; a release row changed or deleted refused; a link to another account's payment
    refused (QA-ENG-05). `// req: FR-BNK-12`
- [x] 2.2 `internal/domain/paymentbatch`: the batch's content by version, its items and the checks of design D2
  (posted, SGD, open, not in another batch, the payee's account in force at a Singapore bank and no change waiting,
  no withholding-tax decision pending, nothing recorded by hand since), every problem at once (ENG-23); the batch
  suggested from bills planned on or before its date (FR-AP-13).
  - Tests: each hold triggered by a fixture (QA-RPT-02); a supplier's three bills as one payment. `// req: FR-BNK-12`,
    `// req: FR-AP-13`, `// req: FR-AP-19`
- [x] 2.3 Approval (design D3): the batch's envelope holds its version, status, total and each payee's account in
  force; approval posts and numbers it; `payment_batch.reapprove` after a payee's account changed.
  - Tests G4-055: a batch approved, then a vendor's bank change approved, refuses release until re-approved, and the
    re-approval shows the new account. `// req: FR-APR-05`, `// req: FR-APR-10`, `// req: G4-055`
- [x] 2.4 Release (design D4, D5):
  - who may release: R on "Payment batches", a person, stepped up; SOD-2 and SOD-1 refused in Standard mode and
    logged in Founder mode (FM-3);
  - every check again, under ENG-47's locks;
  - one bill payment per supplier and one reimbursement per claimant, through `RecordIn`, dated the payment date and
    referenced with the batch's number;
  - the release row and its audit event, idempotent;
  - cancelling a draft or an approved, unreleased batch, with a reason.
  - Built: only an approved, unreleased batch is cancelled; a draft cannot go to cancelled (a document moves draft →
    approved → reversed), so it is changed or left. A batch names the chart's money account, as a payment recorded by
    hand does, and Banking's account for it gives its kind and identifier.
  - Tests:
    - the entries are those of recording by hand, to the cent, and the bills and claims show paid;
    - a second release refused;
    - in Standard mode the preparer's release refused (SOD-2);
    - in Founder mode the approver's release is logged.

    `// req: FR-BNK-12`, `// req: SOD-1`, `// req: SOD-2`, `// req: FM-1`, `// req: FM-3`
- [x] 2.5 The file (design D6, D7):
  - `internal/domain/paymentfile` with its writer interface;
  - the release seals the file into its row (SEC-11), with the writer's name and version and the SHA-256 of the
    clear file;
  - `getPaymentBatchFile` for R, stepped up, each download audited (built for R only: one policy names one right; the
    approver compares the accounts and total in the approval instead).
  - Tests: the file is opened only for the release's batch; a download without step-up refused; the audit trail and
    the log hold no number. `// req: FR-BNK-12`, `// req: SEC-11`, `// req: AUD-02`
- [x] 2.6 The payment list writer, `payment-list@1` (design D6, rev 0.156): the CSV's header, one row per payment, the
  count and total; formula-like cells prefixed.
  - Tests: a golden file worked by hand, to the byte; a payee named `=HYPERLINK(…)` written inert; the total equals
    the batch's. `// req: FR-BNK-12`
- [x] 2.7 Paid and remittance (design D8):
  - a batch's payment is paid when bank import matches it, by a person or by auto-reconcile, and the batch is paid
    when all its payments are;
  - the match enqueues `payments.remittance` (keyed by the payment): the worker renders the supplier's remittance
    template to PDF, emails it to the remittance address, and records the send;
  - a supplier without one is ⚠.
  - Built: matched by a person (match-recorded); auto-reconcile leaves a payment recorded by hand, or by a batch, to a
    person, as before. The advice goes from the invoices sender (the accounts address) until a purchases sender exists.
    Not tested: a payment voided between its match and the worker's run, which the worker skips.
  - Tests: one email per payment however often the match is undone and redone; no email for a rejected (voided)
    payment; claims send nothing. `// req: FR-BNK-12`, `// req: FR-AP-13`

## 3. Screens and docs

- [x] 3.1 `payment-batches` and `payment-batch` as designed (`PaymentBatches.html`, `PaymentBatch.html`, updated for
  rev 0.154):
  - the list and the record;
  - New batch from planned bills and approved claims;
  - the checks and holds;
  - approval and re-approval, release with step-up, the file's download and hash, cancel;
  - paid, and the remittance state per supplier.

  Basic and Expert tags, help keys, the guide page `purchases.payment-batches` compiled into `kb/`, the threat model
  `docs/security/threat-models/payment-batches.md`, and the approval words for `payment_batch` and
  `claimant_bank_change`.
  - Tests: Vitest with axe on each state, including release blocked after a payee's change. `// req: FR-BNK-12`
  - Built: the list and the record as Bills › Batches; a new batch starts from the suggestion for its date, and the
    record removes items; the approval words for `payment_batch`. The batch's ledger-impact, evidence and activity tabs
    of the design wait (the payments' own pages show their entries); the API adds `reapproval_needed` and
    `money_account_name` for the screens.
- [x] 3.2 Check the screens on the dev stack in a browser: prepare, approve as the other founder, release, download,
  match a statement line and see the remittance in Mailpit.
  - Checked on 28 Oct 2026 with two founders on a local stack: two batches prepared from planned bills, asked for,
    approved by the other founder, released by the preparer (Founder mode), the payment list read with its hash,
    statement lines imported and matched, each batch paid, and the remittance advice received in Mailpit with its PDF.
  - Found and fixed on the way: the worker could not keep or send remittance advice under its own database role
    (migration 000189 fences it to a matched batch payment; the test now runs as the worker); a money account on a
    chart account with no currency of its own was refused (#319); a new batch for a day with nothing due sent an
    empty batch; "1 payments" and an ISO date in the approval and the list; the record's breadcrumb; Categorise
    called a batch's payment "recorded by hand"; a remittance that gave up showed "queued" for ever (now
    **Remittance not sent**).
  - Seen, not changed here: a person invited to an organisation has no screen to redeem the invitation (sign-in
    offers only the Super admin invitation); a remittance job that dies is never retried (no dead-letter replay).
- [x] 3.3 Finish with `make check` and `make test`; `make test-e2e` for the batch journey.
  - 28 Oct 2026: `make test` and the non-local `go test ./...` pass; `make test-e2e` runs the new journey
    `e2e/payment-batches.spec.ts` (new batch, ask, the other founder approves, release, download) with the 16 others.

## Workflow follow-up

- The bank's own file (DBS UFF Version 2.2, or the chosen bank's) is a follow-up change once the owner chooses the
  bank (rev 0.156): its writer, its identifiers as settings, a golden file from the bank's specification, and a test
  upload [verify].
- The co-founder review (#6) of money movement before production (AGENTS.md).
