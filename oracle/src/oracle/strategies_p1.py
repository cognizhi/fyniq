"""Hypothesis strategies for scenarios with P1 sources (QA-ENG-02), one group of kinds per source.

Not part of the gate's profile yet: ``cmd/scenario-run`` does not read the P1 shapes, so
``make differential`` draws from these only for the sources named in ``FYNIQ_DIFFERENTIAL_SOURCES``
(test/differential/README.md, "P1 sources"). The oracle's own properties use them now.

As for manual journals, each document is built to an *intent* (``post`` or ``reject``), never to an
expected figure. Only shapes whose rule PRD-001 settles are generated; the readings the PRD leaves
open (README, "P1 readings") are covered by the pending golden cases instead. For bank lines that
means: lines in the base currency (the currency of a foreign line's parts is a reading), and fees
only on a transfer paid from a base-currency account. For purchases: parts recorded in full only on
base-currency bills (the 3% of a foreign bill's tolerance is a reading), and no payment of a bill that
withholds tax (its open amount is what it owes after withholding, which the generator does not track).

For assets: proceeds received name a supply code whenever the organisation is registered on the
disposal's date, and may name none otherwise (FR-FA-03, B.6 rev 0.169), and no books' start (the opening balances' share of 1590 is not in a scenario). For sales: receipts, write-offs and deposits deducted only against invoices that charge no GST (so
the generator knows what each owes without computing GST), and receipts in full only on base-currency
invoices.

For the founders' money (README, "Costs before the books start" to "Conversions of founders' balances to
shares"): the people ``P0001`` and ``P0002``, the director's accounts 2500 and 2120 with their roles and
settings keys, share capital (3010, 3020), 3300 and 6050 join the chart, and the books start on the first
day of a month on or before the scenario's first date, so every other document is dated in the books and
no asset depreciates before them (the opening balances' share of 1590 stays out). A waiver, repayment or
conversion built to post, or refused for another rule, draws only on what the company is known to owe that person on
that account; one built to draw on more (README, "P1 readings") asks for a cent or more above the most it can owe
there, which is exact until a conversion leaves a remainder the generator does not work out, or for any amount where
nothing can be owed; a repayment's may split it between two settlements on one account (each within what is owed when
it is a cent above), and may carry a settlement within what is owed on the other account. A GST claim on a cost only while registered and only of GST the receipt shows (the
README leaves a claim otherwise open); a fair value always names its ``difference``, an income account for a
gain and an expense account for a loss; and no loan claims (``expense_claim@2``).

The generator keeps what it built so far in a :class:`_Book` (the bills and invoices a settlement may
settle and what each has open, the deposits and what is left of each), so a document built to post
really can.
"""

from __future__ import annotations

from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from hypothesis import strategies as st

from oracle import money
from oracle.scenario import SCENARIO_SCHEMA
from oracle.strategies import POST, REJECT, Intent

SOURCES = ("bank", "purchases", "sales", "claims", "assets", "founders")

CURRENCIES = {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3}
MONEY_ACCOUNTS = {"1010": "SGD", "1011": "SGD", "1021": "USD", "1022": "JPY", "1023": "KWD"}
SYSTEM = {
    "receivable": "1100",
    "payable": "2010",
    "gst": "2200",
    "prepayments": "1210",
    "deferred_revenue": "2400",
    "fx": "7030",
    "bank_charges": "6460",
    "withholding_tax": "2330",
    "customer_deposits": "2410",
    "claims_payable": "2150",
    "advances": "1250",
    "depreciation": "6610",
    "accumulated_depreciation": "1590",
    "disposal": "7050",
    "disposal_clearing": "1160",
}
EXPENSES = ["6010", "6020", "6110", "6460", "7030"]
REVENUES = ["4010", "4020", "4030"]
OTHER = ["1500", "2050", "2999"]  # an asset, an accrual, suspense: P&L-free parts
_DATES = st.dates(date(2025, 6, 1), date(2027, 3, 31))
# Appendix B.6's directions: a supply document takes the supply codes, a purchase document the purchase
# codes (FR-AP-02, FR-AR-09, FR-FA-03); a bank line's part takes any, NA included (Appendix B.1).
_SUPPLY_CODES = ["SR", "SR", "ZR", "ES", "OS"]
_PURCHASE_CODES = ["TX", "TX", "IM", "ZP", "BL", "NR", "EP", "OP"]
_CODES_IN = [*_SUPPLY_CODES, "NA"]
_CODES_OUT = [*_PURCHASE_CODES, "NA"]
_NOT_PURCHASE = ["NA", "SR", "ZR", "ES", "OS"]  # refused on a bill's, a vendor credit's or a receipt's line
_NOT_SUPPLY = ["NA", "TX", "IM", "ZP", "BL", "NR", "EP", "OP"]  # refused on an invoice's line, a deposit, proceeds

BANK_POSTING = ["bank-line", "bank-line", "bank-split", "bank-spread", "transfer-same", "transfer-cross"]
BANK_REJECTED = ["bank-parts-mismatch", "bank-part-on-money", "bank-part-on-control", "bank-spread-not-p-and-l",
                 "transfer-unbalanced", "transfer-missing-rate"]
PURCHASES_POSTING = ["bill", "bill", "bill-foreign", "bill-spread", "bill-quantity", "bill-withholding",
                     "vendor-credit", "pay", "pay", "pay-in-full", "pay-from-base", "pay-unapplied"]
PURCHASES_REJECTED = ["bill-spread-not-expense", "pay-more-than-open", "pay-beyond-tolerance", "pay-unknown-bill",
                      "bill-wrong-code"]
SALES_POSTING = ["invoice", "invoice", "invoice-foreign", "invoice-discount", "invoice-spread", "credit-note",
                 "receipt", "receipt", "receipt-in-full", "receipt-from-base", "deposit", "deposit-security",
                 "invoice-deducting", "deposit-refund", "write-off"]
SALES_REJECTED = ["receipt-more-than-open", "invoice-spread-not-revenue", "invoice-deducting-too-much",
                  "write-off-more-than-open", "invoice-wrong-code", "deposit-wrong-code"]
CLAIMS_POSTING = ["claim", "claim", "claim-trips", "claim-foreign", "claim-advance", "reimburse", "reimburse",
                  "advance", "advance-refund"]
CLAIMS_REJECTED = ["reimburse-more-than-open", "claim-trip-foreign", "advance-refund-too-much", "claim-wrong-code",
                   "reimburse-in-another-currency"]
ASSETS_POSTING = ["asset", "asset", "asset-residual", "asset-later-start", "dispose-money", "dispose-clearing",
                  "dispose-write-off"]
ASSETS_REJECTED = ["dispose-unknown", "dispose-twice", "asset-on-expense", "dispose-wrong-code",
                   "dispose-registered-no-code"]
