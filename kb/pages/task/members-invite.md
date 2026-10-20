---
key: members.invite
type: task
title: Invite someone, or join with an invitation
summary: Create a one-time invitation with roles, give the code in person, withdraw it if unused, and join a company with a code
sources:
  - docs/user-guide/members-and-roles.md sha256:f790a365bb33fa449acb8257b213e2d16e211b8f1634c8c8207a4518fb0bed2a
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - invitation code
  - add a user
  - join company
  - external auditor access
---

# Invite someone, or join with an invitation

## Invite

- **Settings → Members and roles**, then **Invite**: the person's name and their roles ([[members.roles]] explains roles).
- For someone from outside the company, such as an auditor, set the date their roles end; the Owner role never ends.
- **Create invitation** (confirming it's you if asked) shows the code once: copy it and give it in person or on a call. It works once and stops working after 7 days.
- Under **Invitations not yet used**, **Withdraw** one and its code stops working at once.

(src: docs/user-guide/members-and-roles.md#invite-someone)

## After the second member joins

- An invitation, like adding or ending a role, waits for another Owner's approval ([[approvals.decide]]).
- Its code is shown to you at once, and works once the invitation is approved.
- In Standard mode an invitation cannot give roles that conflict: invite with one role, then give the other with a reason ([[settings.team-mode]]).

(src: docs/user-guide/members-and-roles.md#change-someones-roles)

## Join

- Sign in, then on **Joining by invitation** enter your name and the code.
- You join with the invitation's roles and work in the company from your next page.
- A wrong, used, withdrawn or expired code is refused and nothing changes: ask for a new invitation.

(src: docs/user-guide/members-and-roles.md#join-with-an-invitation)
