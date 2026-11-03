---
key: founders.director-accounts
type: task
title: Director accounts, each founder's statement, and naming the person on 2500 and 2120
summary: 2500 and 2120 by person; marking directors, linking their member, naming the person on journal lines; statements and their PDF
sources:
  - docs/user-guide/director-accounts.md sha256:1783e426a7f77a15656ab35858cb8818614e8daa636194d39a014dada003a7c9
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
  - exempt private company
  - section 162
  - settle a balance
  - convert to shares
  - share issue
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
- **Account in debit**: the person owes the company; what it means depends on the company's
  exempt-private-company status. (src: docs/user-guide/director-accounts.md#what-it-is)
- A founder's own view: [[founders.balance]]; settling a balance: [[founders.settle-balance]]; costs
  paid before the books start: [[founders.pre-incorporation-costs]].

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

## Exempt private company status

- The top of the page says the status in force; **Record exempt status** records it from a day on
  with the ACRA business profile, by an Owner or the Controller, and a new one supersedes it from
  its day, for example when a corporate investor joins.
  (src: docs/user-guide/director-accounts.md#record-the-exempt-private-company-status)
- An account in debit: when exempt, the loan is allowed and disclosed; when not exempt or nothing
  is recorded, section 162 of the Companies Act bans loans to directors, but advances for company
  expenses are allowed when approved in general meeting or repaid within 6 months after the next
  AGM. (src: docs/user-guide/director-accounts.md#record-the-exempt-private-company-status)

## Pay choices

- Each card shows the person's pay choice in force this month, **No salary** when none is
  recorded; **Change pay choice** records a new one from a month, with the signed decision
  attached. Details: [[founders.pay-choice]].
  (src: docs/user-guide/director-accounts.md#record-a-pay-choice)

## Directors' fees

- **Fees** on a director's card records a fee the shareholders voted: the day of the meeting, the
  fee and the period, with the shareholders' resolution or minutes attached, then **Ask for
  approval**. (src: docs/user-guide/director-accounts.md#record-a-directors-fee)
- Another person who holds the approval right approves it, never the director paid; it then posts
  to 6050 Directors' fees on the day voted, owed to the director on 2120 until paid. A posted fee
  never changes: a journal to the director's account corrects it.
  (src: docs/user-guide/director-accounts.md#record-a-directors-fee)

## Settle a balance

- **Settle** on a person's card: choose **Repay in cash**, **Keep as a loan**, **Convert to
  shares** or **Waive** (a SAFE or note comes later), the account it is owed on and the
  amount, at most what is owed, then the details: a loan's interest and repayment day, or the
  account a waiver credits, the capacity and why; **Save the draft**.
  (src: docs/user-guide/director-accounts.md#settle-a-balance)
- Check what it posts, attach the loan note, board minute or deed of waiver, then **Ask for
  approval**. (src: docs/user-guide/director-accounts.md#settle-a-balance)
- An Owner or the Controller settles; another person who holds the approval right approves, never
  the person settled, and the accountant too when every other approver benefits. A loan kept posts
  nothing; a repayment goes in the next payment batch ([[founders.settle-balance]]); a waiver posts
  on the day approved. (src: docs/user-guide/director-accounts.md#settle-a-balance)

## Convert a balance to shares

- In **Settle**, **Convert to shares**: the class, the price per share and its basis, and the
  capacity; whole shares only, the rest stays owed. Attach all four documents, each as its type:
  the shareholders' approval, the board resolution, the conversion agreement and the balance
  confirmation; **Ask for approval**, which posts nothing.
  (src: docs/user-guide/director-accounts.md#convert-a-balance-to-shares)
- Once ACRA's register of members shows the allotment: the lodgement reference, the resolutions
  and the day registered, then **Record as registered**; it posts on that day, never earlier,
  and the shares join the register of share issues.
  (src: docs/user-guide/director-accounts.md#convert-a-balance-to-shares)
- The shareholders' approval lapses on the date by which the next AGM must be held; a closed
  period is reopened rather than a later date chosen; the measurement basis is the carrying
  amount unless the accountant notes the shares' fair value.
  (src: docs/user-guide/director-accounts.md#convert-a-balance-to-shares)

## Name the person on a line

- On a journal or opening balance line on 2500 or 2120, choose **Whose account** and **Where it
  came from**; a balance owed to two founders is two lines.
  (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)
- A journal that leaves the person out does not post, nor one that names a person on another
  account. (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)
- A bank line categorised to 2500 takes the founder as its contact: money in is lent, money out is
  paid out to them. (src: docs/user-guide/director-accounts.md#name-the-person-on-a-journal-or-opening-balance)

## Year-end confirmations

- After each financial year ends, every director and shareholder confirms their balances at the
  year's end in **Money I put in** with their passkey; a card says **Confirmed**, **Changed since
  confirmed** (an entry dated in that year posted after it) or **Not confirmed**, and a director
  with no member linked cannot confirm until the member is linked.
  (src: docs/user-guide/director-accounts.md#year-end-confirmations)
- The close of a financial year's last month warns of every director not confirmed.
  (src: docs/user-guide/director-accounts.md#year-end-confirmations)

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
| The fee has no shareholders' resolution attached | Attach the resolution or minutes, then ask for approval |
| The status has no evidence attached | Attach the ACRA business profile, then record it |
| The fee is paid to you, so another person approves it | Ask another Owner or the Controller |

(src: docs/user-guide/director-accounts.md#common-problems)
