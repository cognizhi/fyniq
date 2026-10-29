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

From P1 the ledger also posts the **P1 sources** (bank lines and transfers, ...), each through its
rule set in :mod:`oracle.sources`, in scenario order with the manual journals. A source's entry is
written in its canonical form (:func:`oracle.book.canonical`); a registered asset posts none. What a
source schedules (a spread's releases, an asset's depreciation) posts after every document and
reversal, when dated on or before ``as_of`` (ENG-05: up to "today", never the future), one entry per
document, kind and date; a later document may end a schedule (a disposal ends its asset's).

A scenario with ``"statements": true`` also gets every month's financial statements, read from the
posted entries by :mod:`oracle.statements` (FR-RPT-01).
"""

from __future__ import annotations

from collections import defaultdict
from datetime import date
from decimal import Decimal, localcontext
from typing import Any

from oracle import money
from oracle.book import Entry, PostedLine, Rejected, Scheduled, canonical
from oracle.rules import Rules
from oracle.scenario import RESULT_SCHEMA, Document, Reversal, Scenario, Source, check_result
from oracle.sources import RULE_SETS
from oracle.statements import statements

__all__ = ["Entry", "Ledger", "PostedLine", "Rejected", "period", "run"]


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
        self.rules = Rules(scenario) if scenario.settings is not None else None
        self.books: dict[str, Any] = {}  # what each posted P1 document left for later ones to read
        # (the document's position, its scheduled entries): the list is read when the schedules post,
        # so a later document (an asset's disposal) can still end it.
        self._scheduled: list[tuple[int, list[Scheduled]]] = []
        with localcontext(money.EXACT):  # an operator that slipped past money.* still cannot round
            for index, document in enumerate(scenario.documents):
                if isinstance(document, Source):
                    self._source(index, document)
                else:
                    self._journal(document)
            for reversal in scenario.reversals:
                self._reversal(reversal)
            self._schedule()

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

    def _source(self, index: int, document: Source) -> None:
        assert self.rules is not None  # the scenario reader refuses a source without settings
        try:
            posting = RULE_SETS[document.type](self.rules, document, self.books)
            lines = canonical(posting.lines)
            if any(ln.base_amount.copy_abs() >= money.AMOUNT_LIMIT for ln in lines):
                raise Rejected("base-out-of-range")  # §11.3: NUMERIC(20,4)
            if posting.lines and len(lines) < 2:  # every other leg rounded to nothing (README, "P1 readings")
                raise Rejected("entry-rounds-to-one-line")
        except Rejected as exc:
            self._reject(document.id, exc.reason)
            return
        if not money.total([ln.base_amount for ln in lines]).is_zero():
            raise RuntimeError(f"oracle defect: {document.type} {document.id} posts an unbalanced entry {lines}")
        for effect in posting.effects:
            effect()
        self.books[document.id] = posting.facts
        if posting.lines:  # a registered asset posts nothing itself, only its schedule
            entry = Entry(document.id, "post", document.date, lines)
            self._posted[document.id] = entry
            self._record(entry)
        self._scheduled.append((index, posting.scheduled))

    def _schedule(self) -> None:
        """Scheduled entries dated up to ``as_of``, one per (date, document, kind), in that order."""
        as_of = self.scenario.as_of
        groups: dict[tuple[date, int, str], list[PostedLine]] = {}
        for index, items in self._scheduled:
            for scheduled in items:
                if as_of is not None and scheduled.posting_date <= as_of:
                    groups.setdefault((scheduled.posting_date, index, scheduled.kind), []).extend(scheduled.lines)
        for (day, index, kind), lines in sorted(groups.items()):
            entry_lines = canonical(lines)
            if entry_lines:
                self._record(Entry(self.scenario.documents[index].id, kind, day, entry_lines))

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
        if s.statements:
            result["statements"] = statements(s, self.entries)
        check_result(result)
        return result


def run(scenario: Scenario) -> dict[str, Any]:
    """Post ``scenario`` and return its result document."""
    return Ledger(scenario).result()
