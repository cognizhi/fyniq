---
key: settings.team-mode
title: Choose how strictly duties are separated
mode: basic
roles: [owner, admin, controller, accountant, payment_releaser, auditor, dpo]
requirements: [FR-ADM-04, SOD-6, FM-1, FM-8]
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
- In Standard mode a person whose roles conflict, with no override, also cannot approve or reject a
  request about money (a journal, a bill, a payment batch), release a payment batch, sign off a bank
  reconciliation or close a month.

## Switch the team mode

1. Open **Settings → Team mode** (Expert mode) and choose the mode.
2. For Standard mode, deal with each conflict listed: end one of the member's roles, or give the
   reason they keep both. Withdraw any invitation whose roles conflict.
3. Select **Ask to switch**, and confirm it's you with your passkey or authenticator app when
   Fyniq asks.
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

## Break-glass

Only in Founder mode, when the other founder can't be reached (on a flight, or ill) and a payment can't
wait for them.

- An Owner starts break-glass with a reason. For 24 hours they may release payment batches that wait
  for approval without a second person.
- It releases up to the break-glass payment cap in all: S$20,000 by default
  ([Organisation settings](organisation-settings.md#break-glass-payment-cap)).
- It never repays a director, never pays the Owner using it, and never pays a bank account that
  changed in the last 30 days.
- Nothing else changes: every other high-risk change still waits for another person.
- Every Owner and the Accountant get an email at once, and every page shows a red banner while it is
  on.

### Start break-glass

1. Open **Break-glass**, from the banner or from **Settings → Team mode**.
2. Say who is unreachable and what must be paid.
3. Select **Start break-glass**, and confirm it's you with your passkey or authenticator app.

You can't start another break-glass until the last one is reviewed, so it can't be extended or
renewed.

### Release a batch under break-glass

1. Prepare the batch as usual ([Pay suppliers and claims in a batch](payment-batches.md)).
2. On the batch, select **Release under break-glass**. You need the Payment releaser role as well.
3. Pay it in the bank as you would any batch.

A request waiting for the batch's approval is cancelled: the break-glass approved the batch instead.

### End and review a break-glass

- An Owner may select **End now** before the 24 hours are up, for example when the other founder is
  back.
- Once it has ended, another Owner reviews it on **Break-glass**: why it was used and each batch it
  released. The Owner who used it never reviews it.
- Its review is due 7 days after it ends. From then, the Accountant may review it instead, if the
  other founder is still away.
- Still unreviewed after that, Checks shows it as cannot post until fixed, and the month can't be
  closed until it is reviewed.

## Common problems

- **Conflicts block the switch**: the message lists each member. End one of their roles, or give a
  reason for each.
- **The roles would conflict**: give a reason, or choose a role that does not conflict.
- **That team mode is not available**: Enterprise mode needs features Fyniq does not have yet. Use
  Standard mode.
- **An earlier break-glass waits for its review**: ask another Owner to review it, or the Accountant
  once its review is due.
- **Its bank account came into force in the last 30 days**: take that payment out of the batch;
  another founder approves it later.
