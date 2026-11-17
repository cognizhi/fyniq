---
key: ai.proposals
type: task
title: Review, approve or reject a change the AI proposed
summary: An AI proposal: Fyniq's summary apart from the AI's reasons, approving some items or all, rejecting with a reason, and what happens after
sources:
  - docs/user-guide/ai-proposals.md sha256:bb341528a034f597108ff1d7c003ba1d8a2d4d426e67e4a6945b4e226191607d
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
