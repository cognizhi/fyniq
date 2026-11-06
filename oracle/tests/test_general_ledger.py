"""The general ledger's shapes: opening balances, automatic reversals and edits (QA-ENG-01; task 8.1).

test/differential/README.md, "Opening balances, automatic reversals and edits", written from FR-GL-02 (rev 0.55),
FR-GL-06 (rev 0.49, 0.66), FR-GL-12 (rev 0.24, 0.54, 0.58), §10.2, INV-13 and ENG-24. Every figure is worked by
hand beside it; the properties check the generator's intent and the shapes' invariants.
"""

import copy
from collections import defaultdict
from decimal import Decimal

import pytest
from builders import P1_ACCOUNTS, P1_SETTINGS, journal, line, p1_scenario, scenario
from hypothesis import HealthCheck, Phase, find, given, settings

from oracle import money
from oracle.book import PostedLine
from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, scenario_from_json
from oracle.strategies import POST, REJECT
from oracle.strategies_gl import (
    CORRECTIONS_POSTING,
    CORRECTIONS_REJECTED,
    JOURNAL_POSTING,
    JOURNAL_REJECTED,
    OPENING_POSTING,
    OPENING_REJECTED,
    REJECTED_KINDS,
)
from oracle.strategies_p1 import p1_scenarios, p1_scenarios_with_intent

START = "2026-01-01"
SETTINGS = {**P1_SETTINGS, "books_start": START}


def balances(*lines: dict, doc_id: str = "OB", day: str = START, version: int = 1) -> dict:
    return {"type": "opening_balances", "id": doc_id, "version": version, "date": day, "memo": "", "lines": list(lines)}


def loan(amount: str, party: str | None = "P0001", origin: str | None = "lent") -> dict:
    out = line("2500", "SGD", amount)
    if party is not None:
        out["party"] = party
    if origin is not None:
        out["origin"] = origin
    return out


def edit(document: str, version: int, day: str, *lines: dict, reverse_on: str | None = None) -> dict:
    content = {"version": version, "date": day, "memo": "", "lines": list(lines)}
    if reverse_on is not None:
        content["reverse_on"] = reverse_on
    return {"document": document, "edit": content}


def void(document: str, day: str) -> dict:
    return {"document": document, "date": day}


def reversing(doc_id: str, day: str, reverse_on: str, amount: str = "1200.00") -> dict:
    doc = journal(doc_id, day, line("6010", "SGD", amount), line("2010", "SGD", f"-{amount}"))
    doc["reverse_on"] = reverse_on
    return doc


def run(*documents: dict, corrections: tuple = (), as_of: str = "2026-06-30") -> tuple[dict, Ledger]:
    data = p1_scenario(*documents, as_of=as_of, settings=SETTINGS)
    data["reversals"] = list(corrections)
    ledger = Ledger(scenario_from_json(data))
    return ledger.result(), ledger


def entries(result: dict) -> list[tuple[str, str, str]]:
    return [(e["document"], e["kind"], e["posting_date"]) for e in result["entries"]]


def lines(result: dict, document: str, kind: str = "post") -> list[tuple[str, str, str, str]]:
    entry = next(e for e in result["entries"] if e["document"] == document and e["kind"] == kind)
    return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in entry["lines"]]


OPENING = balances(line("1010", "SGD", "25000.00"), line("3010", "SGD", "-10000.00"), loan("-8000.00"),
                   line("2999", "SGD", "-7000.00"))


# ---------------------------------------------------------------------------- opening balances
# req: FR-GL-06, INV-13
def test_opening_balances_post_at_the_books_start_in_canonical_form_share_capital_included():
    result, _ = run(OPENING)
    assert result["rejected"] == []
    assert entries(result) == [("OB", "post", START)]
    assert lines(result, "OB") == [("1010", "SGD", "25000.00", "25000.00"), ("2500", "SGD", "-8000.00", "-8000.00"),
                                   ("2999", "SGD", "-7000.00", "-7000.00"), ("3010", "SGD", "-10000.00", "-10000.00")]
    assert result["people"] == {"P0001": {"2500": "-8000.00"}}


