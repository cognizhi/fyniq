---
key: approvals.decide
type: task
title: Ask for, approve or reject a change that needs a second approver
summary: Which changes wait for a second person, how to approve or reject one from the Inbox, and why a request becomes void or expires
sources:
  - docs/user-guide/approvals.md sha256:989775143149d16b5e9811e491ebe2a503b534bbb7293d32f7067c6ac6cdb44d
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - two-person rule
  - second approver
  - waiting for approval
  - to approve
---

# Ask for, approve or reject a change that needs a second approver

## Which changes wait

- Settings such as thresholds, retention and the AI assistant ([[settings.change]]).
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

(src: docs/user-guide/approvals.md#approve-or-reject)

## When it can no longer be approved

- **Changed**: the setting or the person's roles changed after the request, so it is void and nothing changes; ask again.
- **Expired**: a request waits 7 days; then ask again.
- **Rejected**: the change is not made, and the reason is shown.

(src: docs/user-guide/approvals.md#when-a-request-can-no-longer-be-approved)
