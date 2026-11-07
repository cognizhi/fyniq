"""The financial statements of a posted scenario, month by month (QA-ENG-01, QA-ENG-02).

Written from PRD-001 FR-RPT-01 (rev 0.159, the layout from the chart's attributes), FR-COA-02 (the
attributes), FR-GL-10 (retained earnings), INV-10 and INV-11 (the checks) and §10.4's ``RND-PERCENT``,
with the sections of the OpenSpec change ``p1-reports`` (design D2 and D3), never from the engine.
``test/differential/README.md`` ("Statements") states each reading; in short:

**Profit and loss** (each figure is what it adds to profit: revenue positive, costs negative). Every
revenue and expense account is placed by its role, type and function, in this order:

1. the account with the role ``income-tax-expense``: income tax;
2. a revenue account: other income when its function is Other, else revenue (the streams);
3. an expense account in the report group of the depreciation account: depreciation and amortisation,
   before its function is read (EBITDA excludes depreciation and amortisation by definition, rev 0.159
   [accountant to confirm]). The depreciation account is the one with the role ``depreciation`` or, when
   no account has that role, the one the settings' ``depreciation`` key names (one system role in the
   product); with no report group, its group is the account alone;
4. an expense account whose function is COGS: cost of sales;
5. an expense account whose function is Other: finance costs and other expenses;
6. any other expense account: operating expenses (by report group, and by function with no function
   counted as Other).

Gross profit = revenue + cost of sales; EBITDA = gross profit + operating expenses; operating profit =
EBITDA + depreciation and amortisation; profit before tax = operating profit + other income + finance
costs and other expenses; net profit = profit before tax + income tax. Each line has its
``RND-PERCENT`` of revenue, and none when the revenue is zero.

**Balance sheet** at the month's last day. A balance-sheet account (asset, liability or equity) is
placed by its cash-flow line, and only on its own side (rev 0.159 [accountant to confirm]): an asset
with Cash & cash equivalents, Receivables or Other current assets in current assets, and with Capital
expenditure or Depreciation & amortisation in non-current assets; a liability with Payables & accruals,
Payroll liabilities, Tax liabilities, Deferred revenue or (rev 0.207) Share application monies in current
liabilities, and with Borrowings in borrowings; an equity account with Share capital or Dividends & other equity in equity. The account with
the role ``retained-earnings`` is always in equity,
and its report group also holds the profit to date (every earlier year's and this year's: FR-GL-10
folds the years into retained earnings, and FR-RPT-01 shows the year's profit inside them). Assets are
debit positive; liabilities, borrowings and equity credit positive. An account whose cash-flow line is
missing, outside the standard chart's set or of the other side (a liability on Share capital, an equity
account on Cash & cash equivalents) sits in no section and is named by the check ``unplaced``; it breaks
``balances``, which stays total assets = total liabilities + equity. A person whose
lines on the directors' accounts (roles ``director-loan`` and ``deferred-remuneration``) net to a debit
owes the company: their balances on those accounts show on the advances account of the settings instead
(FR-EQ-11, ``p1-founders-money`` design D14), with no posting; a scenario without one shows them as they are.

**Cash flow, indirect** (money in positive), on the same placement. Each entry of the month is
*investing* when it has a line on a capital-expenditure asset, else *financing* when it has a line on an
account placed in borrowings or equity, or on a liability on Share application monies (money received for
shares, IAS 7 para 17(a); rev 0.207), else *operating*: FR-RPT-01 classifies transactions ("a
transaction that moved no money ... is left out of investing and financing"). An entry's cash is the sum
of its lines on the cash accounts (assets on Cash & cash equivalents). Investing and financing are their
entries' cash. Operating is net profit; plus the depreciation and amortisation the operating entries put
on accumulated depreciation (assets on that line); less the profit or loss of the investing and
financing entries (a gain on a disposal, a cost a director paid personally); less each working-capital
line's change from the operating entries (an account placed in current assets other than cash, or in
current liabilities, by its line); less the operating entries' lines on every other account, which sit in
no section (``other_accounts``, p1-reports design D3, rev 0.159 [accountant to confirm]). So operating is
always the operating entries' cash, ``ties`` always holds, and net burn is the true cash.
An investing or financing entry with no line on a cash account moved no money: it is listed apart as
non-cash, a conversion of a founder's balance labelled "Converted to shares", a waiver "Waived" (FR-EQ-10) and
a cash issue or an exercise of options allotted from share application monies "Allotted from share application
monies" (rev 0.207).
The check ``ties`` is opening cash + net change = closing cash.

**Movement in equity**: opening equity (the month before's closing), the month's net profit, the
change in share capital issued for cash (every line on the Share capital line but a conversion's), the
shares issued for non-cash consideration (a conversion's: FR-EQ-10), the change in the other equity
accounts (retained earnings' own lines included), and closing equity, which is the balance sheet's.
"""

