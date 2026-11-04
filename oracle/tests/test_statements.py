"""The statements' rules, worked by hand on small charts (FR-RPT-01 rev 0.159; README, "Statements").

The whole layout is worked by hand in ``test_pending_golden.py::test_pending_statements``; these tests pin
each rule on its own: where an account goes, what makes an entry investing, financing or non-cash, the
percentages, and the scenarios the format refuses.
"""

from decimal import Decimal

import pytest

from builders import journal, line
from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import Account, ScenarioError, scenario_from_json
from oracle.statements import balance_sheet_section, profit_and_loss_section


def _account(code: str, kind: str, **attributes: str) -> dict:
    return {"code": code, "type": kind, "group": False, "active": True, **attributes}


CHART = [
    _account("1010", "asset", report_group="Cash and bank", cash_flow_line="Cash & cash equivalents"),
    _account("1510", "asset", report_group="Equipment", cash_flow_line="Capital expenditure"),
    _account("2010", "liability", report_group="Payables", cash_flow_line="Payables & accruals"),
    _account("2600", "liability", report_group="Bank loan", cash_flow_line="Borrowings"),
    _account("3100", "equity", role="retained-earnings", report_group="Retained earnings"),
    _account("4010", "revenue", report_group="Sales"),
    _account("6010", "expense", report_group="Office", function="G&A"),
]


def _scenario(*documents: dict, accounts: list[dict] | None = None, statements: bool = True,
              currencies: dict | None = None, **more: object) -> dict:
    data = {
        "schema": "fyniq-scenario@1",
        "name": "statements-by-hand",
        "base_currency": "SGD",
        "currencies": currencies or {"SGD": 2},
        "accounts": accounts if accounts is not None else CHART,
        "documents": list(documents),
        "reversals": [],
    }
    if statements:
        data["statements"] = True
    return {**data, **more}


def _statements(*documents: dict, **options) -> dict:
    return Ledger(scenario_from_json(_scenario(*documents, **options))).result()["statements"]


def _pl(code: str, kind: str, depreciation_group: str | None = "D&A", **attributes: str) -> str:
    return profit_and_loss_section(Account(code, kind, False, True, **attributes), depreciation_group)


# req: FR-RPT-01
def test_each_revenue_and_expense_account_goes_to_the_first_section_that_applies():
    assert _pl("8010", "expense", role="income-tax-expense", function="COGS", report_group="D&A") == "income_tax"
    assert _pl("4010", "revenue") == "revenue"
    assert _pl("4020", "revenue", function="G&A") == "revenue"  # only Other moves revenue out of the streams
    assert _pl("4900", "revenue", function="Other") == "other_income"
    # The depreciation account's group before the function (rev 0.159): EBITDA excludes D&A by definition.
    assert _pl("5010", "expense", function="COGS", report_group="D&A") == "depreciation_and_amortisation"
    assert _pl("5020", "expense", function="COGS", report_group="Hosting") == "cost_of_sales"
    assert _pl("6620", "expense", function="Other", report_group="D&A") == "depreciation_and_amortisation"
    assert _pl("7010", "expense", function="Other") == "finance_costs_and_other_expenses"
    assert _pl("6010", "expense", function="R&D") == "operating_expenses"
    assert _pl("6300", "expense") == "operating_expenses"
    # Without a depreciation account there is no such group; an account without a group is its own code's.
    assert _pl("6610", "expense", depreciation_group=None) == "operating_expenses"
    assert _pl("6610", "expense", depreciation_group="6610") == "depreciation_and_amortisation"


