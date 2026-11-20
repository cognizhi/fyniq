---
key: accounting.allocations
type: task
title: Allocate a shared cost to departments, projects or customers
summary: Saving an allocation rule, its three keys, previewing a month and running it into a draft journal, and what stays unallocated
sources:
  - docs/user-guide/allocations.md sha256:dc06abdba3a938ebb65e89c47c4dfc3a8d61e56da73e68632552752710b8fc81
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - allocation rule
  - allocate shared costs
  - split a cost by headcount
  - unallocated cost
---

# Allocate a shared cost to departments, projects or customers

## Find it

- In Expert mode, **Accounting** has **Allocations**. An Accountant saves and runs a rule; a Controller or Owner reads them.

(src: docs/user-guide/allocations.md#find-it)

## Save a rule

- A rule names the expense accounts whose cost is shared, a key and the targets (departments, projects or other dimension values). Only cost that carries no dimension value is split, so a line already given to a department is never moved again.
- **Fixed percentages**: each target has a percentage above nothing with at most two decimals, together at most 100; what they leave stays unallocated.
- **Headcount**: each target names a department and the cost is split by its people in the people projection on the last day of the month; people with no department are what the key cannot place.
- **Usage**: each target has the code a supplier's file calls it by. The file's rows must add up to its billed total and the total must equal the supplier's bill it explains, or the file is refused; rows whose code is no target's are what the key cannot place.
- Each change is the rule's next version; a rule put out of use is not run again and its runs stay.

(src: docs/user-guide/allocations.md#save-a-rule)

## Preview and run it

- A preview shows each account's cost with no dimension value, each target's part and the unallocated part. The parts are rounded by the largest-remainder method, so they and the unallocated part add up to the cost exactly, an odd cent going to the largest remainder, the earlier target first among equals.
- **Propose the journal** makes a draft: each part debited to the account with its target's values, what was placed credited with none, so what the key could not place stays on the account. A rule allocates a month once, and the draft posts like any journal on an approver's say; the rule posts nothing itself.
- Moving cost between dimension values changes no account's total, only who it is shown against.

(src: docs/user-guide/allocations.md#preview-and-run-it)
