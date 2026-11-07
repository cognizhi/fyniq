# Proposal

## Why

The workbook starts from the bank: each line is categorised, matched to an invoice or a bill, and reconciled to the
statement. Fyniq has invoices, bills, receipts and payments but no bank lines: receipts and payments are typed by
hand, nothing proves the books agree with the bank, and the close waits for reconciliations
(`internal/domain/closing`). Its dependencies have landed (`p1-sales`, `p1-purchases`, `p1-connector-framework`),
so the bank comes in now (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`, wave 3).

## What Changes

- **The owner's decisions of 25 Oct 2026, taken interactively (PRD-001 rev 0.140):**
  - **organisation connector secrets live in a dedicated secrets project** per environment, managed by the API's
    service account there and nowhere else; Google does not document that a `resource.name` condition limits
    `secretmanager.secrets.create`, so the connector framework's open `[verify]` (its threat model, T12) is resolved
    by design;
  - **Cognizhi holds DBS, Wise and Airwallex accounts**, and other banks or cards come in by file, so this change
    builds the DBS emailed reports, both API connectors and every file format (open questions 3 and 45);
  - **a transfer across currencies books each leg at its day's rate**: a fee the provider reports goes to bank
    charges, the rest to 7030; the accountant confirms before the oracle session;
  - **UC-BNK-03 builds the switch, not RAPID**: a money account can move its data source from emailed reports to an
    API connector; the RAPID connector stays in P2 (`p2-bank-feeds-payments`, FR-BNK-17 Phase 2).

- **Money accounts (FR-BNK-01, FR-BNK-13, FR-BNK-16):**
  - every money account of the chart (`is_money_account`) gets its register: kind (bank, card, wallet,
    payment gateway, cash), currency, institution, the account identifier statements name it by, and its data
    source;
  - each account shows its last sync and the date it is complete to, with ⚠ when stale (C.2 #8, #43);
  - the daily cash position totals every account and currency at the latest rates, from the balance service.
- **Statement import (FR-BNK-02, FR-BNK-16):**
  - statements for several accounts at once, from CSV and XLSX (a saved column mapping per bank, signed amount or
    in and out columns), OFX/QFX, camt.053 and MT940 (SWIFT and SAP);
  - each file is an untrusted upload, checked and scanned like any other, and kept with the statement;
  - every statement's opening balance must equal the previous closing balance; a gap is flagged, never filled;
  - PDF statements stay in P2.
- **One bank data model and its duplicates (FR-BNK-15, FR-BNK-04, FR-BNK-16):**
  - files, emailed reports and API connectors all produce the same statement and line: bank reference, booking and
    value dates, counterparty, amount, currency, running balance and position;
  - a line overlapping one already held is dropped only on proof (the bank's reference, or the running balance);
    a look-alike is kept and marked possible duplicate (C.2 #51); identical lines in one statement are both kept;
    a transfer already recorded from the other account is recognised.
- **Rules and the Categorise queue (FR-BNK-05, FR-BNK-06):**
  - ordered bank rules set a line's category, contact, GST code, dimensions and reference, and are tested against
    history before they are saved;
  - the queue shows each unreconciled line with suggested matches and rule hits; accept, edit, split, record as a
    transfer, or accept in bulk; in Basic mode one line at a time, in Expert mode as a grid.
  - AI suggestions join the queue with the AI changes; they are never auto-applied (AI-P4).
- **Posting (the bank posting rules of PRD §10.2 and Appendix B.1):**
  - a categorised line posts money in or out with the GST extracted by `RND-GST-INCL`, or a spread to 1210 or 2400
    released by `ALLOC-EVEN`, through the posting engine in the same unit of work;
  - the independent oracle gets the same rules in its own session, as `p1-sales` and `p1-purchases` did.
- **Matching (FR-BNK-07, FR-BNK-08):**
  - a line matches invoices or bills by reference (exact, then normalised), then by amount and contact: one to many,
    many to one, partial payments and overpayments as credits;
  - accepting a match **creates the receipt or the bill payment from the line** through the existing commands, so a
    settlement has one posting path; a receipt or payment already recorded by hand is matched instead, never
    recorded twice;
  - realised FX and the settlement tolerances are the existing ones (rev 0.121).
- **Transfers and suspense (FR-BNK-09, FR-BNK-11):** a transfer is recorded once and matched on both accounts, shown
  in transit until it arrives; an unmatched line may post to suspense (2999) with ⚠ until it is classified.
- **Auto-reconcile, deterministically (FR-BNK-14):**
  - the system principal reconciles, without a click, a line matching exactly one open document, a transfer
    matched on both accounts, and a line hit by a rule an approver marked auto-apply;
  - marking a rule auto-apply is an approval object showing the back-test; per-line and per-rule daily caps;
  - an Auto-reconciled list with the rule or match that did it, undo while the period is open, and a sample in the
    weekly review; repeated coding is offered as a rule, never applied by itself.
- **Reconciliation (FR-BNK-10, FR-GL-07):**
  - each account and period shows the statement balance, the reconciled book balance, the unreconciled items and
    the difference; a different person signs it off, recording the ledger revision it saw (ENG-46);
  - the close refuses a period whose money accounts are not reconciled and signed off, and warns on suspense, old
    unreconciled lines, possible duplicates and stale data (C.1 #30, #31, #32; C.2 #8, #9, #43, #51).
- **DBS, Phase 1 (FR-BNK-17, UC-BNK-01, UC-BNK-02):**
  - each organisation gets a **bank-ingest address**, a capture address whose only known senders are the bank
    addresses each money account registers; anything else is quarantined (FR-INT-22);
  - the daily account report is parsed and imported with the continuity and duplicate checks, then auto-reconciled;
    the monthly MT940 export or e-statement ties out the closing balance;
  - an incoming-funds alert is a **pending signal** that shows "likely paid" on the invoice and on Home and posts
    nothing; only statement lines are reconciled.
- **Wise and Airwallex (FR-BNK-18, UC-BNK-04; the first organisation connectors):**
  - read-only API keys (scoped where offered), installed and approved through the connector framework's
    organisation path under Appendix D's "Bank data… connections" row;
  - balances and transactions sync on the scheduler tick; a signed balance webhook only prompts an early sync;
  - Prism mocks of both APIs in the local `partners` profile, fakes in tests, the providers' sandboxes in staging.
- **The source switch (UC-BNK-03, the owner's decision):** a money account moves its data source from emailed
  reports to an API connector without a gap, keeps the e-statement tie-out, and keeps the signed bank agreements as
  evidence (FR-DOC-05); proved with a fake API connector.
- **Organisation connector secrets (the owner's decision):** a dedicated secrets project per environment in
  OpenTofu; the API's account manages secrets there only; the threat model's T12 is updated.
- **Screens** (all designed in `docs/design/screens/inventory.json`): `bank-accounts`, `add-bank-account`,
  `bank-account`, `statement-import`, `categorise-basic`, `categorise-expert`, `bank-rules`, `auto-reconciled` and
  `bank-reconciliation`, with their help keys and user-guide pages.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs: `banking/bank-transactions` (FR-BNK-01, 02, 04 to 11, 13 to
18, and the use cases of §J.3) and the bank posting rules in `ledger/posting-rules`.

### Modified Capabilities

None through deltas: the change sets `skip_specs`. PRD-001 revision 0.140 notes the P1 scope of UC-BNK-03 and adds
the secrets projects to §12.4; the main spec of the use case is updated with it and stamped (Recipe B).

## Impact

- **Database:** migrations for money accounts, statements and lines, mappings, rules, matches, transfers, pending
  signals, auto-reconcile records and reconciliations, each with `organisation_id` and `FORCE ROW LEVEL SECURITY`,
  guards and grants, audited in the same transaction; a capture address kind for the bank-ingest address.
- **Go:** `internal/bankfile` (the parsers), `internal/domain/banking` (lines, duplicates, rules, matching,
  auto-reconcile, reconciliation), the bank posting rule set in `internal/domain/ledger`, the storage port and
  the PostgreSQL adapter, the sync and auto-reconcile jobs on the tick, the close's new checks.
- **Connectors:** `connectors/wise` and `connectors/airwallex` (organisation scope, bank-data kind), their partner
  documents in `contracts/partners/`, and a bank-data capability port.
- **API:** the Banking operations (accounts, imports, lines, rules, matches, transfers, reconciliations, cash
  position), each in the contract first, with the authorisation matrix rows for "Bank transactions, categorise",
  "Bank reconciliation sign-off" and "Bank data… connections".
- **Web:** the nine screens, the Banking navigation item, help keys and user-guide pages compiled into `kb/`.
- **Infra:** the secrets projects and their grants in `infra/gcp/`.
- **Security:** a banking threat model (statement files and emailed reports are untrusted; webhooks prompt a pull
  and are never trusted for data; bank connectors are read-only) and the connector threat model's T12.
- **Oracle:** the bank posting rules, in a session of their own (never from a build session).
- **Not in this change:** payment batches (FR-BNK-12, `p1-payments-out`), bank feeds and APIs (FR-BNK-03, P2),
  DBS RAPID (P2), PDF statements (P2), and AI suggestions (with the AI changes).
