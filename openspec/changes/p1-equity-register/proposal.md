# Proposal

## Why

Fyniq keeps the register of share issues that founders' money writes. That covers the incorporation subscription and
conversions of what the company owes a founder. It does not yet hold:
- who the holders and investors are, or the share classes and their rights;
- a cash allotment after the books start, or cash that arrives before the allotment takes effect;
- transfers, SAFEs, notes and options;
- the cap table at a date, or a round's dilution;
- the reconciliation to ACRA's register of members.

The workbook modelled a SAFE conversion without whole shares (Appendix A). Investors, the accountant and the tax agent
will ask for the cap table and its evidence at the first priced round.

This change builds PRD-001 §8.23 for P1: FR-EQ-15…22 and FR-EQ-25…27, with C.2 #26–29. The equity register is in the
launch subset (accepted 16 Oct 2026). It sits on the longest path to launch: `p1-workbook-migration` and then launch
readiness wait on it. It also finishes what `p1-founders-money` left open: the SAFE or note route, cash issues, share
application monies and the cap table.

## What Changes

- **The owner's decisions of 7 Nov 2026, taken interactively (PRD-001 rev 0.203):**
  - **Reconciling to ACRA (FR-EQ-26, C.2 #26).** The holdings from ACRA's business profile are typed in, holder by
    holder and class by class, with the PDF attached as evidence. Fyniq compares them with the register at the
    extract's date. The alternatives were parsing the PDF (brittle: ACRA's layout is not a published format) and AI
    extraction later.
  - **A holder's identifier (FR-EQ-15, CMP-PD-07).** Fyniq keeps only an individual's ID type and its last 4
    characters, to tell holders apart. The full number stays in ACRA's register and the ID document kept as evidence;
    Fyniq files nothing that needs it. An entity keeps its UEN or foreign registration number. The alternatives were
    the full number encrypted, or no identifier.
  - **The option pool and grants (FR-EQ-19)** are built now, as a register: the pool sized by resolution, grants with
    vesting and expiry, and exercise as an equity transaction. The expense schedules and IR8A data stay P2. The
    alternatives were after go-live, or the pool without grants.
- **Holders and investors (FR-EQ-15).**
  - A contact marked shareholder, or the new mark investor (SAFE, note or option holder), carries its holder details:
    individual or entity, the identifier as decided, address and contact people.
  - Its record shows its holdings by class, instruments, grants, rights and documents.
  - A change to those details is a new version, approved and audited. Identity documents are evidence with the
    personal-data class.
- **Share classes (FR-EQ-16).** Ordinary (3010) and preference series (3020), each with structured rights recorded
  and not computed: votes per share, liquidation preference, dividend rights, conversion ratio, anti-dilution,
  pro-rata and redemption. The constitution and the shareholders' agreement are attached as evidence.
- **Equity transactions (FR-EQ-17).** They are append-only, dated by their effective date, approved (FM-1, FM-9),
  evidenced and opening their ACRA tasks (FR-EQ-09).
  - **The kinds:**
    - an issue for cash;
    - an issue for non-cash consideration, which founders' money already routes;
    - a transfer, which makes no entry;
    - a conversion between classes;
    - a SAFE or note conversion;
    - an option exercise.
  - **Share application monies.** Cash that arrives before an issue's effective date is held there, per subscriber
    (Appendix B.7; G4-043), on a new standard-chart account `[accountant to confirm the code]`.
  - **Corrections** are made by a reversing transaction, which cites the ACRA extract when ACRA was right.
- **SAFEs and convertible notes (FR-EQ-18).**
  - **Recording.** Each instrument is recorded with its terms, form, denominator and the accountant's classification
    [verify per instrument], and posted to 2700.
  - **Conversion.** At a priced round the engine computes the conversion with `RND-SHARE` and creates the share issues
    as drafts. It does so only for the approved template, YC's Singapore post-money SAFE (valuation cap, no discount;
    question 33), and only once a Singapore lawyer confirms its conversion terms. Every other form becomes a draft for
    the accountant, with ⚠ (G4-044).
- **The option pool and grants (FR-EQ-19, register part)**, as decided. An employee sees their own grants.
- **The cap table at any date (FR-EQ-20).**
  - **Content.** It is computed from the transactions and never edited. It shows each holder's holdings by class,
    issued and fully diluted, with ownership and voting percentages computed exactly and shown to 2 decimals.
  - **Use.** Two dates can be compared side by side. Each figure drills down to its transactions and evidence, and the
    table exports to XLSX and PDF.
  - **The oracle** computes the same table (QA-ENG-02).
