---
key: home.overview
title: The Home page and finding your way around
mode: basic
roles: [all]
requirements: [FR-UX-01, FR-UX-29, FR-UX-31, FR-UX-40, FR-KPI-01, FR-KPI-03, FR-PLN-03, REL-05, FR-INT-11]
status: draft            # written with the app shell's first slice (P1); reviewed when the shell's screens are built
---

# The Home page and finding your way around

## What it is

**Home** is where Fyniq opens once you are signed in. It says which organisation you are signed in
to, which currency its books are kept in, and its team mode. The pages of your workspace appear as
each module arrives; you only ever see the pages your roles allow.

- **The pulse card** shows the cash in the bank at the end of this month, the **runway** (how many
  months that cash lasts at the average net burn of the last three months, or **Not burning** when
  those months brought in more cash than they used) and **books health**: **OK**, **Review** or
  **Problems**, with how many checks block the close, ask for a review or are due, and how many are
  not assessed. Select it to open the checks ([Read books health and the checks](checks.md)). If
  your roles do not read the checks, the card shows this month's **net burn** instead.
- **The tiles** show what customers owe the company and what it owes, and this month's net burn
  when books health is on the pulse card. In Expert mode they add the
  month's revenue, the gross margin and the net profit for the financial year so far, and three
  charts: revenue, costs and profit by month; cash by month; and costs by function, this month
  beside the last.
- Select **ⓘ** beside a figure to see how it is worked out, the dates it reads, where it comes from
  and its definition's version. Select the figure to open the report behind it. A figure the books
  cannot give shows **—** with the reason, never 0.
- **All KPIs** opens the start-up KPIs ([Read the start-up KPIs](kpis.md)).
- **Waiting for you** counts the requests waiting for your approval and the high-risk actions you
  can review, with any that are overdue. Select one to open the Inbox or the review log. It also
  counts the automatic reversals waiting for a month to be opened; select it to open **Journals**.
- **Set up the company** lists the first steps, each marked **Done** or **To do**, with progress.
  It goes once every step is done.

The pulse card, the tiles and the charts appear once the books have a month and your roles let you
read the reports. When your roles read the plans, the tiles add the lowest balance of the next 60 days, the
month cash runs out in the latest forecast and, in Expert mode, revenue and costs against the budget (see
[Read the plans on Home](#read-the-plans-on-home)). Unbilled time and GST due join them with their
modules.

## Before you start

- Sign in first ([Sign in to Fyniq](sign-in.md)). If Fyniq has not been set up yet, it opens the
  setup page instead ([Set up a new installation](setup-first-run.md)).

## Steps

### Set up the company

Work down the list. Each step opens the page where you do it:

- Give yourself the roles you work in, such as **Controller** and **Accountant**: the Owner role
  approves but does not keep the books (**Settings → Members and roles**). Before a second person
  joins, the change is made at once.
- Enter the company's legal name and UEN, and set when the books start (**Settings → Organisation**).
- Load the chart of accounts, and add the bank accounts to it (**Accounting → Chart of accounts**).
- Add customers and vendors (**Contacts**).
- Invite the other founders (**Settings → Members and roles**).
- Add a passkey (**Settings → Profile and security**).

A step your roles cannot see is left out. Opening balances, bank feeds, employees and importing
from the workbook join the list as they arrive.

### Read the plans on Home

- **Cash ahead** is the lowest bank balance of the next 60 days and its day, and says when it falls below the buffer.
  Select it to open **Cash ahead** ([Cash ahead](cash-flow.md)).
- **Cash runs out** is the month cash first falls below zero in the latest forecast you recorded,
  in its first scenario (usually Base). It leaves out the funding the forecast plans, because none
  of it is in the bank yet, and says so; the month with the funding is shown beside it. **Not within
  the forecast** means cash lasts for all the months the forecast covers.
- **Revenue against budget** and **Costs against budget** (Expert mode) compare the financial year
  so far, to the end of the month the tiles show, with the year's approved budget for the same
  months. Each says how far apart they are as a percentage of the budget, and whether that is
  **favourable**: revenue above its budget, or costs below theirs.
- A tile without a figure shows **—** and says why, such as "No approved budget for the year from
  1 Jan 2027" or "No forecast is recorded yet".

The figures are the books' and the plans' as they stand when you open Home. A budget counts once it
is approved; a forecast once it is recorded.

### Find anything with search

1. Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
2. Type part of a page's name or a command, such as "dark".
3. Use the arrow keys to pick a result, then press **Enter**. **Esc** closes the search.

Results marked **Expert** belong to Expert mode.

To find a record, such as an invoice by its number or a document by its amount, choose **Search the
records for …** at the end of the list: see [Search the records](search.md).

### Follow long work

Some work runs in the background so you can keep working, such as an accountant export or **Run now** on **Checks**.
Select **Operations** (the hourglass in the header) to follow it; the header counts the work still running.

- **Running** work shows a moving bar. Fyniq does not yet know how far along it is, so the bar does not fill. The list
  is read again every 5 seconds while anything runs.
- **Done** work links to what it made: **Download** for an export, for 24 hours after it is ready, and **See the
  checks** for a run of the checks.
- **Failed** work says why when it knows, and how to ask again.

You see only the work you asked for. An export also sends you a notification when it is ready.

### Copy and paste amounts

Amounts copied from a page, a table or a report reach the clipboard as plain numbers, ready for a
spreadsheet: −1,234.56 and (1,234.56) become -1234.56, and 1,090.00 becomes 1090.00, with the rest of
what you copied as it was.

Pasting an amount into an amount field reads it the way you copied it: (1,234.56), S$1,234.56,
−1,234.56 or SGD 1,234.56, or the way your computer writes numbers. The field shows the amount it
read, such as -1234.56, so you can check it before you save.

### Change how Fyniq looks

Select **Appearance** in the header to change the theme, high contrast, density and motion. See
[Change how Fyniq looks](appearance.md).

### Settings

Select **Settings** at the foot of the sidebar (on a phone, under **More**). It lists your own
settings first, such as [Your profile and security](profile-and-security.md), then the
organisation's settings your roles let you see. Most of the organisation's settings are in Expert
mode: in Basic mode, Settings says how many more there are and offers to switch.

### See which version you are on

Select **Help** (the question mark in the header), then **About Fyniq**. It shows the version, the commit (the git
SHA the services were built from), when they were built and, for a published release, its tag. Quote the version and
the commit when you report a problem. Every exported report names the same version and commit in its footer: the last
line of a CSV or a spreadsheet, and the footer of a PDF.

### Sign out

Select **Sign out** in the header. Fyniq ends your session in this browser and returns you to the
sign-in page.

## Common problems

- **"The API is not running."** Fyniq cannot reach its server. Try again in a minute; if you run
  Fyniq on your own computer, start it with `make dev`.
- **"You are not a member of an organisation yet."** You are signed in, but no organisation has
  invited you. Ask an Owner or Administrator to invite you
  ([Invite people and give them roles](members-and-roles.md)).

## Related

- [Read the start-up KPIs](kpis.md)
- [Change how Fyniq looks](appearance.md)
- [Sign in to Fyniq](sign-in.md)
- [Your profile and security](profile-and-security.md)
