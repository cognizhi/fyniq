---
key: members.invite
type: task
title: Invite someone, or join with an invitation
summary: Create a one-time invitation with roles, give the code in person, withdraw it if unused, and join a company with a code
sources:
  - docs/user-guide/members-and-roles.md sha256:8cb6790e7da388ded2d653e12232bbc36332c73ed25f845a8cb4a396279cca5b
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

- **Settings → Members**, then **Invite**: the person's name and their roles ([[members.roles]] explains roles).
- For someone from outside the company, such as an auditor, set the date their roles end; the Owner role never ends.
- **Create invitation** shows the code once: give it in person or on a call. It works once and stops working after 7 days.
- **Withdraw** an unused invitation and its code stops working at once.

(src: docs/user-guide/members-and-roles.md#invite-someone)

## After the second member joins

- An invitation, like adding or ending a role, waits for another Owner's approval ([[approvals.decide]]).
- Its code is shown to you at once, and works once the invitation is approved.

(src: docs/user-guide/members-and-roles.md#change-someones-roles)

## Join

- Sign in, then on **Joining by invitation** enter your name and the code.
- You join with the invitation's roles and work in the company from your next page.
- A wrong, used, withdrawn or expired code is refused and nothing changes: ask for a new invitation.

(src: docs/user-guide/members-and-roles.md#join-with-an-invitation)