from __future__ import annotations

from collections import defaultdict
from collections.abc import Iterable
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Entry
from oracle.scenario import Account, Scenario, Source

CASH = "Cash & cash equivalents"
CAPITAL_EXPENDITURE = "Capital expenditure"
DEPRECIATION = "Depreciation & amortisation"
SHARE_CAPITAL = "Share capital"
# (rev 0.207) Money received for shares not yet allotted (2040, B.7): a current liability whose cash is financing.
SHARE_APPLICATION = "Share application monies"
# The standard chart's cash-flow lines (p1-reports design D2), by the balance-sheet section each places.
SECTIONS = {
    CASH: "current_assets",
    "Receivables": "current_assets",
    "Other current assets": "current_assets",
    CAPITAL_EXPENDITURE: "non_current_assets",
    DEPRECIATION: "non_current_assets",
    "Payables & accruals": "current_liabilities",
    "Payroll liabilities": "current_liabilities",
    "Tax liabilities": "current_liabilities",
    "Deferred revenue": "current_liabilities",
    SHARE_APPLICATION: "current_liabilities",
    "Borrowings": "borrowings",
    SHARE_CAPITAL: "equity",
    "Dividends & other equity": "equity",
}
WORKING_CAPITAL = ("Receivables", "Other current assets", "Payables & accruals", "Payroll liabilities",
                   "Tax liabilities", "Deferred revenue")
ASSET_SECTIONS = ("current_assets", "non_current_assets")
# The type of account each section takes (rev 0.159): a line of the other side places nothing.
_SIDE = {"current_assets": "asset", "non_current_assets": "asset", "current_liabilities": "liability",
         "borrowings": "liability", "equity": "equity"}
CLAIM_SECTIONS = ("current_liabilities", "borrowings", "equity")
PL_LINES = ("revenue", "cost_of_sales", "gross_profit", "operating_expenses", "ebitda",
            "depreciation_and_amortisation", "operating_profit", "other_income", "finance_costs_and_other_expenses",
            "profit_before_tax", "income_tax", "net_profit")
FUNCTIONS = ("R&D", "S&M", "G&A", "Other")
# A director's or shareholder's accounts, whose per-person net debit shows under the advances (D14).
DIRECTOR_ROLES = frozenset({"director-loan", "deferred-remuneration"})
# The non-cash entries that say what they were, by their document's type (FR-EQ-10).
NON_CASH_LABELS = {"director_conversion": "Converted to shares", "director_waiver": "Waived",
                   "share_issue_cash": "Allotted from share application monies",
                   "option_exercise": "Allotted from share application monies",
                   "safe_conversion": "Converted to shares"}
# The shares issued for non-cash consideration: a founder's balance, or a SAFE's money (FR-EQ-10, FR-EQ-18).
NON_CASH_SHARES = frozenset({"director_conversion", "safe_conversion"})


def _month(day: date) -> str:
    return f"{day.year:04d}-{day.month:02d}"


