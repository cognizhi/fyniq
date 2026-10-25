---
key: banking.categorise
type: task
title: Categorise and match the waiting bank lines
summary: Accept, change or skip each waiting line's suggestion, run Reconcile now, and in Expert split, transfer, match or suspend lines
sources:
  - docs/user-guide/banking.md sha256:49aa643d2012253039f4a0e3060e4259e28c7e64c6a31f00c0e6e0891e5e4368
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-26
aliases:
  - categorise
  - bank queue
  - reconcile a bank line
  - match a payment
  - accept a suggestion
  - Reconcile now
  - split a bank line
  - transfer between accounts
  - suspense
---

# Categorise and match the waiting bank lines

## One line at a time

- **Categorise** lists the lines waiting for a person: new lines, possible duplicates and lines in suspense; Fyniq never applies a suggestion by itself.
- Each line shows whether money came in or went out, its amount, and its description as the bank sent it.
- **What is this?** lists the suggestions, best first: money already recorded by hand, an open invoice or bill whose number the line names, one of the amount the line's counterparty owes, then the first bank rule the line meets.
- **Accept** posts the line as the chosen suggestion says; **Change the category** posts the whole line to the chosen account with its GST code and, optionally, its contact; **Skip** moves on and comes back later.
- **Reconcile now** lets Fyniq reconcile the exact matches among the waiting lines by itself, within the organisation's auto-reconcile limits ([[settings.organisation]]), and says how many it left and why; lines that arrive by email or from a connector wait for it.
- A possible duplicate ⚠ says so (compare it on the account first, [[banking.accounts]]); a line in suspense ⚠ is posted to suspense (2999) until it is categorised or matched.

(src: docs/user-guide/banking.md#categorise-the-waiting-lines)

## In Expert mode

- **Grid view** lists every waiting line with its best suggestion, to select several and accept their suggestions at once; each line posts on its own, so one refused never stops the others.
- **Split the line** posts it to several accounts, each part with its amount, one part possibly taking the rest.
- **Mark as a transfer** matches money moved between two of the company's accounts to the other side's line, or leaves it in transit until that line arrives.
- **Match to documents** settles one customer's invoices or one supplier's bills with the line, several at once; what it pays beyond them stays as a credit with them, and a small shortfall settled in full clears within the settlement tolerance.
- **Suspense** posts the line to suspense until its purpose is known.
- Rules that suggest a category: [[banking.rules]]. What Fyniq reconciled by itself: [[banking.auto-reconciled]].

(src: docs/user-guide/banking.md#categorise-the-waiting-lines)
