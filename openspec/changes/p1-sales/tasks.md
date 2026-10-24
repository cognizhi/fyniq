# Tasks

Each slice is one pull request, merged when green. Tests carry `// req:` tags (QA-TRACE-01); every new
✖ or ⚠ message is triggered by a fixture (QA-RPT-02).

## 1. Invoices: the document, its rule and its approval

- [x] 1.1 The invoice document: versions, the payload of `design.md` (lines or one amount, service period,
  template, notes), the customer's terms and due date, the invoice sequence numbered at issue. Tests
  `// req: FR-AR-03`
- [x] 1.2 The posting rule `invoice@1`: 1100 the total, revenue or 2400 the net under `RND-FX`, 2200 the output
  GST under `RND-GST-EXCL` once GST-registered, a foreign invoice's GST in SGD; a replay golden. Tests
  `// req: ledger/posting-rules Posting an invoice`, `// req: ledger/posting-rules Amounts of a posted invoice`
- [x] 1.3 Issue, void and edit through the posting service: FM-2's threshold, FM-1 for a void, SOD-3, FR-GL-12;
  the five states. Tests `// req: FR-AR-03`, `// req: FM-2`, `// req: SOD-3`
  (24 Oct 2026, PRD-001 rev 0.116, tasks 1.1 and 1.2 and this in part: `internal/domain/invoice` (the payload, lines under
  `RND-LINE` with its discount, the customer's terms, GST codes for supplies and the rate only once GST-registered),
  `ledger.PostInvoice` with `testdata/replay/invoice@1.golden`, `journal.Invoices` (create, revise, check, issue with its
  `INV-` number, void, edit, refused while settled), the replay of invoices, migrations 000109 and 000110. Tests:
  `TestInvoicesPostThroughThePostingService`, `TestPostInvoiceInTheBaseCurrency`, `TestPostInvoiceInAnotherCurrency`,
  `TestEveryPostedInvoiceBalances`, `TestLineDiscountedPinnedCases`. **Left:** the approvals (FM-2, FM-1, SOD-3) with
  the API, task 1.4, and the delivery and settlement states with sending and receipts)
  (24 Oct 2026, PRD-001 rev 0.117, the rest with task 1.4: the approvals on "Invoices, credit notes" (object `invoice`,
  `INVOICE-10` to `INVOICE-12` and `INVOICE-14`), the five states in the API (delivery `not_sent` until 5.x, settlement
  from allocations until 2.1 records receipts); the codes renamed `INVOICE-nn`, as `INV-nn` are the invariants' IDs)
- [x] 1.4 The API: list, get, create, revise, issue, void, edit and preview invoices, with ETags, idempotency,
  attachments and the matrix rows; Spectral clean. Tests `// req: FR-INT-01`
  (24 Oct 2026, PRD-001 rev 0.117: `internal/api/invoices.go` and `invoice_evidence.go`, fourteen operations under
  `/v1/invoices`, issuing as `issueInvoice` with `issue-approval`; issuing notifies those who approve invoices. Tests:
  `TestInvoicesThroughTheAPI`, `TestInvoiceDraftsAndGSTThroughTheAPI`, `TestEvidenceOnAnInvoice`, the matrix rows,
  `TestEverySequenceIsInTheContract`)
- [x] 1.5 Spreads: a spread line to 2400 and its monthly releases by the scheduler (`ALLOC-EVEN`), reversed with a
  void or edit. Tests `// req: ledger/posting-rules Spreading an invoice or bill`
  (24 Oct 2026, PRD-001 rev 0.118: `ledger.Release` shared by bills and invoices, `ledger.InvoiceReleases` and
  `invoice_release@1` with its replay golden; `journal.Releases` releases invoices as `scheduler:invoices.releases` by
  the hourly job `invoices.releases` and at issue and edit, reversed with a void or an edit; migration 000111; getInvoice
  lists the releases. Tests: `TestInvoiceReleasesAreTheSpreadSplitByMonth`, `TestPostInvoiceRelease`,
  `TestInvoiceReleasesProperties`, `TestEveryInvoiceReleaseRuleVersionReplaysItsGoldenEntries`,
  `TestTheSchedulerReleasesASpreadInvoice`, `TestAttackAReleaseIsNotTheInvoicesEntry`, `TestSpreadInvoicesThroughTheAPI`)
- [x] 1.6 The AR sub-ledger equal to 1100 (INV-07), shown at the close; AR ageing by customer and currency with the
  unrealised FX exposure. Tests `// req: INV-07`, `// req: FR-AR-10`
  (24 Oct 2026, PRD-001 rev 0.119: `internal/domain/receivables` (the open invoices, the reconciliation, the ageing and
  the exposure), the ageing and reconciliation shared with payables in `internal/domain/subledger`, the close's
  `receivables` and `receivables-unsupported` checks, 1100 revalued invoice by invoice (`journal.SubLedgers`), and
  `getReceivables`. Tests: `TestTheARSubLedgerReconcilesWith1100`, `TestReceivablesThroughTheAPI`, the matrix row)

## 2. Receipts, allocations, tolerances and write-offs

- [x] 2.1 Customer receipts recorded by hand (the owner's decision of 24 Oct 2026): from a money account against
  one or more invoices, realised FX per allocation, unapplied amounts and overpayments as customer credits,
  voided by reversal; the allocations generalised from the bills'. Tests
  `// req: ledger/posting-rules Customer and supplier payments`, `// req: FR-AR-18`, G4-045, G4-054
  (24 Oct 2026, PRD-001 rev 0.120: `internal/domain/receipt`, `customer_receipt@1` (`bill_payment@1`'s legs on 1100,
  negated) with its replay golden, `journal.CustomerReceipts`, migrations 000112 and 000113, receipts' credits in the AR
  sub-ledger, `/v1/customer-receipts` with the approval object `customer_receipt`. Tests:
  `TestPostCustomerReceiptInTheBaseCurrency`, `TestPostCustomerReceiptRealisesFX`, `TestTheReceiptRuleRefusesWhatCannotPost`,
  `TestCustomerReceiptsProperties`, `TestEveryCustomerReceiptRuleVersionReplaysItsGoldenEntries`,
  `TestCustomerReceiptsSettleInvoices`, `TestCustomerReceiptsThroughTheAPI`, the matrix rows. G4-054's second receipt
  is refused (RECEIPT-05) rather than kept unapplied; G4-045 waits for credit applications and bounce, tasks 3.1 and 5.x)
- [x] 2.2 Settlement tolerances after realised FX, for invoices and bills, within the caps per document and month
  (provisional until question 32). Tests `// req: ledger/posting-rules Settlement tolerances`,
  `// req: ledger/posting-rules Settlement difference on an invoice or bill`
  (24 Oct 2026, PRD-001 rev 0.121: a part recorded `in_full` in `bill_payment@1` and `customer_receipt@1` clears the
  difference on its own 7030 line; `subledger.Tolerance` reads the `bank.tolerance_*` settings and the month's
  allocations; the allocation's `tolerance` (migrations 000114 and 000115); the close's `tolerances` check (C.2 #49).
  Tests: `TestASettlementDifferenceClearsWithinItsOwnLine`, `TestSettlementDifferencesWithinTolerance`,
  `TestSettlementDifferencesThroughTheAPI`)
- [x] 2.3 Write-offs: reason-coded, with evidence, approved by someone other than whoever recorded the receipt
  (SOD-9), posted to the reason's account; a bad debt tagged for the relief review. Tests `// req: FR-AR-17`,
  `// req: ledger/posting-rules Bad-debt write-off`, `// req: SOD-9`
  (24 Oct 2026, PRD-001 rev 0.122: `internal/domain/writeoff`, `write_off@1` with its replay golden, `journal.WriteOffs`
  (draft, post on approval, void), migrations 000116 and 000117, seven operations under `/v1/write-offs` with the
  approval object `write_off`; the invoice's `written_off` state. The reasons' default accounts (4095, 7030) and a bad
  debt's named account are `[verify]`; evidence files on write-offs wait for FR-DOC-06. Tests: `TestPostWriteOff`,
  `TestTheWriteOffRuleRefusesWhatCannotPost`, `TestEveryWriteOffRuleVersionReplaysItsGoldenEntries`,
  `TestWriteOffsSettleWhatIsLeft`, `TestWriteOffsThroughTheAPI`, the matrix rows)

## 3. Credit notes, refunds and deposits

- [x] 3.1 Credit notes linked to an invoice in full or part, or standalone, posting the reverse of its legs;
  applied by allocation; refunded from a money account. Tests `// req: FR-AR-04`,
  `// req: ledger/posting-rules Credit documents and opening items`
  (In part, 24 Oct 2026, PRD-001 rev 0.123: the credit note itself: the document `credit_note` (`CN-`) through the
  invoice API with `kind`, `credit_note@1` with its replay golden, the link to the credited invoice (INVOICE-15) with
  GST at that invoice's date `[verify]`, credit notes as customer credits in the AR sub-ledger, migrations 000118 and
  000119. Tests: `TestPostCreditNoteReversesAnInvoicesLegs`, `TestACreditNoteIsAnInvoiceNegated`,
  `TestEveryCreditNoteRuleVersionReplaysItsGoldenEntries`, `TestCreditNotesPostThroughThePostingService`,
  `TestCreditNotesThroughTheAPI`. Still to come: applying customer credits to invoices, and customer refunds.)
  (In part, 24 Oct 2026, PRD-001 rev 0.124: applying customer credits (credit notes and receipts' unapplied amounts)
  to invoices: `internal/domain/customercredit`, `customer_credit_application@1` with its replay golden,
  `journal.CustomerCreditApplications` (apply, void), migrations 000120 and 000121, four operations under
  `/v1/customer-credit-applications` with the approval object `customer_credit_application`. Tests:
  `TestPostCustomerCreditApplication`, `TestPostCustomerCreditApplicationRefuses`,
  `TestPostCustomerCreditApplicationProperties`, `TestEveryCustomerCreditApplicationRuleVersionReplaysItsGoldenEntries`,
  `TestCustomerCreditsApplyToInvoices`, `TestCustomerCreditsThroughTheAPI`, the matrix rows. Still to come: customer
  refunds.)
  (24 Oct 2026, PRD-001 rev 0.125: refunds to customers: `customer_refund@1` with its replay golden,
  `journal.CustomerRefunds` (record, void), migrations 000122 and 000123, four operations under `/v1/customer-refunds`
  with the approval object `customer_refund`. Tests: `TestPostCustomerRefund`, `TestACustomerRefundIsAReceiptNegated`,
  `TestEveryCustomerRefundRuleVersionReplaysItsGoldenEntries`, `TestCustomerRefundsSettleCredits`,
  `TestCustomerRefundsThroughTheAPI`, the matrix rows. The oracle posts none of 3.1 yet: task 9.1.)
- [x] 3.2 Deposits and retainers to 2410 (role `customer-deposits`), applied to invoices; security deposits without
  GST; a part-payment deposit's GST when GST-registered. Tests `// req: FR-AR-09`,
  `// req: ledger/posting-rules Customer refunds and deposits`
  (In part, 24 Oct 2026, PRD-001 rev 0.126, on the owner's decisions of 24 Oct 2026: a deposit is deducted on the final
  invoice, and may be in any currency at the rate it came in. Receiving built: `internal/domain/deposit`,
  `customer_deposit@1` with its replay golden, `RND-GST-INCL` (`rounding.GSTIncl`), the role `customer-deposits` on 2410,
  `journal.CustomerDeposits` (record, void), migrations 000124 and 000125, five operations under `/v1/customer-deposits`
  with the approval object `customer_deposit`. Tests: `TestPostCustomerDeposit`, `TestPostCustomerDepositRefuses`,
  `TestPostCustomerDepositProperties`, `TestEveryCustomerDepositRuleVersionReplaysItsGoldenEntries`,
  `TestGSTInclPinnedCases`, `TestGSTInclProperties`, `TestCustomerDepositsPostThroughThePostingService`,
  `TestCustomerDepositsThroughTheAPI`, the matrix rows. Still to come: deducting on the invoice, refunds of deposits,
  and the 2410 reconciliation.)
  (In part, 24 Oct 2026, PRD-001 rev 0.127: deducting on the final invoice: the invoice's `deposits`, `invoice@1`'s new
  input (two replay cases added), `RND-DEPOSIT` (`rounding.DepositPart`, `deposit.Open.Use`), the allocation's `tax`,
  migrations 000126 and 000127; voiding or editing an invoice reopens its deposits. Tests: `TestAnInvoiceDeductsDeposits`,
  `TestAnInvoiceWithDepositsBalances`, `TestDepositPartPinnedCases`, `TestDepositPartProperties`,
  `TestInvoicesDeductDeposits`, `TestInvoicesDeductDepositsThroughTheAPI`. Still to come: refunds of deposits and the
  2410 reconciliation.)
  (24 Oct 2026, PRD-001 rev 0.128: refunds of deposits (`deposit_refund@1` with its replay golden, through
  `/v1/customer-refunds`), the deposits sub-ledger (`deposit.Held`, `deposit.Reconcile`) and the close checks
  `deposits` and `deposits-unsupported` (INV-07, C.2 #2). Tests: `TestPostDepositRefund`,
  `TestDepositsAreRefundedAndReconciled`, `TestDepositsAreRefundedThroughTheAPI`. The oracle posts none of 3.2 yet:
  task 9.1.)

## 4. Quotes and sales orders

- [x] 4.1 Quotes and sales orders: lines, validity, statuses, the pipeline value, nothing posted; expiry by the
  scheduler. Tests `// req: FR-AR-01`
  (In part, 24 Oct 2026, PRD-001 rev 0.129: `internal/domain/quote`, `journal.Quotes` (create, revise, send, cancel),
  migrations 000128 and 000129, seven operations under `/v1/quotes` with the pipeline. Tests:
  `TestQuotesAreSentAndCancelled`, `TestQuotesThroughTheAPI`, the matrix rows. Still to come: the customer's answer
  (accepted, declined) and expiry by the scheduler.)
  (24 Oct 2026, PRD-001 rev 0.130: the customer's answer and expiry: the table `quote_outcome` (migration 000130),
  `Quotes.Accept`, `Decline`, `ExpireDue`, the daily job `quotes.expiry`, `acceptQuote` and `declineQuote`. Tests:
  `TestQuotesAreAnsweredAndExpire`, `TestQuotesAreAnsweredThroughTheAPI`, the matrix rows.)
- [x] 4.2 Conversion to a draft invoice in full or in part, linked both ways. Tests `// req: FR-AR-02`
  (24 Oct 2026, PRD-001 rev 0.131: `Quotes.Invoice`, `quote.Invoicing`, `quote.CheckInvoiceLines` in the invoice
  checks, invoice lines naming their quote line (`lines[].quote`), the query `InvoicesOfQuote`, `invoiceQuote`, the
  quote's `invoicing` and `invoices`, the status invoiced and the pipeline at what is left. Tests:
  `TestQuotesAreInvoiced`, `TestQuotesAreInvoicedThroughTheAPI`, the matrix row.)

## 5. Sending, the online link and tax-invoice particulars

- [x] 5.1 The tax invoice's particulars in the invoice template: the IRAS particulars, SGD amounts for a foreign
  currency, "Tax invoice" only once GST-registered, the 30-day ⚠, the simplified tax invoice. Tests
  `// req: FR-AR-05`
  (24 Oct 2026, PRD-001 rev 0.132: `salesdoc.InvoiceData`, `ledger.BaseTotalsOf`, `invoice.Late` (INVOICE-LATE),
  the setting `sales.simplified_tax_invoices` (the owner's decision: off by default), `getInvoiceDocument`. Tests:
  `TestInvoicesFillTheirDocument`, `TestInvoicesRenderAsTaxInvoices`, `TestBaseTotalsOfAForeignInvoice`, the matrix
  row. Credit notes' layout, with the reason IRAS requires, follows with sending.)
- [x] 5.2 Sending quotes and invoices: the issued version's PDF, the email template, the invoices sender, the
  delivery state from the providers' events. Tests `// req: FR-AR-06`, `// req: FR-AR-16`
  (24 Oct 2026, PRD-001 rev 0.133: `emailInvoice`, `emailQuote`, `salesdoc.QuoteData`, `salesdoc.Delivery`, the table
  `document_send` (migrations 000131 and 000132), evidence of source `generated`, the email purposes `invoice` and
  `quote`, attachments in the email port and its three adapters, `email.QueueWith` and `evidence.Files`. Tests:
  `TestInvoicesAndQuotesAreEmailed`, `TestASendIsOfAnIssuedInvoicesCurrentVersion`, `TestAMessageCarriesItsEvidenceFiles`,
  `TestDeliveryFollowsTheLatestSend`, the adapters' attachment tests, the matrix rows. The link in the email follows
  with task 5.3.)
- [x] 5.3 The online link: a hashed 256-bit token per version, the public read endpoint, viewed recorded once, the
  quote accepted or declined as evidence, expiry and revocation; its threat model. Tests `// req: FR-AR-06`,
  `// req: FR-AR-16`
  (24 Oct 2026, PRD-001 rev 0.134: a link per email rather than per version, so each email's link is revoked on its
  own; the token in the URL's fragment and the `Fyniq-Link` header, never a path; `document_link`,
  `document_link_token`, `document_link_route` (migration 000133); `salesdoc.NewToken`, `TokenHash`, `Live`; the
  public operations and `revokeDocumentLink`; `docs/security/threat-models/document-links.md`. Tests:
  `TestDocumentLinksOpenTheirDocumentOnly`, `TestAQuoteIsAnsweredThroughItsLink`, `TestDocumentLinksAreRateLimited`,
  `TestALinkChangesOnlyInItsViewAndRevocation`, `TestALinkTokenIsRandomAndKeptHashed`, `TestALinkIsLiveUntilItEnds`, the
  matrix rows. The public page itself comes with the screens, task 8.1.)

## 6. Recurring invoices

- [x] 6.1 Recurring invoices made by the scheduler as drafts, or issued and sent once a second person approved
  their content (the owner's decision of 24 Oct 2026), listed for the weekly review; month-end dates. Tests
  `// req: FR-AR-07`, `// req: FM-3`
  (In part, 24 Oct 2026, PRD-001 rev 0.135: making drafts, with weekly and fortnightly schedules beside the monthly
  ones: `recurring_invoice`, `create_scheduled_invoice_draft` (migration 000134), `journal.Invoices.CreateRecurring`
  and `MakeDue`, the job `invoices.recurring`, `/v1/recurring-invoices`, SOD-3 on its drafts. Tests:
  `TestRecurringInvoicesMakeDraftsTheSchedulerCannotIssue`, `TestRecurringInvoicesThroughTheAPI`,
  `TestARecurringInvoicesAuthorDoesNotApproveItsDrafts`, `TestRecurringOccurrences`, the matrix rows.)
  (25 Oct 2026, PRD-001 rev 0.136: issue and send, done by the worker and fenced by the database (the owner's
  decision of 24 Oct 2026): the recurring invoice's `mode` and approved version, the approval object
  `recurring_invoice` and `requestRecurringInvoiceApproval`, the review kind `scheduled_issue`, `salesmail.Sender`
  shared by `emailInvoice` and the job, the worker's fenced grants (FY201, FY202; migrations 000135 and 000136);
  `docs/security/threat-models/scheduled-invoices.md`. Tests: `TestTheSchedulerIssuesOnlyApprovedRecurringInvoices`,
  `TestARecurringInvoiceIsApprovedBeforeTheSchedulerIssuesIt`, the matrix row.)

## 7. Reminders and statements

- [x] 7.1 Reminder schedules per customer group with their templates; a due reminder waits in the Inbox to be sent
  or skipped until bank import (the owner's decision of 24 Oct 2026); quotes about to expire. Tests
  `// req: FR-AR-08`
  (25 Oct 2026, PRD-001 rev 0.137: `customer_group` and `sales_reminder` (FY203; migrations 000137 and 000138), the
  contact's `customer_group_id`, `salesreminder.Due`, `salesmail.Sender.PrepareReminder` (the PDF already sent, the
  reminders sender), the customer group operations and `listSalesReminders`, `sendSalesReminder`, `skipSalesReminder`;
  threats T15 and T16 in `email-delivery.md`. Tests: `TestRemindersWaitForAPerson`, `TestARemindersStepIsDecidedOnce`,
  the matrix rows. The Inbox screen comes with 8.1; automatic reminders with bank import.)
- [x] 7.2 Statements of account by date range, adding up, as a PDF sent like an invoice. Tests `// req: FR-AR-08`
  (25 Oct 2026, PRD-001 rev 0.138: `internal/domain/statement` (`Of`, `Make`, `Read`), the document type `statement`
  (ST-; FY204; migrations 000139 and 000140), `salesdoc.StatementData`, `salesmail.Statement`, the statement template's
  ageing rows; `getCustomerStatement`, `createStatement`, `listStatements`, `getStatement`, `emailStatement`. Tests:
  `TestAStatementAddsUpAndIsSentAsMade`, `TestAStatementStaysAsItWasMade`, `TestAStatementsPeriod`,
  `TestAStatementsLinesInWords`, the matrix rows.)

## 8. Screens

- [ ] 8.1 Build invoice-editor, invoice-record, quotes-list, record-payment, recurring-documents,
  statements-reminders and customer-document-view as designed. Add help keys and `docs/user-guide/` pages
  compiled into `kb/`
  (In part, 25 Oct 2026: invoice-editor and invoice-record. **Sales › Invoices** in the navigation (Basic), the list
  of invoices and credit notes with their states, and the invoice page: a new invoice or credit note, a draft its
  preparer saves and an AR specialist or Accountant issues (or asks approval for), and an issued one, emailed,
  voided or edited with a reason; its checks, totals, emails, ledger impact and evidence. `getSalesGSTCodes` gives
  the line's codes. Help keys, `docs/user-guide/invoices.md` and `kb/pages/task/sales-invoices.md`. Tests:
  `src/screens/sales/invoices.test.tsx`, the matrix row.)
  (In part, 25 Oct 2026: quotes-list. **Sales › Quotes and orders**: the list of quotes or sales orders with their
  status and email, the pipeline, and the quote page: a draft saved and sent, emailed, the customer's answer
  recorded, cancelled with a reason, and an accepted one made into a draft invoice; its invoicing line by line.
  `docs/user-guide/quotes.md` and `kb/pages/task/sales-quotes.md`. Tests: `src/screens/sales/quotes.test.tsx`.)
  (In part, 25 Oct 2026: record-payment. **Record a payment** on an issued invoice: the date, the account it came
  into and what the customer paid; what is beyond the invoice stays as their credit, and a small shortfall settles in
  full within the tolerance. Not yet: applying one receipt across several invoices. Tests: the receipt cases in
  `src/screens/sales/invoices.test.tsx`.)
  (In part, 25 Oct 2026: recurring-documents, the invoices' half. **Sales › Recurring**: the recurring invoices with
  their schedule (weekly to yearly), next date, what the scheduler does and status; the page sets the name, schedule,
  customer, lines and mode, checks it without saving, pauses and resumes it, and asks for approval of the content to
  issue and send. Recurring bills keep their own page under Purchases. Tests: `src/screens/sales/recurring.test.tsx`.)
  (Still to come: statements-reminders and customer-document-view.)

## 9. The oracle and finishing

- [ ] 9.1 The invoice, receipt, credit, deposit, write-off and spread rules in the independent oracle, with scenario
  shapes and differential tests: an oracle session (QA-ENG-01), never a build session
- [ ] 9.2 Finish with `make check` and `make test`
