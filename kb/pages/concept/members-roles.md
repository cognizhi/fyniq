---
key: members.roles
type: concept
title: Members, roles and the first founder's setup
summary: What roles do, why the Owner sets up members alone until a second person joins, and why role changes then need a second approver
sources:
  - docs/user-guide/members-and-roles.md sha256:af4746970713ccd3bb7a35fe0e092599a2cf734948001eba8e5f132ccea9bfa5
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - permissions
  - give someone access
  - founder roles
  - second approver
  - who can do what in the company
  - permissions
---

# Members, roles and the first founder's setup

## What roles do

- **Settings → Members and roles** (Expert mode) lists everyone in the company with their roles, the invitations not yet used, and role changes waiting for approval.
- A role decides what a person can see and do; one person can hold several roles.
- The Owner approves; the Controller approves and keeps the defaults; the Accountant prepares and posts journals; the Bookkeeper records.

(src: docs/user-guide/members-and-roles.md#what-it-is)

## Before the second founder joins

- The first founder is the Owner, and sets up members and roles alone until a second person joins: there is nobody yet to approve with.
- Give yourself the roles you work in, such as **Controller** and **Accountant**, and invite the other founders with theirs ([[members.invite]]).
- An invitation made then keeps its roles, however late it is accepted; everything is recorded in the audit trail.
- Once a second person joins, every role change needs a second approver: it waits until another Owner approves it ([[approvals.decide]]). Inviting every founder before the first one joins saves those approvals.

(src: docs/user-guide/members-and-roles.md#before-the-second-founder-joins)

## Changing roles

- **Change roles** next to the person, then choose a role and **Add role**, or **End role** next to one they hold; it takes effect on their next page.
- A person whose roles conflict shows **Role conflict**.
- The company always keeps an Owner: make someone else Owner before ending the last Owner's role.
- Once the company has a second member, adding or ending a role, or inviting someone, waits for another Owner's approval.
- In Standard mode, a role that conflicts with the person's other roles needs a reason, and an invitation cannot give roles that conflict ([[settings.team-mode]]).

(src: docs/user-guide/members-and-roles.md#change-someones-roles)

## Who has signed in and who has a passkey

- Beside each person, the list says when they last signed in, or that they have not yet, and how many passkeys they hold.
- A person with no passkey confirms it's you by signing in again each time, so ask them to add one under Profile and security.
- An Owner and an Administrator also see each person's sign-in email there; the Auditor and the DPO, who read the list, do not.

(src: docs/user-guide/members-and-roles.md#who-has-signed-in-and-who-has-a-passkey)

## Narrow an Approver to departments

- When giving the **Approver** role, or inviting someone with it, **Departments this Approver acts for** lists the organisation's department values: tick the ones the role reaches; the other Owner approves them with the invitation.
- Their views and approvals that Fyniq limits by department then reach only those departments' records, lines and budgets; the profit and loss they open counts only those lines (the balance sheet, cash flow, trial balance, budget comparatives and exports are the whole company's, so they are not shown to them).
- A department's budget holder sees only their department's lines of a budget, with those lines' totals and their department's Budget vs Actual, and plans by saving their own lines: the draft's other lines stay as they were. Drafting, copying, importing and asking for approval, the forecasts and the planning summary are not theirs.
- An Approver role given with no department reaches none of a budget or a report, so tick at least one; the choice is fixed when the role is given, so to change it end the role and give it again.
- The people's list shows it beside the role, such as "Approver for Engineering"; the assistant and connected apps acting for that person are limited the same way.
- A person who holds any other role that already reaches the same records, such as an Accountant, is not narrowed by this: that role's own rights decide.
- An Approver narrowed to departments also reaches only their departments' bills, invoices and expense claims and the requests for them: a record is in a department when its lines carry that department's values, and one whose lines carry none is in no department. They are told of a request, and may sign it, only when it is theirs; the company's payables and receivables, bill reminders and the files of a bill or invoice are not shown to them, and their own claims stay theirs.

(src: docs/user-guide/members-and-roles.md#narrow-an-approver-to-departments)

