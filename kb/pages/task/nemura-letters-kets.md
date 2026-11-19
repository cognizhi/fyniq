---
key: nemura.letters.kets
type: task
title: Record a person's key employment terms and issue their KETs in Nemura
summary: Who records which key employment terms, how the KETs letter is asked for, checked and signed, and when Checks reminds the people team
sources:
  - docs/user-guide/nemura/letters-kets.md sha256:c8dc38fb56d14782fcdaf14098aa63f9ea8cf0d59b36fa09e344deb7761e083e
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - KETs
  - key employment terms
  - employment terms in writing
  - written terms of employment
---

# Record a person's key employment terms and issue their KETs in Nemura

## What it is

- The Employment Act asks the employer to give an employee covered by the Act the key employment terms in writing, no later than 14 days after the employment starts, when it lasts 14 days or more.
- MOM's guidelines list 17 items. Nemura keeps the list in its country pack and fills it from the person's record and the terms recorded here; an item that does not apply says so in words. (src: docs/user-guide/nemura/letters-kets.md#what-it-is)

## Before you start

- The letterhead is set, the **Key employment terms** type is on and its template is published; see [[nemura.letters.templates]].
- The person's record, working pattern and legal classification are recorded; the letter reads the working days and hours, and whether overtime applies, from them. (src: docs/user-guide/nemura/letters-kets.md#before-you-start)

## Steps

1. Under the person's **Key employment terms**, select **Record the terms**.
2. The people team writes the main duties, the notice period, the other medical benefits and, for a fixed-term contract, its last day.
3. The Payroll administrator or the Owner writes the pay: the salary period, the basic salary, the fixed allowances and deductions, the overtime payment period if it is not the salary period, and other pay. The people team does not see or write it.
4. Select **Ask for the KETs letter**. A missing fact is named; record it and ask again.
5. Another member of the people team checks the letter and the Owner signs it; see [[nemura.letters.issue]].

(src: docs/user-guide/nemura/letters-kets.md#steps)

## What happens next

- New terms start on a day you choose and end the earlier ones the day before; a record of terms is history and never changes.
- **Checks** shows a person who started 14 or more days ago with no issued KETs, and a reminder in the 3 days before.
- The audit trail says that pay terms were set, never what they were. (src: docs/user-guide/nemura/letters-kets.md#what-happens-next)

## When it looks wrong

- The pay terms are not recorded: ask the Payroll administrator or the Owner to record them.
- The new terms start too early: choose a day after the current terms began.
- The overtime item says it does not apply: the classification and the salary decide it; check the classification. See [[nemura.person]]. (src: docs/user-guide/nemura/letters-kets.md#common-problems)
