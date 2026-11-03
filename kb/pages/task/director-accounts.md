---
key: founders.director-accounts
type: task
title: Director accounts, each founder's statement, and naming the person on 2500 and 2120
summary: 2500 and 2120 by person; marking directors, linking their member, naming the person on journal lines; statements and their PDF
sources:
  - docs/user-guide/director-accounts.md sha256:ab8ff2f359ac9a50a890ea6171e1893cbab0f50a6955627d8ff34fd849976750
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-03
aliases:
  - director accounts
  - director's account
  - loan from director
  - deferred remuneration
  - whose account
  - link member
  - balance confirmation
  - account in debit
---

# Director accounts, each founder's statement, and naming the person on 2500 and 2120

## What it shows

- Accounting › **Director accounts**: what the company owes each director and shareholder, and
  what they owe it. (src: docs/user-guide/director-accounts.md#what-it-is)
- **2500 Loan from director / shareholder** holds money lent and costs paid personally and kept as
  a loan; **2120 Deferred remuneration payable** holds pay and directors' fees owed but not yet paid.
  (src: docs/user-guide/director-accounts.md#what-it-is)
- Every amount names its person and where it came from: lent, paid personally, pay deferred,
  director's fee, paid out to them, or other. A loan and unpaid pay are taxed differently.
  (src: docs/user-guide/director-accounts.md#what-it-is)
- **Account in debit**: the person owes the company; a loan to a director is restricted unless the
  company is an exempt private company, so the accountant is asked.
  (src: docs/user-guide/director-accounts.md#what-it-is)
- A founder's own view: [[founders.balance]]; settling a balance: [[founders.settle-balance]].

## Mark the directors and shareholders

- In Contacts, tick **A director**, **A shareholder** or both; a contact can also be a customer or a
  vendor. (src: docs/user-guide/director-accounts.md#mark-the-founders-contacts)
- Once an amount on 2500 or 2120 names a contact, it stays a director or shareholder.
  (src: docs/user-guide/director-accounts.md#mark-the-founders-contacts)

## Link the member who signs in as them

- **Link member** on the person's card asks for approval; another person who holds the approval
  right approves it, never the member being linked.
  (src: docs/user-guide/director-accounts.md#link-each-director-to-the-member-who-signs-in-as-them)
- The linked member sees their own account in **Money I put in** and never approves their own
  settlement; a member signs in as one director or shareholder at most; **Nobody** unlinks them.
  (src: docs/user-guide/director-accounts.md#link-each-director-to-the-member-who-signs-in-as-them)

## Name the person on a line

- On a journal or opening balance line on 2500 or 2120, choose **Whose account** and **Where it
  came from**; a balance owed to two founders is two lines.
  (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)
- A journal that leaves the person out does not post, nor one that names a person on another
  account. (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)
- A bank line categorised to 2500 takes the founder as its contact: money in is lent, money out is
  paid out to them. (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)

## Statements and balance confirmations

- **Statement** on a card: for each account, the opening balance, the amounts by where they came
  from, and the closing balance, owed to the person shown positive; **Every movement** lists each
  line. (src: docs/user-guide/director-accounts.md#read-a-statement)
- **PDF for confirmation** downloads it to send to the person; **CSV** gives its movements; each
  download is in the audit trail. (src: docs/user-guide/director-accounts.md#send-it-for-a-balance-confirmation)

## Messages and fixes

| Message | Fix |
|---|---|
| Account 2500 is a director's account, so the line names whose it is | Choose the person and where the amount came from |
| The person is not a director or shareholder of this organisation | Mark the contact as one, or choose another |
| Account is not a director's account, so the line names no person | Leave the person out, or post to the director's account |
| Amounts on 2500 and 2120 name no director or shareholder | Reverse those journals and post them again with each person |
| You are the member being linked, so another person approves it | Ask another Owner or the Controller |

(src: docs/user-guide/director-accounts.md#common-problems)
