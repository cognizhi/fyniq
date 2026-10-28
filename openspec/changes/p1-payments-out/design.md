# Design

## Context

See proposal.md for why. What exists:
- Payments are recorded by hand after the bank has paid:
  - a bill payment (`billpayment.Draft`, `bill_payment@1`: Dr 2010 · Cr the money account, per bill at its stored rate);
  - a claim reimbursement (`claim.Reimbursement`, `claim_reimbursement@1`: Dr 2150 · Cr the money account).

  Both are recorded under Appendix D's "Payment batches" row (rev 0.148), are voided through the approval engine in
  Founder mode, and are matched by bank import as documents recorded by hand (`MatchRecorded`, FY228), including by
  the system principal's auto-reconcile (FR-BNK-14).
- A bill can carry a planned payment date (FR-AP-13), which the bills board reads and which a waiting bank-details
  change freezes (PAYEE-12).
- Vendors' bank accounts are sealed in `payee_bank_account` (SEC-11, FY237, FY238). Each has its last four characters
  and fingerprint; one is in force per vendor and currency, and the account in force is found with `payee.InForce`.
- A money account names its chart account, currency, institution and identifier (the account number).
- The approval engine rebuilds an envelope from its declared dependencies at execution and voids a request whose
  dependencies changed (FR-APR-05, FR-APR-10).
- The review log records self-approvals and high-risk actions for the weekly review (FM-3).
- The remittance advice template exists, and the render service renders it. Nothing sends it yet.

## Goals / Non-Goals

**Goals:**
- A batch whose approval covers exactly the payees' accounts that the file pays, so a changed account can never ride
  on an earlier approval (G4-055).
- One posting path: a batch posts through the rules that already post payments by hand, so the oracle, matching and
  voids stay as they are.
- Full account numbers exist in clear only while the file is written; the file is sealed at rest.

**Non-Goals:**
- Sending the file to the bank (P2, a bank API connector); FAST or GIRO status from the bank beyond the statement line.
- Foreign-currency or overseas payments; salaries; FM-8 break-glass (`p1-approval-engine` task 3.3).
- Splitting one bill across two batches: a bill is paid whole in one batch, for what is open on it.

## Decisions

### D1. A batch is a document; its release is a record of its own

A batch is a `payment_batch` document (PB-), unnumbered as a draft and numbered when approved, posting nothing itself.
- **Its content:**
  - the money account (SGD, at a Singapore bank);
  - the payment date;
  - the method (FAST or GIRO, D6);
  - its items, each a bill or an expense claim to pay whole.
- **Its life:**
  - draft: prepared, changed version by version;
  - approved: the document posted, content frozen, by the engine;
  - cancelled, before release only: reversed, with a reason, posting nothing (as `document_reversed_posted` allows
    for purchase orders);
  - released.
- **The release:** a `payment_batch_release` row, written once, never changed (a guard, FY239). It holds the
  releaser, the time, the file's name and SHA-256, the file sealed (D7), and the payments it posted (D5).

**Alternatives:** a batch as a plain table, like claim categories. Rejected: approvals, versions, numbering, the
audit history and the FR-DOC-06 evidence on release ("bank confirmation, recommended") all hang on documents.

### D2. Items, holds and what a batch may hold

Each item is one document. A batch's checks, ✖ blocking approval and release and ⚠ warning, are recomputed at every
read, ask and release:
- **The bill:**
  - it is posted, in SGD, has something open, and is not in another approved or released batch;
  - no payment of it is recorded by hand while it is in a batch (the reverse is refused too: recording a payment of a
    bill in an approved, unreleased batch is refused);
  - its supplier has an account in force in SGD at a Singapore bank, with no change waiting (PAYEE-12);
  - its withholding-tax decision is not pending (FR-AP-19).
- **The claim:**
  - it is approved, with something owed, and not already in another batch;
  - its claimant has an account in force (D9), with no change waiting.
- **The bill's planned date:** it sorts and suggests the items ("New batch for Fri 31 Oct" takes the bills planned
  on or before it). It never adds one by itself.

Items are paid per payee: one supplier's bills become one payment, one claimant's claims one reimbursement (D5).

### D3. Approval: the batch and its payees' accounts in one envelope

- **The object:** `payment_batch`, with `payment_batch.approve`, on Appendix D's "Payment batches" row (A: the
  Controller).
- **Its dependencies:** the batch's version and status, its total, and each payee's account in force, as payee →
  change ID.
