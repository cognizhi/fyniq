---
key: compliance.controls
title: See whether the controls hold
mode: expert
roles: [owner, admin, controller, accountant, auditor, dpo]
requirements: [CMP-01, FR-CTL-05]
---

# See whether the controls hold

## What it is

**Controls** (from **Compliance**, Expert mode) lists every control that keeps the books and the platform in order: that
every journal entry balances, that payments are approved by a second person, that every member signs in with a second
factor, that the audit trail stays intact, and the rest. Each control shows its status, why, the role that answers for
it, the frameworks it maps to, and each of its checks with what it last said and its evidence.

Fyniq works out every control's status each night from its checks. Nobody types a status in.

The Owner, the Administrator, the Controller, the Accountant, the Auditor and the Compliance officer see it.

## What each status means

- **Pass**: each of its checks passed.
- **Fail**: a check found something that breaks the control, such as a payment approved by the person who prepared it.
- **Overdue**: a review or a check fell due and was not done, such as a quarter's access review not attested.
- **Not assessed**: a check could not say, and the control says why: a check not built yet, one that runs only in the
  cloud, or one that needs something first, such as the company's GST registration.
- **Not applicable**: none of its checks applies here.

A control takes the worst of its checks: fail, then overdue, then not assessed, then pass. The problems come first in
the list; select a status's count to see only those.

## Filter by framework

Select a framework, such as **SOC 2**, **PDPA** or **Singapore statute**, to see only the controls mapped to it. **By
framework** shows how many of each framework's controls pass. **All frameworks** shows every control again.

## A control's checks

Select a control to see its checks, by how often each runs:

- **In CI**: tests and scans that run on every change before it can ship. In a release, they show as passed, with a
  link to the evidence the release keeps. In a development build they are not assessed, since no release's evidence
  comes with it.
- **Nightly job**: a job the platform runs each night, such as recomputing the balances from their lines or verifying
  the audit trail. **Nightly verification** shows those of the ledger and the audit trail.
- **Check**: a check of the books, also on **Checks**.
- **Verification**: a check of the records of the last day, such as every payment's two approvers.
- **Not built yet**: a check that arrives with a later change, which it names.

## When a control fails

When a check that runs in Fyniq newly fails a control, or makes it overdue, the people who hold the control's owner role
get a notice at once, by email too. They get it once for each failure, not each night. A control that recovers is
recorded in the audit trail without a notice. A check that runs in CI never sends one: a failing check there stops the
change from shipping.

The controls you answer for that fail or are overdue are also listed in your **Inbox**, under **Compliance**.
