# Design

## Context

What exists that the founders' money builds on:

- **The chart:**
  - the roles `director-loan` (2500), `deferred-remuneration` (2120), `share-capital` (3010, 3020) and
    `capital-contribution` (3300) (migration 000024);
  - 6050 Directors' fees and 1250 Staff & director advances in the default chart.
  A manual journal to share capital is refused by the domain and by the posting-source check (000014, INV-13).
- **The ledger:** `ledger.Line` has an account, amount, rate, base amount and description, but **no person**. The
  AR and AP sub-ledgers are document-based (`internal/domain/subledger`), and `balance.Control` reads a control
  account's lines by source type (INV-07).
- **Contacts** are customers or vendors (`contact_role_known`). A vendor's bank account changes under dual control
  (`payee_bank_account`, FR-CT-03). A member's own account for reimbursements is in the same table, keyed by
  `member_user_id` (FR-AP-22).
- **Expense claims** are keyed to a member (the claimant). A claim posts on its approval (`claim.post`, which nobody
  approves for themselves, FR-AP-17), crediting 2150. Payment batches pay approved bills and claims, post on release
  and are matched by bank import (`p1-payments-out`).
- **Bank transactions** have one optional contact (`banktransaction.Draft.ContactID`) and parts to accounts.
- **The approval engine:** object types with ordered steps, the requester never signs, and a per-operation
  `executable` check for each signer (`internal/api/approvals.go`).
- **Statements:**
  - the cash flow lists non-cash investing and financing entries beside it (`financials.NonCash`);
  - the movement in equity has "Shares issued" (`financials/flows.go`);
  - customers' statements render to PDF through the render service (`p1-sales` 7.2).
- **The checks engine** (`p1-checks`): definitions with triggers, runs, items marked reviewed.
- **Step-up with a passkey** (WebAuthn) and evidence on any record (FR-DOC).
- **Appendix D's row** "Director accounts, settlements, share issues" is in `authz` as `DirectorAccounts`:
  - Owner and Controller A; Accountant C/P; Bookkeeper V;
  - Payroll V (deferred pay); Releaser R (repayments); Employee V (own);
  - Auditor, Tax agent V; Board V (share issues); AI ask only.
- **Ports touched (ADR-T8):** `storage` only. No cloud dependency; nothing new runs in the background beyond the
  checks' refresh.

## Goals / Non-Goals

**Goals:**

- every amount on 2500 and 2120 names its person, from every source, so each person's balance is exact by
  construction;
- each settlement shows its ledger impact before anyone approves it, and the beneficiary never approves it (FM-9);
- share capital moves only through the register of share issues or opening balances (INV-13), and a conversion
  posts on the date ACRA registered it, never earlier;
- the engine and the oracle agree on every founders' leg to the cent, including `RND-SHARE`.

**Non-Goals:**

- the SAFE or note route, holders' records, classes with rights, cash issues and the cap table (`p1-equity-register`);
- the pay run's deferral and C.2 #24 (`p1-payroll-link`);
- lodging anything with ACRA (Fyniq never lodges, §4.2) and the statutory calendar's view (`p1-gst`);
- the corporate profile (`p1-company-secretary`); FR-EQ-13 (P2).

## Decisions

### D1 The person: a contact marked director or shareholder, linked to a member

- **Contacts:**
  - `contact` gains `is_director`, `is_shareholder` and `member_user_id`;
  - `contact_role_known` widens to any of the four roles;
  - a member links to at most one contact (unique where set), keyed to `user_membership` as `payee_bank_account` is.
- **The member link changes under dual control (FM-1),** as an approval object `contact_member_link`:
  - an Owner or Controller asks;
  - another holder of A on "Director accounts, settlements, share issues" approves;
  - never the member being linked.
  The link decides where repayments go (D10) and who is excluded from approving a settlement (FM-9), so one person
  must never set it alone.
- **A contact with lines on 2500 or 2120 stays marked:** the database refuses removing the role (FY249).

### D2 The person on every line of 2500 and 2120

- **The column:** `journal_line.party_id`, nullable, with a foreign key to `contact`.
- **The backstop:** `journal_line_check` already reads the line's account, so it refuses:
  - a line on an account with the role `director-loan` or `deferred-remuneration` without a party;
  - a party that is not marked director or shareholder;
  - a party on any other account (FY250).
- **The engine:** `ledger.Line` gains `Party uuid.UUID`. The engine refuses the same three cases first, with a field
  and a fix (`LEDGER-41`, ENG-23). Existing rules set no party, so their replay stays byte for byte (ENG-06).
