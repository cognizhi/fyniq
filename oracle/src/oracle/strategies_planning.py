"""Hypothesis strategies for fyniq-forecast@1 and fyniq-feasibility@1 documents (p1-planning design D9, QA-ENG-02).

Harness code: it draws documents within forecast.schema.json's limits for the differential run; it never computes an
expected figure. Rates have few decimals and amounts cents, as a person types them, so sixty months of exact
compounding stay within the engine's digits.
"""

from __future__ import annotations

from typing import Any

from hypothesis import HealthCheck, Phase, given, settings
from hypothesis import seed as hypothesis_seed
from hypothesis import strategies as st


def _decimal(top: int, places: int) -> st.SearchStrategy[str]:
    """A decimal string from 0 to top, with up to places decimals."""
    return st.integers(0, top * 10**places).map(lambda n: _text(n, places))


def _text(units: int, places: int) -> str:
    if places == 0:
        return str(units)
    whole, part = divmod(units, 10**places)
    return f"{whole}.{part:0{places}d}"


def _fraction(top_percent: int, places: int = 3) -> st.SearchStrategy[str]:
    """A fraction from 0 to top_percent%, with up to places decimals."""
    return st.integers(0, top_percent * 10 ** (places - 2)).map(lambda n: _text(n, places))


def _month(start_year: int = 2026, years: int = 6) -> st.SearchStrategy[str]:
    return st.tuples(st.integers(start_year, start_year + years - 1), st.integers(1, 12)).map(lambda t: f"{t[0]:04d}-{t[1]:02d}")


_LEVER = st.sampled_from(["0", "0.5", "0.6", "0.7", "0.8", "0.9", "0.95", "1", "1.05", "1.1", "1.2", "1.3", "1.5", "2"])


@st.composite
def _collection(draw: st.DrawFn) -> list[str]:
    if draw(st.booleans()):
        a = draw(st.integers(0, 10))
        b = draw(st.integers(0, 10 - a))
        return [_text(a, 1), _text(b, 1), _text(10 - a - b, 1)]
    return [draw(_fraction(100, 2)) for _ in range(3)]  # need not total 100%: the engine flags it


@st.composite
def forecasts(draw: st.DrawFn) -> dict[str, Any]:
    horizon = draw(st.integers(12, 60))
    start = draw(_month(2026, 3))
    scenario_names = draw(st.lists(st.sampled_from(["Base", "Best", "Worst", "Custom"]), min_size=1, max_size=3, unique=True))
    scenarios = []
    for name in scenario_names:
        scenarios.append(
            {
                "name": name,
                "levers": {k: draw(_LEVER) for k in ("new_customers", "churn", "price", "services", "opex", "direct_costs", "fund_amount")}
                | {"hire_delay": draw(st.integers(0, 6)), "fund_delay": draw(st.integers(0, 6))},
                "buffer_months": draw(st.one_of(st.none(), st.integers(0, 12).map(str))),
            }
        )
    headcount = [
        {
            "role": f"Role {i}",
            "function": draw(st.sampled_from(["COGS", "R&D", "S&M", "G&A"])),
            "start": draw(_month(2025, 5)),
            "end": draw(st.one_of(st.none(), _month(2026, 6))),
            "salary": draw(st.one_of(st.just("0"), _decimal(20000, 2), st.sampled_from(["640.00", "4500.00", "8000.00", "12000.00"]))),
            "cpf": draw(st.booleans()),
        }
        for i in range(draw(st.integers(0, 6)))
    ]
    funding = []
    for _ in range(draw(st.integers(0, 4))):
        kind = draw(st.sampled_from(["Equity", "SAFE", "Loan", "Grant"]))
        loan = kind == "Loan"
        funding.append(
            {
                "type": kind,
                "month": draw(_month(2025, 6)),
                "amount": draw(_decimal(500000, 2)),
                "rate": draw(_fraction(15)) if loan else "",
                "tenor": draw(st.integers(1, 48)) if loan else 0,
            }
        )
    return {
        "schema": "fyniq-forecast@1",
        "name": "forecast",
        "currency": "SGD",
        "minor_units": 2,
        "start": start,
        "horizon": horizon,
        "cash_adjustment": draw(st.sampled_from(["0", "0.00", "-5000.00", "12345.67"])),
        "buffer_months": draw(st.integers(0, 12).map(str)),
        "books": {
            "cash": draw(_decimal(1000000, 2)),
            "receivables": draw(_decimal(200000, 2)),
            "payables": draw(_decimal(100000, 2)),
            "depreciation": draw(st.lists(_decimal(5000, 2), max_size=12)),
        },
        "revenue": {
            "customers": draw(_decimal(2000, 1)),
            "new_customers": draw(_decimal(100, 1)),
            "new_growth": draw(_fraction(10)),
            "churn": draw(_fraction(10)),
            "arpa": draw(_decimal(500, 2)),
            "price_increase": draw(_fraction(10)),
            "services": draw(_decimal(20000, 2)),
            "services_growth": draw(_fraction(5)),
            "other": draw(_decimal(5000, 2)),
            "collection": draw(_collection()),
        },
        "costs": {
            "hosting": draw(_fraction(30)),
            "ai": draw(_fraction(20)),
            "contractors": draw(_fraction(60)),
            "fees": draw(_fraction(5)),
            "salary_increase": draw(_fraction(10)),
        },
        "opex": [
            {
                "label": f"Line {i}",
                "group": draw(st.sampled_from(["", "sales_marketing", "rent", "software"])),
                "base": draw(_decimal(20000, 2)),
                "growth": draw(_fraction(5)),
                "percent": draw(_fraction(10)),
            }
            for i in range(draw(st.integers(0, 4)))
        ],
        "headcount": headcount,
        "capex": [
            {"amount": draw(_decimal(100000, 2)), "month": draw(_month(2025, 6)), "life": draw(st.integers(1, 60))}
            for _ in range(draw(st.integers(0, 3)))
        ],
        "funding": funding,
        "tax": {
            "start_up_years": draw(st.integers(0, 3)),
            "losses_brought_forward": draw(_decimal(300000, 2)),
            "payment_lag": draw(st.integers(0, 15)),
        },
        "scenarios": scenarios,
        "statutory": {
            "employer_cpf": "0.17",
            "ow_ceiling": "8000.00",
            "sdl": {"rate": "0.0025", "minimum": "2.00", "maximum": "11.25"},
            "tax_rate": "0.17",
            "exemptions": {"sute": [["100000.00", "0.75"], ["100000.00", "0.50"]], "pte": [["10000.00", "0.75"], ["190000.00", "0.50"]]},
            "rebates": draw(st.sampled_from([{}, {"2027": {"percent": "0.50", "cap": "40000.00"}}, {"2028": {"percent": "0.25", "cap": "15000.00"}}])),
        },
    }


