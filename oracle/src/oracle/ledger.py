"""The reference ledger: what P0 posts, computed from the rules (QA-ENG-01).

Written from PRD-001 §10 (10.1 to 10.8), §11.3 and Appendix B, never from the Go engine. It is a
port of the workbook's reference ledger (``tests/reference.py`` in the ``zh-sheet-accounting``
repository, by the same owner, used here under this repository's MIT licence): the same shape,
postings accumulated per (account, month) and the trial balance read back from them, rewritten in
exact decimals. The workbook rounded through binary floats (``r2``); here every rounding is the named
§10.4 policy it implements, and only ``RND-FX`` applies to a manual journal.

P0 posts one source: the **manual journal** (§10.2, "Journal: as entered, balanced only"), and
corrects posted history only by **reversal** (FR-GL-12, INV-02). A journal posts when it meets every
rule of ENG-24 (PRD-001 rev 0.24) below; otherwise it is rejected whole and posts no line (ENG-21 ✖):

1. Every amount and rate meets the input precision boundary (ENG-44, ENG-11, §11.3), and every
   currency is one of the scenario's currencies.
2. At least two lines.
3. No line is zero.
4. Every line posts to an existing, active, non-group (postable) account (INV-04), and never to a
   share-capital account (INV-13: share capital changes only through the register of share issues
   or opening balances, so a manual journal line to one is refused).
5. Every line not in the base currency has a rate (ENG-13); a line in the base currency has neither a
   rate nor its source (ENG-03). A rate always comes with its source (ENG-40): the scenario format
   requires it, so a rate without one makes the scenario invalid, not the journal rejected.
6. Its base amounts fit NUMERIC(20,4) (§11.3).
7. It balances in the base currency, summing the stored base amounts (INV-01, INV-05).
8. When every line shares one currency, it also balances in that currency (INV-01).

A reversal posts the negation of every line of the document's posted entry, on the reversal's date
(FR-GL-12). The base amounts are negated as stored, never re-converted (ENG-14). A reversal of an
unknown or rejected document, of a document already reversed, or dated before the entry it reverses
is rejected (FR-GL-12, rev 0.24: "dated on or after the entry's posting date").

The trial balance is the base-currency total per account per calendar month of the posting date
(Appendix B: "period = the month of the posting date").
"""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from datetime import date
from decimal import Decimal, localcontext
from typing import Any

from oracle import money
from oracle.scenario import RESULT_SCHEMA, Document, Reversal, Scenario, check_result


@dataclass(frozen=True)
class PostedLine:
    account: str
    currency: str
    amount: Decimal
    base_amount: Decimal


@dataclass(frozen=True)
class Entry:
    document: str
    kind: str  # "post" or "reversal"
    posting_date: date
    lines: tuple[PostedLine, ...]


