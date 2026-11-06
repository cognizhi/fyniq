"""Hypothesis strategies for the general ledger's shapes: the source ``general-ledger`` (QA-ENG-02).

test/differential/README.md, "Opening balances, automatic reversals and edits", written from FR-GL-02, FR-GL-06,
FR-GL-12, §10.2 and INV-13. :func:`oracle.strategies_p1.p1_scenarios_with_intent` draws them when its sources
name ``general-ledger``:

- **Opening balances** (in three scenarios of four), first in ``documents`` and dated the books' start: balances in
  the base currency on distinct accounts, share capital among them, and a director's balance credited to a person
  (the founders' source then knows what the company owes them from the books' start), netted by a last balance; or
  one of the ways they are refused: not netting to zero, a line in another currency, a rate on a base line, a zero
  line, a director's line without its person (or a person on another line), an inactive or a group account.
- **Manual journals**, drawn per day like the other sources' documents, from the manual-journal generator's posting
  kinds (:mod:`oracle.strategies`), with ``reverse_on`` half the time (the first day of the next month, as the
  screen suggests, or a later day, which may be after ``as_of``); or refused, by a rule of ENG-24 or by a
  ``reverse_on`` on or before their date (JOURNAL-11).
- **Corrections**, after every document, in ``reversals``: edits (the next version, posting or refused for its
  content, its ``reverse_on`` or its version), voids (on or after the entry, or refused before it or a second
  time), edits of a voided, a refused or an unknown journal, and edits of the opening balances. Each is built to
  an intent from what the generator tracks of each journal (posted, its version, its entry's date, voided), and
  recorded in :attr:`oracle.strategies.Intent.corrections`.

- **Dimensions** (in a third of the scenarios; README, "Dimensions", FR-DIM-01 rev 0.171 and 0.172): the declaration of
  :func:`oracle.strategies.dimension_plan`, with two accounts of their own, 6200 (an expense) and 4200 (a revenue),
  of which one or both require dimensions; no other source's generator reaches them, so their documents post as
  built. Journals and edits name values as their accounts require (:func:`oracle.strategies.tag`); the opening
  balances name values freely, may leave a requiring account's out (they are not held to it) and may split a balance
  between two sets of values on one account, which stay apart in canonical form. Kinds of their own: a journal moving
  an amount between values (posts), one leaving a required value out (refused, DIM-05), one naming a value out of use
  (refused, DIM-06), opening balances naming one (refused), and an invoice, credit note, bill, vendor credit, bank line
  or expense claim with a line on 6200 or 4200: refused when the account requires a dimension in use (the format's
  P1 documents carry no values, DIM-05), posted when it requires only dimensions out of use.

Journals reach ordinary accounts (money, equipment, accruals, suspense, income and costs), never the deferral and
control accounts other sources reconcile, nor a director's account, whose balance the founders' source tracks. Every
date is on or after the books' start (the product refuses a journal dated before it, rev 0.57, which the format does
not model).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from hypothesis import assume
from hypothesis import strategies as st

from oracle import money
from oracle.strategies import (
    _NEAR_THE_LIMIT,
    _POSTING_KINDS,
    _REJECTED_KINDS,
    BOUNDED,
    DIMENSION_POSTING,
    DIMENSION_REJECTED,
    POST,
    REJECT,
    Dimensions,
    _Ctx,
    _journal,
    dimension_plan,
    tag,
)

BASE = "SGD"
CURRENCIES = {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3}  # the P1 scenarios' (strategies_p1)
JOURNAL_ACCOUNTS = ["1010", "1011", "1021", "1500", "2050", "2999", "4010", "4020", "4030", "6010", "6020", "6110",
                    "7030"]
INACTIVE, GROUP = "8990", "8900"
SHARE_CAPITAL = "3010"  # in the founders' chart, which the general ledger's scenarios have too
OPENING_ACCOUNTS = ["1010", "1011", "1100", "1500", "1590", "2010", "2050", "3010", "3300", "4010", "6010"]
DIRECTORS = ["2500", "2120"]
PEOPLE = ["P0001", "P0002"]
STRANGER = "P0009"
ORIGINS = ["lent", "paid_personally", "pay", "fee", "paid_out", "other"]
UNKNOWN = "JNL-9999"

JOURNAL_POSTING = ["journal", "journal-reverses"]
JOURNAL_REJECTED = ["journal-refused", "journal-reverse-on-not-after"]
OPENING_POSTING = ["opening-balances"]
OPENING_REJECTED = ["opening-unbalanced", "opening-foreign", "opening-rate", "opening-zero-line",
                    "opening-director-without-person", "opening-inactive", "opening-group"]
CORRECTIONS_POSTING = ["edit", "edit-reverses", "edit-other-month", "void", "opening-edit"]
CORRECTIONS_REJECTED = ["edit-content-refused", "edit-reverse-on-not-after", "edit-version-skips", "edit-of-reversed",
                        "edit-of-refused", "edit-of-unknown", "void-before-entry", "void-twice",
                        "opening-edit-refused", "opening-edit-version-skips"]
# Weighted as the other sources' kinds: each posting kind twice for each refused one.
KINDS = JOURNAL_POSTING * 2 + JOURNAL_REJECTED
OPENING_KINDS = OPENING_POSTING * 2 * len(OPENING_REJECTED) + OPENING_REJECTED
CORRECTION_KINDS = CORRECTIONS_POSTING * 2 + CORRECTIONS_REJECTED
REJECTED_KINDS = frozenset(JOURNAL_REJECTED + OPENING_REJECTED + CORRECTIONS_REJECTED)
# Dimensions (FR-DIM-01): the accounts that may require them, and the kinds drawn only when a scenario declares some.
DIMENSION_ACCOUNTS = {"6200": "expense", "4200": "revenue"}
JOURNAL_DIMENSION_POSTING = ["journal-dimension-move"]
JOURNAL_DIMENSION_REJECTED = ["journal-dimension-required", "journal-dimension-out-of-use"]
DOCUMENT_DIMENSION = ["document-dimension-required"]  # refused, or posted when only dimensions out of use are required
DIMENSION_KINDS = JOURNAL_DIMENSION_POSTING + JOURNAL_DIMENSION_REJECTED + DOCUMENT_DIMENSION
OPENING_DIMENSION_REJECTED = ["opening-dimension-out-of-use"]
# A journal refused by its content: the manual-journal generator's refused kinds but those it posts here (no
# director's account in the context) and, while the statements bound amounts, those it draws balanced instead.
_DIRECTOR_KINDS = ("director-line-without-person", "person-on-another-account")


def context(dims: Dimensions | None = None) -> _Ctx:
    """The manual-journal generator's context on the P1 chart (with the dimensions' accounts when ``dims``)."""
    postable = list(JOURNAL_ACCOUNTS) + (list(DIMENSION_ACCOUNTS) if dims is not None else [])
    return _Ctx(BASE, dict(CURRENCIES), postable, INACTIVE, GROUP, SHARE_CAPITAL, dims=dims)


@st.composite
def dimensions(draw: st.DrawFn) -> Dimensions:
    """The dimensions a general ledger's scenario declares, required on 6200, 4200 or both."""
    return draw(dimension_plan(list(DIMENSION_ACCOUNTS)))


