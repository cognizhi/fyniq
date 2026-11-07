# Design

## Context

See proposal.md for why. What exists, from `p1-founders-money` (archived 3 Nov 2026):

- **The register of share issues.** `share_issue` (migration 000216) is append-only under FORCE RLS. FY255 checks an
  issue's class (an active share-capital account, 3010 or 3020), its holder (a contact marked director or
  shareholder), whole shares and the amount's scale.
- **Posting.** An issue after the books start posts from the source `share_issue` in the same transaction (rule
  `share_issue@1`, LEDGER-46). An entry from that source commits only with its row, so share capital never moves
  without the register (INV-13).
- **Considerations.** `cash`, `debt_capitalised`, `services` and `safe_or_note` are allowed. Only conversions of a
  founder's balance and issues before the books start are built.
- **Rounding.** `RND-SHARE` is in `internal/domain/rounding`, and the oracle's `director_conversion` states it.
- **The ACRA steps.** `conversion_step` (000217) records them for a conversion (FR-EQ-09, FOUNDER-38…40).
- **Exempt status.** The exempt-private-company status is an `exempt_status` document (000208, FR-EQ-11), effective
  from a day.
- **The page.** `/equity/transactions` lists the register. Its guide section is in the founders' guide.
- **Contacts** carry the marks customer, vendor, director and shareholder (CT-02). A contact can be linked to a member
  (`contact_member_link`, approved).
- **The standard chart** has 2700 Convertible notes / SAFE (liability, Borrowings, no role) and 3010/3020 with the role
  `share-capital`. Appendix B.5 leaves share application monies "to be assigned in the CoA review".
- **Access.** Appendix D's `CapTable` row:
  - **Owner and Controller:** A.
  - **Accountant:** C/P.
  - **Employee:** V own grants.
  - **Auditor and Tax agent:** V.
  - **Board:** V own holding, all if granted.
  - **DPO:** M.
  - **AI:** scenarios only, with `fyniq.read.equity`.
- **The equity canvas** has every screen designed: CapTable, Holders, HolderRecord, ShareClasses, EquityRegister,
  EquityTransaction, Instruments, OptionPool and RoundSimulator. ExitWaterfall, InvestorUpdates and BoardPortal are
  P2. MoneyIPutIn is built.

Constraints: golden rules 1–3, 7 and 8 (exact decimals, the database as the backstop, the oracle agrees, every change
audited, tenancy on every row), ADR-T8 (no new port: the blob store, the render service and the outbox exist), and
question 33 (YC's Singapore post-money SAFE as the only automatic template, once a lawyer confirms it).

## Goals / Non-Goals

**Goals:**
- **One source of holdings.** Every share a holder holds traces to an equity transaction, and through it to its
  evidence and, for an issue, to `share_issue` and its entry.
- **The cap table** at any date, from that source alone. The engine and the oracle agree on it to the share and to the
  percentage's last decimal.
- **The database refuses a holding with no transaction**, an issue posted from anywhere but the register, a cash issue
  made effective before it is paid, and an edit of any recorded transaction, instrument, grant or extract.

**Non-Goals:**
- Computing anti-dilution adjustments or liquidation outcomes (FR-EQ-16; P1 records rights only).
- The exit waterfall, investor updates and investor access beyond one's own holding (P2).
- Allotting unpaid or partly paid shares. A cash issue is made effective only once its subscription is paid; anything
  else is entered by the accountant as a manual case, which this change refuses with a plain message
  [accountant to confirm that no go-live allotment is unpaid].
- OAuth scopes. `fyniq.read.equity` is enforced when `p1-mcp-first-party` adds scopes. Until then the AI has no
  equity read tool, so nothing reaches it.

## Decisions

### D1 Holders: the contact, with a holder record document (FR-EQ-15)

**The marks.** A holder is a contact marked shareholder, or marked **investor** (a new mark for SAFE, note and option
holders who hold no shares yet). The `contact` table gains `is_investor`, and CT-02 accepts it.

**The holder record.** A holder's details are a `holder_record` document:
- `kind`: individual or entity;
- the identifier:
  - for an individual: `id_type` (NRIC, FIN, passport) and `id_last4` only, as the owner decided;
  - for an entity: its UEN or foreign registration number in full, since it is not personal data;
- nationality or place of incorporation;
- the registered address;
- the contact people.

A document was chosen over a table because evidence attaches only to documents, and the document pipeline brings
drafts, versions, ETags and the approval engine with it. This is the exempt status's pattern (000208).

**The database's rules** on every version (FY285, through a trigger on `document_version`):
- the payload holds only its own keys;
- an individual's identifier has 1 to 4 letters or digits;
- an entity's has none of them;
- no text in it shows an NRIC or FIN in full.

**Approval.** A draft is recorded only on the approval object `holder_record.record` (FM-1). Another person who holds A
on the cap table approves, never the member linked to the holder. A later recorded record supersedes the earlier,
which stays in the history.

**Evidence.** The identity document attaches as evidence of the class `identity`. The app opens no identity-class file
until FR-DOC-09's permissions for it exist. A masked reader (Appendix D's M, the DPO) never opens any file, and reads the
record without its identifier, address, contact people or note.

