"""Nemura's employee-month and pay run on exact decimals, worked out by hand from PRD-001 §8.7 and Appendix B.3 (QA-ENG-01).

Every expected figure below comes from the comment beside it (the CPF Board's tables, the PRD's hand-pinned G.2 and G.4 rows, the
packs' own pinned cases), never from running the oracle or the Go engine.
"""

import csv
import json
from decimal import ROUND_FLOOR, ROUND_HALF_UP, Decimal
from pathlib import Path

import pytest

from oracle import employee_month as em
from oracle.scenario import ScenarioError, parse_json

ROOT = Path(__file__).resolve().parents[2]
GOLDEN = ROOT / "test" / "differential" / "payrolls"
PACKS = ROOT / "packs"


def month_doc(**over):
    doc = {
        "schema": "fyniq-payroll-month@1",
        "name": "t",
        "month": "2026-06-01",
        "person": {"residency": "citizen", "birth_date": "1990-05-20"},
        "items": [{"key": "basic", "name": "Basic salary", "amount": "5000.00", "cpf_wages": True}],
        "expect": {},
    }
    doc.update(over)
    return doc


def bonus(amount, month="2026-06-30"):
    return {"key": "bonus", "name": "Bonus", "amount": amount, "cpf_wages": True, "for_month": "2025-12-01", "payable_on": month}


def basic(amount, **over):
    return [{"key": "basic", "name": "Basic salary", "amount": amount, "cpf_wages": True, **over}]


def work(**over):
    return em.employee_month_doc(month_doc(**over))


def person(**over):
    return {"residency": "citizen", "birth_date": "1990-05-20", **over}


def figures(result, *keys):
    return {k: Decimal(result[k]) for k in keys}


# --- the engine agent's sixteen golden months: hand-worked from the CPF Board's tables --------------------------------------------


def golden_months():
    out = []
    for path in sorted(GOLDEN.glob("*.json")):
        out.extend(parse_json(path.read_text(encoding="utf-8")))
    return out


@pytest.mark.parametrize("doc", golden_months(), ids=lambda d: d["name"])
def test_the_oracle_reproduces_every_golden_month(doc):  # req: QA-ENG-02, FR-PAY-02, FR-PAY-14, FR-PAY-17
    result = em.employee_month_doc(doc)
    expect = doc["expect"]
    if "error" in expect:
        assert result == {"error": expect["error"]}
        return
    for key, want in expect.items():
        if key == "violations":
            assert bool(result["violations"]) == want
        else:
            assert Decimal(result[key]) == Decimal(want), key


# --- CPF: RND-CPF and RND-CPF-GRAD --------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "wages, total, employee",
    [
        # 55 and below, citizen (sc_spr3), the CPF Board's formula 17% (TW) + 0.6 (TW - 500) for TW above 500 up to 750
        ("49.99", 0, 0),  # up to S$50: nothing
        ("50.00", 0, 0),
        ("50.01", 9, 0),  # 17% of 50.01 = 8.5017 -> 9, nothing by the employee
        ("500.00", 85, 0),  # 17% of 500 = 85
        ("500.01", 85, 0),  # 85.0017 + 0.006 -> total 85.0077 -> 85; the employee 0.006 -> down to 0
        ("600.00", 162, 60),  # 102 + 0.6 x 100 = 162; the employee 60
        ("749.99", 277, 149),  # 127.4983 + 0.6 x 249.99 (149.994) = 277.4923 -> 277; the employee 149.994 -> 149
        ("750.00", 278, 150),  # 127.5 + 150 = 277.5 -> 278 (50 cents up); the employee 150
        # above 750: 17% + 20% of the wages
        ("750.01", 278, 150),  # 37% of 750.01 = 277.5037 -> 278; 20% = 150.002 -> 150
        ("3005.00", 1112, 601),  # G4-003: 1,111.85 -> 1,112; the employee 601.00; the employer 511
        ("3004.99", 1112, 600),  # 1,111.8463 -> 1,112; 20% = 600.998 -> 600: the employee's share is down
        ("8000.00", 2960, 1600),
    ],
)
def test_cpf_of_a_citizen_aged_55_or_under_by_the_wage_band(wages, total, employee):  # req: RND-CPF, RND-CPF-GRAD, FR-PAY-02
    got = work(items=basic(wages))
    assert Decimal(got["cpf_total"]) == total
    assert Decimal(got["cpf_employee"]) == employee
    assert Decimal(got["cpf_employer"]) == total - employee


def test_a_leap_day_birthday_is_a_leap_day_only_in_a_leap_year():  # req: FR-PAY-02
    assert em._passed(em.date(1964, 2, 29), em.date(2024, 3, 1), 60) is True  # 29 Feb 2024 exists: March is the next month
    assert em._passed(em.date(1964, 2, 29), em.date(2024, 2, 1), 60) is False
    assert em._passed(em.date(1963, 2, 28), em.date(2023, 3, 1), 60) is True


