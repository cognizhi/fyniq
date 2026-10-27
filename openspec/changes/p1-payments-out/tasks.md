# Tasks

## 1. Claimants' bank details (FR-AP-22)

- [ ] 1.1 Migrations (design D9):
  - `payee_bank_account` gains `member_user_id`, `contact_id` becomes optional, and the exactly-one check is added
    NOT VALID, then validated;
  - the document type `claimant_bank_change`;
  - FY237 and FY238 extended to a person's change (one in force per person and currency, retired only by another
    approved change, approved only by someone other than the person);
  - the approval object `claimant_bank_change` with `claimant_bank_change.approve`;
  - squawk clean; goldens updated.
  - Tests: a person's account in force without approval refused; a second in force refused; the person signing their
    own refused, for every role (QA-ENG-05). `// req: FR-AP-22`
- [ ] 1.2 `internal/domain/payee` takes a person as payee: a Singapore account only, sealed, shown by its last four
  characters; the change's checks and evidence (FR-DOC-06's payee default); the freeze of their claims while a change
  waits.
  - Tests: an overseas account refused; a waiting change freezes the person; the number never in a response, log or
    audit event (AUD-02). `// req: FR-AP-22`, `// req: SEC-11`
- [ ] 1.3 The API: the caller's own bank account (read, prepare, change, ask, reveal, evidence), approved through the
  engine under "Payment batches" (A), never by the person; the matrix rows; `make openapi` first.
  - Tests: a person cannot read or change another's; the Controller approves and the account comes into force; the
    person's approval refused (FM-1). `// req: FR-AP-22`, `// req: FM-1`
- [ ] 1.4 The bank account card on `profile-preferences` (`docs/design/screens/src/ProfilePreferences.html`, updated):
  masked with Show number, the waiting change with Withdraw, Change my bank account with the evidence panel. Help
  keys; the guide page `settings.profile`, compiled into `kb/`.
  - Tests: Vitest with axe on the card's states. `// req: FR-AP-22`

## 2. Payment batches

- [ ] 2.1 Migrations (design D1):
  - the document type `payment_batch` and its sequence (PB-);
  - `payment_batch_release`, one per batch, never changed or deleted (FY239);
  - `payment_batch_payment`, linking a release to the payments it posted, each a posted bill payment or reimbursement
    of the batch's money account and date (FY240);
  - the approval object `payment_batch` with `payment_batch.approve` and `payment_batch.reapprove`;
  - recording a payment of a bill in an approved, unreleased batch refused;
  - RLS, grants, squawk, goldens.
  - Tests: a second release refused; a release row changed or deleted refused; a link to another account's payment
    refused (QA-ENG-05). `// req: FR-BNK-12`
- [ ] 2.2 `internal/domain/paymentbatch`: the batch's content by version, its items and the checks of design D2
  (posted, SGD, open, not in another batch, the payee's account in force at a Singapore bank and no change waiting,
  no withholding-tax decision pending, nothing recorded by hand since), every problem at once (ENG-23); the batch
  suggested from bills planned on or before its date (FR-AP-13).
  - Tests: each hold triggered by a fixture (QA-RPT-02); a supplier's three bills as one payment. `// req: FR-BNK-12`,
    `// req: FR-AP-13`, `// req: FR-AP-19`
- [ ] 2.3 Approval (design D3): the batch's envelope holds its version, status, total and each payee's account in
  force; approval posts and numbers it; `payment_batch.reapprove` after a payee's account changed.
  - Tests G4-055: a batch approved, then a vendor's bank change approved, refuses release until re-approved, and the
    re-approval shows the new account. `// req: FR-APR-05`, `// req: FR-APR-10`, `// req: G4-055`
- [ ] 2.4 Release (design D4, D5):
  - who may release: R on "Payment batches", a person, stepped up; SOD-2 and SOD-1 refused in Standard mode and
    logged in Founder mode (FM-3);
  - every check again, under ENG-47's locks;
  - one bill payment per supplier and one reimbursement per claimant, through `RecordIn`, dated the payment date and
    referenced with the batch's number;
  - the release row and its audit event, idempotent;
  - cancelling a draft or an approved, unreleased batch, with a reason.
  - Tests:
    - the entries are those of recording by hand, to the cent, and the bills and claims show paid;
    - a second release refused;
    - in Standard mode the preparer's release refused (SOD-2);
    - in Founder mode the approver's release is logged.

    `// req: FR-BNK-12`, `// req: SOD-1`, `// req: SOD-2`, `// req: FM-1`, `// req: FM-3`
- [ ] 2.5 The file (design D6, D7):
  - `internal/domain/paymentfile` with its writer interface and a test writer;
  - the release seals the file into its row (SEC-11), with the SHA-256 of the clear file;
  - `getPaymentBatchFile` for R or A, stepped up, each download audited.
  - Tests: the file is opened only for the release's batch; a download without step-up refused; the audit trail and
    the log hold no number. `// req: FR-BNK-12`, `// req: SEC-11`, `// req: AUD-02`
- [ ] 2.6 **(waits for DBS's specification, which the owner shares into `docs/prd/research/`)** The DBS UFF writer for
  FAST and GIRO, from "DBS UFF Version 1.9.3":
  - the header, transaction and trailer records, field lengths, character set, value date and hash total;
  - the organisation's IDEAL identifiers as settings, with no recommended value, under dual control;
  - a golden file worked by hand from the specification.
  - Tests: the golden file to the byte; a name with characters outside DBS's set refused, with the field named
    (ENG-23). `// req: FR-BNK-12` [verify with a test upload to IDEAL before go-live]
- [ ] 2.7 Paid and remittance (design D8):
  - a batch's payment is paid when bank import matches it, by a person or by auto-reconcile, and the batch is paid
    when all its payments are;
  - the match enqueues `payments.remittance` (keyed by the payment): the worker renders the supplier's remittance
    template to PDF, emails it to the remittance address, and records the send;
  - a supplier without one is ⚠.
  - Tests: one email per payment however often the match is undone and redone; no email for a rejected (voided)
    payment; claims send nothing. `// req: FR-BNK-12`, `// req: FR-AP-13`

## 3. Screens and docs

- [ ] 3.1 `payment-batches` and `payment-batch` as designed (`PaymentBatches.html`, `PaymentBatch.html`, updated for
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
- [ ] 3.2 Check the screens on the dev stack in a browser: prepare, approve as the other founder, release, download,
  match a statement line and see the remittance in Mailpit.
- [ ] 3.3 Finish with `make check` and `make test`; `make test-e2e` for the batch journey.

## Workflow follow-up

- Archive once every task but 2.6 is done and 2.6 has its specification, or archive with 2.6 carried to a follow-up
  change if the specification is still awaited at go-live planning.
- The co-founder review (#6) of money movement before production (AGENTS.md).
