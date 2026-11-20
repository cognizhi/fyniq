"""The capital allowance schedule's reference (FR-FA-04), worked by hand from IRAS's page, and the cases the engine's own test reads
(test/differential/capital-allowances/cases.json). The engine's schedule must give the same, to the cent."""

import json
import random
from datetime import date, timedelta
from pathlib import Path

from oracle.capital_allowances import schedule, year_of_assessment

CASES = Path(__file__).resolve().parents[2] / "test" / "differential" / "capital-allowances" / "cases.json"


def _case(ya, assets, books="2026-01-01", end="2026-12-31"):
    return {"books_start": books, "first_year_end": end, "ya": ya, "assets": assets}


def _a(i, cost, method, bought="2026-06-01", years=0, prescribed=0, disposed=False):
    return {"id": i, "bought": bought, "cost": cost, "method": method, "prescribed": prescribed, "years": years, "disposed": disposed}


# req: FR-FA-04
def test_iras_three_year_example_claims_a_third_each_year():
    for ya, (allow, carried) in {2027: ("1000.00", "2000.00"), 2028: ("1000.00", "1000.00"), 2029: ("1000.00", "0.00")}.items():
        r = schedule(_case(ya, [_a("a", "3000.00", "s19a1")]))["rows"][0]
        assert (r["allowance"], r["carried"]) == (allow, carried)


# req: FR-FA-04
def test_section_19_takes_the_initial_allowance_in_the_first_year_and_adds_to_the_cost():
    cars = [schedule(_case(2027 + i, [_a("c", "10000.00", "s19", years=6, prescribed=6)]))["rows"][0] for i in range(6)]
    assert [c["allowance"] for c in cars] == ["3333.33", "1333.34", "1333.33", "1333.33", "1333.34", "1333.33"]
    assert cars[0]["initial"] == "2000.00" and cars[1]["initial"] == "0.00"
    assert sum(int(c["allowance"].replace(".", "")) for c in cars) == 1_000_000
    assert cars[-1]["carried"] == "0.00"


# req: FR-FA-04
def test_the_cap_takes_whole_low_value_assets():
    assets = [_a(f"a{i}", "4400.00", "s19a10a") for i in range(7)] + [_a("z", "5000.01", "s19a10a")]
    r = schedule(_case(2027, assets))
    assert len(r["rows"]) == 6 and r["total_allowance"] == "26400.00"
    assert [x["id"] for x in r["apart"]] == ["a6", "z"] and {x["reason"] for x in r["apart"]} == {"over_cap"}


# req: FR-FA-04
def test_assets_the_schedule_cannot_place_are_listed_apart():
    r = schedule(_case(2027, [_a("n", "1000.00", None), _a("d", "1000.00", "s19a1", disposed=True), _a("o", "1000.00", "s19a1", bought="2021-06-01"),
                              _a("b", "1000.00", "s19", years=16, prescribed=6)]))
    assert {x["id"]: x["reason"] for x in r["apart"]} == {"n": "no_election", "d": "disposed", "o": "before_pack", "b": "bad_life"}
    assert r["rows"] == []


# req: FR-FA-04
def test_the_year_of_assessment_follows_the_financial_year():
    b, e = date(2026, 4, 1), date(2027, 3, 31)
    assert [year_of_assessment(b, e, d) for d in (date(2026, 4, 1), date(2027, 3, 31), date(2027, 4, 1), date(2026, 3, 31), date(2025, 4, 1), date(2025, 3, 31))] == [
        2028, 2028, 2029, 2027, 2027, 2026]


def _generated(seed=41, count=120):
    rng = random.Random(seed)
    out = []
    for _ in range(count):
        books = date(2026, rng.choice([1, 4, 7, 10]), 1)
        end = _add_months_end(books, rng.choice([12, 12, 12, 9, 15]))
        assets = []
        for n in range(rng.randrange(1, 9)):
            bought = books + timedelta(days=rng.randrange(-700, 900))
            method = rng.choice([None, "s19", "s19a1", "s19a10a", "s19a10a", "s19a1"])
            prescribed = rng.choice([6, 6, 12, 16, 8, 14])
            years = rng.choice([6, 12, 16]) if method == "s19" else 0
            cost = f"{rng.choice([rng.randrange(1, 9_000), rng.randrange(1, 90_000)])}.{rng.randrange(0, 100):02d}"
            assets.append(_a(f"FA-{n + 1:04d}", cost, method, bought.isoformat(), years, prescribed if method == "s19" else 0, rng.random() < 0.08))
        ya = rng.randrange(2026, 2034)
        case = _case(ya, assets, books.isoformat(), end.isoformat())
        case["result"] = schedule(case)
        out.append(case)
    return out


def _add_months_end(start, months):
    y, m = start.year + (start.month - 1 + months) // 12, (start.month - 1 + months) % 12 + 1
    return date(y, m, 1) - timedelta(days=1)


# req: FR-FA-04
# req: QA-ENG-02
def test_the_cases_the_engine_reads_are_the_references_answers():
    cases = _generated()
    assert sum(1 for c in cases if c["result"]["rows"]) > 60 and sum(1 for c in cases if c["result"]["apart"]) > 40
    assert json.loads(CASES.read_text()) == cases, "run: python3 -m oracle.capital_allowances_cases to refresh test/differential/capital-allowances/cases.json"
