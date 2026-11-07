---
key: equity.register
title: Equity register
mode: expert
roles: [owner, controller, accountant, auditor, tax_agent, dpo]
requirements: [FR-EQ-15, FR-EQ-16, FR-EQ-17, FR-EQ-08, FR-EQ-09, FR-EQ-27, CMP-PD-07]
---

# Equity register

## What it is

**Equity** keeps the company's register of who owns it: the shareholders and investors, the
share classes with their rights, and the equity transactions that move every share. It is an
Expert feature. The tabs at the top of each page move between **Holders**, **Classes** and
**Transactions** (with the register of share issues). The cap table, SAFEs and convertible notes,
the option pool and rounds join these tabs as they arrive.

Holders and classes post no entry. Share capital changes only through the register of share
issues, the opening balances, and a conversion between classes on different accounts.

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
| It has no evidence attached (EQUITY-06) | Attach the identity document, or the constitution and the shareholders' agreement, then ask again |
| This is your own holder record, so another person approves it | Ask another Owner or the Controller |
| Only an approved transaction becomes effective, or the effective date is before the approval (EQUITY-08) | Ask for its approval, then record the day ACRA's register shows it |
| The subscriber's money is not all in share application monies (EQUITY-08) | Categorise the bank line of their payment to 2040, naming them, then record the effective date |
| The holder holds fewer of these shares on the day (EQUITY-08) | Check the register: a holder moves only shares they hold |
| The chart has no share application monies account (EQUITY-09) | Add 2040 Share application monies to the chart of accounts |
| A step its transaction does not take, or dated after today (EQUITY-10) | Choose one of the steps shown, on the day it happened |
| The transaction moves your own shares, so another person approves it | Ask another Owner or the Controller |

## Related

Director accounts (the register of share issues) · Contacts · Approvals · Evidence search