# req: FR-GL-06
def test_balances_on_one_account_net_into_one_and_income_and_costs_may_open():
    # A books' start part-way through a year (P1 readings): 4010 and 6010 carry the year so far. Two lines on 1010
    # are one balance, 1,500.00.
    result, _ = run(balances(line("1010", "SGD", "1000.00"), line("4010", "SGD", "-3000.00"),
                             line("1010", "SGD", "500.00"), line("6010", "SGD", "1500.00")))
    assert lines(result, "OB") == [("1010", "SGD", "1500.00", "1500.00"), ("4010", "SGD", "-3000.00", "-3000.00"),
                                   ("6010", "SGD", "1500.00", "1500.00")]


# req: FR-GL-06, ENG-24, ENG-03, FR-EQ-01
@pytest.mark.parametrize(
    ("change", "reason"),
    [
        (lambda ls: ls[3].update(amount="-7000.01"), "unbalanced-base"),  # the balance check
        (lambda ls: ls[0].update(currency="USD", fx_rate="1", fx_rate_source="document"),
         "opening-balance-not-in-base"),  # LEDGER-17
        (lambda ls: ls[0].update(fx_rate="1", fx_rate_source="base"), "rate-on-base-line"),  # ENG-03
        (lambda ls: ls.append(line("6010", "SGD", "0.00")), "zero-line"),
        (lambda ls: ls[2].pop("party"), "director-line-without-person"),  # FR-EQ-01: every line on 2500
        (lambda ls: ls[2].pop("origin"), "director-line-without-person"),
        (lambda ls: ls[0].update(party="P0001"), "person-on-another-account"),
        (lambda ls: ls[0].update(account="1090"), "inactive-account"),
        (lambda ls: ls[0].update(account="4000"), "group-account"),
        (lambda ls: ls[0].update(account="9999"), "unknown-account"),
        (lambda ls: ls[0].update(amount="25000.001"), "amount-too-many-decimals"),
        (lambda ls: ls.__delitem__(slice(1, None)) or ls.append(line("1010", "SGD", "-25000.00")),
         "fewer-than-two-lines"),  # 25,000.00 and -25,000.00 on 1010: one balance of nothing
        (lambda ls: ls.__delitem__(slice(1, None)), "fewer-than-two-lines"),
    ],
)
def test_opening_balances_that_break_a_rule_are_refused_whole(change, reason):
    doc = copy.deepcopy(OPENING)
    change(doc["lines"])
    result, ledger = run(doc)
    assert result["rejected"] == ["OB"] and result["entries"] == []
    assert ledger.reasons == [("OB", reason)]


# req: FR-GL-06, FR-GL-12, INV-13
@pytest.mark.parametrize(
    ("change", "match"),
    [
        (lambda d: d["documents"].append(balances(line("1010", "SGD", "1.00"), line("2999", "SGD", "-1.00"),
                                                  doc_id="OB-2")), "organisation has one set"),
        (lambda d: d["settings"].pop("books_start"), "need settings/books_start"),
        (lambda d: d.pop("settings"), "needs settings"),
        (lambda d: d.pop("as_of"), "needs as_of"),
        (lambda d: d["documents"][0].update(date="2026-01-02"), "dated the books' start"),
        (lambda d: d.update(statements=True, accounts=[*d["accounts"], {
            "code": "3100", "type": "equity", "group": False, "active": True, "role": "retained-earnings"}]),
         "opening balances are not in the format"),
        (lambda d: d["reversals"].append(void("OB", "2026-01-05")), "a reversal names a manual journal"),
        (lambda d: d["reversals"].append(edit("OB", 2, "2026-01-02", line("1010", "SGD", "1.00"),
                                              line("2999", "SGD", "-1.00"))), "dated the books' start"),
        (lambda d: d["reversals"].append(edit("OB", 2, START, line("1010", "SGD", "1.00"), line("2999", "SGD", "-1.00"),
                                              reverse_on="2026-02-01")), "do not reverse by themselves"),
        (lambda d: d["documents"][0].update(reverse_on="2026-02-01"), "reverse_on"),
    ],
)
def test_opening_balances_outside_the_format_make_the_scenario_invalid(change, match):
    data = copy.deepcopy(p1_scenario(OPENING, as_of="2026-06-30", settings=SETTINGS))
    change(data)
    with pytest.raises(ScenarioError, match=match):
        scenario_from_json(data)