FOUNDERS_POSTING = ["cost", "cost", "cost-claim-gst", "fee", "fee", "waiver", "repay", "repay", "convert",
                    "convert-fair-value"]
# "not-a-person" and "not-directors-account" draw the document type they refuse.
# The "-more-than-owed" kinds draw on more than the company owes the person there (README, "P1 readings").
OVER_OWED = ["waiver-more-than-owed", "repay-more-than-owed", "convert-more-than-owed"]
FOUNDERS_REJECTED = ["not-a-person", "not-directors-account", "cost-account-not-allowed", "cost-incurred-after-start",
                     "cost-before-books-start", "fee-account-not-allowed", "waiver-credit-not-allowed", "repay-foreign",
                     "repay-not-money", "convert-not-share-capital", "convert-no-whole-share", *OVER_OWED]
KINDS = {"bank": BANK_POSTING * 3 + BANK_REJECTED, "purchases": PURCHASES_POSTING * 2 + PURCHASES_REJECTED,
         "sales": SALES_POSTING * 2 + SALES_REJECTED, "claims": CLAIMS_POSTING * 2 + CLAIMS_REJECTED,
         "assets": ASSETS_POSTING * 2 + ASSETS_REJECTED, "founders": FOUNDERS_POSTING * 2 + FOUNDERS_REJECTED}
REJECTED_KINDS = frozenset(BANK_REJECTED + PURCHASES_REJECTED + SALES_REJECTED + CLAIMS_REJECTED + ASSETS_REJECTED
                           + FOUNDERS_REJECTED)
MILEAGE = {"car": Decimal("0.60"), "motorcycle": Decimal("0.25")}
WALLETS = {"SGD": "1010", "USD": "1021", "JPY": "1022", "KWD": "1023"}
# The founders' money (README, "Settings and as_of" and the founders' sections): the directors and shareholders, a
# person who is none of them, the director's accounts by settings key and role, and the accounts their documents
# reach (share capital, 3300 Capital contribution, 6050 Directors' fees).
PEOPLE = ["P0001", "P0002"]
STRANGER = "P0009"
DIRECTOR_ACCOUNTS = {"director_loan": ("2500", "director-loan"),
                     "deferred_remuneration": ("2120", "deferred-remuneration")}
SHARE_CLASSES = ["3010", "3020"]


class _Book:
    """What the generator built so far: what a settlement may settle and what each has open."""

    def __init__(self, gst: dict[str, Any] | None = None) -> None:
        self.gst = gst or {}  # the scenario's GST settings, drawn first, so a document can depend on them
        self.open: dict[str, tuple[str, Decimal]] = {}  # bill -> (currency, open amount)
        self.invoices: dict[str, tuple[str, Decimal]] = {}  # invoice charging no GST -> (currency, open)
        self.deposits: dict[str, tuple[str, Decimal]] = {}  # deposit -> (currency, what is left)
        self.claims: dict[str, Decimal] = {}  # claim -> what is owed, in SGD (FR-AP-17: owed in the base currency)
        self.advances: dict[str, Decimal] = {}  # advance -> what is left
        self.assets: dict[str, bool] = {}  # registered asset -> disposed
        self.books_start: date | None = None
        # (person, director's account) -> at least what the company still owes them there, in SGD
        self.owed: dict[tuple[str, str], Decimal] = {}
        # (person, director's account) -> at most what it owes them there: exact but for a conversion's remainder
        self.most: dict[tuple[str, str], Decimal] = {}

    def credit(self, key: tuple[str, str], amount: Decimal) -> None:
        """A posted credit to the person on the account: owed, at least and at most."""
        self.owed[key] = money.EXACT.add(self.owed.get(key, Decimal(0)), amount)
        self.most[key] = money.EXACT.add(self.most.get(key, Decimal(0)), amount)

    def debit(self, key: tuple[str, str], amount: Decimal, *, exact: bool = True) -> None:
        """A posted debit (``exact`` False: at most ``amount``, a conversion's), drawn within what is owed."""
        self.owed[key] = money.EXACT.subtract(self.owed[key], amount)
        if exact:
            self.most[key] = money.EXACT.subtract(self.most[key], amount)

    def registered(self, day: date) -> bool:
        start, end = self.gst.get("registered_from"), self.gst.get("registered_to")
        return start is not None and start <= day.isoformat() and (end is None or day.isoformat() <= end)


def _accounts() -> list[dict[str, Any]]:
    def account(code: str, kind: str) -> dict[str, Any]:
        return {"code": code, "type": kind, "group": False, "active": True}

    chart = [account(code, "asset") for code in [*MONEY_ACCOUNTS, "1100", "1160", "1210", "1250", "1500", "1510", "1590"]]
    chart += [account(code, "liability") for code in ["2010", "2050", "2150", "2200", "2330", "2400", "2410", "2999"]]
    chart += [account(code, "revenue") for code in REVENUES]
    chart += [account(code, "expense") for code in [*EXPENSES, "6610", "7050"]]
    return chart


def _founders_accounts() -> list[dict[str, Any]]:
    """The accounts the founders' documents reach (README, "Settings and as_of"; FR-EQ-01)."""
    chart = [{"code": code, "type": "liability", "group": False, "active": True, "role": role}
             for code, role in DIRECTOR_ACCOUNTS.values()]
    chart += [{"code": code, "type": "equity", "group": False, "active": True, "role": "share-capital"}
              for code in SHARE_CLASSES]
    chart.append({"code": "3300", "type": "equity", "group": False, "active": True})
    chart.append({"code": "6050", "type": "expense", "group": False, "active": True})
    return chart


@st.composite
def _settings(draw: st.DrawFn) -> dict[str, Any]:
    gst: dict[str, Any] = {"rates": [{"from": "2023-01-01", "percent": "8"}, {"from": "2024-01-01", "percent": "9"}]}
    registration = draw(st.sampled_from(["always", "always", "from", "window", "never"]))
    if registration == "always":
        gst["registered_from"] = "2020-01-01"
    elif registration == "from":
        gst["registered_from"] = draw(_DATES).isoformat()
    elif registration == "window":
        start = draw(_DATES)
        gst["registered_from"] = start.isoformat()
        gst["registered_to"] = (start + timedelta(days=draw(st.integers(0, 300)))).isoformat()
    if draw(st.booleans()):
        gst["per_line"] = draw(st.booleans())
    mileage = [{"vehicle": vehicle, "from": "2020-01-01", "rate": format(rate, "f")} for vehicle, rate in MILEAGE.items()]
    return {"accounts": dict(SYSTEM), "money_accounts": dict(MONEY_ACCOUNTS), "gst": gst, "mileage_rates": mileage}


