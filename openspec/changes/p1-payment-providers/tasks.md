# Tasks

Three slices, one stacked chain, merged as one pull request. Design references are to design.md. Cloud and real-account
checks are tagged `[live-verify]`.

## 1. The port, the connectors, the receiver and the event store (FR-PSP-01)

- [x] 1.1 `ports.PaymentProvider` (read-only: `Object`, `List`, `Balance`), the binding builder, `Runtime.PaymentProvider`
  and the capability's trigger check; the connector catalogue's check that a manifest's capabilities equal the binding's.
  - Tests: the interface has no write method and the adapters issue only `GET`; a connector's work needs it enabled, and its webhooks accept enabled or paused (the existing
    runtime's rule, tested in `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject`). `// req: FR-PSP-01`, `// req: FR-INT-27`
  - Built: `ports.PaymentProvider` (`Object`, `List`, `Balance`; `ProviderEvent`, `ProviderObject`), `Binding.PaymentProvider` and `Binding.ParseEvent`, `Runtime.PaymentProvider` (behind the connector's rate limit and breaker) and the authority `ProviderWork` (a system rule). The catalogue's existing check makes a manifest's capabilities equal the binding's builders. The adapters' method-set and GET-only tests are in 1.2 and 1.3.
- [x] 1.2 The Stripe adapter (`internal/adapters/stripe`): balance transactions as `BankData` lines (gross, then `:fee`),
  objects by ID, lists for a period, balance; signature verification with the timestamp tolerance and a rotation overlap;
  minor units by currency scale. Fixtures per API version.
  - Tests: signature accepted, tampered body, wrong secret, stale and future timestamp, two secrets; amounts for
    SGD, JPY, BHD; no float anywhere (`// req: ENG-10`); a missing object is `ErrProviderNotFound` and any other refusal an error with no body. `// req: FR-PSP-01`
  - Built: `internal/adapters/stripe`: `BankData` over `/v1/balance_transactions` in windows of at most 31 days (gross line, fee line `<id>:fee`), objects by ID with links (a payout's balance transactions; a charge's invoice from `/v1/invoice_payments`), lists, balance, `Verify` (Stripe's manual verification, retrieved 19 Nov 2026), `ParseEvent` (handled, informational, unknown). API version pinned `2025-09-30.clover` `[live-verify]`. A zero-decimal currency other than ISO's list (ISK, UGX) `[verify]`.
- [x] 1.3 The HitPay adapter (`internal/adapters/hitpay`): charges, fees and payouts as `BankData` lines, objects by ID,
  `Hitpay-Signature` verification; JSON numbers parsed as exact decimals from the raw text.
  - Tests: signature, tampered body; `0.1`, `1e2` and an 18-digit amount parse exactly or are refused; no float. `// req: FR-PSP-01`
  - Built: `internal/adapters/hitpay`: charges (gross and `<id>:fee` = `fixed_fee` + `discount_fee` `[verify]`) and paid payouts as lines in 7-day windows read again at their last day, objects, balance (available, pending and reserve `[verify]`), `Verify` (`Hitpay-Signature`), `ParseEvent` with an ID made of type, object and body digest because HitPay sends none (design D3's amendment). Refunded charges are not yet lines: **Left** for the live sandbox `[live-verify]`.
- [x] 1.4 Manifests and bindings `connectors/stripe` and `connectors/hitpay` (capabilities `payment-provider` and
  `bank-data`, destinations, secrets with a 24-hour rotation overlap, `parser_version`, `data_classes`, the webhook path
  and events); catalogue line; partner contracts `contracts/partners/stripe.yaml`, `hitpay.yaml` and the Compose services.
  - Tests: the manifests validate and the catalogue accepts them (`TestEveryManifestValidates`, the release catalogue test); the connector test action is the existing `TestBankData`, which both bindings serve.
  - Built: manifests and bindings `connectors/stripe` and `connectors/hitpay` (both capabilities, trigger `system-rule`, `parser_version`, a restricted-key and a webhook secret each, no data class, no write scope), the catalogue, `contracts/partners/stripe.yaml` and `hitpay.yaml` (hand-made extracts, retrieved 19 Nov 2026) with their Compose services on ports 4013 and 4014 and the README rows. The contract tests hold each file to the operations the adapter calls, GET only.
- [x] 1.5 Migration 000620 `psp_event`; the receiver `POST /webhooks/psp/{connector}/{id}` and the outbox kind
  `psp.process`: sealed raw payload with SHA-256, API version, parser version, unique event ID, 2xx on a duplicate, 401 on a
  bad signature, 413 over the cap, 404 for a disabled connector; unknown event types parked with ⚠.
  - Tests: duplicate and out-of-order delivery store one row each; unknown type parked and posts nothing (UC-PSP-01); the
    payload never in a log line or an audit event; a forged and an oversized body (red-team, `// asvs: V13`, `V4`).
    `// req: FR-PSP-01`
  - Built: migration 000620 `psp_event` (sealed payload, SHA-256, API and parser versions, unique per connector and event ID, status forward-only FY620 and FY621, FORCE RLS, audited in the transaction, grants), `internal/pspevents` (`Hooks` at `/webhooks/psp/{connector}/{id}`, `Processor` with `Work` and `Dead`), wired into `cmd/api` and `cmd/worker`; the provider column is a format check, so a new provider needs no migration. The worker fetches the object and hands it to the handler of its kind; the handlers are task 2.2 (until then an event is parked with 'no posting rule reads a ... yet'). `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject`.
- [x] 1.6 The threat model `docs/security/threat-models/payment-providers.md` and the control rows it moves; the
  authorisation matrix rows for the new operations (none writes to a provider); `make openapi` if an operation was added.
  - Built: `docs/security/threat-models/payment-providers.md`. No operation was added in this slice, so no matrix row and no `make openapi`; the reads of slice 3 add theirs.
## 2. Balance transactions, postings and payout reconciliation (FR-PSP-01, 04, 05, 06)

- [x] 2.1 Migration 000622 (`psp_account`, `psp_txn`, `psp_payout`, `psp_balance`); the storage port and the postgres adapter; the provider money account's setup.
  - Tests: the unique posting identity is enforced by the database; RLS and grants. `// req: FR-PSP-06`
  - Built: migration 000622 (`psp_account`, `psp_txn`, `psp_payout`, `psp_balance`; no `psp_posting`, because the line's reference is the posting identity, design D4), `storage.PSPLedger` and its adapter, guards FY622 and FY623, the NaN attacks of the five amounts. `TestAProvidersRecordsAreKeptAsTheProviderReportedThem`. The money account's identifier is the provider's balance currency; the setup API and screen are task 3.3.

- [x] 2.2 The sweep and the posting of what the provider's own data makes certain (design D5): fees, payouts and disputes' money.
  - Tests: G4-041 (30 charges, S$117.00 of fees once, three unsettled charges leave S$288.30); a redelivery and a backfill
    post nothing; a payout that debits the bank and a negative balance post the reverse legs. `// req: FR-PSP-04`,
    `// req: FR-PSP-05`, `// req: FR-PSP-06`
  - Built (decided in building, design D5): the worker cannot make a document and the bank sync posts nothing (rev 0.143), so the sweep (`internal/psp`, the tick's `psp.sweep` and the event's `psp.run`) only mirrors, and posting is a new exact case of `banking.AutoReconcile`, which keeps the S$500 cap, the audit actor, the preview, the weekly sample and undo: a fee to the fee account with GST code `OP`, a dispute's money to disputes clearing, a payout as a transfer to the bank account. Charges and refunds wait for the document (task 3.2). `TestAProviderAccountIsSweptAndItsLinesReconciledOnce`: fees expensed once (S$11.70 of three), a second sync, sweep and Reconcile now post nothing. **Left:** G4-041's 30 charges (the test has three, with the same arithmetic); a payout that debits the bank and a negative balance (the legs are the lines' signs; a payout that brings money in waits for a person, untested); a locked period's refusal is auto-reconcile's own and is not re-tested here.

- [x] 2.3 Disputes (D9): opened and won through disputes clearing; UC-PSP-03.
  - Tests: opened, won (back in clearing) and lost (expense); both reverse cleanly in clearing. `// req: FR-PSP-04`
  - Built: a dispute's opened and won legs through disputes clearing, in the same exact case, when the provider account names a disputes account (otherwise the lines wait). **Left:** a lost dispute posts nothing (design D9, owner decision 1); no standard-chart rows 1055 and 5045 (the Owner adds them; the chart is provisional); the settings `psp.disputes_account` and `psp.dispute_loss_account` became the provider account's own fields.

- [x] 2.4 The daily sweep (`psp.sweep`, on the tick): the bank feed run plus lists by ID, the balance snapshot, the "complete
  to" date; a gap it cannot close is ⚠.
  - Tests: G4-107, a four-day outage fetched by ID with no duplicate posting. `// req: FR-PSP-01`
  - Built: `psp.Runner` (the tick's `psp.sweep` hourly at ten past, and `psp.run` queued by a handled event): from four days before the day each account is complete to (90 days the first time), in weeks, balance transactions, payouts with what they settle, and the balance; the day each account is complete to, or why not, on `psp_account`. A failing provider records its error on the account and shows in C.2 #43. **Left:** G4-107's four-day outage is not simulated; the mirror's once-only rows and the lines' references are what make a re-read harmless.

- [x] 2.5 Checks C.2 #42 (✖) and C.2 #50 (⚠), and the provider part of C.2 #43, on the checks engine; the fee tax invoice
  match function (D8) with its ⚠, on a fixture.
  - Tests: a fixture that triggers each message (QA-RPT-02); a missing balance transaction, a wrong sign and an unposted fee
    each break #42. `// req: FR-PSP-06`
  - Built: C.2 #42 (`payments.payout-nets`, error) reads the posted lines of each paid payout's transactions and the payout; C.2 #50 (`payments.clearing-balance`, warning) compares the book balance with the latest provider balance; C.2 #43 now includes a provider sweep behind or failed. `TestAProviderAccountIsSweptAndItsLinesReconciledOnce` triggers each, then clears them. **Left:** the fee tax invoice match with its warning (C.2 #50's second half, FR-PSP-05), which waits for `p1-document-capture`'s documents; the month-end date (the check compares the books today with the provider's last reading).

- [x] 2.6 Oracle half: a differential scenario of a month of provider activity through the existing sources (receipt, fee line,
  transfer, credit note, refund, dispute lines), written from the rules, staged as `test/differential/pending/` until the
  engine's half is green, then moved. `// req: QA-ENG-02`
  - Built: `test/differential/golden/payment-providers.json`, a month of a provider account through the existing sources (charges to revenue, fees, a dispute and its win, a payout as a transfer), its figures worked by hand in `oracle/tests/test_pending_golden.py`; the gate posts it through both sides with 0 disagreements. No posting source was added, so nothing else needs an oracle half. **Left:** receipts and credit notes of mirrored invoices join it with task 3.2.

## 3. The Model B mirror, the screens and the guide (FR-PSP-03, 04, 07)

- [ ] 3.1 Migration 000624 (`psp_customer`, `psp_subscription`, `psp_invoice`); the mirror from fetched objects; the customer
  mapping.
- [ ] 3.2 Finalised, paid, failed, voided, uncollectible and credited invoices (FR-PSP-07): the mirrored AR document through
  `Invoices.Create` and `Post`, total equals the provider's or parked, the service-period spread (D6), a credit note for a
  refund; unsupported pricing parked.
  - Tests: G4-040 (out-of-order, redelivered), G4-106 (collection state mirrored, no reminder, no charge), UC-PSP-01; a
    prorated upgrade; an annual plan over twelve months. `// req: FR-PSP-03`, `// req: FR-PSP-07`
- [ ] 3.3 The API (additive): `GET /v1/payment-providers/payouts`, `.../payouts/{id}`, `.../subscriptions`,
  `.../events` (parked first), and the resolution of a parked event or invoice by a person; the matrix rows; `make openapi`,
  then the web build.
- [ ] 3.4 The screens `provider-payouts` and `subscriptions` (designed first, fyniq-openspec Recipe C), their help keys,
  guide pages and compiled knowledge-base pages.
- [ ] 3.5 PRD revision (new IDs only, `(rev 0.N)`, revision row), the spec stamp, `make roadmap`, `compliance` control rows.
  **Left unless decided:** the daily-summary posting mode; revenue reversal on a lost dispute (decision 1); capture of the
  provider's tax invoice (`p1-document-capture`); every live-provider check `[live-verify]`.