### D2 Share classes as documents (FR-EQ-16)

**The class.** A share class version is a `share_class` document. It holds:
- the class's name;
- its kind: ordinary, or preference with a series;
- its share-capital account: 3010 or 3020, by role `share-capital`;
- the structured rights:
  - votes per share;
  - liquidation preference: multiple, participating, cap and seniority;
  - dividend rights;
  - conversion ratio;
  - anti-dilution: none, broad-based or full ratchet;
  - pro-rata and redemption;
- the constitution and the shareholders' agreement as evidence.

**Versions.** A later version of a recorded class's rights is another document whose `class_id` names the class: the
first version's document. It keeps the account. The database refuses a class whose account is not active share capital
(FY286).

**The link to issues.** `share_issue` gains `class_id`. Once an account has a recorded class, an issue of it must name
one of its own classes (FY287). An issue that names none takes the account's first recorded class: the register's API
and the founders' conversion both do so. Issues recorded before any class stay class-less and count under that class.

**Changes.** A class is recorded on the approval object `share_class.record` (FM-1). The rights are only recorded. Votes
per share is the only right the engine reads (voting percentages).

### D3 One movement table behind every holding

`holding_movement` is insert-only. Each row holds a transaction, a holder, a class, signed shares and the effective
date. Every equity transaction writes its movements in its own unit of work:

| Kind | Movements | Entry |
|---|---|---|
| Issue (cash, non-cash, SAFE or note, exercise) | + holder, class | from the register (`share_issue`) |
| Transfer | − seller, + buyer, same class | none |
| Conversion between classes | − holder, old class; + holder, new class | Dr 3020 · Cr 3010 (or the reverse) at the carrying amount, when the classes' accounts differ |
| Reversal | the opposite of the transaction it reverses | the reversal of its entry |

**The backstop (the next free FY code):**
- a movement names a recorded equity transaction;
- an issue's movement equals its `share_issue` row in holder, class and shares;
- a transfer's two movements net to zero and leave the seller with no negative holding;
- a reversal reverses all of one transaction, once;
- nothing is updated, deleted or truncated.

**The cap table** at a date is the sum of movements effective on or before it, by holder and class (D7). The
alternative was to recompute holdings from `share_issue` and transfer documents each time. It was rejected because
the movement table makes the database check holdings directly (golden rule 2) and keeps the cap table a single sum.

### D4 Equity transactions as documents, with the ACRA steps (FR-EQ-17, FR-EQ-09)

**The document.** An equity transaction is a `document` of type `equity_transaction` with a kind: issue_cash,
issue_noncash, transfer, class_conversion, instrument_conversion, exercise or reversal. This reuses the document
pipeline: drafts, versions, ETags, evidence, the approval engine and the posting engine.

**Its lifecycle:**
1. **Draft.**
2. **Approved** (FM-1). FM-9 applies when the beneficiary is a member: they never approve, and the accountant's review
   is added when every approver benefits, as for founders' money.
3. **Recorded as effective** with ACRA's date and lodgement reference, never backdated before its approval and never in
   a closed period. Recording writes `share_issue` (for an issue), the movements and the entry in one unit of work.

