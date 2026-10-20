---
key: settings.team-mode
type: concept
title: Team mode: how strictly duties are separated
summary: Founder and Standard mode, which roles conflict, how a switch is asked for and approved, and what Standard mode blocks
sources:
  - docs/user-guide/team-mode.md sha256:bf1b189576c83e66b7c8114744d80a29c3f2ee8d961f34513110790508e80a0e
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - founder mode
  - standard mode
  - enterprise mode
  - segregation of duties
  - conflicting roles
  - SoD
---

# Team mode: how strictly duties are separated

## What the modes are

- The team mode decides whether one person may hold roles that conflict, such as giving people roles and also posting or approving the books.
- **Founder mode** is where every company starts, for a small team of founders: conflicting roles are allowed because another founder approves every high-risk change, such as a role or a setting ([[approvals.decide]]).
- **Standard mode**, for a team with finance staff, blocks conflicting roles unless someone gives a reason the person keeps them and another person approves it.
- **Enterprise mode** is not available yet.

(src: docs/user-guide/team-mode.md#what-it-is)

## Which roles conflict

- The Administrator role together with any role that posts, approves or releases money: Owner, Controller, Accountant, Bookkeeper, AR specialist, Payment releaser or Approver ([[members.roles]]).
- The Owner role on its own is not a conflict: every change an Owner makes to people, roles or approval policies needs another person's approval.
- **Settings → Team mode** lists each member whose roles conflict, in every mode.

(src: docs/user-guide/team-mode.md#which-roles-conflict)

## Switching

- **Settings → Team mode**, choose the mode; for Standard mode end one of each listed member's roles or give the reason they keep both, and withdraw any invitation whose roles conflict.
- Confirm it's you; Fyniq shows **Waiting for approval** and the mode has not changed yet.
- An Administrator asks; an Owner who did not ask approves it.

(src: docs/user-guide/team-mode.md#switch-the-team-mode)

## After a switch

- Each reason given is kept with that mode and shown in **Settings → Team mode**.
- Changes still waiting for approval are cancelled, because they were asked for under the old mode; ask for them again.
- After a later switch the conflicts are listed again and need new reasons.

(src: docs/user-guide/team-mode.md#what-happens-when-the-mode-changes)

## In Standard mode

- Giving someone a role that makes a conflict needs a reason, which the person approving the role change accepts too ([[members.invite]]).
- An invitation cannot give roles that conflict: invite with one role, then give the other with a reason once they join.
- Someone whose roles conflict, with no reason accepted for them, cannot post journals.

(src: docs/user-guide/team-mode.md#in-standard-mode)

## Common problems

- **Conflicts block the switch**: the message lists each member; end one of their roles, or give a reason for each.
- **The roles would conflict**: give a reason, or choose a role that does not conflict.
- **That team mode is not available**: Enterprise mode needs features Fyniq does not have yet; use Standard mode.

(src: docs/user-guide/team-mode.md#common-problems)
