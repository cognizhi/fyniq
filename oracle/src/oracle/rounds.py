"""Rounds modelled (FR-EQ-21), written from PRD-001 FR-EQ-21, Appendix G.3 and the equity register's design D8, as
test/differential/README.md, "Rounds", states them.

A round dilutes the cap table at the end of the day before it (:func:`oracle.captable.table`): it is priced by its
price per share, or by a pre-money valuation over the shares fully diluted before; each investor buys ``RND-SHARE`` of
their money at the price; each instrument named converts as a ``safe_conversion`` at the round would; and each holder is
shown before and after, with the points fully diluted they lose. Modelling posts nothing, and a round that cannot be
worked out is ``None``.
"""

from __future__ import annotations

from datetime import date, timedelta
from decimal import Decimal
from fractions import Fraction
from typing import Any

from oracle import captable, money
from oracle.scenario import Scenario
from oracle.sources import equity

_PLAIN_PRICE_DECIMALS = 6


def _plain(value: Decimal) -> str:
    return format(value.normalize(money.EXACT), "f") if not value.is_zero() else "0"


def _points(x: Fraction) -> str:
    """x to 2 decimals, half away from zero."""
    sign = -1 if x < 0 else 1
    scaled = abs(x) * 100
    n = int(scaled + Fraction(1, 2))  # floor of |x| × 100 + ½
    value = Decimal(sign * n).scaleb(-2)
    return format(value, "f") if n else "0.00"


def _price(spec: dict[str, Any], priced: int) -> Decimal | None:
    if "price" in spec:
        text = spec["price"]
        whole, _, tail = text.partition(".")
        if not whole.isdigit() or (tail and not tail.isdigit()) or len(tail) > _PLAIN_PRICE_DECIMALS or Decimal(text) <= 0:
            return None
        return Decimal(text)
    if priced <= 0:
        return None
    exact = Fraction(Decimal(spec["pre_money"])) / priced
    scale = 10 ** spec["price_decimals"]
    cut = exact.numerator * scale // exact.denominator  # toward zero
    if cut <= 0:
        return None
    return Decimal(cut).scaleb(-spec["price_decimals"], context=money.EXACT)


def model(scenario: Scenario, books: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any] | None:
    """The outcome of a round against ``books``, or None when it cannot be worked out (README, "Rounds")."""
    minor = scenario.currencies[scenario.base_currency]
    on = date.fromisoformat(spec["on"])
    before = captable.table(scenario, books, on - timedelta(days=1))
    issued_before, diluted_before = int(before["issued"]), int(before["fully_diluted"])
    unallocated_before = int(before["unallocated"])
    top_up = spec.get("top_up")
    top = 0
    if top_up is not None:
        if not top_up["shares"].isdigit() or int(top_up["shares"]) <= 0:
            return None
        top = int(top_up["shares"])
    priced = diluted_before + (top if top_up and top_up["before"] else 0)
    price = _price(spec, priced)
    if price is None:
        return None
    holders_known = scenario.people | scenario.subscribers
    gained: dict[str, int] = {}
    issues = []
    seen: set[str] = set()
    for inv in spec["investors"]:
        person = inv["person"]
        if person not in holders_known or person in seen:
            return None
        seen.add(person)
        amount = money.parse_amount(inv["amount"], minor)
        if amount <= 0:
            return None
        shares, cost, left = money.rnd_share(amount, price, minor)
        if shares == 0:
            return None
        issues.append({"person": person, "shares": str(shares), "amount": money.fmt(cost, minor), "left": money.fmt(left, minor)})
        gained[person] = gained.get(person, 0) + shares
    named = spec.get("instruments", [])
    recorded = equity.recorded(books)
    done = books.get("register/converted", set())
    if len(set(named)) != len(named) or any(i not in recorded or i in done for i in named):
        return None
    conversions = []
    if named:
        if issued_before == 0:
            return None
        safes = [(i, recorded[i]) for i in named]
        part = sum((Fraction(s.amount) / Fraction(s.cap) for _, s in safes if s.form == "post_money_safe" and s.cap), Fraction(0))
        if part >= 1:
            return None
        for sid, safe in safes:
            p = equity._price(safe, price, issued_before, part)
            if p <= 0:
                return None
            shares, converted, left = money.rnd_share(safe.amount, p, minor)
            if shares == 0:
                return None
            conversions.append({"instrument": sid, "price": _plain(p), "shares": str(shares), "converted": money.fmt(converted, minor),
                                "left": money.fmt(left, minor)})
            gained[safe.holder] = gained.get(safe.holder, 0) + shares
    new_shares = sum(gained.values())
    issued_after, diluted_after = issued_before + new_shares, diluted_before + new_shares + top
    rows: dict[str, tuple[int, int]] = {h["holder"]: (int(h["issued"]), int(h["fully_diluted"])) for h in before["holders"]}
    for person in gained:
        rows.setdefault(person, (0, 0))

    def share(part: int, total: int) -> str | None:
        return captable._share(Decimal(part), Decimal(total))

    holders = []
    for holder in sorted(rows):
        ib, db = rows[holder]
        n = gained.get(holder, 0)
        ia, da = ib + n, db + n
        dilution = None
        if diluted_before > 0 and diluted_after > 0:
            dilution = _points((Fraction(db, diluted_before) - Fraction(da, diluted_after)) * 100)
        pro_rata = new_shares * db // diluted_before if diluted_before > 0 else 0
        holders.append({"holder": holder, "issued_before": str(ib), "issued_after": str(ia), "fully_diluted_before": str(db),
                        "fully_diluted_after": str(da), "ownership_before": share(ib, issued_before), "ownership_after": share(ia, issued_after),
                        "diluted_before": share(db, diluted_before), "diluted_after": share(da, diluted_after), "dilution": dilution,
                        "pro_rata": str(pro_rata)})

    def valued(shares: int) -> str:
        if shares <= 0:
            return money.fmt(Decimal(0), minor)
        return money.fmt(money.rnd_line(Decimal(shares), price, minor), minor)

    return {"price": _plain(price), "pre_money": valued(priced), "post_money": valued(diluted_after), "issues": issues,
            "conversions": conversions, "holders": holders, "issued_before": str(issued_before), "issued_after": str(issued_after),
            "fully_diluted_before": str(diluted_before), "fully_diluted_after": str(diluted_after),
            "unallocated_before": str(unallocated_before), "unallocated_after": str(unallocated_before + top), "new_shares": str(new_shares)}