# ---------------------------------------------------------------------------- automatic reversals
# req: FR-GL-02, ENG-05, ENG-14
def test_a_journal_reverses_by_itself_on_its_day_once_that_day_has_come():
    usd = journal("JNL-USD", "2026-03-31", line("6010", "USD", "100.00", "1.3472"), line("2010", "SGD", "-134.72"))
    usd["reverse_on"] = "2026-04-01"
    result, _ = run(reversing("JNL-1", "2026-01-31", "2026-02-01"), reversing("JNL-2", "2026-01-31", "2026-07-01"),
                    usd, as_of="2026-04-01")
    assert result["rejected"] == []
    assert entries(result) == [("JNL-1", "post", "2026-01-31"), ("JNL-2", "post", "2026-01-31"),
                               ("JNL-USD", "post", "2026-03-31"),
                               ("JNL-1", "auto_reversal", "2026-02-01"),  # JNL-2's 1 July is after as_of: it waits
                               ("JNL-USD", "auto_reversal", "2026-04-01")]  # as_of itself is "today": it posts
    assert lines(result, "JNL-1", "auto_reversal") == [("6010", "SGD", "-1200.00", "-1200.00"),
                                                       ("2010", "SGD", "1200.00", "1200.00")]
    # Negated as stored, never re-converted (ENG-14): 100.00 × 1.3472 = 134.72.
    assert lines(result, "JNL-USD", "auto_reversal") == [("6010", "USD", "-100.00", "-134.72"),
                                                         ("2010", "SGD", "134.72", "134.72")]
    assert result["trial_balance"]["2026-02"] == {"2010": "1200.00", "6010": "-1200.00"}


# req: FR-GL-02
def test_a_reverse_on_on_or_before_the_journals_date_refuses_the_journal():
    result, ledger = run(reversing("JNL-1", "2026-01-31", "2026-01-31"), reversing("JNL-2", "2026-01-31", "2026-01-30"))
    assert result["rejected"] == ["JNL-1", "JNL-2"] and result["entries"] == []
    assert {reason for _, reason in ledger.reasons} == {"reverse-on-not-after-date"}  # JOURNAL-11


# req: FR-GL-02, FR-GL-12
def test_a_voided_journal_does_not_reverse_by_itself_whatever_the_voids_date():
    # The scheduler runs after every correction: the void (even after reverse_on) took the one reversal there is.
    result, _ = run(reversing("JNL-1", "2026-01-31", "2026-02-01"), reversing("JNL-2", "2026-01-31", "2026-02-01"),
                    corrections=(void("JNL-1", "2026-01-31"), void("JNL-2", "2026-02-15")))
    assert result["rejected"] == []
    assert entries(result) == [("JNL-1", "post", "2026-01-31"), ("JNL-2", "post", "2026-01-31"),
                               ("JNL-1", "reversal", "2026-01-31"), ("JNL-2", "reversal", "2026-02-15")]


# req: FR-GL-02, FR-GL-03
def test_automatic_reversals_sit_with_the_other_scheduled_entries_by_date_then_place():
    spread = {"type": "bank_transaction", "id": "BT-1", "version": 1, "date": "2026-01-15", "account": "1010",
              "currency": "SGD", "amount": "-300.00",
              "parts": [{"account": "6110", "amount": "-300.00", "gst_code": "OP", "spread_months": 3}]}
    result, _ = run(reversing("JNL-1", "2026-01-31", "2026-02-01"), spread, as_of="2026-03-01")
    assert entries(result)[2:] == [
        ("BT-1", "release", "2026-01-15"),
        ("JNL-1", "auto_reversal", "2026-02-01"),  # the same day as BT-1's release: the earlier place first
        ("BT-1", "release", "2026-02-01"),
        ("BT-1", "release", "2026-03-01"),
    ]


# req: FR-GL-02
def test_a_reverse_on_needs_as_of_even_with_manual_journals_only():
    data = scenario(reversing("JNL-1", "2026-01-31", "2026-02-01"))
    with pytest.raises(ScenarioError, match="reverses by itself, which needs as_of"):
        scenario_from_json(data)
    data["as_of"] = "2026-01-30"  # before the journal's date
    with pytest.raises(ScenarioError, match="as_of"):
        scenario_from_json(data)
    data["as_of"] = "2026-02-01"
    assert entries(Ledger(scenario_from_json(data)).result())[-1] == ("JNL-1", "auto_reversal", "2026-02-01")


