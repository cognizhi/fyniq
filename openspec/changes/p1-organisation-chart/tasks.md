# Tasks

## 1. Data model claimed

- [ ] 1.1 Map each of the 14 `data/data-model` requirements to the P0 test that already proves it, and tag the test (`// req: data/data-model <title>`, one tag per requirement). List what is missing in this file
- [ ] 1.2 Add what is missing:
  - effective-dated reference data with `WITHOUT OVERLAPS` keys (`btree_gist`);
  - the explicit list of global tables, which application roles may only read;
  - version counters on mutable records.

  Attack tests in `internal/adapters/postgres` attempt an overlap, a write to a global table and a stale version

## 2. Organisation settings

- [ ] 2.1 Profile, financial year, base currency, time zone and GST status with effective dates and the accounting basis. Tests `// req: FR-ORG-01`, `// req: FR-ORG-02` and `// req: FR-ORG-03`, including a short or long first year, and refusing cash basis until its rules are pinned
- [ ] 2.2 Defaults and gap-free number sequences per document type, locked in the posting transaction. Test `// req: FR-ORG-04` races two postings on independent connections and finds no gap and no repeat
- [ ] 2.3 Feature switches, region and retention, and the recommended defaults with reset and effective dates. Tests `// req: FR-ORG-06`, `// req: FR-ORG-07` and `// req: FR-ORG-08`; changes under FM-1 wait for `p1-approval-engine`
- [ ] 2.4 Logo and brand: SVG and PNG, light and dark, the icon and the accent colour, through the blob-store port with SVG sanitising. Test `// req: FR-ORG-09` covers G4-099's hostile files, and an issued document keeps its logo version

## 3. Chart of accounts and dimensions

- [ ] 3.1 Pre-load the workbook's 83-account chart from data, with every attribute. Tests `// req: FR-COA-01` and `// req: FR-COA-02`; the accountant reviews the data file
- [ ] 3.2 System accounts by role, deactivation, the approved journal-preserving merge, and the hierarchy with posting only to leaves. Tests `// req: FR-COA-03`, `// req: FR-COA-04` and `// req: FR-COA-05`, each attacked as the application role
- [ ] 3.3 Dimensions and dimension sets on journal lines. The balances cache is keyed by dimension set, and the INV-08 rebuild and the oracle's half are extended. Tests `// req: FR-DIM-01` and `// req: FR-DIM-02`; the differential gate is green

## 4. Contacts and items

- [ ] 4.1 Contacts with roles and details, live balances, financial-year totals, ageing and a timeline. Tests `// req: FR-CT-01` and `// req: FR-CT-02` cover the balances P0's ledger holds
- [ ] 4.2 Vendor bank details as versioned high-risk data. A pending version freezes payments and notifies every founder through the outbox; values are masked in logs. Test `// req: FR-CT-03`; approval waits for `p1-approval-engine`, and until then the change is refused, never applied
- [ ] 4.3 Duplicate detection on name, UEN, email and bank account (`// req: FR-CT-04`), and items with prices per currency (`// req: FR-ITM-01`)

## 5. API and screens

- [ ] 5.1 Add every operation to the contract with `make openapi`, then regenerate the client; the authorisation-matrix rows for each new endpoint pass (QA-SEC-03)
- [ ] 5.2 Build the screens as designed:
  - chart-of-accounts, contact-record, contacts-list, items, logo-brand and organisation-settings;
  - help keys, tooltips and a page in `docs/user-guide/` for each, compiled into `kb/` (`make kb-index`, `make kb-lint`).
- [ ] 5.3 Finish with `make check` and `make test`
