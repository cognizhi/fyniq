---
key: members.roles
type: concept
title: Members, roles and the first founder's setup
summary: What roles do, why the Owner sets up members alone until a second person joins, and why role changes then need a second approver
sources:
  - docs/user-guide/members-and-roles.md sha256:e65c55c44c60696fb13c0d8eb273e23990e0e1a40c8dc096d908e26f9f54764b
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

## Narrow an Approver to departments

- When giving the **Approver** role, **Departments this Approver acts for** lists the organisation's department values: tick the ones the role reaches.
- Their views and approvals that Fyniq limits by department then reach only those departments' records, lines and budgets; a report's totals count only those lines.
- An Approver role given with no department reaches none of them, so tick at least one; the choice is fixed when the role is given, so to change it end the role and give it again.
- The people's list shows it beside the role, such as "Approver for Engineering"; the assistant and connected apps acting for that person are limited the same way.
- A person who holds any other role besides Employee is not narrowed by this: that role's own rights decide.

(src: docs/user-guide/members-and-roles.md#narrow-an-approver-to-departments)

