---
key: projects.time
type: task
title: Log your time and approve your team's
summary: How to log time in the week grid or with the timer, send the week, who approves it, and how to correct approved time
sources:
  - docs/user-guide/projects-time.md sha256:2953342f1500fe43eca4045f25d90218d9d489a952789109eb8fc2861700c809
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - timesheet
  - my time
  - log time
  - timer
  - approve time
  - time entry
---

# Log your time and approve your team's

## What it is

- **My time** and **Projects, Timesheet** are where time on projects is logged, a week at a time, and approved by a project's manager.
  Time is whole minutes, shown as hours and minutes, and posts nothing to the books. (src: docs/user-guide/projects-time.md#what-it-is)
- Time is logged only on a project the person is on, on one of its tasks in use; time on an internal project is never billable.
  (src: docs/user-guide/projects-time.md#what-it-is)

## Log a week and use the timer

- **My time** › **Add a row** (a project, then a task), type the time in each day (2:15 or 2.25 both mean 2 hours 15 minutes), add a
  note, then **Save the week**. Working days with no time show a warning under **Days with no time**.
  (src: docs/user-guide/projects-time.md#log-your-week)
- The timer turns into a draft entry on the day it started, to the nearest minute, at least one; one timer runs at a time.
  (src: docs/user-guide/projects-time.md#use-the-timer)

## Send the week and who approves it

- **Send the week for approval** sends each project's part to that project's managers; submitted time is locked until a manager
  approves it or returns it with a comment. (src: docs/user-guide/projects-time.md#send-the-week-for-approval)
- A manager who holds the **Approver** role approves the projects they manage; the **Controller** approves any project's time.
  Naming a manager gives no right of its own, and nobody approves their own time.
  (src: docs/user-guide/projects-time.md#approve-the-teams-time)

## Correct approved time

- Approved time never changes: **Correct it** adds a reversing entry with the opposite time, the right time is entered as a new entry,
  and the week is sent again. (src: docs/user-guide/projects-time.md#correct-approved-time)

Related: [[projects.projects]], [[projects.rates]].
