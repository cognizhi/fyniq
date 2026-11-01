---
key: home.overview
title: The Home page and finding your way around
mode: basic
roles: [all]
requirements: [FR-UX-01, FR-UX-29, FR-UX-31, FR-UX-40, FR-KPI-01, FR-KPI-03]
status: draft            # written with the app shell's first slice (P1); reviewed when the shell's screens are built
---

# The Home page and finding your way around

## What it is

**Home** is where Fyniq opens once you are signed in. It says which organisation you are signed in
to, which currency its books are kept in, and its team mode. The pages of your workspace appear as
each module arrives; you only ever see the pages your roles allow.

- **The pulse card** shows the cash in the bank at the end of this month, the **runway** (how many
  months that cash lasts at the average net burn of the last three months, or **Not burning** when
  those months brought in more cash than they used) and this month's **net burn**.
- **The tiles** show what customers owe the company and what it owes. In Expert mode they add the
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
read the reports. Revenue and costs against the budget, the books' health, unbilled time and GST
due join them with their modules.

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

### Find anything with search

1. Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
2. Type part of a page's name or a command, such as "dark".
3. Use the arrow keys to pick a result, then press **Enter**. **Esc** closes the search.

Results marked **Expert** belong to Expert mode.

### Change how Fyniq looks

Select **Appearance** in the header to change the theme, high contrast, density and motion. See
[Change how Fyniq looks](appearance.md).

### Settings

Select **Settings** at the foot of the sidebar (on a phone, under **More**). It lists your own
settings first, such as [Your profile and security](profile-and-security.md), then the
organisation's settings your roles let you see. Most of the organisation's settings are in Expert
mode: in Basic mode, Settings says how many more there are and offers to switch.

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
