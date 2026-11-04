# Proposal

## Why

Founders pay company costs from their own pockets, lend the company money and, later, turn what it owes them into
shares. The workbook had the accounts (1250, 2500, 2120, 2700, 3010/3020, 3300, 6050) but no workflow, so these amounts
became manual journals without a trail. Fyniq has the accounts and their roles (`director-loan`,
`deferred-remuneration`, `share-capital`, `capital-contribution`) and refuses a manual journal to share capital
(INV-13). It has no person on a founder's balance, no "Paid personally", no settlement and no register of share issues.
This change builds PRD-001 §8.22 and Appendix B.7: what the company owes each founder, from the first receipt to the
share allotment, with the evidence the accountant, the tax agent and investors will ask for. It sits on the longest
path to launch: the equity register (`p1-equity-register`), then the workbook migration (`p1-workbook-migration`),
then launch readiness each wait on the one before.

## What Changes

- **The owner's decisions of 3 Nov 2026, taken interactively (PRD-001 rev 0.167):**
  - a director account belongs to a **contact marked director or shareholder**, optionally **linked to a member**. The
    link lets the person see "Money I put in", keep a claim as a loan, be repaid to their own approved bank account
    (FR-AP-22) and confirm their balance with a passkey. Every journal line on 2500 or 2120 carries the contact as its
    sub-ledger key, which the database requires (B.7). The alternative was members only;
  - **repay, keep, convert to shares and waive** are built here. Converting to a SAFE or note comes with the equity
    register's instruments (FR-EQ-18). The alternative was all five routes now;
  - **exempt-private-company status** is recorded as an effective-dated fact of the organisation until the corporate
    profile (FR-CS-01) takes it over. While none is recorded, the warning cites s162. The alternative was waiting for
    the corporate profile;
  - **the founder pay choice** is recorded from go-live with its signed decision ("No salary" by default, question 16).
    A director's fee approved by the shareholders posts Dr 6050 / Cr 2120. The pay run's part and C.2 #24 come with
    payroll (`p1-payroll-link`). The alternative was leaving FR-EQ-04 to payroll.
- **Director accounts** (FR-EQ-01, FR-EQ-14):
  - the person on every line of 2500 and 2120, from every source that posts there: manual journals, opening balances,
    bank transactions, claims, fees, settlements and conversions;
  - each person's balance and statement by origin, with a PDF for balance confirmations;
  - the **Director accounts** page (Expert) and **Money I put in** (Basic).
- **Paid personally** (FR-EQ-02): a director's claim settles to 2150 (reimburse) or to their 2500 (keep as a loan).
  Another founder approves it.
- **Costs before incorporation** (FR-EQ-03): recorded with their original date, flagged for the accountant's
  decision, posted at the books start.
- **Pay choice and directors' fees** (FR-EQ-04, part).
- **Settlement routes** (FR-EQ-05, FR-EQ-07):
  - each is an approval object that shows its ledger impact before it is approved;
  - FM-9 applies: the beneficiary never approves, and the accountant's review is added when every available approver
    benefits;
  - repayment goes through a payment batch;
  - the default route and price basis are settings.
- **Conversion to shares and the register of share issues** (FR-EQ-06, FR-EQ-08, FR-EQ-09):
  - approve, then effective on the date ACRA registers the allotment, never backdated;
  - shares by `RND-SHARE`, with the remainder kept;
  - the ACRA steps tracked with their evidence;
  - share capital posted only from the register or opening balances (INV-13).
- **Presentation** (FR-EQ-10, FR-EQ-11):
  - non-cash financing in the cash flow;
  - shares issued for cash and for non-cash consideration apart in the movement in equity;
  - a person's net debit shown under 1250;
  - the related-party schedule.
- **Year-end confirmation** (FR-EQ-12): each director confirms their closing balance with a passkey.
- **Checks:** C.2 #21 (account in debit), #22 (ACRA steps), #23 (balance not confirmed) and #25 (share capital against
  the register), on the checks engine.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. The rev 0.167 scenarios are already in the main specs `equity/founders-money` (FR-EQ-01, 04, 05, 11) and
`ledger/posting-rules` (B.7), converted with the PRD revision. This change builds FR-EQ-01 to FR-EQ-12 and FR-EQ-14
(FR-EQ-04 without the pay run, FR-EQ-05 without the SAFE route), Appendix B.7's founders' rows, §10.2's founder
rows, FM-9 and C.2 #21, #22, #23 and #25 (`skip_specs: true`). FR-EQ-13 (shares for services) is P2.

## Impact

- **Migrations:**
  - contacts marked director or shareholder, and their link to a member;
  - `journal_line.party_id`, required on 2500 and 2120 and refused elsewhere;
  - the founders' documents: `director_fee`, `director_settlement`, `director_repayment`, `pre_incorporation_cost`;
  - the tables `share_issue`, `director_pay_choice`, `exempt_status` and `director_confirmation`;
  - the posting source `share_issue`, the only one allowed on share capital besides opening balances.
- **The engine and the oracle:**
  - a line's person;
  - `RND-SHARE`;
  - the legs of a claim kept as a loan, a fee, a repayment, a waiver and a conversion.
  The Python oracle states each independently.
- **The balance service:** balances and movements by person (ENG-04).
- **The API:**
  - director accounts and statements;
  - contacts' roles and member link;
  - settlements, conversions, the register;
  - pay choices, fees, exempt status, confirmations, costs before incorporation.
  Then `make openapi`.
- **Permissions:** Appendix D's row "Director accounts, settlements, share issues" (already in `authz`); approval
  objects for the settlements, fees and member link; FM-9 in the approval engine.
- **Screens:**
  - Director accounts, Settle a balance, Money I put in and Costs before incorporation;
  - the register of share issues on the equity register's screen;
  - the person on the claim editor, contact record, categorise, journal and opening balances screens.
  Each gets its help keys and guide pages compiled into `kb/`. The canvases `people-ledger`, `equity`, `workflows`,
  `sales-purchases` and `banking-claims` are republished.
- **Out of scope:**
  - the SAFE or note route (`p1-equity-register`);
  - the pay run's deferral and C.2 #24 (`p1-payroll-link`);
  - shares instead of pay as an equity transaction (FR-EQ-17), and holders, classes with rights and the cap table
    (`p1-equity-register`);
  - the statutory calendar's view of the ACRA tasks (FR-TAX-07, `p1-gst`);
  - the corporate profile (`p1-company-secretary`);
  - FR-EQ-13 (P2).
