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
- [ ] 3.2 Deposits and retainers to 2410 (role `customer-deposits`), applied to invoices; security deposits without
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

## 4. Quotes and sales orders

- [ ] 4.1 Quotes and sales orders: lines, validity, statuses, the pipeline value, nothing posted; expiry by the
  scheduler. Tests `// req: FR-AR-01`
- [ ] 4.2 Conversion to a draft invoice in full or in part, linked both ways. Tests `// req: FR-AR-02`

## 5. Sending, the online link and tax-invoice particulars

- [ ] 5.1 The tax invoice's particulars in the invoice template: the IRAS particulars, SGD amounts for a foreign
  currency, "Tax invoice" only once GST-registered, the 30-day ⚠, the simplified tax invoice. Tests
  `// req: FR-AR-05`
- [ ] 5.2 Sending quotes and invoices: the issued version's PDF, the email template, the invoices sender, the
  delivery state from the providers' events. Tests `// req: FR-AR-06`, `// req: FR-AR-16`
- [ ] 5.3 The online link: a hashed 256-bit token per version, the public read endpoint, viewed recorded once, the
  quote accepted or declined as evidence, expiry and revocation; its threat model. Tests `// req: FR-AR-06`,
  `// req: FR-AR-16`

## 6. Recurring invoices

- [ ] 6.1 Recurring invoices made by the scheduler as drafts, or issued and sent once a second person approved
  their content (the owner's decision of 24 Oct 2026), listed for the weekly review; month-end dates. Tests
  `// req: FR-AR-07`, `// req: FM-3`

## 7. Reminders and statements

- [ ] 7.1 Reminder schedules per customer group with their templates; a due reminder waits in the Inbox to be sent
  or skipped until bank import (the owner's decision of 24 Oct 2026); quotes about to expire. Tests
  `// req: FR-AR-08`
- [ ] 7.2 Statements of account by date range, adding up, as a PDF sent like an invoice. Tests `// req: FR-AR-08`

## 8. Screens

- [ ] 8.1 Build invoice-editor, invoice-record, quotes-list, record-payment, recurring-documents,
  statements-reminders and customer-document-view as designed. Add help keys and `docs/user-guide/` pages
  compiled into `kb/`

## 9. The oracle and finishing

- [ ] 9.1 The invoice, receipt, credit, deposit, write-off and spread rules in the independent oracle, with scenario
  shapes and differential tests: an oracle session (QA-ENG-01), never a build session
- [ ] 9.2 Finish with `make check` and `make test`
