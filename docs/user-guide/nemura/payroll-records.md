---
key: nemura.payroll-records
title: Set up pay groups and record each person's pay
mode: expert
roles: [payroll_admin, owner]
requirements: [FR-EMP-01, FR-PAY-01, FR-PAY-06, FR-PAY-14, FR-PAY-17, NM-PAY-02, NM-PAY-03, NM-PAY-09, NM-PAY-12]
---

# Set up pay groups and record each person's pay

## What it is

**Payroll** is where the Payroll administrator and the Owner keep what a pay run is worked out from: the pay groups and their calendar, each person's pay record, the year to date of
CPF wages, a permanent resident's joint election, and the one-off items of a month. Nobody else sees pay: the HR roles see that a run exists, not an amount, and the audit trail says
that pay was set, never what it was.

## Before you start

- You are the Payroll administrator or the Owner.
- The person's record and legal classification are recorded: CPF follows their residency, age and, for a permanent resident, the day residence began.

## Steps

1. Under **Pay groups**, make a group: its name, the pay day of the month (0 is the last day), the days before the pay day that inputs close, and the days before the pay day by which the run is approved.
2. Under **A person's pay**, choose the person and select **Start a pay record**: the day it starts, the pay group, the basic monthly salary, the self-help group fund if they give to one, and
   each allowance and fixed deduction. For an allowance say whether it is CPF wages. A fixed deduction such as dormitory fees is never CPF wages. Select **Save the pay record**.
3. For a company that starts Nemura mid-year, select **Import the year to date**: the ordinary wages and additional wages this employer has counted for the person since 1 January, and the last month
   counted. A month that pays additional wages cannot be calculated without it, and it is imported once for a year.
4. For a permanent resident in the first two years whose employer and who jointly applied, add the CPF Board's approval to the person's file, then **Record the election** with that document and choose what the approval is for: **F/G** (full employer rates, graduated employee rates) or **F/F** (full rates for both, so the full table applies, which is 20% from the employee on S$5,000 of wages instead of 5%). The kind never changes: a different kind is a new election from a later day, and the old one ends the day before.
5. Under **One-off items of a month**, choose the month and **Add a one-off item**: a bonus, a commission or an allowance, with the month of employment it is for and the day it is payable.

## Calculate a pay run

Under **Pay runs**, choose the wage month and select **Calculate the run** for a pay group. Nemura works out every person with pay in the group for the month, exactly, from their pay record and
the inputs: a joiner's, a leaver's or a pay change's part month (the days each pay record covers over the days of the month), the month's one-off items, unpaid leave (the day's share of the
month's working days by the person's pattern), and approved overtime (twelve months of basic salary over fifty-two weeks of the normal hours, times the multiple, times the hours). Each amount is
rounded once, to the cent. Open the run to read each person's month: their gross pay, CPF, SDL, fund, net pay, the pay items with the record each came from, and what to look at.

- A person's missing fact stops the run and names them: a legal classification, a date of birth for CPF, or a year to date for a month that pays additional wages.
- A ✖ blocks approval; a ⚠ is shown to the approver.
- **Override a figure** replaces the employee or employer CPF, the SDL or the fund of one person's month, with a reason of at least ten characters. It applies from the next calculation, is flagged,
  and the calculated figure is kept beside it. **Withdraw** an override to take it away from the next calculation.
- A version is read-only once calculated and has a digest. **Calculate the next version** after any change: the version before is superseded. After an approved version it is a correction, which
  needs a reason and states the full corrected figures of its month.
- Overtime on a rest day or a public holiday is not guessed: add it as a one-off item for the month.

## What happens next

- A pay record is history. A change starts a new record from a later day and ends the one before the day before; nothing is edited.
- A one-off item's class follows its facts, not its name: an item for the month and payable by the 14th of the following month is ordinary wages, and any other is additional wages. The run checks it
  and warns when the name disagrees.
- A one-off item, an unpaid day or an overtime entry is marked used by a run only when the run is approved, so recalculating never uses it twice.

## Common problems

- **The pay starts after the person's last day, or before they started.** Choose a day within their employment.
- **The new record starts on the day the current one began.** Choose a later day.
- **The year to date is refused.** It is imported once for a year; additional wages subject to CPF cannot be more than the additional wages paid.
- **The election is refused.** Its evidence is a document on the person's file.

## Related

- [Read and approve a pay run](payroll-approval.md)
- [Read a person's record and their job history](person.md)