def test_cpf_applies_once_to_the_months_total_never_to_a_run():  # req: RND-CPF, FR-PAY-02 (G4-003)
    # S$3,005 as 1,502.50 + 1,502.50 in one employee-month is the same S$3,005: 1,112 / 601 / 511, not 1,112 / 600 / 512.
    two = [
        {"key": "a", "name": "Run 1", "amount": "1502.50", "cpf_wages": True},
        {"key": "b", "name": "Run 2", "amount": "1502.50", "cpf_wages": True},
    ]
    got = work(items=two)
    assert figures(got, "cpf_total", "cpf_employee", "cpf_employer") == {"cpf_total": 1112, "cpf_employee": 601, "cpf_employer": 511}


def test_cpf_of_a_citizen_over_70_rounds_the_total_half_up_and_the_employee_down():  # req: RND-CPF (G4-004)
    # 12.5% of 2,500 = 312.50 -> 313; the employee 5% = 125; the employer 188
    got = work(person=person(birth_date="1950-01-10"), items=basic("2500.00"))
    assert figures(got, "cpf_total", "cpf_employee", "cpf_employer") == {"cpf_total": 313, "cpf_employee": 125, "cpf_employer": 188}


def _march(born, gross, **who):
    return em.employee_month_doc(month_doc(month="2026-03-01", person=person(birth_date=born, **who), items=basic(gross)))


@pytest.mark.parametrize(
    "born, who, gross, total, employee, sdl, shg, net",
    [
        # PRD-001 G.2, "Hand-pinned payroll results (2026 CPF rates)": the March rows; net = gross - employee CPF - SHG
        ("1990-01-01", {"shg_fund": "CDAC"}, "7600.00", 2812, 1520, "11.25", "3.00", "6077.00"),  # ER 1,292; CDAC above 7,500: 3.00
        ("1969-01-01", {}, "5000.00", 1700, 900, "11.25", "0", "4100.00"),  # age 55 to 60: ER 800
        ("1964-01-01", {}, "4000.00", 1000, 500, "10.00", "0", "3500.00"),  # age 60 to 65, SPR from the third year: ER 500
        ("1959-01-01", {}, "3000.00", 495, 225, "7.50", "0", "2775.00"),  # age 65 to 70: ER 270
        ("1950-01-01", {}, "2500.00", 313, 125, "6.25", "0", "2375.00"),  # over 70: ER 188
        ("1990-01-01", {"residency": "foreigner"}, "9000.00", 0, 0, "11.25", "0", "9000.00"),
    ],
)
def test_the_prd_hand_pinned_march_payroll(born, who, gross, total, employee, sdl, shg, net):  # req: FR-PAY-02, FR-PAY-03, FR-PAY-04
    got = _march(born, gross, **who)
    assert figures(got, "cpf_total", "cpf_employee", "sdl", "shg", "net") == {
        "cpf_total": total, "cpf_employee": employee, "sdl": Decimal(sdl), "shg": Decimal(shg), "net": Decimal(net)}


def test_the_prd_hand_pinned_march_wage_above_the_ordinary_ceiling_with_a_bonus():  # req: FR-PAY-02, FR-PAY-14
    # 10,000 ordinary (8,000 subject) + a 5,000 bonus: 13,000 x 37% = 4,810; the employee 20% = 2,600 (ER 2,210); net 15,000 - 2,600 = 12,400.
    # January and February were at the 8,000 ceiling: 16,000 counted; 8,000 x 10 months left = 96,000 estimated: 6,000 of room.
    items = basic("10000.00") + [bonus("5000.00", "2026-03-31")]
    got = em.employee_month_doc(month_doc(month="2026-03-01", items=items, ytd={"ow": "16000", "aw_subject": "0", "aw_paid": "0"}))
    assert figures(got, "cpf_total", "cpf_employee", "cpf_employer", "net") == {"cpf_total": 4810, "cpf_employee": 2600, "cpf_employer": 2210, "net": 12400}


def test_the_prd_wage_of_700_uses_the_graduated_table_and_not_the_workbooks_figures():  # req: RND-CPF-GRAD (G4-006)
    # 17% of 700 = 119 + 0.6 x 200 = 120 -> 239; the employee 120, the employer 119 (the workbook's 140 / 119 is a register item)
    got = _march("1990-01-01", "700.00")
    assert figures(got, "cpf_total", "cpf_employee", "cpf_employer", "sdl", "net") == {
        "cpf_total": 239, "cpf_employee": 120, "cpf_employer": 119, "sdl": Decimal("2.00"), "net": 580}


