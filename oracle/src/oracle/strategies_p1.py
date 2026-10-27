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

For sales: receipts, write-offs and deposits deducted only against invoices that charge no GST (so
the generator knows what each owes without computing GST), and receipts in full only on base-currency
invoices.

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

SOURCES = ("bank", "purchases", "sales", "claims")

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
}
EXPENSES = ["6010", "6020", "6110", "6460", "7030"]
REVENUES = ["4010", "4020", "4030"]
OTHER = ["1500", "2050", "2999"]  # an asset, an accrual, suspense: P&L-free parts
_DATES = st.dates(date(2025, 6, 1), date(2027, 3, 31))
_CODES_IN = ["SR", "SR", "ZR", "ES", "OS", "NA"]
_CODES_OUT = ["TX", "TX", "IM", "ZP", "BL", "NR", "EP", "OP", "NA"]

BANK_POSTING = ["bank-line", "bank-line", "bank-split", "bank-spread", "transfer-same", "transfer-cross"]
BANK_REJECTED = ["bank-parts-mismatch", "bank-part-on-money", "bank-part-on-control", "bank-spread-not-p-and-l",
                 "transfer-unbalanced", "transfer-missing-rate"]
PURCHASES_POSTING = ["bill", "bill", "bill-foreign", "bill-spread", "bill-quantity", "bill-withholding",
                     "vendor-credit", "pay", "pay", "pay-in-full", "pay-from-base", "pay-unapplied"]
PURCHASES_REJECTED = ["bill-spread-not-expense", "pay-more-than-open", "pay-beyond-tolerance", "pay-unknown-bill"]
SALES_POSTING = ["invoice", "invoice", "invoice-foreign", "invoice-discount", "invoice-spread", "credit-note",
                 "receipt", "receipt", "receipt-in-full", "receipt-from-base", "deposit", "deposit-security",
                 "invoice-deducting", "deposit-refund", "write-off"]
SALES_REJECTED = ["receipt-more-than-open", "invoice-spread-not-revenue", "invoice-deducting-too-much",
                  "write-off-more-than-open"]
CLAIMS_POSTING = ["claim", "claim", "claim-trips", "claim-foreign", "claim-advance", "reimburse", "reimburse",
                  "advance", "advance-refund"]
CLAIMS_REJECTED = ["reimburse-more-than-open", "claim-trip-foreign", "advance-refund-too-much"]
KINDS = {"bank": BANK_POSTING * 3 + BANK_REJECTED, "purchases": PURCHASES_POSTING * 2 + PURCHASES_REJECTED,
         "sales": SALES_POSTING * 2 + SALES_REJECTED, "claims": CLAIMS_POSTING * 2 + CLAIMS_REJECTED}
REJECTED_KINDS = frozenset(BANK_REJECTED + PURCHASES_REJECTED + SALES_REJECTED + CLAIMS_REJECTED)
MILEAGE = {"car": Decimal("0.60"), "motorcycle": Decimal("0.25")}
WALLETS = {"SGD": "1010", "USD": "1021", "JPY": "1022", "KWD": "1023"}


class _Book:
    """What the generator built so far: what a settlement may settle and what each has open."""

    def __init__(self) -> None:
        self.open: dict[str, tuple[str, Decimal]] = {}  # bill -> (currency, open amount)
        self.invoices: dict[str, tuple[str, Decimal]] = {}  # invoice charging no GST -> (currency, open)
        self.deposits: dict[str, tuple[str, Decimal]] = {}  # deposit -> (currency, what is left)
        self.claims: dict[str, tuple[str, Decimal]] = {}  # claim -> (currency, what is owed)
        self.advances: dict[str, Decimal] = {}  # advance -> what is left


