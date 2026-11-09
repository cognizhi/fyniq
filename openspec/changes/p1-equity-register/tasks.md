# Tasks

Each slice below is one pull request that passes `make test` on its own. Each slice carries its migration, port and
adapter, its attack tests, its API with matrix rows, its screen or screens with help keys and guide sections compiled
into the knowledge base, and the oracle's half where it computes a figure. Load `fyniq-accounting-domain`,
`fyniq-feature-change` and `fyniq-design-system` before each slice.

## 1. The decisions and the specs

- [x] 1.1 PRD-001 rev 0.203 records the owner's decisions of 7 Nov 2026 (FR-EQ-15 identifiers, FR-EQ-19 register
  now, FR-EQ-26 typed extract) and B.5's 2040 Share application monies `[accountant to confirm]`. The main spec
  `equity/cap-table-and-investors` is updated and stamped (`python3 scripts/prdspec.py stale` is clean, `make check`
  passes). `// req: FR-EQ-15`, `// req: FR-EQ-19`, `// req: FR-EQ-26` (spec text)

## 2. Holders and classes (slice 1)

- [x] 2.1 Migration and storage (D1, D2):
  - `contact.investor`, with CT-02 widened;
  - `holder_record` and `share_class`, insert-only versions numbered at the version read;
  - an individual's identifier refused beyond 4 characters (FY-guarded);
  - `share_issue.class_id`, with the default Ordinary class backfilled and FY255 widened so the class's account
    matches.

  Attack tests: no update or delete; no 5-character identifier; an issue whose class and account disagree.
  `// req: FR-EQ-15`, `// req: FR-EQ-16`, `// req: CMP-PD-07`
  (7 Nov 2026, PRD-001 rev 0.204: built as documents, D1 and D2 revised. Migrations 000250 and 000251 add
  `contact.is_investor`, the document types `holder_record` and `share_class`, the approval objects, FY285 to FY287 and
  `share_issue.class_id`. Tests `TestAttackEquityRecordsKeepTheirRules`, `TestAHolderRecordKeepsTheLast4Only`,
  `TestAShareClassRecordsItsRights`.)
- [x] 2.2 API (D1, D2, D12):
  - holders (contacts marked shareholder or investor, with their holdings, instruments, grants and rights so far);
  - the holder record's versions, as the approval object `holder_record`;
  - the identity document as evidence with the personal-data class;
  - classes and their versions with rights and evidence;
  - Appendix D's scopes (Board and Employee row-scoped).

  Matrix rows for each operation; contract and Spectral. `// req: FR-EQ-15`, `// req: FR-EQ-16`
  (7 Nov 2026: `listHolders`, `getHolder`; `createHolderRecord` (`POST /v1/holder-records`), `getHolderRecord`,
  `replaceHolderRecord`, `requestHolderRecordApproval`; `listShareClasses`, `createShareClass`, `getShareClass`,
  `replaceShareClass`, `requestShareClassApproval`; each record's evidence (add, list, get, remove); `class_id` on share
  issues; the contact mark `investor` and the contacts filter `holder`. Drafts are the Accountant's (C/P), approvals an
  Owner's or the Controller's (A). Test `TestHoldersAndShareClasses` and the matrix rows.)
- [x] 2.3 Screens Holders, HolderRecord and ShareClasses as designed (Expert), with help keys, guide sections in
  `docs/user-guide/equity-register.md` (the founders' guide's register section moves there) and their knowledge-base
  page. Vitest and axe. `// req: FR-EQ-15`, `// req: FR-EQ-16`, `// req: FR-EQ-27`

## 3. Equity transactions, the holdings backstop and share application monies (slice 2)

