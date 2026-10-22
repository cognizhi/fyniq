# Tasks

Each slice is one pull request, merged when green. Tests carry `// req:` tags (QA-TRACE-01); every new
✖ or ⚠ message is triggered by a fixture (QA-RPT-02).

## 1. Bills: the document, its rule and its approval

- [ ] 1.1 `RND-GST-EXCL` as a named policy, per GST-code subtotal (the per-line option an organisation
  setting), with property tests and mutation coverage. Tests `// req: ledger/money-and-rounding RND-GST-EXCL`
- [ ] 1.2 The bill document: versions, the payload of `design.md`, the supplier's number and ours (the
  bill sequence, numbered when it posts), due date from terms, the duplicate ⚠. Tests `// req: FR-AP-02`
- [ ] 1.3 The posting rule `bill@1`: net to the account or 1210, claimable GST as stated to 2200, the rest
  in the cost, 2010 the total; a foreign bill through `ForeignInputTax`; the ⚠ when the stated GST
  differs from `RND-GST-EXCL`; a replay golden. Tests `// req: ledger/posting-rules Posting a bill`,
  `// req: ledger/posting-rules Amounts of a posted bill`, G4-023 and G4-024
- [ ] 1.4 Post, void and edit through the posting service: FM-2's threshold, FM-1 for a void, SOD-3, the
  journal's edit rule (FR-GL-12); the four states. Tests `// req: FR-AP-02`, `// req: FM-2`, `// req: SOD-3`
- [ ] 1.5 The API: list, get, create, revise, post, void, edit and preview bills, with ETags, idempotency
  and the matrix rows; Spectral clean. Tests `// req: FR-INT-01`
- [ ] 1.6 The AP sub-ledger: open documents by supplier, equal to 2010 (INV-07), shown at the close.
  Tests `// req: INV-07`

## 2. Bill screens

- [ ] 2.1 Bills list and the bill editor (`bill-editor`): lines, GST codes, the plain summary in Basic and
  the ledger impact in Expert, the five frames in light and dark; help keys, `docs/user-guide/bills.md`
  compiled into `kb/`
- [ ] 2.2 The bill record (`bill-record`): state chips, actions, tabs (details, lines, ledger impact,
  attachments, activity, related)

## 3. Payments recorded by hand (the owner's decision of 22 Oct 2026)

- [ ] 3.1 `bill_payment`: a money account, a date, an amount, applied to the supplier's bills; Dr 2010 /
  Cr the money account; realised FX per bill (ENG-41); an unapplied amount kept as a supplier credit;
  void reopens. Tests `// req: ledger/posting-rules Customer and supplier payments` (supplier),
  `// req: ENG-41`
- [ ] 3.2 Record payment on the bill record; the payment state; help and guide

## 4. Vendor credits and refunds

- [ ] 4.1 `vendor_credit` (the reverse of a bill's legs), applied to bills (realised FX across rates),
  refunded into a money account. Tests `// req: FR-AP-03`
- [ ] 4.2 The credit variant of the bill record: apply or refund

## 5. Ageing, the board and paying on time

- [ ] 5.1 AP ageing, bills to pay by due date and the cash requirement for the next 30 days, from the
  open documents. Tests `// req: FR-AP-06`
- [ ] 5.2 The bills board (`bills-board`): by status, the due-date calendar, scheduling a payment date,
  early-payment discounts shown, reminders before due dates in the Inbox. Tests `// req: FR-AP-13`
  **Left:** remittance advice email (`p1-templates-email`); "in a batch" (FR-BNK-12)

## 6. Prepayment spreads (the owner's decision of 22 Oct 2026)

- [ ] 6.1 A spread line posts to 1210; `bills.releases` posts each due month's release (`ALLOC-EVEN`) up
  to the current month, waiting while a month is closed; a void or an edit reverses the releases with
  the bill. Tests `// req: FR-GL-03`, `// req: ENG-05`, `// req: ledger/posting-rules Spreading an invoice or bill` (bill),
  G4-022's bill half. Closes `p1-general-ledger-close` 6.4 for bills

## 7. Recurring bills (the owner's decision of 22 Oct 2026)

- [ ] 7.1 Templates with versions; `create_scheduled_bill_draft` (EXECUTE for `fyniq_worker` only) and
  the job `bills.recurring`; drafts a person posts; threat model `scheduler-postings`. Tests
  `// req: FR-AP-09`, `// req: FR-AUT-02`, `// req: SEC-03`
- [ ] 7.2 Recurring bills on the board and their editor

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