def _accounts() -> list[dict[str, Any]]:
    def account(code: str, kind: str) -> dict[str, Any]:
        return {"code": code, "type": kind, "group": False, "active": True}

    chart = [account(code, "asset") for code in [*MONEY_ACCOUNTS, "1100", "1210", "1250", "1500"]]
    chart += [account(code, "liability") for code in ["2010", "2050", "2150", "2200", "2330", "2400", "2410", "2999"]]
    chart += [account(code, "revenue") for code in REVENUES]
    chart += [account(code, "expense") for code in EXPENSES]
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
                                "gst_code": draw(st.sampled_from(_CODES_OUT))}
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
        lines.append(line)
        total = money.EXACT.add(total, money.EXACT.add(net, gst))
    doc: dict[str, Any] = {"type": "vendor_credit" if kind == "vendor-credit" else "bill", "id": doc_id, "version": 1,
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
    if kind in ("deposit", "deposit-security"):
        currency = draw(st.sampled_from(["SGD", "SGD", "USD"]))
        amount = draw(_cents(CURRENCIES[currency], 6))
        if currency != "SGD" and amount < 1:  # a whole unit at least, so its base is at least a cent
            amount = money.EXACT.add(amount, Decimal(1))
        doc = {"type": "customer_deposit", **base, "account": WALLETS[currency] if currency != "SGD" else
               draw(st.sampled_from(["1010", "1011"])), "currency": currency,
               "amount": _text(amount, CURRENCIES[currency]),
               "kind": "security" if kind == "deposit-security" else "part_payment"}
        if kind == "deposit":
            doc["gst_code"] = draw(st.sampled_from(_CODES_IN))
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
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
    codes = ["ZR", "ES", "OS", "NA"] if tracked else _CODES_IN
    lines, total = [], Decimal(0)
    for i in range(draw(st.integers(1, 3))):
        line: dict[str, Any] = {"account": draw(st.sampled_from(REVENUES)), "gst_code": draw(st.sampled_from(codes))}
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
    doc = {"type": "credit_note" if kind == "credit-note" else "invoice", **base, "currency": currency, "lines": lines}
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
        open_ = [c for c, (_, owed) in sorted(book.claims.items()) if owed > 0]
        doc: dict[str, Any] = {"type": "claim_reimbursement", **base, "parts": []}
        if not open_:
            doc.update(account="1010", currency="SGD", parts=[{"document": "EC-NONE", "amount": "1.00"}])
            return doc, REJECT
        target = draw(st.sampled_from(open_))
        currency, owed = book.claims[target]
        minor = CURRENCIES[currency]
        unit = Decimal(1).scaleb(-minor, context=money.EXACT)
        if kind == "reimburse-more-than-open":
            amount = money.EXACT.add(owed, unit)
        else:
            amount = owed if draw(st.booleans()) or owed <= unit else draw(_split(owed, 2, minor))[0]
            book.claims[target] = (currency, money.EXACT.subtract(owed, amount))
        doc.update(account="1010", currency=currency, parts=[{"document": target, "amount": _text(amount, minor)}])
        if currency != "SGD":
            doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"  # reimbursed in SGD at the cascade's
        return doc, REJECT if kind in REJECTED_KINDS else POST

    # A claim.
    currency = draw(st.sampled_from(["USD", "JPY"])) if kind in ("claim-foreign", "claim-trip-foreign") else "SGD"
    minor = CURRENCIES[currency]
    lines, owed = [], Decimal(0)
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
                                "gst_code": draw(st.sampled_from(_CODES_OUT))}
        gst = Decimal(0)
        if line["gst_code"] in ("TX", "IM") and draw(st.booleans()):
            gst = money.round_quotient(money.EXACT.multiply(net, Decimal(9)), Decimal(100), minor)
            line["gst"] = _text(gst, minor)
        lines.append(line)
        owed = money.EXACT.add(owed, money.EXACT.add(net, gst))
    doc = {"type": "expense_claim", **base, "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    if kind == "claim-trip-foreign":
        return doc, REJECT
    if kind == "claim-advance":
        left = [a for a, amount in sorted(book.advances.items()) if amount > 0]
        if left:
            target = draw(st.sampled_from(left))
            doc["advance"] = target
            taken = min(owed, book.advances[target])
            book.advances[target] = money.EXACT.subtract(book.advances[target], taken)
            owed = money.EXACT.subtract(owed, taken)
    book.claims[doc_id] = (currency, owed)
    return doc, POST


@st.composite
def p1_scenarios_with_intent(
    draw: st.DrawFn, sources: tuple[str, ...] = SOURCES, name: str = "generated-p1"
) -> tuple[dict[str, Any], Intent]:
    """A scenario of P1 documents from ``sources`` (with SGD as the base) and each one's intent."""
    kinds = [kind for source in sources for kind in KINDS[source]]
    documents, intent = [], {}
    book = _Book()
    days = sorted(draw(st.lists(_DATES, min_size=1, max_size=10)))
    for i, day in enumerate(days):
        kind = draw(st.sampled_from(kinds))
        doc_id = f"P1-{i + 1:04d}"
        if kind in KINDS["bank"]:
            document, meant = draw(_bank_document(doc_id, day, kind)), REJECT if kind in REJECTED_KINDS else POST
        elif kind in KINDS["purchases"]:
            document, meant = draw(_purchase_document(doc_id, day, kind, book))
        elif kind in KINDS["sales"]:
            document, meant = draw(_sales_document(doc_id, day, kind, book))
        else:
            document, meant = draw(_claims_document(doc_id, day, kind, book))
        documents.append(document)
        intent[doc_id] = meant
    last = max(date.fromisoformat(d["date"]) for d in documents)
    scenario = {
        "schema": SCENARIO_SCHEMA,
        "name": name,
        "base_currency": "SGD",
        "currencies": dict(CURRENCIES),
        "as_of": (last + timedelta(days=draw(st.integers(0, 400)))).isoformat(),
        "settings": draw(_settings()),
        "accounts": _accounts(),
        "documents": documents,
        "reversals": [],
    }
    return scenario, Intent(intent)


def p1_scenarios(sources: tuple[str, ...] = SOURCES, name: str = "generated-p1") -> st.SearchStrategy[dict[str, Any]]:
    return p1_scenarios_with_intent(sources, name).map(lambda pair: pair[0])
