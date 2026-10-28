---
key: approvals.decide
type: task
title: Ask for, approve or reject a change that needs a second approver
summary: Which changes wait for a second person, approving in steps from the Inbox, a person's bank account, withdrawing a request, and why one becomes void
sources:
  - docs/user-guide/approvals.md sha256:261ee0dbf2fffaaaa8ae693e384a2ae5260b2c0f2fc49cc13d0a273ce81814a8
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-28
aliases:
  - review log
  - weekly review
  - self-approved
  - two-person rule
  - second approver
  - waiting for approval
  - to approve
  - withdraw a request
  - cancel my request
  - inbox
  - weekly review
  - review log
  - approve a bank account
  - person's bank account
---

# Ask for, approve or reject a change that needs a second approver

## Which changes wait

- Settings such as thresholds, retention and the AI assistant ([[settings.change]]).
- Activating a statutory pack version ([[settings.statutory-packs]]).
- Posting a journal above the self-approval threshold, and reversing a journal in Founder mode ([[accounting.journals]]).
- Once the company has a second member: adding or ending a role, and inviting someone ([[members.roles]], [[members.invite]]).
- Fyniq asks for approval instead of making the change; another person who approves that kind of change, such as an Owner, decides. Nobody approves their own request.

(src: docs/user-guide/approvals.md#what-it-is)

## Ask for a change

- Make the change as usual, then confirm it's you with your passkey or authenticator app.
- Fyniq shows **Waiting for approval**; nothing has changed yet.
- One request waits for each setting or person at a time.

(src: docs/user-guide/approvals.md#ask-for-a-change)

## Approve or reject

- **Inbox → To approve**, choose the request and check each value before and after.
- **Approve…** or **Reject…** opens the confirmation: exactly what is signed, the step, and the request's fingerprint; a rejection needs a reason.
- **Approve with passkey** (or **Reject with passkey**) and confirm on the device; without a passkey, **Approve** or **Reject** and confirm it's you when asked.
- The Inbox never approves by itself: every decision goes through the confirmation.
- An approval signs exactly what was shown; who approved, when and how they confirmed it is in the audit trail.
- With a passkey the device signs the request itself: the confirmation carries a fingerprint of exactly what is approved or rejected, works once for that request within 5 minutes, and the device's signature is kept with the approval so it can be verified later ([[auth.sign-in]]).
- Some changes need more than one approval, in steps set by the approval policy ([[approvals.policies]]); the request shows each step and its approvals, and the change is made with the last approval.
- You approve a request once, and only at a step that names you or one of your roles.

(src: docs/user-guide/approvals.md#approve-or-reject)

## Approve a person's bank account

- A person's bank account for reimbursements changes only when someone who approves payments, such as the Controller, approves it; never the person themselves ([[preferences.security]]).
- Open the request from **Inbox → To approve**: it names the person, the bank and the last four digits.
- Open the screenshot of their banking app, or their statement's header, under **Evidence**.
- **Show number** under **Compare with the person's screenshot** shows the number after confirming it's you, and is recorded; check the number and the name match the screenshot.
- Approve it, or reject it with the reason; while it waits, payments to the person wait too.

(src: docs/user-guide/approvals.md#approve-a-persons-bank-account)

## Withdraw your request

- Open the request from **Inbox → Your requests, waiting**, select **Withdraw…**, then **Withdraw**.
- The request is cancelled with any approvals it had, and nothing changes.
- Only the person who asked can withdraw it; anyone else who disagrees rejects it with a reason.

(src: docs/user-guide/approvals.md#withdraw-your-request)

## Review high-risk actions

- Every high-risk change is logged when it is made: each change another person approved, each role or invitation the Owner set up alone before a second person joined, and, in Founder mode, each journal a founder posted alone at or below the self-approval threshold; another founder reviews the log weekly.
- **Inbox → Weekly review** opens the **Review log** of unreviewed actions and who did each; tick the ones checked, select **Mark as reviewed**, add a note if wanted, and confirm.
- Nobody reviews their own action; each is reviewed once; one still unreviewed 14 days after it was made shows as overdue in the Inbox and the review log.

(src: docs/user-guide/approvals.md#review-high-risk-actions)

## When it can no longer be approved

- **Changed**: the setting, the person's roles, the approval policy or the team mode changed after the request, so it is void and nothing changes; ask again ([[settings.team-mode]]).
- **An approver lost their right**: someone who approved an earlier step no longer has the role their step needed when the last approval comes, so nothing changes and the request is void.
- **Expired**: a request waits 7 days; then ask again.
- **Rejected**: the change is not made, and the reason is shown.

(src: docs/user-guide/approvals.md#when-a-request-can-no-longer-be-approved)
