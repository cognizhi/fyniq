---
key: members.remove
type: task
title: Remove someone from the company, or give an external person a new end date
summary: When an external person's roles end, removing a member with another Owner's approval, and succeeding an Owner who is permanently unavailable
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
  - remove a user
  - remove member
  - offboard
  - auditor access ended
  - roles ended
  - external user expired
  - delete a user
  - owner succession
  - owner left the company
  - co-founder died
  - remove an owner
---

# Remove someone from the company, or give an external person a new end date

## When an external person's roles end

- A role given with an end date, such as an auditor's, stops working by itself at the end of that day.
- The person stays a member with nothing to do: **Settings → Members and roles** shows **Roles ended**.
- The check "No external user stays a member past their end date" blocks the month's close until someone acts ([[checks.overview]]).
- Either give them a role again with a new end date (**Change roles**, then **Add role**), or remove them.

(src: docs/user-guide/members-and-roles.md#when-an-external-persons-roles-end)

## Remove someone

- **Settings → Members and roles**, **Change roles** next to the person, then **Remove from the company**, and confirm.
- Every role they hold ends, ended ones too, and their next page no longer opens the company.
- The last Owner cannot be removed: the company always keeps an Owner.
- Before the second person joins, the Owner removes someone at once; after that, removing someone waits for another Owner's approval, as a role change does ([[approvals.decide]]).
- A person who was removed can be invited again later, and joins with the new invitation's roles ([[members.invite]]).
- Every Owner is told, at once and by email, when someone becomes an Owner, stops being one, or an Owner is removed.

(src: docs/user-guide/members-and-roles.md#remove-someone)

## When an Owner is permanently unavailable

- When another Owner has left, cannot act or has died, removing them would wait for their own approval: a succession removes them instead.
- **Start a succession** next to the Owner, say why, and choose what it rests on: a **board resolution**, or the external accountant's recorded confirmation; you may name someone to become an Owner, so the company keeps two.
- Confirm it's you; the Owner and every other Owner are told at once, by email. Attach the file under **Owner successions** (**Attach the evidence**).
- For **7 days** the Owner may **object**, which ends it, and you may **withdraw** it; only one succession of the same Owner waits at a time.
- After the 7 days, a Super admin, any one, you included, selects **Complete it, as a Super admin**: the named person becomes an Owner and the outgoing Owner's membership ends; their records and the audit trail stay. It needs the file attached.

(src: docs/user-guide/members-and-roles.md#when-an-owner-is-permanently-unavailable)

## Confirm a member's recovery

- A member who lost every authenticator gets their account back with one of their recovery codes and the confirmation of an Owner or Administrator; never an email or a support request ([[auth.sign-in]]).
- Speak to them first, in person or on a call made to a number already known; then open the member, **Confirm the recovery**, confirm it's you, and read the confirmation to them: shown once, it works for 24 hours.
- Nobody confirms their own recovery; the member and every Owner are told; after recovering, the member cools off for 24 hours, when approving, releasing payments, changing bank details, roles or sign-ins, break-glass and confirming others' recoveries wait.

(src: docs/user-guide/members-and-roles.md#confirm-a-members-recovery)
