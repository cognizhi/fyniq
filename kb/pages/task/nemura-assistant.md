---
key: nemura.assistant
type: task
title: Turn on the people assistant and decide its drafts in Nemura
summary: The people assistant is off until the Owner turns it on, only drafts for a person to approve, and never decides, issues, pays or changes a record
sources:
  - docs/user-guide/nemura/assistant.md sha256:f6d2166f407fb7d0520d6ccbbed9951b60fb0da78fc2305ebf81a227a59a6fa1
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - people assistant
  - ai assistant
  - ai draft
  - assistant settings
  - never list
---

# Turn on the people assistant and decide its drafts in Nemura

## What it is

- The people assistant drafts letters and replies to flexible-work requests for the people team to edit and approve. It is off until the Owner turns it on. (src: docs/user-guide/nemura/assistant.md#what-it-is)
- It never approves or refuses leave or a request, changes pay, a bank account, a role or a record, issues or signs a letter, moves a candidate, decides a hiring, rating, promotion or termination, or reads another person's restricted data. Each draft is a proposal that a named person decides, and one nobody decides in 72 hours expires. (src: docs/user-guide/nemura/assistant.md#what-it-is)

## Steps

1. Open Settings, The people assistant. It starts off, with no route and no kind of draft on.
2. The Owner turns it on, chooses the route and ticks each kind of draft, then saves with a passkey.
3. Read each draft under Drafts waiting for a person, then approve it or give a reason and do not use it.

(src: docs/user-guide/nemura/assistant.md#steps)

## What happens next

- Approving keeps the draft exactly as seen; only that text is used. The person the assistant acted for cannot decide their own draft. The audit trail holds a fingerprint of the text, never the text. Own-pay explanations stay off until a second approver exists, which arrives with the Fyniq link: the assistant explains a payslip line from its rule and the page shows the figures. (src: docs/user-guide/nemura/assistant.md#what-happens-next)

## When it goes wrong

- Off: the Owner turns it on. Cannot approve: it waited more than 72 hours, was drafted for you, or you are not the HR officer, HR administrator or Owner. (src: docs/user-guide/nemura/assistant.md#common-problems)
- Related: [[nemura.letters.request]]. (src: docs/user-guide/nemura/assistant.md#related)
