"""The oracle's purge of a financial year (PRD-001 AUD-10, rev 0.238; p1-audit-explorer design D7b).

Written from the rule, not from the Go engine (``internal/domain/retention``):

* A purge is a ``fyniq-purge@1`` document (test/differential/README.md, "Purges"): a ledger's accounts and posted
  entries, and a boundary, the first day of a financial year.
* Kept: every entry dated on or after the boundary, every entry a permanent register or a reconciliation of the bank
  names, and, until nothing changes, every entry a kept entry reverses or is reversed by. The rest are purged.
* The carry-forward, rule ``purge-carry-forward@1``, sums the purged lines, amount and base apart, under a key: revenue
  and expense under the retained earnings account, their dimension set and the base currency, their base as their
  amount (the year-end fold, FR-GL-10); a control account's under its source document, account, dimension set,
  currency, person and instrument (INV-07); every other account's under its account, dimension set, currency, person
  and instrument (a director's account and a SAFE read their own, INV-13). A key zero in both carries nothing. A key in
  the base currency carries one line; in another currency with no amount, one base-only line; else one line at the
  rate base ÷ amount, rounded half away from zero to 10 decimals (1 when that is not above zero or does not fit 10
  integer digits), whose base is ``RND-FX``'s, and a base-only line of what is left (INV-05).
* Each source document's control keys make an entry with that document as its source, balanced if need be by a last
  line on suspense; the boundary's entry (source ``ledger_boundary``) holds the other keys and, last, takes back on
  suspense what the documents' entries put there. Boundary's entry first, then documents by type and ID; keys by
  account code, dimension set, currency, person and instrument.
* The positions at each date from the boundary on (the boundary, each later posting date of a kept entry, each
  financial year's first day up to the last of those) sum every line dated up to it by account, dimension set,
  currency, person, instrument and, for a control account, its entry's source; revenue and expense from before the
  date's financial year fold into retained earnings in the base currency. The purge must leave every one of them
  unchanged.
"""

from __future__ import annotations

from datetime import date
from decimal import Decimal
from typing import Any, NamedTuple

from .money import EXACT, ZERO, FormatError, fmt, negate, parse_amount, rnd_fx, round_quotient, total
from .scenario import ScenarioError, schema_error

SCHEMA = "fyniq-purge@1"
RESULT = "fyniq-purge-result@1"
BOUNDARY_SOURCE = "ledger_boundary"
ONE = Decimal(1)
RATE_CEILING = Decimal(10) ** 10  # a rate has at most 10 integer digits: NUMERIC(20,10)

Source = tuple[str, str]


class Posted(NamedTuple):
    """A line as the positions read it."""

    on: date
    account: str
    dset: str
    currency: str
    party: str  # "" for none
    instrument: str  # "" for none
    source: Source  # its entry's
    amount: Decimal
    base: Decimal


class Carried(NamedTuple):
    """A line of a carry-forward entry."""

    account: str
    dset: str
    currency: str
    party: str
    instrument: str
    amount: Decimal
    rate: Decimal
    base: Decimal


class PurgeError(ScenarioError):
    """A purge the oracle cannot read."""


