"""A cash issue of shares made effective from share application monies (``share_issue_cash@1``), from PRD-001 FR-EQ-17,
Appendix B.7 ("Share issue for cash", "Cash received before the allotment takes effect") and rev 0.205.

The document is the issue as the register makes it effective: the day ACRA's register of members shows the allotment
(``date``), its one subscriber (``person``, one of the scenario's ``people`` or ``subscribers``), the class's
share-capital account (``class``), the whole number of shares and the price per share in the base currency. Its amount is
``RND-LINE`` of shares x price.

The money came in earlier, to share application monies, the one account of role ``share-application-monies`` (a bank
line naming the subscriber, :mod:`oracle.sources.bank`). The issue posts Dr share application monies with the amount,
the line naming the subscriber, and Cr ``class`` with the same. It is never made effective unpaid: what the
subscriber's monies hold at the end of ``date`` is at least the amount, or it is rejected (test/differential/README.md,
"Cash share issues").

Once posted, the issue moves ``shares`` into the subscriber's holding of ``class`` on ``date``; a transfer
(:func:`share_transfer`) moves shares between holders and posts nothing (README, "Share transfers" and "Cap tables").

A SAFE (:func:`instrument`) is recorded and posts nothing; its money comes in on a bank line naming it on the
instruments account (2700, role ``convertible-instruments``). At a priced round, a conversion (:func:`safe_conversion`)
converts every SAFE recorded and not converted yet, from FR-EQ-18, ``RND-SHARE``, Appendix G.3 (G4-044) and the equity
register's design D6, as README "SAFEs and their conversion" states them: the conversion price is the lower of the
cap price and the discounted round price, cut down to the SAFE's decimals, worked exactly in fractions.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import date, timedelta
from decimal import Decimal
from fractions import Fraction
from typing import Any

from oracle import captable, money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source
from oracle.sources.founders import owed

_WHOLE_SHARES = re.compile(r"[1-9][0-9]{0,13}")
# A valuation cap or a discount as the product keeps them: a plain decimal, at most 12 integer digits and 8 decimals.
_TERM = re.compile(r"[0-9]{1,12}(\.[0-9]{1,8})?")
_FORMS = ("post_money_safe", "pre_money_safe", "discount_safe", "mfn_safe")
_CAPPED = ("post_money_safe", "pre_money_safe")
INSTRUMENTS = "convertible-instruments"
# The register of recorded SAFEs and of those converted, in ``books``: an ID never has a "/".
_RECORDED = "register/instruments"
_CONVERTED = "register/converted"


@dataclass(frozen=True)
class Round:
    """A cash issue as a priced round reads it: its price per share and the day it took effect."""

    price: Decimal
    day: date


@dataclass(frozen=True)
class Safe:
    holder: str
    form: str
    amount: Decimal
    cap: Decimal | None  # a capped form's; a cap on another form is kept and prices nothing
    discount: Decimal | None
    share_class: str
    decimals: int


def recorded(books: dict[str, Any]) -> dict[str, Safe]:
    """Every SAFE recorded so far, by its document's ID, in the order recorded."""
    return books.setdefault(_RECORDED, {})


def _converted(books: dict[str, Any]) -> set[str]:
    return books.setdefault(_CONVERTED, set())


def _term(text: str, below: Decimal | None) -> Decimal:
    if not _TERM.fullmatch(text):
        raise Rejected("bad-term")
    value = Decimal(text)
    if value <= 0 or (below is not None and value >= below):
        raise Rejected("bad-term")
    return value


def _share_class(r: Rules, code: str) -> str:
    share_class = r.scenario.accounts.get(code)
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    return code


