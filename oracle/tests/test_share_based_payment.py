"""The expense of shares issued for services (QA-ENG-01; PRD-001 FR-EQ-13, ALLOC-EVEN, RND-LINE): hand-worked cases the Go engine reproduces (internal/differential/shareservice_test.go),
generated ones, and the properties of the rule."""

import json
import random
from datetime import date
from decimal import Decimal
from pathlib import Path

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from oracle.share_based_payment import ShareServiceRefused, case_schedule, months_between, schedule

FILES = Path(__file__).resolve().parents[2] / "test" / "differential" / "share-services"
HAND, GENERATED = FILES / "hand.json", FILES / "generated.json"


def test_each_hand_worked_grant_is_reproduced():  # req: FR-EQ-13, QA-ENG-02
    cases = json.loads(HAND.read_text())
    assert len(cases) >= 10
    for c in cases:
        assert case_schedule(c) == c["want"], c["name"]


def test_months_are_whole_calendar_months_both_counted():  # req: FR-EQ-13
    assert months_between(date(2026, 1, 1), date(2026, 12, 31)) == 12
    assert months_between(date(2026, 1, 1), date(2026, 1, 1)) == 1
    assert months_between(date(2026, 11, 1), date(2027, 2, 1)) == 4
    with pytest.raises(ShareServiceRefused):
        months_between(date(2026, 3, 1), date(2026, 2, 28))
    with pytest.raises(ShareServiceRefused):
        months_between(date(2026, 3, 2), date(2026, 6, 30))


def test_refused_grants():  # req: FR-EQ-13
    start = date(2026, 1, 1)
    for fair_value, tranches in ((Decimal(0), [(1, date(2026, 3, 31))]), (Decimal("-1"), [(1, date(2026, 3, 31))]), (Decimal(1), []),
                                 (Decimal(1), [(0, date(2026, 3, 31))]), (Decimal(1), [(1, date(2025, 12, 31))])):
        with pytest.raises(ShareServiceRefused):
            schedule(fair_value, 2, start, tranches)


@given(st.integers(1, 10**6), st.integers(1, 10**9), st.integers(1, 36), st.integers(0, 3))
@settings(max_examples=300, deadline=None)
def test_the_months_add_up_to_the_total_and_each_is_within_a_unit_of_an_even_share(shares: int, fv_micro: int, months: int, minor: int):  # req: FR-EQ-13
    fair_value = Decimal(fv_micro).scaleb(-6)
    vest = date(2026 + (months - 1) // 12, (months - 1) % 12 + 1, 28)
    got = schedule(fair_value, minor, date(2026, 1, 1), [(shares, vest)])
    total = Decimal(got["total"])
    assert sum(Decimal(m["expense"]) for m in got["months"]) == total
    assert len(got["months"]) == months
    # the total is the shares times the fair value, rounded once to the minor unit
    assert abs(total - Decimal(shares) * fair_value) <= Decimal(5).scaleb(-minor - 1)
    for m in got["months"]:
        assert abs(Decimal(m["expense"]) - total / months) <= Decimal(1).scaleb(-minor)


def _generated() -> list[dict]:
    """Generated grants of one to four tranches with the reference's answer to each (a fixed seed, so the file is reproducible)."""
    rng = random.Random(20261122)
    out = []
    for n in range(200):
        cur, mu = rng.choice([("SGD", 2), ("USD", 2), ("JPY", 0), ("KWD", 3)])
        start = date(rng.randint(2025, 2028), rng.randint(1, 12), 1)
        tranches = []
        for _ in range(rng.randint(1, 4)):
            months = rng.choice([1, 2, 3, 6, 12, 24, 36, 48, rng.randint(1, 60)])
            index = start.year * 12 + start.month - 1 + months - 1
            vest = date(index // 12, index % 12 + 1, rng.randint(1, 28))
            tranches.append({"shares": rng.choice([1, 3, 100, 1000, 48000, rng.randint(1, 5 * 10**6)]), "vest_on": vest.isoformat()})
        fv = rng.choice(["0.01", "0.125", "0.5", "1", "2.5", "0.000123", f"{rng.randint(1, 10**4)}.{rng.randint(0, 999999):06d}".rstrip("0").rstrip(".")])
        case = {"name": f"generated {n}", "currency": cur, "minor_units": mu, "fair_value": fv, "service_start": start.isoformat(), "tranches": tranches}
        case["want"] = case_schedule(case)
        out.append(case)
    return out


def test_the_generated_cases_are_the_file_the_engine_reads():  # req: FR-EQ-13, QA-ENG-02
    cases = _generated()
    assert len(cases) == 200
    assert json.loads(GENERATED.read_text()) == cases, "run: python3 -m oracle.share_based_payment_cases to refresh test/differential/share-services/generated.json"