- **Who approves:** the preparer never approves; the engine refuses the requester (FY101), and SOD-3's preparer check
  is applied as for bills.
- **On approval:** the batch is posted (approved) and numbered.
- **G4-055:** if a payee's account changes after the approval, the release refuses (D4). The approver of the change
  can then re-approve the batch with `payment_batch.reapprove`, which runs on an approved, unreleased batch, holds the
  new accounts in its envelope, and changes nothing but the approval the release checks.

**Alternatives:**
- cancelling and preparing the batch again: more work for the same control;
- holding the release until approval: the engine executes on the final approval, and the decided release is a
  separate step by the Payment releaser.

### D4. Release: who, and what is checked again

`releasePaymentBatch` needs:
- R on "Payment batches" (the Payment releaser);
- a person signed in who stepped up in the last 5 minutes;
- the batch's latest approval, whose payees' accounts must still be the ones in force (else PB-xx, "re-approval
  needed"). Every D2 check runs again in the release's transaction, with the batch, its bills and claims locked in
  ENG-47's order.

**Separation:**
- **Standard mode:** the releaser is neither the preparer nor an approver (SOD-2), and never someone who prepared or
  approved the change that put a payee's account in force (SOD-1). Both are refused.
- **Founder mode:** the same conflicts are allowed (the owner's decision: whoever prepared or approved the batch may
  release it). The batch was approved by someone other than its preparer, so one person never pays alone. Each
  conflict is written to the review log for the weekly review (FM-3).

The release is one transaction: the payments (D5), the release row with the sealed file (D7), and their audit
events. It is idempotent with Idempotency-Key, and a second release is refused by the release row's key.

### D5. Posting on release reuses the hand-recorded rules

For each supplier, a `bill_payment` is recorded with `BillPayments.RecordIn`:
- dated the batch's payment date, on its money account;
- settling each of the supplier's bills in the batch for what is open, at its stored rate;
- referenced "PB-0003".

For each claimant, a `claim_reimbursement` is recorded with `ClaimReimbursements.RecordIn` in the same way. The rules,
the matching, the voids and the oracle's cases are the ones already built, so no rule version is added (ENG-06).
`payment_batch_payment` links the batch to each payment document.

A payment the bank rejects is voided with its existing void, which needs a second person in Founder mode. The batch
shows it as rejected; its bills or claims are open again and can join another batch.

**Alternatives:**
- one entry for the whole batch: it would need a new rule version and oracle case, and per-supplier matching would
  be lost;
- a payments-in-transit account: rejected by the owner.

### D6. The file behind a writer: the payment list first

`internal/domain/paymentfile` defines a writer: from the batch, the debit account (the money account's identifier)
and each payee's name, bank, number and amount, it returns the file's bytes, name and media type, and the writer's
name and version, which the release row records.
- **The payment list writer** (`payment-list@1`, rev 0.156) writes a CSV (UTF-8, CRLF, RFC 4180 quoting): a header row,
  then one row per payment with the payee's name, bank, BIC, account number, amount (a plain decimal), currency, the
  payment date, the method and the reference (the batch's number and the payee's documents). The batch's count and
  total close it. A cell that a spreadsheet would read as a formula (starting `=`, `+`, `-`, `@`, tab or carriage
  return) is prefixed with `'`. It is tested against a golden file worked by hand.
- **The bank's own file** (DBS UFF, or another bank's) is a follow-up change once the bank is chosen. It is another
  writer behind the same interface, chosen per money account, with the bank's identifiers (such as IDEAL's company
  ID) as organisation settings under dual control and a golden file from the bank's specification.
- **The method** (FAST or GIRO) is chosen per batch, FAST by default, and shown on the list. [proposed: a bank's
  specification may put it per transaction.]

**Alternatives:** generating the file at approval. Rejected: the release is the decided step, and the file must show
the accounts in force at release.

### D7. The file holds full numbers, so it is sealed

- **Writing:** the payees' numbers are opened with `Cipher` only inside the release's transaction, written into the
  file, and the file is sealed (SEC-11) into the release row, bound to the batch (AAD). Its SHA-256 is computed over
  the clear file, so a downloaded copy can be checked against it.
- **Downloading:** `getPaymentBatchFile` opens it for a person holding R or A on "Payment batches", stepped up, and
  audits each download with the hash. Never cached, and never in logs or in a response other than the download.

**Alternatives:**
- the file in the evidence store in clear: refused, because it is full account numbers at rest;
- regenerating it on each download: the accounts in force can change after release, and the file must be the one
  released.

