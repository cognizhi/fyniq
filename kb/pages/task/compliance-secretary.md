---
key: compliance.secretary
type: task
title: Use the company secretary assistant
summary: What the AI company secretary assistant checks, what needs you, its drafts and runs, a run's report, Run now and pause, and its schedule
sources:
  - docs/user-guide/company-secretary.md sha256:7c96932a9f00a69127dca529576e0f6284291355c7189c54836143e612f52ad5
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-15
aliases:
  - company secretary assistant
  - AI company secretary
  - scheduled agent
  - not assessed
  - run now
  - pause the assistant
  - run report
---

# Use the company secretary assistant

## What it is

- An AI agent that, on its schedule or when something happens, checks the company's obligations, explains what it finds, raises tasks and drafts reminders for a person to approve.
- It is not the company secretary and never files, signs, approves or marks an obligation as met: people do.
- Open it from **Compliance → Company secretary assistant**. It is off until an Owner turns it on in Organisation settings with a second approver ([[settings.organisation]]); its runs then act for that Owner, reading and proposing as them, never more. Everything it writes is labelled **AI**.

(src: docs/user-guide/company-secretary.md#what-it-is)

## What needs you

- The open findings of its latest finished run, with what to do next and by when: **Not assessed** first, naming what is missing, then what cannot go on, what is due and what to review.
- Expert mode adds each finding's rule, statutory pack and source.
- The summary never says all is well while anything is open or not assessed.

(src: docs/user-guide/company-secretary.md#what-needs-you)

## Coming up and drafts

- **Coming up** shows the next items of the statutory calendar ([[compliance.calendar]]).
- **Drafts awaiting approval** are the tasks and reminders it proposed that wait for a person; open one to approve or reject it ([[ai.proposals]]).

(src: docs/user-guide/company-secretary.md#coming-up-and-drafts)

## Its runs and a run's report

- **Its runs** lists each run, newest first, with what started it, what it found and whether that is **new since the last run**; a failed or skipped run says why.
- A run's report shows the books' revision and statutory packs it read, every finding with its rule and source, their tasks and what it proposed; the model's words, when it asked the model, are shown apart and labelled **AI**.

(src: docs/user-guide/company-secretary.md#its-runs)
(src: docs/user-guide/company-secretary.md#a-runs-report)

## Run now and pause

- **Run now** queues a run at once; the Owner and the Administrator may ask for one while it is on.
- **Pause** stops its runs at once and keeps its schedule; **Resume** starts them again. The statutory calendar's reminders keep coming while it is paused.

(src: docs/user-guide/company-secretary.md#run-now-and-pause)

## Its schedule

- When it runs, the events that start a run, its quiet hours, who it tells and how, and the most reads and tokens one run may use.
- An Owner changes them in Organisation settings; each change but pausing waits for a second approver ([[settings.organisation]]).

(src: docs/user-guide/company-secretary.md#its-schedule)
