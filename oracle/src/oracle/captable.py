"""The cap table at the end of a day (FR-EQ-20), written from PRD-001 §8.23 (FR-EQ-17, FR-EQ-20), the equity register's
design D3 and D7 and Appendix G.3, as test/differential/README.md, "Cap tables" and "Share transfers", states them.

The register is every movement of a holding: a holder's shares of a class, into the holding when positive and out of
it when negative, on a day. A document adds its movements once it posts (:attr:`oracle.book.Posting.effects`), so a
rejected one moves nothing. The cap table reads the register alone: it is never written, so it cannot be edited.

The option pool (FR-EQ-19, design D9; README, "The option pool and grants") is two more registers: the resolutions,
each a size of a class from a day, and the grants, each a holder's options of a class from a day. Fully diluted, a
holding counts its holder's options too, and the day's total what the pool leaves to grant.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.scenario import Scenario

# The registers' keys in ``books``, which no document ID can take: an ID never has a "/".
REGISTER = "register/holdings"
POOLS = "register/pools"
GRANTS = "register/grants"
EVENTS = "register/grant-events"
EXERCISES = "register/grant-exercises"


@dataclass(frozen=True)
class Movement:
    holder: str
    share_class: str  # the share-capital account: the format's class
    shares: int
    day: date


@dataclass(frozen=True)
class Resolution:
    """A pool resolution as recorded: the shares set aside, of a class, from a day."""

    size: int
    share_class: str
    day: date


@dataclass(frozen=True)
class Grant:
    holder: str
    share_class: str
    number: int
    day: date
    cliff: int = 0  # months before anything vests
    months: int = 0  # months it vests over; 0 vests it all on its day
    id: str = ""
    price: Decimal = Decimal(0)  # the exercise price per share


@dataclass(frozen=True)
class Event:
    """A forfeiture or expiry: options a grant gives up from a day, back in the pool."""

    grant: str
    kind: str
    number: int
    day: date


def events(books: dict[str, Any]) -> list[Event]:
    return books.setdefault(EVENTS, [])


@dataclass(frozen=True)
class Exercise:
    """Options of a grant exercised into shares from a day."""

    grant: str
    number: int
    day: date


def exercises(books: dict[str, Any]) -> list[Exercise]:
    return books.setdefault(EXERCISES, [])


def exercise_fits(grant: Grant, taken: list[Event], done: list[Exercise], number: int, day: date) -> bool:
    """Whether ``number`` more options of ``grant`` exercised on ``day`` fit: on it and each later day an event or
    exercise of the grant is dated, those exercised by then are at most those vested then, and at most the grant's
    options less what its events took by then (README, "The option pool and grants")."""
    mine_taken = [e for e in taken if e.grant == grant.id]
    mine_done = [x for x in done if x.grant == grant.id]
    days = {day} | {e.day for e in mine_taken if e.day > day} | {x.day for x in mine_done if x.day > day}
    for d in sorted(days):
        exercised = sum(x.number for x in mine_done if x.day <= d) + number
        kept = grant.number - sum(e.number for e in mine_taken if e.day <= d)
        if exercised > min(vested(grant, d), kept):
            return False
    return True


def _months(since: date, day: date) -> int:
    """The whole months from ``since`` to ``day``: one passes on since's day of the month, or on the last day of a
    month too short to have it."""
    m = (day.year - since.year) * 12 + day.month - since.month
    last = (day + timedelta(days=1)).day == 1
    if day.day < since.day and not last:
        m -= 1
    return m


def vested(grant: Grant, day: date) -> int:
    """The options of ``grant`` vested at the end of ``day`` (README, "The option pool and grants")."""
    if day < grant.day:
        return 0
    m = _months(grant.day, day)
    if grant.months == 0 or m >= grant.months:
        return grant.number
    if m < grant.cliff:
        return 0
    return grant.number * m // grant.months


def pools(books: dict[str, Any]) -> list[Resolution]:
    return books.setdefault(POOLS, [])


def grants(books: dict[str, Any]) -> list[Grant]:
    return books.setdefault(GRANTS, [])


def pool_on(resolutions: list[Resolution], day: date) -> Resolution | None:
    """The resolution in force at the end of ``day``: the latest dated on or before it, of two on one day the later."""
    found = None
    for r in resolutions:
        if r.day <= day and (found is None or r.day >= found.day):
            found = r
    return found


def granted(given: list[Grant], day: date, taken: list[Event] = ()) -> int:  # type: ignore[assignment]
    """The options the grants dated by ``day`` granted, less those the events dated by then took back."""
    return sum(g.number for g in given if g.day <= day) - sum(e.number for e in taken if e.day <= day)


def short_from(resolutions: list[Resolution], given: list[Grant], since: date, taken: list[Event] = ()) -> bool:  # type: ignore[assignment]
    """Whether, on ``since`` or a later day a resolution or grant is dated, the options granted by then, less those
    the events took back, are more than the pool's size then (FY293)."""
    days = {since} | {r.day for r in resolutions if r.day >= since} | {g.day for g in given if g.day >= since}
    for d in sorted(days):
        pool = pool_on(resolutions, d)
        if granted(given, d, taken) > (pool.size if pool else 0):
            return True
    return False


