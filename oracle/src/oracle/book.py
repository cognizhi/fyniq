"""What the rule sets post: lines, entries, scheduled entries and rejections (QA-ENG-01).

Shared by the manual journal (:mod:`oracle.ledger`) and the P1 sources (:mod:`oracle.sources`).
"""

from __future__ import annotations

from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money


@dataclass(frozen=True)
class PostedLine:
    account: str
    currency: str
    amount: Decimal
    base_amount: Decimal
    party: str | None = None  # a director's account's person (FR-EQ-01, B.7); a P1 source's lines name none yet
    # The line's dimension values, (dimension, code) in name order (FR-DIM-02); a P1 source's lines carry none yet.
    dimensions: tuple[tuple[str, str], ...] = ()


def dimension_set(dimensions: tuple[tuple[str, str], ...]) -> str:
    """A line's set of dimension values as README "Dimensions" writes it: ``Name=CODE`` joined by ``|`` in name
    order, ``""`` for none."""
    return "|".join(f"{name}={code}" for name, code in sorted(dimensions))


@dataclass(frozen=True)
class Entry:
    document: str
    kind: str  # "post", "reversal", "replacement" (an edit's), "release", "depreciation" or "auto_reversal"
    posting_date: date
    lines: tuple[PostedLine, ...]


@dataclass(frozen=True)
class Scheduled:
    """An entry a document schedules (ENG-05): it posts once its date is on or before ``as_of``."""

    kind: str  # "release" or "depreciation"
    posting_date: date
    lines: tuple[PostedLine, ...]


@dataclass
class Posting:
    """What a P1 rule set computes for one document, before anything is recorded.

    ``effects`` change what later documents read (an open amount, what is left of a deposit); the
    ledger runs them only once the document posts, so a rejected document changes nothing.
    """

    lines: list[PostedLine]
    scheduled: list[Scheduled] = field(default_factory=list)
    facts: Any = None
    effects: list[Callable[[], None]] = field(default_factory=list)


class Rejected(Exception):
    """A document or reversal the rules refuse. The reason is for people, never for the result."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def canonical(lines: Iterable[PostedLine]) -> tuple[PostedLine, ...]:
    """A P1 entry's lines netted per (account, currency, person, dimension set), sorted, with zero lines left out.

    test/differential/README.md, "P1 entries": two implementations may split a leg into several
    lines (a residual line beside its control line, one 7030 line or two); the canonical form is
    what the entry does to each account in each currency, so the comparison does not hinge on it. A
    director's account's lines stay apart by person (FR-EQ-01), and every account's by its set of dimension values
    (FR-DIM-02 rev 0.171), sorted after the person as :func:`dimension_set` writes it (none first).
    """
    totals: dict[tuple[str, str, str, str], tuple[Decimal, Decimal, tuple[tuple[str, str], ...]]] = {}
    for ln in lines:
        values = tuple(sorted(ln.dimensions))
        key = (ln.account, ln.currency, ln.party or "", dimension_set(values))
        amount, base, _ = totals.get(key, (money.ZERO, money.ZERO, values))
        totals[key] = (money.EXACT.add(amount, ln.amount), money.EXACT.add(base, ln.base_amount), values)
    return tuple(
        PostedLine(account, currency, amount, base, party or None, values)
        for (account, currency, party, _), (amount, base, values) in sorted(totals.items())
        if not (amount.is_zero() and base.is_zero())
    )
