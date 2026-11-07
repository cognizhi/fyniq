---
key: equity.register
type: task
title: The equity register: holders, share classes and the equity transactions that move every share
summary: Holders and their details (last 4 characters only), share classes, and equity transactions: drafted, approved, made effective with ACRA
sources:
  - docs/user-guide/equity-register.md sha256:a06193665f7a05d05dfa599bbc28c290f0460b256e4ed3c29b58434d0663bee2
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
aliases:
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
---

# The equity register: holders, their recorded details and share classes

## What it is

- **Equity** is the company's register of who owns it: shareholders and investors, share classes
  with their rights, and the equity transactions that move every share. It is an Expert feature.
- Tabs move between **Holders**, **Classes** and **Transactions** (with the register of share
  issues); the cap table, SAFEs and notes, the option pool and rounds join them later.
- Holders and classes post no entry; share capital changes only through the register of share
  issues, the opening balances and a conversion between classes on different accounts
  ([[founders.director-accounts]]).

(src: docs/user-guide/equity-register.md#what-it-is)

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

- **Equity › Transactions** lists issues for cash, transfers, conversions between classes and
  reversals, the latest first: reference (EQ-, on approval), effective date, holders, class, shares
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
  attach the identity document, or the constitution and the shareholders' agreement.
- Your own holder record is approved by another Owner or the Controller.
- EQUITY-08: only an approved transaction becomes effective, never before its approval; the
  subscriber's money is not all in share application monies; the holder holds fewer shares on the
  day. EQUITY-09: no share application monies account, add 2040. EQUITY-10: a step its transaction
  does not take, or dated after today.
- A transaction that moves your own shares is approved by another Owner or the Controller.

(src: docs/user-guide/equity-register.md#common-problems)
