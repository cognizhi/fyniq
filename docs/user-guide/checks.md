---
key: checks.overview
title: Read books health and the checks
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, ar_specialist, ap_specialist, payroll_administrator, payment_releaser, auditor, tax_agent, dpo]
requirements: [FR-CTL-01, FR-CTL-02, FR-CTL-05]
status: draft            # written with the checks engine's first slice (P1); later modules add their checks
---

# Read books health and the checks

## What it is

**Books health** on Home says in one word how the books are: **OK**, **Review** or **Problems**.
Select it to open **Books health and checks**, which lists every check Fyniq runs on the books,
by area, with what each found.

Each check has one of these statuses, shown with its icon and its words:

- **Blocks the close**: something is wrong that stops the month closing until it is fixed. A
  check that could not run counts the same, as **Could not run**.
- **To review**: it posts, but someone should look at it.
- **Due**: a deadline is due or past.
- **OK**: it found nothing.
- **Not assessed**: it cannot run yet, and says what it needs, such as an open month that has
  started.

Books health is **Problems** when any check blocks the close, **Review** when any asks for a
review or is due, and **OK** otherwise. Checks not assessed are counted beside it and never
change it.

## What a check shows

Each check says what it checks, what it found in plain words, and how to fix it. When it found
items, such as a month or an account, they are listed with a link to open each one. If your
roles do not open the items' own page, the check says how many there are, without them. Expert
mode adds the check's row of the checks catalogue and the rules it checks.

A check's **owner** looks into what it finds: the role the catalogue gives it, such as the
Controller for the ledger, or a person named instead.

## When the checks run

The checks run by themselves:

- within about a minute of a change they read, such as a posting or a new account;
- every night, when every check runs, after the night's verification of the ledger;
- when you ask, with **Run all checks now**.

## Nightly ledger verification

Each night Fyniq works the books out again from every entry and compares the result with what
you see: the balances against their journal lines, and the audit trail's chain of events. The
**Nightly ledger verification** card shows how last night's run went.

## Run the checks now

Select **Run all checks now** to run every check without waiting. The page says **Running…**
until they have run, then shows what they found. The checks run at most once a minute; a second
request in the same minute is refused with how long to wait.

## Mark an item reviewed

An item **to review** posted, or is in the books, but someone should look at it: a bill posted
with GST that differs from the rounding rule, or an invoice issued late. Once you have looked,
select **Mark reviewed** beside it and say what you found, if you like. It leaves the check until
the item changes; a changed item comes back to be looked at again. An item that **blocks the
close** is fixed, never reviewed. The Owner, the Controller, the Accountant and the Bookkeeper can
mark items reviewed, and each review is kept in the audit trail.

Two checks are reviewed only where their control lives, so the people they watch cannot hide them
with a click: a logged action is reviewed in the **review log**, and the quarter's access review is
attested on **Settings → Access review**.

## Name a check's owner

An Owner or a Controller can name the person who owns a check: select the pencil beside its
owner, pick a person, and **Save**. Only someone whose roles open the check's items can own it.
Pick the first choice to give the check back to its role. Each change is recorded in the audit
trail.

## The checks so far

- **Books and ledger**: debits equal credits, the balance sheet balances every month, the cash
  flow agrees to the bank accounts, the P&L and the balance sheet include every account, the
  movement in equity reconciles, the balances equal their lines, and the audit trail is intact.
- **Setup**: the opening balances balance, the system accounts exist, nothing that posts by itself
  points at an account that cannot take postings, and no two contacts or monthly rates look
  alike.
- **Bank**: a waiting line that cannot post as it stands (dated in a closed month, with no rate,
  or on an inactive account), a line in suspense, and a line booked after today. A line that only
  waits in the queue is not a problem until it has waited 30 days.
- **Sales**: a draft invoice that cannot be issued, an invoice issued late, one more than 90 days
  overdue, a line worth nothing, and a quote to follow up.
- **Purchases**: a draft bill that cannot post, a posted bill with a warning, two bills alike, a
  bill or claim without its document, and an unpaid bill whose input tax is due back.
- **Journals**: a draft journal that cannot post, and a reversal waiting for its month to open.
- **Assets and recurring entries**: the asset register against its accounts; recurring journals and
  bills whose drafts are late, or whose content cannot post.
- **Bank at the month end**: suspense not at nothing, a bank or cash account below zero, a signed
  reconciliation that does not match, bank data more than 3 days old, lines waiting more than 30
  days, possible duplicates, a connector's sync that failed, and settlement differences over the
  monthly cap.
- **Closing**: the sub-ledgers against their control accounts, automatic reversals waiting, large
  variances without an explanation, the close checklist, the revaluation of foreign-currency
  balances, a closed month changed since its close, and a month still open 15 days after its end.
  The checks of a month's end wait until the month has ended.
- **Planning**: a runway below 6 months.
- **Controls**: in Founder mode, a logged action another founder has not reviewed within 14 days; a
  person whose roles conflict with nothing to cover it; an external person still a member after
  their roles ended; and an access review not attested 14 days after its quarter ended.

The checks of each module still to come, such as payroll, join as that module is built.
