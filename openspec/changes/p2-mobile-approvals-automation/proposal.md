# Proposal

## Why

Approvals are the one thing an owner does away from a desk: on a phone, between meetings, and while on leave. Today an approver
signs in the web app only, nobody can stand in for them while they are away, and a request that nobody looks at waits seven days
and expires without a word. The same people also want the platform to tell them things without being asked ("a bill over S$5,000
was created"), a supplier's cancellation deadline never to pass unnoticed, and a closed month to stay closed for everyone but the
accountant. This change adds those, without adding a single way to approve, post, pay or file that the approval engine and the
ledger do not already govern.

## What Changes

- **Delegation of approval authority** (FR-APR-06): an approver names a delegate for a start and an end date; the delegate
  signs the steps the delegator could, as themselves holding the approval right, with the signature recording on whose behalf.
- **Reminders and escalation** (FR-APR-08): a pending approval reminds its approvers and escalates to the Owners; the platform's
  worker only notifies, it never signs or decides.
- **Approve by email and in the PWA** (FR-APR-07): an approval email links to the request; signing in and the same passkey step-up
  stay the only way to decide; nothing is decided by a link. The installable PWA (FR-UX-16) shows the Inbox and the approval on a
  phone.
- **The installable PWA** (FR-UX-16): the manifest, a service worker that caches the shell and a few read-only lists (never a
  write), camera capture of a receipt and an offline receipt queue that is encrypted on the device, time-limited and checked by the
  server on upload.
- **Workflow rules without code** (FR-AUT-03) and **outbound webhooks** (FR-AUT-04): a rule is a typed condition on an event and a
  fixed action (notify; raise a task); webhooks are FR-INT-12, built in `p2-api-third-parties`.
- **Soft locks** (FR-GL-09): the soft-locked state of a period, and per-module soft locks, enforced by the domain; only the
  accountant posts into one, with a reason, and an exceptions report lists them.
- **Supplier contracts and renewals** (FR-AP-21) with the expected-cost-without-a-bill check C.2 #52, and **vendor statement
  reconciliation** (FR-AP-10).
- **The usage view** (FR-ADM-05), **pinned Basic mode** (FR-UX-28) and **feature tours and What's new** (FR-UX-35).
- **The template editor's P2 half** (FR-TPL-01, FR-TPL-02, FR-TPL-04: drag-and-drop layout, the column chooser, conditional blocks and
  the side-by-side diff) and **code mode** (FR-TPL-06).
- **The annual-return pack** (FR-CS-07) and **evidence export per framework and period** (CMP-04).

## Capabilities

### Modified Capabilities
- `access/approvals` (FR-APR-06, FR-APR-07, FR-APR-08), `access/users-and-sign-in` (FR-ADM-05), `automation/rules-and-schedules`
  (FR-AUT-03, FR-AUT-04), `compliance/compliance-center` (CMP-04), `controls/platform-checks` (C.2 #52), `documents/templates`
  (FR-TPL-06), `experience/help` (FR-UX-35), `experience/modes` (FR-UX-28), `experience/navigation` (FR-UX-16),
  `governance/company-secretary` (FR-CS-07), `ledger/general-ledger` (FR-GL-09), `purchases/payables` (FR-AP-10, FR-AP-21)

## Impact

New tables (migrations 001200 to 001249), a worker job for reminders and one for contract reminders, new operations with matrix
rows, new narrow grants (listed in `docs/security/p2-grants-for-human-review.md`), and web screens with design fragments. No posting
rule changes; no AI or MCP tool is added (the never-list holds: rules and delegations have no AI path). Outside needs: a push
provider is **not** used (reminders go by the existing notification email and in-app channels); camera capture and the service
worker are verified on a real phone later (`[live-verify]`).