- **Round modelling (FR-EQ-21).**
  - **Inputs and outputs.** The inputs are a pre-money valuation or price per share, new money per investor, an
    option-pool top-up, SAFE and note conversions, and pro-rata take-up. The outputs are the price, the shares, the
    pro-forma table and the dilution in points.
  - **Scenarios** are shown side by side and never post. An approved scenario creates draft issues.
- **Investor records (FR-EQ-22).** Each investor's investments, the rights from the agreements (board seat, observer,
  information rights, pro-rata, consent matters) and the documents as evidence.
- **Linked statutory checks (FR-EQ-25).**
  - **Exempt-private-company status.** A holder change that adds a corporate holder or a 21st member ends it.
  - **Controllers.** More than 25% of the shares or votes flags a register-of-controllers update. Control by recorded
    rights is otherwise "Not assessed", never OK (G4-101).
- **Integrity (FR-EQ-26).**
  - INV-13: paid-up capital in the register equals 3010 + 3020.
  - The ACRA reconciliation, as decided.
  - Equity data is restricted: AI and MCP clients need `fyniq.read.equity`, which is never granted by default.
- **Basic and Expert (FR-EQ-27).**
  - Basic: "Who owns the company" (an ownership chart with a table view, my holding and the latest round) and a simple
    round simulator.
  - Expert adds classes and rights, instruments, the option pool, transactions, fully diluted views and the
    reconciliation.
- **Checks:**
  - C.2 #26: the register differs from the latest ACRA extract;
  - C.2 #27: a controller is not yet updated;
  - C.2 #28: exempt status changed;
  - C.2 #29: a SAFE or note is past maturity, or a priced round was recorded without its conversions.

Out of scope, and why:
- **P2:** investor obligation tasks (FR-EQ-28), investor updates (FR-EQ-23), investor access beyond one's own holding
  (FR-EQ-24), and the option expense and IR8A data (FR-EQ-13, FR-EQ-19 P2).
- **C, P2:** the exit waterfall (FR-EQ-29), and cancellation, buy-back, capital reduction, split and consolidation.
- **Computing rights:** anti-dilution and liquidation outcomes are recorded, never computed, in P1 (FR-EQ-16).

## Capabilities

### New Capabilities

None. The requirements are approved in `equity/cap-table-and-investors`, `controls/platform-checks` (C.2 #26–29) and
`ledger/posting-rules` (B.7's share issue rows).

### Modified Capabilities

None as a delta. PRD-001 rev 0.203 records the owner's three decisions in FR-EQ-15, FR-EQ-19 and FR-EQ-26 and the share
application monies account in B.5. The main spec `equity/cap-table-and-investors` is updated from it and stamped in the
same commit (Recipe B), so this change only builds approved requirements: `skip_specs: true`.

## Impact

- **Database:**
  - `contact`: the investor mark and the holder details;
  - new tables for the share class, the equity transaction and its legs, the instrument, the option pool and grant,
    the investor right, the round scenario and the ACRA extract with its typed holdings;
  - `share_issue` widened to the new considerations and kinds;
  - the new standard-chart account for share application monies;
  - each table append-only or versioned, under FORCE RLS and audited, as the register is.
- **Engine:** `internal/domain/equity` grows from a check of an issue into the cap table, `RND-SHARE` conversions, round
  modelling and the controller and exempt-status tests. Postings from the register on its effective date go through the
  posting engine (INV-13).
- **API:** holders, classes, transactions, instruments, pool and grants, the cap table (with date, compare and export),
  round scenarios, investor rights and ACRA extracts, all under the `CapTable` capability (Appendix D). The Board viewer
  and employees see only their own holding or grants (FR-EQ-19, FR-EQ-24 P1 part).
- **Web:** the equity canvas's screens: cap-table, holders, holder-record, share-classes, equity-transaction
  (EquityRegister), instruments, option-pool and round-simulator. They are already designed (13/13 on the equity
  canvas), with help keys, a guide page and Basic/Expert tags.
- **Oracle:** the cap table, SAFE conversions under both conventions and a round's dilution, against G.3 and G4-043/044.
- **Checks:** C.2 #26–29 on the checks engine.
- **Depends on:** `p1-founders-money` (the register, RND-SHARE, the ACRA steps), the approval engine, evidence, the
  checks engine, report exports (XLSX) and the render service (PDF).
