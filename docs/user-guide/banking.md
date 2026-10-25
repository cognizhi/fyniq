---
key: banking.accounts
title: Bank accounts
mode: basic
roles: [bookkeeper, accountant, controller, owner, administrator, ar_specialist, ap_specialist, payment_releaser, auditor, tax_agent]
requirements: [FR-BNK-01, FR-BNK-02, FR-BNK-04, FR-BNK-05, FR-BNK-06, FR-BNK-07, FR-BNK-08, FR-BNK-09, FR-BNK-11, FR-BNK-13, FR-BNK-14, FR-BNK-15, FR-BNK-16, FR-BNK-17, FR-BNK-18, FR-DOC-08, FR-INT-22]
status: draft            # written with the banking screens (P1, p1-bank-import-reconcile task 10.1); reviewed with the accountant
---

# Bank accounts

## What it is

**Banking** shows every money account the company holds: bank current accounts, cards, wallets such as
Wise or Airwallex, payment gateways and petty cash. For each one it shows its balance, how up to date its
data is, and how many of its bank lines still wait to be categorised or matched.

- **Cash position** at the top adds up the cash in every open money account. An account in another
  currency is converted at the day's rate; one with no rate for the day is listed apart and left out of
  the total.
- A Bookkeeper or an Accountant adds money accounts and imports statements. The Owner, the Controller,
  the AR and AP specialists, the Payment releaser, the Auditor and the Tax agent can see them.
- Connections to banks and wallets are read-only: Fyniq never moves money through them. Payments go out
  from your bank, as approved files or by hand.

## Add a money account

Select **Add account** and follow the three steps.

1. **The account**: its kind (bank, card, wallet, payment gateway or cash), its name, the bank or
   provider, and the account in the chart it posts to. Only an account marked as a money account in the
   chart can be chosen; its currency is the money account's. Enter the account number, or the provider's
   balance ID for Wise or Airwallex: Fyniq shows only its last four characters afterwards.
2. **Where its data comes from**: statement files you import, the bank's reports by email, or a
   connector such as Wise or Airwallex. See [Where its data comes from](#where-its-data-comes-from).
3. **Check**: what happens next, such as waiting for the first emailed report.

## Where its data comes from

- **Statement files**: you import the bank's export (CSV, XLSX, OFX, camt.053 or MT940). This is
  the default, and needs nothing more.
- **Bank reports by email**: the bank emails its daily report, and its incoming-funds alerts, to your
  **bank-ingest address**. Only the bank's addresses you register are accepted, and only when the
  bank's mail passes DMARC; anything else waits in quarantine for someone to decide. The worker imports
  each report as a statement, and its lines wait for **Reconcile now**.
- **A connector**: an organisation connector such as Wise or Airwallex, installed and turned on under
  **Settings → Integrations**, reads the account's transactions every hour with a read-only key.

Setting where the data comes from, and naming the bank-ingest address, is for an Administrator. A
source starts on a day and is never changed for the days before it, so switching from emailed reports to
a connector leaves no gap.

## An account's statements and lines

Select an account to see its statements, its lines and anything that waits for a person.

- **Statements** lists each statement with its days, its opening and closing balances, its lines and
  how it came: a file, an emailed report or a connector. **Continues** means its opening balance is the
  previous statement's closing balance; **Gap before it** ⚠ means it is not.
- **Lines** shows each line's booking date, description, counterparty, amount and status: **Waiting**,
  **Reconciled**, **In suspense** ⚠, **Possible duplicate** ⚠ or **Excluded**. Descriptions are shown as
  the bank sent them, as text.
- A **possible duplicate** is a line that looks like one already held, without the bank's reference or a
  running balance to prove it. Select **Compare** to see both side by side, then **Keep it** as a line
  of its own, or **Exclude it** with the reason. Neither posts anything.
- **Incoming-funds alerts** are listed apart, as pending: they are not lines, and nothing is posted from
  them. Each names the invoice it likely pays; **Dismiss** one with a reason when it is wrong.
- **Transfers in transit** are transfers recorded with this account whose other side's line has not
  arrived yet.

## Import statements

Select **Import statement**, then choose the bank's files: several at once, for one account or several.

1. For each file, check its **format**. A CSV or XLSX file needs its **account** and a saved **column
   mapping**; OFX, camt.053 and MT940 files name their own account.
2. Select **Preview**: for each statement it shows the lines to add, the lines held already and dropped,
   the possible duplicates kept for you, and whether it continues the balance chain. Nothing is saved.
3. Select **Import**. Each file is imported on its own, with its own result; Fyniq then reconciles what it
   can by itself, within the organisation's limits, and the rest waits in the queue.

- Each file is checked and scanned for malware before it is read, and kept with its statements.
- **New column mapping** saves which column of a bank's CSV or XLSX export holds the date, the amount (one
  signed column, or money in and money out), and optionally the reference, counterparty, description and
  running balance. With a running balance, an overlap with a statement held already is proved, and its
  lines are dropped.

