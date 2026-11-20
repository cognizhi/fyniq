---
key: accounting.allocations
title: Allocate a shared cost to departments, projects or customers
mode: expert
roles: [accountant, controller, owner]
requirements: [FR-DIM-03, ALLOC-PRORATA]
status: draft            # written with allocation rules (P2); the headcount key's reading and the usage key's tie to the supplier's bill wait for the accountant's confirmation
---

# Allocate a shared cost to departments, projects or customers

## Find it

In Expert mode, open **Accounting** and choose **Allocations**. An Accountant saves and runs a rule; a Controller or Owner reads them.

## Save a rule

A rule names the **expense accounts** whose cost is shared, a **key** and the **targets** the cost goes to. A target is a department, a project or another dimension value. Only the cost that carries **no** dimension value is split, so a line already given to a department is never moved again.

- **Fixed percentages**: give each target a percentage above nothing, with at most two decimals. Together they are at most 100. What they leave stays unallocated.
- **Headcount of each department**: each target names a department, and the cost is split by the people of that department in the people projection on the last day of the month. People with no department are what the key cannot place.
- **Usage from a supplier's file**: each target has the code the file calls it by. Paste the file's rows (code, quantity, billed), the billed total and the ID of the supplier's bill it explains. The rows must add up to the total, and the total must equal the bill, or the file is refused. Rows whose code is no target's are what the key cannot place.

Each change is the rule's next version. **Put the rule out of use** when you no longer need it: it is not run again, and its runs stay.

## Preview and run it

Choose a month and **Preview**. For each account you see its cost with no dimension value, each target's part and the **unallocated** part. The parts are rounded by the largest-remainder method, so they and the unallocated part add to the cost exactly, and an odd cent goes to the target with the largest remainder, the earlier target first among equals.

**Propose the journal** makes a draft journal: each part debited to the account with its target's values, what was placed credited with none. What the key could not place stays on the account. A rule allocates a month once. The draft posts like any journal, when an approver posts it; nothing is posted by the rule.

Moving cost between dimension values changes no account's total, only who the cost is shown against.
