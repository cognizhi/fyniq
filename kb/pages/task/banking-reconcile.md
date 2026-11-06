---
key: banking.reconcile
type: task
title: Reconcile a money account for a month, and sign it off
summary: Prepare a month's bank reconciliation from the statement, explain any difference, and have another person sign it off
sources:
  - docs/user-guide/banking.md sha256:09023df1241255bcf5a46d44d41cd5c9e5aecc685ccacc21b6ec0b25c3dcfa28
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - bank reconciliation
  - reconcile the bank
  - sign off a reconciliation
  - statement balance
  - difference
---

# Reconcile a money account for a month, and sign it off

## Steps

- **Reconcile** (Expert mode) proves the books agree with the bank for a month: one person prepares it, another signs it off.
- **Prepare it** takes the closing balance of the latest statement imported up to the month's end and the books' balance on that day; in Standard mode the e-statement's balance may be typed instead, while in Founder mode it comes only from an imported statement or feed.
- It shows the statement's balance, the books' balance, the lines the books do not hold yet, and the **difference** they leave; **Ready to sign off** means the difference is nil, otherwise categorise the waiting lines ([[banking.categorise]]) and prepare it again.
- The Controller **signs it off** after confirming it's them, never one they prepared; once signed off it is locked with the month.
- Lines left in suspense are counted apart: categorise them before the month closes.

(src: docs/user-guide/banking.md#reconcile-an-account-for-a-month)