**Evidence** follows FR-DOC-06's rules per kind:
- an issue: the resolutions and the return of allotment;
- a transfer: the instrument of transfer;
- a reversal: the ACRA extract when ACRA was right.

**The ACRA steps** become `equity_step`, keyed by the transaction: insert-only, the later step superseding. It
generalises `conversion_step`, which stays for founders' conversions. The steps by kind:
- an issue: resolution, then return of allotment;
- a transfer: instrument signed, then notice of transfer lodged (s126, within 14 days by ACRA's guidance), then the
  register updated, which is the effective date;
- a class conversion: resolution, then lodgement.

They raise ⏰ and ⚠ as FOUNDER-38…40 do.

**A transfer** makes no entry. Share duty (0.2% of the higher of price and net asset value) is the parties' matter and
is shown as information only.

**Revised while building (rev 0.205):**
- A cash issue has one subscriber. A round with several is several transactions, so FM-9 excludes each subscriber from
  their own and the accountant's review applies as for founders' settlements.
- A conversion between accounts moves the carrying amount the Accountant enters, shown in the approval
  [accountant to confirm the basis].
- The backstop is FY288 (FY257 was taken by dimensions); the conversion's entry is held to its movements by FY289.
- The accountant's review step could not be signed: the Accountant holds no A on the approval rows. Their A on
  "Accountant review" now reaches a step that names the review, and no other.

### D5 Cash issues and share application monies (B.7, G4-043)

**The account.** The standard chart gains **2040 Share application monies** (liability, Payables & accruals, role
`share-application-monies`) `[accountant to confirm the code and classification]`. Like 2500, it carries the
subscriber as its sub-ledger key: the database's key rule for the roles of 2500 and 2120 is widened to this role.

**An existing organisation** whose chart lacks the role is refused when cash would post there (EQUITY-xx, naming the
account to add). C.1 #7 does not require it, so no organisation turns ✖.

**The flow:**
1. An approved cash issue is a **pending subscription** for each subscriber and amount.
2. **Cash arrives.**
   - Before the effective date: a bank line is matched to the pending subscription (a new match target in bank
     reconciliation) and posts Dr Bank · Cr 2040 (subscriber), on the bank date.
   - On the effective date: the bank line matches the issue directly, Dr Bank · Cr 3010/3020.
3. **Effective.** Recording the issue as effective posts Dr 2040 (subscriber) · Cr 3010/3020. It is refused until the
   subscriber's 2040 balance covers the issue (or the same-day bank line is matched). A cash issue is never made
   effective unpaid (Non-Goals).
4. **If the issue does not proceed,** the money is refunded from 2040 through a payment batch (FR-BNK-12).

G4-043 is the pinned case. 26 Feb: Dr Bank · Cr 2040, with no C.1 #30 warning, since 2999 is untouched. 12 Mar: Dr
2040 · Cr 3010. The February balance sheet shows the liability, and the cap table changes on 12 Mar only.

### D6 SAFEs and convertible notes (FR-EQ-18, G4-044)

**The instrument.** `instrument` holds:
- its form: post-money SAFE with a cap, pre-money SAFE, discount only, MFN, or convertible note;
- the holder (marked investor), amount, date, valuation cap, discount, MFN clause, and interest and maturity (notes);
- the conversion trigger and the class it converts into;
- the denominator as its terms define it;
- how note interest converts;
- the conversion price's precision;
- the accountant's classification, liability or equity [verify per instrument].

**Recording.** Recording is approved (FM-1). The cash is matched like D5: Dr Bank · Cr 2700, keyed by instrument.
2700 gains the role `convertible-instruments` and the instrument as its sub-ledger key, so each instrument's balance
reads from the ledger (ENG-04).

**Templates.** `instrument_template` lists the forms the engine may convert automatically. Only
`yc-sg-post-money-cap@1` is seeded: YC's "SAFE: Valuation Cap, No Discount (Singapore)". It converts automatically
only once its `lawyer_confirmed` evidence is attached (question 33). Every other instrument, and any term the template
does not cover (an MFN, a discount, interest), converts by a draft for the accountant with ⚠.

**Conversion at a priced round** (the trigger is a recorded issue_cash round, or an approved round scenario, D8):
- **The post-money cap price** is cap ÷ the company capitalisation, which includes the converting SAFEs' own shares.
  Each post-money SAFE s with amount A_s and cap cap_s converts into A_s ÷ cap_s of that capitalisation, so the SAFEs
  together take f = Σ A_t ÷ cap_t and the capitalisation is C₀ ÷ (1 − f), over the shares C₀ the terms count before the
  round (shares, options and the pool, as the template says). Each one's cap price is cap_s × (1 − f) ÷ C₀. (Built in
  slice 4b-i: the earlier n_s = A_s × C₀ ÷ (cap_s − ΣA) is the same only when every cap is equal; the two-SAFE test pins
  the difference.)
