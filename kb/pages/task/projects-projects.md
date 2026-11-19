---
key: projects.projects
type: task
title: Track projects, their people and their tasks
summary: What a project is (a Project dimension value with customer, billing, budget, people, tasks), how to add one, who approves its time, and how to close one
sources:
  - docs/user-guide/projects-projects.md sha256:105e69cf7c007e1812b008c0f0bf4555139c3d2b275bdeafacc470f266ce4666
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - projects
  - add a project
  - project manager
  - project tasks
  - project dimension
  - close a project
---

# Track projects, their people and their tasks

## What a project is

- A project has a customer, a billing method, dates, a budget in hours and in money, the people who
  log time on it and the managers who approve that time. It is also a value of the **Project**
  dimension, so every bill, claim, invoice or journal line tagged with it already counts towards it.
  (src: docs/user-guide/projects-projects.md#what-it-is)
- Projects are off until an Owner or an Administrator switches **Projects and time** on under
  **Settings, Organisation, Features**; while they are off nothing can be saved, and what was saved
  stays. An Accountant keeps the projects; an Approver or an Employee sees only the projects they are
  on. (src: docs/user-guide/projects-projects.md#what-it-is)

## Add a project

- **Projects** › **Add a project**: a code that never changes, a name, how it is billed (time and
  materials, fixed fee, retainer, or internal), the customer and the currency, which never changes,
  then the dates, the budgets and the people. An internal project has no customer and is never
  billed. (src: docs/user-guide/projects-projects.md#add-a-project)

## Who approves a project's time

- Members log time; a manager approves it. Naming someone a manager gives them no right of its own:
  only a person who holds the **Approver** or the **Controller** role can approve time, and the
  project warns when a named manager holds neither. A project that is billed needs at least one
  manager, and nobody approves their own time.
  (src: docs/user-guide/projects-projects.md#say-who-is-on-a-project-and-who-approves-its-time)

## Tasks, setting up and closing

- Time is logged against a task of a project, such as Data migration; add one under **Tasks**. A task
  is never deleted: put it out of use. (src: docs/user-guide/projects-projects.md#add-tasks)
- A value of the Project dimension added earlier is listed as **Not set up**: open it and select
  **Set up the project**. (src: docs/user-guide/projects-projects.md#set-up-a-project)
- Set a project's status to **Closed** to take no more time or new tasks; its dimension value goes out
  of use, and lines already tagged stay. Set it back to **Active** to reopen it.
  (src: docs/user-guide/projects-projects.md#close-a-project)

Related: [[projects.time]], [[claims.make-a-claim]], [[accounting.chart]], [[settings.organisation]].