# --- the age band changes the month after the birthday -------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "born, month, total, employee",
    [
        ("1971-03-15", "2026-03-01", 1850, 1000),  # turns 55 in March: still 55 and below (17% + 20% of 5,000)
        ("1971-03-15", "2026-04-01", 1700, 900),  # April: above 55 to 60, 16% + 18% = 34%
        ("1971-03-01", "2026-03-01", 1850, 1000),  # a birthday on the 1st is read the same way: the band changes the month after
        ("1971-03-01", "2026-04-01", 1700, 900),
        ("1971-02-28", "2026-03-01", 1700, 900),  # the birthday was in February: March is the new band
        ("1972-02-29", "2027-03-01", 1850, 1000),  # the 55th birthday would be 29 Feb 2027, which does not exist: 1 March [verify]; still 55 and below
        ("1972-02-29", "2027-04-01", 1775, 950),  # April 2027: above 55 to 60 at 2027's 16.5% + 19% = 35.5% of 5,000
        ("1956-02-29", "2026-03-01", 825, 375),  # the 70th birthday is 1 March 2026: still above 65 to 70 (16.5% of 5,000)
        ("1956-02-29", "2026-04-01", 625, 250),  # April: above 70 (12.5%)
        ("1966-12-20", "2026-12-01", 1700, 900),  # turns 60 in December 2026: still 55 to 60 at 34%
        ("1966-12-20", "2027-01-01", 1300, 650),  # January 2027: above 60 to 65 at 2027's 13% + 13% = 26% of 5,000
        ("1961-06-10", "2026-06-01", 1250, 625),  # turns 65 in June 2026: 60 to 65 at 12.5% + 12.5% = 25% of 5,000
        ("1961-06-10", "2026-07-01", 825, 375),  # July: above 65 to 70 at 9% + 7.5% = 16.5% of 5,000 = 825; the employee 375
        ("1956-06-10", "2026-06-01", 825, 375),  # turns 70 in June 2026: still above 65 to 70
        ("1956-06-10", "2026-07-01", 625, 250),  # July: above 70 at 7.5% + 5% = 12.5% of 5,000
    ],
)
def test_the_age_band_changes_from_the_first_day_of_the_month_after_the_birthday(born, month, total, employee):  # req: FR-PAY-02
    got = work(month=month, person=person(birth_date=born))
    assert figures(got, "cpf_total", "cpf_employee") == {"cpf_total": total, "cpf_employee": employee}


# --- permanent residents -----------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "start, month, joint, total, employee",
    [
        ("2026-02-10", "2026-02-01", False, 450, 250),  # the first year, graduated both: 4% + 5% = 9% of 5,000
        ("2026-02-10", "2027-02-01", False, 450, 250),  # to the end of the month of the first anniversary
        ("2026-02-10", "2027-03-01", False, 1200, 750),  # the second year: 9% + 15% = 24% of 5,000
        ("2026-02-10", "2028-02-01", False, 1200, 750),  # to the end of the month of the second anniversary
        ("2026-02-10", "2028-03-01", False, 1850, 1000),  # from the third year: full rates, 37%
        ("2026-02-10", "2026-06-01", True, 1100, 250),  # first year with the joint election: full employer 17% + graduated employee 5% = 22%
        ("2026-02-10", "2027-03-01", True, 1600, 750),  # second year, full employer 17% + graduated employee 15% = 32%
        (None, "2026-06-01", False, 1850, 1000),  # no start date held: read as past the second year [verify]
    ],
)
def test_a_permanent_residents_rates_follow_the_year_of_residence(start, month, joint, total, employee):  # req: FR-PAY-02, FR-PAY-14
    p = person(residency="permanent_resident", joint_election=joint)
    if start:
        p["pr_start"] = start
    got = work(month=month, person=p)
    assert figures(got, "cpf_total", "cpf_employee") == {"cpf_total": total, "cpf_employee": employee}


def test_a_first_year_permanent_resident_on_700_is_graduated_in_both_shares():  # req: RND-CPF-GRAD, FR-PAY-02
    # 4% of 700 = 28 + 0.15 x 200 = 30 -> 58; the employee 30; the employer 28
    got = work(person=person(residency="permanent_resident", pr_start="2026-02-10"), items=basic("700.00"))
    assert figures(got, "cpf_total", "cpf_employee", "cpf_employer") == {"cpf_total": 58, "cpf_employee": 30, "cpf_employer": 28}


def test_a_contractor_has_no_cpf_no_sdl_and_no_shg():  # req: FR-PAY-03, FR-PAY-04
    got = work(person=person(employee=False, shg_fund="CDAC"), items=basic("5000.00"))
    assert figures(got, "cpf_total", "sdl", "shg", "net") == {"cpf_total": 0, "sdl": 0, "shg": 0, "net": 5000}


# --- wage classes by facts ---------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "extra, cls",
    [
        ({}, "ordinary"),  # for this month, payable on the pay day (the month's last day)
        ({"payable_on": "2026-07-14"}, "ordinary"),  # the 14th of the following month is in time
        ({"payable_on": "2026-07-15"}, "additional"),  # the 15th is not
        ({"for_month": "2026-05-01", "payable_on": "2026-06-10"}, "additional"),  # for another month's employment [verify]
        ({"for_month": "2025-12-01", "payable_on": "2026-06-30"}, "additional"),  # an annual bonus
        ({"cpf_wages": False}, "none"),
    ],
)
def test_a_wage_is_classed_by_its_facts_and_not_its_name(extra, cls):  # req: FR-PAY-17
    item = {"key": "x", "name": "Commission", "amount": "100.00", "cpf_wages": True, "suggested": "additional", **extra}
    assert em.wage_class(item, em.date(2026, 6, 1), em.last_day(em.date(2026, 6, 1))) == cls


