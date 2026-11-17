---
key: ai.proposals
title: Approve a change the AI proposed
mode: basic
roles: [owner, admin, controller, accountant, bookkeeper, company_secretary, dpo]
requirements: [AI-P3, AI-P4, AI-P5, AI-06, AI-07, AI-10, AI-14, AI-32, AI-89]
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

## Related

- [Approve a change](approvals.md), [The AI assistant](ai-assistant.md) and [Organisation settings](organisation-settings.md).
