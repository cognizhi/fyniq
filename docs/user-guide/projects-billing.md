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

The assistant can propose this draft for a project when you ask it to invoice the project by its code: you see the customer,
the lines and the total, and a person who may draft the invoice approves it (see [AI proposals](ai-proposals.md)).

## Billed once

Nothing is billed while the invoice is a draft. **When you issue the invoice**, the time and costs on it are marked billed and leave the unbilled
list; if you **void** the invoice, they come back. Two drafts may name the same work, but only the first to be issued bills it: issuing the
second is refused, and you remove the work from it or void the first.

## Revenue over time

An accountant (or Controller or Owner) can elect a **fixed-fee** project in the base currency for **revenue over time** on its record (Expert mode): the
estimated total cost, instead of recognising each milestone as it is invoiced. Fyniq then works each month out from the posted ledger: the **fee** is the
milestones' total, the **cost to date** is the costs posted to the project, **progress** is cost to date over the estimate (at most 100%), and the **revenue
earned** is the fee times that progress, rounded once to the cent on the running total. A milestone invoice already books revenue, so the month's journal is a
true-up: it moves the revenue the ledger holds to what is earned, and the difference is a **contract asset** (earned, not yet invoiced) or a **contract
liability** (invoiced ahead of the work).

**Propose the month's journal** makes it as a draft, every line carrying the project, for a person to approve and post like any journal. A month is proposed
once, in order, and only after the earlier months' journals are posted. The three accounts are set under organisation settings (Projects). A **fixed-fee project in another currency** is worked in the base currency: its fee (the milestones) and its estimate are converted at the rate of the
month's last day (today's, in the month under way), the page shows that rate and its date, and a month with no rate for the day is refused until you enter
one. Each month re-measures the fee at that day's rate. Every month, early on the first days of the next, Fyniq also **proposes the month that has just ended
by itself** as a draft journal for each project elected for revenue over time (when nothing is yet to be moved, the earlier month is not posted, or the accounts are
not set up, it waits and tries again the next day); it only ever makes the draft, and a person approves and posts it.

If some of the costs posted to the project do not show progress (waste, rework, a cost out of proportion to the work), enter the running total to
**leave out of progress** with the reason when you elect it, and progress is measured by the costs posted less those: the page shows both. More left
out than was posted is refused. When the cost
reaches the estimate the whole fee is earned and the page says to revisit the estimate. The method is for the accountant to confirm: Fyniq follows the
input method of SFRS(I) 15.

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