def generate(seed: int, count: int) -> list[dict[str, Any]]:
    """``count`` distinct forecast documents, the same for the same seed and Hypothesis version, named
    ``forecast-<seed>-<i>``."""
    found: dict[str, dict[str, Any]] = {}
    for attempt in range(100):
        batch: list[dict[str, Any]] = []

        @hypothesis_seed(seed * 1_000_003 + attempt)
        @settings(max_examples=count - len(found), database=None, deadline=None, phases=[Phase.generate],
                  suppress_health_check=list(HealthCheck))
        @given(forecasts())
        def collect(doc: dict[str, Any]) -> None:
            batch.append(doc)

        collect()
        for doc in batch:
            found.setdefault(repr(doc), doc)
        if len(found) >= count:
            break
    else:  # pragma: no cover - the strategy cannot be this narrow
        raise RuntimeError(f"the forecast strategy produced only {len(found)} distinct documents")
    return [{**doc, "name": f"forecast-{seed}-{i:04d}"} for i, doc in enumerate(list(found.values())[:count])]


def _growth() -> st.SearchStrategy[str]:
    """A yearly change from −50% to +100%, with up to 2 decimals of a fraction."""
    return st.integers(-50, 100).map(lambda n: ("-" if n < 0 else "") + _text(abs(n), 2))


@st.composite
def feasibilities(draw: st.DrawFn) -> dict[str, Any]:
    """A feasibility study within feasibility.schema.json's limits, as a person types it."""
    investment = draw(_decimal(500_000, 2))
    salvage_units = draw(st.integers(0, int(investment.replace(".", ""))))
    return {
        "schema": "fyniq-feasibility@1",
        "name": "feasibility",
        "currency": "SGD",
        "minor_units": 2,
        "years": draw(st.integers(1, 10)),
        "rate": draw(_fraction(40, 2)),
        "max_payback": draw(st.sampled_from(["1", "2", "2.5", "3", "4", "5", "7", "10"])),
        "investment": investment,
        "life": draw(st.integers(1, 15)),
        "salvage": _text(salvage_units, 2),
        "working_capital": draw(_fraction(30, 2)),
        "units": draw(_decimal(5000, 1)),
        "unit_growth": draw(_growth()),
        "price": draw(_decimal(5000, 2)),
        "price_growth": draw(_growth()),
        "variable_cost": draw(_decimal(3000, 2)),
        "variable_cost_growth": draw(_growth()),
        "fixed": draw(_decimal(400_000, 2)),
        "fixed_growth": draw(_growth()),
        "tax_rate": draw(st.sampled_from(["0", "0.17"])),
    }


def generate_feasibilities(seed: int, count: int) -> list[dict[str, Any]]:
    """``count`` distinct feasibility studies, the same for the same seed and Hypothesis version, named
    ``feasibility-<seed>-<i>``."""
    found: dict[str, dict[str, Any]] = {}
    for attempt in range(100):
        batch: list[dict[str, Any]] = []

        @hypothesis_seed(seed * 1_000_003 + attempt)
        @settings(max_examples=count - len(found), database=None, deadline=None, phases=[Phase.generate],
                  suppress_health_check=list(HealthCheck))
        @given(feasibilities())
        def collect(doc: dict[str, Any]) -> None:
            batch.append(doc)

        collect()
        for doc in batch:
            found.setdefault(repr(doc), doc)
        if len(found) >= count:
            break
    else:  # pragma: no cover - the strategy cannot be this narrow
        raise RuntimeError(f"the feasibility strategy produced only {len(found)} distinct documents")
    return [{**doc, "name": f"feasibility-{seed}-{i:04d}"} for i, doc in enumerate(list(found.values())[:count])]