- **A pre-money cap price** is cap ÷ C₀.
- **The conversion price** is the lower of the cap price and the discounted round price, as the terms say, at the
  recorded precision, cut down (never rounded up, so the shares never cost more than the terms allow) [accountant to
  confirm]. The round's own price always competes: when it is below the cap price, the SAFE converts at the round's
  price, and a post-money SAFE then carries a review for the accountant, since its capitalisation assumed the cap.
- **The shares** come from `RND-SHARE`(amount, price). The remainder stays on 2700 until settled under the terms.
- **The draft** is an `instrument_conversion` transaction (Dr 2700 · Cr class), for approval.

G.3 pins both conventions:
- pre-money: 52,083 shares at S$2.40, with S$0.80 left;
- post-money: 52,631 shares at S$2.375, with S$1.37 left; founders 79.83%, investor 15.97%, SAFE 4.20%.

C.2 #29 (⚠) flags an instrument past its maturity, or a priced round recorded without its conversions.

### D7 The cap table and its percentages (FR-EQ-20)

`equity.CapTable(at)` reads the movements, the outstanding instruments (as converted at a stated price: the round's,
or one the user gives) and the grants and unallocated pool (D9).

**What it returns.** By holder and class:
- issued shares, with the ownership percentage;
- voting percentage, from votes per share;
- fully diluted shares and percentage.

**Exactness.** Percentages are exact `apd` quotients, shown to 2 decimals half-up (`RND-DISPLAY`) and never forced to
sum to 100.00. G.3's figures happen to.

**Compare and drill-down.** Two dates compare side by side. Each figure lists its movements with their transactions
and evidence.

**Export.** *Superseded in slice 3b:* XLSX goes through report exports: a new kind `cap_table`, which answers 202 with
its operation (FR-INT-11). PDF goes through the render service's report layout.

*Built instead:* `exportCapTable` exports the cap table at a day directly, as `exportFinancialStatement` does: CSV, XLSX
or PDF from the render service, audited with its hash. A cap table is one row per holding, so it is no long-running work
for FR-INT-11's 202. Nor does it need a background kind that the Reports capability would let a Bookkeeper ask for.

**API (slice 3b).**
- `getCapTable` takes `at` and `compare`. Each line lists its movements with the transaction (and its evidence) or the
  share issue.
- `getMyHolding` serves the Board viewer's "own holding". The whole table "if granted", and an Employee's own grants,
  arrive with the pool.

The table is never written, so it cannot be edited.

**Built in slice 3a** (`equity.TableAt`, `equity.TableIn`, `rounding.Ownership`):
- Each holding's votes are its shares × the votes per share of its class's version recorded by the cap table's day, or
  of its first recorded version for a day before that.
- Shares of an account with no recorded class count one vote a share, flagged as assumed, until the account has a
  class [accountant to confirm]. This follows the Companies Act 1967's default of one vote a share for equity shares
  [verify].
- A percentage of a zero total (no share votes) is undefined, never 0.00.
- Fully diluted equals issued until the instruments (slice 4) and the pool (slice 5) add to it.
- The oracle's `cap_table` reads its own register of movements. The format gains `share_transfer`, `votes_per_share`
  and `cap_tables` (test/differential/README.md, "Cap tables"). `golden/cap-table.json` pins G.3's round, and the
  generator's `equity` source runs in `make differential`.

### D8 Round scenarios (FR-EQ-21)

**The scenario.** `round_scenario` is versioned. It holds the inputs:
- pre-money valuation, or price per share;
- each investor and their new money;
- a pool top-up, before or after the money;
- which instruments convert;
- pro-rata take-up.

