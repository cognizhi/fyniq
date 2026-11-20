"""Revenue over time for a fixed-fee project (QA-ENG-01; PRD-001 FR-PRJ-11, RND-RECOGNISE): the hand-worked cases the Go engine reproduces (internal/differential/projectrevenue_test.go), and properties."""

import json
from decimal import Decimal
from pathlib import Path

from hypothesis import given, settings
from hypothesis import strategies as st

from oracle.project_revenue import earned_to_date, position

CASES = Path(__file__).resolve().parents[2] / "test" / "differential" / "project-revenue" / "project-revenue.json"


def test_each_hand_worked_project_month_is_reproduced():  # req: FR-PRJ-11, QA-ENG-02
    cases = json.loads(CASES.read_text())
    assert len(cases) >= 10
    for c in cases:
        got = position(Decimal(c["price"]), Decimal(c["estimate"]), Decimal(c["cost"]), Decimal(c["billed"]), Decimal(c["held"]), c["minor_units"])
        assert got == c["want"], c["name"]


def test_the_cumulative_figure_is_rounded_once_so_the_months_add_up():  # req: FR-PRJ-11
    # 10,000.00 over three equal thirds of cost: 3,333.33, 6,666.67 and 10,000.00 cumulative, so the monthly adjustments are 3,333.33, 3,333.34 and 3,333.33 and sum to the price.
    steps = [earned_to_date(Decimal("10000.00"), Decimal("3.00"), Decimal(n), 2) for n in (1, 2, 3)]
    assert [str(s) for s in steps] == ["3333.33", "6666.67", "10000.00"]
    assert sum(b - a for a, b in zip([Decimal(0), *steps], steps)) == Decimal("10000.00")


@given(st.integers(0, 10**9), st.integers(1, 10**7), st.integers(0, 2 * 10**7), st.integers(0, 10**9), st.integers(-10**8, 10**8))
@settings(max_examples=400, deadline=None)
def test_the_journal_moves_revenue_by_exactly_the_change_in_the_contract_position(price: int, estimate: int, cost: int, billed: int, prior: int):  # req: FR-PRJ-11
    p, e, c, b = (Decimal(x).scaleb(-2) for x in (price, estimate, cost, billed))
    held = b + Decimal(prior).scaleb(-2)  # what the ledger holds: invoiced plus the earlier adjustments
    got = position(p, e, c, b, held, 2)
    earned, adj, asset, liab = (Decimal(got[k]) for k in ("earned", "adjustment", "asset", "liability"))
    assert Decimal(got["asset_change"]) - Decimal(got["liability_change"]) == adj == earned - held
    assert asset * liab == 0 and asset - liab == earned - b
    assert Decimal(0) <= earned <= p
    assert got["overrun"] == (c >= e)
