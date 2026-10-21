---
key: accounting.journals
type: task
title: Prepare, post and reverse journals
summary: Prepare, check and post journals, ask for approval, edit or reverse a posted one, set up recurring journals, and fix automatic reversals that wait
sources:
  - docs/user-guide/journals.md sha256:e088e2b989eb6168dbe23aff40370ada8515272b531fa696ce9f57ae17bda8db
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - journal
  - manual journal
  - journal entry
  - accrual
  - post a journal
  - reverse a journal
  - void a journal
  - draft journal
  - self-approval threshold
  - journal approval
  - journal status
  - entry does not balance
  - edit a journal
  - auto-reversing journal
  - accrual reversal
  - correct a posted journal
  - recurring journal
  - repeating journal
  - monthly accrual
  - scheduled journal
  - pause a recurring journal
  - automatic reversal waiting
  - reversal waiting
---

# Prepare, post and reverse journals

## What journals are

- **Accounting → Journals** (Expert mode) lists the manual journals, newest first, each with its number, date, description, total (its debits), who prepared it, who posted it and its status: **Draft**, **Posted** or **Reversed**.
- A journal moves amounts between accounts (an accrual, a correction, a reclassification); its debits equal its credits, and it posts on its date.
- Narrow the list by status, or search part of a description or number.
- An Accountant or a Tax agent prepares journals; only an Accountant posts and reverses them, and a Controller approves the ones that need a second person; others with a finance role can read them.

