---
key: compliance.secretary
title: The company secretary assistant
mode: basic
roles: [owner, administrator, controller, accountant, auditor, tax_agent, board_viewer, company_secretary]
requirements: [FR-CS-04, AI-62, AI-63, AI-64, AI-65, AI-67]
status: draft
---

# The company secretary assistant

## What it is

- The **company secretary assistant** is an AI agent. On its schedule, or when something happens, it checks the
  company's obligations, explains what it finds, raises tasks and drafts reminders for a person to approve.
- It is not the company secretary. It never files, signs, approves or marks an obligation as met: people do.
- Open it from **Compliance → Company secretary assistant**. It is off until an Owner turns it on in
  [Organisation settings](organisation-settings.md), and a second approver confirms it.
- Its runs act for the Owner who turned it on: it reads and proposes as them, never more. Everything it writes is
  labelled **AI**.

## What needs you

- **What needs you** lists the open findings of its latest finished run, with what it says, what to do next and by
  when. Those it could not check come first, as **Not assessed**, naming what is missing; then what cannot go on, what
  is due and what to review.
- In Expert mode each finding also shows its rule, the statutory pack and the source.
- The summary never says all is well while anything is open or not assessed.

## Coming up and drafts

- **Coming up** shows the next items of the [statutory calendar](statutory-calendar.md).
- **Drafts awaiting approval** are the tasks and reminders it proposed that wait for a person. Open one to review and
  approve or reject it ([Approve a change the AI proposed](ai-proposals.md)).

## Its runs

- **Its runs** lists each run, newest first: what started it, when, what it found and whether it is **new since the
  last run**. A run that failed or was skipped says why.

## A run's report

- A run's report shows when and why it ran, the books' revision and the statutory packs it read, every finding with
  its rule and source, the tasks they have and what it proposed.
- When it asked the model, the model's words are shown apart, labelled **AI**. When nothing it reads had changed, the
  model was not asked.

## Run now and pause

- **Run now** queues a run at once. The Owner and the Administrator may ask for one while it is on.
- **Pause** stops its runs at once and keeps its schedule; **Resume** starts them again. The statutory calendar's
  reminders keep coming while it is paused.

## Its schedule

- **Its schedule** shows when it runs, the events that start a run, its quiet hours, who it tells and how, and the most
  reads and tokens one run may use.
- An Owner changes them in [Organisation settings](organisation-settings.md); each change but pausing waits for a
  second approver.

## Related

- [Organisation settings](organisation-settings.md), [The statutory calendar](statutory-calendar.md) and
  [Approve a change the AI proposed](ai-proposals.md).
