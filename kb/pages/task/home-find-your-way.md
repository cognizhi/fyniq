---
key: home.overview
type: task
title: The Home page, search and commands, Settings, and signing out
summary: What Home shows (pulse card, tiles, charts, what waits, set-up steps), Search and commands (Ctrl+K), Settings, and signing out
sources:
  - docs/user-guide/home.md sha256:ccc0277c2af42fe25b3cc9a37f9063a8f7e08aa82b8fc1870907fdb1ad76929e
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
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
---

# The Home page, search and commands, Settings, and signing out

## What Home shows

- Home is where Fyniq opens after sign-in.
- It names the organisation the person is signed in to, the currency its books are kept in, and its team mode.
- **Waiting for you** counts requests waiting for the person's approval ([[approvals.decide]]) and high-risk actions they can review, with any overdue; it also counts the automatic reversals waiting for a month to be opened, which open **Journals** ([[accounting.journals]]).
- **Set up the company** lists the first steps, **Done** or **To do**, with progress, and goes once all are done.
- Workspace pages appear as each module arrives; a person only sees the pages their roles allow.
- The pulse card, the tiles and the charts appear once the books have a month and the person's roles let them read the reports; see the next section.
- Without a session Fyniq opens sign-in ([[auth.sign-in]]); a new installation opens setup ([[installation.setup]]).

(src: docs/user-guide/home.md#what-it-is) (src: docs/user-guide/home.md#before-you-start)

## The pulse card, tiles and charts

- **The pulse card**: the cash in the bank at the end of this month; the **runway**, how many months that cash lasts at the average net burn of the last three months, or **Not burning** when those months brought in more cash than they used; and this month's **net burn**.
- **The tiles**: what customers owe the company and what it owes. Expert mode adds the month's revenue, the gross margin and the net profit for the financial year so far.
- Expert mode also draws three charts: revenue, costs and profit by month; cash by month; and costs by function, this month beside the last.
- **ⓘ** beside a figure says how it is worked out, the dates it reads, its source and its definition's version; selecting the figure opens the report behind it. A figure the books cannot give shows **—** with the reason, never 0.
- **All KPIs** opens the start-up KPIs ([[reports.kpis]]). Revenue and costs against the budget, the books' health, unbilled time and GST due join with their modules.

(src: docs/user-guide/home.md#what-it-is)

## Set up the company

- First, give yourself the roles you work in, such as **Controller** and **Accountant**: the Owner role approves but does not keep the books; before a second person joins, the change is made at once ([[members.roles]]).
- Each step opens its page: the legal name, UEN and books' start in **Settings → Organisation** ([[settings.organisation]]); the chart and the bank accounts in **Accounting → Chart of accounts** ([[accounting.chart]]); customers and vendors in **Contacts** ([[contacts.manage]]); inviting the other founders in **Settings → Members and roles** ([[members.invite]]); a passkey in **Settings → Profile and security** ([[preferences.security]]).
- A step the person's roles cannot see is left out; opening balances, bank feeds, employees and the workbook import join as they arrive.

(src: docs/user-guide/home.md#set-up-the-company)

## Find a page or run a command

- Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
- Type part of a page's name or a command, pick a result with the arrow keys and press **Enter**; **Esc** closes it.
- Results marked **Expert** belong to Expert mode.
- **Appearance** in the header changes theme, high contrast, density and motion: [[preferences.appearance]].

(src: docs/user-guide/home.md#find-anything-with-search) (src: docs/user-guide/home.md#change-how-fyniq-looks)

## Settings

- **Settings** sits at the foot of the sidebar; on a phone it is under **More**.
- It lists the person's own settings first, such as their profile and security ([[preferences.security]]), then the organisation's settings their roles let them see.
- Most organisation settings are in Expert mode; in Basic mode Settings says how many more there are and offers to switch.

(src: docs/user-guide/home.md#settings)

## Sign out

- Select **Sign out** in the header: the session in this browser ends and the sign-in page opens.

(src: docs/user-guide/home.md#sign-out)

## Common problems

- "The API is not running.": Fyniq cannot reach its server; try again in a minute, or start it with `make dev` on a local install.
- "You are not a member of an organisation yet.": ask an Owner or Administrator for an invitation ([[members.invite]]).

(src: docs/user-guide/home.md#common-problems)
