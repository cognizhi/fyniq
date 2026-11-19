---
key: projects.projects
title: Track projects, their people and their tasks
mode: basic
roles: [accountant, controller, owner, approver, ar_specialist, auditor, tax_agent, employee]
requirements: [FR-PRJ-01, FR-ORG-06, FR-UX-40]
status: draft            # written with the projects screens (P1, after go-live); the accountant reviews it with the time and rates pages
---

# Track projects, their people and their tasks

## What it is

**Projects** lists the work you track for customers and for yourselves. A project has a customer, a
billing method, dates, a budget in hours and in money, the people who log time on it and the managers
who approve that time. A project is also a value of your **Project** dimension, so every bill, claim,
invoice or journal line you tag with it already counts towards it.

Projects are off until an Owner or an Administrator switches **Projects and time** on under
**Settings, Organisation, Features**. While they are off nobody sees them and nothing can be saved, whatever
their roles. What you saved stays when they are switched off.

Your roles decide what you see. An Accountant keeps the projects. The Owner, the Controller, the
Accountant, the AR specialist, the Auditor and the Tax agent see every project. An Approver or an
Employee sees only the projects they are on.

## Add a project

1. Open **Projects** and select **Add a project**.
2. Give it a **code** (such as ACME-01, never changed) and a **name**.
3. Choose **how it is billed**: time and materials, fixed fee, retainer, or internal. An internal project
   has no customer and is never billed.
4. Choose the **customer** and the **currency** it is billed in. The currency never changes: add another
   project for another currency.
5. Enter the dates and the budgets if you have them. The hours budget is hours, such as 400 or 7:30; the money budget is in
   the project's currency.
6. Name the people on the project, then select **Add the project**.

The first project also makes your Project dimension if you have none.

## Say who is on a project and who approves its time

Members log time on the project. A **manager** is a member who approves the project's time.

Naming someone a manager gives them no right of its own. Only a person who holds the **Approver** role (or the
**Controller** role) can approve time. If you name a manager who holds neither, the project shows a warning and
that person cannot approve until an Administrator gives them the role. A project that is billed needs at least
one manager. Nobody approves their own time.

## Add tasks

Time is logged against a task of a project, such as Data migration or Support.

1. Open the project and select **Add a task** under **Tasks**.
2. Name it and say whether time on it is billable.

A task is never deleted: put it out of use when it is done. Its name is then free for another task.

## Set up a project

A value you added to the Project dimension before projects were switched on is listed as **Not set up**. Open
it and select **Set up the project** to give it a customer, a currency, a billing method and its people.

## Close a project

Set its status to **Closed**. Closing a project takes no more time or new tasks, and puts its dimension value
out of use, so new lines cannot be tagged with it. Lines already tagged stay as they are. Set the status back to
**Active** to reopen it.