class Rejected(Exception):
    """A document or reversal the rules refuse. The reason is for people, never for the result."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def period(day: date) -> str:
    """The calendar-month period of a posting date, as ``YYYY-MM`` (Appendix B)."""
    return f"{day.year:04d}-{day.month:02d}"


class Ledger:
    """Posts a scenario: the documents in order, then the reversals in order."""

    def __init__(self, scenario: Scenario) -> None:
        self.scenario = scenario
        self.base_minor = scenario.currencies[scenario.base_currency]
        self.entries: list[Entry] = []
        self.rejected: list[str] = []
        self.reasons: list[tuple[str, str]] = []  # (document or "<id>/reversal", reason), in order
        self.post: defaultdict[tuple[str, str], Decimal] = defaultdict(lambda: money.ZERO)
        self._posted: dict[str, Entry] = {}
        self._reversed: set[str] = set()
        with localcontext(money.EXACT):  # an operator that slipped past money.* still cannot round
            for document in scenario.documents:
                self._journal(document)
            for reversal in scenario.reversals:
                self._reversal(reversal)

    # ------------------------------------------------------------------ posting
    def p(self, account: str, month: str, base_amount: Decimal) -> None:
        self.post[(account, month)] = money.EXACT.add(self.post[(account, month)], base_amount)

    def _record(self, entry: Entry) -> None:
        self.entries.append(entry)
        month = period(entry.posting_date)
        for line in entry.lines:
            self.p(line.account, month, line.base_amount)

    def _reject(self, key: str, reason: str) -> None:
        self.rejected.append(key)
        self.reasons.append((key, reason))

    def _journal(self, document: Document) -> None:
        try:
            lines = self._journal_lines(document)
        except Rejected as exc:
            self._reject(document.id, exc.reason)
            return
        entry = Entry(document.id, "post", document.date, lines)
        self._posted[document.id] = entry
        self._record(entry)

    def _journal_lines(self, document: Document) -> tuple[PostedLine, ...]:
        s = self.scenario
        base = s.base_currency

        # Rule 1: the precision boundary, before anything is computed (ENG-44).
        parsed: list[tuple[Decimal, Decimal | None]] = []
        for line in document.lines:
            minor = s.currencies.get(line.currency)
            if minor is None:
                raise Rejected("unknown-currency")
            try:
                amount = money.parse_amount(line.amount, minor)
                rate = money.parse_rate(line.fx_rate) if line.fx_rate is not None else None
            except money.FormatError as exc:
                raise Rejected(exc.reason) from exc
            parsed.append((amount, rate))

        # Rule 2.
        if len(document.lines) < 2:
            raise Rejected("fewer-than-two-lines")

        # Rule 3.
        if any(amount.is_zero() for amount, _ in parsed):
            raise Rejected("zero-line")

        # Rule 4 (INV-04).
        for line in document.lines:
            account = s.accounts.get(line.account)
            if account is None:
                raise Rejected("unknown-account")
            if account.group:
                raise Rejected("group-account")
            if not account.active:
                raise Rejected("inactive-account")
            if account.role == "share-capital":  # INV-13
                raise Rejected("share-capital-account")

        # Rule 5 (ENG-13, ENG-03), then RND-FX for every line not in the base currency.
        posted: list[PostedLine] = []
        for line, (amount, rate) in zip(document.lines, parsed, strict=True):
            if line.currency == base:
                if line.fx_rate is not None or line.fx_rate_source is not None:
                    raise Rejected("rate-on-base-line")
                base_amount = amount  # already at the base minor units; nothing to round
            else:
                if rate is None:
                    raise Rejected("missing-rate")
                base_amount = money.rnd_fx(amount, rate, self.base_minor)
            # Rule 6 (§11.3): a stored base amount is NUMERIC(20,4) too.
            if base_amount.copy_abs() >= money.AMOUNT_LIMIT:
                raise Rejected("base-out-of-range")
            posted.append(PostedLine(line.account, line.currency, amount, base_amount))

        # Rule 7 (INV-01, INV-05): the stored base amounts balance. Never round a sum.
        if not money.total([ln.base_amount for ln in posted]).is_zero():
            raise Rejected("unbalanced-base")

        # Rule 8 (INV-01): a single-currency entry also balances in its own currency.
        if len({ln.currency for ln in posted}) == 1 and not money.total([ln.amount for ln in posted]).is_zero():
            raise Rejected("unbalanced-currency")

        return tuple(posted)

    def _reversal(self, reversal: Reversal) -> None:
        key = f"{reversal.document}/reversal"
        original = self._posted.get(reversal.document)
        if original is None:
            self._reject(key, "reversal-of-unposted-document")
            return
        if reversal.document in self._reversed:
            self._reject(key, "already-reversed")
            return
        if reversal.date < original.posting_date:  # FR-GL-12: on or after the entry's posting date
            self._reject(key, "reversal-before-entry")
            return
        self._reversed.add(reversal.document)
        lines = tuple(
            PostedLine(ln.account, ln.currency, money.negate(ln.amount), money.negate(ln.base_amount))
            for ln in original.lines
        )
        self._record(Entry(reversal.document, "reversal", reversal.date, lines))

    # ------------------------------------------------------------------ reading back
    def trial_balance(self) -> dict[str, dict[str, Decimal]]:
        """Base-currency total per account per posting month; accounts that total zero are left out.

        Every month with a posting appears, even when all of its accounts net to zero.
        """
        months: dict[str, dict[str, Decimal]] = {period(e.posting_date): {} for e in self.entries}
        for (account, month), amount in self.post.items():
            if not amount.is_zero():
                months[month][account] = amount
        return {m: dict(sorted(months[m].items())) for m in sorted(months)}

    def result(self) -> dict[str, Any]:
        """The ``fyniq-scenario-result@1`` document, every amount a plain decimal string."""
        s = self.scenario

        def line(ln: PostedLine) -> dict[str, str]:
            return {
                "account": ln.account,
                "currency": ln.currency,
                "amount": money.fmt(ln.amount, s.currencies[ln.currency]),
                "base_amount": money.fmt(ln.base_amount, self.base_minor),
            }

        result = {
            "schema": RESULT_SCHEMA,
            "name": s.name,
            "entries": [
                {
                    "document": e.document,
                    "kind": e.kind,
                    "posting_date": e.posting_date.isoformat(),
                    "lines": [line(ln) for ln in e.lines],
                }
                for e in self.entries
            ],
            "rejected": list(self.rejected),
            "trial_balance": {
                month: {account: money.fmt(amount, self.base_minor) for account, amount in accounts.items()}
                for month, accounts in self.trial_balance().items()
            },
        }
        check_result(result)
        return result


def run(scenario: Scenario) -> dict[str, Any]:
    """Post ``scenario`` and return its result document."""
    return Ledger(scenario).result()
