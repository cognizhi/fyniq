---
key: equity.register
type: task
title: The equity register: holders, their recorded details and share classes
summary: Shareholders and investors, recording their details (last 4 characters only) on approval, and share classes with their rights
sources:
  - docs/user-guide/equity-register.md sha256:c2a8c0d48ebea17e453dfa29315323ec4547f688c35db4bbe93d79b9fb9f9ef2
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
---

# The equity register: holders, their recorded details and share classes

## What it is

- **Equity** is the company's register of who owns it: shareholders and investors, share classes
  with their rights, and the register of share issues. It is an Expert feature.
- Tabs move between **Holders**, **Classes** and **Transactions** (the register of share issues);
  the cap table, SAFEs and notes, the option pool and rounds join them later.
- Nothing here posts an entry; share capital changes only through the register of share issues and
  the opening balances ([[founders.director-accounts]]).

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

## The register of share issues

- **Equity › Transactions** (also **Register of share issues** on Director accounts): every
  allotment with its holder, class, number, price, what paid for it, the resolutions and ACRA's
  reference; share capital changes only with it or the opening balances.
- **Record shares issued before the books start** (Owner or Controller), such as the
  incorporation subscription: no entry posts, the opening balances hold their share capital.
  Conversions of a director's balance add theirs when ACRA registers them
  ([[founders.director-accounts]]); transfers and issues for cash come next.

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

(src: docs/user-guide/equity-register.md#common-problems)