def _months(first: str, last: str) -> list[str]:
    year, month = int(first[:4]), int(first[5:])
    result = []
    while f"{year:04d}-{month:02d}" <= last:
        result.append(f"{year:04d}-{month:02d}")
        year, month = (year + 1, 1) if month == 12 else (year, month + 1)
    return result


def _sum(values: Iterable[Decimal]) -> Decimal:
    return money.total(tuple(values))


def _group(account: Account) -> str:
    return account.report_group or account.code


def is_balance_sheet(account: Account) -> bool:
    return account.type in ("asset", "liability", "equity")


def balance_sheet_section(account: Account) -> str | None:
    """The balance-sheet section of an asset, liability or equity account, or None (unplaced): its cash-flow
    line places it only on its own side (an asset in assets, a liability in liabilities, equity in equity)."""
    if account.role == "retained-earnings":
        return "equity"
    section = SECTIONS.get(account.cash_flow_line or "")
    return section if section is not None and _SIDE[section] == account.type else None


def is_cash(account: Account) -> bool:
    """A cash account: an asset on Cash & cash equivalents."""
    return account.type == "asset" and account.role != "retained-earnings" and account.cash_flow_line == CASH


def profit_and_loss_section(account: Account, depreciation_group: str | None) -> str:
    """The P&L section of a revenue or expense account, by the order in the module's docstring."""
    if account.role == "income-tax-expense":
        return "income_tax"
    if account.type == "revenue":
        return "other_income" if account.function == "Other" else "revenue"
    if depreciation_group is not None and _group(account) == depreciation_group:
        return "depreciation_and_amortisation"  # before COGS: EBITDA excludes it by definition
    if account.function == "COGS":
        return "cost_of_sales"
    if account.function == "Other":
        return "finance_costs_and_other_expenses"
    return "operating_expenses"


