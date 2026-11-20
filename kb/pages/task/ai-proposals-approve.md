---
key: ai.proposals
type: task
title: Review, approve or reject a change the AI proposed
summary: Review, approve, reject or undo an AI proposal; the AI tasks page, catalogue and never-list; the tasks and reminders the AI may propose; task types on or off
sources:
  - docs/user-guide/ai-proposals.md sha256:2b17d1c50f70cbafb0c9690bd235a8094e53e6a4bc260aeba7eeac447b851cdb
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
  - undo an AI change
  - undo what the AI did
  - AI update contact
  - ct.update_contact
  - AI bank rule
  - bank.rule_upsert
  - AI recurring journal
  - recur.create_template
  - AI budget draft
  - plan.propose_budget
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

## Review an invoice the AI drafted

- Draft an invoice from a project's unbilled work (`ar.draft_invoice`, tier 1) is a draft invoice the AI asks Fyniq to make for a time-and-materials project it was told the code of: the same draft as Draft an invoice on the project's Unbilled tab ([[projects.billing]]), with approved time at billing rates and billable costs at cost plus markup.
- The review shows the customer, each line and the total, and says it is a draft: nothing is issued or posted, and the invoice follows the approval policies when someone issues it. Someone who may draft a project's invoice themselves, such as the AR specialist or the accountant, approves it.
- Nothing is drafted if time was approved, returned or billed, or a billing rate or markup changed, after the review: the proposal is marked out of date and the AI can propose it again. An internal project, or one with no customer or nothing to bill, is refused with the reason. The AI never sees what anyone is paid.

