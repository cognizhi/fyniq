---
key: checks.overview
type: task
title: Read books health and the checks, run them now and name a check's owner
summary: What books health and each check status mean, when the checks run, the nightly ledger verification, Run now, and naming a check's owner
sources:
  - docs/user-guide/checks.md sha256:39752601963080bbf2d47ac72db8a7507d8a039dd59df8d1f3c841690af52ea9
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-01
aliases:
  - books health
  - checks
  - checks page
  - blocks the close
  - not assessed
  - run the checks
  - check owner
  - nightly verification
  - why can't I close the month
---

# Read books health and the checks, run them now and name a check's owner

## What books health and the statuses mean

- **Books health** on Home is **OK**, **Review** or **Problems**; selecting it opens **Books health and checks**, every check by area with what each found.
- **Blocks the close**: something is wrong that stops the month closing until it is fixed. A check that could not run counts the same, shown as **Could not run**.
- **To review**: it posts, but someone should look at it. **Due**: a deadline is due or past. **OK**: it found nothing.
- **Not assessed**: it cannot run yet and says what it needs, such as an open month that has started.
- Books health is **Problems** when any check blocks the close, **Review** when any asks for a review or is due, and **OK** otherwise; checks not assessed are counted beside it and never change it.

(src: docs/user-guide/checks.md#what-it-is)

## What a check shows

- Each check says what it checks, what it found in plain words, and how to fix it.
- Items it found, such as a month or an account, are listed with a link to each; if the person's roles do not open the items' own page, the check gives only how many there are.
- Expert mode adds the check's row of the checks catalogue and the rules it checks.
- A check's **owner** looks into what it finds: the role the catalogue gives it, such as the Controller for the ledger, or a person named instead.

(src: docs/user-guide/checks.md#what-a-check-shows)

## When the checks run

- Within about a minute of a change they read, such as a posting or a new account.
- Every night, when every check runs, after the night's verification of the ledger.
- When someone selects **Run all checks now**.

(src: docs/user-guide/checks.md#when-the-checks-run)

## Nightly ledger verification

- Each night the books are worked out again from every entry and compared with what the person sees: the balances against their journal lines, and the audit trail's chain of events.
- The **Nightly ledger verification** card shows how last night's run went.

(src: docs/user-guide/checks.md#nightly-ledger-verification)

## Run the checks now

- **Run all checks now** runs every check without waiting; the page says **Running…** until they have run.
- The checks run at most once a minute: a second request in the same minute is refused with how long to wait.

(src: docs/user-guide/checks.md#run-the-checks-now)

## Name a check's owner

- An Owner or a Controller selects the pencil beside a check's owner, picks a person and saves; the first choice gives the check back to its role.
- Only someone whose roles open the check's items can own it; each change is recorded in the audit trail.

(src: docs/user-guide/checks.md#name-a-checks-owner)

## The checks so far

- The checks of the books and the ledger: debits equal credits, the balance sheet balances every month, the cash flow agrees to the bank accounts, the P&L and the balance sheet include every account, the opening balances balance, the system accounts exist, the movement in equity reconciles, the balances equal their lines, and the audit trail is intact.
- The checks of each module, such as invoices, bills or payroll, join as that module's checks are built.

(src: docs/user-guide/checks.md#the-checks-so-far)
