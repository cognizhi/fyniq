---
key: ai.proposals
type: task
title: Review, approve or reject a change the AI proposed
summary: Review, approve or reject an AI proposal; the AI tasks page with its proposal history, catalogue and never-list; turning a task type on or off
sources:
  - docs/user-guide/ai-proposals.md sha256:4791b5644971a50464292dfa10a0cb3b0b779732a4fb32215368e5f6888e4fea
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-15
aliases:
  - AI proposal
  - approve what the AI proposed
  - the AI wants to change something
  - task type
  - proposal expired
  - stale proposal
  - AI tasks
  - turn on a task type
  - what the AI can never do
---

# Review, approve or reject a change the AI proposed

## What it is

- The AI never changes the books itself. It may only propose a kind of change an Owner turned on, a **task type** ([[settings.organisation]]), for a person to approve; only then does Fyniq make it, in the approver's name.
- A proposal waits in the **Inbox** for 72 hours, then expires and nothing changes.
- Each person sees the proposals made for them and those they may approve; an Owner and the Data Protection Officer see every one.

(src: docs/user-guide/ai-proposals.md#what-it-is)

## Review it

- The **Summary** is Fyniq's, from the proposal and its dry run, never the AI's: it says what would change.
- **What changes** is the dry run's result and **Evidence** the records the AI relied on.
- **The AI's reasons** are shown apart with the **AI** label: the AI's own words, to check against the summary.

(src: docs/user-guide/ai-proposals.md#review-a-proposal)

## Approve or reject

- **Approve** signs exactly what the review shows, by its fingerprint; you confirm it's you first ([[approvals.decide]]).
- With several items, clear the ones you disagree with: the approval covers only the items kept. A routine (tier 1) proposal can be approved whole in one action; any other names each item approved.
- **Reject** needs a reason, which the assistant reads with your next question in the same conversation ([[ai.assistant]]).
- The person who asked may approve their own routine (tier 1) proposal.

(src: docs/user-guide/ai-proposals.md#approve-or-reject)

## After you approve

- Fyniq makes the change as the approver, once. If a record the proposal relied on changed since its dry run, nothing is made and it shows **Stale** with why; ask the assistant again.
- **Checks** flags a proposal waiting more than 48 hours.

(src: docs/user-guide/ai-proposals.md#after-you-approve)

## The AI tasks page

- **AI tasks**, opened from the **Inbox**: **Proposals** lists every proposal you may see, newest first, filtered by state (such as **Waiting** or **Executed**), each opening its review.
- **Catalogue** lists the task types by tier: what each does, the most items per proposal, who approves it, how long a proposal waits before it expires, and whether it is on; one this release cannot run yet shows **Not in this release**.
- **Never proposed** is what no task type can ever propose: approving anything, payments, a payee's bank details, users and roles, approval policies, the AI's own settings, statutory parameters, reopening a period, voiding a posted record, filing with IRAS, CPF or ACRA, exporting the company's data, the audit trail, shares and investor rights, a director's balance, officers and registers, and marking an obligation as met. A person does these.
- With the AI switched off the page says so, and nothing is proposed until an Owner switches it on again ([[settings.ai]]).

(src: docs/user-guide/ai-proposals.md#the-ai-tasks-page)

## Turn a task type on or off

- Every task type starts off. **Turn on…** waits for a second approver, like the setting **AI task types** ([[settings.organisation]]); the page links to the request.
- **Turn off** takes effect at once, from the next question.
- Only people who may change the organisation's settings can do either.

(src: docs/user-guide/ai-proposals.md#turn-a-task-type-on-or-off)
