# Threat model: the fixed asset register, its depreciation and disposals

| | |
|---|---|
| Feature and requirement IDs | The asset register, monthly depreciation by the scheduler, disposals and write-offs, the register against its accounts at the close: FR-FA-01, FR-FA-02, FR-FA-03, INV-07, FR-AUT-01, FR-AUT-02, FR-GL-12, FM-1 (PRD-001 rev 0.149 and 0.150; p1-fixed-assets design D1…D7) |
| Author, reviewer, date | Claude Code for the founders, 27 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Assets' names, costs, lives and residual values, their months of depreciation, and their disposals' proceeds, buyers' invoices and money accounts (confidential). An asset's name may name the person who uses it. No bank account numbers, payee bank details or secrets |
| Who acts | The Accountant, who registers and changes assets (C) and disposes of them (P) on Appendix D's "Fixed assets"; the Controller, who approves a disposal's void in Founder mode (A, FM-1); the Owner, the Bookkeeper, the Auditor and the Tax agent, who read (V); the platform job `assets.depreciation` as `scheduler:assets.depreciation` with the worker's database role `fyniq_worker`; the API's role `fyniq_api` |

## 1. What we are building

An asset is registered from the line of a posted bill, or of a categorised bank line, that put its cost on an asset
account; or, bought before the books start, with the cost and accumulated depreciation the opening balances carry.
Registering posts nothing. Each month, once its last day has come, the scheduler posts its depreciation
(`asset_depreciation@1`, Dr 6610 · Cr 1590) as its own entry, per asset, version and month. A disposal sells or writes
off the asset, whole or in part (`asset_disposal@1`): the share's accumulated depreciation off 1590, its cost off the asset
account, the proceeds in a money account (with output GST) or on the disposal clearing account through a sales invoice,
and the gain or loss to `assets.disposal_account`. A void reverses a disposal and restores the asset. At the close the
register is reconciled with each asset account and 1590.

