# Tasks

Each slice carries its migration, attack tests, API with authorisation-matrix rows, and the pinned or hand-worked tests behind
every figure. Load `fyniq-sg-statutory`, `fyniq-feature-change` and `fyniq-api-integrations` first.

## 1. Decisions and specs

- [ ] 1.1 PRD-001 rev 0.480 records the decisions above; the specs `sales/receivables`, `purchases/payables` and
  `tax/gst-and-filings` are updated and stamped. `// req: FR-AR-12`, `// req: FR-AP-05`, `// req: FR-TAX-10`, `// req: FR-TAX-11`

## 2. The documents

- [ ] 2.1 `invoicenow.Build` of an invoice and a credit note into PINT-SG UBL and `invoicenow.Check` of the rules in D2, with
  hand-worked documents (SGD, USD with the accounting currency, ZR, a credit note with its reason). `// req: FR-AR-12`
- [ ] 2.2 `invoicenow.Parse` of a received invoice into a draft bill's fields. `// req: FR-AP-05`

## 3. The port

- [ ] 3.1 `ports.AccessPoint`, the local adapter and `connectors/peppol-access-point/` with its contract fixtures; the manifest is
  valid and the adapter answers every fixture. **[live-verify]** against an access point's sandbox. `// req: FR-AR-12`

## 4. The submissions and the gate

- [ ] 4.1 Migration 001000: submissions, events, exclusions, contingencies, the two settings; attack tests (no second
  contingency approver the same as the requester, events append-only, tenancy). `// req: FR-TAX-10`, `// req: AUD-01`
- [ ] 4.2 The population and `Gate`, with a fixture for each exclusion reason and G4-030 to G4-034. `// req: FR-TAX-10`, `// req: FR-TAX-11`
- [ ] 4.3 The jobs `invoicenow.send` and `invoicenow.poll` on the outbox, and received documents into draft bills. `// req: FR-AP-05`

## 5. The API and the screen

- [ ] 5.1 Operations and matrix rows; `make openapi`. `// req: QA-SEC-03`
- [ ] 5.2 Screen `invoicenow-submissions`, registry tags (Expert), help keys, guide page, knowledge-base page. `// req: QA-UI-05`

## 6. At registration (question 2)

- [ ] 6.1 The F5 filing calls `Gate` (`p1-gst` 7.2). **[live-verify]** the access point's sandbox, then production.
