---
key: ai.proposals
type: task
title: Review, approve or reject a change the AI proposed
summary: Review, approve or reject an AI proposal; the AI tasks page, catalogue and never-list; the tasks and reminders the AI may propose; task types on or off
sources:
  - docs/user-guide/ai-proposals.md sha256:214d6d51c38df6c3c7bca5d20a9ffc886bd31873138be3051c5ec9e6f27b0e90
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
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
  - AI suggested task
  - sec.create_task
  - AI drafted reminder
  - sec.draft_reminder
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

## Create a compliance task

- **Create a compliance task** (`sec.create_task`, tier 1) puts a task in the Inbox, with an owner and a due date, for something the AI's own cross-check found; the company secretary assistant's checks raise their own tasks themselves.
- One task per finding: the AI cannot propose a second while one is open or waiting for approval.
- The Owner or the Company secretary approves it; the task is a reminder for a person and never files, approves or marks an obligation as met.

(src: docs/user-guide/ai-proposals.md#create-a-compliance-task)

## Send a reminder the AI drafted

- **Draft a reminder about an obligation** (`sec.draft_reminder`, tier 1) is an email the AI drafts to the members holding a role, such as the company secretary or the accountant, about a filing, a deadline or a step the company must take.
- The review shows the email as it would be sent and how many people it reaches; nothing is sent until the Owner or the Company secretary approves it, then it goes once to each member of that role with a sign-in email, ending by saying the AI drafted it and a person approved it.
- A role in which no one has a sign-in email cannot be reminded: the proposal says so and is not kept.

(src: docs/user-guide/ai-proposals.md#send-a-reminder-the-ai-drafted)

## Review a corporate document the AI drafted

- **Draft a corporate document** (`sec.draft_document`, tier 1): a resolution, notice or minutes the AI asks Fyniq to draft from its templates, filled from the records, as in [[compliance.corporate-profile]].
- The review shows the paper's text; nothing is drafted until the Owner or the Company secretary approves, and then the draft appears in the corporate profile.
- The AI never signs, files or puts a draft in the minute book; a person records the signed copy.
- If the directors, the secretary or the members change before approval, the proposal is out of date and drafts nothing.

(src: docs/user-guide/ai-proposals.md#review-a-corporate-document-the-ai-drafted)

## Turn a task type on or off

- Every task type starts off. **Turn on…** waits for a second approver, like the setting **AI task types** ([[settings.organisation]]); the page links to the request.
- **Turn off** takes effect at once, from the next question.
- Only people who may change the organisation's settings can do either.

(src: docs/user-guide/ai-proposals.md#turn-a-task-type-on-or-off)
