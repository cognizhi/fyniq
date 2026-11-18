---
key: home.overview
type: task
title: The Home page, search and commands, long work, Settings, the version, and signing out
summary: Home (pulse, tiles, plans, your work, deadlines, set-up), Search and commands (Ctrl+K), Operations, Settings, the version (Help › About), signing out
sources:
  - docs/user-guide/home.md sha256:409653610a991d47626cac0fb91ceb0047d4f6131cdf97ab2afdfcf4fccbd9af
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-18
aliases:
  - your work
  - what do I need to do today
  - bank lines to categorise
  - overdue invoices on home
  - upcoming deadlines
  - what is due soon
  - copy amounts to excel
  - paste an amount
  - command palette
  - quick search
  - home page
  - log out
  - where is everything
  - settings
  - preferences
  - getting started
  - set-up checklist
  - dashboard
  - how much cash do we have
  - runway
  - burn
  - when do we run out of cash
  - budget vs actual on home
  - cash out month
  - what version is this
  - about fyniq
  - git sha
  - build number
  - operations
  - background jobs
  - is my export ready
  - long running work
---

# The Home page, search and commands, long work, Settings, the version, and signing out

## What Home shows

- Home is where Fyniq opens after sign-in.
- It names the organisation the person is signed in to, the currency its books are kept in, and its team mode.
- **Waiting for you** counts requests waiting for the person's approval ([[approvals.decide]]) and high-risk actions they can review, with any overdue; it also counts the automatic reversals waiting for a month to be opened, which open **Journals** ([[accounting.journals]]).
- **Your work** counts what waits in the person's areas, by their roles; **Upcoming deadlines** lists the next statutory deadlines (see [Your work and upcoming deadlines](#your-work-and-upcoming-deadlines)).
- **Set up the company** lists the first steps, **Done** or **To do**, with progress, and goes once all are done.
- Workspace pages appear as each module arrives; a person only sees the pages their roles allow.
- The pulse card, the tiles and the charts appear once the books have a month and the person's roles let them read the reports; see the next section.
- Without a session Fyniq opens sign-in ([[auth.sign-in]]); a new installation opens setup ([[installation.setup]]).