# ---------------------------------------------------------------------------- edits
# req: FR-GL-12
def test_an_edit_reverses_the_entry_on_its_own_date_and_posts_the_new_version_on_its_date():
    # Edits need no as_of with manual journals only. 300.00 in January becomes 330.00 in February: January is made
    # right by the reversal, February takes the new version (rev 0.58).
    data = scenario(journal("JNL-1", "2026-01-10", line("6010", "SGD", "300.00"), line("1010", "SGD", "-300.00")))
    data["reversals"] = [edit("JNL-1", 2, "2026-02-10", line("6010", "SGD", "330.00"), line("1010", "SGD", "-330.00"))]
    result = Ledger(scenario_from_json(data)).result()
    assert result["rejected"] == []
    assert entries(result) == [("JNL-1", "post", "2026-01-10"), ("JNL-1", "reversal", "2026-01-10"),
                               ("JNL-1", "replacement", "2026-02-10")]
    assert result["trial_balance"] == {"2026-01": {}, "2026-02": {"1010": "-330.00", "6010": "330.00"}}


# req: FR-GL-12, FR-GL-02
def test_after_an_edit_the_replacement_is_what_a_void_or_the_scheduler_reverses():
    result, ledger = run(
        reversing("JNL-1", "2026-01-31", "2026-02-01"),
        journal("JNL-2", "2026-01-10", line("6010", "SGD", "300.00"), line("1010", "SGD", "-300.00")),
        corrections=(
            edit("JNL-1", 2, "2026-01-31", line("6010", "SGD", "1250.00"), line("2010", "SGD", "-1250.00"),
                 reverse_on="2026-03-01"),
            edit("JNL-2", 2, "2026-02-10", line("6010", "SGD", "330.00"), line("1010", "SGD", "-330.00")),
            void("JNL-2", "2026-02-05"),  # before the replacement: refused
            void("JNL-2", "2026-02-10"),
        ),
    )
    assert result["rejected"] == ["JNL-2/reversal"]
    assert ledger.reasons == [("JNL-2/reversal", "reversal-before-entry")]
    assert lines(result, "JNL-1", "auto_reversal") == [("6010", "SGD", "-1250.00", "-1250.00"),
                                                       ("2010", "SGD", "1250.00", "1250.00")]
    assert entries(result)[-1] == ("JNL-1", "auto_reversal", "2026-03-01")  # version 2's day, not version 1's
    assert [e for e in entries(result) if e[0] == "JNL-2"][-1] == ("JNL-2", "reversal", "2026-02-10")


