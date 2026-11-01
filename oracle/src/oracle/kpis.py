"""The KPIs of every stated month (FR-KPI-02, FR-KPI-03, RND-KPI; QA-ENG-01, QA-ENG-02).

Written from the workbook's own words (its PRD §11.5, ``zh-sheet-accounting``, the definitions FR-KPI-02
adopts), FR-KPI-02 and FR-KPI-03 with their rev 0.161 decision, and §10.4's ``RND-KPI`` and
``RND-PERCENT``, never from the engine. ``test/differential/README.md`` ("KPIs") states each reading.

Every KPI is read from the result's statements and trial balance and the entered counts, as exact
decimals; each is rounded once at the end, half away from zero (``RND-KPI``): amounts to the base
currency's minor units; percentages and margins to 1 decimal as ``RND-PERCENT`` (against the size of the
whole, with the part's sign); ratios, multiples, months and days to 1 decimal; headcount whole. An
undefined KPI is ``None`` (null), never 0, and runway is ``"not-burning"`` when the three-month average
net burn is not above zero.

Three-month averages take this month and up to two before it, inside the same financial year and on or
after the first stated month (the workbook: "the months of the selected financial year that exist").
The month before the first stated month had nothing posted, so its revenue and ARR are 0.
"""

from __future__ import annotations

from decimal import Decimal
from fractions import Fraction
from typing import Any

from oracle import money
from oracle.scenario import Scenario
from oracle.statements import is_balance_sheet

NOT_BURNING = "not-burning"
FUNCTIONS = ("R&D", "S&M", "G&A")


def _d(text: str) -> Decimal:
    return Decimal(text)


def _round(value: Fraction, decimals: int) -> str:
    return money.fmt(money.round_quotient(Decimal(value.numerator), Decimal(value.denominator), decimals), decimals)


def _percent(part: Fraction, whole: Fraction) -> str | None:
    """``RND-PERCENT``: part ÷ the size of the whole × 100, to 1 decimal; none when the whole is zero."""
    if whole == 0:
        return None
    return _round(part / abs(whole) * 100, 1)


def _ratio(top: Fraction | None, bottom: Fraction | None) -> Fraction | None:
    if top is None or bottom is None or bottom == 0:
        return None
    return top / bottom


def _year_start(month: str, start_month: int) -> str:
    year, number = int(month[:4]), int(month[5:])
    return f"{year if number >= start_month else year - 1:04d}-{start_month:02d}"


