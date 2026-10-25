---
key: banking.rules
type: task
title: Make, test and approve bank rules
summary: Make a bank rule that suggests how to code recurring lines, test it against the lines held, and ask for it to apply by itself
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
  - bank rule
  - rule
  - auto-apply
  - back-test
  - rule order
  - rules to consider
---

# Make, test and approve bank rules

## Steps

- **Rules** (Expert mode) teach Fyniq how to code lines that come back, such as every AWS charge to Software; a rule suggests and does not post by itself.
- A rule meets a line by what its description contains or a regular expression it matches, its counterparty, money in or out, its size, its currency and its money account; it codes the line to an account, with a GST code and optionally a contact.
- Rules are in an order: the first rule a line meets is the one suggested ([[banking.categorise]]).
- **Test it** shows how many of the lines held the rule would have matched, and a few of them, before saving.
- **Ask to apply by itself** asks another person to let the rule reconcile the lines it meets without a click, within the organisation's auto-reconcile limits ([[settings.organisation]]); you confirm it's you first, changing the rule meanwhile stops the request, and a change afterwards needs a new approval.
- **Rules to consider** lists lines coded the same way several times; **Make it a rule** starts a rule from them, and none applies until saved.
- A rule that no longer reads, such as one whose account was turned off, says so ⚠ and matches nothing until changed; a rule an AI proposal made carries the AI label.

(src: docs/user-guide/banking.md#bank-rules)