(src: docs/user-guide/home.md#what-it-is) (src: docs/user-guide/home.md#before-you-start)

## The pulse card, tiles and charts

- **The pulse card**: the cash in the bank at the end of this month; the **runway**, how many months that cash lasts at the average net burn of the last three months, or **Not burning** when those months brought in more cash than they used; and **books health**, **OK**, **Review** or **Problems**, with how many checks block the close, ask for a review or are due, and how many are not assessed. Selecting it opens the checks ([[checks.overview]]). For someone whose roles do not read the checks, the card shows this month's **net burn** instead.
- **The tiles**: what customers owe the company and what it owes, and this month's net burn when books health is on the pulse card. Expert mode adds the month's revenue, the gross margin and the net profit for the financial year so far.
- Expert mode also draws three charts: revenue, costs and profit by month; cash by month; and costs by function, this month beside the last.
- **ⓘ** beside a figure says how it is worked out, the dates it reads, its source and its definition's version; selecting the figure opens the report behind it. A figure the books cannot give shows **—** with the reason, never 0.
- **All KPIs** opens the start-up KPIs ([[reports.kpis]]). Unbilled time and GST due join with their modules.
- For someone whose roles read the plans, the tiles add the lowest balance of the next 60 days, the month cash runs out in the latest forecast and, in Expert mode, revenue and costs against the budget; see the next section.

(src: docs/user-guide/home.md#what-it-is)

## Your work and upcoming deadlines

- **Your work** shows what the person's roles work through, each row counting a list and opening it.
- **Bookkeeper**: bank lines to categorise, and draft invoices and bills to finish.
- **AR specialist**: draft invoices to finish, invoices overdue, receipts to apply (the customer statements show each one) and quotes waiting for an answer.
- **AP specialist**: draft bills to finish, bills overdue, bills to pay, payment batches being prepared, approved claims to pay and bank-detail changes waiting for a second person.
- **Payment releaser**: batches waiting for release and payments the bank rejected, then each bank account's balance.
- The Administrator lands on **Settings**, and the Company secretary and the DPO on **Compliance**, where their rows come first: the Administrator's people asking to join, invitations waiting to be accepted, role changes waiting for a second approver and integrations failing; the Company secretary's items to see to, register updates waiting for approval and the upcoming deadlines; the DPO's data requests to answer and breaches still open.
- The Auditor lands on **Reports**, where **Your engagement** says until when the read-only access lasts and opens the journals, the evidence and the audit trail.
- A row whose list the person's roles cannot read is left out rather than shown as nothing to do; with several roles each row shows once.
- The Owner, the Controller, the Accountant and the Tax agent read the same work from the pulse card, the tiles and the Inbox, so the card does not show for them alone.
- **Upcoming deadlines** lists the next five statutory deadlines from the calendar, those **Overdue** or **Due soon** first, each with its date; **The statutory calendar** opens it when Compliance is one of the person's areas ([[compliance.calendar]]).

(src: docs/user-guide/home.md#your-work) (src: docs/user-guide/home.md#upcoming-deadlines)

## The plans on Home

- **Cash ahead**: the lowest bank balance of the next 60 days and its day, saying when it falls below the buffer; selecting it opens Cash ahead ([[planning.cash]]).
- **Cash runs out**: the month cash first falls below zero in the latest recorded forecast ([[planning.forecast]]), in its first scenario (usually Base). It leaves out the funding the forecast plans, because none of it is in the bank yet, and says so; the month with the funding is shown beside it. **Not within the forecast** means cash lasts for all the months the forecast covers.
- **Revenue against budget** and **Costs against budget** (Expert mode): the financial year so far, to the end of the month the tiles show, against the year's approved budget for the same months, with the gap as a percentage of the budget and whether it is **favourable** (revenue above its budget, or costs below theirs).
- A tile without a figure shows **—** and says why, such as "No approved budget for the year from 1 Jan 2027" or "No forecast is recorded yet".
- The figures are the books' and the plans' as they stand when Home opens; a budget counts once approved, a forecast once recorded.

(src: docs/user-guide/home.md#what-it-is) (src: docs/user-guide/home.md#read-the-plans-on-home)

## Set up the company

- First, give yourself the roles you work in, such as **Controller** and **Accountant**: the Owner role approves but does not keep the books; before a second person joins, the change is made at once ([[members.roles]]).
- Each step opens its page: the legal name, UEN and books' start in **Settings → Organisation** ([[settings.organisation]]); the chart and the bank accounts in **Accounting → Chart of accounts** ([[accounting.chart]]); customers and vendors in **Contacts** ([[contacts.manage]]); inviting the other founders in **Settings → Members and roles** ([[members.invite]]); a passkey in **Settings → Profile and security** ([[preferences.security]]).
- A step the person's roles cannot see is left out; opening balances, bank feeds, employees and the workbook import join as they arrive.

(src: docs/user-guide/home.md#set-up-the-company)

## Find a page or run a command

- Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
- Type part of a page's name or a command, pick a result with the arrow keys and press **Enter**; **Esc** closes it.
- Results marked **Expert** belong to Expert mode.
- **Search the records for …**, at the end of the list, finds a record such as an invoice by its number or a document by its amount: [[shell.search]].
- **Appearance** in the header (on a phone, under **More**) changes theme, high contrast, density and motion: [[preferences.appearance]].

(src: docs/user-guide/home.md#find-anything-with-search) (src: docs/user-guide/home.md#change-how-fyniq-looks)

## Follow long work

- Some work runs in the background so the person can keep working, such as an accountant export or **Run now** on **Checks**.
- **Operations** (the hourglass in the header) lists it; the header counts the work still running.
- **Running** work shows a moving bar that does not fill, because Fyniq does not yet know how far along it is; the list is read again every 5 seconds while anything runs.
- **Done** work links to what it made: **Download** for an export, for 24 hours after it is ready, and **See the checks** for a run of the checks.
- **Failed** work says why when it knows, and how to ask again.
- Each person sees only the work they asked for; an export also sends a notification when it is ready: [[settings.notifications]].
- On a phone, **Operations** is not in the header yet: the export's notification says when it is ready, and **Reports** lists the person's exports under **Accountant exports**.

(src: docs/user-guide/home.md#follow-long-work)

## Copy and paste amounts

- Amounts copied from a page, a table or a report reach the clipboard as plain numbers for a spreadsheet: −1,234.56 and (1,234.56) become -1234.56, 1,090.00 becomes 1090.00, and the rest of what was copied stays as it was.
- Pasting into an amount field reads (1,234.56), S$1,234.56, −1,234.56, SGD 1,234.56 or the computer's own number format; the field shows the amount read, such as -1234.56, to check before saving.

(src: docs/user-guide/home.md#copy-and-paste-amounts)

## Settings

- **Settings** sits at the foot of the sidebar; on a phone it is under **More**.
- It lists the person's own settings first, such as their profile and security ([[preferences.security]]), then the organisation's settings their roles let them see.
- Most organisation settings are in Expert mode; in Basic mode Settings says how many more there are and offers to switch.

(src: docs/user-guide/home.md#settings)

## See which version you are on

- Select **Help** (the question mark in the header; on a phone, under **More**), then **About Fyniq**.
- It shows the version, the commit (the git SHA the services were built from), when they were built and, for a published release, its tag.
- Quote the version and the commit when reporting a problem.
- Every exported report names the same version and commit in its footer: the last line of a CSV or a spreadsheet, and the footer of a PDF.
- The same **Help** menu opens the **User guide**, in Fyniq: [[help.guide]].

(src: docs/user-guide/home.md#see-which-version-you-are-on)

## Sign out

- Select **Sign out** in the header (on a phone, under **More**): the session in this browser ends and the sign-in page opens.

(src: docs/user-guide/home.md#sign-out)

## Common problems

- "The API is not running.": Fyniq cannot reach its server; try again in a minute, or start it with `make dev` on a local install.
- "You are not a member of an organisation yet.": ask an Owner or Administrator for an invitation ([[members.invite]]).

(src: docs/user-guide/home.md#common-problems)
