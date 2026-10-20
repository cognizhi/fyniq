---
key: approvals.decide
type: task
title: Ask for, approve or reject a change that needs a second approver
summary: Which changes wait for a second person, approving or rejecting in steps from the Inbox, withdrawing your request, and why one becomes void or expires
sources:
  - docs/user-guide/approvals.md sha256:6e64d0185037cd0091acb7b72b2f6c7b863e56e65d68284e21c59af54522cdfa
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - review log
  - weekly review
  - two-person rule
  - second approver
  - waiting for approval
  - to approve
  - withdraw a request
  - cancel my request
---

# Ask for, approve or reject a change that needs a second approver

## Which changes wait

- Settings such as thresholds, retention and the AI assistant ([[settings.change]]).
- Activating a statutory pack version ([[settings.statutory-packs]]).
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
- Confirm it's you, then **Approve** to make the change now, or **Reject** and say why.
- An approval signs exactly what was shown; who approved, when and how they confirmed it is in the audit trail.
- With a passkey the device signs the request itself: the confirmation carries a fingerprint of exactly what is approved or rejected, works once for that request within 5 minutes, and the device's signature is kept with the approval so it can be verified later ([[auth.sign-in]]).
- Some changes need more than one approval, in steps set by the approval policy ([[approvals.policies]]); the request shows each step and its approvals, and the change is made with the last approval.
- You approve a request once, and only at a step that names you or one of your roles.

(src: docs/user-guide/approvals.md#approve-or-reject)

## Withdraw your request

- Open the request you asked for and select **Withdraw**.
- The request is cancelled with any approvals it had, and nothing changes.
- Only the person who asked can withdraw it; anyone else who disagrees rejects it with a reason.

(src: docs/user-guide/approvals.md#withdraw-your-request)

## Review high-risk actions

- Every high-risk change is logged when it is made: each change another person approved, and each role or invitation the Owner set up alone before a second person joined; another founder reviews the log weekly.
- **Review log** lists the unreviewed actions and who did each; choose the ones checked, add a note if wanted, and select **Mark as reviewed**.
- Nobody reviews their own action; each is reviewed once; one still unreviewed 14 days after it was made shows as overdue on **Checks**.

(src: docs/user-guide/approvals.md#review-high-risk-actions)

## When it can no longer be approved

- **Changed**: the setting, the person's roles, the approval policy or the team mode changed after the request, so it is void and nothing changes; ask again ([[settings.team-mode]]).
- **An approver lost their right**: someone who approved an earlier step no longer has the role their step needed when the last approval comes, so nothing changes and the request is void.
- **Expired**: a request waits 7 days; then ask again.
- **Rejected**: the change is not made, and the reason is shown.

(src: docs/user-guide/approvals.md#when-a-request-can-no-longer-be-approved)
