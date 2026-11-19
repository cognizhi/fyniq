---
key: nemura.leave.government-paid
type: task
title: Record government-paid leave and track the employer's claim in Nemura
summary: How the people team records maternity, paternity and shared parental leave, the rules that apply, and how the employer's reimbursement claim is tracked
sources:
  - docs/user-guide/nemura/leave-government-paid.md sha256:1802f49edfbb9cc25ee35e21e1bf3d6e35da194d24e24c883bef48c82eec1359
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - maternity leave
  - paternity leave
  - shared parental leave
  - government-paid leave claim
  - reimbursement claim
  - claim deadline
---

# Record government-paid leave and track the employer's claim in Nemura

## What it is

- **Government-paid leave** lists each block of maternity, paternity or shared parental leave with the day the employer's claim must be filed by and the claims filed for it.
- A person files the claim on the Government-Paid Leave portal; Nemura keeps the record and moves no money, and the salary owed never depends on the reimbursement.

(src: docs/user-guide/nemura/leave-government-paid.md#what-it-is)

## Rules that apply

- The HR officer and the HR administrator record leave and claims; the people team, the Owner and Payroll read. The child must be declared among the dependants with a date of birth ([[nemura.person]]).
- For a child born in 2026: maternity leave is 16 weeks (the employer pays the first 8 for a first or second child; the Government reimburses the last 8 weeks, capped at S$10,000 for each 4 weeks and S$20,000 in all); paternity leave is 4 weeks, reimbursed up to S$2,500 a week and S$10,000 in all; shared parental leave is 10 weeks for children born from 1 April 2026 and 6 weeks for children born from 1 April 2025 to 31 March 2026, up to S$2,500 a week.
- The child must be a Singapore Citizen and the employee must have served 3 months before the birth. The values have not yet been checked by a second person; see [[nemura.packs]].

(src: docs/user-guide/nemura/leave-government-paid.md#before-you-start)

## Steps

1. Under **Record a block of leave**, choose the person, the child, the scheme, the child's order and the first and last day.
2. Nemura refuses a block beyond the scheme's weeks, outside its window, or overlapping other leave; each block shows its claim deadline, 3 months after the last day.
3. After the claim is filed on the portal, select **Record claim**: amount, day, reference. An amount above the scheme's cap is recorded and flagged, never refused.
4. Record the portal's answer under the claim: **Received** (partial is fine) or **Rejected**.

(src: docs/user-guide/nemura/leave-government-paid.md#steps)

## When it looks wrong

- A non-citizen child: the employer pays any leave the company gives.
- Under 3 months' service before the birth: the scheme asks for 3 months.
- A near deadline appears in what needs a look when no claim is filed within 30 days of it.
- A child with no date of birth: record it under the dependants first.

(src: docs/user-guide/nemura/leave-government-paid.md#common-problems)
