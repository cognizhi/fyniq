---
key: accounting.journals
title: Prepare, post and reverse journals
mode: expert
roles: [accountant, tax_agent, controller, owner, auditor]
requirements: [FR-GL-01, FR-GL-02, FR-GL-12, FR-AUT-01, FR-INT-14, ENG-20, ENG-21, ENG-24, ENG-40, FM-1, FM-2, SOD-3, FR-DOC-05, FR-DOC-07]
status: draft            # written with the journal screens (P1, p1-general-ledger-close); reviewed with the accountant
---

# Prepare, post and reverse journals

## What it is

**Accounting → Journals** lists the company's manual journals, newest first. A journal moves
amounts between accounts: an accrual, a correction, a reclassification. Its debits equal its
credits, and it posts on its date.

- Each journal shows its number, date, description, total (its debits), who prepared it, who posted
  it and its status: **Draft**, **Posted** or **Reversed**.
- Choose a status to narrow the list, or type part of a description or number in the search box.
- An Accountant or a Tax agent prepares journals. Only an Accountant posts and reverses them, and a
  Controller approves the ones that need a second person. Everyone else with a finance role can
  read them. The pages are in Expert mode.

## Prepare a journal

1. Select **New journal**.
2. Enter the **Date** the journal belongs to and a **Description** of what it is for.
3. For each line, choose the **account**, add a description if it helps, and enter the amount under
   **Debit** or **Credit**, never both. Select **Add a line** for more.
4. Watch the balance under the lines: it says **Balanced** once the debits equal the credits in each
   currency.
5. Select **Save as a draft**.

A draft keeps every version: when you change it and select **Save the changes**, the earlier version
stays in its history. Fyniq records who saved each version.

Type amounts without a sign, as `1,234.56` or `S$1,234.56`: a credit goes under **Credit**. For a
line in another currency, enter its currency code. Its **Rate** is how much of the base currency one
unit buys. Leave it blank and the line takes the rate for the journal's date from
**Accounting → Currencies** when it is checked and posted; under the field, Fyniq says which rate and
where from. A rate you type is the journal's own and is used instead.

## Check and post a journal

A draft says what stops it posting, as the workbook's Journal sheet did: the list shows the first
problem beside its status, such as **Entry does not balance**, and the draft lists every one with how
to fix it. A draft with nothing to fix shows **Ready to post**. A posted journal whose month is closed
shows **Locked (closed month)**.

1. Open the draft from the list.
2. Select **Check**. Fyniq runs every posting rule and shows what posting writes in the base
   currency, without saving anything.
3. Select **Post**. The journal takes its number and posts on its date.

Fyniq records who posted it and when. Once posted, a journal never changes in place: it is edited or
reversed, and the ledger keeps every entry.

## Journals that reverse by themselves

An accrual is often reversed on the first day of the next month. Instead of reversing it by hand:

1. While preparing the journal, tick **Reverses by itself**. Fyniq suggests the first day of the
   month after the journal's date; change it if you need another date after the journal's.
2. Post the journal as usual.

On that date Fyniq reverses the journal by itself, with the reason "Automatic reversal", and the
journal shows **Reversed**. The reversal belongs to the journal you posted, so it needs no approval of
its own. If the month of the reversal is closed or not open yet, the reversal waits and Fyniq tries
again every hour. The journal page shows the date until it reverses.

## Automatic reversals that wait

A reversal cannot post into a month that is closed, or into a financial year that is not open yet,
such as a December accrual that reverses on 1 January before the new year is opened. Fyniq then
keeps the reversal waiting and tries again every hour:

- the journal shows **Its automatic reversal is waiting**, with what to do;
- **Journals** lists the journals that wait at the top, and **Home** counts them under **Waiting for
  you**;
- the close of a month warns (⚠) of a journal that reverses by itself on or before the month's end,
  because once the month closes its reversal cannot post.

Open the financial year in **Accounting → Periods and close**, or ask for the month to be reopened.
The reversal then posts within the hour. You can also reverse the journal yourself in an open month.

## Recurring journals

A recurring journal repeats: a monthly rent accrual, say. Fyniq makes its **draft** on each date, and
you check and post it like any other journal. Nothing recurring posts by itself, and the usual
approvals apply.

1. Open **Accounting → Journals**, select the **Recurring** tab, then **New recurring journal**.
2. Name it, and choose how often it repeats: every month, every 3 months, every 6 months or every
   year, on a day of the month. The 29th to the 31st fall on the last day of a shorter month.
3. Choose the date it starts from and, if it stops, the date it ends. A start in the past makes the
   drafts of the dates already passed, up to 12 an hour.
4. Enter the description and the lines of each draft. For an accrual, tick **Each draft reverses by
   itself**: each draft then reverses on the first day of the next month once it is posted.
5. Select **Check** to see the date of its next draft and anything that would stop its drafts
   posting. Nothing is saved. Then select **Save**.

Each draft is dated on its date and named after the recurring journal it came from. It takes its
number when it posts. Whoever wrote the recurring journal's lines does not approve the posting of its
drafts: another person does.

- **Changing it**: a change to its lines is a new version. Drafts already made keep theirs.
- **Pause** stops it making drafts. **Resume** starts again from today: the dates that passed while
  it was paused are skipped. Journal them by hand if you need them.
