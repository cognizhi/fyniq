# Design

All decisions below were taken while the owner was away, on the recommended option, and are `[owner to confirm]`.

## D1. Delegation never grants a right

A delegation is a row (delegator, delegate, first day, last day, reason). It changes **who a step is open to** and nothing else:

- the delegate must already hold A on the object's Appendix D row (a policy never grants an approval right Appendix D does not,
  FR-APR-01), is an active member, is not the delegator, and is not the requester (FM-1);
- the step must be one the delegator is eligible for by their live roles and the step's names (`approval.Eligible`);
- the delegate signs as themselves, with their own step-up and passkey; the signature records `on_behalf_of` (the delegator);
- the delegator and the delegate never both count towards one step's quorum: a person who is on behalf of another, or who was
  signed for, is counted once;
- no chaining (a delegate cannot re-delegate what they hold by delegation), no self-delegation, at most 90 days, only the delegator
  (or an Owner, to end it) changes it; creating, ending and using one are audited and notify both people.
- The database refuses a signature that names `on_behalf_of` without a live delegation on the signing day (checked against
  `signed_at`, not the clock) and one where the delegation's people do not match.

*Not chosen:* letting a delegation lend the delegator's A to a delegate who holds none (a widening: owner option, recommended
against: a person on leave has no more right than at their desk, and a policy that wants a stand-in names one).

## D2. Reminders and escalation only notify

The worker job `approvals.remind` (hourly) reads the pending requests and, for each, notes a reminder when the request has waited
`approval_reminder_hours` (organisation setting, default 24, 1 to 168) since it was requested or last reminded, up to three, then an
escalation once after `approval_escalate_hours` (default 72) to the Owners and the requester. A reminder goes to the people who may
sign the current step now (including delegates), as the new notification types `approval.reminder` and `approval.escalated`.
Neither changes a request, signs, extends its expiry or approves; the insert-only table `approval_reminder` keeps one row per
(request, kind, number) so a rerun repeats nothing. The worker has `INSERT` on that table only.

## D3. Email and PWA approvals are the web approval

An approval email (`approval.requested`, `approval.reminder`) carries a link to `/inbox/<id>`; the web app asks for sign-in and
returns there. The link carries no token and no GET changes anything (mail scanners open links). Signing stays `challengeApproval`
then `approveApproval` with the envelope hash, `If-Match` and a fresh step-up: there is no new approve operation, and the PWA is the
same web app installed. Push notifications are not built (no provider chosen): `[live-verify]` if added.

## D4. The PWA

A web manifest and a service worker. The worker caches the app shell and static assets, and answers a short allow-list of GET requests
(the Inbox list and an approval) from the cache when offline, marked "offline: as of <time>"; it never caches or queues a POST, PUT,
PATCH or DELETE, so no approval or any write is ever replayed later. Its caches are cleared at sign-out. Receipt capture uses the
camera input; the offline receipt queue (FR-UX-16) keeps files in IndexedDB encrypted with a key held in the page's memory-bound
`CryptoKey` (non-extractable), limited to `offline_queue_hours` (setting, default 72), removed at sign-out and not offered on a
device the person marks shared; upload goes through the same document inbox operation, with a client-generated ID and the file's
hash so the server deduplicates, and the server re-checks the person's rights at upload.

## D5. Workflow rules

A rule: an event type from the webhook catalogue, a typed condition (amount at least, currency, contact; no expressions), and one
action from a fixed list: **notify** (named members or roles; each recipient's right to see the object is re-checked) and **raise a
task** (a compliance task, FR-CMP-tasks). A database CHECK holds the action list; no action maps to a command that posts, approves,
pays, files or sends outside; there is no AI or MCP tool for rules. A rule runs in the worker where webhooks fan out; a rule's own
notification never triggers a rule; a feature setting `features.automation_rules` is off until an Owner turns it on. Webhooks
(FR-AUT-04) are FR-INT-12 and are satisfied by it.

## D6. Soft locks

`soft_locked` already exists in the period status. The domain refuses a posting into a soft-locked period unless the poster holds the
Accountant role, has a reason, and the posting is listed on the exceptions report; a per-module soft lock (AR, AP, banking, payroll)
refuses postings of that module's documents into the period for everyone but the Accountant. The hard close and INV-03 are
unchanged and stay the database's.

## D7. Supplier contracts, vendor statements, usage, modes, tours

Supplier contracts: a register (supplier, owner, dates, renewal type, notice deadline, billing, amount, minimum commitment) with a
document as evidence; reminders to the owner's Inbox 30 and 7 days before the notice deadline; it posts nothing. C.2 #52 reads the
register. Vendor statements: the supplier's statement lines are entered or imported and matched to bills and payments; differences
are listed, nothing is posted. The usage view reads AI tokens, storage and exports per month. Pinned Basic: an organisation setting
per role. Tours: per-feature tips and What's new cards on the existing help state.

## D8. Template editor and code mode

Layout, columns and conditions are the typed JSON the render service already reads; the visual diff compares two published
versions. Code mode lets an expert edit the Typst source of a template inside the render sandbox (no network, no packages, time
and memory limits), versioned and approved like any template.

## D9. Annual return and evidence export

The annual-return pack assembles officers, members, share capital and figures by filing category; it files nothing (BizFile+ stays
manual). Evidence export packages a control framework's evidence for a period as a signed archive made by a job.
