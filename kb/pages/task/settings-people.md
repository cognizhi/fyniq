---
key: settings.people
type: task
title: People and HR: where payroll lives and how its outputs are met
summary: See where payroll lives, the Nemura link's state, how a month's statutory outputs are met, and how a provider's payroll journal becomes a draft
sources:
  - docs/user-guide/settings-people.md sha256:7a88c19f406040086ac4ceab16fc41769063aee7c6c7a7dc2d37c95c004b3d91
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