- **Every source names the person:**

  | Source | The person comes from | Origin (D3) |
  |---|---|---|
  | Manual journal | each line on 2500 or 2120 names it, with an origin | as entered |
  | Opening balances | each such line, with an origin (question 15: the founders' balances at the books start) | as entered |
  | Bank transaction | the transaction's contact, which must be a director or shareholder when a part posts there (`BANK-nn`) | lent (credit), paid out (debit) |
  | Expense claim kept as a loan (D6) | the claimant's linked contact | paid personally |
  | Cost before the books start (D7) | the director who paid | paid personally |
  | Director's fee (D8) | the person paid | fee |
  | Settlement and repayment (D10) | the settlement's person | paid out, waived |
  | Share issue (D11) | the holder | converted |

- **Lines posted before this change** have no person. There are none in production. On a local database the close's
  sub-ledger check names them (D4), and the sample company's generator gives every 2500 line a person from now on.

### D3 The origin of a movement

- **Derived from the source**, so it cannot drift from the posting:
  - a bank transaction is "lent" or "paid out";
  - a claim or a cost before the books start is "paid personally";
  - a fee is "fee", and the pay run's deferral will be "pay";
  - a repayment is "paid out", a waiver "waived" and a share issue "converted".
- **A manual journal or opening balance line** on those accounts records its origin in the document's payload: lent,
  paid personally, pay, fee, paid out or other.
- The statement groups movements by origin (FR-EQ-01).

### D4 Balances and statements by person, through the balance service (ENG-04)

- **`balance.People(asAt)`:** each person's balance on each `director-loan` and `deferred-remuneration` account, and
  their net. A net debit means the person owes the company.
- **`balance.PersonLedger(person, from, to)`:** the opening balance, movements by origin and the closing balance.
- **How they read:** the person is not in the cached balances, so both read the accounts' lines a period at a time,
  as `balance.Control` does, under the same `MaxControlLines` guard. Founders' accounts carry few lines; a cache is
  not needed.
- **The close's sub-ledger check** gains "Director accounts" (INV-07): the sum by person equals 2500 and 2120, and a
  line with no person is named as ✖.

### D5 The pages and the API

- **The API:**
  - `listDirectorAccounts(asAt)`: per person, their contact and member, balances, pending settlements, pay choice in
    force, confirmation status and warnings;
  - `getDirectorStatement(person, from, to)`, and its PDF through the render service, as customers' statements are;
  - `getMyDirectorAccount`: the signed-in member's own (Employee V (own)).
- **The pages:**
  - **Director accounts** (`/accounting/directors`, Expert): the people, the statement, the related-party schedule,
    pay choices and exempt status;
  - **Money I put in** (`/me/money-i-put-in`, Basic): the founder's own balance, latest items, pending settlements
    and confirmation.
  - The ⚠ and ⏰ of C.2 #21 to #23 show in both modes (FR-EQ-14, FR-UX-24).
- **Help:** each new field and chip has a help key, a guide page in `docs/user-guide/` compiled into `kb/`, and a
  tooltip (QA-UI-05).

### D6 Paid personally (FR-EQ-02)

- **The choice:** a claim gains `settle_to`, either reimburse (2150, today's rule) or loan. Loan is allowed only
  when the claimant's member is linked to a director contact (`CLAIM-nn`).
- **A claim kept as a loan:**
  - it cannot also settle a cash advance;
  - it is never reimbursed: the money stays owed on 2500.
- **The posting:** a new rule version, `claim@2`:
  - it credits 2500 (gross, the person, origin "paid personally") instead of 2150;
  - input GST only when the line meets FR-AP-20, otherwise the GST stays in the cost (B.7);
  - claims posted before it replay under `claim@1`.
- **Approval is unchanged:** nobody approves their own claim (FR-AP-17), which in Founder mode is the other founder
  (FM-2, FM-3).

### D7 Costs before the books start (FR-EQ-03)

- **The record:** a `pre_incorporation_cost` document holds:
  - its original date, which must be before the books start (the incorporation date belongs to the corporate profile,
    FR-CS-01, so the books start is the boundary until then);
  - the supplier and a description;
  - the director who paid, and the gross amount in SGD with any GST shown;
  - the receipt as required evidence.
- **States:**
  - **needs decision**, shown as ⚠;
  - **decided:** the accountant's treatment (revenue cost deductible under s14R, capital, or not deductible
    [verify]), the account, and the input tax (not claimed while unregistered; a claim needs IRAS's approval in the
    first GST return);
  - **posted:** at the books start, or on the decision date when the books start's period is closed. It posts
    Dr the account (gross, or net and 2200 when claimed), Cr 2500 (the person, paid personally).
- **Who:** the Accountant or the Controller decides; an Owner or Controller records. The "before GST registration"
  flag on ordinary claims comes with GST registration (`p1-gst`; question 2: not registered).

### D8 Pay choices and directors' fees (FR-EQ-04, part)

- **`director_pay_choice`:**
  - per person, from a month, one of five choices: no salary, salary paid, salary deferred, director's fee, or shares
    instead of pay;
  - the signed decision is required as evidence;
  - append-only, so a later row supersedes from its month, and audited;
  - with no row, the choice is "No salary" (recommended default 16, FR-ORG-08).
  "Shares instead of pay" is recorded as a choice. Its equity transaction comes with FR-EQ-17.
- **A `director_fee` document:**
  - holds the person, the date they became entitled (the meeting that voted it), the period it covers and the amount;
  - holds the account, defaulting to the chart's directors' fees account (6050);
  - requires the shareholders' resolution as evidence;
  - is an approval object (`director_fee.post`) that the person paid never approves (FM-9's reading for fees);
  - posts Dr 6050 / Cr 2120 (the person, fee) on the entitlement date (§10.2).

### D9 Exempt-private-company status (FR-EQ-11)

- **`exempt_status`:** effective-dated rows, each with a from date, exempt or not, the evidence (the ACRA business
  profile) and who recorded it, audited. The corporate profile takes the table over when it is built.
- **The warning on a net debit:**
  - **exempt:** allowed, but disclosed to investors;
  - **not exempt, or nothing recorded:** cites s162 and its exception for advances for company expenses (approved in
    general meeting, or repaid within 6 months after the next AGM).
- **A new row** re-runs C.2 #21 (a trigger, D15).

### D10 Settlements: repay, keep and waive, with FM-9 (FR-EQ-05, FR-EQ-07)

- **The `director_settlement` document** names:
  - the person, the account (2500 or 2120) and the amount, no more than what is owed at the request;
  - the route and its details:
    - **keep:** the terms (interest, default none; a repayment date or on demand; subordinated or not) and the loan
      note or board minute;
    - **repay:** the person's bank account in force: the linked member's (FR-AP-22), else the contact's (FR-CT-03);
    - **waive:** the credit account (the capital-contribution account, or an income account the accountant picks),
      the capacity (as a shareholder, or otherwise) and the reason;
    - **convert:** D11.
- **Approval:**
  - asking makes an approval request `director_settlement.approve` whose envelope carries the ledger impact,
    worked out by the rule as a dry run (FR-APR-02);
  - the defaults "keep as a loan" and "latest priced round" are organisation settings (recommended default 15).
- **FM-9 in the approval engine:**
  - a signer may not be the person's linked member (`APR-15`);
  - **every available approver benefits** when each member who could sign, other than the person, is themselves a
    director with a settlement of their own waiting or approved in the last 30 days. Then the request gains a final
    step that the Accountant role signs, recording their review. This reading of "for example, all founders convert
    together" is tagged [accountant to confirm].
- **On approval:**
  - **keep** records the terms, with no entry;
  - **waive** posts Dr 2500/2120 (the person, waived) and Cr the chosen account, on the organisation's today. When
    the waived account is 2120, or its movements include fees, it is tagged "deducted earlier" for the tax
    computation [verify];
  - **repay** becomes payable: a payment batch item of a third kind, `director`. Its release posts a
    `director_repayment` (Dr 2500/2120, the person, paid out; Cr bank), matched by bank import as other batch
    payments are (FM-1).

### D11 Conversion to shares and the register of share issues (FR-EQ-06, FR-EQ-08)

- **Approve step** (the settlement's convert route):
  - the amount;
  - the class: a share-capital account, 3010 ordinary or 3020 preference;
  - the price per share with its basis (latest priced round, independent valuation, or a price every shareholder
    approves) and its evidence. The equity register's rounds are not built, so the price is entered;
  - the founder's capacity: lender, employee or shareholder;
  - the four documents: the shareholders' approval, the board resolution, the conversion agreement and the founder's
    signed balance confirmation, all required before asking (FR-DOC-06).
  - **The shares:** `n = RND-SHARE(amount, price)`; the converted amount is round(n × price, 2), and the remainder
    stays on the account.
  - **The measurement basis:** carrying amount (the default), or fair value as the accountant chooses with a note.
    Under fair value, the difference goes to a profit-or-loss account the accountant picks, since the CoA review has
    not assigned one.
- **Effective step:**
  - the lodgement reference and the effective date: the date ACRA's register shows the allotment, no later than
    today and no earlier than the approval;
  - in one transaction it writes a `share_issue` row (holder, class, number, price, amount, consideration "debt
    capitalised", resolution references, lodgement reference, effective date, settlement) and posts from the source
    `share_issue`: Dr 2500/2120 (the person, converted), Cr the class's account;
  - an effective date in a closed period is refused, and reopening the period is the fix (never a later date).
- **INV-13:** the posting-source check allows share capital from `share_issue` as well as `opening_balance`.
- **Shares issued before the books start** (the incorporation subscription) are recorded in the register without an
  entry, so C.2 #25 compares share capital with the register from the start.
- **`RND-SHARE`** (`internal/domain/rounding`): the largest whole n with round(n × price, 2) ≤ amount, half up;
  never fractional shares. Property tests, and the oracle states it independently.

### D12 ACRA steps (FR-EQ-09)

- **An approved conversion records:**
  - the resolution date;
  - when the shareholders' approval was lodged, with its evidence;
  - the date by which the next AGM must be held: by default six months after the end of the latest financial year
    that ended on or before the resolution, else of the year it falls in [verify], editable with evidence;
  - when the return of allotment was lodged;
  - the effective date (D11).
- **What they raise:**
  - **⏰** until the approval is lodged, due 14 days after the resolution (s161(5), s186);
  - **⚠** when the conversion is not registered a set number of days after its approval (a setting, default 14);
  - **⚠** within 30 days of the next AGM date while not effective. After that date the conversion can no longer
    become effective (`FOUNDER-nn`), and a new approval is the fix.
- **Where they show:** on the settlement page and in C.2 #22. The statutory calendar shows them once FR-TAX-07 is
  built.

### D13 Year-end confirmation (FR-EQ-12)

- **`director_confirmation`:** the person, the financial year's end, the balances confirmed (2500 and 2120 at that
  end, from `balance.People`), and who confirmed and when. It is append-only and needs a passkey step-up by the
  linked member.
- **Confirmed** means the latest confirmation's balances equal the year-end balances now. Anything else is ⚠: C.2 #23
  and the year-end close checklist's item.
- **A director with no member link** cannot confirm; the warning says to link the member (D1).

### D14 Presentation (FR-EQ-10, FR-EQ-11)

- **Balance sheet:** at a period end, a person's net debit on 2500 and 2120 shows under 1250 Staff & director
  advances, with no posting. The statements' builder reads `balance.People`.
- **Cash flow:** conversions and waivers move no cash, so they are already in the non-cash list. They are labelled
  "Converted to shares" and "Waived", and never shown as shares issued for cash and a loan repaid.
- **Movement in equity:** shares issued are split into cash and non-cash consideration, by the register's
  consideration type.
- **Related-party schedule:** per person and nature (loan, deferred pay, fees for the year, converted, waived), as at
  a date, from the balance service. It shows on Director accounts and can be exported. The notes (FR-RPT-11, P3) read
  it.

### D15 The checks (C.2 #21, #22, #23, #25)

| Check | Severity | Fires when | Reruns on |
|---|---|---|---|
| C.2 #21 | ⚠ | a person's net balance on 2500 and 2120 is a debit at the latest month end; the words follow D9 | their sources, and `exempt_status` |
| C.2 #22 | ⏰ / ⚠ | D12's three cases | settlements and share issues |
| C.2 #23 | ⚠ | a director's balance for the latest ended financial year is not confirmed (D13) | confirmations and sources |
| C.2 #25 | ✖ | a share-capital account's balance differs from its register | share issues and opening balances |

Each has a fixture that fires it and one that passes (QA-RPT-02).

### D16 The oracle

- **The scenario format** gains a line's `party`, and the founders' events:
  - a claim kept as a loan, a fee, a repayment and a waiver;
  - a conversion with its price.
- **The oracle (`oracle/`) independently states:**
  - the per-person sub-ledgers;
  - `RND-SHARE`;
  - each leg of B.7 and §10.2's founder rows, from the PRD's text.
- **The tests:** golden cases and the differential sample carry founders' events. The engine and the oracle agree to
  the cent (QA-ENG-02).

## Risks / Trade-offs

- **A new column on `journal_line`, the busiest table.** It is nullable with no default, so adding it does not
  rewrite the table. The trigger reads the account row it already fetches.
- **Balances by person from lines, not a cache.** Founders' accounts carry few lines. `MaxControlLines` refuses a
  pathological period rather than slowing the page.
- **FM-9's "every available approver benefits"** is read as D10 says [accountant to confirm]. A stricter reading
  adds the accountant to every founder's settlement, which a setting could switch on later.
- **The next AGM date's default** is a reading of the Companies Act's AGM timing for private companies [verify].
  It is editable, so a wrong default costs a correction, not a missed lapse.
- **A conversion registered in a closed period cannot post** until the period is reopened (FR-GL-08). Posting it on a
  later date instead would backdate the balance sheet's truth the other way.

## Migration Plan

- **Expand only.** New columns are nullable or defaulted, and new tables have RLS forced. The widened checks are
  added NOT VALID and validated in their own migrations (squawk). There is no backfill, since there is no production
  data (DEV-08).
- **The sample company's seed** gives its 2500 lines a person. The golden cases are regenerated through the oracle.

## Open Questions

None that change what is built: the owner decided the four on 3 Nov 2026. Three readings carry tags for the
review register: FM-9's accountant step and the waiver's tax tag [accountant to confirm], and the next AGM date
[verify].