def test_the_14th_of_the_following_month_across_a_year_end():  # req: FR-PAY-17
    item = {"key": "x", "name": "Pay", "amount": "1.00", "cpf_wages": True, "payable_on": "2027-01-14"}
    assert em.wage_class(item, em.date(2026, 12, 1), em.date(2026, 12, 31)) == "ordinary"
    assert em.wage_class({**item, "payable_on": "2027-01-15"}, em.date(2026, 12, 1), em.date(2026, 12, 31)) == "additional"


def test_unpaid_leave_comes_off_the_ordinary_wages():  # req: FR-PAY-01, FR-PAY-02
    items = basic("5000.00") + [{"key": "ul", "name": "Unpaid leave", "amount": "-1000.00", "cpf_wages": True}]
    got = work(items=items)  # gross 4,000; ordinary wages 4,000; 37% = 1,480; the employee 800
    assert figures(got, "gross", "ow_subject", "cpf_total", "cpf_employee") == {"gross": 4000, "ow_subject": 4000, "cpf_total": 1480, "cpf_employee": 800}


def test_wages_below_nothing_carry_no_cpf_and_no_sdl():  # req: FR-PAY-02, FR-PAY-03
    got = work(items=basic("-100.00"))
    assert figures(got, "ow_subject", "cpf_total", "sdl") == {"ow_subject": 0, "cpf_total": 0, "sdl": 0}


def test_an_item_that_is_not_cpf_wages_is_in_gross_sdl_and_shg_but_not_in_cpf():  # req: FR-PAY-17, FR-PAY-03, FR-PAY-04 [verify]
    items = basic("1000.00") + [{"key": "t", "name": "Allowance", "amount": "1500.00", "cpf_wages": False}]
    got = work(items=items, person=person(shg_fund="CDAC"))
    # gross 2,500; CPF on 1,000: 370, the employee 200; SDL 0.25% of 2,500 = 6.25; CDAC for wages above 2,000 up to 3,500: 1.00
    assert figures(got, "gross", "cpf_total", "cpf_employee", "sdl", "shg") == {
        "gross": 2500, "cpf_total": 370, "cpf_employee": 200, "sdl": Decimal("6.25"), "shg": Decimal("1.00")}


# --- the additional wage ceiling and the year to date ---------------------------------------------------------------------------------


def test_a_joiners_estimate_counts_the_months_left_and_not_twelve_months_of_this_one():  # req: FR-PAY-14 [verify]
    # May, a joiner: nothing counted yet. Ordinary wages this month 5,000; eight months left including May: 40,000 estimated for the
    # year; 102,000 - 40,000 = 62,000 of additional wages can count. A bonus of 70,000 counts 62,000.
    got = work(month="2026-05-01", items=basic("5000.00") + [bonus("70000.00", "2026-05-31")], ytd={"ow": "0", "aw_subject": "0", "aw_paid": "0"})
    assert Decimal(got["aw_subject"]) == 62000


def test_the_ordinary_wage_estimate_uses_the_capped_ordinary_wages():  # req: FR-PAY-14, FR-PAY-02
    # 9,000 a month is 8,000 subject; July: 6 months counted (48,000) + 6 months left including July at 8,000 = 96,000 estimated.
    # 102,000 - 96,000 = 6,000 of additional wages, less 2,000 already counted = 4,000.
    got = work(month="2026-07-01", items=basic("9000.00") + [bonus("10000.00", "2026-07-31")],
               ytd={"ow": "48000", "aw_subject": "2000", "aw_paid": "2000"})
    assert Decimal(got["aw_subject"]) == 4000


def test_nothing_more_counts_once_the_ceiling_is_used():  # req: FR-PAY-14
    got = work(month="2026-07-01", items=basic("5000.00") + [bonus("1000.00", "2026-07-31")], ytd={"ow": "30000", "aw_subject": "42000", "aw_paid": "42000"})
    assert Decimal(got["aw_subject"]) == 0  # 102,000 - 60,000 = 42,000, all counted


def test_the_last_month_of_employment_trues_up_though_it_is_not_december():  # req: FR-PAY-14
    # A leaver in August: ordinary wages so far 40,000 and 5,000 this month = 45,000 actual; 102,000 - 45,000 = 57,000 allowed.
    # Additional wages paid 60,000 (50,000 counted): 57,000 allowed counts 7,000 more, and this month's bonus of 10,000 is part of the 60,000.
    got = work(month="2026-08-01", items=basic("5000.00") + [bonus("10000.00", "2026-08-31")], last_month=True,
               ytd={"ow": "40000", "aw_subject": "50000", "aw_paid": "50000"})
    assert Decimal(got["aw_subject"]) == 7000
    assert Decimal(got["refund_wages"]) == 0


