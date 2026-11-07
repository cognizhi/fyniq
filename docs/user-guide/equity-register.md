---
key: equity.register
title: Equity register
mode: basic
roles: [owner, controller, accountant, auditor, tax_agent, dpo, board_viewer, company_secretary, employee]
requirements: [FR-EQ-15, FR-EQ-16, FR-EQ-17, FR-EQ-18, FR-EQ-19, FR-EQ-20, FR-EQ-21, FR-EQ-22, FR-EQ-25, FR-EQ-26, FR-EQ-08, FR-EQ-09, FR-EQ-27, CMP-PD-07]
---

# Equity register

## What it is

**Equity** keeps the company's register of who owns it: the shareholders and investors, the
share classes with their rights, and the equity transactions that move every share. Its first
page, **Who owns the company**, is the cap table, in Basic and Expert mode. The tabs at the top of
each page move between **Cap table**, **Holders**, **Classes** and **Transactions** (with the
register of share issues) and **SAFEs and notes**, which are Expert pages. The option pool and rounds
join these tabs as they arrive.

Holders and classes post no entry. Share capital changes only through the register of share
issues, the opening balances, and a conversion between classes on different accounts.

## The cap table

**Equity › Cap table**, **Who owns the company**, shows who owns what on a day. It is computed
from the equity register: from the shares issued before the books start, and from each equity
transaction on the day ACRA's register of members shows it. No one types a figure into it, and
it cannot be edited. To change a holding, draft an equity transaction.

- **As at** picks the day; left empty, it is today.
- **Ownership** shows each holder's part of every share issued, as a bar with a legend, or as a
  table with **Table**. Each percentage is computed exactly and shown to 2 decimals, so they do
  not always add up to 100.00.
- **My holding** shows the shares of the shareholder you sign in as, and their percentage. A
  director or shareholder is linked to the person who signs in as them on **Directors' accounts**.
- **Latest round** shows the most recent issue of shares for cash.
- A note lists the transactions that are approved and wait for ACRA. Their shares count from
  the day ACRA registers them, not before.

In Basic mode the table lists each shareholder, their type of share, their shares and what they
own. In Expert mode the table **By holder and class** shows each holder's shares in each class:
issued, the percentage of every share issued, the percentage of every vote, and fully diluted.
Fully diluted counts each holder's shares and the options granted to them, and the total adds what
the option pool leaves to grant, on a row of its own, **Unallocated option pool**; a holder with
options and no shares has a line with their options only. SAFEs and notes count once they
convert. With no options and no pool, fully diluted is the same as issued. **Votes
assumed** on a line means its class is not recorded yet, so its shares count one vote each until
you record the class and its rights.

A Board viewer sees only their own holding on this page, never the other holders.

## Compare two days

In Expert mode, **Compare with** takes a second day. **Since** then shows how many shares were
issued on each day, and each holder's percentage then and now. A holder who held nothing then
shows 0.00%.

## See where a figure comes from

In Expert mode, select a holding's shares in **By holder and class**. A window lists the
movements they come from, oldest first: each equity transaction, which you open to see its
evidence, or the register of share issues for shares recorded before the books start.

## Export the cap table

**Excel** and **PDF** download the cap table at the day shown. The workbook has a sheet of every
holding and a sheet of every holder, each with a total; the PDF is the cap table to print. Each
export is recorded in the audit trail with its day, its format and its hash.

## Holders

**Equity › Holders** lists every shareholder and investor, with the identifier from their record,
the shares they hold by class, and whether their details are recorded. **All**, **Shareholders**
and **Investors** filter the list, and the search box finds a holder by name.

A holder is a contact. To add one, open **Contacts**, open or add the contact and tick **A
shareholder**, or **An investor** for someone who holds a SAFE, a note or options and no shares
yet. **Add a holder** on the Holders page takes you there.

## Record a holder's details

Open a holder from the list. **Details in force** shows what is recorded: individual or entity,
the identifier, nationality or place of incorporation, the registered address and the people to
contact.

1. Select **Draft the details** (or **Change the details** once a record is in force). An
   Accountant drafts them.
2. For an individual, choose NRIC, FIN or passport and enter **only the last 4 characters**,
   such as 567D. Fyniq never keeps the full number: it stays in ACRA's register and the identity
   document. For an entity, choose UEN or foreign registration and enter the number.
