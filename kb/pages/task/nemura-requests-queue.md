---
key: nemura.requests-queue
type: task
title: Decide changes of details and answer questions in Nemura
summary: How the people team approves or refuses a change of details with its evidence and answers a question, and what approving an NRIC or FIN change does
sources:
  - docs/user-guide/nemura/requests-queue.md sha256:6f08e2843bfbc7f128e41cf6480fb1d1a64514e878a2822fa5c17864622380d4
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - approve a change of details
  - HR queue
  - answer an employee question
---

# Decide changes of details and answer questions in Nemura

## What it is

- **Requests** lists what waits for the people team: changes of legal name, residency or NRIC or FIN with evidence, and questions, soonest due first.
- Nobody decides their own change or answers their own question; another member of the people team does. (src: docs/user-guide/nemura/requests-queue.md#what-it-is)

## Steps

1. Open **Requests**; each row names the person, what they ask and the day due.
2. For a change, select **Read the evidence** and check it against what the person wrote.
3. Select **Approve**, or write the reason and select **Refuse**; a refusal needs a reason, which the person sees.
4. For a question, write the answer and select **Send the answer**.

(src: docs/user-guide/nemura/requests-queue.md#steps)

## What happens next

- Approving a legal name changes the record from the day of the decision; approving a residency starts a new legal classification from that day.
- Approving an NRIC or FIN change records only that the evidence was checked; record the new number under the person's restricted fields.
- A decided request keeps its words and evidence and is decided once. (src: docs/user-guide/nemura/requests-queue.md#what-happens-next)

## When it looks wrong

- Nobody decides their own change: ask another member of the people team.
- A residency change needs the person's legal classification: record it first. See [[nemura.person]].
- A decision refused as decided: someone decided first, reload the list. (src: docs/user-guide/nemura/requests-queue.md#common-problems)
