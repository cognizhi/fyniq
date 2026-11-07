---
key: equity.register
title: Equity register
mode: expert
roles: [owner, controller, accountant, auditor, tax_agent, dpo]
requirements: [FR-EQ-15, FR-EQ-16, FR-EQ-27, CMP-PD-07]
---

# Equity register

## What it is

**Equity** keeps the company's register of who owns it: the shareholders and investors, the
share classes with their rights, and the register of share issues. It is an Expert feature. The
tabs at the top of each page move between **Holders**, **Classes** and **Transactions** (the
register of share issues). The cap table, SAFEs and convertible notes, the option pool and
rounds join these tabs as they arrive.

Nothing on these pages posts an entry. Share capital changes only through the register of share
issues and the opening balances, as described in Director accounts.

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

## The register of share issues

**Equity › Transactions** (also **Register of share issues** on Director accounts) lists every
allotment of shares: who holds them, the class, the number, the price, what paid for them, the
resolutions and ACRA's reference. Share capital changes only with this register or the opening
balances.

Record the shares issued before the books start, such as the incorporation subscription, with
**Record shares issued before the books start**. No entry posts, because the opening balances
hold their share capital. An Owner or the Controller records them. Conversions of a director's
balance add their shares when ACRA registers them. Transfers and issues for cash come next.

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

## Related

Director accounts (the register of share issues) · Contacts · Approvals · Evidence search
