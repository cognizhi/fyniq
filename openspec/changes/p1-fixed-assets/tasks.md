# Tasks

## 1. The register

- [x] 1.1 Migrations: the document types `fixed_asset` (FA-) and `asset_disposal`; the asset's source guard (one asset
  per posted bill line or bank-transaction part on an asset account); the approval object `asset_disposal.void`;
  `bank_match_posting` accepts `asset_disposal`; widened checks NOT VALID and validated after; squawk clean; goldens
  updated (design D1, D4, D7).
  - Tests: a second asset on one source line refused; an asset on a line of another account refused (QA-ENG-05).
    `// req: FR-FA-01`
- [x] 1.2 `internal/domain/asset`: the asset's content by version (design D2: cost, residual, life in months, method,
  start, carried, from; its source or its opening figures), read and checked with every problem at once (ENG-23); the
  category as the asset account (design D1); the settings `assets.disposal_account` (7050) and
  `assets.disposal_clearing_account` (unset), with their guide sections compiled into `kb/`.
  - Tests: a life of zero, a residual above the cost, a start before the purchase month refused (B.4: the start is the
    purchase month or a later depreciate-from month); an opening asset carries its opening accumulated depreciation
    from the books start. `// req: FR-FA-01`
- [x] 1.3 The API: register (from a source line, or an opening asset), list, read and change an asset (a change is the
  next version, applied to the months not yet posted), under Appendix D's "Fixed assets" (C/P; V to read), with the
  matrix rows (QA-SEC-03); `make openapi` first.
  - Tests: an asset registered from a bill line takes the line's base cost; from a foreign-currency bill, its RND-FX
    base; a Bookkeeper reads but does not register. `// req: FR-FA-01`

## 2. Depreciation

- [x] 2.1 `asset_depreciation@1` in the ledger (Dr 6610 · Cr 1590) and the schedule as a pure function of a version
  (design D2, D3): `ALLOC-EVEN` of cost − residual − carried over its months, the end at start + life − 1; the
  database refuses a depreciation entry that names no posted asset at its version; the replay reproduces it (ENG-06).
  - Tests: worked by hand: S$3,600 over 36 months is S$100 a month; S$1,000 over 3 months is 333.33, 333.34, 333.33
    (`ALLOC-EVEN`); an asset started before the books start posts from the books start, its earlier months in the
    opening balances; a change of life after 6 months spreads what is left over the new remaining months.
    `// req: FR-FA-02`, `// req: ledger/posting-rules Depreciation`
- [x] 2.2 `journal.Depreciation` and the job `assets.depreciation` (hourly, as `scheduler:assets.depreciation`): every
  month whose last day has come in the organisation's time zone posts, each in its own transaction; registering or
  changing an asset posts its months due at once; a month with no open period waits, with
  `fixed_asset.depreciation_waiting`; the asset shows each month posted, waiting or projected (ENG-05); PRD FR-AUT-01
  gains the job (a rev note).
  - Tests: a run twice posts once; a closed month waits and posts once it reopens; a future month is never posted.
    `// req: FR-FA-02`, `// req: FR-AUT-01`

## 3. Disposals

- [x] 3.1 `asset_disposal@1` (design D4): the share's accumulated depreciation off 1590, its cost off the asset
  account, the proceeds from a money account (with output GST when GST-registered, `RND-GST-INCL`) or a sales
  invoice's line on the clearing account, the gain or loss to `assets.disposal_account`; refused while a month before
  it waits; a partial disposal makes the asset's next version; bank import matches a money sale; the replay reproduces
  it.
  - Tests: worked by hand: a S$3,600 laptop after 12 months (S$1,200 accumulated) sold for S$2,000 is a loss of S$400;
    a write-off of the same is a loss of S$2,400; half of it sold on an invoice for S$1,500 clears the clearing account
    and gains S$300, and the other half depreciates on; the entry balances. `// req: FR-FA-03`,
    `// req: ledger/posting-rules Asset disposal`, `// req: ledger/fixed-assets Proceeds received or invoiced (rev 0.149)`
