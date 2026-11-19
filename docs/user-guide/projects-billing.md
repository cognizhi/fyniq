---
key: projects.billing
title: Bill a project's time and costs
mode: expert
roles: [owner, controller, accountant, ar_specialist, approver]
requirements: [FR-PRJ-10, FR-PRJ-08, FR-PRJ-06]
status: draft            # provisional until the owner signs off (PRD-001 rev 0.298 and 0.301)
---

# Bill a project's time and costs

## What a project can bill

Open a project and look at **Unbilled work**. It lists what the project can bill its customer now:

- **Time**: approved, billable time at the project's billing rate for each day worked, each entry valued to the cent. Time with no billing rate
  is listed apart; enter a billing rate from the first day worked to bill it.
- **Costs**: posted bills and claims whose line is marked billable and carries the project, at cost plus the markup you gave. The cost is
  before GST; the invoice's GST code decides the GST on the sale. Costs in another currency than the project's are not on its invoice.

Only a **time-and-materials** project bills its time. A **fixed-fee** project bills by milestone, a **retainer** draws time down, and an
**internal** project is never billed.

## Draft the invoice

Select **Draft an invoice**, choose the revenue account and the GST code, and Fyniq saves a draft invoice to the project's customer: one line a
person and rate for the time, and one a cost. It is an ordinary draft invoice, so you check it, change it and issue it as any other, and the
usual approval applies. You need the right to create invoices (the Accountant or the AR specialist).

## Billed once

Nothing is billed while the invoice is a draft. **When you issue the invoice**, the time and costs on it are marked billed and leave the unbilled
list; if you **void** the invoice, they come back. Two drafts may name the same work, but only the first to be issued bills it: issuing the
second is refused, and you remove the work from it or void the first.

## Fixed fee and retainer

On a **fixed-fee** project, add its **milestones**: a name, an amount in the project's currency and a due date. A milestone's name and amount
never change. When the customer accepts one, select **Record acceptance** with the day; then **Draft an invoice** bills that milestone once, as one
line at its amount. A milestone is invoiced once, and a voided invoice releases it.

On a **retainer** project, add a **period** for each month's amount. A period needs no acceptance, and its invoice bills the amount. The
project's approved billable time is not invoiced as time: it is **drawn down** at billing rates against what the retainer has billed, and the
card shows what was billed, what was drawn and what is left, which is negative when more time was worked than was billed.

## The check

**Approved time is invoiced within 30 days of its month** lists a time-and-materials project whose approved time has waited more than 30 days after the end of
the month it was worked in without being on a posted invoice.