## Categorise the waiting lines

**Categorise** lists the lines waiting for a person: new lines, possible duplicates and lines in
suspense. Fyniq never applies a suggestion by itself.

- Each line shows whether money came in or went out, its amount, and its description as the bank sent it.
- **What is this?** lists the suggestions, best first: money already recorded by hand, an open invoice or
  bill whose number the line names, one of the amount the line's counterparty owes, then the first bank
  rule the line meets.
- **Accept** posts the line as the chosen suggestion says: a payment of the invoice or bill, a match to
  the money recorded by hand, or the rule's category. **Change the category** posts the whole line to
  the account you choose, with its GST code and, if you like, its contact. **Skip** moves on and comes
  back to it later.
- **Reconcile now** lets Fyniq reconcile the exact matches among the waiting lines by itself, within the
  organisation's auto-reconcile limits, and says how many it left for you and why. Lines that arrive by
  email or from a connector wait for it.
- A possible duplicate ⚠ says so: compare it on the account first. A line in suspense ⚠ is posted to
  suspense (2999) until you categorise or match it.

In Expert mode:

- **Grid view** lists every waiting line with its best suggestion: select several and accept their
  suggestions at once. Each line posts on its own, so one refused never stops the others.
- **Split the line** posts it to several accounts, each part with its amount; one part may take the
  rest of the line.
- **Mark as a transfer** matches money moved between two of your accounts to the other side's line, or
  leaves it in transit until that line arrives.
- **Match to documents** settles one customer's invoices or one supplier's bills with the line, several
  at once. What it pays beyond them stays as a credit with them; a small shortfall settled in full clears
  within the organisation's settlement tolerance.
- **Suspense** posts the line to suspense until its purpose is known.

## Bank rules

**Rules** (Expert mode) teach Fyniq how to code lines that come back, such as every AWS charge to
Software. A rule suggests; it does not post by itself.

- A rule meets a line by what its description contains or a regular expression it matches, its
  counterparty, money in or out, its size, its currency and its money account. It codes the line to an
  account, with a GST code and, if you like, a contact.
- Rules are in an order: the first rule a line meets is the one suggested.
- **Test it** shows how many of the lines held the rule would have matched, and a few of them, before
  you save it.
- **Ask to apply by itself** asks another person to let the rule reconcile the lines it meets without a
  click, within the organisation's auto-reconcile limits. You confirm it's you first; changing the rule
  meanwhile stops the request, and a change afterwards needs a new approval.
- **Rules to consider** lists lines coded the same way several times; **Make it a rule** starts a rule
  from them. None applies until you save it.
- A rule that no longer reads, such as one whose account was turned off, says so ⚠ and matches nothing
  until it is changed. A rule an AI proposal made carries the AI label.

## What Fyniq reconciled by itself

**Auto-reconciled** lists what Fyniq reconciled without a click in the last 30 days and still stands:
exact matches (the amount, the document's number and the contact), the other side of a transfer, and
lines an approved rule met.

- Each shows why, and the principal and time that did it.
- **Undo it** puts the line back in the queue with your reason, while its month is open: what it posted
  is reversed or voided on its own date.
- The page shows the limits Fyniq works within. Each week the 5 largest and 5 others picked at random
  join the weekly review.

## Freshness and gaps

- **Complete to** is the last day the account's statements cover with no break before it.
- **Last statement** is when its latest statement arrived.
- A **gap** ⚠ is a break in the balance chain: a statement whose opening balance is not the previous
  closing balance. Import the missing statement, or resolve the gap with the reason there is none.
- An account with no statements yet says so.

## Related

- Incoming-funds alerts and when they expire: [Organisation settings](organisation-settings.md).
- Connectors: [Integrations](integrations.md).