def test_a_true_up_excess_is_a_refund_task_with_the_employees_share_cents_dropped():  # req: FR-PAY-14
    # Ordinary wages 96,000 actual: 6,000 allowed; 30,000 counted: 24,000 refundable; the employee's share 20% of 24,000 = 4,800.
    got = work(month="2026-12-01", person=person(), items=basic("8000.00"), last_month=True, ytd={"ow": "88000", "aw_subject": "30000", "aw_paid": "30000"})
    assert figures(got, "aw_subject", "refund_wages", "refund_employee") == {"aw_subject": 0, "refund_wages": 24000, "refund_employee": 4800}


def test_a_refund_with_a_part_cent_drops_the_cents_for_the_employee():  # req: FR-PAY-14
    # Actual ordinary wages 89,999.93 + 8,000 = 97,999.93: 4,000.07 allowed; 12,100.14 counted: 8,100.07 refundable.
    # The employee's share 20% of 8,100.07 = 1,620.014: cents dropped, 1,620.01. 
    got = work(month="2026-12-01", items=basic("8000.00"), last_month=True, ytd={"ow": "89999.93", "aw_subject": "12100.14", "aw_paid": "12100.14"})
    assert figures(got, "refund_wages", "refund_employee") == {"refund_wages": Decimal("8100.07"), "refund_employee": Decimal("1620.01")}


def test_a_month_that_pays_additional_wages_with_no_year_to_date_is_refused():  # req: FR-PAY-14
    assert work(items=basic("5000.00") + [bonus("1000.00")]) == {"error": "needs_year_to_date"}


def test_no_year_to_date_is_needed_when_no_additional_wages_are_paid_or_cpf_does_not_apply():  # req: FR-PAY-14
    assert "error" not in work(items=basic("5000.00"))
    assert "error" not in work(person=person(residency="foreigner"), items=basic("5000.00") + [bonus("1000.00")])
    assert "error" not in work(person=person(employee=False), items=basic("5000.00") + [bonus("1000.00")])


# --- SDL ---------------------------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "gross, sdl",
    [
        ("0.00", "0"),  # nothing when the remuneration is not above nothing
        ("100.00", "2.00"),  # 0.25 is held at the S$2 minimum
        ("799.99", "2.00"),  # 1.9999999.. -> 2.00
        ("2000.00", "5.00"),
        ("2002.00", "5.01"),  # 5.005 -> the cent rounds half up
        ("1234.56", "3.09"),  # 3.0864
        ("4500.00", "11.25"),
        ("4500.01", "11.25"),  # 11.250025
        ("10000.00", "11.25"),  # held at the maximum
    ],
)
def test_sdl_per_employee_is_the_rate_rounded_to_the_cent_between_the_minimum_and_maximum(gross, sdl):  # req: RND-SDL, FR-PAY-03
    assert Decimal(work(person=person(residency="foreigner"), items=basic(gross))["sdl"]) == Decimal(sdl)


def test_sdl_payable_is_the_employer_months_total_rounded_down_once():  # req: RND-SDL, FR-PAY-03
    assert em.sdl_payable([Decimal("11.25")] * 4 + [Decimal("10.00"), Decimal("7.50"), Decimal("6.25"), Decimal("2.00")]) == 70  # G.2: 70.75 -> 70
    assert em.sdl_payable([Decimal("2.00"), Decimal("7.50"), Decimal("11.25"), Decimal("10.80")]) == 31  # 31.55 -> 31
    assert em.sdl_payable([]) == 0


def test_an_off_cycle_bonus_recomputes_the_months_sdl_on_the_total_remuneration():  # req: RND-SDL, FR-PAY-03 (G4-002)
    # The employee on S$4,000 gets a S$4,000 bonus in the same month: S$8,000 in all, SDL 11.25 (not 10.00 + 10.00); the March total
    # 70.75 - 10.00 + 11.25 = 72.00, rounded down once: 72 (the payable was 70).
    march = [("1990-01-01", "citizen", "7600.00"), ("1969-01-01", "citizen", "5000.00"), ("1964-01-01", "citizen", "4000.00"),
             ("1959-01-01", "citizen", "3000.00"), ("1950-01-01", "citizen", "2500.00"), ("1990-01-01", "foreigner", "9000.00"),
             ("1990-01-01", "citizen", "15000.00"), ("1990-01-01", "citizen", "700.00")]
    people = [{"id": f"P{i}", "person": person(birth_date=born, residency=res), "items": basic(gross)} for i, (born, res, gross) in enumerate(march)]
    first = em.payroll_run({"schema": "fyniq-payroll-run@1", "name": "march", "month": "2026-03-01", "people": people})
    assert Decimal(first["totals"]["sdl_employees"]) == Decimal("70.75") and Decimal(first["totals"]["sdl_payable"]) == 70
    people[2]["items"] = basic("4000.00") + [{"key": "off", "name": "Off-cycle bonus", "amount": "4000.00", "cpf_wages": True}]
    second = em.payroll_run({"schema": "fyniq-payroll-run@1", "name": "march", "month": "2026-03-01", "people": people})
    assert Decimal(second["totals"]["sdl_employees"]) == Decimal("72.00") and Decimal(second["totals"]["sdl_payable"]) == 72