### D8. Paid, and remittance advice on the match

- **Paid:** a batch's payment is paid when bank import matches it to a statement line, by a person or by
  auto-reconcile. The batch is paid when all its payments are. This is read from the matches, never stored.
- **The email:** the transaction that matches a batch's bill payment enqueues `payments.remittance` in the outbox, keyed
  by the payment, so the email is sent once. The worker:
  1. renders the supplier's remittance template (the organisation's default, or the supplier's own, FR-TPL-05) to PDF
     through the render service;
  2. emails it from the purchases sender to the supplier's remittance email;
  3. records the send as other sent documents are (FR-AR-06's `document_send`).
- **No remittance email:** a supplier without one is shown ⚠ on the batch, and nothing is sent.
- **Claims:** nothing is sent; My claims shows the reimbursement as paid.

### D9. Claimants' bank details reuse the payee sealing

`payee_bank_account` gains a nullable `member_user_id`, and its `contact_id` becomes nullable: exactly one is set,
which a check validates in a later migration.
- **The change:** a new document type, `claimant_bank_change`, carries a person's change. The person makes their own
  only: the handler checks the caller is the claimant.
- **Approval:** the approval object `claimant_bank_change` on "Payment batches" (A) means someone who holds A, never
  the person, approves. FY237 and FY238 extend to it: one account in force per person and currency, retired only by
  another approved change.
- **Evidence:** the payee default of FR-DOC-06 applies, so a screenshot of the person's bank app, or a statement
  header showing their name and number, is required.
- **Reveal and freeze:** the same as for vendors. A person's waiting change freezes their claims out of batches.

**Alternatives:**
- a separate table: it duplicates the guards and the sealing;
- claimants as contacts: rejected by the owner.

### D10. Operations and rights

All operations are on "Payment batches" unless noted:
- **Batches:** list and read (V); prepare, change, ask for approval and cancel a draft (C); re-ask approval after a
  payee's change (C); release (R); download the file (R or A); attach the bank's confirmation (C on Evidence, under
  "Payment batches").
- **A person's own bank account:** read, prepare, ask, reveal and attach evidence. These are under the caller's own
  claims ("Expense claims", C own) and checked as theirs in the handler. Approval goes through the engine under
  "Payment batches" (A).

Each operation has its authorisation-matrix row. Writes take Idempotency-Key, If-Match and previews, as the
conventions require.

### D11. Audit events (AUD-01)

The document's own events cover a batch being created, changed, asked for, approved or re-approved, and cancelled.
Beyond those:
- `payment_batch.released`, with the file's hash and the payments;
- `payment_batch.file_downloaded`, with the hash;
- `document.remittance_sent`;
- the claimant bank change events, as for vendors (last four characters and fingerprint only, AUD-02).

## Risks / Trade-offs

- [A payment keyed wrongly from the list] → bank import matching leaves a payment whose amount or reference differs
  unmatched, and the batch stays unpaid until it is resolved; the guide asks for the bank's batch count and total to
  be compared with the list's before authorising in the bank. A wrong account number with the right amount is not
  caught by matching: the bank file, once built, removes the keying.
- [A name or reference opened in a spreadsheet runs as a formula] → cells that start like a formula are prefixed
  (D6).
- [A bill paid twice, by hand and by a batch] → recording a payment of a bill in an approved, unreleased batch is
  refused, and a batch refuses a bill with a payment recorded since it was added (D2).
- [An account diverted between approval and release] → the release checks the accounts against the latest approval
  (D3, D4), and a waiting change freezes the payee.
- [The downloaded list is edited] → the release shows its SHA-256, and each download is audited.
- [Founder mode lets the preparer release] → the approval by another person comes first, and the release is logged
  for the weekly review. This is the owner's decision.

## Migration Plan

Forward-only goose migrations, squawk-clean:
- the document type `payment_batch` and its sequence (PB-);
- `payment_batch_release` and `payment_batch_payment` with their guards (FY239, FY240), RLS and grants;
- `payee_bank_account`'s `member_user_id`, its contact made optional, and the exactly-one check added NOT VALID, then
  validated;
- the document type `claimant_bank_change`;
- the approval objects `payment_batch` (approve, reapprove) and `claimant_bank_change` (approve), added NOT VALID and
  then validated.

No backfill: no batch exists yet. A rollback is a new forward migration.

## Open Questions

- Which bank, and so which bank file: the owner's choice (rev 0.156); the follow-up change adds its writer without
  changing this design.
