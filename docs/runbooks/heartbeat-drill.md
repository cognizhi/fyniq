# Runbook: the heartbeat drill

**When:** each quarter, which the review `heartbeat-drill` in Compliance › Reviews tracks (due 3 months after the last
drill that passed); and after any change to the Scheduler, the heartbeat's alerts or their channels.
**Requirements:** PRD-001 NFR-OBS-02, CTL-RES-05; `p1-operations-dr` design D6.
**Who:** whoever holds staging's break-glass pauses and resumes the tick; the DPO records the drill in Compliance ›
Controls › Heartbeat drills (the Compliance Center's register, which only the DPO writes).

The tick is the one Scheduler job that starts every platform job and reconciles the outbox. If it stops, nothing
complains inside Fyniq, so Cloud Monitoring watches its heartbeat from outside (infra/gcp/modules/heartbeat):

| Absent for | Who is told |
|---|---|
| 30 minutes | The named platform owner (`platform_owner_emails`; without one, the Owners) |
| 60 minutes | The Owners as well: the escalation |
| A daily job late | The platform owner, at the first line `heartbeats.check` logs |

The drill proves both alerts arrive. Hold it in **staging**: pausing production's tick stops production's work.

1. **Check the alerts are set.** Compliance › Controls › CTL-RES-05: the daily check `scheduler.configured` passes in
   staging (the tick's Scheduler job runs; the three policies are on with a channel).
2. **Tell the people who will be alerted** that a drill is starting, so nobody opens an incident for it.
3. **Pause the tick** and note the time `[verify the command]`:

   ```bash
   gcloud scheduler jobs pause tick --location=asia-southeast1 --project=fyniq-staging
   ```

4. **Wait for the alerts.** Note how many minutes after the pause the platform owner's email came (within 30) and the
   Owners' (within 60). Keep the tick paused at least 65 minutes, so the escalation has its chance.
5. **Resume the tick:**

   ```bash
   gcloud scheduler jobs resume tick --location=asia-southeast1 --project=fyniq-staging
   ```

   Within five minutes the tick beats again and the alerts close by themselves. The platform jobs whose slots were
   missed are recorded as skipped and run at their next slot; `jobs heartbeat-check` passes once each has beaten.
6. **Record the drill** (the DPO): Compliance › Controls › Heartbeat drills › Record a drill, with the day, where, how
   long the tick was paused, the minutes after the pause each alert came (empty when one never came), and the findings.
   It passes only when both came in time; a drill that did not pass leaves the review due. A failed drill is a finding
   for the platform owner: fix the alert, then drill again.