# --- SHG ---------------------------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    "fund, wages, amount",
    [
        ("CDAC", "2000.00", "0.50"), ("CDAC", "2000.01", "1.00"), ("CDAC", "3500.00", "1.00"), ("CDAC", "3500.01", "1.50"),
        ("CDAC", "7500.00", "2.00"), ("CDAC", "7500.01", "3.00"),
        ("ECF", "1000.00", "2.00"), ("ECF", "1000.01", "4.00"), ("ECF", "7000.00", "12.00"), ("ECF", "7000.01", "16.00"),
        ("MBMF", "10000.00", "24.00"), ("MBMF", "10000.01", "26.00"), ("MBMF", "7000.00", "22.00"),
        ("SINDA", "15000.00", "18.00"), ("SINDA", "15000.01", "30.00"), ("SINDA", "7000.00", "9.00"),
        ("CDAC", "7000.00", "2.00"),  # the CPF Board's four funds at S$7,000 (PRD-001 G.2 note): 2, 12, 22 and 9
    ],
)
def test_shg_by_fund_and_wage_band(fund, wages, amount):  # req: FR-PAY-04
    p = person(shg_fund=fund, residency="citizen")
    assert Decimal(work(person=p, items=basic(wages))["shg"]) == Decimal(amount)


def test_shg_is_not_taken_from_an_employee_who_opted_out_or_has_no_fund():  # req: FR-PAY-04
    assert Decimal(work(person=person(shg_fund="CDAC", shg_opt_out=True))["shg"]) == 0
    assert Decimal(work(person=person())["shg"]) == 0
    assert Decimal(work(person=person(shg_fund="CDAC"), items=basic("0.00"))["shg"]) == 0  # no wages, no contribution [verify]


# --- deductions, reimbursements, held monies and net pay --------------------------------------------------------------------------


def ded(key, kind, amount, **over):
    return {"key": key, "kind": kind, "amount": amount, **over}


def violations(deductions, **over):
    return work(items=basic("4000.00"), deductions=deductions, **over)["violations"]


def test_net_pay_and_the_amount_to_disburse():  # req: FR-PAY-18, FR-PAY-16
    # gross 5,000, employee CPF 1,000, a damage deduction of 100: net 3,900; + a claim of 45 - 200 held = 3,745
    got = work(deductions=[ded("d", "damage_or_loss", "100.00")], reimbursements=["45.00"], held="200.00")
    assert figures(got, "net", "disburse", "deductions", "reimbursements", "held") == {
        "net": 3900, "disburse": 3745, "deductions": 100, "reimbursements": 45, "held": 200}
    assert Decimal(got["employer_cost"]) == Decimal("5861.25")  # 5,000 + employer CPF 850 + SDL 11.25
    assert got["violations"] == []


def test_the_50_percent_limit_counts_everything_but_absence_advances_and_cooperatives():  # req: FR-PAY-16
    assert violations([ded("a", "damage_or_loss", "2000.00")]) == []  # exactly 50% of 4,000 is allowed
    assert violations([ded("a", "damage_or_loss", "2000.01")]) == [{"key": "", "rule": "half"}]
    assert violations([ded("a", "damage_or_loss", "1500.00"), ded("b", "accommodation", "500.01")]) == [{"key": "", "rule": "half"}]
    assert violations([ded("a", "absence", "3000.00"), ded("b", "damage_or_loss", "2000.00")]) == []
    assert violations([ded("a", "cooperative", "3000.00"), ded("b", "court_order", "2000.00")]) == []
    assert violations([ded("a", "recovery", "3000.00"), ded("b", "income_tax", "2000.00")]) == []  # recovery is not counted
    assert violations([ded("a", "income_tax", "1000.00"), ded("b", "cpf", "1000.01")]) == [{"key": "", "rule": "half"}]


def test_the_50_percent_limit_does_not_apply_on_termination():  # req: FR-PAY-16
    assert violations([ded("a", "damage_or_loss", "3900.00")], final=True) == []


def test_a_deduction_is_for_more_than_nothing_and_is_not_counted():  # req: FR-PAY-16
    assert violations([ded("a", "damage_or_loss", "0.00")]) == [{"key": "a", "rule": "amount"}]
    assert violations([ded("a", "damage_or_loss", "0.00"), ded("b", "damage_or_loss", "2000.00")]) == [{"key": "a", "rule": "amount"}]  # no "half": 2,000 is 50% of 4,000


def test_any_other_purpose_needs_written_consent():  # req: FR-PAY-16
    assert violations([ded("a", "other", "10.00")]) == [{"key": "a", "rule": "consent"}]
    assert violations([ded("a", "other", "10.00", consent=True)]) == []


