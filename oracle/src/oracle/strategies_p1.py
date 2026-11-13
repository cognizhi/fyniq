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
invoices; a foreign invoice whose GST is S$0.00 in base and whose entry is left all in its currency, out of balance
there, is meant to be refused (README, "P1 readings"; INV-01). For claims: what the claimant paid (``paid``) on a third of the receipt lines of a claim in another currency,
a few cents and up to 3% off the line at the claim's rate, and a third of the claims settling an advance in another
currency.

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

For the month-end revaluation (README, "Month-end revaluation"; source ``fx``, which ``make differential`` draws only
once ``cmd/scenario-run`` reads its shapes): the foreign money accounts, 1100 and 2010 marked ``revalue`` (the SGD
money accounts in half the scenarios, and one account left unmarked in a fifth), 7040 as ``unrealised_fx``, and a
period-end rate for each currency at each month's end from the first document's month to ``as_of``'s, a stored rate or
a quote in one of ``RND-RATE``'s forms, one in twelve left out so its month waits. Its kinds are the foreign
documents of the other sources (bills, invoices, receipts and payments at another rate, a receipt from SGD keeping an
unapplied amount, transfers into and out of the wallets, a USD deposit) and its own: a manual journal moving a
revalued account in another currency (the item no document supports), and a payment of a foreign bill from SGD keeping
an unapplied amount at the rate it implies (``RND-RATE``), and transfers into a foreign wallet and out of it again at
another rate, which leave a balance of nothing carrying a base amount. Its refusals are those of the other sources on foreign
documents, and a journal on a revalued account that does not balance: a refused document leaves no item.

For the general ledger (README, "Opening balances, automatic reversals and edits"; :mod:`oracle.strategies_gl`):
opening balances at the books' start, manual journals that may reverse by themselves, and the voids and edits of
``reversals``, with the founders' chart, people and books' start; a director's opening balance counts in what the
founders' documents after it may draw on. A third of those scenarios declare dimensions (README, "Dimensions"), with
6200 and 4200 joining the chart to require them, and draw the general ledger's dimension kinds besides its others.

For the equity register (README, "Cash share issues", "Share transfers" and "Cap tables"; source ``equity``): share
application monies (2040) and the share classes, each class's votes per share drawn; the subscribers ``P0003`` and
``P0004``, whose money comes in and is refunded (within what they paid) on bank lines naming them; cash issues within
what each has paid; transfers of what a subscriber holds to another holder, the founders' people among them when their
source is drawn too (only the subscribers sell, so what each holds is exact); SAFEs recorded, their money on 2700
naming them, and their conversion at a posted round, whose shares the generator works out as README "SAFEs and their
conversion" states them; and one to three days to state the cap table at, a document's day or ``as_of`` as often as
any from a month before the first document. Three scenarios in four start with money in and issues, so the documents
after them find shares to transfer, and one in four of those records a SAFE, pays it in and converts it at a round.

