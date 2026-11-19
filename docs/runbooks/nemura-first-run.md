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

## Not covered yet

- **An Owner who cannot sign in** (their provider account is lost): Nemura has no recovery procedure of
  its own yet. Until it has, the second Owner (a company should hold two) approves a new Owner's role
  request. This is a gap in `p1-nemura-platform`, listed in its tasks (10.2).
- **Restoring Nemura on its own** from a backup of its database: the drill is `[live-verify]` against
  the dev cloud, and the audit chain's verification after a restore is shared with Fyniq's
  (`docs/runbooks/audit-restore.md`).