def test_an_advance_is_recovered_in_at_most_12_instalments_of_at_most_25_percent():  # req: FR-PAY-16 (G4-008)
    adv = {"kind": "recovery", "is_advance": True}
    # S$3,000 on S$4,000 in two instalments of 1,500: over 25% (1,000); three of 1,000 pass
    assert violations([ded("a", amount="1500.00", instalment=1, **adv)]) == [{"key": "a", "rule": "advance-quarter"}]
    assert violations([ded("a", amount="1000.00", instalment=1, **adv)]) == []
    assert violations([ded("a", amount="100.00", instalment=12, **adv)]) == []
    assert violations([ded("a", amount="100.00", instalment=13, **adv)]) == [{"key": "a", "rule": "advance-instalments"}]


def test_final_pay_recovers_an_advance_only_as_far_as_the_employee_consented():  # req: FR-PAY-16 [accountant to confirm]
    assert violations([ded("a", "recovery", "800.00", instalment=1, is_advance=True)], final=True) == [{"key": "a", "rule": "advance-final-consent"}]
    assert violations([ded("a", "recovery", "800.00", instalment=1, is_advance=True, consent=True)], final=True) == []


def test_deductions_do_not_change_what_cpf_is_computed_on():  # req: FR-PAY-16, FR-PAY-02
    assert work(deductions=[ded("d", "absence", "1000.00")])["cpf_total"] == work()["cpf_total"]


# --- the run: totals, the entry and the later-run difference -----------------------------------------------------------------------


def a_run():
    a = {"id": "A", "person": person(), "items": basic("5000.00"),
         "deductions": [ded("adv", "recovery", "500.00", instalment=1, is_advance=True)], "reimbursements": ["45.00"]}
    b = {"id": "B", "person": person(residency="foreigner"), "items": basic("3000.00"),
         "deductions": [ded("dmg", "damage_or_loss", "100.00")], "held": "200.00"}
    return {"schema": "fyniq-payroll-run@1", "name": "run", "month": "2026-06-01", "people": [a, b]}


def test_a_runs_totals_and_balanced_entry():  # req: FR-PAY-18, FR-PAY-07, FR-PAY-03
    out = em.payroll_run(a_run())
    # A: gross 5,000; CPF 1,850 (EE 1,000, ER 850); SDL 11.25; net 5,000 - 1,000 - 500 = 3,500; to disburse 3,545; cost 5,861.25.
    # B (a foreigner): gross 3,000; SDL 7.50; net 3,000 - 100 = 2,900; 200 held: to disburse 2,700.
    a, b = out["people"]
    assert figures(a, "net", "disburse") == {"net": 3500, "disburse": 3545}
    assert figures(b, "net", "disburse") == {"net": 2900, "disburse": 2700}
    t = {k: Decimal(v) for k, v in out["totals"].items()}
    assert t["gross"] == 8000 and t["employee_cpf"] == 1000 and t["employer_cpf"] == 850
    assert t["sdl_employees"] == Decimal("18.75") and t["sdl_payable"] == 18  # rounded down once
    assert t["deductions"] == 600 and t["reimbursements"] == 45 and t["held"] == 200
    assert t["net"] == 6400 and t["disburse"] == 6245
    assert t["employer_cost"] == 8868  # 8,000 + 850 + the SDL payable 18
    # Dr 8,000 + 850 + 18 + 45 = 8,913 = Cr 500 (1250) + 100 (the damage deduction's account) + 6,445 (2100: 6,245 + 200 held)
    # + 1,868 (2110: 1,000 + 850 + 18)
    p = out["postings"]
    assert Decimal(p["salary"]["debit"]) == 8000 and Decimal(p["6020"]["debit"]) == 850 and Decimal(p["6030"]["debit"]) == 18
    assert Decimal(p["2150"]["debit"]) == 45 and Decimal(p["1250"]["credit"]) == 500 and Decimal(p["deductions"]["credit"]) == 100
    assert Decimal(p["2100"]["credit"]) == 6445 and Decimal(p["2110"]["credit"]) == 1868
    assert out["balanced"] is True


def test_a_person_refused_is_left_out_of_the_totals():  # req: FR-PAY-14, FR-PAY-18
    run = a_run()
    run["people"][0]["items"].append(bonus("1000.00"))
    out = em.payroll_run(run)
    assert out["people"][0] == {"id": "A", "error": "needs_year_to_date"}
    assert Decimal(out["totals"]["gross"]) == 3000


def test_a_later_run_posts_only_the_difference():  # req: FR-PAY-01, B.3 (a later run recomputes the employee-month)
    # Posted: S$4,000 (CPF 1,480 = EE 800 + ER 680; SDL 10.00; net 3,200). Now an off-cycle S$4,000 in the month: S$8,000
    # (CPF 2,960 = 1,600 + 1,360; SDL 11.25; net 6,400): the difference is 4,000, 800, 680, 1.25, 0, 0, 0, 3,200, 3,200.
    entry = {"id": "A", "person": person(), "items": basic("4000.00") + [{"key": "off", "name": "Off-cycle", "amount": "4000.00", "cpf_wages": True}],
             "posted": {"items": basic("4000.00")}}
    out = em.payroll_run({"schema": "fyniq-payroll-run@1", "name": "later", "month": "2026-06-01", "people": [entry]})
    d = out["people"][0]["difference"]
    assert {k: Decimal(v) for k, v in d.items()} == {
        "gross": 4000, "cpf_employee": 800, "cpf_employer": 680, "sdl": Decimal("1.25"), "shg": 0, "deductions": 0,
        "reimbursements": 0, "net": 3200, "disburse": 3200}


