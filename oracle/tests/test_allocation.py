"""Allocation of a shared cost (QA-ENG-01; PRD-001 FR-DIM-03, ALLOC-PRORATA): hand-worked cases the Go engine reproduces (internal/differential/allocation_test.go), generated ones,
and the properties of the rule."""

import json
import random
from decimal import Decimal
from pathlib import Path

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from oracle.allocation import AllocationRefused, alloc_prorata, case_split, percent_units, quantity_units

FILES = Path(__file__).resolve().parents[2] / "test" / "differential" / "allocations"
HAND, GENERATED = FILES / "hand.json", FILES / "generated.json"


def test_each_hand_worked_case_is_reproduced():  # req: FR-DIM-03, QA-ENG-02
    cases = json.loads(HAND.read_text())
    assert len(cases) >= 15
    for c in cases:
        assert case_split(c) == c["want"], c["name"]


def test_the_parts_and_the_unallocated_part_add_to_the_amount_in_every_hand_case():  # req: FR-DIM-03
    for c in json.loads(HAND.read_text()):
        got = case_split(c)
        assert sum(Decimal(p) for p in got["parts"]) + Decimal(got["unallocated"]) == Decimal(c["amount"]), c["name"]


def test_refused_weights():  # req: FR-DIM-03
    for percents in (["0"], ["100.01"], ["60", "40.01"], ["12.345"], ["-1"], ["1e2"], ["abc"], [""], ["5 0"], ["5."], [".5"], ["+5"]):
        with pytest.raises(AllocationRefused):
            percent_units(percents)
    for quantities in (["-1"], ["1.1234567"], ["1e3"], ["x"], [""], ["5."], [".5"], ["1,5"]):
        with pytest.raises(AllocationRefused):
            quantity_units(quantities)
    for weights in ([], [0], [0, 0], [1, -1]):
        with pytest.raises(AllocationRefused):
            alloc_prorata(100, weights)


def test_percent_units_leave_the_rest_unplaced():  # req: FR-DIM-03
    assert percent_units(["40", "35"]) == ([4000, 3500], 2500)
    assert percent_units(["12.5", "87.5"]) == ([1250, 8750], 0)
    assert percent_units(["0.01"]) == ([1], 9999)


def test_quantity_units_are_millionths():  # req: FR-DIM-03
    assert quantity_units(["1250", "3.5", "0.000001", "0"]) == [1250000000, 3500000, 1, 0]


@given(st.integers(-10**15, 10**15), st.lists(st.integers(0, 10**9), min_size=1, max_size=8))
@settings(max_examples=500, deadline=None)
def test_the_parts_add_up_and_each_is_within_one_unit_of_its_exact_share(minor: int, weights: list[int]):  # req: FR-DIM-03
    if sum(weights) == 0:
        with pytest.raises(AllocationRefused):
            alloc_prorata(minor, weights)
        return
    parts = alloc_prorata(minor, weights)
    assert sum(parts) == minor
    total = sum(weights)
    for p, w in zip(parts, weights):
        exact = abs(minor) * w / total
        assert abs(abs(p) - exact) < 1, (p, exact)  # the whole units of the share, or one more
        assert p == 0 or (p > 0) == (minor > 0)
        assert w > 0 or p == 0  # a part with no weight takes nothing
    assert alloc_prorata(-minor, weights) == [-p for p in parts]  # a negative amount is the mirror image


@given(st.integers(0, 10**9), st.integers(1, 6))
@settings(max_examples=200, deadline=None)
def test_equal_weights_differ_by_at_most_one_unit_and_the_earlier_parts_take_the_extra(minor: int, n: int):  # req: FR-DIM-03
    parts = alloc_prorata(minor, [7] * n)
    assert max(parts) - min(parts) <= 1
    assert parts == sorted(parts, reverse=True)


def _generated() -> list[dict]:
    """Generated cases of every key and currency, with the reference's answer to each (the seed is fixed, so the file is reproducible)."""
    rng = random.Random(20261121)
    out = []
    for n in range(240):
        cur, mu = rng.choice([("SGD", 2), ("USD", 2), ("JPY", 0), ("KWD", 3)])
        digits = rng.choice([1, 2, 4, 8, 12, 15])
        amount = Decimal(rng.randint(0, 10**digits)).scaleb(-mu) * rng.choice([1, 1, -1])
        key = ("weights", "percent", "quantity")[n % 3]
        k = rng.randint(1, 6)
        case = {"name": f"generated {n}", "currency": cur, "minor_units": mu, "amount": f"{amount:.{mu}f}" if mu else f"{int(amount)}", "key": key}
        if key == "weights":
            case["targets"] = [str(rng.choice([0, 0, 1, 2, 3, 5, 12, 100, rng.randint(0, 10**6)])) for _ in range(k)]
            case["unplaced"] = str(rng.choice([0, 0, 1, 4, rng.randint(0, 1000)]))
            if sum(int(t) for t in case["targets"]) + int(case["unplaced"]) == 0:
                case["targets"][0] = "1"
        elif key == "percent":
            cuts = sorted(rng.sample(range(1, 10000), k)) if k <= 9999 else [5000]
            share = [b - a for a, b in zip([0] + cuts[:-1], cuts)]
            keep = rng.choice([True, False])
            if not keep and len(share) > 1:
                share = share[:-1]  # leaves the last share unplaced
            case["targets"] = [f"{s // 100}.{s % 100:02d}".rstrip("0").rstrip(".") if s % 100 else str(s // 100) for s in share]
        else:
            case["targets"] = [f"{rng.randint(0, 10**7)}.{rng.randint(0, 999999):06d}".rstrip("0").rstrip(".") for _ in range(k)]
            if rng.random() < 0.5:
                case["unplaced"] = f"{rng.randint(0, 10**5)}.{rng.randint(0, 999999):06d}".rstrip("0").rstrip(".")
            if sum(Decimal(t) for t in case["targets"]) + Decimal(case.get("unplaced", "0")) == 0:
                case["targets"][0] = "1"
        case["want"] = case_split(case)
        out.append(case)
    return out


def test_the_generated_cases_are_the_file_the_engine_reads():  # req: FR-DIM-03, QA-ENG-02
    cases = _generated()
    assert len(cases) == 240
    assert json.loads(GENERATED.read_text()) == cases, "run: python3 -m oracle.allocation_cases to refresh test/differential/allocations/generated.json"


def test_the_golden_allocation_journals_move_cost_between_dimension_values_and_the_unplaced_stays():  # req: FR-DIM-03, QA-ENG-02
    from oracle.ledger import Ledger
    from oracle.scenario import load_scenario

    ledger = Ledger(load_scenario(FILES.parent / "golden" / "allocation.json"))
    r = ledger.result()
    assert ledger.reasons == [("JNL-0005", "unbalanced-base")]  # an allocation whose parts do not add to what it takes out is refused
    # 1,000.00 by 40% and 35%: 400.00 and 350.00, the unplaced 250.00 stays on no department; 0.05 by 1:1:1 is 0.02, 0.02 and 0.01
    assert r["dimension_balances"]["2026-03"]["6200"] == {"": "250.00", "Department=ENG": "400.00", "Department=SALES": "350.00"}
    assert r["dimension_balances"]["2026-03"]["6210"] == {"Department=ENG": "0.02", "Department=OPS": "0.01", "Department=SALES": "0.02"}
    assert r["trial_balance"]["2026-03"] == {"1010": "-1000.05", "6200": "1000.00", "6210": "0.05"}  # moving cost between values changes no account's total