Trust boundaries: **person ↔ API** (the Appendix D row, step-up, Founder mode's second person for a void); **API ↔
database** (`fyniq_api`, RLS per organisation, `fixed_asset_source_guard`, `journal_entry_asset_depreciation`, the ledger
guards); **scheduler ↔ database** (`fyniq_worker`, posting only what the asset's version schedules, up to the
organisation's today).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | API ↔ database | Tampering: one purchase registered as two assets, or a line of rent registered as an asset | Depreciation of a cost twice, or of a cost that was never an asset | Medium |
| T2 | scheduler ↔ database | Tampering: a month of depreciation posted twice, by two runs or by a run and a registration | 6610 and 1590 overstated | Medium |
| T3 | scheduler ↔ database | Tampering: depreciation posted into a closed month, or a month not ended yet | A closed month's figures changed; the future mixed with actuals (ENG-05) | Low |
| T4 | API ↔ database | Tampering: a month posted for an asset's earlier version, or for a document that is not an asset | Depreciation no asset explains | Low |
| T5 | person ↔ API | Repudiation and elevation: a disposal hides an asset taken (written off, or sold for little to a friend), and a founder voids or redoes it alone | An asset gone with nobody accountable; the loss buried in 7050 | Medium |
| T6 | person ↔ API | Tampering: a disposal recorded late leaves months of depreciation posted after it, or a void after what was left depreciated again | The asset depreciated past its disposal; 1590 and the register apart | Low |
| T7 | API ↔ database | Tampering: the register and the ledger disagree: a manual journal to 1510 or 1590, a bill voided after its asset was registered, a purchase never registered | The balance sheet's assets no document supports, or one never depreciated | Medium |
| T8 | person ↔ API | Tampering: one invoice's line used as the proceeds of two disposals | The clearing account cleared twice; a gain recorded twice | Low |
| T9 | person ↔ API | Information disclosure: another organisation's assets read; a person outside the row reads the register | Assets and who uses them disclosed | Low |
| T10 | person ↔ API | Spoofing: the AI or an integration registers, changes or disposes of an asset | A posting no person stands behind | Low |
| T11 | person ↔ API | Tampering: a retried request registers an asset or records a disposal twice | Two assets, or two disposals' entries | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | `fixed_asset_source` holds one asset per source line (`fixed_asset_source_line_once`), only a line of a posted bill or bank line at its current version, on that line's own account as the version saved it, an active account of the register that its entry, not reversed, debits; a source never changes or goes, for any role (FY235). The asset's cost is what the line's entry put on the account | `TestAssetSourcesKeepTheirLine` (`// req: QA-ENG-05`, `// asvs: V2.2.1`), `TestRegisteringFixedAssets` |
| T2 | A month's posting identity is the asset, its version and the month: the database refuses a second; each month posts in its own transaction with the asset locked, after checking it has not posted; registering and the job share the same command | `TestTheSchedulerDepreciatesTheRegister` (a run twice posts once; `// asvs: V2.3.1`) |
| T3 | The period guard refuses a posting in a closed period (INV-03); a month with no open period waits with an audit event and holds back the months after it; only a month whose last day has come in the organisation's time zone is posted | `TestTheSchedulerDepreciatesTheRegister` (`// req: ENG-05`, `// req: FR-AUT-01`) |
| T4 | `journal_entry_asset_depreciation` refuses a month that does not name a registered asset at its current version (FY236), for every role; the replay re-runs each month from its version | `TestTheSchedulerDepreciatesTheRegister` (`// req: QA-ENG-05`) |
| T5 | Disposing needs P on "Fixed assets" (the Accountant), and is audited with its notes; the gain or loss posts to its own account, reviewed at the close; in Founder mode a void is asked for and approved by another person who holds A (ASSET-19, FM-1), its envelope bound to the disposal's status, the asset's version and the cost (FR-APR-05) | `TestDisposingOfFixedAssets`, `TestAuthorisationMatrix` (`// req: FM-1`, `// asvs: V8.2.2`) |
| T6 | A disposal is refused while a month before it waits (ASSET-17); one recorded late reverses the asset's months on or after its month, on their own dates, in the same transaction; a void is refused once the asset changed again or what was left depreciated (ASSET-18) | `TestDisposingOfFixedAssets`, `TestADisposalIsVoidedAndTheAssetRestored` (`// req: FR-GL-12`) |
| T7 | At the close each asset account is reconciled with its assets' cost and the purchases not in the register, and 1590 with their accumulated depreciation, through the balance service: ✖ for what no document explains, ⚠ naming manual journals and purchases not registered; an asset whose source was voided or edited shows ⚠ | `TestTheRegisterAgreesWithItsAccounts` (`// req: INV-07`), `TestRegisteringFixedAssets`, `TestPurchasesNotInTheRegisterYet` |
| T8 | An invoiced sale locks the invoice and names its line once among the posted disposals (ASSET-16); the line must be on the clearing account, unspread, and its entry's credit there is the proceeds; a voided invoice shows ⚠ on its disposal | `TestDisposingOfFixedAssets` (`// req: FR-FA-03`) |
| T9 | `fixed_asset_source` is under FORCE RLS; the documents and entries are per organisation; every operation needs a right on "Fixed assets" | `TestTenantKeysIndexesAndForeignKeys`, `TestAuthorisationMatrix` (`// asvs: V8.2.2`) |
| T10 | The AI holds no right on "Fixed assets" (Appendix D) and no write tool (§9.6); registering, changing and disposing need a person signed in | `TestAuthorisationMatrix` |
| T11 | Every command takes an Idempotency-Key, and a change or disposal If-Match with the asset's ETag: a retry gets the saved answer and writes nothing; a line registered once is refused again | The API conventions' idempotency tests, `TestRegisteringFixedAssets` |

## 4. What remains

- The independent oracle does not post `asset_depreciation@1` or `asset_disposal@1` yet (a session of its own); until it
  does, the hand-worked tests and the replay are the second check.
- An asset registered from the wrong line cannot be removed from the register; its bill is corrected, and the asset shows
  ⚠ until it is disposed of. A removal while nothing has posted is a candidate for a later change.
- A bill voided or edited after its asset was registered is not refused; the asset and the close show it (T7).
- The accountant confirms the asset accounts' report groups, 7050, and recommended lives per account; the disposal
  clearing account is unset until the accountant names it.
