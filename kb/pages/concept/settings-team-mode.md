---
key: settings.team-mode
type: concept
title: Team mode: how strictly duties are separated
summary: Founder and Standard mode, which roles conflict, how a switch is asked for and approved, what Standard mode blocks, and break-glass for a founder alone
sources:
  - docs/user-guide/team-mode.md sha256:4416685817b32d0b6134dd2b849c24c09d465325d12f6f239713a8116d7b2276
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - founder mode
  - standard mode
  - enterprise mode
  - segregation of duties
  - conflicting roles
  - SoD
  - break-glass
  - emergency payment
  - other founder unreachable
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
- In Standard mode a person whose roles conflict, with no override, also cannot approve or reject a request about money (a journal, a bill, a payment batch), release a payment batch, sign off a bank reconciliation or close a month.

(src: docs/user-guide/team-mode.md#which-roles-conflict)

## Switching

- **Settings → Team mode** (Expert mode), choose the mode; for Standard mode end one of each listed member's roles or give the reason they keep both, and withdraw any invitation whose roles conflict.
- **Ask to switch** and confirm it's you if asked; Fyniq shows **Waiting for approval** and the mode has not changed yet.
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

## Break-glass

- Founder mode only, when the other founder can't be reached and a payment can't wait: an Owner starts break-glass with a reason and, for 24 hours, releases payment batches waiting for approval without a second person ([[purchases.payment-batches]]).
- It releases up to the break-glass payment cap in all, S$20,000 by default ([[settings.organisation]]).
- Never: repaying a director, paying the Owner using it, or paying a bank account that changed in the last 30 days.
- Nothing else changes: every other high-risk change still waits for another person.
- Every Owner and the Accountant are emailed at once; every page shows a red banner while it is on.

(src: docs/user-guide/team-mode.md#break-glass)

## Start, release, end and review

- Start: open **Break-glass** from the banner or **Settings → Team mode**, say who is unreachable and what must be paid, select **Start break-glass** and confirm it's you. Another can't start until the last one is reviewed, so it can't be extended or renewed.
- Release: prepare the batch as usual, then **Release under break-glass** on it (the Payment releaser role is needed as well); a request waiting for its approval is cancelled.
- End: an Owner may select **End now** before the 24 hours are up.
- Review: once it has ended, another Owner reviews why it was used and each batch it released; never the Owner who used it. Its review is due 7 days after it ends; from then the Accountant may review it instead. Still unreviewed, Checks shows it as cannot post until fixed and the month can't be closed ([[approvals.decide]]).

(src: docs/user-guide/team-mode.md#start-break-glass)
(src: docs/user-guide/team-mode.md#release-a-batch-under-break-glass)
(src: docs/user-guide/team-mode.md#end-and-review-a-break-glass)

## Common problems

- **Conflicts block the switch**: the message lists each member; end one of their roles, or give a reason for each.
- **The roles would conflict**: give a reason, or choose a role that does not conflict.
- **That team mode is not available**: Enterprise mode needs features Fyniq does not have yet; use Standard mode.
- **An earlier break-glass waits for its review**: ask another Owner to review it, or the Accountant once its review is due.
- **Its bank account came into force in the last 30 days**: take that payment out of the batch; another founder approves it later.

(src: docs/user-guide/team-mode.md#common-problems)
