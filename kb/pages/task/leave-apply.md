---
key: leave.apply
type: task
title: Apply for leave in Nemura
summary: How an employee asks for annual leave in Nemura, what the form shows before it is sent, how the days are counted and reserved, and what happens next
sources:
  - docs/user-guide/nemura/leave-apply.md sha256:fbac1061de4298344d9f1d80c09ae71768462582a996d8be3dbf42ff252b9c01
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - annual leave
  - take a day off
  - half day leave
  - leave request
  - request leave
---

# Apply for leave in Nemura

## Where

- Annual leave is asked for from **Apply for leave**, reached from **My leave** ([[nemura.my-leave]]). Before sending, the form shows what the request costs day by day, the balance after it and who else in the team is away. (src: docs/user-guide/nemura/leave-apply.md#what-it-is)
- Annual leave must be granted for the year first, and the people team must have set the working pattern so the days can be counted. A half day is allowed on the first day, the last day or both. (src: docs/user-guide/nemura/leave-apply.md#before-you-start)

## Steps

1. Open **Apply for leave** and choose the **First day** and the **Last day** (leave the last day empty for one day).
2. Tick **Only half of the first day** or **Only half of the last day** for a half day.
3. Read **How the days were counted**: public holidays and rest days cost nothing, and a holiday on a rest day moves to the next working day. Check the balance after the request.
4. **Away in your team on those days** shows names and dates, never the type; it is information only.
5. Add a note and select **Send to my manager**.

(src: docs/user-guide/nemura/leave-apply.md#steps)

## After sending

- The line manager approves or refuses; a refusal always comes with a reason and the days come back ([[leave.approve]]).
- The days are reserved when the request is sent, so two requests cannot both take the last days.
- A request still waiting, or an approved one before it starts, can be cancelled; leave that has started is corrected by the people team.

(src: docs/user-guide/nemura/leave-apply.md#what-happens-next)

## Common problems

- A request beyond the balance is refused and the days left are named.
- Leave already asked for on some of the days: cancel it first or choose other days.
- A request over the new year is asked for each year on its own.
- No working pattern set: ask the people team.

(src: docs/user-guide/nemura/leave-apply.md#common-problems)

## Related

- Sick leave is told, not applied for: [[leave.report-sick]]. (src: docs/user-guide/nemura/leave-apply.md#related)
