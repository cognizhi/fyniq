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

## Name a check's owner

An Owner or a Controller can name the person who owns a check: select the pencil beside its
owner, pick a person, and **Save**. Only someone whose roles open the check's items can own it.
Pick the first choice to give the check back to its role. Each change is recorded in the audit
trail.

## The checks so far

The checks of the books and the ledger come first: debits equal credits, the balance sheet
balances every month, the cash flow agrees to the bank accounts, the P&L and the balance sheet
include every account, the opening balances balance, the system accounts exist, the movement in
equity reconciles, the balances equal their lines, and the audit trail is intact. The checks of
each module, such as invoices, bills or payroll, join as that module's checks are built.
