---
key: members.remove
type: task
title: Remove someone from the company, or give an external person a new end date
summary: When an external person's roles end, giving a role again with a new end date, and removing a member with another Owner's approval
sources:
  - docs/user-guide/members-and-roles.md sha256:a5ef440ad5c2de54990e7d895bc9ba13f9932be7b21882574ec8311478762aba
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-02
aliases:
  - remove a user
  - remove member
  - offboard
  - auditor access ended
  - roles ended
  - external user expired
  - delete a user
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

(src: docs/user-guide/members-and-roles.md#remove-someone)
