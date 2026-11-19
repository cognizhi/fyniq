---
key: projects.costs
type: task
title: Put costs on a project and bill them on
summary: How a bill or claim line becomes a project cost, how to bill a cost on with a markup, and the project on a purchase order
sources:
  - docs/user-guide/projects-costs.md sha256:3bbb7e6ef926407330ca8d94904b67a16ac8a9823e98d8c63b9459dfd6df6df7
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - billable expense
  - markup
  - project cost
---

# Put costs on a project and bill them on

## Costs on a project

- A cost belongs to a project when its line carries the project as a dimension value: a bill line, a claim receipt or trip, a card or bank
  line, or a journal. (src: docs/user-guide/projects-costs.md#what-it-is)

## Bill a cost on to the customer

- On a bill or claim line that carries a project, tick **Bill this cost to the customer** and type a **Markup (%)** from 0 to 999.99 if you
  add a margin; the posted cost then shows in the project's unbilled list at cost plus markup, rounded to the cent. A billable line must
  carry a project, and a markup belongs to a billable line.
  (src: docs/user-guide/projects-costs.md#bill-a-cost-on-to-the-customer)

## A purchase order for a project

- Choose the **Project** on a purchase order and every line of a bill made from it carries the project.
  (src: docs/user-guide/projects-costs.md#a-purchase-order-for-a-project)

Related: [[projects.projects]], [[projects.rates]], [[projects.reports]].
