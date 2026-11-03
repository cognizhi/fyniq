---
key: settings.members
title: Invite people and give them roles
mode: basic
roles: [owner, admin, controller, auditor, dpo]
requirements: [FR-ADM-01, FM-1, SEC-06]
---

# Invite people and give them roles

## What it is

**Settings → Members and roles** lists everyone in the company with their roles, the invitations
not yet used, and the role changes waiting for approval. It is in Expert mode. A role decides what a person
can see and do: the Owner approves, the Controller approves and keeps the defaults, the Accountant
prepares and posts journals, the Bookkeeper records, and so on. One person can hold several roles.

## Before the second founder joins

The first founder is the company's Owner. Until a second person joins, the Owner sets up members
and roles alone, because there is nobody yet to approve with:

- Give yourself the roles you work in, such as **Controller** and **Accountant**.
- Invite the other founders with their roles. An invitation made now keeps its roles, however late
  it is accepted.
- Everything you do here is recorded in the audit trail.

Once a second person joins, every role change needs a second approver: it waits until another
Owner approves it (see [Approve a change](approvals.md)). Inviting every founder before the first one
joins saves those approvals.

## Invite someone

1. Open **Settings → Members and roles** and select **Invite**.
2. Enter the person's name and choose their roles. For someone from outside the company, such as an
   auditor, set the date their roles end. The Owner role never ends.
3. Select **Create invitation**, and confirm it's you if Fyniq asks. The code is shown once: copy
   it and give it to the person in person or on a call. It works once and stops working after
   7 days.

Under **Invitations not yet used**, you can **Withdraw** an invitation; its code stops working at
once.

## Join with an invitation

1. Sign in to Fyniq.
2. On **Joining by invitation**, enter your name and the code you were given.
3. You join the company with the roles in the invitation. From your next page you work in it.

A wrong, used, withdrawn or expired code is refused, and nothing changes. Ask for a new invitation.

## Change someone's roles

- In **Settings → Members and roles**, select **Change roles** next to the person. Choose a role
  and select **Add role**, or select **End role** next to one they hold.
- The company always keeps an Owner: make someone else Owner before ending the last Owner's role.
- A change takes effect on the person's next page. A person whose roles conflict shows **Role
  conflict**.
- Once the company has a second member, adding or ending a role, or inviting someone, waits for
  another Owner's approval. An invitation's code is shown to you at once, and works once the
  invitation is approved.
- In Standard mode, a role that conflicts with the person's other roles needs a reason, and an
  invitation cannot give roles that conflict ([Choose how strictly duties are separated](team-mode.md)).

## When an external person's roles end

A role given with an end date, such as an auditor's, stops working by itself at the end of that day.
The person stays a member with nothing to do: **Settings → Members and roles** shows **Roles
ended**, and the check "No external user stays a member past their end date" blocks the month's
close until you act. Either give them a role again with a new end date (**Change roles**, then
**Add role**), or remove them.

## Remove someone

- In **Settings → Members and roles**, select **Change roles** next to the person, then **Remove
  from the company**, and confirm.
- Every role they hold ends, ended ones too, and their next page no longer opens the company.
- The company always keeps an Owner: the last Owner cannot be removed.
- Before the second person joins, the Owner removes someone at once. After that, removing someone
  waits for another Owner's approval, as a role change does.
- A person you removed can be invited again later: they join with the new invitation's roles.