@st.composite
def _cents(draw: st.DrawFn, minor: int = 2, digits: int = 7) -> Decimal:
    """A positive amount, log-uniform up to ``digits`` integer digits."""
    size = draw(st.integers(1, digits + minor))
    return Decimal(draw(st.integers(10 ** (size - 1), 10**size - 1))).scaleb(-minor, context=money.EXACT)


def _text(value: Decimal, minor: int) -> str:
    return money.fmt(value, minor)


@st.composite
def _split(draw: st.DrawFn, total: Decimal, parts: int, minor: int) -> list[Decimal]:
    """``total`` (positive) in ``parts`` positive pieces at the minor unit."""
    units = int(total.scaleb(minor, context=money.EXACT))
    parts = min(parts, units)
    cuts = sorted(draw(st.lists(st.integers(1, units - 1), min_size=parts - 1, max_size=parts - 1, unique=True))) \
        if parts > 1 else []
    edges = [0, *cuts, units]
    return [Decimal(b - a).scaleb(-minor, context=money.EXACT) for a, b in zip(edges, edges[1:], strict=False)]


@st.composite
def _rate(draw: st.DrawFn) -> str:
    """A rate from 0.01 to 100 with up to 6 decimals: one unit of any currency is at least a cent."""
    decimals = draw(st.integers(0, 6))
    least = max(1, 10 ** (decimals - 2))
    return format(Decimal(draw(st.integers(least, 10 ** (decimals + 2)))).scaleb(-decimals, context=money.EXACT), "f")


@st.composite
def _bank_document(draw: st.DrawFn, doc_id: str, day: date, kind: str) -> dict[str, Any]:
    if kind.startswith("transfer"):
        return draw(_transfer(doc_id, day, kind))
    sign = draw(st.sampled_from([1, -1]))
    account = draw(st.sampled_from(["1010", "1011"]))
    gross = draw(_cents())
    pieces = draw(_split(gross, draw(st.integers(2, 4)) if kind == "bank-split" else 1, 2))
    if kind == "bank-parts-mismatch":
        pieces[0] = money.EXACT.add(pieces[0], Decimal("0.01"))
    category = EXPENSES if sign < 0 else REVENUES
    codes = _CODES_OUT if sign < 0 else _CODES_IN
    parts = []
    for i, piece in enumerate(pieces):
        target = draw(st.sampled_from(category + OTHER if kind in ("bank-line", "bank-split") else category))
        if kind == "bank-part-on-money" and i == 0:
            target = "1011" if account == "1010" else "1010"
        if kind == "bank-part-on-control" and i == 0:
            target = draw(st.sampled_from(["1100", "2010"]))
        part = {"account": target, "amount": _text(sign * piece, 2), "gst_code": draw(st.sampled_from(codes))}
        if kind == "bank-spread" or (kind == "bank-spread-not-p-and-l" and i == 0) or \
                (kind == "bank-split" and draw(st.integers(0, 3)) == 0 and target in category):
            part["spread_months"] = draw(st.sampled_from([1, 2, 3, 6, 12, 24, 120]))
        if kind == "bank-spread-not-p-and-l" and i == 0:
            part["account"] = draw(st.sampled_from(OTHER + (REVENUES if sign < 0 else EXPENSES)))
        parts.append(part)
    return {"type": "bank_transaction", "id": doc_id, "version": 1, "date": day.isoformat(), "account": account,
            "currency": "SGD", "amount": _text(sign * gross, 2), "parts": parts}


@st.composite
def _transfer(draw: st.DrawFn, doc_id: str, day: date, kind: str) -> dict[str, Any]:
    def leg(account: str, amount: Decimal, rate: str | None) -> dict[str, Any]:
        currency = MONEY_ACCOUNTS[account]
        result = {"account": account, "currency": currency, "amount": _text(amount, CURRENCIES[currency])}
        if rate is not None:
            result["fx_rate"], result["fx_rate_source"] = rate, "document"
        return result

    if kind in ("transfer-same", "transfer-unbalanced"):
        paying, receiving = draw(st.permutations(["1010", "1011"]))
        amount = draw(_cents())
        received = money.EXACT.subtract(amount, Decimal("0.01")) if kind == "transfer-unbalanced" else amount
        if not received > 0:
            received = money.EXACT.add(amount, Decimal("0.01"))
        return {"type": "bank_transfer", "id": doc_id, "version": 1, "date": day.isoformat(),
                "from": leg(paying, amount, None), "to": leg(receiving, received, None)}
    foreign = draw(st.sampled_from(["1021", "1022", "1023"]))
    base_side = draw(st.sampled_from(["1010", "1011"]))
    pays_foreign = draw(st.booleans())
    paying, receiving = (foreign, base_side) if pays_foreign else (base_side, foreign)
    paid = draw(_cents(CURRENCIES[MONEY_ACCOUNTS[paying]]))
    received = draw(_cents(CURRENCIES[MONEY_ACCOUNTS[receiving]]))
    rate = None if kind == "transfer-missing-rate" else draw(_rate())
    doc = {"type": "bank_transfer", "id": doc_id, "version": 1, "date": day.isoformat(),
           "from": leg(paying, paid, rate if pays_foreign else None),
           "to": leg(receiving, received, None if pays_foreign else rate)}
    if not pays_foreign and draw(st.booleans()) and paid > Decimal("0.01"):
        doc["fee"] = _text(draw(_split(paid, 2, 2))[0], 2)  # a fee below what was paid
    return doc