- [x] 3.2 The API: dispose, list and read disposals, void (while no later month posted; in Founder mode on the
  Controller's approval, FM-1), with the matrix rows.
  - Tests: a void restores the asset's previous version; a void in Founder mode is asked for and approved by another.
    `// req: FR-FA-03`, `// req: FM-1`

## 4. The register against the ledger and the prompt

- [x] 4.1 The register reconciled with the asset accounts and 1590 as at a day, through the balance service, shown at
  the close (design D6, INV-07); bills and categorised bank lines on an asset account at or above the capitalisation
  threshold, and not registered, show ⚠ `ASSET-UNREGISTERED`, and the register lists them.
  - Tests: the register equals the accounts after a purchase, a month's depreciation and a disposal; a manual journal
    to 1590 shows ⚠ at the close; a S$1,200 laptop bill shows the prompt, a S$900 one does not. `// req: INV-07`,
    `// req: ledger/fixed-assets Capitalisation threshold prompt`

## 5. The screen and the guide

- [x] 5.1 `fixed-assets` at `/accounting/assets` (Expert; docs/design/screens FixedAssets): the register with cost,
  accumulated depreciation and NBV; an asset's page with its schedule (posted, waiting, projected) and its source;
  register, change, dispose or write off; **Not in the register yet**; the prompt on the bill and bank-line screens;
  help keys and registry entries; component tests with axe. `// req: FR-FA-01`, `// req: FR-FA-02`, `// req: FR-FA-03`
- [x] 5.2 The guide page `accounting.fixed-assets` (`docs/user-guide/fixed-assets.md`), compiled into `kb/pages/`,
  `make kb-index` and `make kb-lint` clean; the screen checked in the dev stack against the real API.
  `// req: QA-UI-05`

## 6. Finish

- [x] 6.1 The threat model `docs/security/threat-models/fixed-assets.md` (a disposal hiding an asset taken, a
  depreciation posted twice or into a closed month, a register that disagrees with the ledger), each abuse case with
  its test; `make check`, `make test`, `make scan` and `make images` pass; mutation on the ledger at least 90%; a
  conventional commit with upgrade notes (name the clearing account before a sale on an invoice; check 7050).

## 7. The owner's decisions of 27 Oct 2026 (rev 0.151)

- [x] 7.1 PRD rev 0.151 and its specs; 1160 Asset disposal clearing in the standard chart, recommended by
  `assets.disposal_clearing_account` (design D7); the recommended mileage rates S$0.60 (car) and S$0.25 (motorcycle);
  the fixed-assets screen in the review register's accountant area, so it shows "Needs additional verification"
  (FR-UX-41); the guide sections compiled into `kb/`.
  - Tests: the standard chart loads 88 accounts with 1160; an invoiced sale clears 1160 with no setting changed; a
    car trip with no rate changed is S$0.60 a km. `// req: FR-COA-01`, `// req: FR-FA-03`, `// req: FR-AP-16`
- [x] 7.2 The default useful life on an asset account (design D8): the migration, the chart's rules and API, the
  standard chart's 36/60/36/36, the chart screen's field, and the register's form filling it.
  - Tests: an account outside the register's groups refuses a life (COA-11); the form fills 60 for 1520 and keeps a
    life typed. `// req: FR-COA-02`, `// req: ledger/fixed-assets The account's default useful life is offered`
- [ ] 7.3 Voiding an asset (design D9): the migration (released, the live-line index, FY235), the command with its
  refusals, the approval object in Founder mode, the API, the screen's Void, the guide and the threat model.
  - Tests: a void reverses October and November on their own dates and the line registers again; refused while a
    disposal stands, with a month in a closed period, and for a second void; FY235 refuses a release of a live asset's
    source and any delete; Founder mode asks for approval. `// req: FR-FA-01`, `// req: FR-GL-12`, `// req: FM-1`

## 8. The independent oracle

- [ ] 8.1 `asset_depreciation@1` and `asset_disposal@1` in the independent oracle, written from PRD §10.2 and FR-FA-02
  and FR-FA-03 in a session of its own with the other pending rule sets (the owner's decision of 27 Oct 2026; QA-ENG-01,
  QA-ENG-02), and the differential green.

## Workflow follow-up

- The accountant confirms the asset accounts' report groups, 7050, 1160, the default lives and the mileage rates (the
  review brief of 27 Oct 2026).
- FR-FA-04 (the movement schedule and the capital allowance schedule) and reducing balance follow in P2; the C.1 #22
  check line with `p1-checks`.
