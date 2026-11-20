"""The tax capital allowance schedule (FR-FA-04), written from IRAS's page "Capital Allowances" and p2-reporting-planning design D11, not from the engine.

For a year of assessment (YA), each asset claims under the method the company elected: section 19 (an initial allowance of 20% of the cost, then the other 80% over
the elected working life, 6 or 12 years for a prescribed life of 12 years or less, 6, 12 or 16 for 16), section 19A(1) (a third a year over 3 years) or section 19A(10A)
(the whole cost in one year, for an asset of at most S$5,000, the year's claims within S$30,000). The year of assessment of an asset is the year after the financial
year it was bought in ends. Amounts are exact: Fractions, and a whole number of cents for each year (the initial allowance rounded half away from zero, the rest split by
cumulative rounding: year k = round(total * k / n) - round(total * (k - 1) / n), ALLOC-EVEN). An asset the schedule cannot place is listed apart, never guessed.
"""

from __future__ import annotations

from datetime import date
from fractions import Fraction
from typing import Any

FIRST_PACK_YA = 2023  # the page's rules apply to assets acquired in the basis period of YA 2023 and later
LOW_MAX = 500_000  # cents
LOW_CAP = 3_000_000


def _cents(text: str) -> int:
    whole, _, tail = text.partition(".")
    return int(whole) * 100 + int((tail + "00")[:2])


def _plain(units: int) -> str:
    return f"{units // 100}.{units % 100:02d}"


def _round(x: Fraction) -> int:
    """Half away from zero for a non-negative fraction."""
    return int((x + Fraction(1, 2)).__floor__())


def _spread(total: int, n: int) -> list[int]:
    parts, done = [], 0
    for k in range(1, n + 1):
        cum = _round(Fraction(total * k, n))
        parts.append(cum - done)
        done = cum
    return parts


def year_of_assessment(books_start: date, first_year_end: date, day: date) -> int:
    """The year after the financial year that holds the day ends in; before the books start the years are twelve months, ending on the first year's anniversary."""
    end = first_year_end
    if day >= books_start:
        while day > end:  # after the first year: twelve-month years follow it
            end = _add_year(end, 1)
    else:
        while day <= _add_year(end, -1):  # before the books start
            end = _add_year(end, -1)
    return end.year + 1


def _add_year(d: date, n: int) -> date:
    try:
        return d.replace(year=d.year + n)
    except ValueError:  # 29 February
        return d.replace(year=d.year + n, day=28)


def _allowed_years(prescribed: int) -> list[int]:
    if 1 <= prescribed <= 12:
        return [6, 12]
    if prescribed == 16:
        return [6, 12, 16]
    return []


def _series(method: str, years: int, cost: int) -> list[int]:
    if method == "s19":
        initial = _round(Fraction(cost * 20, 100))
        rest = _spread(cost - initial, years)
        rest[0] += initial
        return rest
    if method == "s19a1":
        return _spread(cost, 3)
    return [cost]  # s19a10a


def schedule(case: dict[str, Any]) -> dict[str, Any]:
    """case: books_start, first_year_end, ya, assets [{id, bought, cost, method, prescribed, years, disposed}] in the order the claim takes them (method null: none elected)."""
    books, first_end = date.fromisoformat(case["books_start"]), date.fromisoformat(case["first_year_end"])
    ya = case["ya"]
    claims, apart = [], []
    for a in case["assets"]:
        first = year_of_assessment(books, first_end, date.fromisoformat(a["bought"]))
        cost = _cents(a["cost"])
        if a.get("disposed"):
            apart.append((a["id"], "disposed", first))
        elif a["method"] is None:
            apart.append((a["id"], "no_election", first))
        elif first < FIRST_PACK_YA:
            apart.append((a["id"], "before_pack", first))
        elif a["method"] == "s19" and a["years"] not in _allowed_years(a["prescribed"]):
            apart.append((a["id"], "bad_life", first))
        elif a["method"] == "s19a10a" and cost > LOW_MAX:
            apart.append((a["id"], "over_cap", first))
        else:
            claims.append({"id": a["id"], "first": first, "cost": cost, "method": a["method"], "series": _series(a["method"], a.get("years", 0), cost)})
    used: dict[int, int] = {}
    for c in claims:  # the one-year write-off, year by year: the cap takes whole assets in order
        if c["method"] != "s19a10a":
            continue
        if used.get(c["first"], 0) + c["cost"] > LOW_CAP:
            c["dropped"] = True
            apart.append((c["id"], "over_cap", c["first"]))
        else:
            used[c["first"]] = used.get(c["first"], 0) + c["cost"]
    rows = []
    for c in claims:
        k = ya - c["first"]
        if c.get("dropped") or k < 0 or k >= len(c["series"]):
            continue
        before = sum(c["series"][:k])
        initial = _round(Fraction(c["cost"] * 20, 100)) if c["method"] == "s19" and k == 0 else 0
        rows.append({"id": c["id"], "allowance": _plain(c["series"][k]), "initial": _plain(initial), "brought": _plain(c["cost"] - before),
                     "carried": _plain(c["cost"] - before - c["series"][k])})
    apart = sorted((x for x in apart if x[2] <= ya), key=lambda x: x[0])
    return {"rows": rows, "apart": [{"id": i, "reason": r} for i, r, _ in apart],
            "total_allowance": _plain(sum(_cents(r["allowance"]) for r in rows)), "total_carried": _plain(sum(_cents(r["carried"]) for r in rows))}