3. Select **Draft the record**, then attach the identity document to the draft.
4. Select **Ask for approval**. Another Owner or the Controller approves it, never the person
   who asked, and never the holder themselves. Once approved, the record is in force and the one
   before it moves to **Earlier records**.

A draft can be changed until approval is asked for. A recorded record changes no more: draft a
new one instead.

Who sees what: the Owner, the Controller, the Accountant, the Auditor and the Tax agent read the
holders. The data protection officer reads them masked and opens no file. Identity documents are
never opened in the app; they stay in the evidence store for the audit.

## Record an investor's rights

On a holder's page, **Rights from the agreements** shows what the agreements give them: the
directors they may appoint, a board observer seat, whether they appoint or remove a majority of
the board, their information rights (what and when), a pro-rata right in later rounds, and the
matters that need their consent. Fyniq records these rights; it computes nothing from them.

- An Accountant selects **Record rights**, names the agreements the rights come from and the day
  they apply from, attaches the agreements (the shareholders' agreement, a side letter or the
  subscription agreement) and selects **Ask for approval**. Another Owner or the Controller records
  them, never the holder. Rights recorded later supersede them.
- A board majority, or any consent matter, **could confer control**: the holder is then named in the
  controller test for the company secretary to weigh.

## Registrable controllers and the exempt status