# req: FR-RPT-01, INV-10
def test_a_balance_sheet_account_is_placed_by_its_cash_flow_line():
    def section(kind: str, line_: str | None = None, role: str | None = None) -> str | None:
        return balance_sheet_section(Account("x", kind, False, True, role=role, cash_flow_line=line_))

    assert section("asset", "Cash & cash equivalents") == "current_assets"
    assert section("asset", "Receivables") == "current_assets"
    assert section("asset", "Other current assets") == "current_assets"
    assert section("asset", "Capital expenditure") == "non_current_assets"
    assert section("asset", "Depreciation & amortisation") == "non_current_assets"
    for line_ in ("Payables & accruals", "Payroll liabilities", "Tax liabilities", "Deferred revenue"):
        assert section("liability", line_) == "current_liabilities"
    assert section("liability", "Borrowings") == "borrowings"
    assert section("equity", "Share capital") == "equity"
    assert section("equity", "Dividends & other equity") == "equity"
    # A line places an account only on its own side (rev 0.159): any other combination is unplaced.
    assert section("liability", "Receivables") is None
    assert section("liability", "Share capital") is None
    assert section("equity", "Cash & cash equivalents") is None
    assert section("equity", "Borrowings") is None
    assert section("asset", "Borrowings") is None
    assert section("asset", "Payables & accruals") is None
    assert section("equity", None, role="retained-earnings") == "equity"
    assert section("equity", "Receivables", role="retained-earnings") == "equity"  # its role wins
    assert section("asset", None) is None
    assert section("asset", "cash & cash equivalents") is None  # outside the set, by exact text