class Kpis:
    def __init__(self, scenario: Scenario, result: dict[str, Any]) -> None:
        assert scenario.kpis is not None
        self.settings = scenario.kpis
        self.accounts = scenario.accounts
        self.minor = scenario.currencies[scenario.base_currency]
        self.statements: dict[str, dict[str, Any]] = result["statements"]
        self.trial_balance: dict[str, dict[str, str]] = result["trial_balance"]
        # Booked MRR: the revenue (not other income) of accounts whose report group names a subscription stream.
        self.subscription = [
            a.code for a in self.accounts.values()
            if a.type == "revenue" and a.function != "Other" and "subscription" in (a.report_group or "").lower()
        ]

    def _amount(self, value: Fraction | None) -> str | None:
        return None if value is None else _round(value, self.minor)

    def _line(self, balance: dict[str, Fraction], line: str) -> Fraction:
        """The balance of the accounts on a cash-flow line, debit positive."""
        return sum((v for code, v in balance.items()
                    if is_balance_sheet(self.accounts[code]) and self.accounts[code].role != "retained-earnings"
                    and self.accounts[code].cash_flow_line == line), Fraction(0))

    def compute(self) -> dict[str, dict[str, Any]]:
        months = list(self.statements)
        balance: dict[str, Fraction] = {}
        seen: dict[str, dict[str, Fraction]] = {}  # each earlier month's revenue, costs and net burn
        previous = {"revenue": Fraction(0), "mrr": Fraction(0) if self.subscription else None}
        result = {}
        for month in months:
            s = self.statements[month]
            pl, bs, cf = s["profit_and_loss"], s["balance_sheet"], s["cash_flow"]
            movement = {code: Fraction(_d(v)) for code, v in self.trial_balance.get(month, {}).items()}
            for code, value in movement.items():
                balance[code] = balance.get(code, Fraction(0)) + value

            revenue = Fraction(_d(pl["revenue"]["total"]))
            gross = Fraction(_d(pl["gross_profit"]))
            costs = -Fraction(_d(pl["cost_of_sales"]["total"])) - Fraction(_d(pl["operating_expenses"]["total"]))
            mrr = -sum((movement.get(code, Fraction(0)) for code in self.subscription), Fraction(0)) \
                if self.subscription else None
            spend = {f: sum((v for code, v in movement.items()
                             if self.accounts[code].type == "expense" and self.accounts[code].function == f),
                            Fraction(0)) for f in FUNCTIONS}
            burn = -(Fraction(_d(cf["operating"]["total"])) + Fraction(_d(cf["investing"])))
            seen[month] = {"revenue": revenue, "costs": costs, "burn": burn}
            start = _year_start(month, self.settings.year_start_month)
            window = [m for m in months if start <= m <= month][-3:]
            n = len(window)

            cash = Fraction(_d(cf["closing_cash"]))
            burn_total = sum((seen[m]["burn"] for m in window), Fraction(0))
            runway = NOT_BURNING if burn_total <= 0 else _round(cash * n / burn_total, 1)

            counts = self.settings.counts.get(month, {})
            customers = "paying_customers" in counts
            paying = counts.get("paying_customers", 0)
            new = counts.get("new_customers", 0)
            lost = counts.get("customers_lost", 0)
            opening_customers = paying - new + lost
            margin = gross / abs(revenue) if revenue != 0 else None  # the gross margin, exact
            arpa = _ratio(mrr, Fraction(paying)) if customers else None
            churn = Fraction(lost, opening_customers) if customers and opening_customers > 0 else None
            cac = _ratio(spend["S&M"], Fraction(new)) if customers else None
            ltv = None if arpa is None or margin is None else _ratio(arpa * margin, churn)
            payback = None if arpa is None or margin is None else _ratio(cac, arpa * margin)
            arr = None if mrr is None else mrr * 12
            net_new_arr = None if arr is None or previous["mrr"] is None else arr - previous["mrr"] * 12
            multiple = _ratio(burn, net_new_arr) if burn > 0 and net_new_arr is not None and net_new_arr > 0 else None

            current_assets = Fraction(_d(bs["current_assets"]["total"]))
            current_liabilities = Fraction(_d(bs["current_liabilities"]["total"]))
            equity = Fraction(_d(bs["equity"]["total"]))
            receivables = self._line(balance, "Receivables")
            payables = -self._line(balance, "Payables & accruals")
            revenue_total = sum((seen[m]["revenue"] for m in window), Fraction(0))
            costs_total = sum((seen[m]["costs"] for m in window), Fraction(0))
            headcount = counts.get("headcount")

            result[month] = {
                "booked_mrr": self._amount(mrr),
                "arr": self._amount(arr),
                "contracted_mrr": None,
                "revenue_growth": _percent(revenue - previous["revenue"], previous["revenue"]),
                "gross_margin": _percent(gross, revenue),
                "spend_by_function": {f: _round(spend[f], self.minor) for f in FUNCTIONS},
                "net_burn": _round(burn, self.minor),
                "runway": runway,
                "arpa": self._amount(arpa),
                "churn": None if churn is None else _percent(Fraction(lost), Fraction(opening_customers)),
                "cac": self._amount(cac),
                "ltv": self._amount(ltv),
                "ltv_to_cac": self._one(_ratio(ltv, cac)),
                "cac_payback": self._one(payback),
                "burn_multiple": self._one(multiple),
                "current_ratio": self._one(_ratio(current_assets, current_liabilities)),
                "quick_ratio": self._one(_ratio(cash + receivables, current_liabilities)),
                "working_capital": _round(current_assets - current_liabilities, self.minor),
                "debt_to_equity": self._one(_ratio(Fraction(_d(bs["total_liabilities"])), equity)) if equity > 0 else None,
                "dso": self._one(_ratio(receivables * 30 * n, revenue_total)),
                "dpo": self._one(_ratio(payables * 30 * n, costs_total)),
                "ebitda_margin": _percent(Fraction(_d(pl["ebitda"])), revenue),
                "net_margin": _percent(Fraction(_d(pl["net_profit"])), revenue),
                "headcount": None if headcount is None else str(headcount),
                "revenue_per_employee": self._amount(_ratio(revenue * 12, Fraction(headcount))) if headcount else None,
            }
            previous = {"revenue": revenue, "mrr": mrr}
        return result

    @staticmethod
    def _one(value: Fraction | None) -> str | None:
        return None if value is None else _round(value, 1)


def kpis(scenario: Scenario, result: dict[str, Any]) -> dict[str, dict[str, Any]]:
    """The ``kpis`` of a result: month -> that month's KPIs (README, "KPIs")."""
    return Kpis(scenario, result).compute()
