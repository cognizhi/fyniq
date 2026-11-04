"""Generated scenarios that ask for the statements (FR-RPT-01; README, "Statements").

Any generated scenario (manual journals, or the P1 sources) becomes one: every account gets the chart's
attributes, drawn so that report groups are shared and most balance-sheet accounts sit on a cash-flow
line their type would have; one in forty has none (or one outside the set, or one of the other side,
which places nothing either: rev 0.159), so `balances` fails too and the cash flow keeps the movement in
operating's other accounts. A
retained-earnings account joins the chart, and an expense account may take the depreciation or the
income-tax role. As in the product, whose chart holds one role per account, a statement role never goes
to an account the settings name under another key (the settings' ``depreciation`` may take its own role). Random journals seldom move both a cash account and a capital-expenditure or
borrowings account, so up to four journals in the base currency are added that do: bought for cash, a
loan received, a sale at a gain or loss, equipment on account and a cost a director paid (the last two
moved no money). With ``kpis``, the scenario also asks for the KPIs, with a financial year starting in
any month and counts entered for some months (FR-KPI-02 rev 0.161). ``make differential`` draws these
once ``statements`` (or ``kpis``) is in FYNIQ_DIFFERENTIAL_SOURCES.

Statements read back each account's balance for a month, which the database keeps as NUMERIC(20,4), so these
scenarios keep every month's balance, per currency, in amount and in base, far below 10^16 (README, "Balances
within NUMERIC(20,4)"). The base scenario is drawn with ``strategies.BOUNDED`` set, so its journals stay small, and
a final guard (:func:`reach`) bounds the sum of every line the scenario could post, as absolute values: a journal's
lines in amount and in amount × rate, once more per reversal; a P1 document's figures (with quantity × unit price
and distance × mileage rate), with those of every document it names (the bill a payment settles, the asset a
disposal disposes of), at the largest rate among them, times eight for the lines one figure can land on (spreads,
depreciation, disposal, settlement). A month's balance, and every running sum on the way, is at most that sum,
which must stay below 10^15. P1 scenarios stay below it as drawn (about 10^14 at most), so the guard seldom fires.
An edit's lines count twice (its replacement, and the reversal of it by a later edit, a void or the scheduler), and
a journal that reverses by itself once more.

The statements of a scenario with opening balances are not in the format yet (README, "P1 readings"), so the general
ledger's opening balances, and the edits naming them, are dropped from a scenario drawn here.
"""

from __future__ import annotations

import re
from collections import Counter
from decimal import Decimal
from typing import Any

from hypothesis import assume
from hypothesis import strategies as st

from oracle import money
from oracle.strategies import BOUNDED
from oracle.statements import CAPITAL_EXPENDITURE, CASH, DEPRECIATION, SHARE_CAPITAL

RETAINED = "9100"  # generated codes are 1000 to 8999, and the P1 chart has none from 9000
_GROUPS = ["Group A", "Group B", "Group C", "Group D"]
_LINES = {
    "asset": [CASH, CASH, "Receivables", "Other current assets", CAPITAL_EXPENDITURE, DEPRECIATION],
    "liability": ["Payables & accruals", "Payroll liabilities", "Tax liabilities", "Deferred revenue", "Borrowings"],
    "equity": [SHARE_CAPITAL, "Dividends & other equity"],
}
_FUNCTIONS = [None, None, "COGS", "R&D", "S&M", "G&A", "Other"]
# The settings' keys an account may be named under and still take a statement role: its own system role's.
_ROLE_KEYS = {"retained-earnings": frozenset(), "depreciation": frozenset({"depreciation"}),
              "income-tax-expense": frozenset()}
# What every line a scenario could post may add up to, as absolute values: a tenth of NUMERIC(20,4)'s 10^16.
LIMIT = Decimal(10) ** 15
_POSTINGS = 8  # the posted lines one figure of a P1 document can land on, at most
_PLAIN = re.compile(r"-?[0-9]+(\.[0-9]+)?")
_NOT_FIGURES = frozenset({"account", "fx_rate", "percent", "business_share", "discount", "version"})


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
            result["cash_flow_line"] = draw(st.sampled_from(_LINES[account["type"]]))
        else:  # unplaced: no line, one outside the set, or one of the other side (rev 0.159)
            other = sorted(kind for kind in _LINES if kind != account["type"])
            result["cash_flow_line"] = draw(st.sampled_from([None, "Other", *(ln for k in other for ln in _LINES[k])]))
            if result["cash_flow_line"] is None:
                del result["cash_flow_line"]
    else:
        function = draw(st.sampled_from(_FUNCTIONS))
        if function is not None:
            result["function"] = function
    return result


