---
key: settings.people
type: task
title: People and HR: where payroll lives and how its outputs are met
summary: See where payroll lives, the Nemura link's state, how a month's statutory outputs are met, and how a provider's payroll journal becomes a draft
sources:
  - docs/user-guide/settings-people.md sha256:0c5d0f1b1f9ff638a940a1289522b0230133b3b11efae4a1530208925c172964
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - people and hr
  - payroll go-live gate
  - payroll arrangements
  - provider payroll journal
  - payroll import
  - nemura link
  - link health
  - ask nemura to send them again
  - add a passkey to approve pay runs in nemura
  - finance people switch
---

# People and HR: where payroll lives and how its outputs are met

## What it is

- **Settings › People and HR** says where people records, payroll, leave and letters live: in Nemura, in an external system, or nowhere (payroll is posted by manual journal).
- It shows the link's state and what Fyniq received from Nemura, and records how each month's statutory payroll outputs are met.

(src: docs/user-guide/settings-people.md#what-it-is)

## Where each feature lives

- Linking Nemura, changing where a feature lives and unlinking need the Owner and a second approver; they are asked for through the API today.
- While payroll lives in Nemura a provider's payroll journal or a file import is not taken: the pay run posts through the link, so it is never posted from two sources.

(src: docs/user-guide/settings-people.md#where-each-feature-lives)

## Link or unlink Nemura

- **Link Nemura** asks to link with what Nemura's link page shows once an Owner there has proposed the link: its organisation ID and web address, the two client IDs, its link key and client key, and an expiry within a year. Nothing is linked by asking: the Owner and a second approver approve it here and another Owner or Administrator approves Nemura's half there.
- **Ask to unlink Nemura** needs a reason and the same two approvers. Work in flight settles first: salary payments not yet settled, payments with an unknown outcome and events waiting for Nemura. While any is left the link stays unlinking and the page counts them; **Finish unlinking** ends it when nothing is left. Both sides keep their data, and a later link starts at a higher epoch.

(src: docs/user-guide/settings-people.md#link-or-unlink-nemura)

## Move where a feature lives

- **Move where a feature lives** asks to move one feature (People records, Payroll, Leave, Letters) to Nemura, an external system or none, from a date today or later, with a reason. The Owner and a second approver approve it; nothing moves by asking. Moving to Nemura needs an active link; moving payroll away does not end the link or touch any posted entry.

(src: docs/user-guide/settings-people.md#move-where-a-feature-lives)

## Link health

- While the link is active, **Link health** shows how it stands, by identity only: how many events Nemura has queued for Fyniq, the highest number Fyniq's inbox holds, and how many events wait to be delivered, fail or were refused. Nothing in it is a payload, a name or a figure.
- A gap is a run of event numbers that Nemura says it delivered and Fyniq never received. The page names the runs and offers **Ask Nemura to send them again**: Nemura sends the current state of what those events were about, never an old payload, and Fyniq applies what it missed and ignores what it holds. Nemura answers at new event numbers, so the numbers asked for never arrive: the page then says they were asked for in the last day and does not ask again; after a day they show as missing again. If Nemura's log is longer than one look reads, the newest part is read and the page says the oldest was left out.
- If Nemura's log cannot be read, the page says why and shows no number it does not know; **Check again** looks once more. It needs People and HR's right.

(src: docs/user-guide/settings-people.md#link-health)

## People who approve pay runs in Nemura

- A pay run approved in Nemura while the apps are linked is signed with a passkey enrolled in Fyniq. Nemura tells Fyniq, signed, which sign-in accounts approve pay runs there: an account and a yes or no, never a role name or an email.
- A person Nemura says approves pay runs, with no passkey in Fyniq, sees a reminder at the top of every page: **Add a passkey to approve pay runs in Nemura**. It gives them no role in Fyniq. A person with no sign-in in Fyniq at all needs an invitation first.
- Fyniq also tells Nemura, for each sign-in account, whether it holds a role in the finance workspace and whether it has a passkey, so Nemura's **Finance | People** switch and its warning before an approval are right. Both are hints, never grants.
- The **Finance | People** switch in Fyniq's header shows only to a person who holds a role in the finance workspace and whom Nemura says is an active member there. It opens Nemura with the sign-in provider the person used.

(src: docs/user-guide/settings-people.md#people-who-approve-pay-runs-in-nemura)

## How the month's payroll outputs are met

- Before a pay run that pays a person can be approved for payment, five things are recorded as evidence: itemised payslips, key employment terms, CPF, SDL and SHG submission and payment, and where they apply the Auto-Inclusion Scheme and IR21.
- Each is met by a named provider, a documented manual process, Nemura (payslips and key employment terms only, while the link is active), or not applicable with the reason (the Auto-Inclusion Scheme and IR21 only).
- Every method but Nemura's needs a file of evidence; an arrangement holds from the month chosen until a later one replaces it and is never changed.
- While one is missing the salary batch cannot be asked for approval and says which ([[payroll.salary-payments]]).

(src: docs/user-guide/settings-people.md#how-the-months-payroll-outputs-are-met)

## A provider's payroll journal

- A provider's journal or a file import, by account and department, becomes a draft journal under **Accounting › Journals** ([[accounting.journals]]); a Controller or Accountant approves it because the provider's own approval cannot be checked.
- It is taken once for each provider, pay period and version; a later version drafts only the difference from the version before, once the earlier journal is posted.
- A file may not name a person: pay is kept by account and department.

(src: docs/user-guide/settings-people.md#a-providers-payroll-journal)
