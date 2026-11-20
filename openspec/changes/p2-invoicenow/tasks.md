# Tasks

Each slice carries its migration, attack tests, API with authorisation-matrix rows, and the pinned or hand-worked tests behind
every figure. Load `fyniq-sg-statutory`, `fyniq-feature-change` and `fyniq-api-integrations` first.

## 1. Decisions and specs

- [x] 1.1 PRD-001 rev 0.480 records the decisions above; the specs `sales/receivables`, `purchases/payables` and
  `tax/gst-and-filings` are updated and stamped. `// req: FR-AR-12`, `// req: FR-AP-05`, `// req: FR-TAX-10`, `// req: FR-TAX-11`
  (21 Nov 2026, rev 0.480 and 0.481. Built: the decisions and the specs; 0.481 reconciles the contingency's roles with the GST return's rights.)

## 2. The documents

- [x] 2.1 `invoicenow.Build` of an invoice and a credit note into PINT-SG UBL and `invoicenow.Check` of the rules in D2, with
  hand-worked documents (SGD, USD with the accounting currency, ZR, a credit note with its reason). `// req: FR-AR-12`
  (Built: `internal/domain/invoicenow`, tests in `pint_test.go`: an SGD invoice, a USD invoice with its SGD totals, a credit note with its reason, each rule refused, the GST tolerance. **Not verified:** the identifiers `urn:peppol:bis:billing`, tax scheme `GST`, participant prefix `SGUEN`, credit note code 381 and the units; the golden file is made by `Build`. **Left:** `FromInvoice` refuses a foreign-currency invoice until its SGD totals are mapped from the posting.)

- [x] 2.2 `invoicenow.Parse` of a received invoice into a draft bill's fields. `// req: FR-AP-05`
  (Built: `Parse`, with the foreign-currency GST total and every refusal. The draft bill it feeds is task 4.3, Left.)

## 3. The port

- [x] 3.1 `ports.AccessPoint`, the local adapter and `contracts/connectors/peppol-access-point.contract.json` with its contract fixtures; the manifest is
  valid and the adapter answers every fixture. **[live-verify]** against an access point's sandbox. `// req: FR-AR-12`
  (Built: `ports.AccessPoint`, `local.AccessPoint`, `contracts/connectors/peppol-access-point.contract.json` and `porttest.AccessPointContract`. No manifest until a provider is chosen. **[live-verify]** stays.)

## 4. The submissions and the gate

- [x] 4.1 Migration 001000: submissions, events, exclusions, contingencies, the two settings; attack tests (no second
  contingency approver the same as the requester, events append-only, tenancy). `// req: FR-TAX-10`, `// req: AUD-01`
  (Built: migration 001000, the pack `sg-invoicenow@2026.1`, the settings, `TestAttackInvoiceNowKeepsItsRules`. Contingency evidence is text; evidence files follow.)

- [ ] 4.2 The population and `Gate`, with a fixture for each exclusion reason and G4-030 to G4-034. `// req: FR-TAX-10`, `// req: FR-TAX-11`
  (Built in part: `Population` and `Gate` over posted invoices, credit notes and bills, the pack's rule exclusions and the recorded ones, in `TestInvoiceNowFromTheGateToIRASsAcknowledgement` (a Peppol and an extracted sale, a reason, a contingency, an outage). **Left:** a fixture for each exclusion reason and purchases through the API; the gate is not yet called by an F5 filing, which does not exist (`p1-gst` 7.2).)

- [x] 4.3 The jobs `invoicenow.send` and `invoicenow.poll` on the outbox, and received documents into draft bills. `// req: FR-AP-05`

## 5. The API and the screen

- [x] 5.1 Operations and matrix rows; `make openapi`. `// req: QA-SEC-03`
  (Built: nine operations with matrix rows, `make openapi`, `make api-check`. The send is called by a person, not a job.)

- [x] 5.2 Screen `invoicenow-submissions`, registry tags (Expert), help keys, guide page, knowledge-base page. `// req: QA-UI-05`

## 6. At registration (question 2)

- [x] 6.1 The F5 filing calls `Gate` (`p1-gst` 7.2). **[live-verify]** the access point's sandbox, then production.

(21 Nov 2026, second pass. 4.3: `invoicenow.dispatch` every 15 minutes as the worker role (`DispatchAll`, tested with `workerStore` through an outage), and `Receive` and `DraftInbound` with migration 001001, run by a person with the bills right: a job cannot make bills, as the worker holds no such right, so there is no poll job. 5.2: Compliance › InvoiceNow, Expert, with its help keys, guide page and knowledge-base page; its test with axe. 6.1: `recordStatutoryFiling` refuses a GST F5 while the gate waits (API-INVOICENOW-01, `TestAnF5IsNotFiledWhileTheGateIsBlocked`). Foreign-currency invoices take their SGD totals from the posting (`TestAForeignCurrencyInvoiceIsSentWithItsSGDTotalsFromThePosting`).)

(21 Nov 2026, third pass, rev 0.568: the contingency's approval button and the exclusion form on the page (a pending request shows on the gate as `pending_contingency`), and a fixture for each exclusion reason (`TestEveryExclusionReasonLetsADocumentThroughAndWithdrawingBringsItBack`).)

**Left in this change:** purchases' solution-extracted and aggregated submissions (POS, staff claims, petty cash), the evidence files of a contingency (it is recorded as text), and the real access point `[live-verify]`. A deposit-bearing foreign invoice is refused until its SGD totals can be taken from the posting.
