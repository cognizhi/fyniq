---
key: checks.overview
type: task
title: Read books health and the checks, run them now, mark items reviewed and name a check's owner
summary: Books health and the check statuses, when checks run, nightly verification, Run now, marking items reviewed, and naming owners
sources:
  - docs/user-guide/checks.md sha256:f7c709205d3fe7e567e1871bce346f0ac79b33ff871643e9a5c8bbf1de940c7f
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-02
aliases:
  - books health
  - checks
  - checks page
  - blocks the close
  - not assessed
  - run the checks
  - check owner
  - mark reviewed
  - close checklist
  - runway warning
  - acknowledge a warning
  - nightly verification
  - why can't I close the month
  - controls checks
---

# Read books health and the checks, run them now, mark items reviewed and name a check's owner

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
- In Expert mode, **Compliance** › **Controls** shows these verifications, and the checks that answer for a control, beside the rest of the controls ([[compliance.controls]]).

(src: docs/user-guide/checks.md#nightly-ledger-verification)

## Run the checks now

- **Run all checks now** runs every check without waiting; the page says **Running…** until they have run.
- The checks run at most once a minute: a second request in the same minute is refused with how long to wait.

(src: docs/user-guide/checks.md#run-the-checks-now)

## Mark an item reviewed

- An item **to review** posted or is in the books, but someone should look at it, such as a bill posted with GST that differs from the rounding rule, or an invoice issued late.
- **Mark reviewed** beside it, with an optional note, takes it off the check until the item changes; a changed item comes back.
- An item that **blocks the close** is fixed, never reviewed.
- The Owner, the Controller, the Accountant and the Bookkeeper can mark items reviewed; each review is kept in the audit trail.
- Two checks are reviewed only where their control lives: a logged action in the **review log**, and the quarter's access review on **Settings → Access review** ([[settings.access-review]]).

(src: docs/user-guide/checks.md#mark-an-item-reviewed)

## Name a check's owner

- An Owner or a Controller selects the pencil beside a check's owner, picks a person and saves; the first choice gives the check back to its role.
- Only someone whose roles open the check's items can own it; each change is recorded in the audit trail.

(src: docs/user-guide/checks.md#name-a-checks-owner)

## The checks so far

- **Books and ledger**: debits equal credits, the balance sheet balances every month, the cash flow agrees to the bank accounts, the P&L and the balance sheet include every account, the movement in equity reconciles, the balances equal their lines, and the audit trail is intact.
- **Setup**: the opening balances balance, the system accounts exist, nothing that posts by itself points at an account that cannot take postings, and no two contacts or monthly rates look alike.
- **Bank**: a waiting line that cannot post as it stands (dated in a closed month, with no rate, or on an inactive account), a line in suspense, and a line booked after today; a line that only waits is not a problem until it has waited 30 days.
- **Sales**: a draft invoice that cannot be issued, an invoice issued late, one more than 90 days overdue, a line worth nothing, and a quote to follow up.
- **Purchases**: a draft bill that cannot post, a posted bill with a warning, two bills alike, a bill or claim without its document, and an unpaid bill whose input tax is due back.
- **Journals**: a draft journal that cannot post, and a reversal waiting for its month to open.
- **Assets and recurring entries**: the asset register against its accounts; recurring journals and bills whose drafts are late, or whose content cannot post.
- **Bank at the month end**: suspense not at nothing, a bank or cash account below zero, a signed reconciliation that does not match, bank data more than 3 days old, lines waiting more than 30 days, possible duplicates, a connector's sync that failed, and settlement differences over the monthly cap.
- **Closing**: the sub-ledgers against their control accounts, automatic reversals waiting, large variances without an explanation, the close checklist, the revaluation of foreign-currency balances, a closed month changed since its close, and a month still open 15 days after its end; the checks of a month's end wait until it has ended.
- **Planning**: a runway below 6 months.
- **Controls**: in Founder mode, a logged action not reviewed by another founder within 14 days; a person whose roles conflict with nothing to cover it; an external person still a member after their roles ended ([[members.remove]]); and an access review not attested 14 days after its quarter ended.
- **Payments**: a supplier whose bank details were added or changed in the last 30 days with a bill still to pay (confirm the new account on a number already known before paying), and a cash advance open past the claim deadline.
- **The platform**: a scheduled job (recurring documents, depreciation, the month-end revaluation) that has not run in its time; a statutory pack past its review date or with a later version not activated; an evidence file that no longer matches its fingerprint; the email sent, from the sending domains' records and, over 30 days, 2% hard bounces or 0.1% complaints with 50 or more sent, or any complaint with fewer; and an integration whose last test failed or whose key expires within 14 days ([[settings.integrations]]).
- The checks of each module still to come, such as payroll, join as that module is built.

(src: docs/user-guide/checks.md#the-checks-so-far)
