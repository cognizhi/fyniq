# Tasks

Decisions are in design.md. Cloud and real-account checks are tagged `[live-verify]`. Migrations 001050 to 001069.

## 1. Model A: payment links and the automatic leg (FR-AR-13, FR-PSP-02)

- [x] 1.1 `ports.PaymentLinks` (one method), the HitPay adapter's `POST /v1/payment-requests` through a client that refuses
  any other method, the `payment-link` capability in the manifest, the partner contract's POST section.
  - Tests: the interface has one method; the client refuses GET and DELETE; amounts are exact decimals; the form carries
    `amount`, `currency`, `reference_number`, `payment_methods[]`, `redirect_url`, `expiry_date`. `// req: FR-PSP-02`, `// req: FR-INT-27`
- [x] 1.2 Migration (`payment_link`, `payment_link_payment`, one live link per invoice, FORCE RLS, audited), the storage port,
  the command (`POST /v1/invoices/{id}/payment-link`, `GET` it), codes PSP-09 and up, the matrix rows.
  - Tests: a second live link is refused by the database; a draft, void, paid or foreign-currency invoice is refused; the
    amount is the open balance even when the client sends another. `// req: FR-AR-13`
- [x] 1.3 The worker's `payment_request` handler (fetch by GET, mirror the payments) and the auto-reconcile exact case.
  - Tests: a completed request reconciles once; a replay, a second payment, a wrong amount and a void invoice post
    nothing and park with a warning; a locked period leaves the line. `// req: FR-PSP-02`
- [x] 1.4 The invoice page's payment-link card, help keys, guide page, kb page, design fragment. **Left:** the link on the sent email and the PDF, and a setting that adds a link when an invoice is sent.
- [x] 1.5 Threat model update, PRD rev 0.510, spec scenarios, stamp.
- Built (rev 0.511): `ports.PaymentLinks`, `internal/adapters/hitpay/links.go` (a client that refuses any method or path but the one POST), the capability `payment-link` (`connector.PaymentLink`, `Runtime.PaymentLinks`, `connector.LinkRequest`), migration 001050, `internal/domain/paymentlink`, `internal/api/payment_links.go`, the worker's mirror (`psp.recordLinkPayment`: the charge names its payment request through `payment_request_id`) and `banking.linkPlan` (`LeftLink` otherwise), `PaymentLinkCard` on the invoice page, the guide section and kb page `sales.invoices`, the design fragment, threat model T15 to T18, the partner contract's POST. Decisions recorded in design.md D1 to D3. No posting rule changed, so no oracle half. Needs-human-review: the new connector write and the worker's grant on `payment_link_payment`.
- **Left, `[live-verify]`:** HitPay's field for a payment's charge ID, the sandbox run, a dynamic PayNow QR image per invoice
  (the checkout page carries the QR meanwhile), Stripe payment links (Model A is HitPay's, by question 23).

## 2. Host-to-host files (FR-BNK-17, FR-BNK-15)

- [x] 2.1 `ports.FileDrop`, the local directory adapter, the sync of drop folders as a bank-data source (rev 0.577): `internal/ports/filedrop.go`, `internal/adapters/sftp` (pinned host key, key auth, read-only), `internal/adapters/local/filedrop.go` (local build), `internal/adapters/filedrop` (files as transactions), `connectors/filedrop` and local mode's `filedrop-local`. Tests: `TestAnSFTPFolderListsAndReadsPlainFilesFromAPinnedHost` (an in-process SFTP server), `TestStatementFilesInADropFolderAreAnAccountsTransactions`, `TestADirectoryDropListsAndReadsOnlyItsOwnPlainFiles`, `TestStatementFilesInADropFolderFeedAMoneyAccount`. MT942 reports in an intraday folder become pending signals (rev 0.582: `ports.BankReports`, `TestIntradayReportsInADropFolderAreSignalsNotLines`, `TestIntradayReportsAreTheFilesThatChangedSinceTheTimeAsked`).
- [x] 2.2 An MT942 reader (`internal/bankfile`) whose lines are signals; closing a signal by the MT940 line.
  - Built (rev 0.522): `mt942` as a money account's report format (migrations 001074, 001075), `bankfile` reads it (no balances, the floor limit's currency), `bankingest.Intraday` records each credit as a pending signal once per bank reference; the existing signal machinery closes it when its statement line is matched to the invoice, or expires it (`TestAnIncomingFundsAlertSpeedsUpMatching` proves the closing for alerts; an intraday signal is the same record). `TestAnIntradayMT942ReportHasLinesAndNoBalances`, `TestAnIntradayReportMakesSignalsAndNoStatementLines`. Not built as a manual import format: an intraday report arrives as a report on the account's source.
  - Tests: an intraday file creates signals and no statement line; the daily file closes them. `// req: FR-BNK-16`
- **Left:** PGP, the static egress IP and a run against a real bank's SFTP `[live-verify]` (DBS onboarding),  DBS RAPID and UOB API Services
  adapters: no public specification, so no code and no cassette `[verify]`.

## 3. Corporate cards (FR-AP-08)

- [x] 3.1 Receipt suggestions for a card line (rev 0.549: `capture.SuggestsFor`, `listCardReceiptSuggestions`, `attachCardReceiptSuggestion`; tests `TestAReceiptIsSuggestedOnlyForTheSameAmountNearTheDate`, `TestACapturedReceiptIsSuggestedForACardLineAndAttachedByItsHolder`). Only mail to the holder's own capture address (an Employee may not upload to the inbox).
- [x] 3.2 The daily missing-receipt reminder through the Inbox (rev 0.548: the job `notify.card_receipts`, notification `card.receipt_missing`, setting `claims.receipt_reminder_days`; test `TestACardholderIsToldOnceADayOfTheirLinesWithNoReceipt`). 

## 5. The payment link in the invoice email (FR-AR-13, rev 0.578)

- [x] 5.1 The email carries a live stored link: `salesmail.livePaymentLink`, the setting `invoices.payment_link_in_email`; test `TestAnInvoiceEmailCarriesItsLivePaymentLinkOnlyWhileItIsLive`. No provider call in the send path.
- [x] 5.2 Built (rev 0.589; `TestIssuingAnInvoiceQueuesALinkJobThatTheWorkerRunsOutsideTheRequest`, `TestTheNarrowFunctionRecordsALiveLinkOnlyForAPostedInvoiceAndAnEnabledConnector`). As designed: the link made automatically when an invoice is issued: the issue enqueues a `payment_link.make` worker job; the worker asks the provider (`Runtime.PaymentLinks`, egress-guarded) and stores the link through a narrow `SECURITY DEFINER` function that checks the invoice is posted and the URL is https; the email then carries the link only once created, and a send before that goes without it. The function is a worker grant and is `needs-human-review`. The link on the invoice's PDF is a template change with its own version (FR-TPL-03).
  - **Left:** the link on the invoice's PDF (a template change with its own version, FR-TPL-03) and automatic links for invoices issued by recurring or project billing paths.
