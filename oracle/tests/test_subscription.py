"""Subscription contracts' contracted MRR (QA-ENG-01; PRD-001 FR-AR-15, FR-KPI-02): the hand-worked cases the Go engine reproduces (internal/differential/subscription_test.go), and properties."""

import json
from datetime import date, timedelta
from pathlib import Path

from hypothesis import given, settings
from hypothesis import strategies as st

from oracle.subscription import contracted_mrr, state_on

CASES = Path(__file__).resolve().parents[2] / "test" / "differential" / "subscriptions" / "subscriptions.json"


def test_each_hand_worked_set_of_contracts_is_reproduced():  # req: FR-AR-15, FR-KPI-02, QA-ENG-02
    cases = json.loads(CASES.read_text())
    assert len(cases) >= 4
    for case in cases:
        for check in case["checks"]:
            got = contracted_mrr(case["contracts"], date.fromisoformat(check["on"]))
            assert got == check["want"], (case["name"], check["on"])


def test_a_renewal_outlives_the_agreed_end_and_a_cancellation_on_the_first_day_never_starts():  # req: FR-AR-15
    renewed = {"id": "R", "currency": "SGD", "starts_on": "2026-01-01", "ends_on": "2026-03-31", "mrr": "10.00", "amendments": [{"kind": "cancel", "effective_on": "2026-02-01", "mrr": "0.00"}, {"kind": "renew", "effective_on": "2026-03-01", "mrr": "12.00"}]}
    assert state_on(renewed, date(2026, 8, 1))[0] is True
    never = {"id": "N", "currency": "SGD", "starts_on": "2026-05-01", "mrr": "70.00", "amendments": [{"kind": "cancel", "effective_on": "2026-05-01", "mrr": "0.00"}]}
    assert state_on(never, date(2026, 5, 1))[0] is False


@given(st.integers(0, 400), st.lists(st.tuples(st.integers(1, 60), st.integers(1, 500)), max_size=5))
@settings(max_examples=200, deadline=None)
def test_a_contract_with_only_upgrades_and_downgrades_is_never_below_its_smallest_value_or_out_of_force(offset: int, steps: list[tuple[int, int]]):  # req: FR-AR-15
    start = date(2026, 1, 1)
    day, amendments, values = start, [], [100]
    for gap, value in steps:
        day += timedelta(days=gap)
        amendments.append({"kind": "upgrade" if value > values[-1] else "downgrade", "effective_on": day.isoformat(), "mrr": f"{value}.00"})
        values.append(value)
    contract = {"id": "P", "currency": "SGD", "starts_on": start.isoformat(), "mrr": "100.00", "amendments": amendments}
    in_force, value = state_on(contract, start + timedelta(days=offset))
    assert in_force and min(values) <= value <= max(values)
