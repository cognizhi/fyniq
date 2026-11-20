"""A part month's pay in working days (QA-ENG-01; PRD-002 NM-PAY-12): the hand-worked cases the Go engine reproduces (internal/differential/partmonth_test.go) and properties."""

import json
from datetime import date, timedelta
from decimal import Decimal
from pathlib import Path

from hypothesis import given, settings
from hypothesis import strategies as st

from oracle.part_month import part_month, working_days

CASES = Path(__file__).resolve().parents[2] / "test" / "differential" / "part-months" / "part-months.json"


def test_each_hand_worked_part_month_is_reproduced():  # req: NM-PAY-12, QA-ENG-02
    # The file is worked by hand: June 2026 has 22 weekdays; a joiner on Wed 10 June works 10-12, 15-19, 22-26 and 29-30 June, which is 15, so 4,500 x 15 / 22 = 3,068.18; the
    # public holiday of Mon 10 Aug 2026 is a working day, so a joiner on 5 August has 19 of 21 (4,071.43); a three-day week has 13 days in June 2026 (Mon, Wed, Fri) and 6 from 17 June.
    cases = json.loads(CASES.read_text())
    assert len(cases) >= 15
    for c in cases:
        got = part_month(Decimal(c["basic"]), date.fromisoformat(c["month"]), set(c["working_weekdays"]), date.fromisoformat(c["from"]), date.fromisoformat(c["to"]))
        assert (got["days"], got["denominator"], str(got["pay"])) == (c["want_days"], c["want_denominator"], c["want_pay"]), c["name"]


def test_a_part_month_is_not_the_calendar_day_ratio():  # req: NM-PAY-12
    # Calendar days would give 21 of 30 = 3,150.00 for the joiner of 10 June; MOM's working days give 15 of 22 = 3,068.18.
    got = part_month(Decimal("4500.00"), date(2026, 6, 1), {1, 2, 3, 4, 5}, date(2026, 6, 10), date(2026, 6, 30))
    assert got["pay"] == Decimal("3068.18") != Decimal("3150.00")


@given(st.integers(0, 29), st.integers(0, 29), st.sets(st.integers(1, 7), min_size=1))
@settings(max_examples=300, deadline=None)
def test_the_working_days_of_a_split_span_add_up(a: int, b: int, weekdays: set[int]):  # req: NM-PAY-12
    first = date(2026, 9, 1)
    lo, hi = sorted((a, b))
    left = working_days(weekdays, first, first + timedelta(days=lo - 1)) if lo else 0
    span = working_days(weekdays, first + timedelta(days=lo), first + timedelta(days=hi))
    right = working_days(weekdays, first + timedelta(days=hi + 1), date(2026, 9, 30))
    assert left + span + right == working_days(weekdays, first, date(2026, 9, 30))


@given(st.sets(st.integers(1, 7), min_size=1), st.integers(0, 40), st.integers(0, 40))
@settings(max_examples=200, deadline=None)
def test_a_span_never_pays_more_than_the_month_and_the_whole_month_pays_the_rate(weekdays: set[int], a: int, b: int):  # req: NM-PAY-12
    month = date(2027, 2, 1)
    basic = Decimal("3333.33")
    whole = part_month(basic, month, weekdays, month, date(2027, 2, 28))
    assert whole["pay"] == basic
    lo, hi = sorted((a, b))
    got = part_month(basic, month, weekdays, month + timedelta(days=lo), month + timedelta(days=hi))
    assert Decimal("0.00") <= got["pay"] <= basic
