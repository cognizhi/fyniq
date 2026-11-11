---
key: compliance.controls
type: task
title: See whether the controls hold
summary: Every control with its status, owner, frameworks, checks and evidence; the notice when one fails; the incident, restore drill and heartbeat drill registers
sources:
  - docs/user-guide/compliance-controls.md sha256:1b20bfb0a3a0b45f1618d68a46c299567ac6862a6e41ebf351ea915d44dd8f03
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-11
aliases:
  - compliance center
  - control dashboard
  - controls failing
  - is the control passing
  - which controls fail
  - SOC 2 controls
  - PDPA controls
  - control evidence
  - control overdue
  - not assessed
  - security incident
  - incident register
  - open an incident
  - we were hacked
  - incident severity
  - restore drill
  - disaster recovery test
  - can we restore the books
  - backup test
  - heartbeat drill
  - scheduler stopped
  - tick alert
---

# See whether the controls hold

## What it is

- **Controls** (from **Compliance**, Expert mode) lists every control that keeps the books and the platform in order, such as every journal entry balancing, payments approved by a second person, every member signing in with a second factor, and the audit trail staying intact ([[compliance.audit-trail]]).
- Each shows its status, why, the role that answers for it, the frameworks it maps to, and each check with what it last said and its evidence.
- Fyniq works out every status each night from the checks; nobody types one in. The risks the controls reduce are in the risk register ([[compliance.risks]]).
- The Owner, the Administrator, the Controller, the Accountant, the Auditor and the Compliance officer see it ([[members.roles]]).

(src: docs/user-guide/compliance-controls.md#what-it-is)

## What each status means

- **Pass**: each check passed. **Fail**: a check found something that breaks the control. **Overdue**: a review or check fell due and was not done.
- **Not assessed**: a check could not say, and the control says why: not built yet, run only in the cloud, or waiting for something first.
- **Not applicable**: none of its checks applies here.
- A control takes the worst of its checks: fail, then overdue, then not assessed, then pass. Problems come first; select a status's count to see only those.

(src: docs/user-guide/compliance-controls.md#what-each-status-means)

## Filter by framework

- Select a framework, such as **SOC 2**, **PDPA** or **Singapore statute**, to see only its controls; **All frameworks** shows every control again.
- **By framework** shows how many of each framework's controls pass.

(src: docs/user-guide/compliance-controls.md#filter-by-framework)

## A control's checks

- **In CI**: tests and scans that run on every change before it ships; passed in a release, with a link to the evidence it keeps; not assessed in a development build.
- **Nightly job**: such as recomputing the balances or verifying the audit trail; **Nightly verification** shows the ledger's and the audit trail's.
- **Check**: a check of the books, also on **Checks** ([[checks.overview]]). **Verification**: a check of the last day's records, such as every payment's two approvers.
- **Not built yet**: it arrives with a later change, which it names.

(src: docs/user-guide/compliance-controls.md#a-controls-checks)

## When a control fails

- When a check that runs in Fyniq newly fails a control or makes it overdue, the people holding its owner role get a notice at once, by email too, once per failure.
- A recovery is recorded in the audit trail without a notice; a check in CI never sends one, since a failing one stops the change from shipping.
- The controls you answer for that fail or are overdue are listed in your **Inbox**, under **Compliance**.

(src: docs/user-guide/compliance-controls.md#when-a-control-fails)

## The incident register

- The **Incidents** tab lists every security incident, from when it was seen to its review.
- Open one as soon as something is suspected: a security alert, a failing control, a report from a customer or a supplier, anything that may have exposed money, personal data or access.
- **Open an incident**: a few words, what is known, its **severity** and, if it was seen earlier, when. **Critical**: money, personal data, access or the books' integrity lost or at once at risk. **High**: an attempt that was stopped, or exposure without signs of access. **Medium**: a failing control. **Low**: a false alarm worth keeping.
- **Next step**, in order: **Contained**, **Assessed**, **Notified** (after it is assessed), **Recovered** (after it is contained), **Reviewed and closed** (after it is recovered: the root-cause review). Each step takes a note.
- Personal data involved: record the breach first in **Compliance › Data protection › Breaches** ([[compliance.data-protection]]), then choose it on the **Assessed** step; the breach register keeps the PDPA's timers.
- Steps are only ever added, each in the audit trail. Never write a secret, a token, a full NRIC or a bank number in an incident.
- The DPO keeps the register; the Owners, the Administrators, the Controller, the Accountant and the Auditor read it.

(src: docs/user-guide/compliance-controls.md#the-incident-register)

## Restore drills

- The **Restore drills** tab is the register of full restore drills: proof that the books come back after the worst.
- **Every four weeks** Fyniq copies its database as it was an hour earlier, checks the ledger and the audit trail on the copy, and removes the copy. The control **CTL-RES-02** shows whether the latest copy passed; its owners are told when one fails.
- **Each quarter** people run the full drill by the restore drill runbook: the weekly copy of the database restored somewhere separate, as if the production platform were lost, and timed.
- **Record a drill** (the DPO): the day, the scenario, what was restored from, how much data the copy lacked and how long the recovery took, in minutes, and what worked on the restored copy: the ledger's check, the audit trail's check, documents, keys, sign-in, the scheduled jobs and the queued work.
- A drill counts as **Recovered** only when every one worked. The next full drill is due 3 months after the last that recovered; the review **Restore drill** under **Reviews and attestations** says when.
- Drills are only ever added, each in the audit trail. The DPO keeps the register; the Owners, the Administrators, the Controller, the Accountant and the Auditor read it.

(src: docs/user-guide/compliance-controls.md#restore-drills)

## Heartbeat drills

- Fyniq's scheduled work starts from one tick every five minutes. If it stops, the platform owner is told within 30 minutes and the Owners within an hour, by alerts that watch it from outside Fyniq.
- The **Heartbeat drills** tab is the register of the drills that prove it: each quarter the tick is paused in staging, by the heartbeat drill runbook, and timed.
- **Record a drill** (the DPO): the day, where the tick was paused and for how long, and how many minutes after the pause the first alert and the escalation came; leave one empty when it never came.
- A drill **Passed** only when both came in time. The next drill is due 3 months after the last that passed; the review **Heartbeat drill** under **Reviews and attestations** says when.
- Drills are only ever added, each in the audit trail. The DPO keeps the register; the Owners, the Administrators, the Controller, the Accountant and the Auditor read it.

(src: docs/user-guide/compliance-controls.md#heartbeat-drills)
