---
key: ai.proposals
title: Approve a change the AI proposed
mode: basic
roles: [owner, admin, controller, accountant, bookkeeper, company_secretary, dpo]
requirements: [AI-P3, AI-P4, AI-P5, AI-06, AI-07, AI-10, AI-14, AI-31, AI-32, AI-89, AI-91, AI-65]
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

## Review a bill or an expense claim the AI drafted

- **Draft a bill from a supplier's document** (`ap.draft_bill`, tier 1) is a draft bill the AI asks Fyniq to make from an invoice
  or receipt you gave it: the supplier, the supplier's own number, the date, the currency and each line's account, net amount,
  GST code and the GST the document states. It is the same draft as **New bill**. Someone who may enter bills themselves, such
  as the AP specialist or the accountant, approves it.
- **Draft an expense claim from receipts** (`exp.draft_claim`, tier 1) is a draft of your own claim, from receipts you gave the
  AI. It is always the claim of the person who asked, and only that person can approve it, so no one else's claim is made for
  them.
- The review shows the lines, the total, how each line's input tax is decided and what would stop the bill being posted or the
  claim submitted. Both are drafts: nothing is posted or submitted, and the usual approval policies apply when a person posts
  the bill or submits the claim.
- The AI says only what the document states. If it is not sure what kind of document it is, the input tax is left out: you
  confirm it from the document before the bill is posted. It never decides whether GST is claimable.
- A bill is marked out of date, and drafts nothing, if the supplier changed after the review. A supplier that is not an active
  supplier, an account that cannot be costed, a GST code that is not a purchase code and a category that is not one of your
  claim categories are refused with the reason.

## Review a contact change the AI proposed

- **Update a contact's details** (`ct.update_contact`, tier 2) changes up to six details of a customer or supplier company, as
  **Change a contact** does: the billing email, phone, address, payment terms, default category and default GST code. The
  review shows each detail before and after.
- It never changes a legal name, UEN or tax ID, Peppol ID, remittance email, country, tax residence, currency, discount terms,
  customer group or any bank detail, and never the record of a person (an individual, a director, a shareholder, an investor
  or a member who signs in): a person makes those changes. A detail cannot be cleared by the AI.
- It is tier 2, so someone other than the person who asked approves it, and that person must be someone who may change contacts
  themselves, such as the AR or AP specialist or the accountant. If the contact changed after the review, nothing is changed:
  the proposal is marked out of date.

## Undo a change the AI made

- A change that **changes or posts a record** can be undone from the proposal's review, with **Undo this change**: you say why,
  and Fyniq puts back what it replaced, as you, and keeps who, when and why in the audit trail. You need the right the change
  itself needs, so someone who could not have made it cannot undo it.
- It works once, and only while nobody has changed the same record since. If a person or another change has touched the contact
  after, the undo is refused and says so: correct it by hand. For a posted entry the undo is a reversing entry, never an edit,
  and needs the period to be open.
- A **draft** the AI made is not undone: it posts nothing, so you edit it or ignore it. A message that was sent cannot be unsent.
  The proposal itself stays **Executed**, and the review then says **Undone**, when and why.

## Review a recurring journal the AI proposed

- **Add a recurring journal** (`recur.create_template`, tier 2) adds a recurring journal, as **Add a recurring journal** does: a name,
  the memo and balancing lines, whether each draft reverses on the first day of the next month, and a schedule of every 1, 3, 6 or 12
  months on a day of the month. The scheduler then makes a draft journal on each occurrence, which a person posts under the usual
  approvals: nothing recurring posts by itself.
- Its lines keep to income, cost, prepayment, deferred revenue and depreciation accounts in your base currency: never a bank, equity,
  sub-ledger, tax, payroll or director's account. Its monthly equivalent (the debits over the months between occurrences) is limited to
  S$5,000. A recurring bill or invoice is made by a person.
- It is tier 2: an Accountant other than the person who asked approves it. If an account it names changed after the review, nothing is
  made and the proposal is out of date. **Undo this change** pauses the template; drafts the scheduler already made stay for a person to
  post or ignore.

## Review a budget the AI drafted

- **Draft a budget version** (`plan.propose_budget`, tier 2) drafts a budget for a financial year, as **Draft a budget** does: income
  and expense accounts with the twelve monthly amounts, for the whole company. It is only a draft: it is not approved or recorded, and a
  person asks for its approval, which records and locks it. A line for one department or project is a person's.
- An Accountant other than the person who asked approves it. A budget draft cannot be undone, since no function deletes one: you edit it
  or leave it.

## Review a bank rule the AI proposed

- **Create or change a bank rule** (`bank.rule_upsert`, tier 1) saves a bank rule, as **Create a bank rule** and **Change a bank rule** do: a name, its
  place in the order, what a line must meet (a text or pattern in the description, the direction, an amount range, the currency, the counterparty) and
  the ordinary account it codes lines to, with an optional contact, GST code and reference.
- The review shows the rule's **back-test**: how many of the lines held its conditions would have matched, by their status. A rule only suggests a coding,
  which you accept. The AI cannot make a rule apply by itself, and cannot code lines to a bank, receivables or payables account: letting a rule apply by itself
  needs another person's approval after its back-test, and changing a rule stops it applying meanwhile. A rule the AI proposed is marked as such.
- Someone who may keep bank rules, such as the bookkeeper or the accountant, approves it. **Undo this change** switches a new rule off, or puts a changed
  rule back as it was, while nobody has changed it since.

## Approval quality

- The **Approval quality** tab on the AI tasks page, for the Owner and the DPO, shows how each month's AI proposals were decided: how many were made,
  approved, rejected, expired, invalid or out of date; how many were approved within ten seconds of opening them, or without opening them; the median time
  to a decision; and how many rejections gave a reason. A high share approved within seconds means proposals are stamped, not read.
- After a month ends, the Owner or the DPO signs its review (after confirming it is them), with a note. A month is signed once, and the signed review is kept as evidence.

## Turn a task type on or off

- Every task type starts off. **Turn on…** asks for a second approver, as the setting **AI task types** in
  [Organisation settings](organisation-settings.md) does; the page links to the request.
- **Turn off** takes effect at once: the AI stops proposing it from the next question.
- Only the people who may change the organisation's settings can turn a task type on or off.

## Related

- [Approve a change](approvals.md), [The AI assistant](ai-assistant.md) and [Organisation settings](organisation-settings.md).
