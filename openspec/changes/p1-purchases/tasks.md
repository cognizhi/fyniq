# Tasks

Each slice is one pull request, merged when green. Tests carry `// req:` tags (QA-TRACE-01); every new
✖ or ⚠ message is triggered by a fixture (QA-RPT-02).

## 1. Bills: the document, its rule and its approval

- [x] 1.1 `RND-GST-EXCL` as a named policy, per GST-code subtotal (the per-line option an organisation
  setting), with property tests and mutation coverage. Tests `// req: RND-GST-EXCL`
  (22 Oct 2026: `rounding.GSTExcl`, #148; the setting `gst.round_per_line` with the bills)
- [x] 1.2 The bill document: versions, the payload of `design.md`, the supplier's number and ours (the
  bill sequence, numbered when it posts), due date from terms, the duplicate ⚠. Tests `// req: FR-AP-02`
  (22 Oct 2026: `internal/domain/bill`; document type and sequence `bill` (000075, 000076); input tax claimable
  for a purchase code with an input-tax box once the organisation is GST-registered on the bill's date, the
  facts of 9.2 to follow. Test `TestBillsPostThroughThePostingService`)
- [x] 1.3 The posting rule `bill@1`: net to the account or 1210, claimable GST as stated to 2200, the rest
  in the cost, 2010 the total; a foreign bill through `ForeignInputTax`; the ⚠ when the stated GST
  differs from `RND-GST-EXCL`; a replay golden. Tests `// req: ledger/posting-rules Posting a bill`,
  `// req: ledger/posting-rules Amounts of a posted bill`, G4-023 and G4-024
  (22 Oct 2026: `ledger.PostBill`, `testdata/replay/bill@1.golden`; the replay reads the rate and each line's
  claimed input tax from the stored entry, so a later rate or registration change never makes an old bill differ)
- [x] 1.4 Post, void and edit through the posting service: FM-2's threshold, FM-1 for a void, SOD-3, the
  journal's edit rule (FR-GL-12); the four states. Tests `// req: FR-AP-02`, `// req: FM-2`, `// req: SOD-3`
  (22 Oct 2026: `journal.Bills` posts, voids and edits in one transaction each; through the API the post and the
  void and the edit with their approvals, the approval engine's object `bill` (000077, 000078) and the four states, PRD
  rev 0.76. Test `TestBillsThroughTheAPI`)
