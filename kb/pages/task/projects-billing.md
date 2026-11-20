---
key: projects.billing
type: task
title: Bill a project's time and costs
summary: What Unbilled work lists, how to draft the invoice, when the work counts as billed, and the check on time left uninvoiced
sources:
  - docs/user-guide/projects-billing.md sha256:948688dadca647be9725acc7b67e2df1a79935a8c6b22e5003451f28650a6ac7
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
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

The assistant can propose this draft for a project when asked to invoice it by its code: the customer, the lines and the total are shown and a person who may draft the invoice approves it ([[ai.proposals]]). (src: docs/user-guide/projects-billing.md#draft-the-invoice)

## Billed once

- The work is marked billed when the invoice is issued, and released if the invoice is voided; two drafts may name the same work but only the
  first issued bills it. (src: docs/user-guide/projects-billing.md#billed-once)

## Revenue over time for a fixed-fee project

- An accountant elects a fixed-fee project in the base currency for revenue over time, with its estimated total cost (Expert mode, on the project's record). Fyniq works each month out from the posted ledger: the fee is the milestones' total, progress is the cost posted to the project over the estimate (at most 100%), and the revenue earned is the fee times progress, rounded once to the cent on the running total.
- A milestone invoice already books revenue, so the month's journal is a true-up of the revenue the ledger holds to what is earned; the difference is a contract asset (earned, not invoiced) or a contract liability (invoiced ahead of the work). **Propose the month's journal** makes a draft for a person to approve; a month is proposed once, in order, after earlier months are posted. The accounts are under organisation settings (Projects). A fixed-fee project in another currency is worked in the base currency at the rate of the month's last day, shown with its date (a month with no rate is refused). Early each month Fyniq also proposes the month that has just ended by itself as a draft journal for each elected project (a person approves and posts it). The accountant can leave out of progress a running total of posted costs that do not show it (with a reason; never more than was posted). At the estimate the whole fee is earned and the estimate is revisited. The method is for the accountant to confirm.

(src: docs/user-guide/projects-billing.md#revenue-over-time)

## Fixed fee and retainer

- A fixed-fee project has milestones (a name and an amount that never change, a due date): record the customer's acceptance, then draft an
  invoice that bills it once. A retainer has periods that need no acceptance; the project's approved billable time is drawn down at billing
  rates against what was billed, and the balance is billed less drawn. (src: docs/user-guide/projects-billing.md#fixed-fee-and-retainer)

## The check

- A check lists a time-and-materials project whose approved time is still on no posted invoice more than 30 days after its month ended.
  (src: docs/user-guide/projects-billing.md#the-check)

Related: [[projects.costs]], [[projects.rates]], [[projects.reports]].
