---
key: compliance.controls
type: task
title: See whether the controls hold
summary: Every control with its status, why, its owner role and frameworks, each check and its evidence; the framework filter; the notice when one fails
sources:
  - docs/user-guide/compliance-controls.md sha256:c6e11891f68203a44f0df8119383f65854e91281e44baea90a092cde9e6dc967
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-09
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
---

# See whether the controls hold

## What it is

- **Controls** (from **Compliance**, Expert mode) lists every control that keeps the books and the platform in order, such as every journal entry balancing, payments approved by a second person, every member signing in with a second factor, and the audit trail staying intact ([[compliance.audit-trail]]).
- Each shows its status, why, the role that answers for it, the frameworks it maps to, and each check with what it last said and its evidence.
- Fyniq works out every status each night from the checks; nobody types one in.
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