A holder with **more than 25%** of the shares or of the votes is a **registrable controller**. The
company enters them in its register of registrable controllers within 7 days, and updates ACRA's
central register within 2 business days after that [verify against ACRA's guidance in force]. It
sends each controller a notice at least once a year; the reply is due within 30 days, and the
register is updated within 7 days of it.

- On a holder's page, **Registrable controller** shows what the cap table's test finds, where the
  registers stand and what is due: **Enter in the register by**, **Update ACRA's register by**, or
  **In both registers**. The 2 business days leave out weekends only, so the day shown is never
  later than the law's [verify a public-holiday calendar].
- Rights that could confer control cannot be tested from the cap table. Until the company
  secretary's conclusion is recorded, the holder is **Not assessed**, and the check is never all
  clear.
- An Accountant selects **Record the conclusion**: a registrable controller, on their grounds and
  from the day they became one, with the days of both registers and the yearly notices; or not a
  controller, with the reason. They attach the company secretary's confirmation and ask for
  approval; another Owner or the Controller records it, never the holder. To add a register day, a
  notice or the day they ceased, they select **Update the entry**.
- The checks show what is due: a controller to enter or to lodge, a yearly notice due, a notice
  unanswered after 30 days, or a controller who no longer holds more than 25%.

Each change to the register also tests the **exempt-private-company status** again: more than 20
members, or a member that is a company or another entity, ends it [accountant to confirm the Act's
exceptions]. The test reads each member's holder record, which says individual or entity. When the
register says otherwise than the status recorded, the checks warn: record the new status from the
ACRA business profile, and re-check the directors' accounts in debit.

## Share classes

**Equity › Classes** lists each share class with its rights, as the constitution gives them.
Fyniq records the rights; it does not compute with them yet.

1. Select **Draft a class** and name it as the constitution does, such as Ordinary shares.
2. Choose ordinary or preference (with its series, such as A) and its share capital account:
   3010 Ordinary or 3020 Preference share capital.
3. Enter the rights: votes per share, the liquidation preference (its multiple, whether it is
   participating, any cap, and its seniority), dividends (none, non-cumulative or cumulative),
   whether it converts into ordinary shares, anti-dilution (none, broad-based weighted average or
   full ratchet), the pro-rata right in later rounds, and any redemption terms.
4. Attach the constitution and the shareholders' agreement, then select **Ask for approval**.
   Another Owner or the Controller approves it.

To change a recorded class's rights, draft new rights for the class. A class keeps its account.

Until an account has a recorded class, its share issues count under the account itself. Once it
has one, every new issue on that account names a class, and an issue that names none takes the
account's first recorded class.

## Equity transactions

**Equity › Transactions** lists every equity transaction, the latest first: issues for cash,
transfers between holders, conversions between classes and reversals. Each shows its reference
(EQ-, given on approval), its effective date, its holders, class and shares, and where it stands:
**Draft**, **Waiting for ACRA** (approved), **Effective** or **Reversed**. A **Due** chip means an
ACRA lodgement is due; **Review** means it has waited too long. The tiles show the shares held and
what waits for ACRA.

## Draft a transaction

1. Select **New transaction**. An Accountant drafts it.
2. Choose the type and fill it in:
   - **Issue for cash**: the subscriber (a shareholder or investor), the class, the shares, the
     price per share and the resolutions. One subscriber per issue: a round with several investors
     is several issues.
   - **Transfer**: who the shares come from and go to, the class and the shares. It makes no entry
     in the company's books; share duty on the instrument of transfer is the parties' matter.
   - **Conversion between classes**: the holder, the class the shares leave and the class they
     join, the shares out and in, and, when the classes are on different accounts, the carrying
     amount that moves between 3010 and 3020.
   - **Reversal**: the effective transaction it undoes and why.
3. Select **Draft the transaction**, attach what it rests on (the resolutions and subscription
   agreement, the instrument of transfer, or the ACRA register extract for a reversal), then
   **Ask for approval**. Another Owner or the Controller approves it, never a holder it moves shares
   of or for. When every other approver has an equity transaction of their own waiting or approved
   in the last 30 days, the Accountant reviews it too.

## Make it effective

An approved transaction waits for ACRA. Record its **ACRA steps** as they happen:

- an issue: the shareholders' approval, lodged with ACRA within 14 days; the date of the next AGM,
  when that approval lapses; and the return of allotment;
- a transfer: the instrument of transfer signed, and the notice of transfer lodged;
- a conversion: the resolution, and its lodgement.

A **Due** chip shows while a lodgement is due. When ACRA's register of members shows the
transaction, an Owner or the Controller selects **Record the effective date**: never after today
or before the approval, in an open period. Its shares move then, and only then.

A cash issue takes effect only once its subscriber's money is in **2040 Share application
monies**: categorise the bank line of their payment to 2040, naming them. It then posts Dr 2040 ·
Cr share capital. If an issue does not go ahead, pay the refund from your bank and categorise that
bank line to 2040 the same way.

## SAFEs and convertible notes

**Equity › SAFEs and notes** lists every SAFE and convertible note: its holder, form, amount, the
day it was signed, its terms (the valuation cap, a discount, a note's interest and maturity), the
accountant's classification, what **2700 Convertible notes / SAFE** holds for it today, and where
it stands: **Draft**, **Waiting for approval**, **Recorded**, **Converting** (its conversion is
drafted or approved) or **Converted** (its shares were allotted). The tiles count the outstanding
ones, the drafts and those converting or converted.

## Record a SAFE or note

1. Select **Add instrument**. An Accountant drafts it.
2. Choose the holder, an investor or shareholder (mark the contact first in Contacts), and the
   form: a post-money SAFE, a pre-money SAFE, a discount-only SAFE, an MFN SAFE or a convertible
   note. Enter the amount paid and the day it was signed, then the terms its form has: the
   valuation cap of a post-money or pre-money SAFE, a discount, and a note's interest and maturity.
3. Choose what converts it, the share class it converts into, how its terms count the shares its
   price divides by, and the decimals its terms give the conversion price. The accountant records
   its classification, liability or equity, from its terms.
4. Select **Draft the instrument**, attach the signed SAFE or note, then **Ask for approval**:
   another Owner or the Controller records it, never the holder.

Its money comes in on a bank line: in Banking, categorise the line to **2700 Convertible notes /
SAFE** and choose the recorded SAFE or note under **SAFE or note**. A repayment out is categorised
the same way. A line to 2700 that names no recorded instrument is refused. A founder's balance can
become its money too: settle the balance on Director accounts and choose **Convert to a SAFE or
note**.

**Checks** warns when a recorded note is past its maturity, or when shares were allotted for cash
after a SAFE was signed and it has not converted: draft its conversion from the round, or settle
the note.

## The forms Fyniq converts by itself

**Forms Fyniq converts** lists the templates Fyniq knows. A conversion that an enabled template
covers in every term is ready for approval; any other form, and a SAFE with a discount, a
most-favoured-nation clause or interest, converts by a draft the accountant completes (⚠). To
enable a template, an Accountant selects **Enable it**, attaches your lawyer's confirmation that its
conversion terms are right for the company, and asks for approval; another Owner or the Controller
approves it, once.

## Preview a conversion

**Conversion preview** works out what each recorded SAFE and note would convert into, as a
sandbox: nothing posts and nothing is saved. Choose an approved issue for cash under **At the
round**, or type a round price per share, then select **Preview**. For each instrument it shows
the working: its ownership at the cap, its cap price, its discounted round price, the conversion
price cut down to its decimals, the whole shares, and what stays on 2700. At an approved round, an
Accountant can then select **Draft the SAFE conversions** there.

## Convert SAFEs and notes

At a priced round, each recorded SAFE or convertible note converts into shares. On the round's
issue for cash, once it is approved, an Accountant selects **Draft the SAFE conversions**. Fyniq
drafts one **SAFE or note conversion** for each instrument the round converts, with its shares and
its conversion price:

- the price is the lower of its cap price and the round's price less its discount, cut down to
  the decimals its terms give. A post-money SAFE's cap price counts the shares every converting
  post-money SAFE takes; a pre-money SAFE's is its cap divided by the shares before the round;
- the shares are its money divided by that price, rounded down to whole shares. What is left of
  its money stays on **2700 Convertible notes / SAFE** until it is settled under its terms.

A conversion that an enabled template covers in every term is ready for approval. Any other shows
**The accountant completes it** with what to check, such as a discount, an MFN clause or a round
below its cap price: check the shares and the price against the terms before asking. The holder
and the class are the instrument's. Attach the resolutions and ask for approval like any other
transaction; an instrument converts once while its conversion stands. It becomes effective on or
after its round, while 2700 holds its money, and then posts Dr 2700 · Cr share capital.

## The option pool and grants

The **option pool** is the shares a resolution sets aside for options, such as an employee share
option plan. Its size on a day is the latest recorded resolution passed on or before that day; a
later resolution sets it again from the day it was passed. **Unallocated** is what the pool leaves
to grant: its size less the options granted.

A **grant** gives a person options from the pool: how many, the exercise price per share, the
grant date, the vesting (a cliff, then evenly by month), its acceleration on a change of control
and the day it lapses. Its holder is a contact marked An investor or A shareholder, of the pool's
class. An Accountant drafts each resolution and grant and attaches its evidence (the signed
resolution; the grant letter), then asks for approval; another Owner or the Controller records it,
never the grant's own holder.

- A grant fits only what the pool leaves on its grant date, and on every later day a grant was
  made or a resolution passed: an earlier-dated grant cannot take options a later one already has.
- A resolution that sets aside fewer shares than the options granted by then is refused.
- **Vested** is worked out each day, never stored: none before the cliff, then the options × the
  whole months since the grant ÷ the vesting months, rounded down to whole options; all of them
  once the vesting months have passed. A month passes on the grant's day of the month, or on a
  shorter month's last day [accountant to confirm].
- A person whose contact is linked to their sign-in sees their own grants, and nobody else's.

When a holder leaves, their options not vested are **forfeited**; those vested and not exercised
**expire** when their window to exercise closes, as they do at the grant's expiry date. On the
grant, an Accountant drafts a **Forfeiture** or an **Expiry** with its number of options and its
day, attaches the notice (the leaver's letter, the board's minute or the notice of lapse), and asks
for approval; another Owner or the Controller records it, never the grant's holder.

- A forfeiture takes at most the options not vested on its day; an expiry, at most what the grant
  has left. Neither is dated before the grant.
- From its day, the options leave the holder's fully diluted shares and are back in the pool to
  grant again [accountant to confirm against the plan's rules].
- **Outstanding** is the grant's options less those forfeited, expired and exercised; **Vested**
  never counts more than the grant keeps, so a leaver keeps only what vested before they left.

To **exercise** options, an Accountant drafts an equity transaction of type **Exercise of
options**: they choose the grant, the number of options and the board resolution allotting the
shares. The holder, the share class and the price per share are the grant's; it takes at most
what is vested and not exercised (**Exercisable** on the grant). It is approved like any other
issue, never by the holder, and made effective once the holder's money is in share application
monies: it then posts Dr 2040 Share application monies · Cr share capital with shares × the
exercise price, and the holder holds the shares. Exercised options never return to the pool; a
reversal of the exercise makes them outstanding again. A grant at an exercise price of 0 is not
exercised this way yet: ask your accountant [accountant to confirm].

## Record the pool and a grant

**Equity › Option pool** shows the pool: its size by resolution, the options granted, those
forfeited or expired back in the pool, those exercised and now shares, what is outstanding and
vested, and what is **available** to grant. Below it are every grant with its vesting, expiry and
status, what vests or expires in the next 30 days, and the resolutions that set the pool.

- To set or enlarge the pool, an Accountant selects **Record a resolution**: the shares set aside,
  the class and the day it was passed. On its page they attach the signed resolution and select
  **Ask for approval**.
- To grant options, they select **New grant**: the holder, the options, the exercise price, the
  grant date, the vesting and the expiry. On the grant's page they attach the grant letter and ask
  for approval.
- On a recorded grant, **Forfeit or let lapse** drafts a forfeiture or expiry; attach its notice
  and ask for approval there.

## My shares and options

Under **My shares and options**, a person whose sign-in is linked to their contact sees their own
grants: the options vested, what vests next and when, the exercise price, the vesting and what is
exercised. Nobody else's grants, and no cash value: an option's worth depends on a future sale or
funding round. To exercise vested options, they ask the company's finance team.

## Model a round

**Equity › Round simulator** is a sandbox. It tries a priced funding round against the cap table
as it stands at the end of the day before the round, and shows what everyone would own before and
after it, fully diluted. Nothing is posted or recorded. Anyone who may read the whole cap table,
such as an Owner, the Controller or the Accountant, can try rounds; an Employee and the Board viewer
cannot.

- In Basic mode, enter **how much you raise** and **from** which investor (a contact marked An
  investor), the **value of the company before the money** (the pre-money valuation) and any **more
  options for employees**. The page shows who would own what as two bars, now and after the round,
  and the part **you would own** when your sign-in is linked to your contact.
- In Expert mode, set the price from a **pre-money valuation** or a **price per share**. A price
  from a valuation is the valuation divided by the shares fully diluted before the round, with a
  top-up before the money, cut down to the decimals you choose [accountant to confirm what the
  terms count]. A pool **top-up** is before the money, so the existing holders bear it, or after,
  so the new investors share it.
- Add each investor's money. An existing holder taking up their **pro-rata right** is an investor
  too, and the page shows what their right entitles them to of the new shares [accountant to
  confirm against the side letter]. Turn on each recorded SAFE or note that converts at the round:
  it converts at its own terms over the shares issued before the round.
- Each investor gets the whole shares their money buys at the round's price (RND-SHARE); what buys
  no whole share is not taken. The **pro-forma cap table** shows each holder's shares and part fully
  diluted, now and after, the **points** each loses or gains, the new money, and the option pool not
  granted. **Side by side** compares the saved scenarios, each worked out again against today's
  books whenever the page opens.

An Accountant selects **Save scenario** to keep a scenario. To make it real, they name the
resolutions its share issues will name and select **Send for approval** (Expert); a scenario with a
top-up is sent on or after its round's day. Once another Owner or the Controller approves it, Fyniq
drafts an issue for cash for each investor at the round's price and, for a top-up, a resolution
enlarging the pool; each then follows its own approval and ACRA steps, and the SAFEs and notes are
converted from the round's issue once it is approved. An approved scenario changes no more: to try
other numbers, start a new one.

## Reconcile the register to ACRA

**Equity › Reconciliation** compares the register of members with ACRA's, as the latest business
profile shows it, and paid-up capital with share capital in the ledger.

- Buy the business profile from ACRA's Bizfile. An Accountant selects **Type in a business
  profile**, enters the day printed on it and each holder's shares of each class as it lists them,
  saves the extract and attaches the profile's PDF. A new extract starts with a row for each
  holding the register has, the shares left empty to type from the PDF.
- They select **Record**: from then on the extract changes no more, and the register is compared
  with it at its day, holder by holder and class by class. A newer business profile, recorded
  later, takes its place.
- Each difference shows on the page and on the checks, with the register's shares and ACRA's. Where
  ACRA is right, correct the register by a reversal citing the extract; where the typing is wrong,
  type the profile in again.
- While the register holds shares and no business profile is recorded, the check is not assessed.

## Correct a transaction

An approved transaction never changes. To correct an effective one, select **Reverse it** and
draft a reversal that says why, with the ACRA extract when ACRA's register is right and Fyniq was
wrong. Once approved and effective, it undoes the transaction's shares and entries whole; a holder
must still hold the shares it takes back. Changing the legal position, such as a buy-back, is a new
transaction with its own resolutions, never a reversal.

## The register of share issues

Below the transactions, the **Register of share issues** (also on Director accounts) lists every
allotment of shares: who holds them, the class, the number, the price, what paid for them, the
resolutions and ACRA's reference. Share capital changes only with this register or the opening
balances.

Record the shares issued before the books start, such as the incorporation subscription, with
**Record shares issued before the books start**. No entry posts, because the opening balances
hold their share capital. An Owner or the Controller records them. Cash issues and conversions of
a director's balance add their shares when they take effect.

## Common problems

| Message | Fix |
|---|---|
| The contact is not marked shareholder or investor (EQUITY-03) | Tick A shareholder or An investor on the contact in Contacts |
| Fyniq keeps only the last 4 letters or digits of an individual's identifier (EQUITY-03) | Enter the last 4 characters only, such as 567D |
| The record shows an NRIC or FIN in full (EQUITY-03) | Remove it from the address or the note; keep only the last 4 characters |
| An entity is identified by its registration number (EQUITY-03) | Enter its UEN or foreign registration number instead |
| The class's account is not an active share-capital account (EQUITY-04) | Choose 3010 Ordinary or 3020 Preference share capital |
| Only a participating preference has a cap (EQUITY-04) | Mark it participating, or leave the cap empty |
| It is recorded, so it changes no more (EQUITY-05) | Draft a new one; once recorded, it supersedes this one |
| It has no evidence attached (EQUITY-06) | Attach the identity document, the constitution and the shareholders' agreement, the resolution or the grant letter, then ask again |
| This is your own holder record, so another person approves it | Ask another Owner or the Controller |
| Only an approved transaction becomes effective, or the effective date is before the approval (EQUITY-08) | Ask for its approval, then record the day ACRA's register shows it |
| The subscriber's money is not all in share application monies (EQUITY-08) | Categorise the bank line of their payment to 2040, naming them, then record the effective date |
| The holder holds fewer of these shares on the day (EQUITY-08) | Check the register: a holder moves only shares they hold |
| A conversion of the instrument stands already (EQUITY-07) | Change that conversion, or reverse it first |
| Account 2700 holds SAFEs and convertible notes, so the line names its instrument (LEDGER-49) | Choose the recorded SAFE or note under SAFE or note |
| The instrument is missing its signed copy (EQUITY-06) | Attach the signed SAFE or note, then ask for approval |
| The template is enabled already, or is not one Fyniq knows (EQUITY-15) | A template is enabled once; Fyniq knows YC's Singapore post-money SAFE |
| The round is not an approved issue for cash (EQUITY-07) | Draft the conversions from the round's issue once it is approved |
| Its round is not effective on or before the day (EQUITY-08) | Make the round effective first: the SAFEs convert with it, on its day or after |
| Its round was reversed (EQUITY-08) | Draft the conversions again from the round that replaces it |
| 2700 holds less for the instrument than it converts (EQUITY-08) | Categorise the bank line of the SAFE's money to 2700, naming the instrument |
| The chart has no share application monies account (EQUITY-09) | Add 2040 Share application monies to the chart of accounts |
| A step its transaction does not take, or dated after today (EQUITY-10) | Choose one of the steps shown, on the day it happened |
| The transaction moves your own shares, so another person approves it | Ask another Owner or the Controller |
| The pool's resolution does not fit (EQUITY-16) | Enter a whole number of shares above zero, the share-capital class, the resolution and the day it was passed |
| The options granted by a day are more than the shares the resolution sets aside (EQUITY-16) | Set aside at least the options granted, or date it after them |
| No recorded resolution sets an option pool on the grant date (EQUITY-17) | Record the pool's resolution first |
| The pool leaves fewer options than the grant, on its grant date or a later day (EQUITY-17) | Grant at most what the pool leaves, or record a resolution that enlarges it first |
| This is your own grant of options, so another person approves it | Ask another Owner or the Controller |
| The grant is not recorded (EQUITY-18) | Record the grant first: only a recorded grant's options are forfeited or expire |
| The options not vested are fewer than the forfeiture (EQUITY-18) | Forfeit only the options not vested; record those that lapse later as an expiry |
| The grant's other events leave fewer options (EQUITY-18) | Take at most what the grant has left |
| These are your own options, so another person approves their forfeiture or expiry | Ask another Owner or the Controller |
| The options vested and not exercised are fewer than the exercise (EQUITY-07) | Exercise at most what the grant shows as exercisable, or make it effective once more have vested |
| The grant's exercise price is 0 (EQUITY-07) | Ask your accountant how to record shares issued for no cash; Fyniq records an exercise for cash only |
| The render service did not make the PDF (REPORT-08) | Try again in a moment, or export to Excel |

## Related

Director accounts (the register of share issues) · Contacts · Approvals · Evidence search
