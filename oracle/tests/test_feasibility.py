"""The oracle's feasibility calculator (FR-PLN-06, RND-PLAN; p1-planning design D8, D9): figures worked by hand."""

from __future__ import annotations

import json
from fractions import Fraction
from pathlib import Path

import pytest

from oracle.feasibility import FeasibilityError, feasibility, irr
from oracle.strategies_planning import generate_feasibilities

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "feasibilities"


def study(**edits: object) -> dict:
    """One year: 100.00 invested and depreciated over it, one unit sold for 300.00, no costs, no tax, at 10%."""
    doc = json.loads((GOLDEN / "one-year-feasible.json").read_text(encoding="utf-8"))
    doc.update(edits)
    return doc


def test_one_year_worked_by_hand() -> None:  # req: FR-PLN-06, RND-PLAN
    # The year's flow is 300.00 (a profit of 200.00 after 100.00 of depreciation, added back), worth 272.73 at 10%: an NPV
    # of 172.73. The IRR is 200%: −100 × 3 + 300 = 0. Payback 100 ÷ 300 = 0.3; discounted 100 ÷ 272.73 = 0.4.
    r = feasibility(study())
    y = r["years"][1]
    assert (y["revenue"], y["depreciation"], y["ebit"], y["cash_flow"], y["pv"]) == ("300.00", "100.00", "200.00", "300.00", "272.73")
    assert (r["npv"], r["irr"], r["payback"], r["discounted_payback"]) == ("172.73", "2.0000", "0.3", "0.4")
    assert (r["profitability_index"], r["break_even_units"], r["break_even_revenue"], r["margin_of_safety"]) == ("2.73", "0.0", "0.00", "100.0")
    assert r["grid"][0][2] == "118.18" and r["grid"][2][2] == "172.73"  # 80% of the volume: 240.00, worth 218.18
    assert r["npv_at"][0] == "177.78" and r["npv_at"][-1] == "114.29"
    assert r["verdict"] == "feasible"
    assert feasibility(study(max_payback="0.2"))["verdict"] == "marginal"


def test_nothing_sold_and_nothing_entered() -> None:  # req: FR-PLN-06
    r = feasibility(study(units="0", price="0"))
    assert (r["npv"], r["irr"], r["payback"], r["break_even_units"], r["margin_of_safety"]) == ("-100.00", None, None, None, None)
    assert r["verdict"] == "not_feasible"
    e = feasibility(study(investment="0", units="0", price="0"))
    assert (e["verdict"], e["irr"], e["profitability_index"]) == ("no_figures", None, None)


def test_a_loss_saves_tax_and_the_salvage_is_taxed_on_its_gain() -> None:  # req: FR-PLN-06
    # Two years at 0% with 20% tax: each loses 300.00 after 200.00 of depreciation, saving 60.00: a flow of −40.00. The book
    # value at the end is 600.00, so selling for 200.00 loses 400.00, saving 80.00: 280.00 more.
    r = feasibility(study(years=2, rate="0", investment="1000", life=4, salvage="200", tax_rate="0.2", units="10", price="50", fixed="600"))
    assert [y["cash_flow"] for y in r["years"]] == ["-1000.00", "-40.00", "240.00"]
    assert (r["years"][1]["tax"], r["years"][2]["salvage"], r["npv"], r["verdict"]) == ("-60.00", "280.00", "-800.00", "not_feasible")


def test_the_irr_nearest_ten_percent() -> None:  # req: FR-PLN-06
    def flows(*xs: int) -> list[Fraction]:
        return [Fraction(x) for x in xs]

    assert irr(flows(-100, 230, -132)) == Fraction(1, 10)  # roots at 10% and 20%
    assert irr(flows(-100, 245, -147)) == Fraction(5, 100)  # at 5% and 40%
    assert irr(flows(-100, 0, 150)) == Fraction(2247, 10000)  # √1.5 − 1
    assert irr(flows(-100, 0)) is None and irr(flows(100, 50)) is None and irr(flows(0, 0, 0)) is None


def test_a_study_breaking_its_rules_is_refused() -> None:
    with pytest.raises(FeasibilityError):
        feasibility(study(salvage="200"))  # above the investment
    with pytest.raises(FeasibilityError):
        feasibility(study(years=11))


def test_generated_studies_are_valid_and_reproducible() -> None:  # req: QA-ENG-02
    a, b = generate_feasibilities(5, 8), generate_feasibilities(5, 8)
    assert a == b and len({json.dumps(d, sort_keys=True) for d in a}) == 8
    for doc in a:
        feasibility(doc)
