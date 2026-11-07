---
key: equity.register
type: task
title: The equity register: who owns the company, holders, share classes and the equity transactions that move every share
summary: The cap table at any day, holders (last 4 only), share classes, equity transactions made effective with ACRA, SAFE conversions and the option pool
sources:
  - docs/user-guide/equity-register.md sha256:4574d09e37dfaba9f92713ac08871f9d30062b9cb1c9074613134a854d78dfc5
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
aliases:
  - cap table
  - who owns the company
  - ownership percentage
  - my holding
  - shareholding
  - fully diluted
  - voting percentage
  - compare cap table
  - export cap table
  - equity register
  - register of members
  - shareholders
  - investors
  - holders
  - holder record
  - add a shareholder
  - add an investor
  - shareholder details
  - NRIC last 4
  - identity document
  - share class
  - preference shares
  - class rights
  - liquidation preference
  - anti-dilution
  - pro-rata right
  - constitution
  - share issue
  - register of share issues
  - allotment
  - shares issued before the books start
  - equity transaction
  - issue shares for cash
  - share transfer
  - transfer shares
  - convert shares between classes
  - reverse a share issue
  - ACRA steps
  - return of allotment
  - notice of transfer
  - effective date
  - share application monies
  - refund a subscription
  - convert a SAFE
  - SAFE conversion
  - convertible note conversion
  - priced round
  - conversion price
  - valuation cap
  - record a SAFE
  - convertible note
  - preview a conversion
  - SAFE template
  - categorise SAFE money
  - 2700
  - option pool
  - ESOP
  - employee share options
  - grant options
  - option grant
  - vesting
  - cliff
  - exercise price
  - unallocated options
  - my options
  - forfeit options
  - options lapse
  - leaver options
  - exercise options
  - exercise price
  - my shares and options
  - my options vesting
---

# The equity register: who owns the company, holders, share classes and transactions

## What it is

- **Equity** is the company's register of who owns it: shareholders and investors, share classes
  with their rights, and the equity transactions that move every share. Its first page, **Who owns
  the company**, is the cap table, in Basic and Expert mode.
- Tabs move between **Cap table**, **Holders**, **Classes**, **Transactions** (with the register
  of share issues) and **SAFEs and notes**, all but the first Expert pages; the option pool and
  rounds join them later.
- Holders and classes post no entry; share capital changes only through the register of share
  issues, the opening balances and a conversion between classes on different accounts
  ([[founders.director-accounts]]).

