---
key: projects.billing
type: task
title: Bill a project's time and costs
summary: What Unbilled work lists, how to draft the invoice, when the work counts as billed, and the check on time left uninvoiced
sources:
  - docs/user-guide/projects-billing.md sha256:84ed5c38e1816922dfd172fcd4414a139b7016aa8ffa5d1ce1fb7e6753b62854
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - unbilled work
  - draft invoice from a project
  - time and materials
---

# Bill a project's time and costs

## What a project can bill

- A project's **Unbilled work** lists approved, billable time at the billing rate of each day worked (entries with no rate are listed apart) and
  posted billable costs of bills and claims at cost plus markup, before GST. (src: docs/user-guide/projects-billing.md#what-a-project-can-bill)
- Only a time-and-materials project bills its time; a fixed fee bills by milestone, a retainer draws time down, an internal project is never
  billed. (src: docs/user-guide/projects-billing.md#what-a-project-can-bill)

## Draft the invoice

- **Draft an invoice** saves an ordinary draft invoice to the project's customer, a line a person and rate for the time and a line a cost, on the
  revenue account and GST code you choose; the Accountant and the AR specialist can do it.
  (src: docs/user-guide/projects-billing.md#draft-the-invoice)

## Billed once

- The work is marked billed when the invoice is issued, and released if the invoice is voided; two drafts may name the same work but only the
  first issued bills it. (src: docs/user-guide/projects-billing.md#billed-once)

## The check

- A check lists a time-and-materials project whose approved time is still on no posted invoice more than 30 days after its month ended.
  (src: docs/user-guide/projects-billing.md#the-check)

Related: [[projects.costs]], [[projects.rates]], [[projects.reports]].