- [x] 3.1 Migration (D3, D4, D5):
  - `holding_movement`, with FY288's rules (FY257 was taken): a movement names a recorded transaction; an issue's
    movement equals its `share_issue`; a transfer nets to zero with no negative holding; a reversal reverses all of one
    transaction, once; nothing changes;
  - `equity_step` (moved to 3.2's ACRA steps, slice 2b);
  - the `equity_transaction` document type and approval object;
  - 2040 Share application monies in the standard chart (role `share-application-monies`), with the sub-ledger key
    rule widened to it;
  - existing issues given their movements in the migration.

  Attack tests for each FY257 rule and for a key-less 2040 line. `// req: FR-EQ-17`, `// req: INV-13`
- [x] 3.2 Engine and API (slice 2a built the kinds, draft, approval, effective, reversal and the holdings; slice 2b the
  ACRA steps and money from the bank, in and refunded (the owner's decision of 7 Nov 2026: a refund is recorded from
  the bank line, not through a payment batch); and the oracle's golden case `equity.json`, with 2040 on a financing
  cash-flow line of its own, rev 0.207):
  - the kinds issue_cash, transfer, class_conversion and reversal (issue_noncash keeps founders' money's route):
    draft, approval (FM-1, FM-9), and effective with ACRA's date and reference, never backdated and never in a closed
    period, writing the issue, its movements and its entry in one unit of work;
  - the ACRA steps with their ⏰/⚠;
  - a bank line matched to a pending subscription posts Dr Bank · Cr 2040 (subscriber);
  - effective refused until paid;
  - a refund through a payment batch.

  Tests: G4-043 end to end, with the February balance sheet and the cap table on 12 Mar only; a transfer makes no
  entry; a reversal citing the ACRA extract; and the oracle's G4-043 golden case.
  `// req: FR-EQ-17`, `// req: FR-EQ-09`, `// req: INV-13`, `// req: QA-ENG-02`
- [x] 3.3 Screens EquityRegister and EquityTransaction as designed, replacing the founders' register page. Steps,
  evidence, the subscription's payment and the reversal; help keys and guide sections. Vitest and axe.
  (Slice 2c: Equity › Transactions lists every transaction with its ACRA flags above the register of share issues, at
  the founders' old address; a transaction's page drafts, changes, asks for approval, records the ACRA steps and the
  effective date, and starts a reversal; its evidence goes through the journal-evidence panel.)
  `// req: FR-EQ-17`

## 4. The cap table (slice 3)

- [x] 4.1 `equity.CapTable(at)` (D7):
  - issued, voting and fully diluted, by holder and class, with exact percentages shown to 2 decimals half-up;
  - two dates compared;
  - drill-down to movements, transactions and evidence.

  The oracle's `cap_table` agrees on G.3 and generated scenarios (`make differential`). `// req: FR-EQ-20`,
  `// req: QA-ENG-02`, `// req: RND-DISPLAY`
  (Slice 3a: `equity.TableAt` and `TableIn`, each line keeping its movements for the drill-down; two dates are two
  calls, which the API compares in 4.2. The differential gains `share_transfer`, `votes_per_share` and `cap_tables`,
  the golden case `cap-table.json` and the generator's `equity` source.)
- [x] 4.2 API: `GET` the cap table with `at` and `compare`; the `cap_table` export kind (XLSX, 202 with its operation)
  and the PDF through the render service; no write path to holdings but D4. Matrix rows; the Board and Employee scopes.
  `// req: FR-EQ-20`
  (Slice 3b: `getCapTable`, `exportCapTable` (CSV, XLSX, PDF; synchronous and audited, as D7 now says) and
  `getMyHolding` for the Board viewer's own holding. An Employee's own grants come with 6.1.)
- [x] 4.3 Screen CapTable: Basic "Who owns the company" (an ownership chart with its table view, my holding, the latest
  round) and Expert's fully diluted columns, compare and drill-down (dataviz checks, `fyniq-design-system`). Help keys
  and guide sections. Vitest and axe. `// req: FR-EQ-20`, `// req: FR-EQ-27`
  (Slice 3c: Equity opens on the cap table at `/equity`, in Basic too. The ownership bar comes from the kit's chart
  classes, with a legend and a table view. The Board viewer sees only My holding. The checks card (FR-EQ-25, FR-EQ-26)
  and the SAFE and pool rows wait for their slices.)

## 5. SAFEs and convertible notes (slice 4)

- [x] 5.1 Migration and storage (D6):
  - `instrument` and `instrument_template`, seeded with `yc-sg-post-money-cap@1`, disabled until its
    `lawyer_confirmed` evidence;
  - 2700's role `convertible-instruments`, with the instrument as its sub-ledger key.

  Attack tests. `// req: FR-EQ-18`
  (Slice 4a-i built the register: `instrument` and `instrument_template` documents, recorded on another person's approval
  with their evidence (the signed instrument; the lawyer's confirmation), FY291 and FY249 as the backstop, and the API.
  Slice 4a-ii keyed 2700 by instrument: its role `convertible-instruments`, `journal_line.instrument_id` with FY292, bank
  lines in and out and journals naming the instrument, and each instrument's balance read from the ledger. The founder's
  route to a SAFE (FR-EQ-05) comes with the conversions.)
- [x] 5.2 Engine and API:
  - recording an instrument (approved), its cash matched Dr Bank · Cr 2700;
  - conversion at a priced round, under both conventions with `RND-SHARE`;
  - drafts for approval, or a draft for the accountant with ⚠ when the template is not enabled or a term is not
    covered;
  - C.2 #29 (maturity passed, or a round without its conversions).

  Tests: G.3 pre-money (52,083 at S$2.40, S$0.80 left) and G4-044 post-money (52,631 at S$2.375, S$1.37 left; 79.83%,
  15.97%, 4.20%); a two-SAFE case worked by hand; the oracle's `safe_conversion`. `// req: FR-EQ-18`,
  `// req: RND-SHARE`, `// req: QA-ENG-02`
  (Slice 4b-i built the conversion: `equity.Convert` with the generalised post-money price (D6), the
  `instrument_conversion` transaction drafted from an approved round by `draftInstrumentConversions` and approved like any
  other, `instrument_conversion@1` posting Dr 2700 naming the instrument · Cr the class, FY288 converting each instrument
  once, and the cap table after G4-044's conversion at 79.83%, 15.97% and 4.20%. Slice 4b-ii brought the oracle's
  `safe_conversion`, written from the differential contract's section "SAFEs and their conversion": the `instrument`
  and `safe_conversion` documents, bank lines naming a SAFE on 2700, the golden cases `safe-conversion.json` (G4-044,
  then a discount, an MFN and a pre-money SAFE at a later round) and `safe-conversion-caps.json` (two caps and G.3's
  pre-money SAFE), and the generator's SAFE kinds. Slice 4d brought C.2 #29 (`equity.instruments-converted`: a note past
  its maturity, or a SAFE that shares allotted for cash after it was signed have not converted, ⚠) and the founder's
  route to a SAFE (FR-EQ-05: the settlement's `safe` route, `director_safe@1` Dr 2500 · Cr 2700 on approval, its
  `director_safe` document in the differential and `golden/safe-from-a-founder.json`). Fully diluted with outstanding
  SAFEs as converted comes with the rounds (D7, D8).)
- [x] 5.3 Screen Instruments as designed, with help keys and guide sections. Vitest and axe. `// req: FR-EQ-18`
  (Slice 4c: Equity › SAFEs and notes with each instrument's terms, what 2700 holds and its status; the record page with
  the signed copy, its approval and its conversion; the templates and their enablement with the lawyer's confirmation;
  and the conversion preview, worked by the engine (`previewInstrumentConversions`: at an approved round or a price typed
  in, each price's working under RND-DISPLAY), from which the Accountant drafts the conversions at an approved round.
  Bank categorisation now offers 2700 and 2040, naming the SAFE or the subscriber. The preview's "From the round
  simulator" waits for the rounds (D8).)

## 6. The option pool and grants (slice 5)

- [x] 6.1 Migration, engine and API (D9):
  - `option_pool` by resolution;
  - `option_grant` and `grant_event`, with FY258 (no grant beyond the unallocated pool);
  - vesting computed;
  - exercise as an issue;
  - an employee reads only their own grants.

  Attack tests and API tests; the fully diluted cap table includes the pool. `// req: FR-EQ-19`
  (Slice 5a-i: `option_pool` resolutions and `option_grant` documents, each drafted, evidenced and recorded on another
  person's approval, never the grant's holder; vesting computed (whole months, rounded down [accountant to confirm]);
  the pool covers its grants on every day from a record's date on, in the domain (EQUITY-16, EQUITY-17) and the
  database (FY293, under a lock on the pool; FY258 was taken); `getMyOptionGrants` for an Employee's own grants, and an
  investor may be linked to a member. Slice 5a-ii: the fully diluted cap table counts each grant's options in its
  holder's line and the unallocated pool in the total (`equity.DilutedAt`, the API's `options`, `unallocated_pool` and
  `pool_diluted`, the exports' pool row and the screen's), and the differential format has `option_pool` and
  `option_grant` (`golden/option-pool.json`). Slice 5b-i: a grant's forfeitures and expiries (`option_event`, recorded
  on another person's approval with the notice, never the holder's), a forfeiture at most what is not vested; recorded,
  never more than the grant gave (FY293), and the options back in the pool from the event's day; the differential's
  `option_event` and grants' vesting (`golden/option-events.json`). Slice 5b-ii: an exercise is an equity transaction of
  kind `exercise` (D4), an issue to the grant's holder at its exercise price from share application monies, at most what
  is vested and not exercised (EQUITY-07, again when effective), its options never back in the pool and outstanding again
  if it is reversed; the database's FY293 on its movement; the differential's `option_exercise` and grants'
  `exercise_price` (`golden/option-exercise.json`). An exercise at a price of 0 waits for the accountant [accountant to
  confirm].)
- [x] 6.2 Screen OptionPool as designed, and the employee's own grants view. Help keys and guide sections. Vitest and
  axe. `// req: FR-EQ-19`, `// req: FR-EQ-27`
  (Slice 5c: Equity › Option pool with the pool's figures from the server (granted, given back, exercised, outstanding,
  vested and not exercised, available), every grant, what vests or expires in 30 days and the resolutions; the grant's
  and the resolution's pages with their evidence and approval, and Forfeit or let lapse; and My shares and options for
  an Employee, with the next vesting the engine works out (`equity.NextVesting`). The canvases' new states wait for
  task 10.1.)

## 7. Round modelling (slice 6)

- [x] 7.1 `round_scenario` (D8): inputs versioned and outputs computed on read; side by side; nothing posts; approving
  creates the draft transactions. The oracle's `round_dilution` agrees on G.3. `// req: FR-EQ-21`,
  `// req: QA-ENG-02`
  (Slice 6a: `equity.ModelRound`, pure, over the cap table fully diluted the day before; a price or a pre-money valuation
  over the shares fully diluted before (with a top-up before the money) cut down to its decimals; RND-SHARE per investor;
  the SAFEs converting by D6's `Convert`; each holder before and after with the points lost and a pro-rata entitlement
  [accountant to confirm both]. The API's round scenarios, with the sandbox as `Prefer: return=preview`; approving one
  drafts each investor's issue for cash and a top-up's pool resolution, while the conversions are drafted from the
  round's issue once it is approved (D6). The oracle's side is `oracle/src/oracle/rounds.py`, agreeing on
  `golden/rounds.json`.)
- [x] 7.2 Screen RoundSimulator: Basic with a few inputs, Expert with the pool top-up, conversions and pro-rata. Help
  keys and guide sections. Vitest and axe. `// req: FR-EQ-21`, `// req: FR-EQ-27`
  (Slice 6b: `/equity/rounds`. The sandbox is its own read-only operation, `modelRound` (`POST /v1/round-models`), for
  anyone who reads the whole cap table: Appendix D gives the Owner and the Controller A, not C, on the cap table, so
  they could not use `createRoundScenario`'s preview. Saving and asking stay with the Accountant. The outcome adds the
  money raised, the pool's part fully diluted before and after, and `you` on the caller's own linked holder. Basic edits
  one investor and shows the rest under "Has advanced details". Not built from the design: an investor "not yet named"
  (an investor is a contact) and the assistant card (the AI's lane waits for `p1-ai-assistant`).)

## 8. Investor rights, controllers and exempt status (slice 7)

- [x] 8.1 `investor_right` and `controller_entry` (D10):
  - the exempt-status re-test after each recorded transaction, raising C.2 #28;
  - the controller test (more than 25% of shares or votes), raising C.2 #27, and "Not assessed" when rights could
    confer control without confirmation;
  - the annual notices recorded.

  Tests: G4-101; a corporate holder ends exempt status; a 21st member; a crossing at 25.01%. `// req: FR-EQ-22`,
  `// req: FR-EQ-25`
  (Slice 7a: `equity.ControllerTest`, pure, reads the cap table on each day the register moved: more than 25% of the
  shares issued or the votes, exactly, from the first day of the latest run over it; a board majority or consent matters
  name a holder for a person to weigh. `equity.ExemptTest`: an entity member or a 21st member ends exempt status, and a
  member with no holder record leaves it undecided [accountant to confirm the Act's exceptions]. `getStatutoryTests`
  gives each holder's standing (to enter, to lodge, registered, not assessed, not a controller) with the company's
  register due 7 days after and ACRA's 2 business days after that, weekends left out [verify public holidays]; C.2 #27
  is ⏰ for what is due, ⚠ for a notice unanswered 30 days or a controller the test no longer names, and not assessed when
  only rights remain to weigh; C.2 #28 warns when the register decides otherwise than the status recorded. The annual
  notices are recorded on the controller entry; the company secretary's confirmation is its evidence (question 41).)
- [x] 8.2 The investor rights and controllers on HolderRecord, with help keys and guide sections. Vitest and axe.
  `// req: FR-EQ-22`, `// req: FR-EQ-25`
  (Slice 7b: on the holder page, "Rights from the agreements" with a "Could confer control" chip, and "Registrable
  controller" with the test's words, the standing chip and the days due, the entry in force with its notices, each with
  its draft, evidence and approval. The design's investments, obligations (FR-EQ-28, P2) and documents tabs are not
  built here; the exempt test shows on the checks page.)

## 9. The ACRA reconciliation and integrity (slice 8)

- [ ] 9.1 `acra_extract` and `acra_extract_holding` (D11): typed holdings with the PDF as evidence; the comparison at
  the extract's date; C.2 #26 listing each difference; a reversal citing the extract fixes one. Tests: a matching
  extract, a missing holder, a wrong class and a wrong count. `// req: FR-EQ-26`
- [ ] 9.2 The reconciliation page (Expert), with help keys and guide sections. Vitest and axe. `// req: FR-EQ-26`,
  `// req: FR-EQ-27`

## 10. Finish

- [ ] 10.1 The canvases match what was built (`make design-check`); the help registry and QA-UI-05 hold; the knowledge
  base lints clean; the threat model `docs/security/threat-models/equity-register.md` covers the holder identifiers,
  the scopes and the backstops.
- [ ] 10.2 `make check`, `make test` and `make test-e2e` pass; the full authorisation matrix passes.

## Workflow follow-up

- Archive with `/opsx:archive p1-equity-register` once every slice is merged, running `prdspec.py coverage` and
  stamping what the archive changes.
