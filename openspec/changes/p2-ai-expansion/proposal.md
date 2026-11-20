# Proposal

## Why

P1 built the AI's proposal engine and, with it, a first reading of how people decide what the AI proposes (AI-92). The P2 rows ask for the rest:
the approval-quality metrics PRD AI-33 names (the edit-before-approve rate, the reversals after approval and the breakdown per task type and
approver, so a rubber stamp shows where it happens), and a way for a conversation to leave know-how behind without ever filing an answer by itself
(AI-59: the AI may propose a know-how page, which an Owner or Administrator approves).

## What Changes

- **Approval-quality metrics** (AI-33): the month's report gains the share of approvals edited before approving, the approved proposals undone
  afterwards, each task type's approvals, edits, reversals and median time to decide, and what each person approved of each task type. The read
  stays the Owner's and the DPO's, computed from the proposals' own records.
- **`kb.propose_page`** (AI-59): a tier-1 task type that proposes a know-how page from a conversation, without figures; an Owner or Administrator
  approves it; nothing becomes a page by itself. Its schema refuses figures, and its red-team case proves it.

## Capabilities

### Modified Capabilities
- `ai/proposals` (AI-33), `ai/knowledge-base` (AI-59)

## Impact

AI-33 adds fields to an existing read-only report and one join in a read query: no new table, no new grant. `kb.propose_page` needs a catalogue entry,
a column-limited executor grant on the knowledge base's notes and an undo (each of which is listed for review), evals and a red-team case; it
widens nothing the AI can reach: the AI reads and proposes only, as the never-list requires (§9.6).