- [ ] 1.5 The API: list, get, create, revise, post, void, edit and preview bills, with ETags, idempotency
  and the matrix rows; Spectral clean. Tests `// req: FR-INT-01`
  (22 Oct 2026, part: `listBills`, `getBill` (states, checks, ledger impact), `createBill`, `replaceBill`, `postBill`,
  `voidBill`, `editBill`, `requestBillPost`, `requestBillVoid`, `requestBillEdit`, each with Prefer: return=preview, and
  their matrix rows. **Left:** a bill's attachments, with the screens)
- [x] 1.6 The AP sub-ledger: open documents by supplier, equal to 2010 (INV-07), shown at the close.
  Tests `// req: INV-07`
  (23 Oct 2026, rev 0.78: `payables.Open` and `payables.Reconcile` from the documents, `balance.Control`, the close's
  `payables` (✖ only unexplained, the owner's decision) and `payables-unsupported` (⚠ opening balances and manual
  journals). Test `TestTheAPSubLedgerReconcilesWith2010`. **Left, with their own tasks:** the nightly verification
  (FR-CTL-05) and the ⚠ on a manual journal to a control account)

## 2. Bill screens

- [x] 2.1 Bills list and the bill editor (`bill-editor`): lines, GST codes, the plain summary in Basic and
  the ledger impact in Expert, the five frames in light and dark; help keys, `docs/user-guide/bills.md`
  compiled into `kb/`
  (22 Oct 2026: Purchases › Bills and the bill page; `getPurchaseGSTCodes` gives a line its codes from the pack in
  force on the bill's date. Tests `bills.test.tsx`)
- [ ] 2.2 The bill record (`bill-record`): state chips, actions, tabs (details, lines, ledger impact,
  attachments, activity, related)
  (22 Oct 2026, part: the posted bill on the same page, read-only, with its states, ledger impact, and Void and Edit,
  asked for in Founder mode. **Left:** the tabs for attachments, activity and related records, with attachments)

## 3. Payments recorded by hand (the owner's decision of 22 Oct 2026)

- [x] 3.1 `bill_payment`: a money account, a date, an amount, applied to the supplier's bills; Dr 2010 /
  Cr the money account; realised FX per bill (ENG-41); an unapplied amount kept as a supplier credit;
  void reopens. Tests `// req: ledger/posting-rules Customer and supplier payments` (supplier),
  `// req: ENG-41` (rev 0.77: `bill_payment@1`, allocations, the residual of a bill paid in parts on a
  base-only 2010 line, the API and the void's approval)
- [x] 3.2 Record payment on the bill record; the payment state; help and guide (the Payments card with
  each payment's void; the sample company's money accounts from the standard chart)

## 4. Vendor credits and refunds

- [x] 4.1 `vendor_credit` (the reverse of a bill's legs), applied to bills (realised FX across rates),
  refunded into a money account. Tests `// req: FR-AP-03`
  (23 Oct 2026, part, rev 0.80: the credit note, `vendor_credit@1`, `VC-`, the bill service and API with `kind`, the
  sub-ledger's credits. Tests `TestPostVendorCreditReversesABillsLegs`, `TestVendorCreditsPostAsABillReversed`,
  `TestVendorCreditsThroughTheAPI`. 23 Oct 2026, part, rev 0.81: applying credits (vendor credits and payments'
  unapplied amounts) by `credit_application@1`, `journal.CreditApplications`, the API and the void's approval. Tests
  `TestPostCreditApplication`, `TestCreditsAreAppliedToBills`, `TestCreditApplicationsThroughTheAPI`. 23 Oct 2026, rev
  0.82: refunds by `supplier_refund@1`, `journal.SupplierRefunds`, the API and the void's approval. Tests
  `TestPostSupplierRefund`, `TestSuppliersRefundCredits`, `TestSupplierRefundsThroughTheAPI`)
- [x] 4.2 The credit variant of the bill record: apply or refund
  (23 Oct 2026: New credit note and the Credit notes list on Purchases › Bills; on a posted credit note, Apply to a
  bill and Record refund; Part applied and Applied; its applications and refunds under Payments. Tests
  `bills.test.tsx` "A credit note"; checked in a browser at the five frames)

## 5. Ageing, the board and paying on time

- [x] 5.1 AP ageing, bills to pay by due date and the cash requirement for the next 30 days, from the
  open documents. Tests `// req: FR-AP-06`
  (23 Oct 2026, rev 0.79: `payables.Age`, `payables.ToPay`, `bill.Due`, `getPayables`, the summary on Purchases ›
  Bills. Tests `TestAgeingBucketsByDaysPastDue`, `TestBillsToPayAndTheCashTheyNeed`, `TestPayablesThroughTheAPI`)
- [ ] 5.2 The bills board (`bills-board`): by status, the due-date calendar, scheduling a payment date,
  early-payment discounts shown, reminders before due dates in the Inbox. Tests `// req: FR-AP-13`
  **Left:** remittance advice email (`p1-templates-email`); "in a batch" (FR-BNK-12)

## 6. Prepayment spreads (the owner's decision of 22 Oct 2026)

- [x] 6.1 A spread line posts to 1210; `bills.releases` posts each due month's release (`ALLOC-EVEN`) up
  to the current month, waiting while a month is closed; a void or an edit reverses the releases with
  the bill. Tests `// req: FR-GL-03`, `// req: ENG-05`, `// req: ledger/posting-rules Spreading an invoice or bill` (bill),
  G4-022's bill half. Closes `p1-general-ledger-close` 6.4 for bills
  (23 Oct 2026, PRD-001 rev 0.83, provisional: the spread line in `bill@1` on 1210 in the base currency;
  `bill_release@1` dated the first of its month, or the bill's date for months before it; releases already due
  post at once when the bill posts or is edited, the hourly `bills.releases` posts the rest and a month with no
  open period waits with an audit event; a void reverses them on its date, an edit on their own; migration 000089
  (FY172, and FY055/FY056 not counting a release as the bill's entry); `spread_months`, `spread_from` and
  `releases` in the API; the spread fields and **Spread over months** on the bill screen. Follows: the 1210
  reconciliation and a close check for releases not posted, INV-07)

## 7. Recurring bills (the owner's decision of 22 Oct 2026)

- [x] 7.1 Templates with versions; `create_scheduled_bill_draft` (EXECUTE for `fyniq_worker` only) and
  the job `bills.recurring`; drafts a person posts; threat model `scheduler-postings`. Tests
  `// req: FR-AP-09`, `// req: FR-AUT-02`, `// req: SEC-03`
  (23 Oct 2026, PRD-001 rev 0.84, provisional: migration 000090; `journal.Bills` recurring commands and `MakeDue`; the
  hourly `bills.recurring`; a draft keeps naming the recurring bill's version, and SOD-3 refuses its author on the post;
  the six `/v1/recurring-bills` operations; the threat model covers it and rev 0.83's releases)
- [x] 7.2 Recurring bills on the board and their editor
  (23 Oct 2026, PRD-001 rev 0.85, provisional: a Recurring tab on Purchases › Bills until recurring invoices join the
  designed shared page; the list and the recurring bill's page; the bill line editor shared with bills; the close checks
  `recurring-bills` and `recurring-bills-content`; the guide's "Recurring bills")

## 8. Purchase orders

- [ ] 8.1 `purchase_order`: lines, approval (FR-APR-01, object type `purchase_order`), statuses, convert
  to a draft bill. Tests `// req: FR-AP-01`
- [ ] 8.2 The purchase orders screen (`purchase-orders`)

## 9. Non-resident payments and input tax

- [ ] 9.1 A vendor's tax residence; a non-resident bill's nature of payment; the tax agent's decision per
  supplier and nature, with evidence, reused; ⚠ and the payment hold until decided; the withholding
  posting to 2330. Tests `// req: FR-AP-19`, G4-010. **Left:** the statutory-calendar task (`p1-gst`)
- [ ] 9.2 Input-tax eligibility per line from its facts and the organisation's registration at the bill
  date. Tests `// req: FR-AP-20`, G4-026, G4-027, G4-028

## 10. Finish

- [ ] 10.1 The bill, credit, payment and spread rules in the independent oracle, with scenario shapes and
  golden cases (an oracle session, QA-ENG-01; a build session never reads or edits `oracle/`)
- [ ] 10.2 The bills of `p1-multi-currency-fx` task 3.6: revalued one by one
- [ ] 10.3 `make check`, `make test` and `make test-e2e`; the five frames of each screen in a browser