def dimension_accounts(dims: Dimensions) -> list[dict[str, Any]]:
    """6200 and 4200, each with what it requires."""
    chart = []
    for code, kind in DIMENSION_ACCOUNTS.items():
        account: dict[str, Any] = {"code": code, "type": kind, "group": False, "active": True}
        if code in dims.requires:
            account["requires"] = list(dims.requires[code])
        chart.append(account)
    return chart


def dimension_kinds(dims: Dimensions) -> list[str]:
    """The dimension kinds a declaration can build as meant."""
    retired = dims.retired_values() or dims.retired_dimensions()
    return [k for k in DIMENSION_KINDS if k != "journal-dimension-out-of-use" or retired]


def accounts() -> list[dict[str, Any]]:
    """What the general ledger adds to the P1 chart (with the founders'): an inactive account and a group."""
    return [{"code": INACTIVE, "type": "expense", "group": False, "active": False},
            {"code": GROUP, "type": "expense", "group": True, "active": True}]


@dataclass
class Tracked:
    """What the generator knows of a journal or the opening balances, to build each correction to an intent."""

    id: str
    type: str  # "manual_journal" or "opening_balances"
    posted: bool
    version: int
    day: date  # its current entry's date
    reversed: bool = False


def _refused_content_kinds() -> list[str]:
    near = _NEAR_THE_LIMIT if BOUNDED.get() else ()
    return [k for k in _REJECTED_KINDS if k not in _DIRECTOR_KINDS and k not in near]


