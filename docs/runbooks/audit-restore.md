# Runbook: a database restore behind the audit anchors

**When:** the database was restored from a backup (point-in-time recovery, a copy, a disaster recovery), or the
nightly verification failed with "a restore landed behind the audit anchors", or the check of the audit trail
(Checks › C.2 #3) names a recovery waiting. **Requirements:** PRD-001 AUD-09, AUD-03, AUD-06, INV-12, G4-070, G4-071;
`p1-audit-explorer` design D8.

The audit trail is a hash chain. Every hour its head is signed and written to the anchors' write-once bucket, which
nobody can change or delete. A restore from a backup can land **behind** an anchor already published: the chain then
continues from the restored head, and its new events take sequence numbers the old anchors sign. Fyniq never deletes
or rewrites an anchor. Instead the **recovery step** records what happened:

- the **restored head**: the last event whose hash matches every anchor; the chain continues from it;
- the **anchored range no longer proven**: from the event after the restored head to the last one the anchors signed;
- a **signed recovery** in the bucket (`audit-anchors/<organisation>/recovery/<epoch>.json`), a row in
  `fyniq.audit_recovery`, and the event `audit.restored` in the chain;
- a new **anchor epoch**: the anchors written from now on go under `audit-anchors/<organisation>/e<epoch>/`.

Verification then reports the lost range as **explained**, with the recovery, never as clean and never as failed. The
check of the audit trail stays ✖ until the Owner acknowledges the recovery.

## Before the restored copy opens

Run the recovery step **before anyone uses the restored database**: it also re-applies the erasure register, so no
security event's IP address or browser erased after the backup was taken comes back (G4-070).

1. **Open an incident** (SEC-32): when the restore happened, why, which backup, who ran it. A restore is a security
   event in itself.
2. **Run the recovery step** with the worker's credential, against the restored database:

   ```bash
   # locally
   make audit-recover
   # in the cloud: the jobs image's command, as a one-off job with the worker's credential
   jobs audit-recover
   ```

   For each organisation whose trail is behind its anchors it prints the epoch it started and the range no longer
   proven, and it re-applies every entry of the organisation's erasure register (each entry's signature is checked
   first). A trail that agrees with its anchors is left alone, so running it again is safe.
3. **Run the other after-restore steps** before opening the copy: `jobs outbox-after-restore` (every unfinished outbox
   row waits for reconciliation), then the usual checks of the restore.
4. **Open the copy.** The Owner and the DPO receive the notice "The audit trail was restored behind its anchors"
   (always on, emailed at once).

## After the copy opens

1. **Verify.** The nightly verification (`jobs audit-verify`, 02:00 Singapore time) runs the recovery step first, then
   checks every trail. It passes, with the lost anchors counted as explained. A range of days over the lost events
   (Compliance › Audit trail › Verify integrity) answers **explained**, naming the recovery.
2. **Record what was lost.** The events in the range no longer proven happened, but the restored database does not
   hold them: whatever they recorded (a posting, an approval, a sign-in) is gone from the copy. Use the incident to
   decide what must be entered again, from the source documents, and by whom.
3. **Acknowledge the recovery.** The Owner, signed in, acknowledges it once
   (`POST /v1/audit/recoveries/{recovery_id}/acknowledge`, with its ETag from `GET /v1/audit/recoveries`; the screen
   arrives with Compliance › Audit trail). Nobody else can: nobody holds A on "Audit trail" yet. The check of the
   audit trail passes again once every recovery is acknowledged; verification still explains the range.

## If something does not agree

- **The recovery step does nothing but verification fails:** the chain does not link, an anchor's signature does not
  verify, or a purge boundary's signed copy differs. That is not a restore: treat it as tampering (the audit-chain
  incident, SEC-32) and do not acknowledge anything.
- **An erasure register entry does not verify:** the step stops and names it. Do not open the copy; the register is
  written once and signed, so a bad entry means the bucket or the key is not the one you think.
- **The restore was behind a purge boundary too** (G4-071): the recovery's restored head is after the boundary;
  verification runs from the boundary and explains both the purged days and the lost range.

## What is recorded

| Where | What |
|---|---|
| The anchors' bucket | `audit-anchors/<organisation>/recovery/<epoch>.json`, signed; the new epoch's anchors under `e<epoch>/` |
| `fyniq.audit_recovery` | The epoch, the restored head and its hash, the range no longer proven, when it was found, and its signed copy |
| `fyniq.audit_recovery_acknowledgement` | Who acknowledged it and when, once |
| The audit trail | `audit.restored`, `audit.details_erased` (with `reapplied`), `audit.restore_acknowledged`, and each night's `audit.verification.completed` with the recoveries waiting |
| Notifications | `security.audit_recovered` to the Owner and the DPO |