def test_one_person_twice_in_a_run_is_refused():  # req: FR-PAY-01 (ENG-20: one employee-month holds every item)
    run = a_run()
    run["people"][1]["id"] = "A"
    with pytest.raises(ScenarioError):
        em.payroll_run(run)


@pytest.mark.parametrize(
    "bad",
    [
        {"items": [{"key": "x", "name": "x", "amount": "1.001", "cpf_wages": True}]},
        {"month": "2026-06-02"},
        {"packs": {"cpf": "sg-cpf@2026.2", "extra": "x"}},
        {"deductions": [{"key": "d", "kind": "bribe", "amount": "1.00"}]},
    ],
)
def test_a_document_that_is_not_valid_input_is_refused(bad):  # req: QA-ENG-01
    with pytest.raises(ScenarioError):
        work(**bad)


def test_the_run_and_month_schemas_check_the_input_the_oracle_reads():  # req: QA-ENG-02
    for schema in ("payroll.schema.json", "payroll-run.schema.json"):
        assert json.loads((ROOT / "test" / "differential" / schema).read_text())["title"].startswith("fyniq-payroll-")


# --- independent pinned data: the packs' own hand-pinned tables, read here by this test's own reader --------------------------------


def _rows(pack, version, name):
    with (PACKS / pack / version / "pinned" / name).open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def _born(age, month):
    """A birth date that puts the person at ``age`` for the wage month: in the 55 to 70 bands the birthday is long past, and the CPF Board's
    own 'age 55' (up to 55) has its birthday in the wage month itself, which the band reads from the month after."""
    year, mm = month.split("-")
    return f"{int(year) - age}-{mm}-15" if age in (55, 60, 65, 70) else f"{int(year) - age}-01-15"


def test_the_packs_pinned_graduated_cases_by_group_and_age():  # req: RND-CPF-GRAD, FR-PAY-02
    rows = _rows("sg-cpf", "2026.2", "low_wages.csv")
    assert len(rows) > 100
    held = [r for r in rows if r["want_total"] != "none"]
    assert len(held) > 100
    for r in held:
        born = _born(int(r["age"]), r["month"])
        got = em.cpf_split("sg-cpf@2026.2", r["group"], em.date.fromisoformat(born), em.date.fromisoformat(r["month"] + "-01"), Decimal(r["wage"]))
        want = (Decimal(r["want_total"]).quantize(Decimal(1), rounding=ROUND_HALF_UP), Decimal(r["want_employee"]).quantize(Decimal(1), rounding=ROUND_FLOOR))
        assert got == want, r["case"]


def test_the_packs_pinned_rates_by_group_and_age():  # req: FR-PAY-02, RND-CPF
    checked = 0
    for r in _rows("sg-cpf", "2026.2", "rates.csv"):
        if r["want_employer"] == "none":
            continue
        born = _born(int(r["age"]), r["month"])
        got = em._rates("sg-cpf@2026.2", r["group"], em.date.fromisoformat(born), em.date.fromisoformat(r["month"] + "-01"))
        assert got == (Decimal(r["want_employer"]), Decimal(r["want_employee"])), r["case"]
        checked += 1
    assert checked > 30


def test_the_packs_pinned_maxima_are_the_band_at_the_ordinary_wage_ceiling():  # req: RND-CPF, FR-PAY-02
    rows = _rows("sg-cpf", "2026.2", "maxima.csv")
    assert len(rows) > 20
    for r in rows:
        born = _born(int(r["age"]), r["month"])
        got = em.cpf_split("sg-cpf@2026.2", r["group"], em.date.fromisoformat(born), em.date.fromisoformat(r["month"] + "-01"), Decimal(8000))
        assert got == (Decimal(r["want_total"]), Decimal(r["want_employee"])), r["case"]


def test_the_packs_pinned_sdl_and_shg_and_ceilings():  # req: RND-SDL, FR-PAY-04, FR-PAY-02
    for r in _rows("sg-sdl", "2026.1", "levy.csv"):
        if r["want"] != "none":
            assert em.sdl_employee("sg-sdl@2026.1", em.date.fromisoformat(r["month"] + "-01"), Decimal(r["wages"])) == Decimal(r["want"]), r["case"]
    shg = _rows("sg-shg", "2026.1", "bands.csv")
    for r in shg:
        if r["want"] != "none":
            assert em.shg_fund_amount("sg-shg@2026.1", em.date.fromisoformat(r["month"] + "-01"), r["fund"], Decimal(r["wages"])) == Decimal(r["want"]), r["case"]
    for r in _rows("sg-cpf", "2026.2", "ceilings.csv"):
        if r["want"] != "none":
            assert em._ceiling("sg-cpf@2026.2", r["ceiling"], em.date.fromisoformat(r["month"] + "-01")) == Decimal(r["want"]), r["case"]
