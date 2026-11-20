"""A part month's pay in MOM's working days, worked out from the rule and not from the engine (QA-ENG-01; PRD-002 NM-PAY-12, owner decision of 21 Nov 2026).

The rule (https://www.mom.gov.sg/employment-practices/salary/monthly-and-daily-salary, retrieved 21 Nov 2026): "Monthly gross rate of pay / Total number of working days in
that month x Total number of days the employee actually worked in that month", the working days excluding rest days and non-working days and including public holidays.
So a person's working days are the days of their working pattern, a public holiday is one of them, and the pay of a span is the monthly rate times the working days of
the span over the working days of the month, rounded once to the cent, half away from zero (RND-PAY-RATIO). Nothing is left out for a public holiday, and a holiday on a
rest day is not a working day either `[accountant to confirm: MOM's page says nothing of a day in lieu]`.
"""

import calendar
from datetime import date, timedelta
from decimal import Decimal

from oracle.employee_month import ratio


def working_days(weekdays: set[int], first: date, last: date) -> int:
    """The days from ``first`` to ``last``, both included, whose ISO weekday (1 Monday to 7 Sunday) is one the person works."""
    count, day = 0, first
    while day <= last:
        if day.isoweekday() in weekdays:
            count += 1
        day += timedelta(days=1)
    return count


def part_month(basic: Decimal, month: date, weekdays: set[int], first: date, last: date) -> dict[str, object]:
    """The working days of the span and of the month, and the pay: the span is clipped to the month; none is nothing."""
    start = month.replace(day=1)
    end = start.replace(day=calendar.monthrange(start.year, start.month)[1])
    first, last = max(first, start), min(last, end)
    den = working_days(weekdays, start, end)
    days = working_days(weekdays, first, last) if first <= last else 0
    pay = ratio(basic, Decimal(days), Decimal(den)) if days and den else Decimal("0.00")
    return {"days": days, "denominator": den, "pay": pay}