The generator keeps what it built so far in a :class:`_Book` (the bills and invoices a settlement may
settle and what each has open, the deposits and what is left of each), so a document built to post
really can.
"""

from __future__ import annotations

from datetime import date, timedelta
from decimal import Decimal
from fractions import Fraction
from typing import Any

from hypothesis import strategies as st

from oracle import money, strategies_gl
from oracle.scenario import SCENARIO_SCHEMA
from oracle.strategies import POST, REJECT, Intent

SOURCES = ("bank", "purchases", "sales", "claims", "assets", "founders", "general-ledger", "fx", "equity", "opening-items")

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
# The "claim-paid-" kinds state what the claimant paid where it may not be (README, "Expense claims"): on a line of a
# base-currency claim, on a trip, or not a valid base-currency amount above zero.
PAID_REJECTED = ["claim-paid-on-base", "claim-paid-on-trip", "claim-paid-invalid"]
CLAIMS_REJECTED = ["reimburse-more-than-open", "claim-trip-foreign", "advance-refund-too-much", "claim-wrong-code",
                   "reimburse-in-another-currency", *PAID_REJECTED]
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
# The month-end revaluation's source: the other sources' foreign documents, and its own kinds (FX_OWN).
FX_POSTING = ["bill-foreign", "bill-foreign", "invoice-foreign", "invoice-foreign", "pay", "receipt", "receipt-from-base",
              "transfer-cross", "transfer-cross", "pay-unapplied", "deposit", "fx-journal", "fx-journal",
              "fx-pay-from-base-unapplied", "fx-wallet-in", "fx-wallet-out"]
FX_REJECTED = ["pay-more-than-open", "receipt-more-than-open", "transfer-missing-rate", "fx-journal-unbalanced"]
FX_OWN = frozenset({"fx-journal", "fx-journal-unbalanced", "fx-pay-from-base-unapplied", "fx-wallet-in", "fx-wallet-out"})
# The equity register (README, "Cash share issues", "Share transfers" and "Cap tables"): money for shares in, and
# refunded, on bank lines naming a subscriber; cash issues within what each subscriber has paid; transfers of what a
# subscriber holds to another holder; SAFEs, their money and their conversion. Refused: an issue above what is paid, a
# transfer of more than the seller holds, to themselves, or of an account that is not share capital; a SAFE that breaks
# a rule, money on 2700 naming no recorded SAFE, and a conversion of a SAFE whose money never came. The option pool
# (README, "The option pool and grants"): resolutions covering what is granted, and grants within what the pool leaves;
# refused, a grant beyond it or before any pool, and a resolution smaller than what is granted. A grant's forfeitures
# and expiries; refused, one of more than the grant has left, and a forfeiture of options vested. Exercises of vested
# options paid from the holder's monies; refused, one of options not vested.
EQUITY_POSTING = ["equity-money-in", "equity-money-in", "equity-refund", "equity-issue", "equity-issue",
                  "equity-transfer", "equity-transfer", "equity-safe", "equity-safe-money", "equity-safe-conversion",
                  "equity-pool", "equity-grant", "equity-grant", "equity-forfeiture", "equity-expiry", "equity-exercise"]
EQUITY_REJECTED = ["equity-issue-unpaid", "equity-transfer-too-many", "equity-transfer-to-self",
                   "equity-transfer-not-share-capital", "equity-safe-bad", "equity-safe-money-unnamed",
                   "equity-safe-conversion-unfunded", "equity-grant-beyond", "equity-grant-no-pool", "equity-pool-short",
                   "equity-event-too-many", "equity-forfeit-vested", "equity-exercise-not-vested"]
# Opening open items (README, "Opening open items"; FR-GL-06, rev 0.257): invoices and bills from before the books'
# start, posted at it against the clearing account, then settled by receipts and payments like any other. Refused: one
# dated on or after the books' start, and one that spreads.
OPENING_POSTING = ["opening-invoice", "opening-invoice", "opening-invoice-foreign", "opening-invoice-taxed", "opening-bill",
                   "opening-bill", "opening-bill-foreign", "receipt", "receipt", "receipt-in-full", "pay", "pay", "pay-in-full"]
OPENING_REJECTED = ["opening-item-not-before-start", "opening-item-spread"]
OPENING_CLEARING = "3900"
REVALUED = ["1021", "1022", "1023", "1100", "2010"]  # README, "Month-end revaluation": the standard chart's flags
UNREALISED_FX = "7040"
RATE_FORMS = ["fx_rate", "fx_rate", "base-per-unit", "base-per-100", "foreign-per-base"]
KINDS = {"bank": BANK_POSTING * 3 + BANK_REJECTED, "purchases": PURCHASES_POSTING * 2 + PURCHASES_REJECTED,
         "sales": SALES_POSTING * 2 + SALES_REJECTED, "claims": CLAIMS_POSTING * 2 + CLAIMS_REJECTED,
         "assets": ASSETS_POSTING * 2 + ASSETS_REJECTED, "founders": FOUNDERS_POSTING * 2 + FOUNDERS_REJECTED,
         "general-ledger": strategies_gl.KINDS, "fx": FX_POSTING * 2 + FX_REJECTED,
         "equity": EQUITY_POSTING * 2 + EQUITY_REJECTED, "opening-items": OPENING_POSTING * 2 + OPENING_REJECTED}
REJECTED_KINDS = frozenset(BANK_REJECTED + PURCHASES_REJECTED + SALES_REJECTED + CLAIMS_REJECTED + ASSETS_REJECTED
                           + FOUNDERS_REJECTED + FX_REJECTED + EQUITY_REJECTED + OPENING_REJECTED)
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
# The investors who are not among PEOPLE (README, "Cash share issues"), and share application monies.
SUBSCRIBERS = ["P0003", "P0004"]
MONIES = "2040"
# The SAFEs (README, "SAFEs and their conversion"): the account of their money, and their forms.
HOLDING = "2700"
SAFE_FORMS = ["post_money_safe", "pre_money_safe", "discount_safe", "mfn_safe"]


class _Book:
    """What the generator built so far: what a settlement may settle and what each has open."""

    def __init__(self, gst: dict[str, Any] | None = None) -> None:
        self.gst = gst or {}  # the scenario's GST settings, drawn first, so a document can depend on them
        self.open: dict[str, tuple[str, Decimal]] = {}  # bill -> (currency, open amount)
        self.invoices: dict[str, tuple[str, Decimal]] = {}  # invoice charging no GST -> (currency, open)
        self.invoice_base: dict[str, tuple[Decimal | None, Decimal]] = {}  # invoice -> (its rate, its open base)
        self.deposits: dict[str, tuple[str, Decimal]] = {}  # deposit -> (currency, what is left)
        self.claims: dict[str, Decimal] = {}  # claim -> what is owed, in SGD (FR-AP-17: owed in the base currency)
        self.advances: dict[str, Decimal] = {}  # advance -> what is left
        self.assets: dict[str, bool] = {}  # registered asset -> disposed
        self.wallets: dict[str, Decimal] = {}  # foreign wallet -> what the fx source's own transfers put in it
        self.books_start: date | None = None
        # (person, director's account) -> at least what the company still owes them there, in SGD
        self.owed: dict[tuple[str, str], Decimal] = {}
        # (person, director's account) -> at most what it owes them there: exact but for a conversion's remainder
        self.most: dict[tuple[str, str], Decimal] = {}
        self.monies: dict[str, Decimal] = {}  # subscriber -> what their share application monies hold
        self.shares: dict[tuple[str, str], int] = {}  # (subscriber, class) -> the shares they hold, exactly
        self.holders: list[str] = list(SUBSCRIBERS)  # whom a transfer may move shares to
        self.issued: list[tuple[date, int]] = []  # every allotment of shares posted: its day and its shares
        self.rounds: list[tuple[str, date, Decimal]] = []  # the cash issues posted: a SAFE's priced rounds
        self.safes: dict[str, dict[str, Any]] = {}  # SAFE -> its terms, in the order recorded
        self.funded: set[str] = set()  # the SAFEs whose whole amount is on 2700
        self.converted: set[str] = set()
        # The option pool: the resolution in force (its size and class), the last recorded, as the documents come in
        # date order; and every option granted so far, all by the latest day.
        self.pool: tuple[int, str] | None = None
        self.granted = 0  # less what events gave back
        # grant -> [options it has left, whether they all vested on its day, its exercise price, holder, class]: the
        # others vest past every date drawn
        self.grants: dict[str, list[Any]] = {}
        self.pay_next: str | None = None  # a vested grant's holder whose money comes in next, so they may exercise

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
def _opening_document(draw: st.DrawFn, doc_id: str, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    """An opening invoice or bill (README, "Opening open items"; FR-GL-06, rev 0.257): dated before the books' start, it
    posts its total at the books' start against the clearing account, and is kept open for the receipts and payments
    that settle it. Its lines bear no GST but the taxed invoice's, which is in the base currency and kept out of what
    later receipts settle. Refused: an item dated on or after the books' start, and one that spreads."""
    assert book.books_start is not None
    sales = kind.startswith("opening-invoice") or kind in OPENING_REJECTED and draw(st.booleans())
    if kind == "opening-item-not-before-start":
        day = book.books_start + timedelta(days=draw(st.integers(0, 60)))
    else:
        day = book.books_start - timedelta(days=draw(st.integers(1, 120)))
    currency = draw(st.sampled_from(["USD", "JPY", "KWD"])) if kind.endswith("-foreign") else "SGD"
    minor = CURRENCIES[currency]
    taxed = kind == "opening-invoice-taxed"
    lines, total = [], Decimal(0)
    for i in range(draw(st.integers(1, 3))):
        net = draw(_cents(minor, 6))
        if sales:
            codes = ["SR", "ZR"] if taxed else ["ZR", "ES", "OS"]
            line: dict[str, Any] = {"account": draw(st.sampled_from(REVENUES)), "gst_code": draw(st.sampled_from(codes)),
                                    "amount": _text(net, minor)}
        else:
            line = {"account": draw(st.sampled_from(EXPENSES + ["1500"])), "gst_code": draw(st.sampled_from(_PURCHASE_CODES)),
                    "amount": _text(net, minor)}
            if line["gst_code"] in ("TX", "IM") and draw(st.booleans()):
                gst = money.round_quotient(money.EXACT.multiply(net, Decimal(9)), Decimal(100), minor)
                line["gst"] = _text(gst, minor)
                net = money.EXACT.add(net, gst)
        if kind == "opening-item-spread" and i == 0:
            line["account"] = draw(st.sampled_from(REVENUES if sales else EXPENSES))
            line["spread_months"] = draw(st.sampled_from([3, 12]))
        lines.append(line)
        total = money.EXACT.add(total, net)
    doc: dict[str, Any] = {"type": "invoice" if sales else "bill", "id": doc_id, "version": 1, "date": day.isoformat(),
                           "currency": currency, "lines": lines, "opening_item": True}
    rate = None
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        rate = Decimal(doc["fx_rate"])
    if kind in OPENING_REJECTED:
        return doc, REJECT
    base = total if rate is None else money.rnd_fx(total, rate, 2)
    if base.is_zero():
        return doc, REJECT  # its base amount rounds to nothing, so only the control account's line is left (README, "P1 readings")
    if taxed:
        return doc, POST  # its GST depends on the registration on its date: not tracked for settlement
    if sales:
        book.invoices[doc_id] = (currency, total)
        book.invoice_base[doc_id] = (rate, base)
    else:
        book.open[doc_id] = (currency, total)
    return doc, POST


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
        meant = REJECT if kind in REJECTED_KINDS else POST
        if kind == "write-off-more-than-open":
            amount = money.EXACT.add(left, unit)
        else:
            amount = left if draw(st.booleans()) or left <= unit else draw(_split(left, 2, minor))[0]
            if _carrying(book, target, amount, left).is_zero():
                # Its base is S$0.00, so the account's line drops out and 1100's is left alone (README, "P1 readings").
                meant = REJECT
            else:
                _settle_invoice(book, target, amount, left)
        doc = {"type": "write_off", **base, "invoice": target, "amount": _text(amount, minor),
               "account": draw(st.sampled_from(["6010", "7030", "4010"]))}
        return doc, meant

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
    lines, total, nets = [], Decimal(0), []  # nets: each line's (GST code, net), in the invoice's currency
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
        nets.append((line["gst_code"], net))
    credit = kind == "credit-note" or kind == "invoice-wrong-code" and draw(st.booleans())
    doc = {"type": "credit_note" if credit else "invoice", **base, "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
    meant = REJECT if kind in REJECTED_KINDS else POST
    if kind == "invoice-foreign" and _left_unbalanced_in_its_currency(nets, Decimal(doc["fx_rate"]), day, book, minor):
        meant = REJECT  # README, "P1 readings": all in one currency and out of balance in it (INV-01)
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
        rate = Decimal(doc["fx_rate"]) if currency != "SGD" else None
        book.invoice_base[doc_id] = (rate, total if rate is None else money.rnd_fx(total, rate, 2))  # its 1100 line
    return doc, meant


def _carrying(book: _Book, invoice: str, amount: Decimal, left: Decimal) -> Decimal:
    """What settling ``amount`` of what is left of a tracked invoice takes off 1100 in base: what is left of its base
    when it settles all that is left, else ``RND-FX`` of the amount at the invoice's stored rate (README, "Invoices":
    a write-off)."""
    rate, open_base = book.invoice_base[invoice]
    if amount == left:
        return open_base
    return amount if rate is None else money.rnd_fx(amount, rate, 2)


def _settle_invoice(book: _Book, invoice: str, amount: Decimal, left: Decimal) -> None:
    currency, _ = book.invoices[invoice]
    rate, open_base = book.invoice_base[invoice]
    book.invoice_base[invoice] = (rate, money.EXACT.subtract(open_base, _carrying(book, invoice, amount, left)))
    book.invoices[invoice] = (currency, money.EXACT.subtract(left, amount))


def _left_unbalanced_in_its_currency(nets: list[tuple[str, Decimal]], rate: Decimal, day: date, book: _Book,
                                     minor: int) -> bool:
    """Whether an invoice in another currency (no spread, no deposit) is left all in its currency without balancing
    in it (README, "P1 readings"; INV-01): 1100 takes its nets plus their ``RND-GST-EXCL`` in its currency and each
    revenue line its net, so it is out of balance there by that GST; it stays all in that currency when the GST in base
    (2200) is S$0.00 and 7030 is nothing (USD 0.10 on SR at 0.1, registered: USD 0.11 against 0.10)."""
    if not book.registered(day):
        return False
    rates = [row for row in book.gst.get("rates", ()) if row["from"] <= day.isoformat()]
    taxed = [net for code, net in nets if code == "SR"]
    if not rates or not taxed:
        return False
    percent = Decimal(max(rates, key=lambda row: row["from"])["percent"])

    def gst(amounts: list[Decimal], units: int) -> Decimal:
        if book.gst.get("per_line", False):
            return money.total([money.round_quotient(money.EXACT.multiply(a, percent), Decimal(100), units)
                                for a in amounts])
        return money.round_quotient(money.EXACT.multiply(money.total(amounts), percent), Decimal(100), units)

    in_currency = gst(taxed, minor)
    if in_currency.is_zero() or not gst([money.rnd_fx(net, rate, 2) for net in taxed], 2).is_zero():
        return False
    customer = money.rnd_fx(money.EXACT.add(money.total([net for _, net in nets]), in_currency), rate, 2)
    return money.EXACT.subtract(customer, money.total([money.rnd_fx(net, rate, 2) for _, net in nets])).is_zero()


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
        _settle_invoice(book, target, left, left)  # in full: all that is left, its base too
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
    _settle_invoice(book, target, amount, left)
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

    # A claim. One settling an advance is in another currency a third of the time (UC-EXP-03: it owes SGD).
    foreign = kind in ("claim-foreign", "claim-trip-foreign", "claim-paid-invalid") or \
        (kind == "claim-advance" and draw(st.integers(0, 2)) == 0)
    currency = draw(st.sampled_from(["USD", "JPY"])) if foreign else "SGD"
    minor = CURRENCIES[currency]
    lines, owed, receipts = [], Decimal(0), []  # receipts: each receipt line and its net plus GST (claim's currency)
    for _ in range(draw(st.integers(1, 3))):
        if kind in ("claim-trips", "claim-trip-foreign", "claim-paid-on-trip") or \
                (kind == "claim" and draw(st.integers(0, 2)) == 0):
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
        receipts.append((line, money.EXACT.add(net, gst)))
    if kind in ("claim-paid-on-base", "claim-paid-on-trip"):  # a base-currency claim's line is already what was paid
        lines[0]["paid"] = _text(draw(_cents(2, 4)), 2)
    doc = {"type": "expense_claim", **base, "currency": currency, "lines": lines}
    if currency != "SGD":
        doc["fx_rate"], doc["fx_rate_source"] = draw(_rate()), "document"
        # What the company owes, in SGD: each receipt line at the claim's rate under RND-FX, or what the claimant states
        # they paid for it (rev 0.169), summed (FR-AP-17).
        owed = Decimal(0)
        for i, (line, amount) in enumerate(receipts):
            at_rate = money.rnd_fx(amount, Decimal(doc["fx_rate"]), 2)
            if kind == "claim-paid-invalid" and i == 0:
                line["paid"] = draw(_paid_invalid(at_rate))
            elif draw(st.integers(0, 2)) == 0:
                paid = draw(_paid(at_rate))
                line["paid"] = _text(paid, 2)
                at_rate = paid
            owed = money.EXACT.add(owed, at_rate)
    if kind in CLAIMS_REJECTED:
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
def _paid(draw: st.DrawFn, at_rate: Decimal) -> Decimal:
    """What a claimant states they paid for a receipt (FR-AP-17 rev 0.169): its amount at the claim's rate, a few cents
    and up to 3% off either way (a card's rate and spread), and above zero."""
    hundredths = Decimal(draw(st.integers(-300, 300)))  # of a percent: up to 3% either way
    percent = money.round_quotient(money.EXACT.multiply(at_rate, hundredths), Decimal(10000), 2)
    cents = Decimal(draw(st.integers(-5, 5))).scaleb(-2, context=money.EXACT)
    paid = money.EXACT.add(money.EXACT.add(at_rate, percent), cents)
    return paid if paid > 0 else Decimal("0.01")


@st.composite
def _paid_invalid(draw: st.DrawFn, at_rate: Decimal) -> str:
    """A ``paid`` the claim refuses: not above zero, or not a valid base-currency amount (rule 1, ENG-44)."""
    text = _text(max(at_rate, Decimal("0.01")), 2)
    return draw(st.sampled_from(["0", "0.00", f"-{text}", f"{text}1", "1e2", "12,00", " 1.00", "+1.00", "1.", ".50"]))


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
        book.issued.append((date.fromisoformat(base["date"]), money.rnd_share(amount, price, 2)[0]))  # its shares
    return doc, meant, kind


def _equity_accounts(draw: st.DrawFn, accounts: list[dict[str, Any]]) -> None:
    """Share application monies and the share classes (README, "Cap tables"), each class's votes per share drawn."""
    accounts.append({"code": MONIES, "type": "liability", "group": False, "active": True,
                     "role": "share-application-monies"})
    accounts.append({"code": HOLDING, "type": "liability", "group": False, "active": True,
                     "role": "convertible-instruments"})
    have = {a["code"]: a for a in accounts}
    for code in SHARE_CLASSES:
        if code not in have:
            have[code] = {"code": code, "type": "equity", "group": False, "active": True, "role": "share-capital"}
            accounts.append(have[code])
        votes = draw(st.sampled_from([None, None, "1", "0", "10", "0.5"]))
        if votes is not None:
            have[code]["votes_per_share"] = votes


@st.composite
def _equity_document(
    draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book
) -> tuple[dict[str, Any], str, str]:
    """A document of the equity register, its intent and the kind it was built as (a refund or issue with nothing paid,
    or a transfer with nothing held, becomes money in). Only the subscribers sell, so what each holds is exact."""
    paid = sorted(p for p, left in book.monies.items() if left > 0)
    held = sorted(k for k, n in book.shares.items() if n > 0)
    converting = [sid for sid in book.safes if sid not in book.converted]
    never_funded = [sid for sid in converting if sid not in book.funded]
    if kind == "equity-safe-money" and not never_funded:
        kind = "equity-safe"
    if kind == "equity-safe-conversion-unfunded":  # a round at which every price works, so only the money is short
        outcome = _conversion(draw, book, day, converting) if never_funded else None
        if outcome is None:
            kind = "equity-safe"
    if kind == "equity-safe-conversion":
        if not converting:
            kind = "equity-safe"
        elif never_funded:
            kind = "equity-safe-money"
        else:
            outcome = _conversion(draw, book, day, converting)
            if outcome is None:  # no round with shares before it, or one that would refuse it: money in instead
                kind = "equity-money-in"
    if (kind in ("equity-refund", "equity-issue") and not paid) or \
            (kind in ("equity-transfer", "equity-transfer-to-self") and not held):
        kind = "equity-money-in"
    if kind in ("equity-grant", "equity-grant-beyond") and book.pool is None:
        kind = "equity-pool"
    if kind == "equity-grant" and book.pool is not None and book.pool[0] == book.granted:
        kind = "equity-grant-beyond"  # nothing left to grant
    if kind == "equity-grant-no-pool" and book.pool is not None:
        kind = "equity-grant-beyond"
    if kind == "equity-pool-short" and book.granted < 2:
        kind = "equity-pool"
    left = [g for g, v in book.grants.items() if v[0] > 0]
    if kind in ("equity-forfeiture", "equity-expiry", "equity-event-too-many", "equity-forfeit-vested") and not left:
        kind = "equity-grant" if book.pool is not None else "equity-pool"
    if kind == "equity-forfeiture" and not any(not book.grants[g][1] for g in left):
        kind = "equity-expiry"  # every grant left vested on its day
    if kind == "equity-forfeit-vested" and not any(book.grants[g][1] for g in left):
        kind = "equity-event-too-many"
    if kind == "equity-exercise" and not any(book.grants[g][1] and book.monies.get(book.grants[g][3], Decimal(0)) >= book.grants[g][2]
                                             for g in left):
        kind = "equity-money-in"
        book.pay_next = next((book.grants[g][3] for g in left if book.grants[g][1]), None)
    if kind == "equity-exercise-not-vested" and not any(not book.grants[g][1] for g in left):
        kind = "equity-grant" if book.pool is not None else "equity-pool"
    if kind == "equity-grant" and book.pool is not None and book.pool[0] == book.granted:
        kind = "equity-grant-beyond"
    meant = REJECT if kind in REJECTED_KINDS else POST
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    if kind.startswith(("equity-pool", "equity-grant")):
        return _option(draw, base, kind, book), meant, kind
    if kind in ("equity-forfeiture", "equity-expiry", "equity-event-too-many", "equity-forfeit-vested"):
        return _option_event(draw, base, kind, book), meant, kind
    if kind in ("equity-exercise", "equity-exercise-not-vested"):
        return _exercise(draw, base, kind, book, day), meant, kind
    if kind in ("equity-safe", "equity-safe-bad"):
        return _safe(draw, base, kind, book), meant, kind
    if kind in ("equity-safe-money", "equity-safe-money-unnamed"):
        safe = draw(st.sampled_from(never_funded)) if kind == "equity-safe-money" else None
        amount = book.safes[safe]["amount"] if safe else Decimal("10.00")
        doc = {"type": "bank_transaction", **base, "account": "1010", "currency": "SGD", "amount": _text(amount, 2),
               "parts": [{"account": HOLDING, "amount": _text(amount, 2), "gst_code": "NA"}]}
        if safe:
            doc["instrument"] = safe
            book.funded.add(safe)
        elif draw(st.booleans()):
            doc["instrument"] = "SAFE-9999"  # never recorded
        return doc, meant, kind
    if kind == "equity-safe-conversion-unfunded":  # a SAFE whose money never came: refused for it
        return {"type": "safe_conversion", **base, "round": outcome[0]}, meant, kind
    if kind == "equity-safe-conversion":
        round_id, allotted = outcome
        for sid, shares in allotted:
            terms = book.safes[sid]
            book.shares[(terms["holder"], terms["class"])] = book.shares.get((terms["holder"], terms["class"]), 0) + shares
            book.issued.append((day, shares))
            book.converted.add(sid)
        return {"type": "safe_conversion", **base, "round": round_id}, meant, kind
    if kind in ("equity-money-in", "equity-refund"):
        person = draw(st.sampled_from(SUBSCRIBERS if kind == "equity-money-in" else paid))
        if kind == "equity-money-in" and book.pay_next:
            person, book.pay_next = book.pay_next, None
        amount = draw(_cents(2, 6)) if kind == "equity-money-in" else money.negate(draw(_part_of(book.monies[person])))
        book.monies[person] = money.EXACT.add(book.monies.get(person, Decimal(0)), amount)
        return {"type": "bank_transaction", **base, "account": "1010", "currency": "SGD", "amount": _text(amount, 2),
                "person": person, "parts": [{"account": MONIES, "amount": _text(amount, 2), "gst_code": "NA"}]}, meant, kind
    if kind in ("equity-issue", "equity-issue-unpaid"):
        person = draw(st.sampled_from(paid if kind == "equity-issue" else SUBSCRIBERS))
        share_class = draw(st.sampled_from(SHARE_CLASSES))
        left = book.monies.get(person, Decimal(0))
        if kind == "equity-issue-unpaid":  # at 1.00 a share, a dollar or more above what is paid
            shares, price = int(left) + draw(st.integers(1, 1000)), Decimal("1.00")
        else:  # n × price is at most what is paid, so its cents are too (RND-LINE)
            price = draw(_share_price(left))
            shares = draw(st.integers(1, min(int(money.EXACT.divide_int(left, price)), 10**7)))
            book.monies[person] = money.EXACT.subtract(left, money.rnd_line(Decimal(shares), price, 2))
            book.shares[(person, share_class)] = book.shares.get((person, share_class), 0) + shares
            book.issued.append((day, shares))
            book.rounds.append((doc_id, day, price))
        return {"type": "share_issue_cash", **base, "person": person, "class": share_class, "shares": str(shares),
                "price": format(price, "f")}, meant, kind
    if kind == "equity-transfer-too-many":
        seller, share_class = draw(st.sampled_from(SUBSCRIBERS)), draw(st.sampled_from(SHARE_CLASSES))
        shares = book.shares.get((seller, share_class), 0) + draw(st.integers(1, 1000))
    else:
        seller, share_class = draw(st.sampled_from(held)) if held else (draw(st.sampled_from(SUBSCRIBERS)), "3010")
        shares = draw(st.integers(1, max(1, book.shares.get((seller, share_class), 0))))
    buyer = seller if kind == "equity-transfer-to-self" else draw(st.sampled_from([h for h in book.holders if h != seller]))
    doc = {"type": "share_transfer", **base, "seller": seller, "buyer": buyer, "class": share_class,
           "shares": str(shares)}
    if kind == "equity-transfer-not-share-capital":
        doc["class"] = draw(st.sampled_from([MONIES, "1010", "4010"]))
    if meant == POST:
        book.shares[(seller, share_class)] -= shares
        if buyer in SUBSCRIBERS:
            book.shares[(buyer, share_class)] = book.shares.get((buyer, share_class), 0) + shares
    return doc, meant, kind


def _option(draw: st.DrawFn, base: dict[str, Any], kind: str, book: _Book) -> dict[str, Any]:
    """A pool resolution or a grant (README, "The option pool and grants"). The documents come in date order, so every
    earlier grant is made by the day: a resolution covers them all, or is short; a grant fits what the pool leaves, or
    passes it."""
    if kind in ("equity-pool", "equity-pool-short"):
        size = book.granted + draw(st.integers(1, 10**6)) if kind == "equity-pool" else draw(st.integers(1, book.granted - 1))
        share_class = draw(st.sampled_from(SHARE_CLASSES)) if book.granted == 0 else book.pool[1]  # type: ignore[index]
        if kind == "equity-pool":
            book.pool = (size, share_class)
        return {"type": "option_pool", **base, "size": str(size), "class": share_class}
    left = book.pool[0] - book.granted if book.pool else 0
    doc = {"type": "option_grant", **base, "person": draw(st.sampled_from(SUBSCRIBERS)), "number": "0",
           "class": book.pool[1] if book.pool else draw(st.sampled_from(SHARE_CLASSES))}
    vests_now = draw(st.booleans())
    if not vests_now:  # a cliff of ten years: nothing vests by any date drawn
        doc["cliff_months"], doc["vesting_months"] = 120, 120
    price = Decimal(draw(st.sampled_from(["0.10", "1", "2.5", "0.012345"])))
    doc["exercise_price"] = format(price, "f")
    if kind == "equity-grant":
        number = draw(st.integers(1, min(left, 10**6)))
        book.granted += number
        book.grants[base["id"]] = [number, vests_now, price, doc["person"], doc["class"]]
    else:
        number = left + draw(st.integers(1, 1000))
    doc["number"] = str(number)
    return doc


def _option_event(draw: st.DrawFn, base: dict[str, Any], kind: str, book: _Book) -> dict[str, Any]:
    """A forfeiture or expiry of a recorded grant (README, "The option pool and grants"): at most what it has left, a
    forfeiture only of a grant nothing of which has vested; refused, one more than it has left, or a forfeiture of a
    grant vested on its day. The documents come in date order, so every grant is dated on or before it."""
    candidates = [g for g, (n, vests_now, *_) in book.grants.items()
                  if n > 0 and (kind != "equity-forfeiture" or not vests_now) and (kind != "equity-forfeit-vested" or vests_now)]
    grant = draw(st.sampled_from(sorted(candidates)))
    left = book.grants[grant][0]
    if kind in ("equity-forfeiture", "equity-expiry"):
        number = draw(st.integers(1, left))
        book.grants[grant][0] -= number
        book.granted -= number  # back in the pool from this day
    elif kind == "equity-event-too-many":
        number = left + draw(st.integers(1, 1000))
    else:
        number = draw(st.integers(1, left))
    event_kind = "forfeiture" if kind in ("equity-forfeiture", "equity-forfeit-vested") else draw(
        st.sampled_from(["forfeiture", "expiry"]) if kind == "equity-event-too-many" else st.just("expiry"))
    return {"type": "option_event", **base, "grant": grant, "kind": event_kind, "number": str(number)}


@st.composite
def _round(draw: st.DrawFn, when: st.SearchStrategy[date], book: _Book) -> dict[str, Any]:
    """A priced round to model (README, "Rounds"): a price or a pre-money valuation, one or two investors, the SAFEs not
    converted, and sometimes a top-up before or after the money."""
    spec: dict[str, Any] = {"on": draw(when).isoformat()}
    if draw(st.booleans()):
        spec["price"] = draw(st.sampled_from(["0.10", "1", "3.00", "2.375", "0.000123"]))
    else:
        spec["pre_money"] = _text(Decimal(draw(st.integers(1, 10**9))), 2)
        spec["price_decimals"] = draw(st.integers(0, 6))
    investors = draw(st.lists(st.sampled_from(SUBSCRIBERS), min_size=1, max_size=2, unique=True))
    spec["investors"] = [{"person": p, "amount": _text(draw(_cents(2, 7)), 2)} for p in investors]
    open_safes = sorted(s for s in book.safes if s not in book.converted)
    if open_safes and draw(st.booleans()):
        spec["instruments"] = draw(st.lists(st.sampled_from(open_safes), min_size=1, max_size=len(open_safes), unique=True))
    if draw(st.booleans()):
        spec["top_up"] = {"shares": str(draw(st.integers(1, 10**6))), "before": draw(st.booleans())}
    return spec


def _exercise(draw: st.DrawFn, base: dict[str, Any], kind: str, book: _Book, day: date) -> dict[str, Any]:
    """An exercise of a grant's options (README, "The option pool and grants"): of a grant vested on its day, within
    what it has left and what its holder's monies pay for at its price; refused, one of a grant nothing of which has
    vested."""
    if kind == "equity-exercise-not-vested":
        grant = draw(st.sampled_from(sorted(g for g, (n, vests_now, *_) in book.grants.items() if n > 0 and not vests_now)))
        return {"type": "option_exercise", **base, "grant": grant, "shares": str(draw(st.integers(1, book.grants[grant][0])))}
    grant = draw(st.sampled_from(sorted(g for g, (n, vests_now, price, holder, _) in book.grants.items()
                                        if n > 0 and vests_now and book.monies.get(holder, Decimal(0)) >= price)))
    left, _, price, holder, share_class = book.grants[grant]
    paid = book.monies[holder]
    shares = draw(st.integers(1, max(1, min(left, int(money.EXACT.divide_int(paid, price)), 10**6))))
    while shares > 1 and money.rnd_line(Decimal(shares), price, 2) > paid:  # RND-LINE may round a cent above it
        shares -= 1
    book.grants[grant][0] -= shares
    book.monies[holder] = money.EXACT.subtract(paid, money.rnd_line(Decimal(shares), price, 2))
    book.shares[(holder, share_class)] = book.shares.get((holder, share_class), 0) + shares
    book.issued.append((day, shares))
    return {"type": "option_exercise", **base, "grant": grant, "shares": str(shares)}


def _safe(draw: st.DrawFn, base: dict[str, Any], kind: str, book: _Book) -> dict[str, Any]:
    """A SAFE recorded (README, "SAFEs and their conversion"): a form, whole dollars, a cap at least 20 times its amount
    (so the post-money SAFEs never take the company), a discount on some, and 2 to 6 decimals; or, refused, a capped
    form with no cap, a discount of 100, a class that is not share capital, 7 decimals or a stranger."""
    form = draw(st.sampled_from(SAFE_FORMS))
    amount = Decimal(draw(st.integers(1000, 500000)))
    terms: dict[str, Any] = {"holder": draw(st.sampled_from(SUBSCRIBERS)), "form": form, "amount": amount, "cap": None,
                             "discount": None, "class": draw(st.sampled_from(SHARE_CLASSES)),
                             "decimals": draw(st.integers(2, 6))}
    if form in ("post_money_safe", "pre_money_safe"):
        terms["cap"] = Decimal(draw(st.integers(int(amount) * 20, 10**11)))
    if form == "discount_safe" or (form != "mfn_safe" and draw(st.booleans())):
        terms["discount"] = Decimal(draw(st.sampled_from(["10", "15", "20", "12.5", "33.33"])))
    doc = {"type": "instrument", **base, "person": terms["holder"], "form": form, "amount": _text(amount, 2),
           "class": terms["class"], "price_decimals": terms["decimals"]}
    if terms["cap"] is not None:
        doc["valuation_cap"] = format(terms["cap"], "f")
    if terms["discount"] is not None:
        doc["discount"] = format(terms["discount"], "f")
    if kind == "equity-safe-bad":
        broken = draw(st.sampled_from(["no-cap", "discount", "class", "decimals", "stranger"]))
        if broken == "no-cap":
            doc["form"] = draw(st.sampled_from(["post_money_safe", "pre_money_safe"]))
            doc.pop("valuation_cap", None)
        elif broken == "discount":
            doc["discount"] = "100"
        elif broken == "class":
            doc["class"] = draw(st.sampled_from([MONIES, "1010", "4010"]))
        elif broken == "decimals":
            doc["price_decimals"] = 7
        else:
            doc["person"] = "P0009"
        return doc
    book.safes[base["id"]] = terms
    return doc


def _conversion(draw: st.DrawFn, book: _Book, day: date,
                converting: list[str]) -> tuple[str, list[tuple[str, int]]] | None:
    """A round to convert every funded SAFE at, and the shares each takes (README, "SAFEs and their conversion"): None
    when no posted cash issue has shares issued before its day, or when the conversion would be refused (a price cut to
    nothing, or no whole share). The prices are worked as the README states them, so the generator knows the shares."""
    rounds = [(rid, d, p) for rid, d, p in book.rounds if d <= day and sum(n for when, n in book.issued if when < d) > 0]
    if not rounds:
        return None
    round_id, when, price = draw(st.sampled_from(rounds))
    before = sum(n for d, n in book.issued if d < when)
    terms = [book.safes[sid] for sid in converting]
    part = sum((Fraction(t["amount"]) / Fraction(t["cap"]) for t in terms if t["form"] == "post_money_safe"), Fraction(0))
    allotted = []
    for sid, t in zip(converting, terms, strict=True):
        prices = [Fraction(price) * (100 - Fraction(t["discount"] or 0)) / 100]
        if t["form"] == "post_money_safe":
            prices.append(Fraction(t["cap"]) * (1 - part) / before)
        elif t["form"] == "pre_money_safe":
            prices.append(Fraction(t["cap"]) / before)
        lowest, scale = min(prices), 10 ** t["decimals"]
        cut = Decimal(lowest.numerator * scale // lowest.denominator).scaleb(-t["decimals"], context=money.EXACT)
        if cut <= 0:
            return None
        shares = money.rnd_share(t["amount"], cut, 2)[0]
        if shares == 0:
            return None
        allotted.append((sid, shares))
    return round_id, allotted


def _revalue(draw: st.DrawFn, accounts: list[dict[str, Any]], settings: dict[str, Any]) -> None:
    """Mark the revalued accounts and name the unrealised FX account (README, "Month-end revaluation")."""
    flagged = set(REVALUED)
    if draw(st.booleans()):
        flagged |= {"1010", "1011"}  # base-currency money accounts: marked, but nothing of theirs is foreign
    if draw(st.integers(0, 4)) == 0:
        flagged.discard(draw(st.sampled_from(sorted(flagged))))
    for account in accounts:
        if account["code"] in flagged:
            account["revalue"] = True
    accounts.append({"code": UNREALISED_FX, "type": "expense", "group": False, "active": True})
    settings["accounts"]["unrealised_fx"] = UNREALISED_FX


@st.composite
def _period_end_rates(draw: st.DrawFn, first: date, as_of: date) -> list[dict[str, Any]]:
    """Each foreign currency's rate at each month's end from ``first``'s month to ``as_of``'s, as a stored rate or a
    quote (``RND-RATE``); one in twelve is left out, so its month waits if the currency has an open item."""
    rows = []
    year, month = first.year, first.month
    while (year, month) <= (as_of.year, as_of.month):
        end = date(year + month // 12, month % 12 + 1, 1) - timedelta(days=1)
        for currency in ("USD", "JPY", "KWD"):
            if draw(st.integers(0, 11)) == 0:
                continue
            row: dict[str, Any] = {"date": end.isoformat(), "currency": currency}
            form = draw(st.sampled_from(RATE_FORMS))
            if form == "fx_rate":
                row["fx_rate"] = draw(_rate())
            else:  # every quote _rate() draws (0.01 to 100) stores a valid rate in each form
                row["quote"], row["form"] = draw(_rate()), form
            row["fx_rate_source"] = "period end"
            rows.append(row)
        year, month = (year + 1, 1) if month == 12 else (year, month + 1)
    return rows


@st.composite
def _fx_document(draw: st.DrawFn, doc_id: str, day: date, kind: str, book: _Book) -> tuple[dict[str, Any], str]:
    """The ``fx`` source's own kinds (README, "Month-end revaluation")."""
    base = {"id": doc_id, "version": 1, "date": day.isoformat()}
    if kind == "fx-pay-from-base-unapplied":
        foreign = [b for b, (cur, left) in sorted(book.open.items()) if cur != "SGD" and left > 0]
        if not foreign:
            return {"type": "bill_payment", **base, "account": "1010", "currency": "USD", "base_amount": "1.00",
                    "parts": [{"document": "BILL-NONE", "amount": "1.00"}]}, REJECT
        target = draw(st.sampled_from(foreign))
        currency, left = book.open[target]
        minor = CURRENCIES[currency]
        unit = Decimal(1).scaleb(-minor, context=money.EXACT)
        amount = left if draw(st.booleans()) or left <= unit else draw(_split(left, 2, minor))[0]
        unapplied = draw(_cents(minor, 3))
        # What the SGD account paid: the total near a drawn rate, at least a cent (the rate it implies, RND-RATE).
        paid = money.rnd_fx(money.EXACT.add(amount, unapplied), Decimal(draw(_rate())), 2)
        paid = max(paid, Decimal("0.01"))
        book.open[target] = (currency, money.EXACT.subtract(left, amount))
        return {"type": "bill_payment", **base, "account": draw(st.sampled_from(["1010", "1011"])),
                "currency": currency, "base_amount": _text(paid, 2),
                "parts": [{"document": target, "amount": _text(amount, minor)}],
                "unapplied": _text(unapplied, minor)}, POST
    if kind == "fx-wallet-in" or kind == "fx-wallet-out" and not any(book.wallets.values()):
        # Into a foreign wallet from SGD: what it received at a drawn rate, paid in SGD at that rate (a cent at least).
        wallet = draw(st.sampled_from(["1021", "1022", "1023"]))
        currency = MONEY_ACCOUNTS[wallet]
        minor = CURRENCIES[currency]
        received, rate = draw(_cents(minor, 6)), draw(_rate())
        paid = max(money.rnd_fx(received, Decimal(rate), 2), Decimal("0.01"))
        book.wallets[wallet] = money.EXACT.add(book.wallets.get(wallet, Decimal(0)), received)
        return {"type": "bank_transfer", **base,
                "from": {"account": draw(st.sampled_from(["1010", "1011"])), "currency": "SGD", "amount": _text(paid, 2)},
                "to": {"account": wallet, "currency": currency, "amount": _text(received, minor), "fx_rate": rate,
                       "fx_rate_source": "document"}}, POST
    if kind == "fx-wallet-out":
        # Out again, all the fx source put in, at another drawn rate: the wallet's foreign amount goes back to what the
        # other documents left there, while its base amount keeps the difference of the two rates.
        wallet = draw(st.sampled_from(sorted(w for w, left in book.wallets.items() if left > 0)))
        currency = MONEY_ACCOUNTS[wallet]
        minor = CURRENCIES[currency]
        paid, rate = book.wallets[wallet], draw(_rate())
        received = max(money.rnd_fx(paid, Decimal(rate), 2), Decimal("0.01"))
        book.wallets[wallet] = Decimal(0)
        return {"type": "bank_transfer", **base,
                "from": {"account": wallet, "currency": currency, "amount": _text(paid, minor), "fx_rate": rate,
                         "fx_rate_source": "document"},
                "to": {"account": draw(st.sampled_from(["1010", "1011"])), "currency": "SGD",
                       "amount": _text(received, 2)}}, POST
    # A manual journal moving a revalued account in another currency: an item no document supports on 1100 or 2010.
    account = draw(st.sampled_from(REVALUED))
    currency = MONEY_ACCOUNTS.get(account) or draw(st.sampled_from(["USD", "JPY", "KWD"]))
    minor = CURRENCIES[currency]
    amount = money.EXACT.multiply(draw(_cents(minor, 6)), Decimal(draw(st.sampled_from([1, -1]))))
    rate = draw(_rate())
    in_base = money.rnd_fx(amount, Decimal(rate), 2)
    other = money.negate(in_base)
    if kind == "fx-journal-unbalanced":
        other = money.EXACT.add(other, Decimal("0.01"))
    meant = POST if kind == "fx-journal" and not in_base.is_zero() else REJECT  # a line of S$0.00 is refused
    lines = [{"account": account, "currency": currency, "amount": _text(amount, minor), "fx_rate": rate,
              "fx_rate_source": "document"},
             {"account": draw(st.sampled_from(["2999", "4010", "6010"])), "currency": "SGD", "amount": _text(other, 2)}]
    return {"type": "manual_journal", **base, "memo": "", "lines": lines}, meant


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
    ledger = "general-ledger" in sources
    if "founders" in sources or ledger:  # README, "Settings and as_of": the books start on or before the first date
        month = days[0].year * 12 + days[0].month - 1 - draw(st.integers(0, 3))  # the first's month, or up to 3 before
        book.books_start = date(month // 12, month % 12 + 1, 1)
        settings["books_start"] = book.books_start.isoformat()
        settings["accounts"].update({key: code for key, (code, _) in DIRECTOR_ACCOUNTS.items()})
        accounts += _founders_accounts()
    if "opening-items" in sources:  # README, "Opening open items": the books start after the items' own dates
        if book.books_start is None:
            month = days[0].year * 12 + days[0].month - 1 - draw(st.integers(0, 3))
            book.books_start = date(month // 12, month % 12 + 1, 1)
            settings["books_start"] = book.books_start.isoformat()
        settings["accounts"]["opening_clearing"] = OPENING_CLEARING
        accounts.append({"code": OPENING_CLEARING, "type": "equity", "group": False, "active": True})
    if "fx" in sources:
        _revalue(draw, accounts, settings)
    seeded: list[str] = []  # the equity source's first kinds: money in, then issues, so later ones find shares held
    if "equity" in sources:
        _equity_accounts(draw, accounts)
        if "founders" in sources or "general-ledger" in sources:
            book.holders += PEOPLE  # the directors and shareholders may buy shares too
        start = draw(st.integers(0, 4))
        if start == 4:  # a pool, a grant, and its exercise once its holder's money is in (README, "The option pool and grants")
            seeded = ["equity-money-in", "equity-issue", "equity-pool", "equity-grant", "equity-exercise", "equity-exercise"][:len(days) - 1]
        elif start == 3:  # a SAFE recorded and paid in, then a round it converts at (README, "SAFEs and their conversion")
            seeded = ["equity-money-in", "equity-issue", "equity-safe", "equity-safe-money", "equity-money-in",
                      "equity-issue", "equity-safe-conversion"][:len(days) - 1]
        elif start > 0:
            seeded = ["equity-money-in", "equity-issue", "equity-money-in", "equity-issue"][:len(days) - 1]
    tracked: list[strategies_gl.Tracked] = []  # the general ledger's journals and opening balances
    dims = None  # the dimensions declared (FR-DIM-01), in a third of the general ledger's scenarios
    if ledger:
        accounts += strategies_gl.accounts()
        assert book.books_start is not None
        opening = draw(st.integers(0, 3)) > 0
        if draw(st.integers(0, 2)) == 0:
            dims = draw(strategies_gl.dimensions())
            accounts += strategies_gl.dimension_accounts(dims)
            kinds = kinds + strategies_gl.dimension_kinds(dims)
        if opening:  # the opening balances, first, at the books' start (FR-GL-06)
            kind = draw(st.sampled_from(strategies_gl.opening_kinds(dims)))
            document, meant, credits = draw(strategies_gl.opening_balances("OB-0001", book.books_start, kind, dims))
            documents.append(document)
            intent["OB-0001"], built["OB-0001"] = meant, kind
            tracked.append(strategies_gl.Tracked("OB-0001", "opening_balances", meant == POST, 1, book.books_start))
            for person, account, owed in credits if meant == POST else []:
                book.credit((person, account), owed)  # what the company owes them from the books' start
    for i, day in enumerate(days):
        kind = seeded[i] if i < len(seeded) else draw(st.sampled_from(kinds))
        doc_id = f"P1-{i + 1:04d}"
        if kind.startswith("opening-"):
            document, meant = draw(_opening_document(doc_id, kind, book))
        elif kind in KINDS["bank"]:
            document, meant = draw(_bank_document(doc_id, day, kind)), REJECT if kind in REJECTED_KINDS else POST
        elif kind in KINDS["purchases"]:
            document, meant = draw(_purchase_document(doc_id, day, kind, book))
        elif kind in KINDS["sales"]:
            document, meant = draw(_sales_document(doc_id, day, kind, book))
        elif kind in KINDS["claims"]:
            document, meant = draw(_claims_document(doc_id, day, kind, book))
        elif kind in KINDS["assets"]:
            document, meant = draw(_assets_document(doc_id, day, kind, book))
        elif kind in FX_OWN:
            document, meant = draw(_fx_document(doc_id, day, kind, book))
        elif kind in strategies_gl.DOCUMENT_DIMENSION:
            assert dims is not None
            document, meant = draw(strategies_gl.dimension_document(doc_id, day, dims))
        elif kind in KINDS["equity"]:
            document, meant, kind = draw(_equity_document(doc_id, day, kind, book))
        elif kind in KINDS["general-ledger"] or kind in strategies_gl.DIMENSION_KINDS:
            document, meant, kind = draw(strategies_gl.journal(doc_id, day, kind, dims))
            tracked.append(strategies_gl.Tracked(doc_id, "manual_journal", meant == POST, 1, day))
        else:
            document, meant, kind = draw(_founders_document(doc_id, day, kind, book))
        documents.append(document)
        intent[doc_id] = meant
        built[doc_id] = kind
    reversals, corrections = draw(strategies_gl.corrections(tracked, book.books_start, dims)) if ledger else ([], [])
    last = max(date.fromisoformat(d["date"]) for d in documents)
    last = max(last, strategies_gl.corrections_last_day(reversals) or last)
    as_of = last + timedelta(days=draw(st.integers(0, 400)))
    if "fx" in sources:
        settings["period_end_rates"] = draw(_period_end_rates(min(days[0], book.books_start or days[0]), as_of))
    scenario = {
        "schema": SCENARIO_SCHEMA,
        "name": name,
        "base_currency": "SGD",
        "currencies": dict(CURRENCIES),
        "as_of": as_of.isoformat(),
        "settings": settings,
        "accounts": accounts,
        "documents": documents,
        "reversals": reversals,
    }
    if "founders" in sources or ledger:
        scenario["people"] = list(PEOPLE)
    if dims is not None:
        scenario["dimensions"] = dims.declared
    if "equity" in sources:  # README, "Cap tables": a few days, from before the first document to as_of
        scenario["subscribers"] = list(SUBSCRIBERS)
        when = st.one_of(st.sampled_from([*days, as_of]), st.dates(days[0] - timedelta(days=30), as_of))
        days_asked = draw(st.lists(when, min_size=1, max_size=3, unique=True))
        scenario["cap_tables"] = [d.isoformat() for d in days_asked]
        scenario["rounds"] = [draw(_round(when, book)) for _ in range(draw(st.integers(0, 2)))] or None  # README, "Rounds"
        if scenario["rounds"] is None:
            del scenario["rounds"]
    return scenario, Intent(intent, built, tuple(corrections))


def p1_scenarios(sources: tuple[str, ...] = SOURCES, name: str = "generated-p1") -> st.SearchStrategy[dict[str, Any]]:
    return p1_scenarios_with_intent(sources, name).map(lambda pair: pair[0])