(src: docs/user-guide/equity-register.md#what-it-is)

## The cap table

- **Equity › Cap table**, **Who owns the company**, shows who owns what on a day, computed from the
  register: shares issued before the books start, and each equity transaction from the day ACRA's
  register shows it. It cannot be edited; to change a holding, draft an equity transaction.
- **As at** picks the day (empty is today). **Ownership** is a bar with a legend, or a table with
  **Table**: each percentage is exact and shown to 2 decimals, so they do not always add up to
  100.00.
- **My holding** is the shareholder you sign in as, linked on Directors' accounts
  ([[founders.director-accounts]]); **Latest round** the most recent issue for cash; a note lists
  transactions approved and waiting for ACRA, whose shares count only once registered.
- Basic lists each shareholder, type of share, shares and what they own. Expert's **By holder and
  class** shows issued, the percentage of shares and of votes, and fully diluted: each holder's shares
  and the options granted to them, the total adding the **Unallocated option pool** on a row of its
  own; a holder with options and no shares has a line of options only; SAFEs and notes count once
  they convert. **Votes assumed**: the class is not recorded yet, so its shares count one vote each.
- A Board viewer sees only their own holding.

(src: docs/user-guide/equity-register.md#the-cap-table)

## Compare two days

- Expert: **Compare with** takes a second day; **Since** shows the shares issued on each day and
  each holder's percentage then and now (0.00% for someone who held nothing).

(src: docs/user-guide/equity-register.md#compare-two-days)

## See where a figure comes from

- Expert: select a holding's shares in **By holder and class** to list its movements, oldest
  first: each equity transaction, opened to see its evidence, or the register of share issues.

(src: docs/user-guide/equity-register.md#see-where-a-figure-comes-from)

## Export the cap table

- **Excel** (a sheet of holdings and a sheet of holders, each with a total) or **PDF** (to print),
  at the day shown; each export is recorded in the audit trail with its day, format and hash.

(src: docs/user-guide/equity-register.md#export-the-cap-table)

## Holders

- **Equity › Holders** lists each shareholder and investor with the identifier from their record,
  their shares by class, and whether their details are recorded; filter by **All**,
  **Shareholders** or **Investors**, or search by name.
- A holder is a contact: tick **A shareholder**, or **An investor** for a SAFE, note or option
  holder with no shares yet, in Contacts ([[contacts.manage]]). **Add a holder** goes there.

(src: docs/user-guide/equity-register.md#holders)

## Record a holder's details

- Open a holder: **Details in force** shows individual or entity, the identifier, nationality or
  place of incorporation, the registered address and the people to contact.
- An Accountant selects **Draft the details** (or **Change the details**). For an individual,
  choose NRIC, FIN or passport and enter **only the last 4 characters**, such as 567D; the full
  number stays in ACRA's register and the identity document. An entity gives its UEN or foreign
  registration number.
- **Draft the record**, attach the identity document, then **Ask for approval**: another Owner or
  the Controller approves it, never the asker and never the holder. The record before it moves to
  **Earlier records** ([[approvals.decide]]).
- A draft changes until approval is asked for; a recorded record changes no more, so draft a new one.
- The Owner, Controller, Accountant, Auditor and Tax agent read holders; the data protection officer
  reads them masked and opens no file. Identity documents are never opened in the app.

(src: docs/user-guide/equity-register.md#record-a-holders-details)

## Share classes

- **Equity › Classes** records each class's rights as the constitution gives them; Fyniq does not
  compute with them yet.
- **Draft a class**: its name, ordinary or preference (with a series such as A), and its account,
  3010 Ordinary or 3020 Preference share capital; then the rights: votes per share, liquidation
  preference (multiple, participating, cap, seniority), dividends (none, non-cumulative or
  cumulative), conversion into ordinary shares, anti-dilution (none, broad-based weighted average or
  full ratchet), the pro-rata right in later rounds and any redemption terms.
- Attach the constitution and the shareholders' agreement, then **Ask for approval**; another
  Owner or the Controller approves. New rights for a recorded class are a new draft; a class keeps
  its account.
- Until an account has a recorded class, its issues count under the account; after that each new
  issue names a class, or takes the account's first recorded class.

(src: docs/user-guide/equity-register.md#share-classes)

## Equity transactions

- **Equity › Transactions** lists issues for cash, transfers, conversions between classes, SAFE
  or note conversions and reversals, the latest first: reference (EQ-, on approval), effective date, holders, class, shares
  and status (**Draft**, **Waiting for ACRA**, **Effective**, **Reversed**); **Due** means an ACRA
  lodgement is due, **Review** that it has waited too long.

(src: docs/user-guide/equity-register.md#equity-transactions)

## Draft a transaction

- **New transaction** (an Accountant): an **issue for cash** (one subscriber, the class, shares,
  price per share, resolutions; a round with several investors is several issues); a **transfer**
  (from, to, class, shares; no entry, share duty is the parties' matter); a **conversion between
  classes** (the carrying amount moves between 3010 and 3020 when the accounts differ); a
  **reversal** (what it undoes and why).
- **Draft the transaction**, attach what it rests on, then **Ask for approval**: another Owner or
  the Controller approves, never a holder it is for; the Accountant reviews too when every other
  approver has a transaction of their own waiting or approved in the last 30 days
  ([[approvals.decide]]).

(src: docs/user-guide/equity-register.md#draft-a-transaction)

## Make it effective

- Record the **ACRA steps** as they happen: an issue's shareholders' approval (lodged within 14
  days), AGM date and return of allotment; a transfer's instrument and notice of transfer; a
  conversion's resolution and lodgement. **Due** shows while a lodgement is due.
- **Record the effective date** (an Owner or the Controller) when ACRA's register shows it: never
  after today or before the approval, in an open period. Shares move only then.
- A cash issue takes effect once the subscriber's money is in **2040 Share application monies**:
  categorise their payment's bank line to 2040, naming them ([[banking.categorise]]); it then posts
  Dr 2040 · Cr share capital. A refund is paid from the bank and its line categorised to 2040 the
  same way.

(src: docs/user-guide/equity-register.md#make-it-effective)

## SAFEs and convertible notes

- **Equity › SAFEs and notes** lists each SAFE and note: holder, form, amount, day signed, terms
  (cap, discount, a note's interest and maturity), the accountant's classification, what **2700
  Convertible notes / SAFE** holds for it today, and its status: **Draft**, **Waiting for
  approval**, **Recorded**, **Converting** or **Converted**.

(src: docs/user-guide/equity-register.md#safes-and-convertible-notes)

## Record a SAFE or note

- **Add instrument** (an Accountant): the holder (an investor or shareholder), the form
  (post-money, pre-money, discount-only or MFN SAFE, or a convertible note), the amount paid and
  day signed, the terms its form has, what converts it, the class, how its terms count the shares,
  and the price decimals; the accountant records its classification, liability or equity.
- **Draft the instrument**, attach the signed SAFE or note, then **Ask for approval**: another
  Owner or the Controller records it, never the holder.
- Its money comes in on a bank line categorised to 2700, choosing the recorded instrument under
  **SAFE or note** ([[banking.categorise]]); a repayment out the same way. A line to 2700 naming no
  recorded instrument is refused. A founder's balance can become its money: settle it on Director
  accounts with **Convert to a SAFE or note** ([[founders.director-accounts]]).
- **Checks** warns when a recorded note is past its maturity, or shares were allotted for cash
  after a SAFE was signed and it has not converted: draft its conversion, or settle the note.

(src: docs/user-guide/equity-register.md#record-a-safe-or-note)

## The forms Fyniq converts by itself

- **Forms Fyniq converts** lists the known templates. A conversion an enabled template covers in
  every term is ready for approval; any other form, or a discount, MFN clause or interest, converts
  by a draft the accountant completes (⚠).
- To enable one: **Enable it**, attach your lawyer's confirmation, ask for approval; another Owner
  or the Controller approves it, once.

(src: docs/user-guide/equity-register.md#the-forms-fyniq-converts-by-itself)

## Preview a conversion

- **Conversion preview** is a sandbox (nothing posts or is saved): choose an approved issue for
  cash under **At the round**, or type a round price, then **Preview**. Each instrument's working:
  ownership at the cap, cap price, discounted round price, the conversion price cut to its
  decimals, the whole shares, and what stays on 2700. At an approved round an Accountant can then
  **Draft the SAFE conversions**.

(src: docs/user-guide/equity-register.md#preview-a-conversion)

## Convert SAFEs and notes

- At a priced round, on the round's approved issue for cash, an Accountant selects **Draft the SAFE
  conversions**: one **SAFE or note conversion** per instrument the round converts, with its shares
  and conversion price.
- The price is the lower of the cap price and the round's price less its discount, cut down to the
  decimals its terms give; a post-money SAFE's cap price counts the shares every converting
  post-money SAFE takes, a pre-money SAFE's is its cap divided by the shares before the round.
- The shares are its money divided by that price, rounded down to whole shares; what is left stays
  on **2700 Convertible notes / SAFE** until settled under its terms.
- One an enabled template covers in every term is ready for approval; any other shows **The
  accountant completes it** (a discount, an MFN clause, a round below its cap price): check the
  shares and price against the terms first. The holder and class are the instrument's.
- Attach the resolutions and ask for approval; an instrument converts once while its conversion
  stands. It becomes effective on or after its round while 2700 holds its money, posting Dr 2700 ·
  Cr share capital.

(src: docs/user-guide/equity-register.md#convert-safes-and-notes)

## The option pool and grants

- The **option pool** is the shares a resolution sets aside for options (an employee share option
  plan): its size on a day is the latest recorded resolution passed on or before it, set again from
  a later one's day. **Unallocated** is its size less the options granted.
- A **grant** gives a person options: how many, the exercise price per share, the grant date, the
  vesting (a cliff, then evenly by month), its acceleration on a change of control and the day it
  lapses; its holder is a contact marked An investor or A shareholder, of the pool's class.
- An Accountant drafts each, attaches the signed resolution or the grant letter and asks; another
  Owner or the Controller records it, never the grant's own holder.
- A grant fits only what the pool leaves on its grant date and on every later day a grant was made
  or a resolution passed; a resolution setting aside fewer shares than the options granted by then
  is refused.
- **Vested** is worked out each day, never stored: none before the cliff, then the options × the
  whole months since the grant ÷ the vesting months, rounded down to whole options; all once the
  vesting months have passed; a month passes on the grant's day, or a shorter month's last day
  [accountant to confirm].
- A person whose contact is linked to their sign-in sees their own grants, and nobody else's.
- A leaver's options not vested are **forfeited**; those vested and not exercised **expire** when
  their window closes, as at the grant's expiry date. An Accountant drafts the **Forfeiture** or
  **Expiry** on the grant with the notice; another Owner or the Controller records it, never the
  holder. A forfeiture takes at most what is not vested on its day, an expiry at most what is left;
  from its day the options leave the holder's fully diluted shares and return to the pool
  [accountant to confirm]. **Outstanding** is the grant less those forfeited, expired and exercised;
  **Vested** never counts more than the grant keeps.
- To **exercise**, an Accountant drafts an equity transaction **Exercise of options**: the grant,
  the number and the board resolution; the holder, class and price are the grant's, at most what
  is **Exercisable**. Approved like any issue, never by the holder, and effective once the holder's
  money is in share application monies: Dr 2040 · Cr share capital, shares × the exercise price.
  Exercised options never return to the pool; a reversal makes them outstanding again. A grant at
  an exercise price of 0 is not exercised this way yet [accountant to confirm].

(src: docs/user-guide/equity-register.md#the-option-pool-and-grants)

## Record the pool and a grant

- **Equity › Option pool** shows the pool (size by resolution, granted, forfeited or expired back in
  it, exercised and now shares, outstanding, vested, **available**), every grant with its vesting,
  expiry and status, what vests or expires in the next 30 days, and the resolutions that set it.
- An Accountant selects **Record a resolution** (shares set aside, class, day passed) or **New
  grant** (holder, options, exercise price, grant date, vesting, expiry), attaches the signed
  resolution or the grant letter on its page and selects **Ask for approval**; on a recorded grant,
  **Forfeit or let lapse** drafts a forfeiture or expiry with its notice.

(src: docs/user-guide/equity-register.md#record-the-pool-and-a-grant)

## My shares and options

- A person whose sign-in is linked to their contact sees their own grants under **My shares and
  options**: vested, what vests next and when, the exercise price, the vesting and what is
  exercised. Nobody else's, and no cash value; to exercise, they ask the finance team.

(src: docs/user-guide/equity-register.md#my-shares-and-options)

## Correct a transaction

- An approved transaction never changes: **Reverse it** drafts a reversal citing why (the ACRA
  extract when ACRA is right); once effective it undoes the shares and entries whole, and a holder
  must still hold what it takes back. A buy-back is a new transaction, never a reversal.

(src: docs/user-guide/equity-register.md#correct-a-transaction)

## The register of share issues

- Below the transactions (also on Director accounts): every allotment with its holder, class,
  number, price, what paid for it, the resolutions and ACRA's reference; share capital changes only
  with it or the opening balances.
- **Record shares issued before the books start** (Owner or Controller), such as the
  incorporation subscription: no entry posts, the opening balances hold their share capital. Cash
  issues and conversions of a director's balance add theirs when they take effect.

(src: docs/user-guide/equity-register.md#the-register-of-share-issues)

## Common problems

- EQUITY-03: the contact is not marked shareholder or investor; an individual's identifier longer
  than its last 4 characters; an NRIC or FIN in full in the address or note; an entity without its
  registration number.
- EQUITY-04: the class's account is not 3010 or 3020 share capital; a cap on a non-participating
  preference.
- EQUITY-05: recorded, so it changes no more; draft a new one. EQUITY-06: no evidence attached;
  attach the identity document, the constitution and the shareholders' agreement, the resolution
  or the grant letter.
- Your own holder record is approved by another Owner or the Controller.
- EQUITY-07: a conversion of the instrument stands already (change it, or reverse it first); the
  round is not an approved issue for cash. LEDGER-49: a line on 2700 names its instrument (choose
  it under SAFE or note). EQUITY-15: a template is enabled already, or is not one Fyniq knows.
- EQUITY-08: only an approved transaction becomes effective, never before its approval; the
  subscriber's money is not all in share application monies; the holder holds fewer shares on the
  day; a conversion's round is not effective on or before the day, or was reversed (draft the
  conversions again from the round that replaces it); 2700 holds less for the instrument than it
  converts (categorise the SAFE's money to 2700, naming the instrument). EQUITY-09: no share application monies account, add 2040. EQUITY-10: a step its transaction
  does not take, or dated after today.
- A transaction that moves your own shares is approved by another Owner or the Controller.
- EQUITY-16: the pool's resolution does not fit (whole shares above zero, the share-capital class,
  the resolution and its day), or sets aside fewer shares than the options granted by a day.
  EQUITY-17: no recorded resolution sets a pool on the grant date (record it first); the pool
  leaves fewer options than the grant on its grant date or a later day. Your own grant of options
  is approved by another Owner or the Controller. EQUITY-18: a forfeiture or expiry of a grant not
  recorded, of options vested, or of more than the grant has left; your own options' forfeiture or
  expiry is approved by another Owner or the Controller. EQUITY-07 on an exercise: more than is
  vested and not exercised, or a grant at an exercise price of 0.
- REPORT-08: the render service did not make the PDF; try again, or export to Excel.

(src: docs/user-guide/equity-register.md#common-problems)
