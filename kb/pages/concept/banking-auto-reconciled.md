---
key: banking.auto-reconciled
type: concept
title: What Fyniq reconciled by itself, and undoing it
summary: The Auto-reconciled list: exact matches, a batch's payments, transfers' other sides and approved rules' lines, why and by whom, and undo
sources:
  - docs/user-guide/banking.md sha256:930af81aef70c44afbf4c03e78e113d8e6cfcf0e531ecd9c7c2f14a358ed5332
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - auto-reconciled
  - reconciled without a click
  - undo a match
  - weekly review sample
---

# What Fyniq reconciled by itself, and undoing it

## What it is

- **Auto-reconciled** lists what Fyniq reconciled without a click in the last 30 days and still stands: exact matches (the amount, the document's number and the contact), the payment of a payment batch whose number the line names when no other payment of that batch has its amount, the other side of a transfer, and lines an approved rule met ([[banking.rules]]).
- A line for a whole batch's total always waits for a person.
- Each shows why, and the principal and time that did it.
- **Undo it** puts the line back in the queue with the reason given, while its month is open: what it posted is reversed or voided on its own date.
- The page shows the limits Fyniq works within ([[settings.organisation]]); each week the 5 largest and 5 others picked at random join the weekly review.

(src: docs/user-guide/banking.md#what-fyniq-reconciled-by-itself)
