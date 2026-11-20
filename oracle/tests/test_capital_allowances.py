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


def _a(i, cost, method, bought="2026-06-01", years=0, prescribed=0, disposed=False, sold=None):
    return {"id": i, "bought": bought, "cost": cost, "method": method, "prescribed": prescribed, "years": years, "disposed": disposed or sold is not None,
            "disposal": {"on": sold[0], "proceeds": sold[1]} if sold else None}


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
def test_the_whole_cost_of_a_computer_is_written_off_in_one_year_whatever_it_costs():
    r = schedule(_case(2027, [_a("c", "2000.00", "s19a2"), _a("p", "200.00", "s19a2"), _a("big", "45000.00", "s19a2")]))
    assert [(x["id"], x["allowance"], x["carried"]) for x in r["rows"]] == [("c", "2000.00", "0.00"), ("p", "200.00", "0.00"), ("big", "45000.00", "0.00")]
    assert r["apart"] == [] and r["total_allowance"] == "47200.00"
    assert schedule(_case(2028, [_a("c", "2000.00", "s19a2")]))["rows"] == []


# req: FR-FA-04
def test_iras_example_of_sales_gives_a_balancing_allowance_a_charge_and_a_charge_within_what_was_allowed():
    # IRAS's furniture, computer and van. Furniture of 3,000 on the three-year write-off, sold in YA 2029 for 200 after two years' allowances of 1,000: a written down value of
    # 1,000 and a balancing allowance of 800. The computer of 2,000 written off in YA 2027 and sold in YA 2028 for 200: a balancing charge of 200. The van of 20,000 on the
    # three-year write-off, sold in YA 2028 for 21,000 after one allowance of 6,666.67: a written down value of 13,333.33, and a charge of 7,666.67 restricted to the 6,666.67 allowed.
    furniture = _a("f", "3000.00", "s19a1", sold=("2028-09-01", "200.00"))
    r = schedule(_case(2029, [furniture]))["rows"][0]
    assert (r["brought"], r["allowance"], r["carried"], r["sold"]) == ("1000.00", "0.00", "0.00", {"balancing_allowance": "800.00", "balancing_charge": "0.00"})
    computer = _a("c", "2000.00", "s19a2", sold=("2027-09-01", "200.00"))
    r = schedule(_case(2028, [computer]))["rows"][0]
    assert (r["brought"], r["sold"]) == ("0.00", {"balancing_allowance": "0.00", "balancing_charge": "200.00"})
    van = _a("v", "20000.00", "s19a1", sold=("2027-09-01", "21000.00"))
    r = schedule(_case(2028, [van]))["rows"][0]
    assert (r["brought"], r["sold"]) == ("13333.33", {"balancing_allowance": "0.00", "balancing_charge": "6666.67"})
    # The year of the sale takes no allowance, the year before takes its own, and the years after say nothing.
    assert schedule(_case(2027, [van]))["rows"][0]["allowance"] == "6666.67"
    assert schedule(_case(2029, [van]))["rows"] == []
    both = schedule(_case(2029, [furniture]))
    assert (both["total_balancing_allowance"], both["total_balancing_charge"], both["total_allowance"]) == ("800.00", "0.00", "0.00")


# req: FR-FA-04
def test_an_asset_sold_in_the_year_it_was_bought_takes_no_part_of_the_cap_and_a_charge_is_nothing():
    cheap = [_a(f"a{i}", "5000.00", "s19a10a") for i in range(6)]
    sold = _a("s", "5000.00", "s19a10a", sold=("2026-09-01", "9000.00"))
    r = schedule(_case(2027, [sold] + cheap))
    assert len(r["rows"]) == 7 and r["apart"] == []  # the sold one frees the cap: six of 5,000 fit 30,000 beside it
    row = next(x for x in r["rows"] if x["id"] == "s")
    assert (row["brought"], row["sold"]) == ("5000.00", {"balancing_allowance": "0.00", "balancing_charge": "0.00"})
    # A disposal of a part, or one with no proceeds known, stays listed apart.
    r = schedule(_case(2027, [_a("p", "1000.00", "s19a1", disposed=True)]))
    assert r["apart"] == [{"id": "p", "reason": "disposed"}] and r["rows"] == []


# req: FR-FA-04
def test_the_year_of_assessment_follows_the_financial_year():
    b, e = date(2026, 4, 1), date(2027, 3, 31)
    assert [year_of_assessment(b, e, d) for d in (date(2026, 4, 1), date(2027, 3, 31), date(2027, 4, 1), date(2026, 3, 31), date(2025, 4, 1), date(2025, 3, 31))] == [
        2028, 2028, 2029, 2027, 2027, 2026]


def _generated(seed=43, count=160):
    rng = random.Random(seed)
    out = []
    for _ in range(count):
        books = date(2026, rng.choice([1, 4, 7, 10]), 1)
        end = _add_months_end(books, rng.choice([12, 12, 12, 9, 15]))
        assets = []
        for n in range(rng.randrange(1, 9)):
            bought = books + timedelta(days=rng.randrange(-700, 900))
            method = rng.choice([None, "s19", "s19a1", "s19a10a", "s19a10a", "s19a1", "s19a2"])
            prescribed = rng.choice([6, 6, 12, 16, 8, 14])
            years = rng.choice([6, 12, 16]) if method == "s19" else 0
            cost = f"{rng.choice([rng.randrange(1, 9_000), rng.randrange(1, 90_000)])}.{rng.randrange(0, 100):02d}"
            sold = None
            if rng.random() < 0.3:
                sold = ((bought + timedelta(days=rng.randrange(0, 1500))).isoformat(), f"{rng.choice([0, rng.randrange(0, 3_000), rng.randrange(0, 60_000)])}.{rng.randrange(0, 100):02d}")
            assets.append(_a(f"FA-{n + 1:04d}", cost, method, bought.isoformat(), years, prescribed if method == "s19" else 0, rng.random() < 0.05, sold))
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
    sold = [r["sold"] for c in cases for r in c["result"]["rows"] if "sold" in r]
    assert sum(1 for x in sold if x["balancing_allowance"] != "0.00") > 5 and sum(1 for x in sold if x["balancing_charge"] != "0.00") > 5
    assert json.loads(CASES.read_text()) == cases, "run: python3 -m oracle.capital_allowances_cases to refresh test/differential/capital-allowances/cases.json"
