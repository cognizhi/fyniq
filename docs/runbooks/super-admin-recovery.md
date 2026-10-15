# Runbook: recovering a Super admin

**When:** a Super admin cannot sign in: their sign-in account (Google, Zoho) is lost, locked or
compromised. **Requirements:** PRD-001 FR-ADM-09, SEC-35, CTL-IAM-07; open question 36 (decided 14 Oct
2026: two Super admins, one per founder; each keeps printed recovery codes offline; the deployment
recovery needs both people and is audited).

Every recovery needs **two independent proofs** (SEC-35). Fyniq offers two ways:

| Way | Proof 1 | Proof 2 | Use it when |
|---|---|---|---|
| **A. Recovery code with confirmation** | One of the Super admin's ten printed recovery codes | The other Super admin's confirmation, made in Fyniq with step-up | The other Super admin can sign in |
| **B. Deployment recovery** | Access to the deployment's migrator credential | A second named operator present for the whole procedure | Nobody else can confirm, or the printed codes are lost too |

Both ways end the same: the sign-in the person uses to recover **replaces all their other sign-ins**,
their old recovery codes stop working and they receive ten new ones, and the tokens of every replaced
sign-in are revoked (AI-38). Everything is recorded in the installation's audit trail.

## Before you start

1. **Confirm who is asking.** Speak to the person live, by video or in person, not by email or chat
   alone. A request to recover someone else's access is the classic takeover; if in doubt, stop.
2. **Decide why the old sign-in is gone.** If it may be compromised, say so in the record: the
   recovery revokes its tokens, but the person should also secure the provider account.
3. **Open a record** (the change log or an incident, CTL-CHG): who asked, who confirmed, the way
   (A or B), the time. Attach it to the weekly review (FM-3).

## Way A: recovery code with the other Super admin's confirmation

1. The Super admin who lost access signs in to Fyniq with the **new** sign-in they will keep from now
   on. They are not a Super admin under that sign-in yet.
2. The **other** Super admin signs in, passes step-up, and confirms the recovery
   (`POST /v1/super-admins/{user_id}/recovery-confirmations`; the Super admins screen arrives with the
   SignIn port). Fyniq shows a one-time confirmation code, valid **24 hours**.
3. The other Super admin gives the confirmation code to the person **by a channel they trust** (in
   person, a call), never in a shared channel or a ticket.
4. The person enters one of their printed recovery codes and the confirmation code
   (`POST /v1/super-admins/recovery`). Fyniq shows **ten new recovery codes once**: print them, keep
   them offline, and destroy the old sheet.
5. Record the outcome. The trail shows `super_admin.recovery_confirmed` (by the confirming Super
   admin) and `super_admin.recovered` (`via: recovery_confirmation`), plus one
   `oauth.tokens.revoke_requested` per revoked subject.

A confirmation replaces an unused one; nobody can confirm their own recovery.

## Way B: the deployment recovery (two people)

Both operators are present for every step; the command records **both names**. Locally the
operators are whoever runs the stack. In the cloud they are the two founders (open question 36).

1. Find the Super admin's user ID (the `super_admin.added` event in the installation's trail, or the
   setup result).
2. With the **migrator credential** (the deployment's `fyniq_migrator` role), one operator runs, while
   the other watches:

   ```bash
   # locally
   make super-admin-recovery USER_ID=<user id> OPERATORS="<first operator>, <second operator>"
   # in the cloud: the migrate image's command, as a one-off job with the migrator credential
   migrate super-admin-recovery <user id> "<first operator>, <second operator>"
   ```

   [verify when p0-infra-opentofu lands: how the migrate job is started in the cloud, and who holds
   the migrator credential.] The command refuses fewer or more than two names, or the same name twice.
3. The command prints a one-time code, valid **24 hours**. It is shown once and never logged; the
   trail records `super_admin.deployment_recovery_issued` with both operators. A new code replaces an
   unused one.
4. The Super admin signs in to Fyniq with their new sign-in and enters the code as the confirmation
   code (a printed recovery code is optional with it). Fyniq shows ten new recovery codes once.
5. Record the outcome. The trail shows `super_admin.recovered` (`via: deployment_recovery`).

## Afterwards

- **Tell everyone it concerns.** FR-ADM-09 requires that every recovery notifies every Super admin
  and Owner. Fyniq does not send these notifications yet (they arrive with notifications, P1), so the
  operators tell every Super admin and Owner themselves and note it in the record.
- **No cooling-off yet.** SEC-35's 24-hour cooling-off period (no approvals, payments, role or
  sign-in changes) arrives with approvals in P1; until then, review the recovered account's activity
  in the trail for the next 24 hours.
- **Sessions:** first-party sessions arrive with the SignIn port; until then, revoking the replaced
  sign-ins' tokens is the whole of "revoke all sessions and tokens".
- **If the nightly check fails** (`superadmins.verify`: no active Super admin can sign in;
  `control.check.failed` with CTL-IAM-07 in the installation's trail), follow way B.

## What Fyniq enforces (so you do not have to)

- At most two Super admins in P0, and never none; a Super admin is removed, never deleted.
- Every code is stored only as its SHA-256, used once, and refused with one answer whatever the
  reason (wrong, used, expired, or another person's).
- Inviting, confirming and replacing codes need a Super admin signed in to Fyniq who passed step-up
  in the last 5 minutes; an OAuth client or the AI is always refused.
- Every change has its event in the installation's trail, or the database refuses it.