The outputs are computed on read by the same engine as D6 and D7, and never stored. Scenarios list side by side.

**Approval.** Approving a scenario (FM-1) creates the draft transactions: an issue_cash per new investor, an
instrument_conversion per converting instrument, and a pool resolution draft for a top-up. Nothing posts until each
draft follows D4. Modelling never posts. The AI's "read lane" for scenarios waits for `p1-ai-assistant`.

(Slice 6a, as built) The conversions are not drafted at the scenario's approval: D6 drafts them from an approved round,
so they come from the round's issue once it is approved (draftInstrumentConversions), at the same price. A top-up's
pool resolution is dated the round's day, so a scenario with one is approved on or after it; its resolutions must be
named first. A pre-money valuation is of the shares fully diluted before the round (issued, options outstanding, the
pool unallocated, and a top-up before the money), and a pro-rata entitlement is the round's new shares × the holder's
part fully diluted before, rounded down [accountant to confirm both against the term sheet and the side letter].

### D9 The option pool and grants (FR-EQ-19, register part; the owner's decision)

**The pool.** `option_pool` is versioned by resolution: size, resolution reference and date, with evidence.

**A grant.** `option_grant` is insert-only. It holds the holder (marked investor, or a member's linked contact), the
number, the exercise price, the grant date, the vesting (cliff months, monthly over months, acceleration single or
double, recorded) and the expiry.

**Grant events.** `grant_event` (vested is computed, never stored) records exercise, forfeiture and expiry. An exercise
is an issue (D4) whose movements add shares and whose event reduces the grant.

**The pool's balance** is its size minus outstanding grants and exercises. The database refuses a grant beyond the
unallocated pool. (Slice 5a-i) It checks every day, not only the grant's: once a grant or a resolution is recorded, on
each day from its date on that a grant was made or a resolution passed, the options granted by then are at most the
latest resolution's size (FY293, under a lock on the organisation's pool). So an earlier-dated grant cannot take
options a later one has, and a smaller resolution cannot leave the grants uncovered. (Slice 5b-i) A forfeiture or
expiry is an `option_event` document: a forfeiture takes at most the options not vested on its day, an expiry at most
what is left, and both give their options back to the pool from their day [accountant to confirm against the plan];
"vested" never counts more than is outstanding, so a leaver keeps only what vested before they left. (Slice 5b-ii) An
exercise is the D4 kind `exercise`, an issue to the grant's holder of its class at its exercise price, made effective
from share application monies as a cash issue (share_issue_cash@1); it takes at most what is vested and not exercised,
checked when drafted and again on its effective day and later. Its options never return to the pool; a reversal of it
makes them outstanding from the reversal's day. A grant at an exercise price of 0 is not exercised for cash: how to
record shares issued for no consideration waits for the accountant [accountant to confirm].

**Access.** An employee reads only grants whose holder is the contact linked to them (Appendix D "V own grants").

### D10 Investor rights, controllers and exempt status (FR-EQ-22, FR-EQ-25)

**Investor rights.** `investor_right` is versioned per holder: board seat, observer, information rights (what and
when), pro-rata, and consent matters. The agreements are evidence.

**Exempt-private-company status.** Each recorded transaction re-tests it from the cap table. A corporate holder, or a
21st member, makes the company no longer exempt. When the test differs from the latest `exempt_status`, C.2 #28 warns
(⚠: re-check the directors' debit balances, FR-EQ-11). A person records the new status, as founders' money does; the
engine never records it.

**Controllers.**
- **The test.** A holder with more than 25% of the shares or of the votes is a controller. C.2 #27 shows ⏰ until a
  `controller_entry` records them in the company's register (within 7 days) and the central register (within 2
  business days).
- **Not assessed.** When a holder has recorded rights that could confer control (a board-appointment majority, or
  consent matters) and no confirmation, the check reports "Not assessed", never OK (G4-101).
- **Annual notices.** `controller_entry` also records the annual notices and replies. The named company secretary
  confirms conclusions (question 41).

### D11 The ACRA reconciliation (FR-EQ-26, C.2 #26; the owner's decision)