@st.composite
def _reverse_on(draw: st.DrawFn, day: date) -> date:
    """After ``day``: the first day of the next month (the screen's suggestion), or 1 to 90 days later."""
    if draw(st.booleans()):
        month = day.year * 12 + day.month  # the next month, counted from zero
        return date(month // 12, month % 12 + 1, 1)
    return day + timedelta(days=draw(st.integers(1, 90)))


@st.composite
def _content(draw: st.DrawFn, posting: bool, dims: Dimensions | None = None) -> tuple[list[dict], str]:
    """A journal's lines, built to post or to be refused by ENG-24 (or, with ``dims``, rule 11), and the intent."""
    kinds = _POSTING_KINDS if posting else _refused_content_kinds()
    if dims is not None:
        kinds = kinds + dims.possible(DIMENSION_POSTING if posting else DIMENSION_REJECTED)
    return draw(_journal(context(dims), draw(st.sampled_from(kinds))))


@st.composite
def journal(
    draw: st.DrawFn, doc_id: str, day: date, kind: str, dims: Dimensions | None = None
) -> tuple[dict[str, Any], str, str]:
    """A manual journal of ``kind``, its intent and the kind it was built as."""
    if kind in JOURNAL_DIMENSION_POSTING + JOURNAL_DIMENSION_REJECTED:
        assert dims is not None
        built = {"journal-dimension-move": ["dim-move"], "journal-dimension-required": ["dim-required-missing"],
                 "journal-dimension-out-of-use": dims.possible(["dim-value-out-of-use", "dim-dimension-out-of-use"])}
        lines, meant = draw(_journal(context(dims), draw(st.sampled_from(built[kind]))))
        doc = {"type": "manual_journal", "id": doc_id, "version": 1, "date": day.isoformat(), "memo": "",
               "lines": lines}
        if draw(st.booleans()):  # its reversal, automatic, keeps each line's values
            doc["reverse_on"] = draw(_reverse_on(day)).isoformat()
        return doc, meant, kind
    lines, meant = draw(_content(kind not in ("journal-refused",), dims))
    doc: dict[str, Any] = {"type": "manual_journal", "id": doc_id, "version": 1, "date": day.isoformat(), "memo": "",
                           "lines": lines}
    if kind == "journal-reverses" or (kind == "journal-refused" and draw(st.booleans())):
        doc["reverse_on"] = draw(_reverse_on(day)).isoformat()
    if kind == "journal-reverse-on-not-after":  # JOURNAL-11: on or before its own date
        doc["reverse_on"] = (day - timedelta(days=draw(st.integers(0, 30)))).isoformat()
        meant = REJECT
    if kind == "journal-refused" and meant == POST:  # pragma: no cover - the refused kinds above all refuse
        kind = "journal"
    return doc, meant, kind


def _line(account: str, amount: Decimal, currency: str = BASE) -> dict[str, Any]:
    return {"account": account, "currency": currency, "amount": money.fmt(amount, CURRENCIES[currency])}


@st.composite
def _amount(draw: st.DrawFn) -> Decimal:
    """A non-zero signed amount in cents, up to seven integer digits."""
    size = draw(st.integers(1, 9))
    units = draw(st.integers(10 ** (size - 1), 10**size - 1))
    return Decimal(-units if draw(st.booleans()) else units).scaleb(-2, context=money.EXACT)


@st.composite
def _opening_lines(
    draw: st.DrawFn, kind: str, dims: Dimensions | None = None
) -> tuple[list[dict], str, list[tuple[str, str, Decimal]]]:
    """The opening balances' lines of ``kind``, the intent, and the director's balances credited when they post:
    (person, account, what the company owes them)."""
    pool = OPENING_ACCOUNTS + (list(DIMENSION_ACCOUNTS) if dims is not None else [])
    drawn = draw(st.lists(st.sampled_from(pool), min_size=1, max_size=5, unique=True))
    plug = draw(st.sampled_from([a for a in pool if a not in drawn]))
    lines = [_line(account, draw(_amount())) for account in drawn]
    credits: list[tuple[str, str, Decimal]] = []
    pairs = [(p, a) for p in PEOPLE for a in DIRECTORS]
    for person, account in draw(st.lists(st.sampled_from(pairs), max_size=2, unique=True)):
        owed = draw(_amount()).copy_abs()
        lines.append({**_line(account, money.negate(owed)), "party": person, "origin": draw(st.sampled_from(ORIGINS))})
        credits.append((person, account, owed))
    rest = money.negate(money.total([Decimal(ln["amount"]) for ln in lines]))
    assume(not rest.is_zero())
    lines.insert(draw(st.integers(0, len(lines))), _line(plug, rest))
    if dims is not None:  # values named freely: the opening balances are not held to what an account requires
        tag(draw, dims, lines, required=False)
        ordinary = [i for i, ln in enumerate(lines) if "party" not in ln and abs(Decimal(ln["amount"])) > Decimal("0.01")]
        if ordinary and draw(st.booleans()):  # one balance split between two sets of values, kept apart
            i = draw(st.sampled_from(ordinary))
            whole = Decimal(lines[i]["amount"])
            cents = int(whole.copy_abs().scaleb(2, context=money.EXACT))
            part = Decimal(draw(st.integers(1, cents - 1))).scaleb(-2, context=money.EXACT).copy_sign(whole)
            other = {**lines[i], "amount": money.fmt(money.EXACT.subtract(whole, part), 2)}
            other.pop("dimensions", None)
            tag(draw, dims, [other], required=False)
            lines[i]["amount"] = money.fmt(part, 2)
            lines.insert(draw(st.integers(0, len(lines))), other)
    if kind == "opening-balances":
        return lines, POST, credits
    ordinary = [i for i, ln in enumerate(lines) if "party" not in ln]  # the plug at least
    i = draw(st.sampled_from(ordinary))
    if kind == "opening-dimension-out-of-use":  # DIM-06 holds the opening balances too
        assert dims is not None
        name, code = draw(st.sampled_from(dims.retired_values() + dims.retired_dimensions()))
        lines[i].setdefault("dimensions", {})[name] = code
    elif kind == "opening-unbalanced":  # a cent off: they do not net to zero
        value = Decimal(lines[i]["amount"])
        cent = Decimal("0.01")
        nudged = money.EXACT.add(value, cent) if value > 0 else money.EXACT.subtract(value, cent)
        lines[i]["amount"] = money.fmt(nudged, 2)
    elif kind == "opening-foreign":  # LEDGER-17: a balance in another currency, with its rate
        currency = draw(st.sampled_from(["USD", "KWD"]))
        lines[i]["currency"] = currency
        lines[i]["fx_rate"], lines[i]["fx_rate_source"] = "1", "document"
    elif kind == "opening-rate":  # ENG-03: a rate on a base-currency line
        lines[i]["fx_rate"], lines[i]["fx_rate_source"] = draw(st.sampled_from(["1", "1.25"])), "base"
    elif kind == "opening-zero-line":
        zero = {"account": draw(st.sampled_from(OPENING_ACCOUNTS)), "currency": BASE,
                "amount": draw(st.sampled_from(["0.00", "0", "-0"]))}
        lines.insert(draw(st.integers(0, len(lines))), zero)
    elif kind == "opening-director-without-person":  # rule 9, both ways round
        broken = draw(st.sampled_from(["none", "stranger", "no-origin", "on-another-line"]))
        if broken == "on-another-line":
            lines[i]["party"], lines[i]["origin"] = draw(st.sampled_from(PEOPLE)), "other"
        else:
            lines[i]["account"] = draw(st.sampled_from(DIRECTORS))
            if broken == "stranger":
                lines[i]["party"], lines[i]["origin"] = STRANGER, "lent"
            elif broken == "no-origin":
                lines[i]["party"] = draw(st.sampled_from(PEOPLE))
    elif kind == "opening-inactive":
        lines[i]["account"] = INACTIVE
    elif kind == "opening-group":
        lines[i]["account"] = GROUP
    else:  # pragma: no cover - a kind without a branch is a generator bug
        raise AssertionError(kind)
    return lines, REJECT, []


def opening_kinds(dims: Dimensions | None) -> list[str]:
    """The opening balances' kinds; with ``dims``, a value out of use refuses them too, when there is one."""
    if dims is None or not (dims.retired_values() or dims.retired_dimensions()):
        return OPENING_KINDS
    return OPENING_KINDS + OPENING_DIMENSION_REJECTED


@st.composite
def opening_balances(
    draw: st.DrawFn, doc_id: str, start: date, kind: str, dims: Dimensions | None = None
) -> tuple[dict[str, Any], str, list[tuple[str, str, Decimal]]]:
    """The opening balances of ``kind`` at the books' start, their intent and what they credit each person."""
    lines, meant, credits = draw(_opening_lines(kind, dims))
    doc = {"type": "opening_balances", "id": doc_id, "version": 1, "date": start.isoformat(), "memo": "",
           "lines": lines}
    return doc, meant, credits


@st.composite
def _edit_day(draw: st.DrawFn, current: date, start: date, other_month: bool) -> date:
    """The new version's date: the entry's own, or up to 45 days either side, or in another month; never before
    the books' start."""
    if other_month:
        later = current + timedelta(days=draw(st.integers(32, 70)))
        earlier = current - timedelta(days=draw(st.integers(32, 70)))
        return earlier if earlier >= start and draw(st.booleans()) else later
    if draw(st.booleans()):
        return current
    return max(start, current + timedelta(days=draw(st.integers(-45, 45))))


@st.composite
def corrections(
    draw: st.DrawFn, tracked: list[Tracked], start: date, dims: Dimensions | None = None
) -> tuple[list[dict[str, Any]], list[tuple[str, str, str]]]:
    """The items of ``reversals`` and, for each, (its key, what it was meant to do, the kind built)."""
    items: list[dict[str, Any]] = []
    meant: list[tuple[str, str, str]] = []
    for _ in range(draw(st.integers(0, min(8, 2 * len(tracked) + 1)))):
        kind = draw(st.sampled_from(CORRECTION_KINDS))
        live = [t for t in tracked if t.type == "manual_journal" and t.posted and not t.reversed]
        voided = [t for t in tracked if t.reversed]
        refused = [t for t in tracked if not t.posted]
        opening = [t for t in tracked if t.type == "opening_balances" and t.posted]
        pool = {"edit-of-reversed": voided, "void-twice": voided, "edit-of-refused": refused,
                "opening-edit": opening, "opening-edit-refused": opening, "opening-edit-version-skips": opening,
                "edit-of-unknown": [None]}.get(kind, live)
        if not pool:  # nothing to correct that way: an edit of a posted journal, or of none
            kind, pool = ("edit", live) if live else ("edit-of-unknown", [None])
        target = draw(st.sampled_from(pool))
        if kind.startswith("void"):
            assert target is not None
            if kind == "void-before-entry":
                day = target.day - timedelta(days=draw(st.integers(1, 20)))
            else:
                day = target.day + timedelta(days=draw(st.integers(0, 75)))
            items.append({"document": target.id, "date": day.isoformat()})
            if kind == "void":
                target.reversed = True
            meant.append((f"{target.id}/reversal", POST if kind == "void" else REJECT, kind))
            continue

        doc_id = target.id if target is not None else UNKNOWN
        version = target.version + 1 if target is not None else 2
        if kind in ("edit-version-skips", "opening-edit-version-skips"):
            assert target is not None
            version = draw(st.sampled_from([target.version, target.version + 2]))
        if target is not None and target.type == "opening_balances":
            # The opening balances' edit: at the books' start, never reversing by itself.
            refusals = [k for k in opening_kinds(dims) if k in OPENING_REJECTED + OPENING_DIMENSION_REJECTED]
            lines, intent, _ = draw(_opening_lines(
                draw(st.sampled_from(refusals)) if kind == "opening-edit-refused" else "opening-balances", dims))
            content: dict[str, Any] = {"version": version, "date": start.isoformat(), "memo": "", "lines": lines}
        else:
            current = target.day if target is not None else start
            day = draw(_edit_day(current, start, kind == "edit-other-month"))
            lines, intent = draw(_content(kind != "edit-content-refused", dims))
            content = {"version": version, "date": day.isoformat(), "memo": "", "lines": lines}
            if kind == "edit-reverses" or (kind not in ("edit", "edit-other-month") and draw(st.booleans())):
                content["reverse_on"] = draw(_reverse_on(day)).isoformat()
            if kind == "edit-reverse-on-not-after":  # JOURNAL-11, on the new version
                content["reverse_on"] = (day - timedelta(days=draw(st.integers(0, 30)))).isoformat()
        items.append({"document": doc_id, "edit": content})
        posts = kind in CORRECTIONS_POSTING
        assert intent == POST or not posts  # content built to post, for every kind that posts
        meant.append((f"{doc_id}/edit", POST if posts else REJECT, kind))
        if posts:
            assert target is not None
            target.version = version
            target.day = date.fromisoformat(content["date"])
    return items, meant


@st.composite
def dimension_document(draw: st.DrawFn, doc_id: str, day: date, dims: Dimensions) -> tuple[dict[str, Any], str]:
    """An invoice, credit note, bill, vendor credit, bank line or expense claim in SGD with a line on 6200 (money out)
    or 4200 (money in), a spread line half the time where the document may spread, and its intent: refused when the
    account requires a dimension in use, since the format's P1 documents carry no values (DIM-05); posted otherwise."""
    shape = draw(st.sampled_from(["bill", "vendor_credit", "invoice", "credit_note", "bank-out", "bank-in",
                                  "expense_claim"]))
    out = shape in ("bill", "vendor_credit", "bank-out", "expense_claim")
    account = next(code for code, kind in DIMENSION_ACCOUNTS.items() if kind == ("expense" if out else "revenue"))
    meant = REJECT if dims.required(account) else POST
    amount = Decimal(draw(st.integers(1, 10**9))).scaleb(-2, context=money.EXACT)
    line: dict[str, Any] = {"account": account, "amount": money.fmt(amount, 2),
                            "gst_code": draw(st.sampled_from(["NR", "OP"] if out else ["ZR", "ES", "OS"]))}
    if shape in ("bill", "invoice", "bank-out", "bank-in") and draw(st.booleans()):
        line["spread_months"] = draw(st.sampled_from([1, 3, 12]))  # held to it by its own account
    head = {"type": shape, "id": doc_id, "version": 1, "date": day.isoformat()}
    if shape.startswith("bank"):
        signed = money.fmt(amount if shape == "bank-in" else money.negate(amount), 2)
        return {**head, "type": "bank_transaction", "account": "1010", "currency": BASE, "amount": signed,
                "parts": [{**line, "amount": signed}]}, meant
    lines = [line]
    if draw(st.booleans()):  # beside a line on an account that requires nothing
        lines.insert(draw(st.integers(0, 1)), {"account": "6010" if out else "4010", "amount": "10.00",
                                               "gst_code": line["gst_code"]})
    return {**head, "currency": BASE, "lines": lines}, meant


def corrections_last_day(items: list[dict[str, Any]]) -> date | None:
    """The latest date the corrections name (a void's, or an edit's new version's): ``as_of`` is on or after it."""
    days = [date.fromisoformat(item["date"] if "date" in item else item["edit"]["date"]) for item in items]
    return max(days) if days else None