class Statements:
    """Every month's statements, from the first posting month to the last."""

    def __init__(self, scenario: Scenario, entries: list[Entry]) -> None:
        self.accounts = scenario.accounts
        self.minor = scenario.currencies[scenario.base_currency]
        self.entries = entries
        settings = scenario.settings
        # The depreciation account: the role, or else the settings' key, one system role in the product (rev 0.159).
        depreciation = [a for a in self.accounts.values() if a.role == "depreciation"]
        named = settings.accounts.get("depreciation") if settings is not None else None
        if not depreciation and named in self.accounts:
            depreciation = [self.accounts[named]]
        self.depreciation_group = _group(depreciation[0]) if depreciation else None
        self.retained = next(a for a in self.accounts.values() if a.role == "retained-earnings")
        self.types = {d.id: d.type for d in scenario.documents if isinstance(d, Source)}
        self.advances = settings.accounts.get("advances") if settings is not None else None

    # ------------------------------------------------------------------ placing
    def _bs(self, code: str) -> str | None:
        account = self.accounts[code]
        return balance_sheet_section(account) if is_balance_sheet(account) else None

    def _is_cash(self, code: str) -> bool:
        return is_cash(self.accounts[code])

    def classify(self, entry: Entry) -> str:
        """``investing``, ``financing`` or ``operating``: the cash flow section of an entry."""
        moved = [self.accounts[ln.account] for ln in entry.lines if is_balance_sheet(self.accounts[ln.account])]
        if any(balance_sheet_section(a) == "non_current_assets" and a.cash_flow_line == CAPITAL_EXPENDITURE
               for a in moved):
            return "investing"
        if any(balance_sheet_section(a) in ("borrowings", "equity") for a in moved):
            return "financing"
        if any(balance_sheet_section(a) == "current_liabilities" and a.cash_flow_line == SHARE_APPLICATION for a in moved):
            return "financing"  # money received for shares (IAS 7 para 17(a); rev 0.207)
        return "operating"

    def cash(self, entry: Entry) -> Decimal:
        return _sum(ln.base_amount for ln in entry.lines if self._is_cash(ln.account))

    # ------------------------------------------------------------------ the statements
    def compute(self) -> dict[str, dict[str, Any]]:
        if not self.entries:
            return {}
        by_month: defaultdict[str, list[Entry]] = defaultdict(list)
        for entry in self.entries:
            by_month[_month(entry.posting_date)].append(entry)
        balance: defaultdict[str, Decimal] = defaultdict(lambda: money.ZERO)
        owed: defaultdict[str, defaultdict[str, Decimal]] = defaultdict(lambda: defaultdict(lambda: money.ZERO))
        profit_to_date = money.ZERO
        opening_equity = money.ZERO
        result = {}
        for month in _months(min(by_month), max(by_month)):
            entries = by_month.get(month, [])
            movement: defaultdict[str, Decimal] = defaultdict(lambda: money.ZERO)
            for entry in entries:
                for ln in entry.lines:
                    movement[ln.account] = money.EXACT.add(movement[ln.account], ln.base_amount)
                    if ln.party is not None and self.accounts[ln.account].role in DIRECTOR_ROLES:
                        owed[ln.party][ln.account] = money.EXACT.add(owed[ln.party][ln.account], ln.base_amount)
            opening_cash = _sum(v for code, v in balance.items() if self._is_cash(code))
            for code, value in movement.items():
                balance[code] = money.EXACT.add(balance[code], value)
            closing_cash = _sum(v for code, v in balance.items() if self._is_cash(code))

            pl, figures = self._profit_and_loss(movement)
            profit_to_date = money.EXACT.add(profit_to_date, figures["net_profit"])
            bs, totals = self._balance_sheet(self._advances(balance, owed), profit_to_date)
            cf, net_change = self._cash_flow(entries, figures["net_profit"], opening_cash, closing_cash)
            equity, equity_ok = self._equity(entries, opening_equity, figures["net_profit"], totals["equity"])
            touched = {ln.account for entry in entries for ln in entry.lines}
            unplaced = sorted(
                code for code, account in self.accounts.items()
                if is_balance_sheet(account) and balance_sheet_section(account) is None
                and (not balance[code].is_zero() or code in touched)
            )
            result[month] = {
                "profit_and_loss": pl,
                "balance_sheet": bs,
                "cash_flow": cf,
                "equity_movement": equity,
                "checks": {
                    "balances": totals["assets"] == totals["claims"],
                    "ties": money.EXACT.add(opening_cash, net_change) == closing_cash,
                    "equity": equity_ok,
                    "unplaced": unplaced,
                },
            }
            opening_equity = totals["equity"]
        return result

    def _advances(self, balance: dict[str, Decimal], owed: dict[str, dict[str, Decimal]]) -> dict[str, Decimal]:
        """The balances the balance sheet shows: each person's net debit on the directors' accounts moved from
        those accounts to the advances account, with no posting (D14)."""
        if self.advances is None:
            return balance
        shown = dict(balance)
        for accounts in owed.values():
            net = _sum(accounts.values())
            if net <= 0:
                continue
            for code, value in accounts.items():
                shown[code] = money.EXACT.subtract(shown.get(code, money.ZERO), value)
            shown[self.advances] = money.EXACT.add(shown.get(self.advances, money.ZERO), net)
        return shown

    def _fmt(self, value: Decimal) -> str:
        return money.fmt(value, self.minor)

    def _section(self, figures: dict[str, Decimal]) -> tuple[dict[str, Any], Decimal]:
        """A section from each account's figure: its total and its non-zero report groups."""
        groups: defaultdict[str, Decimal] = defaultdict(lambda: money.ZERO)
        for code, value in figures.items():
            groups[_group(self.accounts[code])] = money.EXACT.add(groups[_group(self.accounts[code])], value)
        total = _sum(figures.values())
        shown = {g: self._fmt(v) for g, v in sorted(groups.items()) if not v.is_zero()}
        return {"total": self._fmt(total), "groups": shown}, total

    def _profit_and_loss(self, movement: dict[str, Decimal]) -> tuple[dict[str, Any], dict[str, Decimal]]:
        placed: dict[str, dict[str, Decimal]] = {name: {} for name in PL_LINES}
        functions: defaultdict[str, Decimal] = defaultdict(lambda: money.ZERO)
        for code, value in movement.items():
            account = self.accounts[code]
            if is_balance_sheet(account):
                continue
            section = profit_and_loss_section(account, self.depreciation_group)
            figure = money.negate(value)  # what it adds to profit
            placed[section][code] = figure
            if section == "operating_expenses":
                function = account.function if account.function in FUNCTIONS else "Other"
                functions[function] = money.EXACT.add(functions[function], figure)
        pl: dict[str, Any] = {}
        f: dict[str, Decimal] = {}
        for name in ("revenue", "cost_of_sales", "operating_expenses", "depreciation_and_amortisation",
                     "other_income", "finance_costs_and_other_expenses"):
            pl[name], f[name] = self._section(placed[name])
        f["income_tax"] = _sum(placed["income_tax"].values())
        f["gross_profit"] = money.EXACT.add(f["revenue"], f["cost_of_sales"])
        f["ebitda"] = money.EXACT.add(f["gross_profit"], f["operating_expenses"])
        f["operating_profit"] = money.EXACT.add(f["ebitda"], f["depreciation_and_amortisation"])
        f["profit_before_tax"] = _sum((f["operating_profit"], f["other_income"], f["finance_costs_and_other_expenses"]))
        f["net_profit"] = money.EXACT.add(f["profit_before_tax"], f["income_tax"])
        pl["operating_expenses"]["functions"] = {
            name: self._fmt(functions[name]) for name in FUNCTIONS if not functions[name].is_zero()
        }
        for name in ("gross_profit", "ebitda", "operating_profit", "profit_before_tax", "income_tax", "net_profit"):
            pl[name] = self._fmt(f[name])
        if not f["revenue"].is_zero():
            pl["percent_of_revenue"] = {
                name: money.fmt(money.rnd_percent(f[name], f["revenue"]), 1) for name in PL_LINES
            }
        ordered = {name: pl[name] for name in PL_LINES}
        if "percent_of_revenue" in pl:
            ordered["percent_of_revenue"] = pl["percent_of_revenue"]
        return ordered, f

    def _balance_sheet(self, balance: dict[str, Decimal], profit_to_date: Decimal) -> tuple[dict[str, Any], dict[str, Decimal]]:
        placed: dict[str, dict[str, Decimal]] = {name: {} for name in (*ASSET_SECTIONS, *CLAIM_SECTIONS)}
        for code, value in balance.items():
            section = self._bs(code)
            if section is None:
                continue
            placed[section][code] = value if section in ASSET_SECTIONS else money.negate(value)
        # The profit to date sits in retained earnings: added to its account's figure, so its group shows it.
        retained = self.retained.code
        placed["equity"][retained] = money.EXACT.add(placed["equity"].get(retained, money.ZERO), profit_to_date)
        bs: dict[str, Any] = {}
        t: dict[str, Decimal] = {}
        for name in placed:
            bs[name], t[name] = self._section(placed[name])
        t["assets"] = _sum(t[name] for name in ASSET_SECTIONS)
        t["liabilities"] = money.EXACT.add(t["current_liabilities"], t["borrowings"])
        t["claims"] = money.EXACT.add(t["liabilities"], t["equity"])
        ordered = {
            "current_assets": bs["current_assets"],
            "non_current_assets": bs["non_current_assets"],
            "total_assets": self._fmt(t["assets"]),
            "current_liabilities": bs["current_liabilities"],
            "borrowings": bs["borrowings"],
            "total_liabilities": self._fmt(t["liabilities"]),
            "equity": bs["equity"],
            "total_liabilities_and_equity": self._fmt(t["claims"]),
        }
        return ordered, t

    def _cash_flow(self, entries: list[Entry], net_profit: Decimal, opening: Decimal,
                   closing: Decimal) -> tuple[dict[str, Any], Decimal]:
        depreciation = results = investing = financing = other = money.ZERO
        working: defaultdict[str, Decimal] = defaultdict(lambda: money.ZERO)
        non_cash = []
        for entry in entries:
            section = self.classify(entry)
            if section != "operating":
                for ln in entry.lines:
                    if not is_balance_sheet(self.accounts[ln.account]):
                        results = money.EXACT.add(results, ln.base_amount)  # takes its profit or loss back out
                if not any(self._is_cash(ln.account) for ln in entry.lines):
                    item = {"document": entry.document, "kind": entry.kind,
                            "posting_date": entry.posting_date.isoformat(), "section": section}
                    label = NON_CASH_LABELS.get(self.types.get(entry.document, ""))
                    if label is not None:
                        item["label"] = label
                    non_cash.append(item)
                elif section == "investing":
                    investing = money.EXACT.add(investing, self.cash(entry))
                else:
                    financing = money.EXACT.add(financing, self.cash(entry))
                continue
            for ln in entry.lines:
                account = self.accounts[ln.account]
                if not is_balance_sheet(account) or is_cash(account):
                    continue
                section = balance_sheet_section(account)
                if section == "non_current_assets" and account.cash_flow_line == DEPRECIATION:
                    depreciation = money.EXACT.subtract(depreciation, ln.base_amount)
                elif section in ("current_assets", "current_liabilities"):
                    working[account.cash_flow_line] = money.EXACT.subtract(working[account.cash_flow_line],
                                                                           ln.base_amount)
                else:  # in no section (an operating entry moves no capital expenditure, borrowings or equity)
                    other = money.EXACT.subtract(other, ln.base_amount)
        operating = _sum((net_profit, depreciation, results, *working.values(), other))
        net_change = _sum((operating, investing, financing))
        cf = {
            "operating": {
                "net_profit": self._fmt(net_profit),
                "depreciation_and_amortisation": self._fmt(depreciation),
                "investing_and_financing_results": self._fmt(results),
                "working_capital": {
                    line: self._fmt(working[line]) for line in WORKING_CAPITAL if not working[line].is_zero()
                },
                "other_accounts": self._fmt(other),
                "total": self._fmt(operating),
            },
            "investing": self._fmt(investing),
            "financing": self._fmt(financing),
            "net_change": self._fmt(net_change),
            "opening_cash": self._fmt(opening),
            "closing_cash": self._fmt(closing),
            "non_cash": non_cash,
        }
        return cf, net_change

    def _equity(self, entries: list[Entry], opening: Decimal, profit: Decimal,
                closing: Decimal) -> tuple[dict[str, str], bool]:
        share_capital = non_cash = other = money.ZERO
        for entry in entries:
            converted = self.types.get(entry.document) in NON_CASH_SHARES
            for ln in entry.lines:
                if self._bs(ln.account) != "equity":
                    continue
                account = self.accounts[ln.account]
                if account.role == "retained-earnings" or account.cash_flow_line != SHARE_CAPITAL:
                    other = money.EXACT.subtract(other, ln.base_amount)
                elif converted:
                    non_cash = money.EXACT.subtract(non_cash, ln.base_amount)
                else:
                    share_capital = money.EXACT.subtract(share_capital, ln.base_amount)
        moved = _sum((opening, profit, share_capital, non_cash, other))
        return {
            "opening": self._fmt(opening),
            "profit": self._fmt(profit),
            "share_capital": self._fmt(share_capital),
            "share_capital_non_cash": self._fmt(non_cash),
            "other": self._fmt(other),
            "closing": self._fmt(closing),
        }, moved == closing


def statements(scenario: Scenario, entries: list[Entry]) -> dict[str, dict[str, Any]]:
    """The ``statements`` of a result: month -> that month's statements (README, "Statements")."""
    return Statements(scenario, entries).compute()
