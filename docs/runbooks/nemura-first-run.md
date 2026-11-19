# Runbook: first run of Nemura, and who to call when the pay-day path fails

**When:** a new Nemura installation has no organisation yet (the sign-in page says "Nemura is not set
up yet"), or a pay-day job of the pay-day path has died. **Requirements:** PRD-002 NM-ARC-01, NM-NFR-05,
NM-NFR-03. Nemura has its own database, sign-in and sessions: nothing here needs Fyniq.

## First run: the first organisation and its Owner

1. **Apply the migrations.** Locally, `PEOPLE=yes make up` (the `nemura-migrate` service does it). In
   the cloud, the migrate job of the release.
2. **Print the one-time setup code**, once, on the machine that holds the migrator credential:
   `make people-setup-code` locally, or the `setup-code` command of the migrate role
   (`NEMURA_ROLE=migrate nemura setup-code`) in the cloud. The code is shown once, kept only as a hash,
   and a new command replaces an unused code. Once setup is done the database refuses it.
3. **Hand the code to the person who will be the first Owner, in person or by a call**, never in a
   shared channel or a ticket. It names no organisation.
4. The person opens Nemura, selects **Set up Nemura** on the sign-in page, types the code, the
   organisation's name and their own, picks the sign-in provider and signs in. The code is used once, with
   the account they sign in with: that account becomes the first organisation's Owner. A wrong or used
   code ends at the sign-in page with a message; run step 2 again for a new one.
5. **Check:** the sign-in page no longer offers **Set up Nemura**; the Owner lands on People home with
   the role Owner; the audit trail holds `nemura.org.created`. Record who held the code and when in the
   change log (CTL-CHG).

Everyone else joins with an invitation: the Owner invites, the person chooses **I have an invitation** on
the sign-in page and types the code, which works once and expires.

## A pay-day job died

A job of the pay-day path (its kind starts with `payday.`) that uses up its attempts raises a notice for
each Payroll administrator and Owner on the day, in the worker's next sweep: within an hour of the
failure (NM-NFR-05). Until notifications carry email and in-app delivery, the notice is a `payday.alert`
error line in the worker's log, naming the organisation, the person's ID and the job's kind, never a
name or a figure.

1. Find the lines: `docker compose logs nemura-worker | grep payday.alert` locally; in the cloud, the
   log-based alert on `payday.alert` [live-verify].
2. Find the dead row: its audit event is `nemura.outbox.row.dead`, with the reason; its `last_error` says
   what failed.
3. Fix the cause, then queue the work again from payroll's own screen (it arrives with payroll). Never
   edit the outbox row: it is append-only history.
4. Tell the Payroll administrator and the Owner what happened and when the payment file will be ready, in
   time for the bank's deadline (NM-PAY-03).

## An Owner who cannot sign in: the deployment recovery (two people)

**When:** an Owner's sign-in account (Google, the company's provider) is lost, locked or compromised, and no
other Owner can be asked to approve a replacement. It is Nemura's own, as Fyniq's
(`docs/runbooks/super-admin-recovery.md`, way B); nothing here needs Fyniq. **Requirements:** NM-ARC-01,
NM-ROLE-05; PRD-001 FR-ADM-09, SEC-35.

Both operators are present for every step and the command records **both names** in the organisation's
audit trail. Locally the operators are whoever runs the stack; in the cloud, the two founders.

1. **Confirm who is asking**, live, by video or in person, not by email or chat alone. A request to
   recover someone else's access is the classic takeover; if in doubt, stop. Open a record in the change
   log (CTL-CHG): who asked, both operators, the time.
2. **Find the IDs.** The organisation's ID and the Owner's member ID are in the `nemura.org.created` event
   of the organisation's trail, and in `GET /v1/session` for any signed-in member.
3. **Issue the code**, with the migrator credential, while the other operator watches:

   ```bash
   # locally
   make people-owner-recovery ORG=<organisation id> MEMBER=<member id> OPERATORS="<first operator>, <second operator>"
   # in the cloud: the migrate job's command, as a one-off job with the migrator credential [live-verify]
   nemura owner-recovery <organisation id> <member id> "<first operator>, <second operator>"
   ```

   It refuses anything but two different names, and a member who is not an active Owner. The code is shown
   once, valid **24 hours**, never logged; the trail records `nemura.owner.recovery_issued` with both names. A new
   code replaces an unused one: only the newest works.
4. **Hand the code to the Owner by a channel they trust** (in person, a call), never a shared channel or a ticket.
5. The Owner opens Nemura, signs in with the account they will keep, chooses **I have a recovery code**,
   picks the provider and types the code. Every other sign-in of theirs is replaced, their sessions end and
   their passkeys are revoked (register a passkey again), each in the trail: `nemura.owner.recovered`, with
   how many of each. An account that already belongs to another member is refused and the code stays good.
6. **Record the outcome**, and tell every other Owner yourselves (Nemura sends no notice yet). There is no
   24-hour cooling-off yet (SEC-35, with notifications): review the recovered account's activity in the trail
   for the next 24 hours.

## Not covered yet

- **Restoring Nemura on its own** from a backup of its database: the drill is `[live-verify]` against
  the dev cloud, and the audit chain's verification after a restore is shared with Fyniq's
  (`docs/runbooks/audit-restore.md`).
