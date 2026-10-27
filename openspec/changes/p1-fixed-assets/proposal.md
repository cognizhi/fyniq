# Proposal

## Why

A start-up buys laptops, furniture and a fit-out, and the books must carry them as assets and spread their cost over
their lives. The workbook kept an asset register and posted depreciation from it, but left disposals to a manual
journal. Fixed assets are in the launch subset (PRD-001 §17, open question 25), and their dependency, `p1-purchases`,
has landed (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`, wave 2): bills and categorised bank lines already debit
the asset accounts (1510 to 1540), so what is missing is the register, its depreciation and its disposals.

## What Changes

- **The owner's decisions of 27 Oct 2026, taken interactively (PRD-001 rev 0.149):**
  - **a sale's proceeds** are money received into a money account, recorded with the disposal and matched by bank
    import, or a sales invoice to the buyer whose line credits the **disposal clearing account** (the setting
    `assets.disposal_clearing_account`, which the organisation names [proposed]), which the disposal clears; a
    write-off has no proceeds;
  - **the gain or loss** goes to one account, the setting `assets.disposal_account` (recommended 7050 Gain / (loss)
    on disposal of assets, under dual control) [verify in the CoA review].
- **The asset register (FR-FA-01):** an asset is registered from the bill line or categorised bank line that bought it
  (its cost is what that line put on the asset account, in the base currency), or, for one bought before the books
  start, with its opening accumulated depreciation. It carries its category (the asset account it sits on), cost,
  purchase date, useful life in months, residual value, method (straight-line in P1) and depreciation start. Saving a
  bill or bank line at or above the capitalisation threshold (`assets.capitalisation_threshold`, FR-ORG-04) prompts
  to register it. A change to the life or residual applies to the months not yet posted.
- **Monthly depreciation (FR-FA-02, posting rule "Depreciation", ENG-05, FR-AUT-01):** each month's charge is
  `ALLOC-EVEN` of cost less residual over the life in months, from the start month to start + life − 1, or the month
  before a disposal; months before the books start are in the opening balances and are not posted. The scheduler
  posts each month once its last day has come in the organisation's time zone, as its own principal
  (`scheduler:assets.depreciation`), Dr 6610 · Cr 1590, `asset_depreciation@1`; a month whose period is closed waits,
  with an audit event; future months are projections, never in the ledger.
- **Disposal and write-off (FR-FA-03, posting rule "Asset disposal"):** the whole asset or a share of its cost; the
  disposed share's accumulated depreciation to date leaves 1590 and its cost leaves the asset account; the proceeds
  are money received (Dr the money account, with output GST on them when the organisation is GST-registered) or the
  invoice's (Dr the clearing account); the difference is the gain or loss on `assets.disposal_account`
  (`asset_disposal@1`). The rest of a partly disposed asset depreciates over its remaining life. A disposal is voided
  by a reversal (FR-GL-12), in Founder mode on a second person's approval (FM-1).
- **The register against the ledger (INV-07):** the register's cost and accumulated depreciation, as at a day, equal
  the asset accounts and 1590 from the balance service, and the close shows any difference.
- **Screens:** `fixed-assets` (`/accounting/assets`, Expert): the register with cost, accumulated depreciation and
  NBV; an asset's page with its depreciation schedule (posted, waiting, projected) and its source; dispose or write
  off; the prompt on a bill or bank line at the threshold. Its help keys and the guide page `accounting.fixed-assets`,
  compiled into `kb/`.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs (`ledger/fixed-assets`, `ledger/posting-rules`); rev 0.149's
two decisions are recorded there; this change builds them (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Requirements built:** FR-FA-01, FR-FA-02, FR-FA-03, the posting rules "Depreciation" and "Asset disposal", and the
  fixed-asset part of INV-07; FR-AUT-01 gains the job `assets.depreciation`.
- **Screens (docs/design/screens/inventory.json):** `fixed-assets` (designed; the fragment `FixedAssets.html` exists).
  The bill and bank-line screens gain the capitalisation prompt.
- **Not built here:** reducing balance (P2); FR-FA-04's movement schedule and tax capital allowance schedule (S, P2);
  the C.1 #22 check line (`p1-checks`); dimensions on assets; asset revaluation and impairment (not in the PRD); the
  oracle's depreciation and disposal (a session of its own, as for every rule).
- **Who acts (Appendix D, "Fixed assets"):** the Accountant registers, changes and disposes (C/P); the Controller
  approves (A); the Owner, the Bookkeeper, the Auditor and the Tax agent view (V).
- **Settings:** `assets.disposal_account` and `assets.disposal_clearing_account` join the catalogue; the
  capitalisation threshold exists.