class _Ledger:
    """A purge document, read and checked."""

    def __init__(self, doc: Any) -> None:
        problem = schema_error("purge.schema.json", doc)
        if problem is not None:
            raise PurgeError(problem)
        self.name: str = doc["name"]
        self.base: str = doc["base_currency"]
        self.units: dict[str, int] = dict(doc["currencies"])
        if self.base not in self.units:
            raise PurgeError(f"the base currency {self.base} is not in currencies")
        self.fy_month: int = doc["fy_start_month"]
        self.accounts: dict[str, dict[str, Any]] = {}
        for account in doc["accounts"]:
            if account["code"] in self.accounts:
                raise PurgeError(f"two accounts have the code {account['code']}")
            self.accounts[account["code"]] = account
        self.retained: str = doc["retained_earnings"]
        self.suspense: str = doc["suspense"]
        for what, code in (("retained earnings", self.retained), ("suspense", self.suspense)):
            if code not in self.accounts:
                raise PurgeError(f"the {what} account {code} is not an account")
            if self.control(code) or self.profit_or_loss(code):
                raise PurgeError(f"the {what} account {code} is a control account, revenue or expense")
        self.boundary = _day(doc["boundary"], "boundary")
        if self.boundary.day != 1 or self.boundary.month != self.fy_month:
            raise PurgeError(f"the boundary {doc['boundary']} is not the first day of a financial year")
        self.entries: dict[str, dict[str, Any]] = {}
        for entry in doc["entries"]:
            if entry["id"] in self.entries:
                raise PurgeError(f"two entries have the ID {entry['id']}")
            self.entries[entry["id"]] = entry
        self.lines: dict[str, list[Posted]] = {}
        for entry in doc["entries"]:
            if "reverses" in entry and entry["reverses"] not in self.entries:
                raise PurgeError(f"{entry['id']} reverses {entry['reverses']}, which is not an entry")
            self.lines[entry["id"]] = self._lines(entry)

    def _lines(self, entry: dict[str, Any]) -> list[Posted]:
        when = _day(entry["posting_date"], f"{entry['id']}: posting_date")
        source = (entry["source"]["type"], entry["source"]["id"])
        out: list[Posted] = []
        balance = ZERO
        for line in entry["lines"]:
            where = f"{entry['id']}: a line on {line['account']}"
            if line["account"] not in self.accounts:
                raise PurgeError(f"{where}: no such account")
            if line["currency"] not in self.units:
                raise PurgeError(f"{where}: the currency {line['currency']} is not in currencies")
            try:
                amount = parse_amount(line["amount"], self.units[line["currency"]])
                base = parse_amount(line["base_amount"], self.units[self.base])
            except FormatError as exc:
                raise PurgeError(f"{where}: {exc.reason}") from exc
            if line["currency"] == self.base and amount != base:
                raise PurgeError(f"{where}: in the base currency, the amount {amount} is not the base amount {base}")
            balance = EXACT.add(balance, base)
            out.append(Posted(when, line["account"], line["dimension_set"], line["currency"], line.get("party", ""),
                              line.get("instrument", ""), source, amount, base))
        if balance != ZERO:
            raise PurgeError(f"{entry['id']} does not balance in the base currency: {balance}")
        return out

    def control(self, code: str) -> bool:
        account = self.accounts[code]
        return bool(account.get("control")) or account.get("role") in ("accounts-receivable", "accounts-payable")

    def profit_or_loss(self, code: str) -> bool:
        return self.accounts[code]["type"] in ("revenue", "expense")

    def year_start(self, day: date) -> date:
        year = day.year if day.month >= self.fy_month else day.year - 1
        return date(year, self.fy_month, 1)


def _day(text: str, where: str) -> date:
    try:
        return date.fromisoformat(text)
    except ValueError as exc:
        raise PurgeError(f"{where}: {text!r} is not a calendar date") from exc


def kept(ledger: _Ledger) -> set[str]:
    """The entries the purge keeps: from the boundary on, those named, and both ends of every reversal touching them."""
    keep = {key for key, entry in ledger.entries.items()
            if entry.get("named") or _day(entry["posting_date"], key) >= ledger.boundary}
    while True:
        more = {end for key, entry in ledger.entries.items() if "reverses" in entry
                for end in (key, entry["reverses"]) if (key in keep) != (entry["reverses"] in keep)}
        if not more:
            return keep
        keep |= more


def _carried_lines(ledger: _Ledger, at: tuple[str, str, str, str, str], amount: Decimal, base: Decimal) -> list[Carried]:
    """A key's lines: one at the rate its sums give, and a base-only line for what that rate leaves."""
    account, dset, currency, party, instrument = at
    if currency == ledger.base:
        return [Carried(account, dset, currency, party, instrument, amount, ONE, base)]
    if amount == ZERO:
        return [Carried(account, dset, currency, party, instrument, ZERO, ONE, base)]
    rate = round_quotient(base, amount, 10)
    if rate <= ZERO or rate >= RATE_CEILING:
        rate = ONE
    first = rnd_fx(amount, rate, ledger.units[ledger.base])
    lines = [Carried(account, dset, currency, party, instrument, amount, rate, first)]
    rest = EXACT.subtract(base, first)
    if rest != ZERO:
        lines.append(Carried(account, dset, currency, party, instrument, ZERO, ONE, rest))
    return lines


