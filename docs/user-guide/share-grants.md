---
key: equity.share-grants
title: Record shares for services and expense them over the service period
mode: expert
roles: [accountant, controller, owner]
requirements: [FR-EQ-13, ALLOC-EVEN]
status: draft            # written with shares for services (P2); the accounts, the whole-month rule and the straight-line spread wait for the accountant's confirmation
---

# Record shares for services and expense them over the service period

## Find it

In Expert mode, open **Equity** and choose **Shares for services**. An Accountant records a grant; a Controller or Owner reads them. The Board viewer, an employee and the DPO see none of it.

## Record a grant

A grant of shares for services is an equity-settled share-based payment (SFRS(I) 2 / IFRS 2). Enter:

- the **recipient**, and the **fair value of a share at the grant date** with where it comes from (the valuation: confirm it before you rely on it). The fair value is fixed at the grant date and never remeasured;
- the day the **service starts**, the first of a month, and the **tranches**: a whole number of shares each, and the day they vest;
- the **expense account** the monthly expense is debited to and the **share-based payment reserve** (an equity account that is not share capital) it is credited to.

Each tranche's service months are the whole calendar months from the service start's month to the month it vests, both counted: from 1 January to a vest day in December is twelve months.

## The expense by month

A tranche's total is its shares times the fair value, rounded once to the cent. It is spread over its months by cumulative rounding, so the months add to the total exactly and no month is more than a cent from an even share. A month's expense is the sum of the tranches' parts for it.

**Propose** a month to make a draft journal: debit the expense account and credit the reserve with that month's expense. An approver posts it like any journal. A month is proposed once. Once a month is proposed, the grant's figures stay as they were: only the recipient, the valuation note and archiving can change; record a new grant for other figures.

No tax deduction is claimed for newly issued shares. A forfeiture before vesting is not worked here. The reserve moves to share capital when the shares are issued, which is an equity transaction, not a journal.