@st.composite
def _purchase_document(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    """A bill, vendor credit or payment, and its intent."""
    if kind.startswith("pay"):
        return draw(_payment(doc_id, day, kind, book))
    currency = draw(st.sampled_from(["USD", "JPY", "KWD"])) if kind == "bill-foreign" else "SGD"
    minor = CURRENCIES[currency]
    lines, total = [], Decimal(0)
    for i in range(draw(st.integers(1, 4))):
        line: dict[str, Any] = {"account": draw(st.sampled_from(EXPENSES + ["1500"])),
                                "gst_code": draw(st.sampled_from(_PURCHASE_CODES))}
        if kind == "bill-quantity":
            quantity = Decimal(draw(st.integers(1, 10**4))).scaleb(-draw(st.integers(0, 3)), context=money.EXACT)
            price = Decimal(draw(st.integers(1, 10**8))).scaleb(-draw(st.integers(0, 6)), context=money.EXACT)
            net = money.rnd_line(quantity, price, minor)
            if net.is_zero():
                quantity, net = Decimal(100), money.rnd_line(Decimal(100), price, minor)
            if net.is_zero():
                price = Decimal(1)
                net = money.rnd_line(quantity, price, minor)
            line["quantity"], line["unit_price"] = format(quantity, "f"), format(price, "f")
        else:
            net = draw(_cents(minor, 6))
            line["amount"] = _text(net, minor)
        gst = Decimal(0)
        if line["gst_code"] in ("TX", "IM") and draw(st.integers(0, 3)) > 0:
            gst = money.round_quotient(money.EXACT.multiply(net, Decimal(9)), Decimal(100), minor)
            line["gst"] = _text(gst, minor)
            if currency != "SGD" and draw(st.booleans()):
                line["gst_base"] = _text(draw(_cents(2, 5)), 2)
        if draw(st.integers(0, 4)) == 0:
            line["business_share"] = draw(st.sampled_from(["0", "50", "60", "33.33", "100"]))
        if kind == "bill-spread" and i == 0:
            line["account"] = draw(st.sampled_from(EXPENSES))
            line["spread_months"] = draw(st.sampled_from([1, 3, 6, 12, 24, 120]))
            if draw(st.booleans()):
                line["spread_from"] = (day - timedelta(days=draw(st.integers(-90, 200)))).isoformat()
        if kind == "bill-spread-not-expense" and i == 0:
            line["account"], line["spread_months"] = draw(st.sampled_from(["1500", "2050"])), 12
        if kind == "bill-wrong-code" and i == 0:  # FR-AP-02: "GST input tax codes" only
            line["gst_code"] = draw(st.sampled_from(_NOT_PURCHASE))
            line.pop("gst", None)
            line.pop("gst_base", None)
        lines.append(line)
        total = money.EXACT.add(total, money.EXACT.add(net, gst))
    credit = kind == "vendor-credit" or kind == "bill-wrong-code" and draw(st.booleans())
    doc: dict[str, Any] = {"type": "vendor_credit" if credit else "bill", "id": doc_id, "version": 1,
                           "date": day.isoformat(), "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    if kind == "bill-withholding":
        doc["withholding"] = {"percent": draw(st.sampled_from(["10", "15", "17", "24"]))}
    elif kind in ("bill", "bill-foreign", "bill-spread", "bill-quantity"):
        book.open[doc_id] = (currency, total)
    return doc, REJECT if kind in REJECTED_KINDS else POST


@st.composite
def _payment(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    least = Decimal("2.02") if kind == "pay-beyond-tolerance" else Decimal(0)
    payable = [b for b, (cur, left) in sorted(book.open.items()) if left > least and
               (cur == "SGD" or kind not in ("pay-in-full", "pay-beyond-tolerance"))]
    doc: dict[str, Any] = {"type": "bill_payment", "id": doc_id, "version": 1, "date": day.isoformat(), "parts": []}
    if kind == "pay-unknown-bill" or not payable:
        doc.update(account="1010", currency="SGD", parts=[{"document": "BILL-NONE", "amount": "1.00"}])
        return doc, REJECT
    target = draw(st.sampled_from(payable))
    currency, left = book.open[target]
    minor = CURRENCIES[currency]
    unit = Decimal(1).scaleb(-minor, context=money.EXACT)
    if kind == "pay-more-than-open":
        doc.update(account="1010" if currency == "SGD" else {"USD": "1021", "JPY": "1022", "KWD": "1023"}[currency],
                   currency=currency, parts=[{"document": target, "amount": _text(money.EXACT.add(left, unit), minor)}])
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        return doc, REJECT
    if kind in ("pay-in-full", "pay-beyond-tolerance"):
        units = int(left.scaleb(2, context=money.EXACT))
        cents = draw(st.integers(0, min(100, units - 1))) if kind == "pay-in-full" else \
            draw(st.integers(101, min(5000, units - 1)))
        amount = money.EXACT.subtract(left, Decimal(cents).scaleb(-2, context=money.EXACT))
        doc.update(account="1010", currency="SGD", parts=[{"document": target, "amount": _text(amount, 2), "in_full": True}])
        if kind == "pay-in-full":
            book.open[target] = (currency, Decimal(0))
            return doc, POST
        return doc, REJECT
    amount = left if draw(st.booleans()) else draw(_split(left, 2, minor))[0] if left > unit else left
    doc["currency"] = currency
    doc["parts"] = [{"document": target, "amount": _text(amount, minor)}]
    if currency == "SGD":
        doc["account"] = draw(st.sampled_from(["1010", "1011"]))
    elif kind == "pay-from-base":
        doc["account"], doc["base_amount"] = "1010", _text(draw(_cents()), 2)
    else:
        doc["account"] = {"USD": "1021", "JPY": "1022", "KWD": "1023"}[currency]
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    if kind == "pay-unapplied":
        doc["unapplied"] = _text(draw(_cents(minor, 3)), minor)
    book.open[target] = (currency, money.EXACT.subtract(left, amount))
    return doc, POST


@st.composite
def _sales_document(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    if kind in ("deposit", "deposit-security", "deposit-wrong-code"):
        currency = draw(st.sampled_from(["SGD", "SGD", "USD"]))
        amount = draw(_cents(CURRENCIES[currency], 6))
        if currency != "SGD" and amount < 1:  # a whole unit at least, so its base is at least a cent
            amount = money.EXACT.add(amount, Decimal(1))
        doc = {"type": "customer_deposit", **base, "account": WALLETS[currency] if currency != "SGD" else
               draw(st.sampled_from(["1010", "1011"])), "currency": currency,
               "amount": _text(amount, CURRENCIES[currency]),
               "kind": "security" if kind == "deposit-security" else "part_payment"}
        if kind == "deposit":
            doc["gst_code"] = draw(st.sampled_from(_SUPPLY_CODES))
        if kind == "deposit-wrong-code":  # FR-AR-09: output GST, so a supply code
            doc["gst_code"] = draw(st.sampled_from(_NOT_SUPPLY))
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        if kind == "deposit-wrong-code":
            return doc, REJECT
        book.deposits[doc_id] = (currency, amount)
        return doc, POST
    if kind == "deposit-refund":
        left = [d for d, (_, amount) in sorted(book.deposits.items()) if amount > 0]
        if not left:
            return {"type": "deposit_refund", **base, "deposit": "DP-NONE", "account": "1010", "amount": "1.00"}, REJECT
        target = draw(st.sampled_from(left))
        currency, amount = book.deposits[target]
        minor = CURRENCIES[currency]
        whole = currency != "SGD" or draw(st.booleans()) or amount <= Decimal(1).scaleb(-minor)
        refund = amount if whole else draw(_split(amount, 2, minor))[0]
        doc = {"type": "deposit_refund", **base, "deposit": target, "account": WALLETS[currency],
               "amount": _text(refund, minor)}
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        book.deposits[target] = (currency, money.EXACT.subtract(amount, refund))
        return doc, POST
    if kind.startswith("receipt"):
        return draw(_receipt(base, kind, book))
    if kind.startswith("write-off"):
        open_ = [i for i, (_, left) in sorted(book.invoices.items()) if left > 0]
        if not open_:
            return {"type": "write_off", **base, "invoice": "INV-NONE", "amount": "1.00", "account": "6010"}, REJECT
        target = draw(st.sampled_from(open_))
        currency, left = book.invoices[target]
        minor = CURRENCIES[currency]
        unit = Decimal(1).scaleb(-minor, context=money.EXACT)
        if kind == "write-off-more-than-open":
            amount = money.EXACT.add(left, unit)
        else:
            amount = left if draw(st.booleans()) or left <= unit else draw(_split(left, 2, minor))[0]
            book.invoices[target] = (currency, money.EXACT.subtract(left, amount))
        doc = {"type": "write_off", **base, "invoice": target, "amount": _text(amount, minor),
               "account": draw(st.sampled_from(["6010", "7030", "4010"]))}
        return doc, REJECT if kind in REJECTED_KINDS else POST

    # An invoice or a credit note.
    currency = draw(st.sampled_from(["USD", "JPY", "KWD"])) if kind == "invoice-foreign" else "SGD"
    deducting = kind in ("invoice-deducting", "invoice-deducting-too-much")
    unknown_deposit = False
    if deducting:
        usable = [d for d, (_, left) in sorted(book.deposits.items()) if left > 0]
        if not usable:  # nothing to deduct: a plain invoice, or one naming a deposit that does not exist
            deducting = False
            unknown_deposit = kind == "invoice-deducting-too-much"
            kind = "invoice" if kind == "invoice-deducting" else kind
        else:
            source = draw(st.sampled_from(usable))
            currency, left = book.deposits[source]
    minor = CURRENCIES[currency]
    tracked = deducting or kind in ("invoice", "invoice-foreign", "invoice-discount") and draw(st.booleans())
    codes = ["ZR", "ES", "OS"] if tracked else _SUPPLY_CODES
    lines, total = [], Decimal(0)
    for i in range(draw(st.integers(1, 3))):
        line: dict[str, Any] = {"account": draw(st.sampled_from(REVENUES)), "gst_code": draw(st.sampled_from(codes))}
        if kind == "invoice-wrong-code" and i == 0:  # a bill's mirror (FR-AP-02): supply codes only
            line["gst_code"] = draw(st.sampled_from(_NOT_SUPPLY))
        if kind == "invoice-discount":
            quantity = Decimal(draw(st.integers(1, 999)))
            price = Decimal(draw(st.integers(1, 10**6))).scaleb(-draw(st.integers(0, 4)), context=money.EXACT)
            discount = Decimal(draw(st.integers(0, 9999))).scaleb(-2, context=money.EXACT)
            net = money.rnd_line(quantity, price, minor, discount)
            if net.is_zero():
                discount, net = Decimal(0), money.rnd_line(quantity, Decimal(1), minor)
                price = Decimal(1)
            line.update(quantity=format(quantity, "f"), unit_price=format(price, "f"), discount=format(discount, "f"))
        else:
            net = draw(_cents(minor, 6))
            line["amount"] = _text(net, minor)
        if (kind == "invoice-spread" and i == 0) or (kind == "invoice-spread-not-revenue" and i == 0):
            line["spread_months"] = draw(st.sampled_from([1, 3, 12, 24]))
            if draw(st.booleans()):
                line["spread_from"] = (day + timedelta(days=draw(st.integers(-60, 120)))).isoformat()
            if kind == "invoice-spread-not-revenue":
                line["account"] = draw(st.sampled_from(EXPENSES + OTHER))
        lines.append(line)
        total = money.EXACT.add(total, net)
    credit = kind == "credit-note" or kind == "invoice-wrong-code" and draw(st.booleans())
    doc = {"type": "credit_note" if credit else "invoice", **base, "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    meant = REJECT if kind in REJECTED_KINDS else POST
    if unknown_deposit:
        doc["deposits"] = [{"deposit": "DP-NONE", "amount": "1.00"}]
    if deducting:
        if kind == "invoice-deducting-too-much":
            used = money.EXACT.add(left, Decimal(1).scaleb(-minor, context=money.EXACT))
        else:
            used = min(left, total) if draw(st.booleans()) else draw(_split(min(left, total), 2, minor))[0] \
                if min(left, total) > Decimal(1).scaleb(-minor) else min(left, total)
            book.deposits[source] = (currency, money.EXACT.subtract(left, used))
            total = money.EXACT.subtract(total, used)
        doc["deposits"] = [{"deposit": source, "amount": _text(used, minor)}]
    if tracked and meant == POST:
        book.invoices[doc_id] = (currency, total)
    return doc, meant


@st.composite
def _receipt(draw: st.DrawFn, base: dict[str, Any], kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    in_full = kind == "receipt-in-full"
    open_ = [i for i, (cur, left) in sorted(book.invoices.items()) if left > 0 and (cur == "SGD" or not in_full)]
    doc: dict[str, Any] = {"type": "customer_receipt", **base, "parts": []}
    if not open_:
        doc.update(account="1010", currency="SGD", parts=[{"document": "INV-NONE", "amount": "1.00"}])
        return doc, REJECT
    target = draw(st.sampled_from(open_))
    currency, left = book.invoices[target]
    minor = CURRENCIES[currency]
    unit = Decimal(1).scaleb(-minor, context=money.EXACT)
    doc["currency"] = currency
    if kind == "receipt-more-than-open":
        doc["parts"] = [{"document": target, "amount": _text(money.EXACT.add(left, unit), minor)}]
        doc["account"] = WALLETS[currency]
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        return doc, REJECT
    if in_full:
        units = int(left.scaleb(2, context=money.EXACT))
        amount = money.EXACT.subtract(left, Decimal(draw(st.integers(0, min(100, units - 1)))).scaleb(-2))
        doc.update(account="1010", parts=[{"document": target, "amount": _text(amount, 2), "in_full": True}])
        book.invoices[target] = (currency, Decimal(0))
        return doc, POST
    amount = left if draw(st.booleans()) or left <= unit else draw(_split(left, 2, minor))[0]
    doc["parts"] = [{"document": target, "amount": _text(amount, minor)}]
    if currency == "SGD":
        doc["account"] = draw(st.sampled_from(["1010", "1011"]))
    elif kind == "receipt-from-base":
        doc["account"], doc["base_amount"] = "1010", _text(draw(_cents()), 2)
    else:
        doc["account"] = WALLETS[currency]
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    if draw(st.integers(0, 4)) == 0:
        doc["unapplied"] = _text(draw(_cents(minor, 3)), minor)
    book.invoices[target] = (currency, money.EXACT.subtract(left, amount))
    return doc, POST


@st.composite
def _claims_document(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    if kind == "advance":
        amount = draw(_cents(2, 4))
        book.advances[doc_id] = amount
        return {"type": "cash_advance", **base, "account": draw(st.sampled_from(["1010", "1011"])),
                "amount": _text(amount, 2)}, POST
    if kind in ("advance-refund", "advance-refund-too-much"):
        left = [a for a, amount in sorted(book.advances.items()) if amount > 0]
        if not left:
            return {"type": "cash_advance", **base, "account": "1010", "amount": "1.00", "refund_of": "ADV-NONE"}, REJECT
        target = draw(st.sampled_from(left))
        amount = book.advances[target]
        if kind == "advance-refund-too-much":
            return {"type": "cash_advance", **base, "account": "1010", "amount": _text(amount + Decimal("0.01"), 2),
                    "refund_of": target}, REJECT
        refund = amount if draw(st.booleans()) or amount <= Decimal("0.01") else draw(_split(amount, 2, 2))[0]
        book.advances[target] = money.EXACT.subtract(amount, refund)
        return {"type": "cash_advance", **base, "account": "1010", "amount": _text(refund, 2), "refund_of": target}, POST
    if kind.startswith("reimburse"):
        open_ = [c for c, owed in sorted(book.claims.items()) if owed > 0]
        doc: dict[str, Any] = {"type": "claim_reimbursement", **base, "parts": []}
        if not open_:
            doc.update(account="1010", currency="SGD", parts=[{"document": "EC-NONE", "amount": "1.00"}])
            return doc, REJECT
        target = draw(st.sampled_from(open_))
        owed = book.claims[target]
        unit = Decimal("0.01")
        if kind == "reimburse-in-another-currency":  # FR-AP-17 (decided): in SGD only, with no rate
            doc.update(account="1010", currency=draw(st.sampled_from(["USD", "JPY"])), fx_rate=draw(_rate()),
                       fx_rate_source="document", parts=[{"document": target, "amount": "1"}])
            return doc, REJECT
        if kind == "reimburse-more-than-open":
            amount = money.EXACT.add(owed, unit)
        else:
            amount = owed if draw(st.booleans()) or owed <= unit else draw(_split(owed, 2, 2))[0]
            book.claims[target] = money.EXACT.subtract(owed, amount)
        # What a claim owes is in SGD (FR-AP-17, SFRS(I) 1-21): reimbursed in SGD, with no rate and no FX.
        doc.update(account=draw(st.sampled_from(["1010", "1011"])), currency="SGD",
                   parts=[{"document": target, "amount": _text(amount, 2)}])
        return doc, REJECT if kind in REJECTED_KINDS else POST

    # A claim.
    currency = draw(st.sampled_from(["USD", "JPY"])) if kind in ("claim-foreign", "claim-trip-foreign") else "SGD"
    minor = CURRENCIES[currency]
    lines, owed, receipts = [], Decimal(0), []  # receipts: each receipt line's net plus GST, in the claim's currency
    for _ in range(draw(st.integers(1, 3))):
        if kind in ("claim-trips", "claim-trip-foreign") or (kind == "claim" and draw(st.integers(0, 2)) == 0):
            vehicle = draw(st.sampled_from(sorted(MILEAGE)))
            distance = Decimal(draw(st.integers(1, 99999))).scaleb(-draw(st.integers(0, 2)), context=money.EXACT)
            amount = money.rnd_mileage(distance, MILEAGE[vehicle], 2)
            if amount.is_zero():
                distance, amount = Decimal(100), money.rnd_mileage(Decimal(100), MILEAGE[vehicle], 2)
            lines.append({"account": draw(st.sampled_from(EXPENSES)), "distance": format(distance, "f"), "vehicle": vehicle})
            owed = money.EXACT.add(owed, amount)
            continue
        net = draw(_cents(minor, 4))
        line: dict[str, Any] = {"account": draw(st.sampled_from(EXPENSES)), "amount": _text(net, minor),
                                "gst_code": draw(st.sampled_from(_PURCHASE_CODES))}
        if kind == "claim-wrong-code" and not lines:  # costed as a bill's line (FR-AP-17): a purchase code
            line["gst_code"] = draw(st.sampled_from(_NOT_PURCHASE))
        gst = Decimal(0)
        if line["gst_code"] in ("TX", "IM") and draw(st.booleans()):
            gst = money.round_quotient(money.EXACT.multiply(net, Decimal(9)), Decimal(100), minor)
            line["gst"] = _text(gst, minor)
        lines.append(line)
        owed = money.EXACT.add(owed, money.EXACT.add(net, gst))
        receipts.append(money.EXACT.add(net, gst))
    doc = {"type": "expense_claim", **base, "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        # What the company owes, in SGD: each receipt line at the claim's rate under RND-FX, summed (FR-AP-17).
        owed = money.total([money.rnd_fx(amount, Decimal(doc["fx_rate"]), 2) for amount in receipts])
    if kind in ("claim-trip-foreign", "claim-wrong-code"):
        return doc, REJECT
    if owed.is_zero():  # every receipt rounds to nothing in base (README, "P1 readings": a claim that owes S$0.00)
        return doc, REJECT
    if kind == "claim-advance":
        left = [a for a, amount in sorted(book.advances.items()) if amount > 0]
        if left:
            target = draw(st.sampled_from(left))
            doc["advance"] = target
            taken = min(owed, book.advances[target])
            book.advances[target] = money.EXACT.subtract(book.advances[target], taken)
            owed = money.EXACT.subtract(owed, taken)
    book.claims[doc_id] = owed
    return doc, POST


@st.composite
def _assets_document(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    if kind.startswith("dispose"):
        live = [a for a, disposed in sorted(book.assets.items()) if not disposed]
        gone = [a for a, disposed in sorted(book.assets.items()) if disposed]
        doc: dict[str, Any] = {"type": "asset_disposal", **base}
        if kind == "dispose-unknown" or (kind == "dispose-twice" and not gone) or not live and kind != "dispose-twice":
            doc["asset"] = "FA-NONE"
            return doc, REJECT
        if kind == "dispose-twice":
            doc["asset"] = draw(st.sampled_from(gone))
            return doc, REJECT
        doc["asset"] = target = draw(st.sampled_from(live))
        amount = _text(draw(_cents(2, 6)), 2)
        if kind == "dispose-registered-no-code" and book.registered(day):
            # FR-FA-03: a registered seller's sale is a taxable supply, so its proceeds name their supply code.
            doc["proceeds"] = {"account": draw(st.sampled_from(["1010", "1011"])), "amount": amount}
            return doc, REJECT
        if kind in ("dispose-wrong-code", "dispose-registered-no-code"):  # FR-FA-03: output tax, so a supply code
            doc["proceeds"] = {"account": draw(st.sampled_from(["1010", "1011"])), "amount": amount,
                               "gst_code": draw(st.sampled_from(_NOT_SUPPLY))}
            return doc, REJECT
        book.assets[target] = True
        if kind == "dispose-money":
            doc["proceeds"] = {"account": draw(st.sampled_from(["1010", "1011"])), "amount": amount}
            if book.registered(day) or draw(st.booleans()):  # a code is optional only while not registered
                doc["proceeds"]["gst_code"] = draw(st.sampled_from(_SUPPLY_CODES))
        elif kind == "dispose-clearing":
            doc["proceeds"] = {"account": "1160", "amount": amount}
        return doc, POST
    cost = draw(_cents(2, 6))
    doc = {"type": "fixed_asset", **base, "account": draw(st.sampled_from(["1500", "1510"])),
           "cost": _text(cost, 2), "life_months": draw(st.sampled_from([1, 3, 12, 36, 60, 120]))}
    if kind == "asset-residual" and cost > Decimal("0.01"):
        doc["residual"] = _text(draw(_split(cost, 2, 2))[0], 2)
    if kind == "asset-later-start":
        doc["depreciate_from"] = (day + timedelta(days=draw(st.integers(0, 120)))).isoformat()
    if kind == "asset-on-expense":
        doc["account"] = draw(st.sampled_from(EXPENSES))
        return doc, REJECT
    book.assets[doc_id] = False
    return doc, POST


@st.composite
def _part_of(draw: st.DrawFn, left: Decimal) -> Decimal:
    """All of ``left`` (positive, in SGD), or a part of it."""
    return left if draw(st.booleans()) or left <= Decimal("0.01") else draw(_split(left, 2, 2))[0]


@st.composite
def _share_price(draw: st.DrawFn, amount: Decimal) -> Decimal:
    """A price per share, at most 4 decimals and at least a cent where it can be, at which ``amount`` buys a whole
    share: about ``amount`` / n for n up to 10,000, rounded down, so n × price never exceeds the amount."""
    decimals = draw(st.integers(0, 4))
    cents = int(amount.scaleb(2, context=money.EXACT))
    units = cents * 10**decimals // (100 * draw(st.integers(1, 10**4)))
    if units == 0 or units * 100 < 10**decimals:  # below a cent, or nothing at these decimals
        return amount  # one share exactly
    return Decimal(units).scaleb(-decimals, context=money.EXACT)


@st.composite
def _founders_document(
    draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book
) -> tuple[dict[str, Any], str, str]:
    """A founders' document (README, "Costs before the books start" to "Conversions of founders' balances to
    shares"), its intent, and the kind it was built as (a waiver, repayment or conversion of nothing owed yet
    becomes a cost or a fee, a GST claim while not registered a cost that claims none)."""
    owed = sorted(key for key, left in book.owed.items() if left > 0)
    if kind in ("waiver", "repay", "convert", "convert-fair-value") and not owed:
        kind = draw(st.sampled_from(["cost", "fee"]))
    over = kind in OVER_OWED
    if kind == "cost-claim-gst" and not book.registered(day):
        kind = "cost"
    meant = REJECT if kind in REJECTED_KINDS else POST
    shape = kind.split("-")[0]
    if kind == "not-a-person":
        shape = draw(st.sampled_from(["cost", "fee", "waiver", "repay", "convert"]))
    elif kind == "not-directors-account":
        shape = draw(st.sampled_from(["waiver", "repay", "convert"]))
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    person = STRANGER if kind == "not-a-person" else draw(st.sampled_from(PEOPLE))
    loan, fees = DIRECTOR_ACCOUNTS["director_loan"][0], DIRECTOR_ACCOUNTS["deferred_remuneration"][0]
    if over:  # anyone on either account: a cent or more above the most it can owe them, or any amount if nothing
        person, account = draw(st.sampled_from(PEOPLE)), draw(st.sampled_from([loan, fees]))
        most = book.most.get((person, account), Decimal(0))
        left = money.EXACT.add(most, Decimal("0.01") if most > 0 and draw(st.booleans()) else draw(_cents(2, 4)))
    elif shape in ("waiver", "repay", "convert") and kind != "not-a-person" and owed:
        # Built to post, or refused for another rule: draws only on what the company owes them there.
        person, account = draw(st.sampled_from(owed))
        left = book.owed[(person, account)]
    else:  # refused either way: what it names need not be owed
        account, left = draw(st.sampled_from([loan, fees])), draw(_cents(2, 4))

    if shape == "cost":
        start = book.books_start
        assert start is not None
        gross = draw(_cents(2, 5))
        doc: dict[str, Any] = {"type": "pre_incorporation_cost", **base,
                               "incurred": (start - timedelta(days=draw(st.integers(1, 400)))).isoformat(),
                               "person": person, "gross": _text(gross, 2),
                               "account": draw(st.sampled_from(["6010", "6020", "6110", "6050", "1500"]))}
        if kind == "cost-before-books-start":  # README: posted on or after the books start
            posted = start - timedelta(days=draw(st.integers(1, 60)))
            doc["date"] = posted.isoformat()
            doc["incurred"] = (posted - timedelta(days=draw(st.integers(1, 300)))).isoformat()
        if kind == "cost-incurred-after-start":  # README: paid before the books start
            doc["incurred"] = (start + timedelta(days=draw(st.integers(0, (day - start).days)))).isoformat()
        if kind == "cost-account-not-allowed":  # not a director's account, a money account or GST control
            doc["account"] = draw(st.sampled_from([loan, fees, "1010", "1011", "2200"]))
        gst = money.round_quotient(money.EXACT.multiply(gross, Decimal(9)), Decimal(109), 2)  # what the receipt shows
        if kind == "cost-claim-gst" and gst.is_zero():
            kind = "cost"
        if not gst.is_zero() and (kind == "cost-claim-gst" or draw(st.integers(0, 2)) == 0):
            doc["gst"] = _text(gst, 2)
            if kind == "cost-claim-gst":
                doc["claim_gst"] = True
            elif draw(st.booleans()):
                doc["claim_gst"] = False
        if meant == POST:
            book.credit((person, loan), gross)
        return doc, meant, kind

    if shape == "fee":
        amount = draw(_cents(2, 5))
        doc = {"type": "director_fee", **base, "person": person,
               "account": draw(st.sampled_from(["6050", "6050", "6010"])), "amount": _text(amount, 2)}
        if kind == "fee-account-not-allowed":  # not a director's account, a money account or GST control
            doc["account"] = draw(st.sampled_from([fees, loan, "1010", "2200"]))
        if meant == POST:
            book.credit((person, fees), amount)
        return doc, meant, kind

    not_directors = ["6010", "2050", "2150"]  # accounts without a director's role
    if shape == "waiver":
        amount = left if over else draw(_part_of(left))
        doc = {"type": "director_waiver", **base, "person": person, "account": account,
               "credit": draw(st.sampled_from(["3300", "3300", *REVENUES])), "amount": _text(amount, 2)}
        if kind == "not-directors-account":
            doc["account"] = draw(st.sampled_from(not_directors))
        if kind == "waiver-credit-not-allowed":  # not a director's account, a money account or GST control
            doc["credit"] = draw(st.sampled_from(["1010", "1011", "2200", fees if account == loan else loan]))
        if meant == POST:
            book.debit((person, account), amount)
        return doc, meant, kind

    if shape == "repay":
        accounts = [account]
        if meant == POST:  # each director's account the person is owed on, one or both
            theirs = [a for p, a in owed if p == person]
            accounts = draw(st.lists(st.sampled_from(theirs), min_size=1, max_size=len(theirs), unique=True))
        settlements = []
        for a in accounts:
            amount = left if over else draw(_part_of(book.owed[(person, a)] if meant == POST else left))
            if over and amount > Decimal("0.01") and draw(st.booleans()):  # two settlements, together above it
                settlements += [{"account": a, "amount": _text(part, 2)} for part in draw(_split(amount, 2, 2))]
            else:
                settlements.append({"account": a, "amount": _text(amount, 2)})
            if meant == POST:
                book.debit((person, a), amount)
        if over and draw(st.booleans()):  # beside a settlement within what is owed on the other account: refused whole
            other = fees if account == loan else loan
            if book.owed.get((person, other), Decimal(0)) > 0:
                within = {"account": other, "amount": _text(draw(_part_of(book.owed[(person, other)])), 2)}
                settlements.insert(draw(st.integers(0, len(settlements))), within)
        doc = {"type": "director_repayment", **base, "person": person,
               "account": draw(st.sampled_from(["1010", "1011"])), "settlements": settlements}
        if kind == "repay-foreign":  # README: a money account in the base currency
            doc["account"] = draw(st.sampled_from(["1021", "1022", "1023"]))
        if kind == "repay-not-money":
            doc["account"] = draw(st.sampled_from(["6010", "1500", "2050"]))
        if kind == "not-directors-account":
            settlements[0]["account"] = draw(st.sampled_from(not_directors))
        return doc, meant, kind

    # A conversion to shares.
    if kind == "convert-no-whole-share":  # a price above the amount: RND-SHARE allots no share
        amount = draw(_part_of(left))
        decimals = draw(st.integers(2, 4))
        extra = Decimal(draw(st.integers(10 ** (decimals - 2), 10 ** (decimals + 1))))
        extra = extra.scaleb(-decimals, context=money.EXACT)
        price = money.EXACT.add(amount, extra)
    else:
        amount = left if over else draw(_part_of(left))
        price = draw(_share_price(amount))
    doc = {"type": "director_conversion", **base, "person": person, "account": account,
           "class": draw(st.sampled_from(SHARE_CLASSES)), "amount": _text(amount, 2), "price": format(price, "f")}
    if kind == "convert-fair-value":
        # In ten-thousandths: a fair value below the price is a gain (an income account), above it a loss (an
        # expense account); never below a cent, so the shares' fair value is never nothing.
        p4 = int(price.scaleb(4, context=money.EXACT))
        gain = p4 > 100 and draw(st.booleans())
        f4 = draw(st.integers(100, p4 - 1)) if gain else draw(st.integers(p4 + 1, 2 * p4 + 10**4))
        doc["fair_value_price"] = format(Decimal(f4).scaleb(-4, context=money.EXACT), "f")
        doc["difference"] = draw(st.sampled_from(REVENUES if gain else ["6010", "6020", "6110"]))
    if kind == "not-directors-account":
        doc["account"] = draw(st.sampled_from(not_directors))
    if kind == "convert-not-share-capital":  # README: the class is a share-capital account
        doc["class"] = draw(st.sampled_from(["3300", "3300", "4010"]))
    if meant == POST:  # what is left owed is at least the amount's remainder, at most what was owed
        book.debit((person, account), amount, exact=False)
    return doc, meant, kind


@st.composite
def p1_scenarios_with_intent(
    draw: st.DrawFn, sources: tuple[str, ...] = SOURCES, name: str = "generated-p1"
) -> tuple[dict[str, Any], Intent]:
    """A scenario of P1 documents from ``sources`` (with SGD as the base) and each one's intent."""
    kinds = [kind for source in sources for kind in KINDS[source]]
    documents, intent, built = [], {}, {}
    settings = draw(_settings())
    book = _Book(settings["gst"])
    days = sorted(draw(st.lists(_DATES, min_size=1, max_size=10)))
    accounts = _accounts()
    if "founders" in sources:  # README, "Settings and as_of": the books start on or before the first date
        month = days[0].year * 12 + days[0].month - 1 - draw(st.integers(0, 3))  # the first's month, or up to 3 before
        book.books_start = date(month // 12, month % 12 + 1, 1)
        settings["books_start"] = book.books_start.isoformat()
        settings["accounts"].update({key: code for key, (code, _) in DIRECTOR_ACCOUNTS.items()})
        accounts += _founders_accounts()
    for i, day in enumerate(days):
        kind = draw(st.sampled_from(kinds))
        doc_id = f"P1-{i + 1:04d}"
        if kind in KINDS["bank"]:
            document, meant = draw(_bank_document(doc_id, day, kind)), REJECT if kind in REJECTED_KINDS else POST
        elif kind in KINDS["purchases"]:
            document, meant = draw(_purchase_document(doc_id, day, kind, book))
        elif kind in KINDS["sales"]:
            document, meant = draw(_sales_document(doc_id, day, kind, book))
        elif kind in KINDS["claims"]:
            document, meant = draw(_claims_document(doc_id, day, kind, book))
        elif kind in KINDS["assets"]:
            document, meant = draw(_assets_document(doc_id, day, kind, book))
        else:
            document, meant, kind = draw(_founders_document(doc_id, day, kind, book))
        documents.append(document)
        intent[doc_id] = meant
        built[doc_id] = kind
    last = max(date.fromisoformat(d["date"]) for d in documents)
    scenario = {
        "schema": SCENARIO_SCHEMA,
        "name": name,
        "base_currency": "SGD",
        "currencies": dict(CURRENCIES),
        "as_of": (last + timedelta(days=draw(st.integers(0, 400)))).isoformat(),
        "settings": settings,
        "accounts": accounts,
        "documents": documents,
        "reversals": [],
    }
    if "founders" in sources:
        scenario["people"] = list(PEOPLE)
    return scenario, Intent(intent, built)


def p1_scenarios(sources: tuple[str, ...] = SOURCES, name: str = "generated-p1") -> st.SearchStrategy[dict[str, Any]]:
    return p1_scenarios_with_intent(sources, name).map(lambda pair: pair[0])