# req: FR-RPT-01, INV-11
def test_an_entry_moving_capital_expenditure_is_investing_even_with_a_loan_in_it():
    s = _statements(
        journal("J1", "2026-01-05", line("1010", "SGD", "5000.00"), line("2600", "SGD", "-5000.00")),
        # Equipment bought with the rest of the loan and some cash: one entry, so all its cash is investing.
        journal("J2", "2026-01-06", line("1510", "SGD", "7000.00"), line("2600", "SGD", "-4000.00"),
                line("1010", "SGD", "-3000.00")),
        # Equipment on account moved no money: non-cash investing. Paying for it later is operating.
        journal("J3", "2026-01-07", line("1510", "SGD", "500.00"), line("2010", "SGD", "-500.00")),
        journal("J4", "2026-01-08", line("2010", "SGD", "500.00"), line("1010", "SGD", "-500.00")),
    )["2026-01"]
    cf = s["cash_flow"]
    assert (cf["investing"], cf["financing"], cf["operating"]["total"]) == ("-3000.00", "5000.00", "-500.00")
    assert cf["operating"]["working_capital"] == {"Payables & accruals": "-500.00"}
    assert cf["non_cash"] == [{"document": "J3", "kind": "post", "posting_date": "2026-01-07", "section": "investing"}]
    assert (cf["net_change"], cf["closing_cash"]) == ("1500.00", "1500.00")
    assert s["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}


# req: FR-RPT-01, INV-11
def test_an_entry_with_a_line_on_a_cash_account_moved_money_even_when_its_cash_nets_to_zero():
    chart = [*CHART, _account("1020", "asset", report_group="Cash and bank", cash_flow_line="Cash & cash equivalents")]
    cf = _statements(
        journal("J1", "2026-01-05", line("1010", "SGD", "100.00"), line("1020", "SGD", "-100.00"),
                line("1510", "SGD", "50.00"), line("2010", "SGD", "-50.00")),
        accounts=chart,
    )["2026-01"]["cash_flow"]
    assert (cf["investing"], cf["non_cash"]) == ("0.00", [])


# req: FR-RPT-01, INV-10, INV-11
def test_an_unplaced_account_breaks_only_balances_and_the_cash_flow_keeps_its_movement():
    # 1990 (an asset with no line) and 2990 (a liability with no line) sit in no section.
    chart = [*CHART, _account("1990", "asset", report_group="Suspense"),
             _account("2990", "liability", report_group="Accruals, unplaced")]
    s = _statements(
        journal("J1", "2026-01-05", line("1990", "SGD", "50.00"), line("1010", "SGD", "-50.00")),  # 50.00 left the bank
        journal("J2", "2026-01-06", line("6010", "SGD", "30.00"), line("2990", "SGD", "-30.00")),  # a cost accrued to it
        accounts=chart,
    )["2026-01"]
    cf = s["cash_flow"]
    # Operating: net profit -30.00; other accounts -(+50.00) - (-30.00) = -20.00; total -50.00, the 50.00 that left.
    assert cf["operating"] == {"net_profit": "-30.00", "depreciation_and_amortisation": "0.00",
                               "investing_and_financing_results": "0.00", "working_capital": {},
                               "other_accounts": "-20.00", "total": "-50.00"}
    assert (cf["net_change"], cf["opening_cash"], cf["closing_cash"]) == ("-50.00", "0.00", "-50.00")
    # Assets -50.00 (the bank); liabilities 0.00 and equity -30.00 (the loss): 20.00 apart, the two unplaced balances.
    assert (s["balance_sheet"]["total_assets"], s["balance_sheet"]["total_liabilities_and_equity"]) == ("-50.00", "-30.00")
    assert s["checks"] == {"balances": False, "ties": True, "equity": True, "unplaced": ["1990", "2990"]}


# req: FR-RPT-01, INV-10, INV-11
def test_a_line_of_the_other_side_places_nothing():
    # 2990 is a liability on Share capital and 3990 an equity account on Cash & cash equivalents: both unplaced.
    chart = [*CHART, _account("2990", "liability", cash_flow_line="Share capital"),
             _account("3990", "equity", cash_flow_line="Cash & cash equivalents")]
    s = _statements(
        # Money in against the liability: not financing (2990 is in no section), so operating, with the 200.00 in
        # other accounts.
        journal("J1", "2026-01-05", line("1010", "SGD", "200.00"), line("2990", "SGD", "-200.00")),
        # 3990 is not cash: 100.00 moved to it leaves 1010, operating, other accounts -(+100.00).
        journal("J2", "2026-01-06", line("3990", "SGD", "100.00"), line("1010", "SGD", "-100.00")),
        accounts=chart,
    )["2026-01"]
    cf = s["cash_flow"]
    assert (cf["operating"]["other_accounts"], cf["operating"]["total"]) == ("100.00", "100.00")  # 200.00 - 100.00
    assert (cf["financing"], cf["closing_cash"]) == ("0.00", "100.00")  # 3990's 100.00 is not cash
    assert s["balance_sheet"]["equity"] == {"total": "0.00", "groups": {}}  # 3990 is not in equity
    assert s["checks"] == {"balances": False, "ties": True, "equity": True, "unplaced": ["2990", "3990"]}


# req: FR-RPT-01
def test_a_cogs_account_in_the_depreciation_group_is_depreciation_below_ebitda():
    chart = [*CHART, _account("6610", "expense", role="depreciation", report_group="D&A"),
             _account("5010", "expense", report_group="D&A", function="COGS"),
             _account("1590", "asset", report_group="Equipment", cash_flow_line="Depreciation & amortisation")]
    pl = _statements(
        journal("J1", "2026-01-05", line("1010", "SGD", "1000.00"), line("4010", "SGD", "-1000.00")),
        # Amortisation of software used to deliver the service: COGS by function, but in the depreciation group.
        journal("J2", "2026-01-31", line("5010", "SGD", "30.00"), line("1590", "SGD", "-30.00")),
        accounts=chart,
    )["2026-01"]["profit_and_loss"]
    # Gross profit and EBITDA are the 1,000.00 of revenue; the 30.00 is below EBITDA: operating profit 970.00.
    assert (pl["cost_of_sales"]["total"], pl["gross_profit"], pl["ebitda"]) == ("0.00", "1000.00", "1000.00")
    assert (pl["depreciation_and_amortisation"]["total"], pl["operating_profit"]) == ("-30.00", "970.00")


# req: FR-RPT-01
def test_with_no_depreciation_role_the_settings_name_the_depreciation_account():
    # No account has the role; the settings' depreciation key names 6610, in Group D. 6620, an expense in Group D
    # with function Other, is depreciation and amortisation with it, not a finance cost.
    chart = [*CHART, _account("6610", "expense", report_group="Group D"),
             _account("6620", "expense", report_group="Group D", function="Other"),
             _account("1590", "asset", report_group="Equipment", cash_flow_line="Depreciation & amortisation")]
    docs = [journal("J1", "2026-01-31", line("6610", "SGD", "20.00"), line("6620", "SGD", "5.00"),
                    line("1590", "SGD", "-25.00"))]
    pl = _statements(*docs, accounts=chart, settings={"accounts": {"depreciation": "6610"}})["2026-01"]["profit_and_loss"]
    assert pl["depreciation_and_amortisation"] == {"total": "-25.00", "groups": {"Group D": "-25.00"}}  # 20.00 + 5.00
    assert pl["finance_costs_and_other_expenses"]["total"] == "0.00"
    # Without the settings' key there is no depreciation account: 6610 has no function (operating expenses) and
    # 6620 is a finance cost.
    pl = _statements(*docs, accounts=chart)["2026-01"]["profit_and_loss"]
    assert (pl["operating_expenses"]["total"], pl["finance_costs_and_other_expenses"]["total"]) == ("-20.00", "-5.00")


# req: FR-RPT-01, INV-10
def test_an_account_with_no_line_is_named_when_it_moved_in_the_month_even_back_to_nothing():
    chart = [*CHART, _account("1990", "asset", report_group="Suspense")]
    s = _statements(
        journal("J1", "2026-06-01", line("1990", "SGD", "50.00"), line("1010", "SGD", "-50.00")),
        journal("J2", "2026-06-02", line("1010", "SGD", "50.00"), line("1990", "SGD", "-50.00")),
        journal("J3", "2026-07-01", line("6010", "SGD", "5.00"), line("1010", "SGD", "-5.00")),
        accounts=chart,
    )
    assert s["2026-06"]["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": ["1990"]}
    assert s["2026-07"]["checks"]["unplaced"] == []  # no balance and no line in July


# req: FR-RPT-01
def test_a_dividend_through_retained_earnings_is_financing_and_an_equity_movement():
    s = _statements(
        journal("J1", "2026-03-02", line("1010", "SGD", "1000.00"), line("4010", "SGD", "-1000.00")),
        journal("J2", "2026-03-30", line("3100", "SGD", "400.00"), line("1010", "SGD", "-400.00")),
    )["2026-03"]
    assert s["cash_flow"]["financing"] == "-400.00"
    assert s["equity_movement"] == {"opening": "0.00", "profit": "1000.00", "share_capital": "0.00",
                                    "share_capital_non_cash": "0.00", "other": "-400.00", "closing": "600.00"}
    assert s["balance_sheet"]["equity"] == {"total": "600.00", "groups": {"Retained earnings": "600.00"}}


# The founders' presentation (p1-founders-money design D14): Ah Kow (P0001) lent 1,000.00 and was paid 1,500.00, so
# he owes the company 500.00; Mei Lin (P0002) lent 2,000.00, converted 1,200.00 at 1.20 (1,000 shares) and waived
# 300.00, so the company owes her 500.00. The 2500 account nets to nothing.
FOUNDERS_CHART = [
    *CHART,
    _account("1250", "asset", report_group="Advances", cash_flow_line="Other current assets"),
    _account("2500", "liability", role="director-loan", report_group="Director loans", cash_flow_line="Borrowings"),
    _account("3010", "equity", role="share-capital", report_group="Share capital", cash_flow_line="Share capital"),
    _account("3300", "equity", report_group="Capital contribution", cash_flow_line="Dividends & other equity"),
]
FOUNDERS_SETTINGS = {"accounts": {"advances": "1250", "director_loan": "2500"}, "money_accounts": {"1010": "SGD"},
                     "books_start": "2026-01-01"}


def _owed(account: str, amount: str, person: str, origin: str) -> dict:
    return {**line(account, "SGD", amount), "party": person, "origin": origin}


FOUNDERS = (
    journal("J1", "2026-03-02", line("1010", "SGD", "1000.00"), _owed("2500", "-1000.00", "P0001", "lent")),
    journal("J2", "2026-03-05", line("1010", "SGD", "2000.00"), _owed("2500", "-2000.00", "P0002", "lent")),
    journal("J3", "2026-03-10", _owed("2500", "1500.00", "P0001", "paid_out"), line("1010", "SGD", "-1500.00")),
    {"type": "director_conversion", "id": "SI-0001", "version": 1, "date": "2026-03-31", "person": "P0002",
     "account": "2500", "class": "3010", "amount": "1200.00", "price": "1.20"},
    {"type": "director_waiver", "id": "DS-0001", "version": 1, "date": "2026-03-31", "person": "P0002",
     "account": "2500", "credit": "3300", "amount": "300.00"},
    journal("J4", "2026-04-08", line("1010", "SGD", "600.00"), _owed("2500", "-600.00", "P0001", "lent")),
)


# req: FR-EQ-10, FR-EQ-11, FR-RPT-01
def test_a_founders_net_debit_shows_under_the_advances_and_conversions_and_waivers_move_no_money():
    s = _statements(*FOUNDERS, accounts=FOUNDERS_CHART, people=["P0001", "P0002"], settings=FOUNDERS_SETTINGS,
                    as_of="2026-04-30")
    mar, apr = s["2026-03"], s["2026-04"]
    # Ah Kow's 500.00 debit shows under 1250; 2500 keeps Mei Lin's 500.00 credit.
    assert mar["balance_sheet"]["current_assets"] == {"total": "2000.00",
                                                      "groups": {"Advances": "500.00", "Cash and bank": "1500.00"}}
    assert mar["balance_sheet"]["borrowings"] == {"total": "500.00", "groups": {"Director loans": "500.00"}}
    assert mar["balance_sheet"]["equity"] == {"total": "1500.00", "groups": {"Capital contribution": "300.00",
                                                                             "Share capital": "1200.00"}}
    assert mar["balance_sheet"]["total_assets"] == mar["balance_sheet"]["total_liabilities_and_equity"] == "2000.00"
    assert mar["cash_flow"]["financing"] == "1500.00"
    assert mar["cash_flow"]["non_cash"] == [
        {"document": "SI-0001", "kind": "post", "posting_date": "2026-03-31", "section": "financing",
         "label": "Converted to shares"},
        {"document": "DS-0001", "kind": "post", "posting_date": "2026-03-31", "section": "financing", "label": "Waived"},
    ]
    assert mar["equity_movement"] == {"opening": "0.00", "profit": "0.00", "share_capital": "0.00",
                                      "share_capital_non_cash": "1200.00", "other": "300.00", "closing": "1500.00"}
    assert mar["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}
    # Ah Kow lends 600.00 in April: he is owed 100.00, so nothing shows under 1250 any more.
    assert apr["balance_sheet"]["current_assets"] == {"total": "2100.00", "groups": {"Cash and bank": "2100.00"}}
    assert apr["balance_sheet"]["borrowings"] == {"total": "600.00", "groups": {"Director loans": "600.00"}}
    assert apr["checks"]["balances"] is True


# req: FR-EQ-11, FR-RPT-01
def test_without_an_advances_account_a_founders_debit_stays_on_their_account():
    bs = _statements(*FOUNDERS[:3], accounts=FOUNDERS_CHART, people=["P0001", "P0002"])["2026-03"]["balance_sheet"]
    assert bs["current_assets"] == {"total": "1500.00", "groups": {"Cash and bank": "1500.00"}}
    assert bs["borrowings"] == {"total": "1500.00", "groups": {"Director loans": "1500.00"}}


# req: FR-RPT-01, FR-GL-10
def test_the_profit_to_date_stays_in_retained_earnings_across_a_year_end():
    s = _statements(
        journal("J1", "2026-12-15", line("1010", "SGD", "250.00"), line("4010", "SGD", "-250.00")),
        journal("J2", "2027-01-15", line("6010", "SGD", "100.00"), line("1010", "SGD", "-100.00")),
    )
    assert list(s) == ["2026-12", "2027-01"]
    assert s["2027-01"]["profit_and_loss"]["net_profit"] == "-100.00"  # January's alone
    assert s["2027-01"]["balance_sheet"]["equity"] == {"total": "150.00", "groups": {"Retained earnings": "150.00"}}
    assert s["2027-01"]["equity_movement"]["opening"] == "250.00"


# req: FR-RPT-01, ENG-14
def test_the_statements_read_the_stored_base_amounts():
    s = _statements(
        journal("J1", "2026-05-04", line("1010", "USD", "100.00", "1.3333"), line("4010", "SGD", "-133.33")),
        currencies={"SGD": 2, "USD": 2},
    )["2026-05"]
    assert s["profit_and_loss"]["revenue"] == {"total": "133.33", "groups": {"Sales": "133.33"}}
    assert s["cash_flow"]["closing_cash"] == "133.33"  # USD 100.00 at 1.3333, as stored


# req: RND-PERCENT
def test_rnd_percent_rounds_once_half_away_from_zero_with_the_parts_sign():
    assert money.rnd_percent(Decimal("-199.80"), Decimal("1200.00")) == Decimal("-16.7")  # -16.65
    assert money.rnd_percent(Decimal("199.80"), Decimal("1200.00")) == Decimal("16.7")
    assert money.rnd_percent(Decimal("1.00"), Decimal("-3.00")) == Decimal("33.3")  # the size of the whole
    assert money.rnd_percent(Decimal("-0.01"), Decimal("1000000.00")) == Decimal("-0.0")
    assert money.rnd_percent(Decimal("5.00"), Decimal("0.00")) is None


# req: FR-RPT-01
def test_no_revenue_means_no_percentages_and_no_entries_means_no_statements():
    s = _statements(journal("J1", "2026-02-02", line("6010", "SGD", "10.00"), line("1010", "SGD", "-10.00")))
    assert "percent_of_revenue" not in s["2026-02"]["profit_and_loss"]
    rejected = journal("J1", "2026-02-02", line("6010", "SGD", "10.00"), line("1010", "SGD", "-9.00"))
    assert _statements(rejected) == {}


# req: QA-ENG-02
def test_a_scenario_without_statements_has_none_in_its_result():
    result = Ledger(scenario_from_json(_scenario(
        journal("J1", "2026-02-02", line("6010", "SGD", "10.00"), line("1010", "SGD", "-10.00")), statements=False,
    ))).result()
    assert "statements" not in result


@pytest.mark.parametrize(
    ("accounts", "message"),
    [
        (CHART + [_account("3110", "equity", role="retained-earnings")], "both have the role retained-earnings"),
        ([*CHART[:4], _account("3100", "liability", role="retained-earnings"), *CHART[5:]], "needs an account of type equity"),
        (CHART + [_account("6610", "revenue", role="depreciation")], "needs an account of type expense"),
        (CHART + [_account("8010", "expense", role="income-tax-expense"),
                  _account("8020", "expense", role="income-tax-expense")], "both have the role income-tax-expense"),
        ([*CHART[:4], _account("3100", "equity"), *CHART[5:]], "needs an account with the role retained-earnings"),
    ],
)
# req: QA-ENG-02
def test_the_format_refuses_a_chart_the_statements_cannot_read(accounts, message):
    with pytest.raises(ScenarioError, match=message):
        scenario_from_json(_scenario(accounts=accounts))


# req: QA-ENG-02
def test_the_schema_refuses_an_unknown_function_and_statements_false():
    with pytest.raises(ScenarioError, match="function"):
        scenario_from_json(_scenario(accounts=[*CHART, _account("6020", "expense", function="Admin")]))
    data = _scenario()
    data["statements"] = False
    with pytest.raises(ScenarioError, match="statements"):
        scenario_from_json(data)
