"""The KPIs' rules, worked by hand on small charts (FR-KPI-02, FR-KPI-03, RND-KPI; README, "KPIs").

The whole set is worked by hand in ``test_pending_golden.py::test_pending_kpis``; these tests pin the rules
one at a time: the financial year's three-month window, the undefined states of G4-047 and G4-048, booked
MRR without a subscription stream, and RND-KPI's single rounding.
"""

import pytest

from builders import journal, line
from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, scenario_from_json


def _account(code: str, kind: str, **attributes: str) -> dict:
    return {"code": code, "type": kind, "group": False, "active": True, **attributes}


CHART = [
    _account("1010", "asset", cash_flow_line="Cash & cash equivalents"),
    _account("1100", "asset", cash_flow_line="Receivables"),
    _account("2010", "liability", cash_flow_line="Payables & accruals"),
    _account("2500", "liability", cash_flow_line="Borrowings"),
    _account("3100", "equity", role="retained-earnings"),
    _account("4010", "revenue", report_group="Subscription revenue"),
    _account("4020", "revenue", report_group="Consulting"),
    _account("6010", "expense", function="S&M"),
]


def _data(*documents: dict, counts: dict | None = None, year_start_month: int = 1,
          accounts: list[dict] | None = None) -> dict:
    return {
        "schema": "fyniq-scenario@1", "name": "kpis-by-hand", "base_currency": "SGD", "currencies": {"SGD": 2},
        "accounts": accounts or CHART, "documents": list(documents), "reversals": [], "statements": True,
        "kpis": {"year_start_month": year_start_month, "counts": counts or {}},
    }


def _kpis(*documents: dict, **options) -> dict:
    return Ledger(scenario_from_json(_data(*documents, **options))).result()["kpis"]


def _sale(doc_id: str, day: str, amount: str, account: str = "4010") -> dict:
    return journal(doc_id, day, line("1010", "SGD", amount), line(account, "SGD", f"-{amount}"))


def _cost(doc_id: str, day: str, amount: str) -> dict:
    return journal(doc_id, day, line("6010", "SGD", amount), line("1010", "SGD", f"-{amount}"))


# req: FR-KPI-02
def test_three_month_averages_stay_inside_the_financial_year():
    docs = [_sale("S1", "2026-03-10", "3000.00"), _sale("S2", "2026-04-10", "600.00"), _sale("S3", "2026-05-10", "1200.00"),
            journal("R1", "2026-05-20", line("1100", "SGD", "450.00"), line("4020", "SGD", "-450.00"))]
    # A year starting in April: May averages April and May only, 450.00 × 30 × 2 ÷ (600.00 + 1,650.00) = 12.0.
    assert _kpis(*docs, year_start_month=4)["2026-05"]["dso"] == "12.0"
    # A calendar year: March to May, 450.00 × 30 × 3 ÷ 5,250.00 = 7.71.
    assert _kpis(*docs, year_start_month=1)["2026-05"]["dso"] == "7.7"
    # April is the year's first month: April alone, receivables 0.00.
    assert _kpis(*docs, year_start_month=4)["2026-04"]["dso"] == "0.0"


# req: FR-KPI-02, FR-KPI-03
def test_runway_averages_the_burn_and_says_not_burning_when_the_average_is_not_above_zero():
    docs = [_sale("S1", "2026-01-02", "1000.00"), _cost("C1", "2026-02-02", "400.00"), _cost("C2", "2026-03-02", "300.00")]
    k = _kpis(*docs)
    assert (k["2026-01"]["net_burn"], k["2026-01"]["runway"]) == ("-1000.00", "not-burning")
    assert k["2026-02"]["runway"] == "not-burning"  # (-1,000.00 + 400.00) ÷ 2 < 0
    assert k["2026-03"]["runway"] == "not-burning"  # (-1,000.00 + 400.00 + 300.00) ÷ 3 < 0
    k = _kpis(_sale("S1", "2026-01-02", "1000.00"), _cost("C1", "2026-02-02", "300.00"), _cost("C2", "2026-03-02", "400.00"),
              _cost("C3", "2026-04-02", "200.00"))
    # April: February to April burn 300.00 + 400.00 + 200.00 = 900.00, 300.00 a month; cash 100.00 left.
    assert k["2026-04"]["runway"] == "0.3"


# req: FR-KPI-03, G4-047
def test_without_counts_the_customer_kpis_are_undefined_and_the_ledger_ones_still_show():
    docs = [_sale("S1", "2026-01-02", "900.00"), _cost("C1", "2026-01-03", "300.00")]
    k = _kpis(*docs)["2026-01"]
    assert [k[key] for key in ("arpa", "churn", "cac", "ltv", "ltv_to_cac", "cac_payback", "headcount",
                               "revenue_per_employee")] == [None] * 8
    assert (k["booked_mrr"], k["gross_margin"], k["net_margin"]) == ("900.00", "100.0", "66.7")
    # With the month's counts entered: 3 paying, 1 new, 1 lost (3 at the start), 2 people.
    k = _kpis(*docs, counts={"2026-01": {"paying_customers": 3, "new_customers": 1, "customers_lost": 1,
                                         "headcount": 2}})["2026-01"]
    assert (k["arpa"], k["churn"], k["cac"]) == ("300.00", "33.3", "300.00")  # 900 ÷ 3; 1 ÷ 3; 300 ÷ 1
    assert k["ltv"] == "900.00"  # 300.00 × 100% ÷ (1 ÷ 3), from the exact churn, never the rounded 33.3%
    assert (k["ltv_to_cac"], k["cac_payback"]) == ("3.0", "1.0")
    assert (k["headcount"], k["revenue_per_employee"]) == ("2", "5400.00")  # 900.00 × 12 ÷ 2


