"""The oracle's feasibility calculator (PRD-001 FR-PLN-06, RND-PLAN; p1-planning design D8, D9).

Written from the rules, not from the Go engine: the workbook's model as the design states it, in exact rational numbers
(``Fraction``).

* A study is a ``fyniq-feasibility@1`` document (test/differential/README.md, "Feasibility studies").
* Year 0 holds the investment, spent. Each year 1 to N: units = units × (1 + unit growth)^(t − 1), exact; revenue,
  the variable costs and the fixed costs are each rounded once from their drivers; depreciation is (investment −
  salvage) ÷ life, rounded once, in each year up to the life; EBIT is revenue less the three; the tax is EBIT × the tax
  rate, rounded once (a loss saves tax); the working capital is its share of the year's revenue, rounded once, and the
  cash flow is EBIT less tax, plus depreciation, less the working capital's change. The last year adds the salvage less
  the tax on its gain over the book value (the investment less the depreciation), rounded once, and releases the
  working capital.
* Each year's present value is its cash flow ÷ (1 + rate)^t, rounded once; the NPV and the running totals add the
  rounded lines.
* The IRR is the root of Σ cash flow × (1 + r)^(N − t) (the NPV times (1 + r)^N) nearest 10%, found on the grid
  −99%, −98%, … +1,000%: where it is zero, or changes sign between two points; that interval is halved exactly until
  both ends round alike to 4 decimals. Flows that are all zero have none.
* A payback is the year the running total stops being below zero: the year before it, plus what was still below zero
  ÷ that year's flow; 1 decimal shown.
* The profitability index is (investment + NPV) ÷ investment, 2 decimals; break-even units the first year's fixed
  costs ÷ (price − variable cost), 1 decimal, and break-even revenue those × price, rounded once; the margin of safety
  (units − break-even) ÷ units as a percentage, RND-PERCENT. None of these when it cannot be worked out.
* The grid is the NPV with the units and the fixed costs each at 80%, 90%, 100%, 110% and 120%; the NPV table at 8%,
  10%, 12%, 15%, 20%, 25%, 30% and 40%.
* The verdict compares exact values: no_figures with no investment, units or price; not_feasible when the exact NPV is
  not above zero; feasible when the payback is within the longest acceptable; marginal otherwise.
"""

from __future__ import annotations

from fractions import Fraction
from typing import Any

from .planning import ForecastError, half_away, q, text

SCHEMA = "fyniq-feasibility@1"
RESULT = "fyniq-feasibility-result@1"
MULTIPLIERS = (Fraction(8, 10), Fraction(9, 10), Fraction(1), Fraction(11, 10), Fraction(12, 10))
RATES = ("0.08", "0.1", "0.12", "0.15", "0.2", "0.25", "0.3", "0.4")


class FeasibilityError(ForecastError):
    """A feasibility study the oracle cannot read."""


def _years(doc: dict, rate: Fraction, units_by: Fraction, fixed_by: Fraction) -> list[dict[str, Fraction]]:
    places = doc["minor_units"]

    def r(x: Fraction) -> Fraction:
        return half_away(x, places)

    n, life = doc["years"], doc["life"]
    invest, salvage, tax_rate = q(doc["investment"]), q(doc["salvage"]), q(doc["tax_rate"])
    zero = Fraction(0)
    years = [{"year": 0, "units": zero, "revenue": zero, "variable": zero, "fixed": zero, "depreciation": zero, "ebit": zero,
              "tax": zero, "nopat": zero, "working_capital": zero, "working_capital_change": zero, "salvage": zero,
              "release": zero, "cash_flow": -invest}]
    each = r((invest - salvage) / life)
    written_off = zero
    capital_before = zero
    for t in range(1, n + 1):
        units = q(doc["units"]) * units_by * (1 + q(doc["unit_growth"])) ** (t - 1)
        revenue = r(units * q(doc["price"]) * (1 + q(doc["price_growth"])) ** (t - 1))
        variable = r(units * q(doc["variable_cost"]) * (1 + q(doc["variable_cost_growth"])) ** (t - 1))
        fixed = r(q(doc["fixed"]) * fixed_by * (1 + q(doc["fixed_growth"])) ** (t - 1))
        dep = each if t <= life else zero
        written_off += dep
        ebit = revenue - variable - fixed - dep
        tax = r(ebit * tax_rate)
        capital = r(q(doc["working_capital"]) * revenue)
        change = capital - capital_before
        capital_before = capital
        flow = ebit - tax + dep - change
        after_tax_salvage = release = zero
        if t == n:
            book = invest - written_off
            after_tax_salvage = salvage - r((salvage - book) * tax_rate)
            release = capital
            flow += after_tax_salvage + release
        years.append({"year": t, "units": units, "revenue": revenue, "variable": variable, "fixed": fixed, "depreciation": dep,
                      "ebit": ebit, "tax": tax, "nopat": ebit - tax, "working_capital": capital,
                      "working_capital_change": change, "salvage": after_tax_salvage, "release": release, "cash_flow": flow})
    running = running_pv = zero
    for t, y in enumerate(years):
        y["pv"] = r(y["cash_flow"] / (1 + rate) ** t)
        running += y["cash_flow"]
        running_pv += y["pv"]
        y["cumulative"], y["cumulative_pv"] = running, running_pv
    return years


