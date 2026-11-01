"""Properties of the statements over generated scenarios (FR-RPT-01, INV-10, INV-11; QA-ENG-01).

Each property reads the statements back against the posted entries and the trial balance, which the
statements never consult, so a misplaced or dropped line shows.
"""

from collections import Counter
from decimal import Decimal

from hypothesis import HealthCheck, given, settings
from hypothesis import strategies as st

from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import scenario_from_json
from oracle.statements import is_balance_sheet
from oracle.strategies import scenarios
from oracle.strategies_p1 import p1_scenarios
from oracle.strategies_statements import with_statements

GENERATED = st.one_of(with_statements(scenarios()), with_statements(p1_scenarios()))


def _d(text: str) -> Decimal:
    return Decimal(text)


def _sum(values) -> Decimal:
    return money.total(tuple(_d(v) for v in values))


def _run(data: dict) -> tuple[Ledger, dict]:
    ledger = Ledger(scenario_from_json(data))
    return ledger, ledger.result()


# req: FR-RPT-01, QA-ENG-02
@given(GENERATED)
def test_every_month_from_the_first_posting_to_the_last_is_stated(data):
    _, result = _run(data)
    months = sorted(result["trial_balance"])
    stated = list(result["statements"])
    if not months:
        assert stated == []
        return
    assert stated[0] == months[0] and stated[-1] == months[-1]
    assert set(months) <= set(stated)
    for earlier, later in zip(stated, stated[1:]):
        year, month = int(earlier[:4]), int(earlier[5:])
        assert later == (f"{year + 1:04d}-01" if month == 12 else f"{year:04d}-{month + 1:02d}")


# req: FR-RPT-01
@given(GENERATED)
def test_the_profit_is_the_trial_balances_revenue_and_expense_and_the_lines_add_up(data):
    ledger, result = _run(data)
    accounts = ledger.scenario.accounts
    for month, s in result["statements"].items():
        pl = s["profit_and_loss"]
        movement = result["trial_balance"].get(month, {})
        assert _d(pl["net_profit"]) == -_sum(v for code, v in movement.items() if not is_balance_sheet(accounts[code]))
        sections = [pl[k]["total"] for k in ("revenue", "cost_of_sales", "operating_expenses",
                                             "depreciation_and_amortisation", "other_income",
                                             "finance_costs_and_other_expenses")]
        assert _sum([*sections, pl["income_tax"]]) == _d(pl["net_profit"])
        assert _d(pl["gross_profit"]) == _sum([pl["revenue"]["total"], pl["cost_of_sales"]["total"]])
        assert _d(pl["ebitda"]) == _sum([pl["gross_profit"], pl["operating_expenses"]["total"]])
        assert _sum(pl["operating_expenses"]["functions"].values()) == _d(pl["operating_expenses"]["total"])
        for key in ("revenue", "cost_of_sales", "operating_expenses", "depreciation_and_amortisation", "other_income",
                    "finance_costs_and_other_expenses"):
            assert _sum(pl[key]["groups"].values()) == _d(pl[key]["total"])
        assert ("percent_of_revenue" in pl) == (not _d(pl["revenue"]["total"]).is_zero())


# req: INV-10, INV-11, FR-RPT-01
@given(GENERATED)
def test_with_every_account_placed_the_balance_sheet_balances_and_the_cash_flow_ties(data):
    _, result = _run(data)
    previous = None
    for s in result["statements"].values():
        bs, cf, equity, checks = s["balance_sheet"], s["cash_flow"], s["equity_movement"], s["checks"]
        if not checks["unplaced"]:
            assert checks["balances"] and checks["ties"]
        assert checks["equity"]
        assert checks["balances"] == (bs["total_assets"] == bs["total_liabilities_and_equity"])
        assert checks["ties"] == (_sum([cf["opening_cash"], cf["net_change"]]) == _d(cf["closing_cash"]))
        assert _d(cf["net_change"]) == _sum([cf["operating"]["total"], cf["investing"], cf["financing"]])
        op = cf["operating"]
        assert _d(op["total"]) == _sum([op["net_profit"], op["depreciation_and_amortisation"],
                                        op["investing_and_financing_results"], *op["working_capital"].values()])
        assert op["net_profit"] == s["profit_and_loss"]["net_profit"] == equity["profit"]
        assert equity["closing"] == bs["equity"]["total"]
        for key in ("current_assets", "non_current_assets", "current_liabilities", "borrowings", "equity"):
            assert _sum(bs[key]["groups"].values()) == _d(bs[key]["total"])
        if previous is not None:
            assert cf["opening_cash"] == previous["cash_flow"]["closing_cash"]
            assert equity["opening"] == previous["equity_movement"]["closing"]
        previous = s