def share_issue_cash(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people | r.scenario.subscribers:
        raise Rejected("not-a-subscriber")
    monies = [code for code, a in r.scenario.accounts.items()
              if a.role == "share-application-monies" and a.active and not a.group]
    if len(monies) != 1:
        raise Rejected("no-share-application-monies")
    share_class = r.scenario.accounts.get(d["class"])
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    if not _WHOLE_SHARES.fullmatch(d["shares"]):
        raise Rejected("bad-shares")
    price = r.quantity(d["price"])
    amount = money.rnd_line(r.quantity(d["shares"]), price, r.minor(r.base))
    if amount <= 0:
        raise Rejected("nothing-issued")
    if owed(r, person, monies[0], doc.date) < amount:  # the credit the subscriber's monies hold that day
        raise Rejected("unpaid")
    return Posting([
        r.line(monies[0], r.base, amount, amount, party=person),
        r.base_line(d["class"], money.negate(amount)),
    ], facts=Round(price, doc.date), effects=[captable.move(books, person, d["class"], int(d["shares"]), doc.date)])


def share_transfer(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """``shares`` of ``class`` from ``seller`` to ``buyer`` on ``date``, the day ACRA's register shows it (FR-EQ-17).

    It posts no entry. It is refused when the seller's holding of ``class`` would fall below zero at the end of any day
    from ``date`` on, as the database refuses "a holding below zero on any day" (FY288).
    """
    d = doc.data
    holders = r.scenario.people | r.scenario.subscribers
    if d["seller"] not in holders or d["buyer"] not in holders:
        raise Rejected("not-a-holder")
    if d["seller"] == d["buyer"]:
        raise Rejected("same-holder")
    share_class = r.scenario.accounts.get(d["class"])
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    if not _WHOLE_SHARES.fullmatch(d["shares"]):
        raise Rejected("bad-shares")
    shares = int(d["shares"])
    if captable.lowest_from(books, d["seller"], d["class"], doc.date, -shares) < 0:
        raise Rejected("more-than-held")
    return Posting([], effects=[captable.move(books, d["seller"], d["class"], -shares, doc.date),
                                captable.move(books, d["buyer"], d["class"], shares, doc.date)])


def instrument(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """A SAFE recorded on ``date``, the day it was signed (FR-EQ-18): it posts nothing, and once recorded a bank line
    may bring its money onto the instruments account and a conversion may convert it (README, "SAFEs and their
    conversion")."""
    d = doc.data
    if d["person"] not in r.scenario.people | r.scenario.subscribers:
        raise Rejected("not-a-holder")
    form = d["form"]
    if form not in _FORMS:
        raise Rejected("unknown-form")
    amount = r.amount(d["amount"], r.base, sign=1)
    cap = _term(d["valuation_cap"], None) if "valuation_cap" in d else None
    if form in _CAPPED and cap is None:
        raise Rejected("no-cap")
    discount = _term(d["discount"], money.HUNDRED) if "discount" in d else None
    if form == "discount_safe" and discount is None:
        raise Rejected("no-discount")
    decimals = d["price_decimals"]
    if not 0 <= decimals <= 6:
        raise Rejected("bad-precision")
    safe = Safe(d["person"], form, amount, cap if form in _CAPPED else None, discount, _share_class(r, d["class"]),
                decimals)
    return Posting([], effects=[lambda: recorded(books).__setitem__(doc.id, safe)])


def held(r: Rules, account: str, instrument_id: str, day: date) -> Decimal:
    """What the instruments ``account`` holds for a SAFE at the end of ``day``: the credit of the lines naming it there
    in the earlier documents' entries dated on or before ``day``, as :func:`oracle.sources.founders.owed` reads what
    is owed a person."""
    return money.negate(money.total([
        ln.base_amount
        for entry in r.posted if entry.posting_date <= day
        for ln in entry.lines if ln.account == account and ln.instrument == instrument_id
    ]))


def _price(safe: Safe, round_price: Decimal, before: int, part: Fraction) -> Decimal:
    """A SAFE's conversion price: the lower of its cap price and the discounted round price, cut down to its decimals.

    ``part`` is f, the part of the company the post-money SAFEs converting take; ``before`` is C₀."""
    prices = [Fraction(round_price) * (100 - Fraction(safe.discount or 0)) / 100]
    if safe.form == "post_money_safe":
        assert safe.cap is not None
        prices.append(Fraction(safe.cap) * (1 - part) / before)
    elif safe.form == "pre_money_safe":
        assert safe.cap is not None
        prices.append(Fraction(safe.cap) / before)
    lowest = min(prices)
    scale = 10 ** safe.decimals
    cut = lowest.numerator * scale // lowest.denominator  # toward zero: the price is above zero
    return Decimal(cut).scaleb(-safe.decimals, context=money.EXACT)


def safe_conversion(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """Every SAFE recorded and not converted yet, converted on ``date`` at the priced round ``round`` (FR-EQ-18,
    ``RND-SHARE``, G4-044; README, "SAFEs and their conversion")."""
    d = doc.data
    priced = books.get(d["round"])
    if not isinstance(priced, Round) or priced.day > doc.date:
        raise Rejected("not-a-round")
    accounts = [code for code, a in r.scenario.accounts.items() if a.role == INSTRUMENTS and a.active and not a.group]
    if len(accounts) != 1:
        raise Rejected("no-instruments-account")
    converting = [(sid, safe) for sid, safe in recorded(books).items() if sid not in _converted(books)]
    if not converting:
        raise Rejected("nothing-converts")
    before = captable.issued(books, priced.day - timedelta(days=1))  # C₀
    if before == 0:
        raise Rejected("no-shares-before")
    part = sum((Fraction(s.amount) / Fraction(s.cap) for _, s in converting if s.form == "post_money_safe" and s.cap),
               Fraction(0))
    if part >= 1:
        raise Rejected("safes-take-the-company")
    lines, effects = [], []
    for sid, safe in converting:
        price = _price(safe, priced.price, before, part)
        if price <= 0:
            raise Rejected("price-cut-to-nothing")
        shares, converted, _ = money.rnd_share(safe.amount, price, r.base_minor)
        if shares == 0:
            raise Rejected("no-whole-share")
        if held(r, accounts[0], sid, doc.date) < converted:
            raise Rejected("money-not-held")
        lines += [r.line(accounts[0], r.base, converted, converted, instrument=sid),
                  r.base_line(safe.share_class, money.negate(converted))]
        effects.append(captable.move(books, safe.holder, safe.share_class, shares, doc.date))
    effects.append(lambda: _converted(books).update(sid for sid, _ in converting))
    return Posting(lines, effects=effects)