def _npv(years: list[dict[str, Fraction]]) -> Fraction:
    return sum((y["pv"] for y in years), Fraction(0))


def _value(flows: list[Fraction], rate: Fraction) -> Fraction:
    """Σ flow × (1 + rate)^(N − t): the exact NPV at rate, times (1 + rate)^N."""
    n = len(flows) - 1
    return sum((f * (1 + rate) ** (n - t) for t, f in enumerate(flows)), Fraction(0))


def irr(flows: list[Fraction]) -> Fraction | None:
    """The root nearest 10%, to 4 decimals; None when the grid finds none, or every flow is zero (any rate would do)."""
    if all(f == 0 for f in flows):
        return None
    target = Fraction(1, 10)
    points = [Fraction(k - 99, 100) for k in range(1100)]  # −0.99 … 10.00
    best: tuple[Fraction, Fraction, Fraction] | None = None
    before = _value(flows, points[0])
    if before == 0:
        best = (Fraction(0) if points[0] <= target <= points[0] else abs(points[0] - target), points[0], points[0])
    for k in range(1, len(points)):
        now = _value(flows, points[k])
        span = None
        if now == 0:
            span = (points[k], points[k])
        elif before != 0 and (before < 0) != (now < 0):
            span = (points[k - 1], points[k])
        if span is not None:
            lo, hi = span
            distance = Fraction(0) if lo <= target <= hi else min(abs(lo - target), abs(hi - target))
            if best is None or distance < best[0]:
                best = (distance, lo, hi)
        before = now
    if best is None:
        return None
    _, lo, hi = best
    low_sign = _value(flows, lo) > 0
    for _ in range(400):
        if half_away(lo, 4) == half_away(hi, 4):
            break
        mid = (lo + hi) / 2
        v = _value(flows, mid)
        if v == 0:
            lo = hi = mid
        elif (v > 0) == low_sign:
            lo = mid
        else:
            hi = mid
    return half_away(lo, 4)


def _payback(running: list[Fraction], flows: list[Fraction]) -> Fraction | None:
    for k, total in enumerate(running):
        if total >= 0:
            return Fraction(0) if k == 0 else (k - 1) + (-running[k - 1]) / flows[k]
    return None


def feasibility(doc: dict) -> dict[str, Any]:
    """A study's fyniq-feasibility-result@1."""
    from oracle.scenario import schema_error

    problem = schema_error("feasibility.schema.json", doc)
    if problem is not None:
        raise FeasibilityError(problem)
    if q(doc["salvage"]) > q(doc["investment"]):
        raise FeasibilityError("the salvage is above the investment")
    places = doc["minor_units"]
    rate = q(doc["rate"])
    years = _years(doc, rate, Fraction(1), Fraction(1))
    flows = [y["cash_flow"] for y in years]
    npv = _npv(years)
    invest, units, price = q(doc["investment"]), q(doc["units"]), q(doc["price"])
    fixed, margin = q(doc["fixed"]), price - q(doc["variable_cost"])
    payback = _payback([y["cumulative"] for y in years], flows)
    discounted = _payback([y["cumulative_pv"] for y in years], [y["pv"] for y in years])
    root = irr(flows)
    if invest == 0 and units == 0 and price == 0:
        verdict = "no_figures"
    elif _value(flows, rate) <= 0:
        verdict = "not_feasible"
    elif payback is not None and payback <= q(doc["max_payback"]):
        verdict = "feasible"
    else:
        verdict = "marginal"

    def amount(x: Fraction) -> str:
        return text(x, places)

    out: dict[str, Any] = {
        "schema": RESULT,
        "name": doc["name"],
        "years": [{k: (v if k == "year" else text(v, 1) if k == "units" else amount(v)) for k, v in y.items()} for y in years],
        "npv": amount(npv),
        "irr": None if root is None else text(root, 4),
        "payback": None if payback is None else text(payback, 1),
        "discounted_payback": None if discounted is None else text(discounted, 1),
        "profitability_index": None if invest <= 0 else text((invest + npv) / invest, 2),
        "break_even_units": None if margin <= 0 else text(fixed / margin, 1),
        "break_even_revenue": None if margin <= 0 else amount(fixed * price / margin),
        "margin_of_safety": None if margin <= 0 or units <= 0 else text((units * margin - fixed) * 100 / (units * margin), 1),
        "grid": [[amount(_npv(_years(doc, rate, u, f))) for f in MULTIPLIERS] for u in MULTIPLIERS],
        "npv_at": [amount(_npv(_years(doc, q(r), Fraction(1), Fraction(1)))) for r in RATES],
        "verdict": verdict,
    }
    return out