# req: INV-11, FR-RPT-01
@given(GENERATED)
def test_non_cash_entries_are_investing_or_financing_and_touch_no_cash(data):
    ledger, result = _run(data)
    accounts = ledger.scenario.accounts
    entries = {(e.document, e.kind, e.posting_date.isoformat()): e for e in ledger.entries}
    for s in result["statements"].values():
        for item in s["cash_flow"]["non_cash"]:
            entry = entries[(item["document"], item["kind"], item["posting_date"])]
            assert not any(accounts[ln.account].cash_flow_line == "Cash & cash equivalents"
                           and accounts[ln.account].role != "retained-earnings"
                           and is_balance_sheet(accounts[ln.account]) for ln in entry.lines)


# req: QA-ENG-02
def test_the_generator_reaches_every_shape_of_the_statements():
    seen: Counter[str] = Counter()

    @settings(max_examples=120, derandomize=True, database=None, deadline=None, suppress_health_check=list(HealthCheck))
    @given(GENERATED)
    def collect(data):
        _, result = _run(data)
        for s in result["statements"].values():
            cf, pl, checks = s["cash_flow"], s["profit_and_loss"], s["checks"]
            seen["non-cash"] += bool(cf["non_cash"])
            seen["investing"] += not _d(cf["investing"]).is_zero()
            seen["financing"] += not _d(cf["financing"]).is_zero()
            seen["results taken out"] += not _d(cf["operating"]["investing_and_financing_results"]).is_zero()
            seen["depreciation added back"] += not _d(cf["operating"]["depreciation_and_amortisation"]).is_zero()
            seen["income tax"] += not _d(pl["income_tax"]).is_zero()
            seen["percentages"] += "percent_of_revenue" in pl
            seen["unplaced"] += bool(checks["unplaced"])
            seen["not tied"] += not checks["ties"]

    collect()
    assert all(seen[shape] for shape in ("non-cash", "investing", "financing", "results taken out",
                                         "depreciation added back", "income tax", "percentages", "unplaced",
                                         "not tied")), seen


WITH_KPIS = st.one_of(with_statements(scenarios(), kpis=True), with_statements(p1_scenarios(), kpis=True))


# req: FR-KPI-02, FR-KPI-03, RND-KPI
@given(WITH_KPIS)
def test_the_kpis_agree_with_the_statements_they_read(data):
    _, result = _run(data)
    counts = data["kpis"].get("counts", {})
    assert list(result["kpis"]) == list(result["statements"])
    for month, k in result["kpis"].items():
        s = result["statements"][month]
        pl, bs, cf = s["profit_and_loss"], s["balance_sheet"], s["cash_flow"]
        percent = pl.get("percent_of_revenue", {})
        assert (k["gross_margin"], k["ebitda_margin"], k["net_margin"]) == (
            percent.get("gross_profit"), percent.get("ebitda"), percent.get("net_profit"))
        assert _d(k["working_capital"]) == _d(bs["current_assets"]["total"]) - _d(bs["current_liabilities"]["total"])
        assert _d(k["net_burn"]) == -(_d(cf["operating"]["total"]) + _d(cf["investing"]))
        if k["booked_mrr"] is not None:
            assert _d(k["arr"]) == _d(k["booked_mrr"]) * 12
        entered = counts.get(month, {})
        assert k["headcount"] == (str(entered["headcount"]) if "headcount" in entered else None)
        if "paying_customers" not in entered:
            assert [k[key] for key in ("arpa", "churn", "cac", "ltv", "ltv_to_cac", "cac_payback")] == [None] * 6
        assert k["contracted_mrr"] is None
        if k["runway"] != "not-burning":
            assert _d(k["runway"]) * _d(cf["closing_cash"]) >= 0  # cash ÷ a positive average burn
