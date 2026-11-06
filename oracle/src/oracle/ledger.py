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
9. (PRD-001 rev 0.167) Every line on a director's account (the roles ``director-loan``, 2500, and
   ``deferred-remuneration``, 2120) names its person, one of the scenario's ``people``, and where its
   amount came from (lent, paid personally, pay, fee, paid out or other); no other line names either
   (FR-EQ-01, Appendix B.7: "all amounts carry the person as a sub-ledger key on 2500 and 2120").
11. (PRD-001 rev 0.171, 0.172) Every dimension value a line names is in use, and of a dimension in use (DIM-06); and
    a line on an account that requires a dimension in use names a value of it (DIM-05), unless the journal's lines on
    that account sum to zero in the base currency: such a journal moves an amount between values (FR-DIM-01). The
    opening balances, and so their edits, are held to DIM-06 only (README, "Dimensions"); a journal's edit to both.

A reversal posts the negation of every line of the document's posted entry, on the reversal's date
(FR-GL-12), each line keeping its person and its dimension values. The base amounts are negated as stored,
never re-converted (ENG-14). A reversal of an unknown or rejected document, of a document already
reversed, or dated before the entry it reverses is rejected (FR-GL-12, rev 0.24: "dated on or after the
entry's posting date").

The trial balance is the base-currency total per account per calendar month of the posting date
(Appendix B: "period = the month of the posting date").

From P1 the ledger also posts the **P1 sources** (bank lines and transfers, ...), each through its
rule set in :mod:`oracle.sources`, in scenario order with the manual journals. A source's entry is
written in its canonical form (:func:`oracle.book.canonical`); a registered asset posts none. A source's entry
that keeps fewer than two lines, or is left all in one currency without balancing in it, refuses its
document (INV-01; README, "P1 readings"). What a
source schedules (a spread's releases, an asset's depreciation) posts after every document and
reversal, when dated on or before ``as_of`` (ENG-05: up to "today", never the future), one entry per
document, kind and date; a later document may end a schedule (a disposal ends its asset's).

From P1 the general ledger posts three more shapes (README, "Opening balances, automatic reversals and
edits"), written from FR-GL-02, FR-GL-06, FR-GL-12, §10.2 and INV-13:

- **The opening balances** (FR-GL-06 rev 0.49): one document, posted as entered at the books' start when
  it meets the manual journal's rules with two differences: every line is in the base currency (LEDGER-17),
  and share capital may take a line (INV-13: the register of share issues and the opening balances are the
  two sources that reach it). Its entry is in the canonical form of a P1 entry.
- **Automatic reversals** (FR-GL-02 rev 0.55): a journal may carry ``reverse_on``, after its own date or the
  journal is refused (JOURNAL-11). Once it posts and is still not reversed, the scheduler reverses its
  current entry on that day, when it is on or before ``as_of``, with the other scheduled entries.
- **Edits** (FR-GL-12 rev 0.54 and 0.58; FR-GL-06 rev 0.66): the next version of a posted, unreversed journal
  (or of the opening balances), which reverses the entry the journal has on that entry's own date and posts
  the new version's entry on its date, both or neither. An edit of a journal never posted or reversed, a
  version that does not follow, or content the rules refuse is refused and changes nothing.

From P1 the scheduler also revalues the accounts marked ``revalue`` at each month end (FR-GL-04), in
:mod:`oracle.revaluation`: base-only lines at the period-end rate against the unrealised FX account, reversed on the
next month's first day (README, "Month-end revaluation").

A scenario that declares dimensions also gets ``dimension_balances`` (FR-DIM-02; README, "Dimensions"): each posting
month's base-currency total of the posted lines on each account by their exact set of dimension values.

A scenario with ``"statements": true`` also gets every month's financial statements, read from the
posted entries by :mod:`oracle.statements` (FR-RPT-01), and one with ``kpis`` every stated month's KPIs, read
from those statements and the trial balance by :mod:`oracle.kpis` (FR-KPI-02).
"""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from datetime import date
from decimal import Decimal, localcontext
from typing import Any

from oracle import money
from oracle.book import Entry, PostedLine, Rejected, Scheduled, canonical, dimension_set
from oracle.revaluation import revaluations
from oracle.rules import Rules
from oracle.scenario import RESULT_SCHEMA, Document, Edit, Line, Reversal, Scenario, Source, check_result
from oracle.sources import RULE_SETS
from oracle.kpis import kpis
from oracle.statements import statements

# The director's accounts' roles (B.7) and the origins a line on one may give (FR-EQ-01, PRD-001 rev 0.167).
DIRECTOR_ACCOUNTS = frozenset({"director-loan", "deferred-remuneration"})
ORIGINS = frozenset({"lent", "paid_personally", "pay", "fee", "paid_out", "other"})

__all__ = ["Entry", "Ledger", "PostedLine", "Rejected", "period", "run"]


@dataclass
class _Journal:
    """A posted manual journal or the opening balances, as it stands: its current version and entry."""

    index: int  # its place in the scenario, which orders its automatic reversal among the scheduled entries
    type: str
    version: int
    entry: Entry
    reverse_on: date | None


def _negated(entry: Entry, kind: str, day: date) -> Entry:
    """The reversal of ``entry`` on ``day``: every stored line negated, amount and base amount (ENG-14), keeping its
    person and its dimension values (README, "Dimensions")."""
    return Entry(entry.document, kind, day, tuple(
        PostedLine(ln.account, ln.currency, money.negate(ln.amount), money.negate(ln.base_amount), ln.party,
                   ln.dimensions)
        for ln in entry.lines
    ))


def period(day: date) -> str:
    """The calendar-month period of a posting date, as ``YYYY-MM`` (Appendix B)."""
    return f"{day.year:04d}-{day.month:02d}"


class Ledger:
    """Posts a scenario: the documents in order, then the reversals and edits in order, then what is scheduled."""

    def __init__(self, scenario: Scenario) -> None:
        self.scenario = scenario
        self.base_minor = scenario.currencies[scenario.base_currency]
        self.entries: list[Entry] = []
        self.rejected: list[str] = []
        self.reasons: list[tuple[str, str]] = []  # (document or "<id>/reversal", reason), in order
        self.post: defaultdict[tuple[str, str], Decimal] = defaultdict(lambda: money.ZERO)
        self._posted: dict[str, Entry] = {}  # each posted document's current entry
        self._journals: dict[str, _Journal] = {}  # the posted manual journals and opening balances, as they stand
        self._reversed: set[str] = set()
        self.rules = Rules(scenario, self.entries) if scenario.settings is not None else None
        self.books: dict[str, Any] = {}  # what each posted P1 document left for later ones to read
        # (the document's position, its scheduled entries): the list is read when the schedules post,
        # so a later document (an asset's disposal) can still end it.
        self._scheduled: list[tuple[int, list[Scheduled]]] = []
        with localcontext(money.EXACT):  # an operator that slipped past money.* still cannot round
            for index, document in enumerate(scenario.documents):
                if isinstance(document, Source):
                    self._source(index, document)
                else:
                    self._journal(index, document)
            for correction in scenario.reversals:  # voids and edits, in the order they are made
                if isinstance(correction, Edit):
                    self._edit(correction)
                else:
                    self._reversal(correction)
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

    def _journal(self, index: int, document: Document) -> None:
        try:
            lines = self._content(document.type, document.date, document.lines, document.reverse_on)
        except Rejected as exc:
            self._reject(document.id, exc.reason)
            return
        entry = Entry(document.id, "post", document.date, lines)
        self._journals[document.id] = _Journal(index, document.type, document.version, entry, document.reverse_on)
        self._posted[document.id] = entry
        self._record(entry)

    def _content(self, kind: str, day: date, lines: tuple[Line, ...], reverse_on: date | None) -> tuple[PostedLine, ...]:
        """The entry a manual journal's or the opening balances' version posts, or Rejected."""
        if kind == "opening_balances":
            entry_lines = canonical(self._journal_lines(lines, opening=True))  # README, "P1 entries"
            if any(ln.base_amount.copy_abs() >= money.AMOUNT_LIMIT for ln in entry_lines):
                raise Rejected("base-out-of-range")  # §11.3: an account's balances together still fit NUMERIC(20,4)
            if len(entry_lines) < 2:  # balances on one account that cancel out leave nothing to post
                raise Rejected("fewer-than-two-lines")
            return entry_lines
        posted = self._journal_lines(lines)
        if reverse_on is not None and reverse_on <= day:  # FR-GL-02 rev 0.55: after its own date (JOURNAL-11)
            raise Rejected("reverse-on-not-after-date")
        return posted

    def _journal_lines(self, lines: tuple[Line, ...], opening: bool = False) -> tuple[PostedLine, ...]:
        """ENG-24's rules on a journal's lines; ``opening``: the opening balances' (README, "Opening balances")."""
        s = self.scenario
        base = s.base_currency

        # Rule 1: the precision boundary, before anything is computed (ENG-44).
        parsed: list[tuple[Decimal, Decimal | None]] = []
        for line in lines:
            minor = s.currencies.get(line.currency)
            if minor is None:
                raise Rejected("unknown-currency")
            try:
                amount = money.parse_amount(line.amount, minor)
                rate = money.parse_rate(line.fx_rate) if line.fx_rate is not None else None
            except money.FormatError as exc:
                raise Rejected(exc.reason) from exc
            parsed.append((amount, rate))

        # The opening balances are in the base currency only (FR-GL-06 rev 0.49, LEDGER-17).
        if opening and any(line.currency != base for line in lines):
            raise Rejected("opening-balance-not-in-base")

        # Rule 2.
        if len(lines) < 2:
            raise Rejected("fewer-than-two-lines")

        # Rule 3.
        if any(amount.is_zero() for amount, _ in parsed):
            raise Rejected("zero-line")

        # Rule 4 (INV-04).
        for line in lines:
            account = s.accounts.get(line.account)
            if account is None:
                raise Rejected("unknown-account")
            if account.group:
                raise Rejected("group-account")
            if not account.active:
                raise Rejected("inactive-account")
            if account.role == "share-capital" and not opening:  # INV-13: the opening balances may reach it
                raise Rejected("share-capital-account")

        # Rule 9 (FR-EQ-01, B.7): the person and origin on a director's account, and only there.
        for line in lines:
            director = s.accounts[line.account].role in DIRECTOR_ACCOUNTS
            if director and (line.party not in s.people or line.origin not in ORIGINS):
                raise Rejected("director-line-without-person")
            if not director and (line.party is not None or line.origin is not None):
                raise Rejected("person-on-another-account")

        # Rule 5 (ENG-13, ENG-03), then RND-FX for every line not in the base currency.
        posted: list[PostedLine] = []
        for line, (amount, rate) in zip(lines, parsed, strict=True):
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
            posted.append(PostedLine(line.account, line.currency, amount, base_amount, line.party, line.dimensions))

        # Rule 7 (INV-01, INV-05): the stored base amounts balance. Never round a sum.
        if not money.total([ln.base_amount for ln in posted]).is_zero():
            raise Rejected("unbalanced-base")

        # Rule 8 (INV-01): a single-currency entry also balances in its own currency.
        if len({ln.currency for ln in posted}) == 1 and not money.total([ln.amount for ln in posted]).is_zero():
            raise Rejected("unbalanced-currency")

        self._dimension_rules(posted, opening)
        return tuple(posted)

    def _dimension_rules(self, posted: list[PostedLine], opening: bool) -> None:
        """Rule 11 (README, "Dimensions"; FR-DIM-01 rev 0.171, 0.172)."""
        dimensions = self.scenario.dimensions
        # DIM-06: a value in use, of a dimension in use (the scenario reader made sure each is declared).
        for line in posted:
            for name, code in line.dimensions:
                if not dimensions[name].active:
                    raise Rejected("dimension-out-of-use")
                if not dimensions[name].values[code]:
                    raise Rejected("dimension-value-out-of-use")
        if opening:  # the opening balances are not held to what an account requires
            return
        # DIM-05: a line on an account that requires a dimension in use names a value of it, unless the journal's lines
        # on that account sum to zero in base, moving an amount between values (rev 0.172).
        for line in posted:
            named = {name for name, _ in line.dimensions}
            missing = [n for n in self.scenario.accounts[line.account].requires
                       if dimensions[n].active and n not in named]
            if missing and not money.total([ln.base_amount for ln in posted if ln.account == line.account]).is_zero():
                raise Rejected("dimension-required")

    def _source(self, index: int, document: Source) -> None:
        assert self.rules is not None  # the scenario reader refuses a source without settings
        try:
            posting = RULE_SETS[document.type](self.rules, document, self.books)
            lines = canonical(posting.lines)
            if any(ln.base_amount.copy_abs() >= money.AMOUNT_LIMIT for ln in lines):
                raise Rejected("base-out-of-range")  # §11.3: NUMERIC(20,4)
            if posting.lines and len(lines) < 2:  # every other leg rounded to nothing (README, "P1 readings")
                raise Rejected("entry-rounds-to-one-line")
            # INV-01 (README, "P1 readings"): an entry left all in one currency balances in it, whatever its number of
            # lines, or the database refuses it ("does not balance"): USD 0.01 on each of two accounts at 0.1 owes
            # S$0.00; a USD 0.10 invoice on SR at 0.1 leaves USD 0.11 against 0.10 once its GST is S$0.00.
            if len({ln.currency for ln in lines}) == 1 and not money.total([ln.amount for ln in lines]).is_zero():
                raise Rejected("unbalanced-currency")
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
        """Scheduled entries dated up to ``as_of``, one per (date, document, kind), in that order.

        A source's are in canonical form. A journal's automatic reversal (FR-GL-02) reverses its current entry,
        its lines negated in their order, unless the journal is reversed already: the scheduler runs after every
        document, reversal and edit (README, "Automatic reversals").
        """
        as_of = self.scenario.as_of
        groups: dict[tuple[date, int, str], list[PostedLine]] = {}
        for index, items in self._scheduled:
            for scheduled in items:
                if as_of is not None and scheduled.posting_date <= as_of:
                    groups.setdefault((scheduled.posting_date, index, scheduled.kind), []).extend(scheduled.lines)
        due: list[tuple[tuple[date, int, str], Entry]] = []
        for (day, index, kind), lines in groups.items():
            entry_lines = canonical(lines)
            if entry_lines:
                due.append(((day, index, kind), Entry(self.scenario.documents[index].id, kind, day, entry_lines)))
        for doc_id, journal in self._journals.items():
            day = journal.reverse_on
            if day is not None and as_of is not None and day <= as_of and doc_id not in self._reversed:
                due.append(((day, journal.index, "auto_reversal"), _negated(journal.entry, "auto_reversal", day)))
        # The month-end revaluations (FR-GL-04) read every other entry, scheduled ones included, and come after every
        # document's scheduled entries of their date (README, "Month-end revaluation").
        after = len(self.scenario.documents)
        for entry in revaluations(self.scenario, [*self.entries, *(e for _, e in due)], self.books):
            due.append(((entry.posting_date, after, entry.kind), entry))
        for _, entry in sorted(due, key=lambda item: item[0]):
            if entry.kind == "auto_reversal":
                self._reversed.add(entry.document)
            self._record(entry)

    def _reversal(self, reversal: Reversal) -> None:
        key = f"{reversal.document}/reversal"
        original = self._posted.get(reversal.document)  # the journal's current entry (an edit's replacement)
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
        self._record(_negated(original, "reversal", reversal.date))

    def _edit(self, edit: Edit) -> None:
        """FR-GL-12 (rev 0.54, 0.58): the next version reverses the current entry on that entry's own date and
        posts its own entry on its date, in one transaction, so both post or neither does."""
        key = f"{edit.document}/edit"
        journal = self._journals.get(edit.document)
        if journal is None:  # never posted: unknown, or refused (an edit is a command on a posted journal)
            self._reject(key, "edit-of-unposted-document")
            return
        if edit.document in self._reversed:  # "the database refuses ... any change to a reversed journal"
            self._reject(key, "edit-of-reversed-document")
            return
        if edit.version != journal.version + 1:  # it saves the next version
            self._reject(key, "version-does-not-follow")
            return
        try:
            lines = self._content(journal.type, edit.date, edit.lines, edit.reverse_on)
        except Rejected as exc:
            self._reject(key, exc.reason)
            return
        self._record(_negated(journal.entry, "reversal", journal.entry.posting_date))
        replacement = Entry(edit.document, "replacement", edit.date, lines)
        self._record(replacement)
        journal.version, journal.entry, journal.reverse_on = edit.version, replacement, edit.reverse_on
        self._posted[edit.document] = replacement

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

    def people(self) -> dict[str, dict[str, Decimal]]:
        """Each person's base-currency total on each director's account over every posted line (FR-EQ-01).

        The sub-ledger of 2500 and 2120: a line names its person there and nowhere else (rule 9), so the
        totals add up to those accounts. Totals of zero are left out, and so is a person with none.
        """
        totals: dict[str, dict[str, Decimal]] = defaultdict(lambda: defaultdict(lambda: money.ZERO))
        for entry in self.entries:
            for ln in entry.lines:
                if ln.party is not None:
                    totals[ln.party][ln.account] = money.EXACT.add(totals[ln.party][ln.account], ln.base_amount)
        out = {}
        for person in sorted(totals):
            accounts = {a: v for a, v in sorted(totals[person].items()) if not v.is_zero()}
            if accounts:
                out[person] = accounts
        return out

    def dimension_balances(self) -> dict[str, dict[str, dict[str, Decimal]]]:
        """Each posting month's base-currency total of the posted lines on each account by their exact set of
        dimension values (FR-DIM-02; README, "Dimensions"), the set as :func:`oracle.book.dimension_set` writes it.

        Every posting month appears, as in the trial balance; totals of zero are left out, and so is an account with
        none. Each month's sets of an account add up to its trial balance total.
        """
        totals: dict[str, dict[str, dict[str, Decimal]]] = {period(e.posting_date): {} for e in self.entries}
        for entry in self.entries:
            month = totals[period(entry.posting_date)]
            for ln in entry.lines:
                sets = month.setdefault(ln.account, {})
                key = dimension_set(ln.dimensions)
                sets[key] = money.EXACT.add(sets.get(key, money.ZERO), ln.base_amount)
        out: dict[str, dict[str, dict[str, Decimal]]] = {}
        for month in sorted(totals):
            accounts = {}
            for account in sorted(totals[month]):
                sets = {key: v for key, v in sorted(totals[month][account].items()) if not v.is_zero()}
                if sets:
                    accounts[account] = sets
            out[month] = accounts
        return out

    def result(self) -> dict[str, Any]:
        """The ``fyniq-scenario-result@1`` document, every amount a plain decimal string."""
        s = self.scenario

        def line(ln: PostedLine) -> dict[str, Any]:
            out = {
                "account": ln.account,
                "currency": ln.currency,
                "amount": money.fmt(ln.amount, s.currencies[ln.currency]),
                "base_amount": money.fmt(ln.base_amount, self.base_minor),
            }
            if ln.party is not None:
                out["party"] = ln.party
            if ln.dimensions:
                out["dimensions"] = dict(ln.dimensions)
            return out

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
        if s.dimensions:  # present exactly when the scenario declares dimensions (FR-DIM-02)
            result["dimension_balances"] = {
                month: {account: {key: money.fmt(total, self.base_minor) for key, total in sets.items()}
                        for account, sets in accounts.items()}
                for month, accounts in self.dimension_balances().items()
            }
        people = self.people()
        if people:
            result["people"] = {
                person: {account: money.fmt(total, self.base_minor) for account, total in accounts.items()}
                for person, accounts in people.items()
            }
        if s.statements:
            result["statements"] = statements(s, self.entries)
        if s.kpis is not None:
            result["kpis"] = kpis(s, result)
        check_result(result)
        return result


def run(scenario: Scenario) -> dict[str, Any]:
    """Post ``scenario`` and return its result document."""
    return Ledger(scenario).result()
