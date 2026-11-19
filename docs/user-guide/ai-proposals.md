---
key: ai.proposals
title: Approve a change the AI proposed
mode: basic
roles: [owner, admin, controller, accountant, bookkeeper, company_secretary, dpo]
requirements: [AI-P3, AI-P4, AI-P5, AI-06, AI-07, AI-10, AI-14, AI-32, AI-89, AI-65]
status: draft
---

# Approve a change the AI proposed

## What it is

- The AI never changes the books itself. When an Owner has turned on a kind of change, a **task type**, in
  [Organisation settings](organisation-settings.md), the assistant may propose one for a person to approve. Only then
  does Fyniq make the change, in the approver's name.
- A proposal waits in the **Inbox** for 72 hours. After that it expires, and nothing changes.
- You see the proposals made for you and those you may approve. An Owner and the Data Protection Officer see every
  one.

## Review a proposal

- Open it from the **Inbox**. The **Summary** is written by Fyniq from the proposal and its dry run, not by the AI: it
  says what would change.
- **What changes** shows the dry run's result, and **Evidence** lists the records the AI relied on.
- **The AI's reasons** are shown apart, with the **AI** label. They are the AI's words: check them against the summary,
  which says what really happens.

## Approve or reject

- **Approve** signs exactly what the review shows, by its fingerprint (its SHA-256 hash). You confirm it's you first.
- For a proposal of several items, clear the ones you disagree with: the approval then covers only the items you keep.
  A routine proposal (tier 1) can be approved whole in one action; any other needs each item you approve named.
- **Reject** needs a reason. The assistant reads it with your next question in the same conversation.
- If you asked for it and it is routine (tier 1), you may approve it yourself.

## After you approve

- Fyniq makes the change as you, once. If a record the proposal relied on changed since its dry run, nothing is made:
  the proposal shows **Stale**, with why. Ask the assistant again.
- **Checks** flags a proposal that has waited more than 48 hours, so it is decided before it expires.

## The AI tasks page

- Open **AI tasks** from the **Inbox**. **Proposals** lists every proposal you may see, newest first; choose a state,
  such as **Waiting** or **Executed**, to show only those, and open one to see its review.
- **Catalogue** lists each task type by tier: what it does, the most items one proposal may hold, who approves it, how
  long a proposal waits before it expires, and whether it is on. A task type this release cannot run yet shows **Not
  in this release**.
- **Never proposed** lists what no task type can ever propose, whatever is turned on: approving anything, payments,
  a payee's bank details, users and roles, approval policies, the AI's own settings, statutory parameters, reopening a
  period, voiding a posted record, filing with IRAS, CPF or ACRA, exporting the company's data, the audit trail, shares
  and investor rights, a director's balance, officers and registers, and marking an obligation as met. A person does
  these.
- If the AI is switched off, the page says so: it proposes nothing until an Owner switches it on again.

## Create a compliance task

- **Create a compliance task** (`sec.create_task`, tier 1) puts a task in the Inbox with an owner and a due date, for
  something the AI's own cross-check found. The company secretary assistant's checks raise their own tasks themselves;
  this task type covers only what the AI suggests.
- A finding has one task: the AI cannot propose a second while one is open or waiting for approval.
- The Owner or the Company secretary approves it. Once approved, the task appears under the Inbox's tasks; it is a
  reminder for a person, and never files, approves or marks an obligation as met.

## Send a reminder the AI drafted

- **Draft a reminder about an obligation** (`sec.draft_reminder`, tier 1) is an email the AI drafts to the members who
  hold a role, such as the company secretary or the accountant, about a filing, a deadline or a step the company must
  take.
- The review shows the email as it would be sent and how many people it reaches. Nothing is sent until the Owner or the
  Company secretary approves it; then it goes once, to each member of that role with a sign-in email, and it ends by
  saying the AI drafted it and a person approved it.
- A role in which no one has a sign-in email cannot be reminded: the proposal says so and is not kept for approval.

## Review a corporate document the AI drafted

- **Draft a corporate document** (`sec.draft_document`, tier 1) is a resolution, a notice or minutes the AI asks Fyniq
  to draft from its own templates, filled from the company's records: the same papers as
  [the corporate profile's](corporate-profile.md) drafts.
- The review shows the paper's text as it would be drafted. Nothing is drafted until the Owner or the Company secretary
  approves it; then the draft appears in the corporate profile, saying the AI proposed it.
- A draft is never signed, filed or put in the minute book by the AI. A person has it signed and records the signed
  copy.
- If the directors, the secretary or the members change before it is approved, the text approved is no longer what
  would be drafted: the proposal is marked out of date and drafts nothing, and the AI can propose it again.

## Review an invoice the AI drafted

- **Draft an invoice from a project's unbilled work** (`ar.draft_invoice`, tier 1) is a draft invoice the AI asks Fyniq to
  make for a time-and-materials project it was told the code of: the same draft as
  [**Draft an invoice** on the project's Unbilled tab](projects-billing.md), with the project's approved time at its billing
  rates and its billable costs at cost plus markup.
- The review shows the customer, each line and the total, and says it is a draft: nothing is issued or posted, and the invoice
  follows your approval policies when someone issues it. Someone who may draft a project's invoice themselves, such as the
  AR specialist or the accountant, approves it.
- Nothing is drafted if time was approved, returned or billed, or a billing rate or markup changed, after the review: the
  proposal is marked out of date and the AI can propose it again. A project that is internal, or has no customer or nothing
  to bill, is refused with the reason. The AI never sees what anyone is paid.

## Turn a task type on or off

- Every task type starts off. **Turn on…** asks for a second approver, as the setting **AI task types** in
  [Organisation settings](organisation-settings.md) does; the page links to the request.
- **Turn off** takes effect at once: the AI stops proposing it from the next question.
- Only the people who may change the organisation's settings can turn a task type on or off.

## Related

- [Approve a change](approvals.md), [The AI assistant](ai-assistant.md) and [Organisation settings](organisation-settings.md).