- Its status shows anything the posting rules would refuse, such as an inactive account, and
  **Due but not created** if a date passed with no draft.
- The close of a month warns of a recurring journal due in it with no draft yet, and of an active
  one whose lines would not post. It refuses to close if a draft is more than a day late, because the
  scheduler has failed: tell an administrator.

## Journals above the self-approval threshold

A journal whose debits are above the self-approval threshold (S$2,000 unless your company changed it
in **Settings → Organisation**) posts only once another person approves it.

1. Open the draft and select **Post**. Fyniq says another person approves this journal.
2. Select **Ask for approval** and confirm it's you.
3. The journal shows that a request waits. A Controller who did not prepare the journal approves it
   in **Inbox → To approve**, and it posts then, with them as the one who posted it.

Changing the draft while the request waits cancels the request: ask again once it is ready. In
Founder mode a journal at or below the threshold posts at once; it is flagged as self-approved, and
another founder reviews it in the weekly review.

## Reverse a posted journal

1. Open the posted journal.
2. Select **Reverse**.
3. Choose the date of the reversing entry, on or after the journal's date, and say why.
4. Select **Reverse**.

A reversing entry with every line the other way round posts on that date. The journal stays in the
books, marked **Reversed**, and the audit trail keeps your reason. A journal is reversed once.

In Founder mode a reversal needs a second person: the button says **Ask to reverse**. Fill in the
date and the reason the same way and select **Ask to reverse**; another founder approves it in
**Inbox → To approve**, and the reversal posts then.

## Edit a posted journal

A posted journal in a month that is still open can be corrected instead of reversed and entered
again.

1. Open the posted journal and select **Edit**.
2. Change the date, the description or the lines, and say why it is edited.
3. Select **Check the edit** to see what it posts, without saving anything.
4. Select **Post the edit**.

The entry the journal had is reversed on its own date, and the new version posts on its date, both at
once; both months must be open. The journal keeps its number and stays **Posted**, and Fyniq records
who prepared the new version and who posted the edit. The ledger keeps the first entry, its reversal
and the new one.

In Founder mode an edit needs a second person: the button says **Ask to post the edit**, and another
founder approves it in **Inbox → To approve**, where each changed line is listed. An edit whose new
debits are above the self-approval threshold needs another person's approval in any mode.

## Attach evidence

Attach the files a journal rests on, such as the calculation of an accrual, an invoice or a board
resolution, under **Evidence** on the journal's page.

1. Choose the file: a PDF, an image, a CSV or text file, an XLSX or DOCX file, or an email, up to
   25 MB.
2. Say what it is, such as "calculation" or "invoice", and add a note if it helps.
3. Say whether it holds personal data or bank details, then select **Attach**.

- Fyniq decides the file's type from its content, scans it for malware, and strips image metadata
  such as location before keeping it.
- While the journal is a draft, a file can come off again. Once the journal is posted, its files are
  kept, and never deleted, for as long as the records are; a file added later is kept the same way.
- The Owner, the Controller, the Accountant and the Tax agent attach files; everyone who can see the
  journal sees them.

## Common problems

| Message | What to do |
|---|---|
| No period covers the posting date (STORAGE-PERIOD-01) | Open the financial year in **Accounting → Periods and close**, then post again |
| The period is closed (STORAGE-INV-03) | Post in an open period, or ask for the period to be reopened |
| The journal does not balance (LEDGER-09, LEDGER-10) | Make the debits equal the credits, in each currency |
| Fewer than two lines, or a line of zero (LEDGER-02, LEDGER-03) | Give the journal at least two lines, each with an amount |
| The account is a group or inactive (LEDGER-05, LEDGER-06) | Choose an active account that is not a group |
| A rate is missing, or entered on a base-currency line (LEDGER-07, LEDGER-13) | Enter a rate only on a line in another currency |
| No exchange rate for this currency (FX-01) | Type the rate on the line, or add the rate for the day or the month in **Accounting → Currencies** |
| A reversal dated before the journal (LEDGER-16) | Date the reversal on or after the journal's date |
| The automatic reversal is not after the journal (JOURNAL-11) | Date it after the journal's date, such as the first of the next month |
| The journal is not a draft (JOURNAL-02) | A posted journal is reversed, not changed |
| Its debits are above the self-approval threshold (JOURNAL-07) | Select **Ask for approval** |
| In Founder mode a reversal needs a second person (JOURNAL-08) | Select **Ask to reverse** |
| You prepared this journal (JOURNAL-09) | Another person who did not prepare it approves it |
| In Founder mode an edit needs a second person (JOURNAL-10) | Select **Ask to post the edit** |
| Its automatic reversal is waiting (JOURNAL-WAITING) | Open the financial year, or ask for the month to be reopened |
| The schedule has no date (RECURRING-01) | Set the end on or after its first date, or leave the end out |
| The recurring journal is already paused, or has ended (RECURRING-02) | Resume a paused one; an ended one makes no more drafts |
| You wrote the recurring journal this draft was made from (JOURNAL-09) | Another person approves it |

## Related

- [Approve a change](approvals.md)
- [Organisation settings: the self-approval threshold](organisation-settings.md)
- [Open the financial year and close the periods](periods-and-close.md)
- [Set up and keep the chart of accounts](chart-of-accounts.md)
- [Read the general ledger](general-ledger.md)
- [Exchange rates](currencies.md)