def carry_forward(ledger: _Ledger, purged: list[str]) -> list[tuple[Source, list[Carried]]]:
    """The carry-forward entries of the purged entries' lines, in the rule's order."""
    sums: dict[tuple[Source | None, tuple[str, str, str, str, str]], list[Decimal]] = {}
    for key in purged:
        for line in ledger.lines[key]:
            if ledger.profit_or_loss(line.account):
                where, at, value = None, (ledger.retained, line.dset, ledger.base, "", ""), line.base
            else:
                where = line.source if ledger.control(line.account) else None
                at, value = (line.account, line.dset, line.currency, line.party, line.instrument), line.amount
            pair = sums.setdefault((where, at), [ZERO, ZERO])
            pair[0], pair[1] = EXACT.add(pair[0], value), EXACT.add(pair[1], line.base)
    boundary: list[Carried] = []
    documents: dict[Source, list[Carried]] = {}
    for (where, at), (amount, base) in sorted(sums.items(), key=lambda kv: kv[0][1]):
        if amount == ZERO and base == ZERO:
            continue
        lines = _carried_lines(ledger, at, amount, base)
        (boundary if where is None else documents.setdefault(where, [])).extend(lines)
    taken = ZERO
    entries = []
    for source in sorted(documents):
        lines = documents[source]
        owed = total([line.base for line in lines])
        if owed != ZERO:
            lines.append(Carried(ledger.suspense, "", ledger.base, "", "", negate(owed), ONE, negate(owed)))
            taken = EXACT.add(taken, owed)
        entries.append((source, lines))
    if taken != ZERO:
        boundary.append(Carried(ledger.suspense, "", ledger.base, "", "", taken, ONE, taken))
    if boundary:
        entries.insert(0, ((BOUNDARY_SOURCE, ledger.boundary.isoformat()), boundary))
    return entries


PositionKey = tuple[str, str, str, str, str, Source | None]


def _positions(ledger: _Ledger, lines: list[Posted], dates: list[date]) -> dict[date, dict[PositionKey, tuple[Decimal, Decimal]]]:
    out = {}
    for day in dates:
        start = ledger.year_start(day)
        sums: dict[PositionKey, tuple[Decimal, Decimal]] = {}
        for line in lines:
            if line.on > day:
                continue
            amount = line.amount
            if ledger.profit_or_loss(line.account) and line.on < start:
                at: PositionKey = (ledger.retained, line.dset, ledger.base, "", "", None)
                amount = line.base
            else:
                at = (line.account, line.dset, line.currency, line.party, line.instrument,
                      line.source if ledger.control(line.account) else None)
            a, b = sums.get(at, (ZERO, ZERO))
            sums[at] = (EXACT.add(a, amount), EXACT.add(b, line.base))
        out[day] = {at: ab for at, ab in sums.items() if ab != (ZERO, ZERO)}
    return out


def _rate(rate: Decimal) -> str:
    text = format(rate, "f")
    return text.rstrip("0").rstrip(".") if "." in text else text


def purge(doc: Any) -> dict[str, Any]:
    """Purge a fyniq-purge@1 document: its fyniq-purge-result@1."""
    ledger = _Ledger(doc)
    keep = kept(ledger)
    purged = [key for key in ledger.entries if key not in keep]
    entries = carry_forward(ledger, purged)
    after = sorted({day for key in keep if (day := _day(ledger.entries[key]["posting_date"], key)) > ledger.boundary})
    dates = {ledger.boundary, *after}
    if after:
        year = ledger.boundary.year + 1
        while date(year, ledger.fy_month, 1) <= after[-1]:
            dates.add(date(year, ledger.fy_month, 1))
            year += 1
    dates_in_order = sorted(dates)
    whole = [line for key in ledger.entries for line in ledger.lines[key]]
    carried = [Posted(ledger.boundary, x.account, x.dset, x.currency, x.party, x.instrument, source, x.amount, x.base)
               for source, lines in entries for x in lines]
    left = [line for key in ledger.entries if key in keep for line in ledger.lines[key]] + carried
    before, now = _positions(ledger, whole, dates_in_order), _positions(ledger, left, dates_in_order)

    def money(value: Decimal, currency: str) -> str:
        return fmt(value, ledger.units[currency])

    def balances(sums: dict[PositionKey, tuple[Decimal, Decimal]]) -> list[dict[str, Any]]:
        order = sorted(sums, key=lambda at: (*at[:5], ("", "") if at[5] is None else at[5]))
        return [{"account": at[0], "dimension_set": at[1], "currency": at[2], "party": at[3] or None, "instrument": at[4] or None,
                 "source": None if at[5] is None else {"type": at[5][0], "id": at[5][1]},
                 "amount": money(sums[at][0], at[2]), "base_amount": money(sums[at][1], ledger.base)} for at in order]

    return {
        "schema": RESULT,
        "name": ledger.name,
        "kept": sorted(key for key in keep if _day(ledger.entries[key]["posting_date"], key) < ledger.boundary),
        "purged": len(purged),
        "entries": [{"source": {"type": source[0], "id": source[1]},
                     "lines": [{"account": x.account, "dimension_set": x.dset, "currency": x.currency, "party": x.party or None,
                                "instrument": x.instrument or None, "amount": money(x.amount, x.currency), "rate": _rate(x.rate),
                                "base_amount": money(x.base, ledger.base)} for x in lines]}
                    for source, lines in entries],
        "positions": [{"date": day.isoformat(), "balances": balances(now[day])} for day in dates_in_order],
        "unchanged": before == now,
    }