**The extract.** `acra_extract` holds the business profile's date and its PDF as evidence. Its holdings are typed in as
`acra_extract_holding` rows (holder, class, shares). It is insert-only; a newer extract supersedes the older.

**The comparison.** The register's holdings at the extract's date are compared, holder by holder and class by class.
Every difference lists on C.2 #26 (⚠) and on the reconciliation page. A difference where ACRA is right is fixed by a
reversing transaction citing the extract (D4).

**INV-13** (paid-up capital = 3010 + 3020) stays enforced at posting. C.1 #23 and C.2 #25 already check it.

### D12 Access, API and screens

**Operations** fall under `CapTable`:
- holders and holder records;
- classes;
- transactions with their steps and evidence;
- instruments and templates;
- the pool and grants;
- the cap table (`?at=`, `?compare=`, drill-down, export);
- scenarios;
- investor rights;
- controller entries;
- ACRA extracts.

**Writes** need step-up, as share issues do. Who may write:
- **Owner and Controller:** approve and record.
- **Accountant:** prepares and proposes (C/P).
- **Auditor and Tax agent:** read.
- **DPO:** masked reads (the identifier is already only 4 characters).
- **Board and Employee:** row-scoped to their own holding or grants, through `contact_member_link`.

**Screens** follow the canvas:
- **Basic:** "Who owns the company" (CapTable, an ownership chart with a table view, my holding, the latest round)
  and RoundSimulator with a few inputs.
- **Expert:** Holders and HolderRecord, ShareClasses, EquityRegister and EquityTransaction, Instruments, OptionPool,
  the fully diluted columns and the reconciliation.

Each screen gets help keys, a guide page `docs/user-guide/equity-register.md` (the founders' guide's register section
moves there) and its knowledge-base page.

### D13 The oracle (QA-ENG-02)

The Python oracle gains:
- `cap_table(movements, instruments, grants, at)`;
- `safe_conversion` under both conventions;
- `round_dilution`.

Each is written from §8.23, B.7 and G.3, not from the engine. Golden cases G.3 (both conventions), G4-043 and G4-044
run in `make test`, and the differential's generator gains equity scenarios.

### Audit events (AUD-01)

`holder_record.saved`, `share_class.saved`, `equity_transaction.*` (the document pipeline's own), `equity_step.recorded`,
`holding_movement.recorded`, `instrument.saved`, `instrument.converted`, `option_pool.saved`, `option_grant.recorded`,
`grant_event.recorded`, `round_scenario.saved`, `investor_right.saved`, `controller_entry.recorded` and
`acra_extract.recorded`, each in its unit of work through `require_audit`.

## Risks / Trade-offs

- **[The post-money SAFE algebra is wrong for several SAFEs or for the template's denominator]** → It is used only for
  the confirmed template; G.3 pins one SAFE; the oracle solves the same equations independently; a second pinned case
  with two SAFEs is worked by hand before the template is enabled.
- **[A typed ACRA holding is mistyped]** → The PDF is the evidence beside it. A difference shows on C.2 #26, never
  silently, and a person fixes the record or the typing, which is a new extract.
- **[2040 collides with the accountant's chart review]** → The code is tagged `[accountant to confirm]`. A rename later
  is an account merge (FR-COA-04), not a data migration.
- **[Unpaid allotments are refused]** → Plain refusal with the manual route. Cognizhi's go-live allotments are paid
  [accountant to confirm].
- **[Large surface: 9 screens, ~12 tables]** → Built in slices, each with its own PR, matrix rows and gates. The
  holdings backstop (D3) lands first, so every later slice writes through it.

## Migration Plan

- **Migrations** add tables and widen `contact`, `share_issue` and the sub-ledger key rule, NOT VALID then validated.
  Existing `share_issue` rows get a holding movement and the default class in a data step inside the migration, under
  the migrator with the organisation set (FORCE RLS).
- **The standard chart** gains 2040. Existing organisations add it when they first need it (D5).
- **Rollback** is by the reverse migration before go-live. After go-live, corrections are reversals (D4).

## Open Questions

- Which pool-and-SAFE denominator YC's Singapore form uses for "Company Capitalization". This is confirmed with the
  lawyer's review before the template converts automatically (question 33). Until then every SAFE converts by draft,
  which the design already supports.
