# Tasks

## 1. Exchange rates

- [ ] 1.1 `RND-RATE`: a quote per one unit, per 100 units, or of foreign currency per one unit of the base, stored as base per one unit to 10 places, half away from zero, with the quote and its form kept. Tests `// req: RND-RATE`
- [ ] 1.2 The organisation's rates: a day's rate and the monthly table, entered or imported from a file, listed, withdrawn with a reason, never changed, audited; duplicates in the monthly table shown with ⚠. Tests `// req: FR-GL-05`
- [ ] 1.3 The daily feed: the `RateFeed` port, the local adapter reading a rates file per date, the platform job `fx.feed` storing missing days of the last 7 in the global feed table. Tests `// req: FR-GL-05`
- [ ] 1.4 The cascade for a currency and a date: the organisation's rate for the day, the feed's (up to 7 days earlier), the monthly table (the average of duplicates, "average of n entries"), otherwise `FX-01`; whether the rate is an average; a look-up operation that shows what a posting would take. Tests `// req: ENG-40` (including G4-025)

## 2. Manual journals

- [ ] 2.1 A foreign-currency journal line without a typed rate takes the cascade's rate for the journal's date when its statuses are worked out and when it posts, with its source; `FX-01` as the line's status when there is none. Tests `// req: ENG-40` and `// req: FR-GL-05` (the source on every converted amount)

## 3. Month-end revaluation

- [ ] 3.1 The `revalue` flag on asset and liability accounts, refused on prepayments and deferred revenue; the standard chart's defaults. Tests `// req: FR-GL-04`
- [ ] 3.2 Base-only lines in the database: the `RND-FX` CHECK accepts amount 0; a deferred trigger accepts a base-only line only from `fx_revaluation` or its reversal, on a `revalue` account, in a foreign currency, offset by the `unrealised-fx` account. Attack tests `// req: INV-05`
- [ ] 3.3 The revaluation rule: per `revalue` account and currency, unrealised = `RND-FX`(open amount × period-end rate) − carrying base, base-only lines against 7040; G4-039's figures at account level. Tests `// req: FR-GL-04`, `// req: ENG-41` and `// req: ledger/posting-rules FX revaluation`
- [ ] 3.4 A period's revaluation: preview, post with its reversal on day 1 of the next period (or waiting until that period opens), the revision it read, run again as a reversal and replacement when stale; the platform job `fx.revalue` under the system principal. Tests `// req: FR-GL-04`, `// req: FR-AUT-01` and `// req: FR-AUT-02`
- [ ] 3.5 The close check: ✖ a period with a foreign-currency balance on a `revalue` account and no current revaluation, naming a missing rate. Tests `// req: FR-GL-07` and `// req: FR-GL-04`
- [ ] 3.6 AR and AP documents and unapplied credits revalued one by one against their stored rates, replacing the control accounts' lines (with `p1-sales` and `p1-purchases`). Tests `// req: FR-GL-04` (G4-039 in full)

## 4. Realised FX, GST and money accounts

- [ ] 4.1 Realised FX on settlement: the currency amount settled × (settlement rate − stored rate), under `RND-FX`, to 7030, with no tolerance; G4-039's receipt. Tests `// req: ENG-41`
- [ ] 4.2 GST on foreign-currency documents: sales at the GST exchange-rate source, then `RND-FX` and `RND-GST-EXCL`; purchases at the stated SGD GST with the difference to 7030, or none stated with the GST in the cost and ⚠; an average rate refused. Tests `// req: ENG-42` (G4-023)
- [ ] 4.3 Settlements and documents call 4.1 and 4.2 (with `p1-sales`, `p1-purchases` and `p1-bank-import-reconcile`). Tests `// req: ENG-41` and `// req: ENG-42`
- [ ] 4.4 Foreign-currency money accounts show the currency balance and the base balance. Tests `// req: ENG-43`
- [ ] 4.5 Reconciliation in the account currency (with `p1-bank-import-reconcile`). Tests `// req: ENG-43`

## 5. Screens

- [ ] 5.1 Accounting › Currencies (`fx-revaluation`): the rates with duplicates ⚠, entering, importing and withdrawing a rate, the look-up, the foreign-currency money accounts. Expert mode; help keys; a guide page compiled into the knowledge base. Tests `// req: FR-GL-05`, `// req: ENG-40` and `// req: ENG-43`
- [ ] 5.2 The month's revaluation on the same page: the items, their rates and sources, the preview and the post, what is not revalued and why; the close page's check. Tests `// req: FR-GL-04`
- [ ] 5.3 The journal editor shows the rate a foreign-currency line will take and where from. Tests `// req: ENG-40`

## 6. The oracle (for the oracle session; never written from the engine)

- [ ] 6.1 `RND-RATE`, base-only revaluation lines and their reversal, realised FX and the ENG-42 GST difference as scenario shapes, generator kinds and golden cases, written from the PRD rules by an author who has not read the engine (QA-ENG-01)

## 7. Finish

- [ ] 7.1 Every new operation in the contract (`make openapi`), with its authorisation-matrix rows
- [ ] 7.2 The MAS adapter of `RateFeed` at the cloud gate, once MAS confirms redistribution (FR-GL-05 [verify])
- [ ] 7.3 Finish with `make check`, `make test` and `make test-e2e`
