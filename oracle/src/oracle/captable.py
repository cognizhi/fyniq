"""The cap table at the end of a day (FR-EQ-20), written from PRD-001 §8.23 (FR-EQ-17, FR-EQ-20), the equity register's
design D3 and D7 and Appendix G.3, as test/differential/README.md, "Cap tables" and "Share transfers", states them.

The register is every movement of a holding: a holder's shares of a class, into the holding when positive and out of
it when negative, on a day. A document adds its movements once it posts (:attr:`oracle.book.Posting.effects`), so a
rejected one moves nothing. The cap table reads the register alone: it is never written, so it cannot be edited.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.scenario import Scenario

# The register's key in ``books``, which no document ID can take: an ID never has a "/".
REGISTER = "register/holdings"


@dataclass(frozen=True)
class Movement:
    holder: str
    share_class: str  # the share-capital account: the format's class
    shares: int
    day: date


def movements(books: dict[str, Any]) -> list[Movement]:
    return books.setdefault(REGISTER, [])


def move(books: dict[str, Any], holder: str, share_class: str, shares: int, day: date) -> Callable[[], None]:
    """The effect that records one movement, run once its document posts."""
    return lambda: movements(books).append(Movement(holder, share_class, shares, day))


def lowest_from(books: dict[str, Any], holder: str, share_class: str, day: date, change: int) -> int:
    """The least the holding holds at the end of any day from ``day`` on, with ``change`` moved on ``day``."""
    theirs = [m for m in movements(books) if m.holder == holder and m.share_class == share_class]
    days = {day} | {m.day for m in theirs if m.day > day}
    return min(sum(m.shares for m in theirs if m.day <= d) + change for d in days)


def _plain(value: Decimal) -> str:
    """A plain decimal with no trailing zeros and no exponent: "1000000", "1.5", "0"."""
    return format(value.normalize(money.EXACT), "f") if not value.is_zero() else "0"


def _share(part: Decimal, total: Decimal) -> str | None:
    """part as a percentage of total to 2 decimals, half-up from the exact quotient; None for a total of zero."""
    if total.is_zero():
        return None
    return format(money.round_quotient(money.EXACT.multiply(part, money.HUNDRED), total, 2), "f")


def _figures(issued: Decimal, votes: Decimal, diluted: Decimal, totals: tuple[Decimal, Decimal, Decimal]) -> dict[str, Any]:
    return {
        "issued": _plain(issued),
        "votes": _plain(votes),
        "fully_diluted": _plain(diluted),
        "ownership": _share(issued, totals[0]),
        "voting": _share(votes, totals[1]),
        "diluted": _share(diluted, totals[2]),
    }


def table(scenario: Scenario, books: dict[str, Any], day: date) -> dict[str, Any]:
    """The cap table at the end of ``day``: each holding with shares, by holder then class, and each holder's."""
    held: dict[tuple[str, str], int] = {}
    for m in movements(books):
        if m.day <= day:
            held[(m.holder, m.share_class)] = held.get((m.holder, m.share_class), 0) + m.shares
    lines = []
    for (holder, share_class), shares in sorted(held.items()):
        if shares == 0:
            continue
        issued = Decimal(shares)
        votes = money.EXACT.multiply(issued, scenario.accounts[share_class].votes_per_share)
        lines.append((holder, share_class, issued, votes, issued))  # fully diluted: the format has nothing dilutive
    totals = (money.total([ln[2] for ln in lines]), money.total([ln[3] for ln in lines]),
              money.total([ln[4] for ln in lines]))
    by_holder: dict[str, list[Decimal]] = {}
    for holder, _, issued, votes, diluted in lines:
        sums = by_holder.setdefault(holder, [Decimal(0), Decimal(0), Decimal(0)])
        for k, value in enumerate((issued, votes, diluted)):
            sums[k] = money.EXACT.add(sums[k], value)
    return {
        "at": day.isoformat(),
        "issued": _plain(totals[0]),
        "votes": _plain(totals[1]),
        "fully_diluted": _plain(totals[2]),
        "lines": [{"holder": holder, "class": share_class, **_figures(issued, votes, diluted, totals)}
                  for holder, share_class, issued, votes, diluted in lines],
        "holders": [{"holder": holder, **_figures(*sums, totals)} for holder, sums in sorted(by_holder.items())],
    }
