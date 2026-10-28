# Proposal

## Why

Today every supplier payment and every claim reimbursement is made in the bank and then recorded by hand in Fyniq
(FR-AP-02, FR-AP-07, rev 0.147). That is the step where a wrong account, a duplicate or a diverted payment slips
through. Payment files joined the launch subset on 26 Oct 2026 (PRD-001 rev 0.146), and their dependencies have
landed:
- bank import and matching (`p1-bank-import-reconcile`);
- the approval engine;
- vendors' sealed, dual-controlled bank details (FR-CT-03, rev 0.153; `p1-organisation-chart` task 4.2).

What is missing is the batch itself: approved twice, released once, written as the bank's file, and posted and
matched without retyping.

## What Changes

- **The owner's decisions of 27 and 28 Oct 2026, taken interactively (PRD-001 rev 0.154, rev 0.156):**
  - **the file:** the bank is not chosen yet and there is no IDEAL account (rev 0.156), so releasing a batch gives a
    **payment list**: a CSV of each payment's payee, bank and account, amount, date and reference, sealed and
    downloaded like a bank file, and keyed into the bank. The bank's own file follows in its own change once the bank
    is chosen; for DBS, the UFF of "DBS UFF Version 2.2". This supersedes rev 0.154's DBS UFF first;
  - **what a batch pays:**
    - SGD from an SGD money account, to Singapore bank accounts, by FAST or GIRO;
    - approved bills (with their planned dates, FR-AP-13) and approved expense claims (FR-AP-07);
    - not yet: overseas and foreign-currency payments, which stay recorded by hand, and salaries, which join with
      `p1-payroll-link`;
  - **posting:** releasing posts each payment as recording one by hand does today (Dr 2010 or 2150 · Cr the money
    account) on the batch's payment date. Bank import matches it, and a payment the bank rejects is voided;
  - **who releases:**
    - in Founder mode, the founder who prepared the batch or the one who approved it (FM-1), after step-up, holding
      the Payment releaser role. Never one person alone; the SOD-2 conflict is logged for the weekly review (FM-3);
    - in Standard mode, a third person (SOD-2);
  - **remittance advice:** emailed to the supplier's remittance address when its payment is matched to the bank
    statement line;
  - **claimants' bank details (new FR-AP-22):** each person enters their own Singapore account. It is sealed
    (SEC-11), and someone who holds A on "Payment batches", never the person, approves each change. Payments to the
    person are frozen until then.
- **Payment batches (FR-BNK-12):**
  - prepare a batch for one SGD money account and payment date from approved bills and claims;
  - each item carries its payee's account in force, with holds that stop it:
    - a bank-details change waiting;
    - no approved account;
    - a withholding-tax decision pending (FR-AP-19);
    - an item no longer open;
  - the batch is approved through the engine by someone who holds A, its envelope holding each payee's account in
    force (FR-APR-05, FR-APR-10, G4-055);
  - it is released by the Payment releaser as decided above. Releasing posts the payments, writes the payment list
    (kept with its SHA-256, downloaded on request, each download audited) and marks the bills and claims paid in a
    batch;
  - it is paid when bank import matches its lines.
- **Claimants' bank details (FR-AP-22):** under My profile. The same sealing, masking, reveal and approval as vendors'
  (FR-CT-03), approved under "Payment batches".
- **Remittance advice:** rendered from the organisation's remittance template and emailed through the outbox when a
  batch's supplier payment matches its bank line; a supplier without a remittance email is shown ⚠ on the batch.
- **Screens:**
  - `payment-batches` and `payment-batch` (designed; fragments `PaymentBatches.html`, `PaymentBatch.html`, updated for
    the decisions);
  - the bank account card on `profile-preferences` (fragment updated);
  - the approval page of a claimant's bank change;
  - help keys and the guide page `purchases.payment-batches`, compiled into `kb/`.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs, and rev 0.154's decisions are recorded there:
- `banking/bank-transactions` FR-BNK-12;
- `purchases/payables` FR-AP-22;
- `access/approvals` FR-APR-05, FR-APR-10 and G4-055;
- `access/roles-and-segregation` SOD-1, SOD-2 and the "Payment batches" matrix row;
- `access/team-modes` FM-1, FM-3 and FM-8.

This change builds them (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Requirements built:** FR-BNK-12 (file, P1 part), FR-AP-22; the payment half of SOD-1 and SOD-2; G4-055; FR-AP-07's
  reimbursement in a batch; FR-AP-13's planned dates read by the batch.
- **Screens (docs/design/screens/inventory.json):** `payment-batches`, `payment-batch` (designed); `profile-preferences`
  gains the bank account card (FR-AP-22).
- **Outside dependency:** none for this change. The bank's own file needs the bank chosen, its specification and a
  test upload, so it is a follow-up change behind the same writer interface (rev 0.156).
- **Not built here:**
  - the bank's own file (DBS UFF or another bank's), a follow-up change once the bank is chosen (rev 0.156);
  - the bank API (P2);
  - overseas and foreign-currency payments: they need SWIFT's structured addresses (November 2026) and FX on release;
  - UOB and OCBC files;
  - salaries and the IR21 hold: they come with `p1-payroll-link` (FR-PAY-10, FR-PAY-11);
  - FM-8 break-glass release, which is checked with this change's release rules but built with `p1-approval-engine`
    task 3.3;
  - the Checks row C.2 #11;
  - the independent oracle needs no new rule: a batch's payments post by `bill_payment@1` and `claim_reimbursement@1`.
- **Who acts (Appendix D, "Payment batches"):**
  - the Accountant and the AP specialist prepare (C);
  - the Controller approves (A);
  - the Payment releaser releases (R);
  - the Owner, the Auditor and the Tax agent view (V);
  - in Founder mode the founders hold these roles.
- **Ports (ADR-T8):**
  - `Cipher`, which opens payees' numbers only while the file is written;
  - the blob store, which keeps the file;
  - the outbox, for the remittance email and the founders' notices;
  - the render service, for the remittance PDF.

  No new port.
- **Money movement:** the co-founder review (#6) before production (AGENTS.md); a threat model,
  `docs/security/threat-models/payment-batches.md`.
