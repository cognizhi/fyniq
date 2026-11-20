"""The expense of shares issued for services, worked out from the rule and not from the engine (QA-ENG-01; PRD-001 FR-EQ-13, ALLOC-EVEN, RND-LINE; p2-reporting-planning design D15).

The rule (equity-settled share-based payment, SFRS(I) 2 / IFRS 2; SFRS for Small Entities section 26 `[accountant to confirm]`). A grant has a fair value per share, measured at the grant date and
never remeasured, and tranches: a number of whole shares each with the day it vests. A tranche's service period runs from the grant's service start (the first day of a month) to the month its vest
day falls in, counted in whole calendar months and including both ends: from January to a vest day in December is twelve months. A tranche's total expense is its shares times the fair value,
rounded once to the currency's minor units, half away from zero (RND-LINE); the total is spread over its months straight-line by cumulative rounding (ALLOC-EVEN), so the months add to the total
exactly. A month's expense is the sum of the tranches' parts for it; it is debited to share-based payment expense and credited to the share-based payment reserve in equity. No tax deduction is
claimed for newly issued shares. A forfeiture before vesting, a non-market condition's true-up and a cash alternative are not worked here.
"""

from datetime import date
from decimal import Decimal

from oracle.money import EXACT, alloc_even, fmt, rnd_line


class ShareServiceRefused(ValueError):
    """The grant cannot be worked: the engine refuses the same inputs."""


def months_between(start: date, vest_on: date) -> int:
    """Whole calendar months from the month of ``start`` to the month of ``vest_on``, both counted."""
    if start.day != 1:
        raise ShareServiceRefused("the service starts on the first day of a month")
    n = (vest_on.year - start.year) * 12 + (vest_on.month - start.month) + 1
    if n < 1:
        raise ShareServiceRefused("a tranche vests in or after the month the service starts")
    return n


def _month(start: date, k: int) -> str:
    index = start.year * 12 + (start.month - 1) + k
    return f"{index // 12:04d}-{index % 12 + 1:02d}"


def schedule(fair_value: Decimal, minor_units: int, service_start: date, tranches: list[tuple[int, date]]) -> dict:
    """The tranches' totals and parts and each month's expense, as the text a case writes them in."""
    if not fair_value > 0:
        raise ShareServiceRefused("a fair value above nothing")
    if not tranches:
        raise ShareServiceRefused("at least one tranche")
    per_month: dict[str, Decimal] = {}
    out_tranches = []
    grand = Decimal(0).scaleb(-minor_units)
    for shares, vest_on in tranches:
        if shares < 1:
            raise ShareServiceRefused("whole shares, at least one")
        n = months_between(service_start, vest_on)
        total = rnd_line(Decimal(shares), fair_value, minor_units)
        parts = alloc_even(total, n, minor_units)
        for k, part in enumerate(parts):
            m = _month(service_start, k)
            per_month[m] = EXACT.add(per_month.get(m, Decimal(0)), part)
        grand = EXACT.add(grand, total)
        out_tranches.append({"months": n, "total": fmt(total, minor_units), "parts": [fmt(p, minor_units) for p in parts]})
    return {"tranches": out_tranches, "months": [{"month": m, "expense": fmt(per_month[m], minor_units)} for m in sorted(per_month)], "total": fmt(grand, minor_units)}


def case_schedule(case: dict) -> dict:
    return schedule(Decimal(case["fair_value"]), case["minor_units"], date.fromisoformat(case["service_start"]),
                    [(t["shares"], date.fromisoformat(t["vest_on"])) for t in case["tranches"]])