# req: FR-GL-12, FR-GL-02
def test_an_edit_without_reverse_on_takes_the_automatic_reversal_away():
    result, _ = run(reversing("JNL-1", "2026-01-31", "2026-02-01"),
                    corrections=(edit("JNL-1", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00")),))
    assert [kind for _, kind, _ in entries(result)] == ["post", "reversal", "replacement"]


# req: FR-GL-12, FR-GL-02, INV-13, ENG-24
@pytest.mark.parametrize(
    ("correction", "reason"),
    [
        (edit("JNL-9", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00")),
         "edit-of-unposted-document"),
        (edit("JNL-X", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00")),
         "edit-of-unposted-document"),  # it was refused
        (edit("JNL-1", 3, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00")),
         "version-does-not-follow"),
        (edit("JNL-1", 1, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00")),
         "version-does-not-follow"),
        (edit("JNL-1", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.01")), "unbalanced-base"),
        (edit("JNL-1", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("3010", "SGD", "-1.00")),
         "share-capital-account"),  # INV-13: only the opening balances and the register reach share capital
        (edit("JNL-1", 2, "2026-01-31", line("6010", "SGD", "1.00"), line("2010", "SGD", "-1.00"),
              reverse_on="2026-01-31"), "reverse-on-not-after-date"),
    ],
)
def test_a_refused_edit_changes_nothing(correction, reason):
    refused = journal("JNL-X", "2026-01-31", line("6010", "SGD", "1.00"))
    result, ledger = run(reversing("JNL-1", "2026-01-31", "2026-02-01"), refused, corrections=(correction,))
    assert result["rejected"] == ["JNL-X", f"{correction['document']}/edit"]
    assert ledger.reasons[-1] == (f"{correction['document']}/edit", reason)
    # Version 1 stands and reverses by itself, as if no edit had been asked for.
    assert entries(result) == [("JNL-1", "post", "2026-01-31"), ("JNL-1", "auto_reversal", "2026-02-01")]


# req: FR-GL-12
def test_an_edit_of_a_voided_journal_is_refused_and_a_refused_edit_leaves_its_version_free():
    j = journal("JNL-1", "2026-01-10", line("6010", "SGD", "300.00"), line("1010", "SGD", "-300.00"))
    content = (line("6010", "SGD", "310.00"), line("1010", "SGD", "-310.00"))
    result, ledger = run(j, corrections=(
        edit("JNL-1", 2, "2026-01-10", line("6010", "SGD", "310.00"), line("1010", "SGD", "-310.01")),  # refused
        edit("JNL-1", 2, "2026-01-10", *content),  # version 2 is still the next
        edit("JNL-1", 3, "2026-01-11", *content),
        void("JNL-1", "2026-01-20"),
        edit("JNL-1", 4, "2026-01-21", *content),  # reversed: refused
    ))
    assert result["rejected"] == ["JNL-1/edit", "JNL-1/edit"]
    assert [reason for _, reason in ledger.reasons] == ["unbalanced-base", "edit-of-reversed-document"]
    assert [kind for _, kind, _ in entries(result)] == ["post", "reversal", "replacement", "reversal", "replacement",
                                                       "reversal"]
    assert result["trial_balance"] == {"2026-01": {}}


# req: FR-GL-06, FR-GL-12
def test_an_edit_of_the_opening_balances_posts_both_entries_at_the_books_start():
    corrected = (line("1010", "SGD", "25000.00"), line("3010", "SGD", "-10000.00"), loan("-8000.00"),
                 line("2999", "SGD", "-6500.00"), line("2010", "SGD", "-500.00"))
    result, ledger = run(OPENING, corrections=(
        edit("OB", 2, START, *corrected[:3], line("2999", "SGD", "-6500.00")),  # does not net to zero
        edit("OB", 2, START, line("1021", "USD", "100.00", "1.25"), line("2999", "SGD", "-125.00")),  # LEDGER-17
        edit("OB", 2, START, *corrected),
    ))
    assert result["rejected"] == ["OB/edit", "OB/edit"]
    assert [reason for _, reason in ledger.reasons] == ["unbalanced-base", "opening-balance-not-in-base"]
    assert entries(result) == [("OB", "post", START), ("OB", "reversal", START), ("OB", "replacement", START)]
    assert lines(result, "OB", "replacement") == [
        ("1010", "SGD", "25000.00", "25000.00"), ("2010", "SGD", "-500.00", "-500.00"),
        ("2500", "SGD", "-8000.00", "-8000.00"), ("2999", "SGD", "-6500.00", "-6500.00"),
        ("3010", "SGD", "-10000.00", "-10000.00")]
    assert result["trial_balance"]["2026-01"]["2999"] == "-6500.00"


# req: FR-GL-12
def test_an_edit_of_a_p1_document_makes_the_scenario_invalid():
    bank = {"type": "bank_transaction", "id": "BT-1", "version": 1, "date": "2026-01-15", "account": "1010",
            "currency": "SGD", "amount": "-10.00", "parts": [{"account": "6010", "amount": "-10.00", "gst_code": "OP"}]}
    data = p1_scenario(bank, settings=SETTINGS)
    data["reversals"] = [edit("BT-1", 2, "2026-01-15", line("6010", "SGD", "1.00"), line("1010", "SGD", "-1.00"))]
    with pytest.raises(ScenarioError, match="an edit names a manual journal or the opening balances"):
        scenario_from_json(data)
    data["reversals"] = [{"document": "BT-1", "date": "2026-01-15", "edit": data["reversals"][0]["edit"]}]
    with pytest.raises(ScenarioError, match="reversals"):  # a reversal or an edit, never both
        scenario_from_json(data)


# ---------------------------------------------------------------------------- the generator
_FIND = settings(max_examples=3000, database=None, phases=[Phase.generate], suppress_health_check=list(HealthCheck))


# req: QA-ENG-01, FR-GL-02, FR-GL-06, FR-GL-12
@given(p1_scenarios_with_intent(("general-ledger",)))
def test_each_document_and_correction_posts_or_is_refused_as_built(pair):
    data, intent = pair
    ledger = Ledger(scenario_from_json(data))
    for doc_id, meant in intent.documents.items():
        assert (doc_id in ledger.rejected) == (meant == REJECT), dict(ledger.reasons)
    refused = [key for key in ledger.rejected if "/" in key]
    assert refused == [key for key, meant, _ in intent.corrections if meant == REJECT], ledger.reasons


# req: FR-GL-12, FR-GL-02, ENG-14
@given(p1_scenarios(("general-ledger", "founders")))
def test_a_journal_is_posted_edited_and_reversed_at_most_once_each_reversal_negating_the_entry_before_it(data):
    parsed = scenario_from_json(data)
    by_document = defaultdict(list)
    for entry in Ledger(parsed).entries:
        by_document[entry.document].append(entry)
    journals = {d.id for d in parsed.documents if d.type in ("manual_journal", "opening_balances")}
    for doc_id in journals & set(by_document):
        kinds = [e.kind for e in by_document[doc_id]]
        assert kinds[0] == "post"
        rest = kinds[1:]
        if len(rest) % 2 == 1:  # reversed at the end: by a void or by the scheduler
            assert rest.pop() in ("reversal", "auto_reversal")
        assert rest == ["reversal", "replacement"] * (len(rest) // 2)
        current = by_document[doc_id][0]
        for entry in by_document[doc_id][1:]:
            if entry.kind == "replacement":
                current = entry
                continue
            assert entry.lines == tuple(PostedLine(ln.account, ln.currency, money.negate(ln.amount),
                                                   money.negate(ln.base_amount), ln.party, ln.dimensions)
                                        for ln in current.lines)  # each line keeps its values (FR-DIM-02)
            assert entry.posting_date >= current.posting_date
            if entry.kind == "auto_reversal":
                assert entry.posting_date <= parsed.as_of


# req: INV-13
@given(p1_scenarios(("general-ledger", "founders")))
def test_share_capital_is_reached_only_by_the_opening_balances_and_the_register_of_share_issues(data):
    parsed = scenario_from_json(data)
    share = {code for code, a in parsed.accounts.items() if a.role == "share-capital"}
    types = {d.id: d.type for d in parsed.documents}
    for entry in Ledger(parsed).entries:
        if any(ln.account in share for ln in entry.lines):
            assert types[entry.document] in ("opening_balances", "director_conversion"), entry


# req: FR-GL-06
@given(p1_scenarios(("general-ledger",)))
def test_the_opening_balances_are_in_the_base_currency_at_the_books_start_and_net_to_zero(data):
    parsed = scenario_from_json(data)
    for entry in Ledger(parsed).entries:
        if entry.document == "OB-0001":
            assert entry.posting_date == parsed.settings.books_start
            assert {ln.currency for ln in entry.lines} == {"SGD"}
            assert money.total([ln.amount for ln in entry.lines]) == Decimal(0)


# req: QA-ENG-02
@pytest.mark.parametrize(
    "kind",
    sorted(set(JOURNAL_POSTING + JOURNAL_REJECTED + OPENING_POSTING + OPENING_REJECTED + CORRECTIONS_POSTING
               + CORRECTIONS_REJECTED)),
)
def test_the_general_ledger_source_draws_each_of_its_kinds_as_meant(kind):
    def drawn(pair):
        _, intent = pair
        return kind in intent.kinds.values() or any(k == kind for _, _, k in intent.corrections)

    data, intent = find(p1_scenarios_with_intent(("general-ledger",)), drawn, settings=_FIND)
    ledger = Ledger(scenario_from_json(data))
    expected = REJECT if kind in REJECTED_KINDS else POST
    for doc_id, built in intent.kinds.items():
        if built == kind:
            assert intent.documents[doc_id] == expected
            assert (doc_id in ledger.rejected) == (expected == REJECT), dict(ledger.reasons)
    refused = [key for key in ledger.rejected if "/" in key]
    assert refused == [key for key, meant, _ in intent.corrections if meant == REJECT]
    assert all(meant == expected for _, meant, k in intent.corrections if k == kind)


# req: QA-ENG-01
def test_the_p1_chart_has_what_the_general_ledger_reaches():
    codes = {a["code"] for a in P1_ACCOUNTS}
    assert {"1010", "2010", "2500", "2999", "3010", "6010"} <= codes
