---
key: nemura.onboarding
type: task
title: Invite new people to Fyniq and record who is leaving in Nemura
summary: The task to invite a new person to Fyniq, how a leaving is recorded with its dates kept apart, and what the leave settlement shows
sources:
  - docs/user-guide/nemura/onboarding.md sha256:87fffad97dc9630d9f3393973edbc82996822fa7c06ed95512a93a8774c4bf9d
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - offboarding
  - resignation
  - last day
  - invite to Fyniq
  - joiner task
  - leave settlement
---

# Invite new people to Fyniq and record who is leaving in Nemura

## What it is

- When the company uses Fyniq, adding a person raises a task for whoever may invite people in Fyniq: invite them as an employee, so they can make claims, and mark the task done. Nemura never grants a Fyniq role itself.
- For a person who is leaving, Nemura keeps the dates that start statutory clocks apart, a checklist, and the annual leave they are left with on their last day. (src: docs/user-guide/nemura/onboarding.md#what-it-is)

## Steps

1. The HR administrator or the Owner switches on **Raise a task for each new person**; it raises no task for people already added.
2. Invite each person in Fyniq with the Employee role and mark the task done, or dismiss it for a person who will not claim expenses.
3. Under **Record a leaving**, choose the person and how the employment ends, enter the day notice was given or the company learned of the cessation and the last day of employment, and for a retrenchment the day employees were told. Tick off equipment returned, exit interview and Fyniq access removed, and save.
4. Ask for the person's service letter under their record; see [[nemura.letters.request]].

(src: docs/user-guide/nemura/onboarding.md#steps)

## What happens next

- The last day goes onto the record and prorates the annual leave; **Annual leave to the last day** shows entitled, taken and untaken days. Their worth and the final pay come from payroll. A negative figure means more was taken than the prorated entitlement.
- The day after the last day the account becomes the narrow one for the person's own documents; see [[nemura.leaver-account]].
- **Withdraw the leaving** before the last day if the person stays; after it, ask the Owner to correct the record. (src: docs/user-guide/nemura/onboarding.md#what-happens-next)

## When it looks wrong

- A date is refused: the last day is before the start, or notice is after the last day; the message names the field.
- The day employees were told is for a retrenchment only.
- No settlement: record the legal classification first; see [[nemura.person]]. (src: docs/user-guide/nemura/onboarding.md#common-problems)