def movements(books: dict[str, Any]) -> list[Movement]:
    return books.setdefault(REGISTER, [])


def move(books: dict[str, Any], holder: str, share_class: str, shares: int, day: date) -> Callable[[], None]:
    """The effect that records one movement, run once its document posts."""
    return lambda: movements(books).append(Movement(holder, share_class, shares, day))


def issued(books: dict[str, Any], day: date) -> int:
    """The shares issued and held at the end of ``day``: every holding's movements to then."""
    return sum(m.shares for m in movements(books) if m.day <= day)


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
    """The cap table at the end of ``day``: each holding with shares or options, by holder then class, and each
    holder's; fully diluted, each holding counts its holder's options of the class, and the day's total what the pool
    leaves to grant."""
    held: dict[tuple[str, str], int] = {}
    options: dict[tuple[str, str], int] = {}
    for m in movements(books):
        if m.day <= day:
            held[(m.holder, m.share_class)] = held.get((m.holder, m.share_class), 0) + m.shares
    by_id = {g.id: g for g in grants(books)}
    for g in grants(books):
        if g.day <= day:
            options[(g.holder, g.share_class)] = options.get((g.holder, g.share_class), 0) + g.number
    for e in events(books):
        g = by_id[e.grant]
        if e.day <= day:
            options[(g.holder, g.share_class)] -= e.number
    for x in exercises(books):
        g = by_id[x.grant]
        if x.day <= day:
            options[(g.holder, g.share_class)] -= x.number
    lines = []
    for key in sorted(held.keys() | options.keys()):
        shares, optioned = held.get(key, 0), options.get(key, 0)
        if shares == 0 and optioned == 0:
            continue
        holder, share_class = key
        issued = Decimal(shares)
        votes = money.EXACT.multiply(issued, scenario.accounts[share_class].votes_per_share)
        lines.append((holder, share_class, issued, votes, Decimal(shares + optioned)))
    pool = pool_on(pools(books), day)
    all_options = granted(grants(books), day, events(books)) - sum(x.number for x in exercises(books) if x.day <= day)
    unallocated = Decimal((pool.size if pool else 0) - granted(grants(books), day, events(books)))
    totals = (money.total([ln[2] for ln in lines]), money.total([ln[3] for ln in lines]),
              money.EXACT.add(money.total([ln[4] for ln in lines]), unallocated))
    by_holder: dict[str, list[Decimal]] = {}
    for holder, _, issued, votes, diluted in lines:
        sums = by_holder.setdefault(holder, [Decimal(0), Decimal(0), Decimal(0)])
        for k, value in enumerate((issued, votes, diluted)):
            sums[k] = money.EXACT.add(sums[k], value)
    return {
        "at": day.isoformat(),
        "issued": _plain(totals[0]),
        "votes": _plain(totals[1]),
        "options": _plain(Decimal(all_options)),
        "unallocated": _plain(unallocated),
        "fully_diluted": _plain(totals[2]),
        "lines": [{"holder": holder, "class": share_class, **_figures(issued, votes, diluted, totals)}
                  for holder, share_class, issued, votes, diluted in lines],
        "holders": [{"holder": holder, **_figures(*sums, totals)} for holder, sums in sorted(by_holder.items())],
    }
