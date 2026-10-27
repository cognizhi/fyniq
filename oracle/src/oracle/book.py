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


@dataclass(frozen=True)
class Entry:
    document: str
    kind: str  # "post", "reversal", "release" or "depreciation"
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
    """A P1 entry's lines netted per (account, currency), sorted, with zero lines left out.

    test/differential/README.md, "P1 entries": two implementations may split a leg into several
    lines (a residual line beside its control line, one 7030 line or two); the canonical form is
    what the entry does to each account in each currency, so the comparison does not hinge on it.
    """
    totals: dict[tuple[str, str], tuple[Decimal, Decimal]] = {}
    for ln in lines:
        amount, base = totals.get((ln.account, ln.currency), (money.ZERO, money.ZERO))
        totals[(ln.account, ln.currency)] = (
            money.EXACT.add(amount, ln.amount),
            money.EXACT.add(base, ln.base_amount),
        )
    return tuple(
        PostedLine(account, currency, amount, base)
        for (account, currency), (amount, base) in sorted(totals.items())
        if not (amount.is_zero() and base.is_zero())
    )
