---
key: nemura.my-leave
type: task
title: See a Nemura leave balance and how it was worked out
summary: What the My leave page shows, how annual leave is pro-rated and carried forward, what the people team can do on it, and what common refusals mean
sources:
  - docs/user-guide/nemura/my-leave.md sha256:61688fec0fd2fe29d8d2f160f14debe154a25c7eb9a0636abd266e8cc9a5246a
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - My leave
  - leave balance
  - working pattern
  - carry forward leave
  - pro-rated annual leave
  - leave ledger
---

# See a Nemura leave balance and how it was worked out

## What it is

- **My leave** shows a year's leave: **entitled**, **taken**, **booked**, **expired** and **left**, with the steps of the calculation and where the days come from (the country pack and the company's policy).
- The leave year is the calendar year, and annual leave is counted in the person's working days, set by their **working pattern**. (src: docs/user-guide/nemura/my-leave.md#what-it-is)

## What to do

1. Sign in ([[nemura.sign-in]]); Leave must be switched on, by the Owner or the HR administrator under **Features** ([[nemura.features]]).
2. Select **My leave** and read the table and **How it was worked out**.
3. The statute's annual leave days are pro-rated by completed months of service in the year and rounded to a whole day: under one half down, a half or more up.
4. **Previous year** and **Next year** move between years.
5. Unused annual leave is carried forward and can be taken until the end of the twelfth month after the year ends; days taken in the new year use the carried days first, and the rest expires.

(src: docs/user-guide/nemura/my-leave.md#steps)

## For the people team

- From a person's record, **Leave and working pattern** shows their leave. The HR officer and the HR administrator can **Work out annual leave** (run again, it writes only the difference as a correction), **Carry unused days forward** and **Set pattern** from a day; a working pattern is history, so a new one ends the old one the day before.
- Nothing is edited or deleted: every grant, correction, carry-forward and expiry is a new ledger entry, and each is audited. (src: docs/user-guide/nemura/my-leave.md#for-the-people-team)

## When it looks wrong

- **No leave granted for this year yet**: the people team has not worked out the year's annual leave.
- **No legal classification**: set it on the person's record first ([[nemura.person]]).
- **No country pack covers the year**: choose a year from 2026 on ([[nemura.packs]]).
- **Under three months of service**: annual leave starts at three completed months, and the earlier months count. (src: docs/user-guide/nemura/my-leave.md#common-problems)