# req: FR-KPI-03, G4-048
def test_negative_equity_in_a_month_that_makes_cash():
    # January loses 2,000.00 in cash; in February a customer pays 3,000.00 ahead, so the two months' burn is
    # 2,000.00 - 3,000.00 < 0 while equity is still -2,000.00.
    chart = [*CHART, _account("2400", "liability", cash_flow_line="Deferred revenue")]
    docs = [journal("L1", "2026-01-02", line("1010", "SGD", "5000.00"), line("2500", "SGD", "-5000.00")),
            _cost("C1", "2026-01-03", "2000.00"),
            journal("D1", "2026-02-02", line("1010", "SGD", "3000.00"), line("2400", "SGD", "-3000.00"))]
    k = _kpis(*docs, accounts=chart)
    assert k["2026-01"]["runway"] == "1.5"  # 3,000.00 ÷ 2,000.00
    assert (k["2026-02"]["debt_to_equity"], k["2026-02"]["runway"], k["2026-02"]["burn_multiple"]) == (
        None, "not-burning", None)
    assert k["2026-02"]["net_burn"] == "-3000.00"


# req: FR-KPI-02
def test_without_a_subscription_stream_booked_mrr_and_what_needs_it_are_undefined():
    chart = [a if a["code"] != "4010" else _account("4010", "revenue", report_group="Sales") for a in CHART]
    k = _kpis(_sale("S1", "2026-01-02", "900.00"), _cost("C1", "2026-01-03", "300.00"), accounts=chart,
              counts={"2026-01": {"paying_customers": 3, "new_customers": 1, "customers_lost": 1}})["2026-01"]
    assert [k[key] for key in ("booked_mrr", "arr", "arpa", "ltv", "ltv_to_cac", "cac_payback", "burn_multiple")] == [None] * 7
    assert k["cac"] == "300.00"  # needs no MRR
    # Other income is never a stream, whatever its group says.
    chart = [a if a["code"] != "4010" else _account("4010", "revenue", report_group="Subscriptions", function="Other")
             for a in CHART]
    assert _kpis(_sale("S1", "2026-01-02", "900.00"), accounts=chart)["2026-01"]["booked_mrr"] is None


# req: FR-KPI-02
def test_the_burn_multiple_needs_burn_and_new_arr():
    docs = [_sale("S1", "2026-01-02", "100.00"), _cost("C1", "2026-01-03", "1000.00"),
            _sale("S2", "2026-02-02", "150.00"), _cost("C2", "2026-02-03", "1000.00"),
            _sale("S3", "2026-03-02", "150.00"), _cost("C3", "2026-03-03", "1000.00"),
            _sale("S4", "2026-04-02", "100.00"), _cost("C4", "2026-04-03", "1000.00")]
    k = _kpis(*docs)
    assert k["2026-01"]["burn_multiple"] == "0.8"  # 900.00 ÷ 1,200.00 of new ARR
    assert k["2026-02"]["burn_multiple"] == "1.4"  # 850.00 ÷ (1,800.00 - 1,200.00) = 1.417
    assert k["2026-03"]["burn_multiple"] is None  # ARR did not grow
    assert k["2026-04"]["burn_multiple"] is None  # ARR shrank to 1,200.00
    # The month's own burn decides, not the three-month average: January made 5,000.00 of consulting cash, so
    # February's average is not burning, but February burned 900.00 for 1,200.00 of new ARR.
    k = _kpis(_sale("S1", "2026-01-02", "5000.00", account="4020"),
              _sale("S2", "2026-02-02", "100.00"), _cost("C2", "2026-02-03", "1000.00"))
    assert (k["2026-02"]["runway"], k["2026-02"]["burn_multiple"]) == ("not-burning", "0.8")


# req: RND-KPI
def test_each_kpi_is_rounded_once_half_away_from_zero():
    docs = [_sale("S1", "2026-01-02", "200.00"), _cost("C1", "2026-01-03", "0.05")]
    k = _kpis(*docs, counts={"2026-01": {"paying_customers": 3, "new_customers": 2, "customers_lost": 0}})["2026-01"]
    assert k["arpa"] == "66.67"  # 200.00 ÷ 3
    assert k["cac"] == "0.03"  # 0.05 ÷ 2 = 0.025, away from zero
    assert k["net_margin"] == "100.0"  # 199.95 ÷ 200.00 = 99.975%, to 1 decimal
    assert k["gross_margin"] == "100.0"


# req: QA-ENG-02
def test_the_format_refuses_kpis_without_statements_and_a_customer_count_alone():
    data = _data()
    del data["statements"]
    with pytest.raises(ScenarioError, match="statements"):
        scenario_from_json(data)
    with pytest.raises(ScenarioError, match="customers"):
        scenario_from_json(_data(counts={"2026-01": {"paying_customers": 3}}))
    with pytest.raises(ScenarioError):
        scenario_from_json(_data(year_start_month=13))