@st.composite
def with_statements(draw: st.DrawFn, base: st.SearchStrategy[dict[str, Any]], kpis: bool = False) -> dict[str, Any]:
    """A scenario from ``base`` that asks for the statements, and for the KPIs when ``kpis``."""
    token = BOUNDED.set(True)
    try:
        scenario = draw(base)
    finally:
        BOUNDED.reset(token)
    openings = {d["id"] for d in scenario["documents"] if d.get("type") == "opening_balances"}
    if openings:  # not in the format with the statements yet
        scenario = {**scenario, "documents": [d for d in scenario["documents"] if d["id"] not in openings],
                    "reversals": [r for r in scenario["reversals"] if r.get("document") not in openings]}
    accounts = [draw(_attributes(account)) for account in scenario["accounts"]]
    retained = {"code": RETAINED, "type": "equity", "group": False, "active": True, "role": "retained-earnings"}
    if draw(st.booleans()):
        retained["report_group"] = draw(st.sampled_from(_GROUPS))
    accounts.append(retained)
    named: dict[str, set[str]] = {}  # account -> the settings' keys that name it
    for key, code in ((scenario.get("settings") or {}).get("accounts") or {}).items():
        named.setdefault(code, set()).add(key)
    expenses = [a for a in accounts if a["type"] == "expense" and not a["group"] and "role" not in a]
    for role in ("depreciation", "income-tax-expense"):
        allowed = [a for a in expenses if named.get(a["code"], set()) <= _ROLE_KEYS[role]]
        if allowed and draw(st.booleans()):
            chosen = draw(st.sampled_from(allowed))
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
    assume(all(total < LIMIT for total in reach(result)))  # a final guard: drawn bounded, it seldom fires
    return result


def _plain(value: Any) -> Decimal | None:
    """The absolute value of a plain decimal string (the format's amounts and rates), else None."""
    return Decimal(value).copy_abs() if isinstance(value, str) and _PLAIN.fullmatch(value) else None


def _strings(node: Any):
    if isinstance(node, str):
        yield node
    elif isinstance(node, list):
        for item in node:
            yield from _strings(item)
    elif isinstance(node, dict):
        for value in node.values():
            yield from _strings(value)


def _size(node: Any, mileage: Decimal) -> tuple[Decimal, Decimal]:
    """A P1 document's figures summed as absolute values, and the largest rate it names (at least 1).

    Every plain decimal counts except account codes, rates and percentages; a quantity also counts times
    its unit price, and a distance times the largest mileage rate. A rate that is not a plain decimal is
    refused by both sides (ENG-11), so it scales nothing.
    """
    total, rate = Decimal(0), Decimal(1)
    if isinstance(node, list):
        for item in node:
            t, r = _size(item, mileage)
            total, rate = total + t, max(rate, r)
    elif isinstance(node, dict):
        for key, value in node.items():
            figure = _plain(value)
            if key == "fx_rate":
                rate = max(rate, figure or Decimal(1))
            elif figure is not None and key not in _NOT_FIGURES:
                total += figure
            elif isinstance(value, (dict, list)):
                t, r = _size(value, mileage)
                total, rate = total + t, max(rate, r)
        quantity, price = _plain(node.get("quantity")), _plain(node.get("unit_price"))
        if quantity is not None and price is not None:
            total += quantity * price
        distance = _plain(node.get("distance"))
        if distance is not None:
            total += distance * mileage
    return total, rate


def reach(scenario: dict[str, Any]) -> tuple[Decimal, Decimal]:
    """Upper bounds on the sum of every line ``scenario`` could post, as absolute values: in amount (all
    currencies together) and in base. Each month's balance of each account, per currency, is at most these."""
    documents = {d["id"]: d for d in scenario["documents"] if isinstance(d.get("id"), str)}
    corrections = scenario.get("reversals") or []
    reversals = Counter(r.get("document") for r in corrections)  # an edit reverses the entry before it, too
    edits = [r["edit"] for r in corrections if isinstance(r.get("edit"), dict)]
    rates = [_plain(m.get("rate")) for m in (scenario.get("settings") or {}).get("mileage_rates") or []]
    mileage = max([Decimal(1), *(r for r in rates if r is not None)])
    amount = base = Decimal(0)
    for doc in [*scenario["documents"], *({**e, "type": "edit"} for e in edits)]:
        times = 1 + reversals[doc.get("id")] + ("reverse_on" in doc)
        if doc.get("type") == "edit":
            times = 2 + ("reverse_on" in doc)
        if doc.get("type") in ("manual_journal", "edit"):
            for line in doc.get("lines") or []:
                figure = _plain(line.get("amount")) or Decimal(0)
                amount += times * figure
                base += times * figure * max(Decimal(1), _plain(line.get("fx_rate")) or Decimal(1))
            continue
        named, todo = {}, [doc]  # the document and every document it names, at any depth
        while todo:
            current = todo.pop()
            if id(current) not in named:
                named[id(current)] = current
                todo.extend(documents[s] for s in _strings(current) if s in documents)
        total, rate = Decimal(0), Decimal(1)
        for current in named.values():
            t, r = _size(current, mileage)
            total, rate = total + t, max(rate, r)
        amount += times * _POSTINGS * total
        base += times * _POSTINGS * total * rate
    return amount, base


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
    cash = _postable(accounts, lambda a: a["type"] == "asset" and a.get("cash_flow_line") == CASH)
    capex = _postable(accounts, lambda a: a["type"] == "asset" and a.get("cash_flow_line") == CAPITAL_EXPENDITURE)
    loans = _postable(accounts, lambda a: a["type"] == "liability" and a.get("cash_flow_line") == "Borrowings")
    payables = _postable(accounts,
                         lambda a: a["type"] == "liability" and a.get("cash_flow_line") == "Payables & accruals")
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
