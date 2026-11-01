"""Generated scenarios that ask for the statements (FR-RPT-01; README, "Statements").

Any generated scenario (manual journals, or the P1 sources) becomes one: every account gets the chart's
attributes, drawn so that report groups are shared and most balance-sheet accounts sit on a cash-flow
line their type would have; one in forty has none (or one outside the set), so the checks fail too. A
retained-earnings account joins the chart, and an expense account may take the depreciation or the
income-tax role. Random journals seldom move both a cash account and a capital-expenditure or
borrowings account, so up to four journals in the base currency are added that do: bought for cash, a
loan received, a sale at a gain or loss, equipment on account and a cost a director paid (the last two
moved no money). With ``kpis``, the scenario also asks for the KPIs, with a financial year starting in
any month and counts entered for some months (FR-KPI-02 rev 0.161). ``make differential`` draws these
once ``statements`` (or ``kpis``) is in FYNIQ_DIFFERENTIAL_SOURCES.
"""

from __future__ import annotations

from decimal import Decimal
from typing import Any

from hypothesis import strategies as st

from oracle import money
from oracle.statements import CAPITAL_EXPENDITURE, CASH, DEPRECIATION, SHARE_CAPITAL

RETAINED = "9100"  # generated codes are 1000 to 8999, and the P1 chart has none from 9000
_GROUPS = ["Group A", "Group B", "Group C", "Group D"]
_LINES = {
    "asset": [CASH, CASH, "Receivables", "Other current assets", CAPITAL_EXPENDITURE, DEPRECIATION],
    "liability": ["Payables & accruals", "Payroll liabilities", "Tax liabilities", "Deferred revenue", "Borrowings"],
    "equity": [SHARE_CAPITAL, "Dividends & other equity"],
}
_FUNCTIONS = [None, None, "COGS", "R&D", "S&M", "G&A", "Other"]


@st.composite
def _attributes(draw: st.DrawFn, account: dict[str, Any]) -> dict[str, Any]:
    result = dict(account)
    group = draw(st.sampled_from([*_GROUPS, None]))
    if group is not None:
        result["report_group"] = group
    if account["type"] in _LINES:
        if account.get("role") == "share-capital":
            result["cash_flow_line"] = SHARE_CAPITAL
        elif draw(st.integers(0, 39)) > 0:
            # A line of another type now and then: the line places the account, whatever its type.
            kind = account["type"] if draw(st.integers(0, 9)) > 0 else draw(st.sampled_from(sorted(_LINES)))
            result["cash_flow_line"] = draw(st.sampled_from(_LINES[kind]))
        elif draw(st.booleans()):
            result["cash_flow_line"] = "Other"  # outside the set: unplaced, like none
    else:
        function = draw(st.sampled_from(_FUNCTIONS))
        if function is not None:
            result["function"] = function
    return result


@st.composite
def with_statements(draw: st.DrawFn, base: st.SearchStrategy[dict[str, Any]], kpis: bool = False) -> dict[str, Any]:
    """A scenario from ``base`` that asks for the statements, and for the KPIs when ``kpis``."""
    scenario = draw(base)
    accounts = [draw(_attributes(account)) for account in scenario["accounts"]]
    retained = {"code": RETAINED, "type": "equity", "group": False, "active": True, "role": "retained-earnings"}
    if draw(st.booleans()):
        retained["report_group"] = draw(st.sampled_from(_GROUPS))
    accounts.append(retained)
    expenses = [a for a in accounts if a["type"] == "expense" and not a["group"] and "role" not in a]
    for role in ("depreciation", "income-tax-expense"):
        if expenses and draw(st.booleans()):
            chosen = draw(st.sampled_from(expenses))
            chosen["role"] = role
            expenses.remove(chosen)
    if accounts and draw(st.booleans()):  # a subscription stream for booked MRR
        streams = [a for a in accounts if a["type"] == "revenue"]
        if streams:
            draw(st.sampled_from(streams))["report_group"] = draw(st.sampled_from(["Subscriptions", "SaaS subscription fees"]))
    journals = draw(_journals(scenario, accounts))
    result = {**scenario, "accounts": accounts, "documents": [*scenario["documents"], *journals], "statements": True}
    if kpis:
        result["kpis"] = draw(_kpi_settings([d["date"][:7] for d in result["documents"]]))
    return result


