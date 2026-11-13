# Runbook: the restore drills

**When:** each quarter, the full drill, which the review `restore-drill` in Compliance › Reviews tracks (due 3 months
after the last drill that recovered); and whenever a change touches backups, the export, keys or the restore path.
**Requirements:** PRD-001 DR-01 to DR-06, NFR-DR-01, NFR-DR-02, NFR-DR-03, CTL-RES-02; `p1-operations-dr` design D2.
**Who:** the Owner runs it with whoever holds the platform's break-glass; the DPO records it in Compliance › Controls ›
Restore drills (the Compliance Center's register, which only the DPO writes).

Two drills prove that the books come back:

| Drill | How often | Scenario | Who | Evidence |
|---|---|---|---|---|
| The monthly drill | Every 28 days, by itself | Database corruption or a mistake: a copy an hour back | The platform job `restore.drill` | CTL-RES-02 (M): the job's outcome; `restore.drilled` in the installation's audit trail |
| The full drill | Each quarter | The loss of the production project: the weekly export restored into an isolated project | People, by this runbook | CTL-RES-02 (Q): the drill in the register, signed off as the review `restore-drill` |

## The monthly drill

Nothing to do while it passes. The job clones the database as it was an hour ago, in production's own project where
no service reaches the copy, runs the ledger's and the audit chain's verifications on the copy, removes the copy
whatever happens, and fails when either verification fails or the drill takes longer than NFR-DR-02's 4 hours. An
anchor written after the copy's point in time is counted, not failed.

When it fails, CTL-RES-02 fails and its owners are told. Then:

1. Read the job's detail in Compliance › Controls › CTL-RES-02, or the `restore.drilled` event in the installation's
   audit trail: which verification failed, for which organisation, and whether the copy was removed.
2. **A ledger or a chain that fails on the copy** fails on production too, or will: run the nightly verifications now
   (`jobs audit-verify`; Checks › Run now) and, if they fail, open an incident (docs/runbooks/incident-response.md).
3. **A copy that was not removed** costs money every hour: delete the instance `<instance>-drill-<time>` (Cloud SQL ›
   the instance › Delete), after turning its deletion protection off.
4. **A drill over 4 hours**: record it as a finding of the next full drill; the RTO target needs a faster restore.

Locally, `make restore-drill` runs the same drill against the local database.

## The full drill (each quarter)

The scenario is the loss of `fyniq-prod`: the newest weekly export, in `fyniq-audit`'s weekly bucket, restored into a
project of its own. Time it from step 3 to the last check. NFR-DR-01 accepts up to 7 days of data lost in this
scenario; NFR-DR-02 sets no target at P1: the drill measures it.

**Known gap `[verify]`:** the evidence bucket and the key that seals sensitive fields (bank numbers, NRIC) live in the
production project. A real loss of `fyniq-prod` loses them with it, so a drill that is honest about this scenario finds
documents and sealed fields that do not open. Record what you find; the owner decides whether the bucket and the key
move to `fyniq-audit` (as the anchors and the export did) or the scenario's scope changes.

1. **Open an incident** (SEC-32) of severity low: "Quarterly restore drill". A drill uses production's backups and is
   a security event in itself.
2. **Prepare the drill project** `fyniq-drill` `[live-verify]`: in the organisation, in Singapore (the organisation
   policy allows nothing else), with no peering or VPN to `fyniq-prod`, and no person's standing access. The release
   images come from Artifact Registry by digest.
3. **Restore the export.** In `fyniq-drill`, create a Cloud SQL for PostgreSQL 18 instance with a private address only.
   Grant its service account read on the weekly bucket and decrypt on the export key in `fyniq-audit` for the drill
   only, then import the newest export `[verify the flags]`:

   ```bash
   gcloud sql import sql <drill instance> gs://fyniq-production-db-weekly/weekly/fyniq-<date>-<time>.sql.gz \
     --database=fyniq --project=fyniq-drill
   ```

   Note the export's time: the data lost is from it to the drill's starting point.
4. **Before the copy opens**, with the worker's credential against it: `jobs audit-recover` (docs/runbooks/audit-restore.md),
   then `jobs outbox-after-restore`, so nothing sent before the export is sent again unchecked.
5. **Start the services** in `fyniq-drill` from the release images, with a `FYNIQ_GCP` naming the drill's own queues,
   buckets and keys. Never point a drill at production's.
6. **Check, and note each as passed or not:**
   - **Ledger**: `jobs run-slot ledger.verify <now>`, or Checks › Run now; every organisation's balances agree.
   - **Audit chain**: `jobs audit-verify`; every trail agrees with its anchors, any range after the export explained by
     the recovery.
   - **Documents**: open an invoice's PDF and a committed piece of evidence.
   - **Keys**: reveal a sealed field (a payee's bank number) and let the hourly anchor be signed.
   - **Sign-in**: an Owner signs in with a passkey.
   - **Tick**: the Scheduler's tick runs a platform job (its heartbeat moves).
   - **Outbox**: the rows `outbox-after-restore` set aside wait for reconciliation, and none is sent a second time
     `[verify where the reconciliation shows]`.
7. **Tear down** `fyniq-drill`'s instance and services, and remove the grants of step 3. Copies of production's data
   never stay longer than the drill (DEV-08).
8. **Record the drill** (the DPO): Compliance › Controls › Restore drills › Record a drill, with the scenario, what was
   restored from, the data lost and the time taken in minutes, each check, and the findings. It counts as recovered
   only when every check passed; a drill that did not recover leaves the review due. Close the incident with the
   findings as its review.

## Other scenarios

- **A zone's failure** (DR-01): in staging, force a failover of the regional instance `[verify: production is
  regional; staging is zonal by the cost decision]`, time how long writes stop, and record it as `zone_failure`.
- **Corruption or a mistake, by hand**: the monthly drill rehearses it. To restore production itself, follow
  docs/runbooks/audit-restore.md after the point-in-time restore.
