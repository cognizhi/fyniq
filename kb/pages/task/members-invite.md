---
key: members.invite
type: task
title: Invite someone, or join with an invitation
summary: Create a one-time invitation with roles, give the code in person, withdraw it if unused, and join a company with a code
sources:
  - docs/user-guide/members-and-roles.md sha256:844f7c14c68abc44b5c06fce47f45aea1f98ef72b45feedb3a8269d800c7bb40
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
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

## Email domains and requests to join

- An Owner claims the company's email domain (such as cognizhi.com) under **Email domains for joining** and confirms it's them; a domain belongs to one organisation only. A Super admin also lets the sign-in provider accept it and take requests ([[settings.sign-in-providers]]); both are needed.
- **Release** stops new requests by a domain; waiting requests stay for an Owner.
- **Requests to join** lists people who signed in with a verified email on a claimed domain and asked; a request gives nothing until an Owner decides. **Approve** makes them a member with the Employee role, after confirming it's them; **Reject** leaves them without an account. Every Owner is told of each request.

(src: docs/user-guide/members-and-roles.md#email-domains-for-joining) (src: docs/user-guide/members-and-roles.md#requests-to-join)
