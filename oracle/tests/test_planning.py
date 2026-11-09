"""The oracle's forecast (FR-PLN-02, FR-PLN-03, RND-PLAN; p1-planning design D3, D9): figures worked by hand."""

from __future__ import annotations

import json
from fractions import Fraction
from pathlib import Path

import pytest

from oracle.planning import ForecastError, forecast, half_away, text
from oracle.strategies_planning import generate

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "forecasts" / "small-company.json"


def small() -> dict:
    return json.loads(GOLDEN.read_text(encoding="utf-8"))


def run_of(result: dict, scenario: str, funding: bool) -> dict:
    return next(r for r in result["runs"] if r["scenario"] == scenario and r["funding"] is funding)


def test_rnd_plan_rounds_half_away_from_zero() -> None:  # req: RND-PLAN
    assert half_away(Fraction("10.005"), 2) == Fraction("10.01")
    assert half_away(Fraction("-10.005"), 2) == Fraction("-10.01")
    assert text(Fraction(1, 3) * 3000 / 36, 2) == "27.78"
    assert text(Fraction("-0.004"), 2) == "0.00"
    assert text(Fraction("126.6032")) == "126.6032"
    with pytest.raises(ForecastError):
        text(Fraction(1, 3))


def test_the_first_months_by_hand() -> None:  # req: FR-PLN-02, RND-PLAN
    base = run_of(forecast(small()), "Base", True)
    m1, m2, m3 = base["months"][:3]
    assert m1["customers_close"] == "108"
    assert (m1["subscription"], m1["revenue"]) == ("5400.00", "6600.00")
    assert (m1["hosting"], m1["ai"], m1["contractors"], m1["fees"]) == ("648.00", "528.00", "250.00", "132.00")
    assert m1["payroll_cogs"] == "3007.50"  # 3,000.00 and SDL 7.50
    assert m1["payroll_rd"] == "8903.25"  # 7,600.00, 17% CPF, SDL at its maximum
    assert m1["opex_total"] == "3066.00"  # 566.00 sales and marketing + 2,500.00 rent
    assert m1["collections"] == "7980.00"
    assert m1["loans_drawn"] == "12000.00"
    assert m1["gross_margin"] == "30.8"
    assert m1["cac"] == "56.60" and m1["ltv"] == "770.64" and m1["ltv_to_cac"] == "13.62"
    assert m1["burn_multiple"] is None
    assert m2["depreciation"] == "183.33"  # 100.00 + 3,000.00 ÷ 36
    assert m2["interest"] == "60.00" and m2["loan_repayments"] == "-1000.00"
    assert m3["customers_close"] == "126.6032"
    assert m3["payroll_sm"] == "10371.25" and m3["interest"] == "55.00"


def test_payroll_of_a_low_salary() -> None:  # req: FR-PLN-02
    base = run_of(forecast(small()), "Base", True)
    # The admin from July at 640.00: 17% CPF (the workbook's one rate) and the 2.00 SDL minimum.
    assert base["months"][6]["payroll_ga"] == "750.80"
    assert base["months"][5]["payroll_ga"] == "0.00"


def test_worst_delays_and_scales_the_funding_not_the_grant() -> None:  # req: FR-PLN-02, FR-PLN-03
    worst = run_of(forecast(small()), "Worst", True)
    assert worst["months"][3]["equity"] == "0.00"
    assert worst["months"][6]["equity"] == "60000.00"
    assert worst["months"][5]["grants"] == "5000.00"
    without = run_of(forecast(small()), "Worst", False)
    assert without["months"][5]["grants"] == "0.00"
    assert all(m["equity"] == "0.00" and m["loans_drawn"] == "0.00" for m in without["months"])


def test_what_is_shown_adds_up() -> None:  # req: RND-PLAN
    for run in forecast(small())["runs"]:
        cash = Fraction(run["opening_cash"])
        for m in run["months"]:
            cash += Fraction(m["net_cash_flow"])
            assert Fraction(m["cash_close"]) == cash
            parts = ("subscription", "services", "other")
            assert Fraction(m["revenue"]) == sum(Fraction(m[k]) for k in parts)


def test_a_document_outside_the_schema_is_refused() -> None:
    doc = small()
    doc["horizon"] = 61
    with pytest.raises(ForecastError, match="horizon"):
        forecast(doc)
    doc = small()
    doc["revenue"]["churn"] = 2  # a JSON number, never a decimal string
    with pytest.raises(ForecastError):
        forecast(doc)


def test_generated_forecasts_are_valid_and_the_same_for_a_seed() -> None:  # req: QA-ENG-02
    first, again = generate(3, 5), generate(3, 5)
    assert first == again and len({json.dumps(d, sort_keys=True) for d in first}) == 5
    for doc in first:
        forecast(doc)