(src: docs/user-guide/ai-proposals.md#review-an-invoice-the-ai-drafted)

## Review a bill or an expense claim the AI drafted

- Draft a bill from a supplier's document (`ap.draft_bill`, tier 1) is a draft bill the AI asks Fyniq to make from an invoice or receipt it was given: supplier, the supplier's number, date, currency and each line's account, net amount, GST code and stated GST. It is the same draft as a new bill. Someone who may enter bills themselves, such as the AP specialist or the accountant, approves it.
- Draft an expense claim from receipts (`exp.draft_claim`, tier 1) is a draft of the requester's own claim. Only the person who asked can approve it, so no one else's claim is made for them.
- The review shows the lines, the total, how each line's input tax is decided and what would stop the bill being posted or the claim submitted. Both are drafts: nothing is posted or submitted, and the approval policies apply when a person posts the bill or submits the claim.
- The AI says only what the document states; when it is not sure of the kind of document the input tax is left out for a person to confirm. It never decides whether GST is claimable.
- A bill is out of date, and drafts nothing, if the supplier changed after the review. A supplier that is not an active supplier, an account that cannot be costed, a GST code that is not a purchase code and a category that is not a claim category are refused with the reason.

(src: docs/user-guide/ai-proposals.md#review-a-bill-or-an-expense-claim-the-ai-drafted)

## Review a contact change the AI proposed

- Update a contact's details (`ct.update_contact`, tier 2) changes up to six details of a customer or supplier company, as Change a contact does: the billing email, phone, address, payment terms, default category and default GST code. The review shows each detail before and after.
- It never changes a legal name, UEN or tax ID, Peppol ID, remittance email, country, tax residence, currency, discount terms, customer group or any bank detail, and never the record of a person (an individual, a director, a shareholder, an investor or a member who signs in): a person makes those changes. The AI cannot clear a detail.
- It is tier 2: someone other than the person who asked approves it, and that person must be able to change contacts themselves, such as the AR or AP specialist or the accountant. If the contact changed after the review, nothing is changed and the proposal is out of date.

(src: docs/user-guide/ai-proposals.md#review-a-contact-change-the-ai-proposed)

## Undo a change the AI made

- A change that changes or posts a record can be undone from the proposal's review with **Undo this change**: you say why, and Fyniq puts back what it replaced, as you, keeping who, when and why in the audit trail. You need the right the change itself needs.
- It works once, and only while nobody has changed the same record since; otherwise it is refused and says so, and a person corrects it by hand. A posted entry is undone by a reversing entry, never an edit, and needs the period to be open.
- A draft the AI made is not undone: it posts nothing, so a person edits or ignores it; a message sent cannot be unsent. The proposal stays **Executed**, and the review then says **Undone**, when and why.

(src: docs/user-guide/ai-proposals.md#undo-a-change-the-ai-made)

## Review a recurring journal the AI proposed

- Add a recurring journal (`recur.create_template`, tier 2) adds a recurring journal, as Add a recurring journal does: a name, the memo and balancing lines, whether each draft reverses on the first day of the next month, and a schedule of every 1, 3, 6 or 12 months on a day of the month. The scheduler then makes a draft journal on each occurrence, which a person posts under the usual approvals: nothing recurring posts by itself.
- Its lines keep to income, cost, prepayment, deferred revenue and depreciation accounts in the base currency: never a bank, equity, sub-ledger, tax, payroll or director's account. Its monthly equivalent is limited to S$5,000. A recurring bill or invoice is made by a person.
- It is tier 2: an Accountant other than the person who asked approves it. If an account it names changed after the review, nothing is made and the proposal is out of date. **Undo this change** pauses the template; drafts the scheduler already made stay for a person to post or ignore.

(src: docs/user-guide/ai-proposals.md#review-a-recurring-journal-the-ai-proposed)

## Review a budget the AI drafted

- Draft a budget version (`plan.propose_budget`, tier 2) drafts a budget for a financial year, as Draft a budget does: income and expense accounts with twelve monthly amounts, for the whole company. It is only a draft: not approved or recorded; a person asks for its approval, which records and locks it. A line for one department or project is a person's.
- An Accountant other than the person who asked approves it. A budget draft cannot be undone, since no function deletes one: a person edits it or leaves it.

(src: docs/user-guide/ai-proposals.md#review-a-budget-the-ai-drafted)

## Review a bank rule the AI proposed

- Create or change a bank rule (`bank.rule_upsert`, tier 1) saves a bank rule, as Create a bank rule and Change a bank rule do: a name, its place in the order, what a line must meet (a text or pattern in the description, the direction, an amount range, the currency, the counterparty) and the ordinary account it codes lines to, with an optional contact, GST code and reference.
- The review shows the rule's back-test: how many of the lines held its conditions would have matched, by status. A rule only suggests a coding, which a person accepts. The AI cannot make a rule apply by itself, nor code lines to a bank, receivables or payables account: applying by itself needs another person's approval after the back-test, and changing a rule stops it applying meanwhile. A rule the AI proposed is marked as such.
- Someone who may keep bank rules, such as the bookkeeper or the accountant, approves it. **Undo this change** switches a new rule off, or puts a changed rule back as it was, while nobody has changed it since.

(src: docs/user-guide/ai-proposals.md#review-a-bank-rule-the-ai-proposed)

## Approval quality

- The Approval quality tab on the AI tasks page, for the Owner and the DPO, shows how each month's AI proposals were decided: made, approved, rejected, expired, invalid or out of date; how many were approved within ten seconds of opening them or without opening them; the median time to a decision; and how many rejections gave a reason. A high share approved within seconds means proposals are stamped, not read.
- It also shows how many approvals were edited before approving (an item changed or removed), how many approved proposals were undone afterwards, and, for each task type, how many were approved, edited and undone and the median time to a decision, and what each person approved of each task type. A task type nobody ever edits, or one often undone, is worth a look.
- After a month ends the Owner or the DPO signs its review, with a note, after confirming it is them. A month is signed once, and the signed review is kept as evidence.

(src: docs/user-guide/ai-proposals.md#approval-quality)

## Turn a task type on or off

- Every task type starts off. **Turn on…** waits for a second approver, like the setting **AI task types** ([[settings.organisation]]); the page links to the request.
- **Turn off** takes effect at once, from the next question.
- Only people who may change the organisation's settings can do either.

(src: docs/user-guide/ai-proposals.md#turn-a-task-type-on-or-off)
