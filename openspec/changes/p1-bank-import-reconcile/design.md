# Design

## Context

See proposal.md for why. What the code has today:

- **Money accounts are chart accounts** with `is_money_account` (migration 000022). Receipts, bill payments,
  refunds and deposits post to them by the account's ID (`internal/domain/ledger/receipt.go`: `Account`). There is
  no register of banks, no statement and no bank line.
- **Settlements exist:** receipts and bill payments allocate to invoices and bills with realised FX and the
  settlement tolerances of rev 0.121 (`internal/domain/ledger/settlement.go`, C.2 #49 in the close).
- **The close** (`internal/domain/closing`) runs its checks, records the ledger revision they saw (ENG-46) and
  waits for this change for reconciliations.
- **Email-in** (FR-INT-22) receives mail at capture addresses (migration 000102: the organisation's and each
  member's), checks DMARC, quarantines unknown senders and keeps attachments as scanned, untrusted files.
- **Connectors** (`p1-connector-framework`): manifests, the organisation lifecycle through the approval engine
  (FM-1), secrets in a `SecretStore` by reference, the egress guard (SEC-36), the runtime that reads the enabled
  connectors at each piece of work, Prism partner mocks. No organisation connector ships yet.
- **Appendix D** has three rows here: "Bank transactions, categorise" (Bookkeeper and Accountant CP, Controller A),
  "Bank reconciliation sign-off" (Bookkeeper and Accountant C, Controller A) and "Bank data, payment-provider and
  ticketing connections" (Administrator C, Owner A with a second approver).

## Goals / Non-Goals

**Goals:**
- One bank data model fed by files, emailed reports and API connectors alike, so matching, auto-reconcile and
  reconciliation never care where a line came from.
- One posting path per economic event: a bank line that settles an invoice creates the receipt the hand path
  creates; a line coded to a category posts through the bank rule set; nothing posts twice.
- Untrusted inputs stay untrusted: a statement file, an emailed report and a webhook body are parsed with limits,
  and a webhook never carries data into the books (it only prompts a pull).
- The first organisation connectors prove that path end to end, including where their secrets live.

**Non-Goals:**
- Payment batches and bank payment files (FR-BNK-12: `p1-payments-out`).
- Bank APIs, host-to-host files, DBS RAPID (P2), PDF statements (P2, document AI).
- AI suggestions in the Categorise queue: the queue has a slot for them; the AI changes fill it.
- The full Checks engine (`p1-checks`): this change adds its bank checks to the close, as earlier changes did.

## Decisions

### D1. The money account register sits on the chart account

A `money_account` row per chart account marked `is_money_account` (one to one): kind (bank, card, wallet, payment
gateway, cash), currency (the chart account's), institution, display name, the **account identifier** statements
name it by (account number, IBAN or the provider's balance ID), and its **data source** in an effective-dated
`money_account_source` (file, emailed reports, or an installed connector), so a switch (D14) keeps its history.
Sync state (last sync, complete-to date, last closing balance) is derived from statements, never typed.

- The identifier is the company's own account number, already printed on invoices (organisation profile), so it is
  not a secret; it is still masked in logs and the audit trail (SEC-15) and shown in full only on the account.
- *Alternative:* a separate money account entity with its own ledger account created behind it. Rejected: receipts
  and payments already post to chart accounts, and two IDs for one account invite drift.

### D2. Statements and lines are immutable once imported

`bank_statement` (account, source, format, parser version, period, opening and closing balances when the format
gives them, the file's evidence reference) and `bank_line` (account, statement, booking and value dates, signed
amount and currency as `NUMERIC(20,4)`, bank reference, counterparty, description, running balance, position,
fingerprint, status). A line's facts never change; its status moves forward only (new → possible duplicate or
unreconciled → categorised / matched / transferred / suspense / excluded), except an auto-reconciliation undone
while the period is open (D8). A guard trigger refuses anything else (INV-style, its own `FY` codes).

- **Continuity (FR-BNK-16):** a statement's opening balance must equal the account's previous closing balance;
  otherwise a `bank_gap` is recorded and shown (⚠), never filled by guesswork. MT940 reads `:60F:` and `:62F:`;
  camt.053 its `OPBD` and `CLBD` balances; CSV and OFX their balance columns when present, else the running balance.
- **Duplicates (FR-BNK-04):** the fingerprint is account, booking date, amount, currency, bank reference,
  counterparty and the running balance or position. An incoming line equal to one held is dropped only when the bank
  reference matches or the running balance proves the overlap; otherwise it is kept as possible duplicate (C.2 #51)
  for a person to resolve. Identical lines inside one statement are both kept (G4-042).

### D3. Parsers are pure, bounded and fuzzed

`internal/bankfile` parses CSV, XLSX, OFX/QFX (SGML and XML), camt.053 and MT940 (SWIFT and SAP field 86) into the
D2 model, with exact decimals (`internal/domain/money`), a parser version per format, and limits on file size,
lines and field lengths. CSV and XLSX use a **saved mapping per institution** (`statement_mapping`: date format,
signed amount or in and out columns, the columns of each field), offered from the first file and reused.

- **XLSX without a new library:** a small reader over `archive/zip` and `encoding/xml` for the first sheet's values
  (shared strings, numbers, date serials), with the zip-bomb limits of the evidence upload. *Alternative:* `excelize`.
  Rejected for now: a large dependency for one sheet of values, on untrusted input.
- Each parser has golden fixtures (synthetic statements) and a Go fuzz test; a parse error names the line and field
  (ENG-23).

### D4. The bank posting rule set, and the oracle

A categorised line posts through the engine with a new rule set (Appendix B.1): money account ± signed gross; 2200
takes − `RND-GST-INCL`(signed gross) when the organisation is registered at the line's date and the code carries GST;
the category takes the rest; a spread puts the category leg on 1210 or 2400 and releases it monthly by
`ALLOC-EVEN` from the posting month (recognition schedules as journals do). The independent oracle gets the same rules
from Appendix B.1 in a session of its own (golden rule 3); the differential runs then cover bank lines.

As built (task 4.1, rev 0.141): the posting's source is a document of its own, `bank_transaction` (unnumbered),
written with its entry, the match and the line's status in one unit of work (ENG-02). A document gives the
categorisation what every other posting source has: a version that keeps its parts, the GST rate of each and the
exchange rate it posted with (so the replay never re-derives them), evidence, a reversal when undone, and the spread
scheduler, whose query reads `date` and `lines[].spread_months` from any document's payload.

- `bank_transaction@1`: the line's signed gross on the money account (RND-FX at the cascade's rate for the booking
  date); per part RND-FX of its share, GST under RND-GST-INCL of that, the rest to its account or to 1210 / 2400;
  for a split line in another currency the cent RND-FX leaves between the parts and the line goes to 7030, as a bill's
  does. Every part has the line's direction; a money account (a transfer, D6) or receivables and payables (a match,
  D5) are refused as categories; 2200 is a category only with no GST (a payment to or from IRAS).
- Releases: `bank_out_release@1` (Dr the part's account, Cr 1210) and `bank_in_release@1` (Dr 2400, Cr the part's
  account), posted at once up to today and then by the hourly job `bank.releases` as `scheduler:bank.releases`.
- A part on 2999 makes the match `suspense` and keeps the line in the queue; classifying it reverses the suspense
  posting in the same unit of work (D7). An undo reverses the transaction and its releases on their own dates and
  puts the line back; a new categorisation is a new document.
- The database refuses a categorised or suspense match whose transaction is not its line's, or whose entry does not
  post the line's amount on its booking date (FY228), and a release of a transaction no longer posted (FY227).
- *Alternative:* the line itself as the source, versioned by its match. Rejected: the parts, GST rates and spreads
  would need a store of their own, and the spread scheduler and evidence would need a second path.

### D5. Matching creates the receipt or payment, or matches one recorded by hand

`bank_match` links a line to what it settles. Accepting a suggested match to invoices calls the **receipt** command,
and to bills the **bill payment** command, with the line as their source: the same validations, settlement
tolerances, realised FX and postings as the hand path, one to many and many to one, with an overpayment kept as a
credit (FR-AR-18). When a receipt or payment for that money was already recorded by hand (same account, amount and
date window, not yet matched), the line matches it and posts nothing new.

- Suggestions (FR-BNK-06, FR-BNK-07): the bank reference against document numbers and payment references (exact,
  then NFKC-normalised, trimmed, case-insensitive), then amount and contact; ranked, never applied by themselves
  except under D8.
- *Alternative:* the line posts its own settlement journal (Dr Bank, Cr 1100), as the workbook did. Rejected: two
  paths to settle one invoice would split the sub-ledger's rules and the tolerance checks.
- As built (task 4.3): `MatchInvoices` and `MatchBills` record the receipt or bill payment through the commands'
  `RecordIn`, dated on the line's booking date into its money account with the bank's reference, in the unit of work
  that makes the match and moves the line's status; for documents in another currency than a base-currency account,
  the line's amount is what the account received or paid. Money recorded by hand already (a receipt, payment, deposit
  or refund within 7 days, moving the line's amount through its account, not yet matched) is refused as a new
  document and matched with `MatchRecorded`, posting nothing. `Unmatch` undoes the match and voids what was made from
  the line on its own date. Suggestions rank a document recorded by hand first, then a number (or a bill's supplier
  number) named exactly, then once normalised, then the amount open from the contact the counterparty names, then
  the first rule; a number is named only on word boundaries (INV-1 is not INV-10). The database checks every match
  posts the line's amount through its account (FY228) and that a matched document is never reversed first (FY229).

### D6. Transfers are one record with two legs

`bank_transfer` posts Dr receiving, Cr paying once, matched to a line on each account; with one side only it is
**in transit**.

- **Across currencies (the owner's decision of 25 Oct 2026):** each leg is booked at its own currency's rate for
  the day, through the rate cascade (ENG-40): the paid leg as paid, the received leg at the day's rate (`RND-FX`). A
  fee the provider reports (Wise shows one) goes to the bank charges account, an organisation setting defaulting to
  6460 Bank charges; whatever difference is left goes to 7030. No rate for the day refuses the transfer with the
  currency and date named (FX-01), as every foreign-currency posting does. The accountant confirms the
  rule before the oracle session.
- *Alternative:* the received leg at the paid leg's base amount (the implied rate), with no difference at transfer.
  Rejected by the owner: it hides the conversion's cost in the balance until revaluation.
- As built (task 4.4): `bank_transfer@1` (Cr the paying account, Dr the receiving one, each at RND-FX of its own
  currency's rate for the day; Dr the bank charges account with the fee in the paying currency; the rest to 7030; in
  one currency the legs differ by the fee only). The bank charges account is the setting `bank.charges_account`, a new
  setting kind `account_code` holding an account's code, 6460 by default, under dual control. `RecordTransfer` writes
  the record, posts its entry and matches the lines known; `CompleteTransfer` matches the other side when it arrives,
  posting nothing. The replay reads the transfer's record and the rates its entry kept. Undoing a transfer comes with
  auto-reconcile's undo (task 5.1).

### D7. Suspense and classification

An unmatched line may post to 2999 with ⚠ (FR-BNK-11). Classifying it later reverses that entry and posts the real
one (never an update, golden rule 2). The close warns while 2999 is not zero (C.1 #30).

### D8. Auto-reconcile by the system principal

A job runs after each import or sync, as the system principal (FR-AUT-02): a line with the same amount and currency
as exactly one open document, a unique reference and the expected contact; a transfer matched on both accounts; a
line hit by a rule marked auto-apply. Each is an `auto_reconciliation` record naming the match or rule, within the
caps of two organisation settings (per line, and per rule per day; FR-ORG-08 recommended defaults). Undo reverses it
while the period is open. A sample joins the weekly review (FM-3, the review log).

- **Auto-apply is an approval object** (`bank_rule`, operation `bank_rule.auto_apply`) whose page shows the
  back-test and the rule's hits since it was created; no line is auto-reconciled by a rule before it is approved.
  A rule records whether it came from an AI proposal, so the G4-059 refusal applies once the AI changes land.
- Repeated coding (five times by default) is offered as a rule to promote, never applied by itself.
- As built (task 5.2): the approval object `bank_rule` with `bank_rule.auto_apply` (migration 000149), approved by a
  person holding A on "Bank transactions, categorise"; its envelope holds the rule's version and the SHA-256 of its
  conditions and action, and the request shows the back-test and the hits since the rule was made. The endpoint
  `requestBankRuleAutoApply` asks for it; the rest of the rules API comes with task 9.1. `banking.Repeated` offers the
  patterns coded the same way five times (by counterparty, or the description's leading words) that no rule covers.

### D9. Reconciliation is prepared, then signed off by another person

`bank_reconciliation` per money account and period: the statement balance (from the statement or an uploaded
e-statement), the reconciled book balance (from the balance service), the unreconciled items, the difference, and
the ledger revision it saw (ENG-46). The preparer (C on "Bank reconciliation sign-off") prepares it; another person
with the sign-off right signs it, with step-up (FM-5, SOD-5); the period's lock locks it.

- **The close** gains ✖ "a money account with activity in the period has no signed-off reconciliation at the
  revision the checks saw" (FR-GL-07), and ⚠ for suspense, a negative balance at month end, a reconciliation that
  does not match, data older than its schedule allows, unreconciled lines older than 30 days and unresolved possible
  duplicates (C.1 #30 to #32; C.2 #8, #9, #43, #51).

### D10. DBS through the bank-ingest address

A third kind of capture address, **bank** (one per organisation, named by an Administrator, such as bank@…). Its
known senders are only the addresses each money account registers for its bank (DMARC must pass, as for every
capture address); any other sender is quarantined (UC-BNK-01). The worker imports an accepted report through the
account's format and mapping, then runs D2's checks and D8.

- **The report's format is `[verify]`:** DBS documents scheduled reports in PDF, Excel or text, and MT940 and CSV as
  exports; which formats a Singapore IDEAL schedule emails is to confirm with a redacted sample of Cognizhi's report
  before the parser is pinned. Excel and CSV go through the mapping (D3), MT940 through its parser; a PDF-only report
  would leave Phase 1 on uploaded exports until P2's document AI.
- **Incoming-funds alerts** become `bank_signal` rows (amount, currency, payer name, received at), matched to an open
  invoice by amount and customer: the invoice and Home show "likely paid" (UC-BNK-02). A signal posts nothing and
  closes when a statement line reconciles the invoice, or expires after a setting's days.

### D11. Wise and Airwallex as organisation connectors

Two connectors in `connectors/`, organisation scope, capability kind `bank-data`, trigger `system-rule` (the sync
schedule), read-only:

- **Wise:** a read-only API token [verify that Wise offers read-only tokens to Singapore business profiles];
  configuration maps each Wise balance to a money account; balance statements (Wise offers camt.053 and MT940 for
  Singapore profiles) and transactions are pulled; the `balances#update` webhook is verified with RSA-SHA256 against
  Wise's published public key [verify the key and header from Wise's webhook guide].
- **Airwallex:** a client ID and an API key, scoped where Airwallex offers it [verify]; balances and transactions are
  pulled; webhooks are verified by HMAC-SHA256 over the `x-timestamp` header and the raw body with the endpoint's
  secret (from Airwallex's "Listen for webhook events" page), with a five-minute window.
- A webhook only **prompts an early sync** of its connector: its body is never trusted for data. Routes are
  `/api/webhooks/bank/{connector}/{installed connector id}`; an unknown, disabled or uninstalled connector gets 404.
- A **bank-data port** (`internal/ports`): accounts, balances and transactions since a cursor. The sync job runs on
  the tick (ADR-T7) per enabled connector, through the egress guard (SEC-36) with the manifests' destinations
  [verify each provider's API and sandbox hosts], writes D2's statements and lines, and moves the cursor in the same
  unit of work.
- **Permissions:** steps of a bank-data connector need Appendix D's "Bank data… connections" row; the connector API
  checks that row by the manifest's kind, beside "Organisation integrations", and the approval policy names it.
- **Local and tests:** fakes in Go tests; Prism mocks from `contracts/partners/wise.yaml` and `airwallex.yaml` in the
  `partners` profile; the providers' sandboxes in staging.

### D12. Organisation connector secrets in a dedicated project (the owner's decision)

OpenTofu adds a secrets project per platform environment (`fyniq-staging-secrets`, `fyniq-prod-secrets` [verify the
IDs with the founder's organisation]). The API's service account holds `roles/secretmanager.admin` there and no
Secret Manager role elsewhere beyond the installation secrets it already has; the API and the worker read there.
`connector.Organisations` names its secrets in that project (`FYNIQ_ORG_SECRETS_PROJECT`); local mode keeps its
secret files. The connector threat model's T12 and its open item are rewritten; `make infra-check` proves no other
project grants the account a create right.

### D13. The source switch (UC-BNK-03, the owner's decision)

Changing a money account's source adds a `money_account_source` row from a date; the next statement from the new
source must continue the chain (D2), and the monthly e-statement tie-out (D9) is unchanged. The bank agreements
are evidence on the money account (FR-DOC-05). Proved by switching an account from emailed reports to a fake API
connector without a gap; the RAPID connector itself is P2.

### D14. API, ports and audit events

- **API (API-first):** money accounts, statement imports (preview first: what will be added, flagged and dropped),
  lines and the queue, rules (with the back-test), matches, transfers, signals, auto-reconciliations and undo,
  reconciliations and sign-off, and the cash position, each in the contract with `Idempotency-Key`, `If-Match`,
  `Prefer: return=preview` and RFC 9457 codes (`BANK-nn`).
- **Ports and adapters (ADR-T8):** the storage port's banking repositories; the bank-data port with the Wise and
  Airwallex adapters; the `SecretStore` pointed at the secrets project; the inbound email port reused.
- **Audit events (AUD-01):** `money_account.*`, `bank_statement.imported`, `bank_line.*` (categorised, matched,
  transferred, suspended, excluded, duplicate resolved), `bank_rule.*`, `auto_reconciliation.*` (made, undone),
  `bank_transfer.*`, `bank_signal.*`, `bank_reconciliation.*` (prepared, signed off), each in the transaction of
  its change.

## Risks / Trade-offs

- [DBS emails a format Fyniq cannot parse] → confirm with a redacted sample before pinning the parser; uploads of
  the MT940 or CSV export work regardless.
- [A parser mis-reads a statement] → golden fixtures per format, fuzzing, the continuity check, the e-statement
  tie-out, and a preview before anything is added.
- [Duplicate detection drops a real line] → drop only on proof; anything less is kept and flagged for a person.
- [Auto-reconcile reconciles the wrong document] → exact-one-match only, caps, the Auto-reconciled list with undo,
  the weekly sample, and auto-apply rules only after an approval with a back-test.
- [A bank connector key leaks] → read-only keys, the secrets project, pause at once (rev 0.139), and webhooks that
  can only prompt a pull.
- [Wise or Airwallex change their APIs] → each adapter's contract tests against the partner documents and the
  sandboxes in staging; the manifests' versions record what was consented.
- [The change is large] → built in slices, each one passing every gate at its own commit.

## Migration Plan

New tables only; nothing existing changes shape except the capture address kind check (widened in one migration,
validated in the next). Order: the store, the importers and posting, matching and auto-reconcile, reconciliation and
the close's checks, DBS by email, the secrets project, Wise and Airwallex, the screens. The secrets project must
exist before the first organisation connector is enabled in staging. Rollback is a release rollback: imported lines
and their postings stay, as posted history always does.

## Open Questions

- The accountant confirms D6's cross-currency rule before the oracle session; a change there changes one rule
  version, not the design.
- The sync schedule's default (hourly, the tick) can change by setting without changing the design.
