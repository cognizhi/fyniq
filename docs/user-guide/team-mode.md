---
key: settings.team-mode
title: Choose how strictly duties are separated
mode: basic
roles: [owner, admin, controller, auditor, dpo]
requirements: [FR-ADM-04, SOD-6, FM-1]
---

# Choose how strictly duties are separated

## What it is

The team mode decides whether one person may hold roles that conflict, such as giving people roles
and also posting or approving the books.

- **Founder mode** is where every company starts. It suits a small team of founders. One person may
  hold roles that conflict, because Fyniq adds safeguards: another founder approves every high-risk
  change, such as a role or a setting.
- **Standard mode** suits a team with finance staff. Roles that conflict are blocked, unless someone
  gives a reason the person keeps them and another person approves it.
- **Enterprise mode** is not available yet.

## Which roles conflict

- The Administrator role together with any role that posts, approves or releases money: Owner,
  Controller, Accountant, Bookkeeper, AR specialist, Payment releaser or Approver.
- The Owner role on its own is not a conflict: every change an Owner makes to people, roles or
  approval policies needs another person's approval.
- **Settings → Team mode** lists each member whose roles conflict, in every mode.

## Switch the team mode

1. Open **Settings → Team mode** and choose the mode.
2. For Standard mode, deal with each conflict listed: end one of the member's roles, or give the
   reason they keep both. Withdraw any invitation whose roles conflict.
3. Confirm it's you with your passkey or authenticator app.
4. Fyniq shows **Waiting for approval**. The mode has not changed yet.

An Administrator asks for the switch. An Owner who did not ask for it approves it
([Approve a change](approvals.md)).

## What happens when the mode changes

- Each reason given is kept with that mode and shown in **Settings → Team mode**.
- Changes that were still waiting for approval are cancelled, because they were asked for under the
  old mode. Ask for them again.
- After a later switch, the conflicts are listed again and need new reasons.

## In Standard mode

- Giving someone a role that makes a conflict needs a reason. The person who approves the role
  change accepts the reason too.
- An invitation cannot give roles that conflict. Invite the person with one role, then give the
  other with a reason once they join.
- Someone whose roles conflict, with no reason accepted for them, cannot post journals.

## Common problems

- **Conflicts block the switch**: the message lists each member. End one of their roles, or give a
  reason for each.
- **The roles would conflict**: give a reason, or choose a role that does not conflict.
- **That team mode is not available**: Enterprise mode needs features Fyniq does not have yet. Use
  Standard mode.