(src: docs/user-guide/journals.md#what-it-is)

## Prepare a journal

- **New journal**: the **Date** and a **Description**, then each line's account, an optional description, and the amount under **Debit** or **Credit**, never both; **Add a line** for more.
- The balance under the lines says **Balanced** once debits equal credits in each currency; then **Save as a draft**.
- Changing a draft and selecting **Save the changes** keeps the earlier version in its history; Fyniq records who saved each version.
- Type amounts without a sign, as `1,234.56` or `S$1,234.56`; a credit goes under **Credit**. A line in another currency takes its currency code and the rate: how much of the base currency one unit buys.

(src: docs/user-guide/journals.md#prepare-a-journal)

## Check and post

- A draft says what stops it posting, as the workbook's Journal sheet did: the list shows the first problem beside its status, such as **Entry does not balance**, and the draft lists every one with how to fix it; a draft with nothing to fix shows **Ready to post**, and a posted journal whose month is closed shows **Locked (closed month)**.
- Open the draft: **Check** runs every posting rule and shows what posting writes in the base currency, saving nothing.
- **Post**: the journal takes its number and posts on its date; Fyniq records who posted it and when.
- Once posted, a journal never changes in place: it is edited or reversed, and the ledger keeps every entry.

(src: docs/user-guide/journals.md#check-and-post-a-journal)

## Journals that reverse by themselves

- For an accrual: while preparing the journal, tick **Reverses by itself**; Fyniq suggests the first day of the month after the journal's date, and any other date after the journal's will do; then post as usual.
- On that date Fyniq reverses the journal by itself, with the reason "Automatic reversal", and the journal shows **Reversed**; the reversal belongs to the journal as posted, so it needs no approval of its own.
- If the reversal's month is closed or not open yet, it waits and Fyniq tries again every hour; the journal page shows the date until it reverses.

(src: docs/user-guide/journals.md#journals-that-reverse-by-themselves)

## Automatic reversals that wait

- A reversal cannot post into a month that is closed, or into a financial year not open yet (a December accrual that reverses on 1 January before the new year is opened); Fyniq keeps it waiting and tries again every hour.
- The journal shows **Its automatic reversal is waiting**, with what to do; **Journals** lists the ones that wait at the top; **Home** counts them under **Waiting for you** ([[home.overview]]).
- The close of a month warns (⚠) of a journal that reverses by itself on or before the month's end: once the month closes, its reversal cannot post ([[accounting.periods]]).
- Fix: open the financial year in **Accounting → Periods and close**, or ask for the month to be reopened; the reversal then posts within the hour. Or reverse the journal yourself in an open month.

(src: docs/user-guide/journals.md#automatic-reversals-that-wait)

## Recurring journals

- A recurring journal repeats, such as a monthly rent accrual: Fyniq makes its **draft** on each date, and a person checks and posts it like any other journal. Nothing recurring posts by itself; the usual approvals apply.
- Set one up: **Accounting → Journals**, the **Recurring** tab, **New recurring journal**; name it; repeat every month, every 3 months, every 6 months or every year on a day of the month (the 29th to the 31st fall on the last day of a shorter month); a start date and, if it stops, an end date; the description and lines of each draft; for an accrual, tick **Each draft reverses by itself** (each draft reverses on the first day of the next month once posted).
- A start in the past makes the drafts of the dates already passed, up to 12 an hour.
- **Check** shows the date of its next draft and anything that would stop its drafts posting, saving nothing; then **Save**.
- Each draft is dated on its date, named after the recurring journal it came from, and takes its number when it posts. Whoever wrote the recurring journal's lines does not approve the posting of its drafts: another person does.
- A change to its lines is a new version; drafts already made keep theirs. **Pause** stops it making drafts; **Resume** starts again from today and skips the dates that passed while it was paused (journal them by hand if needed).
- Its status shows anything the posting rules would refuse, such as an inactive account, and **Due but not created** if a date passed with no draft.
- The close warns of a recurring journal due in the month with no draft yet, and of an active one whose lines would not post; it refuses to close if a draft is more than a day late, because the scheduler has failed: tell an administrator.

(src: docs/user-guide/journals.md#recurring-journals)

## Journals above the self-approval threshold

- A journal whose debits are above the self-approval threshold (S$2,000 unless the company changed it in **Settings → Organisation**, [[settings.organisation]]) posts only once another person approves it.
- **Post** says another person approves this journal; select **Ask for approval** and confirm it's you; the journal then shows that a request waits.
- A Controller who did not prepare the journal approves it in **Inbox → To approve** ([[approvals.decide]]), and it posts then, with them as the one who posted it.
- Changing the draft while the request waits cancels it: ask again once it is ready.
- In Founder mode a journal at or below the threshold posts at once, flagged as self-approved, and another founder reviews it in the weekly review.

(src: docs/user-guide/journals.md#journals-above-the-self-approval-threshold)

## Reverse a posted journal

- Open it, select **Reverse**, choose the date of the reversing entry (on or after the journal's date) and say why, then **Reverse**.
- A reversing entry with every line the other way round posts on that date; the journal stays in the books, marked **Reversed**, and the audit trail keeps the reason. A journal is reversed once.
- In Founder mode a reversal needs a second person: the button says **Ask to reverse**; give the date and the reason the same way, and another founder approves it in **Inbox → To approve**; the reversal posts then.
- Related: opening the financial year so journals can post ([[accounting.periods]]); the accounts journals post to ([[accounting.chart]]); the lines they post, in the general ledger ([[reports.general-ledger]]).

(src: docs/user-guide/journals.md#reverse-a-posted-journal) (src: docs/user-guide/journals.md#related)

## Edit a posted journal

- A posted journal in a month that is still open is corrected instead of reversed and entered again: open it, select **Edit**, change the date, the description or the lines, and say why.
- **Check the edit** shows what it posts, saving nothing; **Post the edit** posts it.
- The entry the journal had is reversed on its own date and the new version posts on its date, both at once; both months must be open.
- The journal keeps its number and stays **Posted**; Fyniq records who prepared the new version and who posted the edit; the ledger keeps the first entry, its reversal and the new one ([[reports.general-ledger]]).
- In Founder mode an edit needs a second person: **Ask to post the edit**, and another founder approves it in **Inbox → To approve**, where each changed line is listed. An edit whose new debits are above the self-approval threshold needs another person's approval in any mode.

(src: docs/user-guide/journals.md#edit-a-posted-journal)

## Common problems

- STORAGE-PERIOD-01, no period covers the posting date: open the financial year in **Accounting → Periods and close**, then post again.
- STORAGE-INV-03, the period is closed: post in an open period, or ask for it to be reopened.
- LEDGER-09 or LEDGER-10, it does not balance: make the debits equal the credits in each currency.
- LEDGER-02 or LEDGER-03: at least two lines, each with an amount.
- LEDGER-05 or LEDGER-06: choose an active account that is not a group.
- LEDGER-07 or LEDGER-13: a rate only on a line in another currency.
- LEDGER-16: date a reversal on or after the journal's date.
- JOURNAL-11, the automatic reversal is not after the journal: date it after the journal's date, such as the first of the next month.
- JOURNAL-02, not a draft: a posted journal is reversed, not changed.
- JOURNAL-07, its debits are above the self-approval threshold: select **Ask for approval**.
- JOURNAL-08, in Founder mode a reversal needs a second person: select **Ask to reverse**.
- JOURNAL-09, you prepared this journal: another person who did not prepare it approves it.
- JOURNAL-10, in Founder mode an edit needs a second person: select **Ask to post the edit**.
- JOURNAL-WAITING, its automatic reversal is waiting: open the financial year, or ask for the month to be reopened.
- RECURRING-01, the schedule has no date: set the end on or after its first date, or leave the end out.
- RECURRING-02, the recurring journal is already paused, or has ended: resume a paused one; an ended one makes no more drafts.
- JOURNAL-09 on a draft from a recurring journal: you wrote its lines, so another person approves it.

(src: docs/user-guide/journals.md#common-problems)