@st.composite
def _kpi_settings(draw: st.DrawFn, months: list[str]) -> dict[str, Any]:
    counts: dict[str, dict[str, int]] = {}
    for month in sorted(set(months)):
        entry: dict[str, int] = {}
        if draw(st.integers(0, 3)) > 0:
            paying = draw(st.integers(0, 200))
            new = draw(st.integers(0, paying))
            entry |= {"paying_customers": paying, "new_customers": new,
                      "customers_lost": draw(st.integers(0, 20))}
        if draw(st.booleans()):
            entry["headcount"] = draw(st.integers(0, 40))
        if entry:
            counts[month] = entry
    return {"year_start_month": draw(st.sampled_from([1, 1, 4, 7, 10, 12])), "counts": counts}


def _postable(accounts: list[dict[str, Any]], test) -> list[str]:
    return [a["code"] for a in accounts
            if a["active"] and not a["group"] and a.get("role") != "share-capital" and test(a)]


@st.composite
def _journals(draw: st.DrawFn, scenario: dict[str, Any], accounts: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Up to four balanced base-currency journals moving cash, capital expenditure and borrowings."""
    days = sorted({d["date"] for d in scenario["documents"]})
    if not days:
        return []
    cash = _postable(accounts, lambda a: a.get("cash_flow_line") == CASH)
    capex = _postable(accounts, lambda a: a.get("cash_flow_line") == CAPITAL_EXPENDITURE)
    loans = _postable(accounts, lambda a: a.get("cash_flow_line") == "Borrowings")
    payables = _postable(accounts, lambda a: a.get("cash_flow_line") == "Payables & accruals")
    revenue = _postable(accounts, lambda a: a["type"] == "revenue")
    expense = _postable(accounts, lambda a: a["type"] == "expense")
    base, minor = scenario["base_currency"], scenario["currencies"][scenario["base_currency"]]
    shapes = {
        "bought for cash": [capex, cash],
        "loan received": [cash, loans],
        "sold at a gain or loss": [cash, capex, revenue],
        "equipment on account": [capex, payables],
        "cost a director paid": [expense, loans],
    }
    possible = [name for name, needs in shapes.items() if all(needs)]
    journals = []
    for i in range(draw(st.integers(0, 4)) if possible else 0):
        shape = draw(st.sampled_from(possible))
        first, second, *rest = (draw(st.sampled_from(codes)) for codes in shapes[shape])
        if first == second:
            continue
        amount = money.EXACT.scaleb(Decimal(draw(st.integers(1, 10**7))), -minor)
        text = money.fmt(amount, minor)
        lines = [{"account": first, "currency": base, "amount": text},
                 {"account": second, "currency": base, "amount": money.fmt(money.negate(amount), minor)}]
        if rest:  # the sale: the cash in is the asset's cost plus a gain, or less a loss
            difference = money.EXACT.scaleb(Decimal(draw(st.integers(-10**5, 10**5).filter(bool))), -minor)
            lines[0]["amount"] = money.fmt(money.EXACT.add(amount, difference), minor)
            lines.append({"account": rest[0], "currency": base, "amount": money.fmt(money.negate(difference), minor)})
            if money.EXACT.add(amount, difference) <= 0:
                continue
        journals.append({"type": "manual_journal", "id": f"STM-{i + 1:04d}", "version": 1,
                         "date": draw(st.sampled_from(days)), "memo": shape, "lines": lines})
    return journals
