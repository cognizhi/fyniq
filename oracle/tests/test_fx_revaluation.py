"""The month-end revaluation (FR-GL-04), RND-RATE's quotes and the fx generator source (QA-ENG-01, QA-ENG-02).

Written from test/differential/README.md, "Month-end revaluation", which is written from PRD-001; every figure here is
worked out by hand in the comment beside it. The golden case (golden/fx-revaluation.json, moved from pending/) is worked out in
test_pending_golden.py.
"""

import copy
import json
from datetime import date, timedelta
from decimal import Decimal
from pathlib import Path

import pytest
from hypothesis import HealthCheck, Phase, find, given, settings

from oracle import money
from oracle.ledger import Ledger
from oracle.revaluation import revaluations
from oracle.scenario import ScenarioError, parse_json, scenario_from_json
from oracle.strategies import POST, REJECT
from oracle.strategies_p1 import FX_OWN, KINDS, p1_scenarios, p1_scenarios_with_intent

PENDING = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden" / "fx-revaluation.json"  # moved from pending/
_FIND = settings(database=None, max_examples=3000, phases=[Phase.generate],
                 suppress_health_check=list(HealthCheck), deadline=None)


def _golden() -> dict:
    return parse_json(PENDING.read_text(encoding="utf-8"))


def _result(data: dict) -> dict:
    return Ledger(scenario_from_json(data)).result()


def _lines(result: dict, document: str, kind: str) -> list[tuple]:
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == kind:
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"], ln.get("party")) for ln in e["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


# ---------------------------------------------------------------------------- RND-RATE
# req: RND-RATE
@pytest.mark.parametrize(("quote", "form", "stored"), [
    ("1.3472", "base-per-unit", "1.3472"),  # round(1.3472 ÷ 1, 10): already a stored rate
    ("0.9012", "base-per-100", "0.009012"),  # SGD per 100 yen
    ("1.234567895", "base-per-100", "0.0123456790"),  # 0.01234567895: exactly half of the 10th place, away from zero
    ("1.234567894", "base-per-100", "0.0123456789"),  # 0.01234567894: below half
    ("0.2265", "foreign-per-base", "4.4150110375"),  # 1 ÷ 0.2265 = 4.41501103752...
    ("0.7407", "foreign-per-base", "1.3500742541"),  # 1 ÷ 0.7407 = 1.35007425408...
    ("112.5", "foreign-per-base", "0.0088888889"),  # 1 ÷ 112.5 = 0.00888...
    ("8", "foreign-per-base", "0.125"),  # exact
])
def test_rnd_rate_stores_a_quote_as_base_per_one_unit(quote, form, stored):
    assert money.rnd_rate_quote(Decimal(quote), form) == Decimal(stored)


# req: RND-RATE
def test_the_rate_a_base_currency_payment_implies():
    # rev 0.77: S$400.00 paid for USD 300.00 implies round(1.3333..., 10) = 1.3333333333; S$2.00 for USD 3.00 is
    # 0.6666666667 (half away from zero at the 10th place: ...66|66 rounds up).
    assert money.rnd_rate(Decimal("400.00"), Decimal("300.00")) == Decimal("1.3333333333")
    assert money.rnd_rate(Decimal("2.00"), Decimal("3.00")) == Decimal("0.6666666667")


# ---------------------------------------------------------------------------- the scenario's rules
def _with(change) -> dict:
    data = _golden()
    change(data)
    return data


def _account(data: dict, code: str) -> dict:
    return next(a for a in data["accounts"] if a["code"] == code)


# req: FR-GL-04, FR-COA-02
@pytest.mark.parametrize(("change", "match"), [
    (lambda d: _account(d, "4010").update(revalue=True), "only an asset or a liability"),
    (lambda d: _account(d, "7040").update(revalue=True), "only an asset or a liability"),
    (lambda d: (d["accounts"].append({"code": "1210", "type": "asset", "group": False, "active": True, "revalue": True}),
                d["settings"]["accounts"].update(prepayments="1210")), "never revalued"),
    (lambda d: d["settings"]["accounts"].pop("unrealised_fx"), "unrealised_fx"),
    (lambda d: d["settings"]["accounts"].update(unrealised_fx="1010"), "revenue or expense"),
    (lambda d: _account(d, "7040").update(active=False), "revenue or expense"),
    (lambda d: _account(d, "7040").update(role="depreciation"), "revenue or expense"),
    (lambda d: d.pop("as_of"), "as_of"),
    (lambda d: d.update(statements=True, accounts=[*d["accounts"], {"code": "3100", "type": "equity", "group": False,
                                                                  "active": True, "role": "retained-earnings"}]),
     "statements"),
    (lambda d: d["settings"]["period_end_rates"][0].update(date="2026-03-30"), "last day"),
    (lambda d: d["settings"]["period_end_rates"][0].update(date="2026-02-30"), "calendar date"),
    (lambda d: d["settings"]["period_end_rates"][0].update(currency="SGD"), "base currency"),
    (lambda d: d["settings"]["period_end_rates"][0].update(currency="EUR"), "not in currencies"),
    (lambda d: d["settings"]["period_end_rates"].append(dict(d["settings"]["period_end_rates"][0])), "second rate"),
    (lambda d: d["settings"]["period_end_rates"][0].update(quote="1.30", form="base-per-unit"), "period_end_rates"),
    (lambda d: d["settings"]["period_end_rates"][1].pop("form"), "period_end_rates"),
    (lambda d: d["settings"]["period_end_rates"][1].update(form="base-per-1000"), "period_end_rates"),
    (lambda d: d["settings"]["period_end_rates"][0].pop("fx_rate_source"), "period_end_rates"),
    (lambda d: d["settings"]["period_end_rates"][0].update(fx_rate="1.3e0"), "not a rate"),
    (lambda d: d["settings"]["period_end_rates"][0].update(fx_rate="0"), "not a rate"),
    (lambda d: d["settings"]["period_end_rates"][0].update(fx_rate="1.12345678901"), "not a rate"),
    # 0.0000000049 SGD per 100 yen is 0.000000000049, 0 once rounded to 10 places; 1 ÷ 0.0000000001 is 10^10.
    (lambda d: d["settings"]["period_end_rates"][1].update(quote="0.0000000049"), "not a rate"),
    (lambda d: d["settings"]["period_end_rates"][3].update(quote="0.0000000001"), "not a rate"),
])
def test_what_makes_a_revaluation_scenario_invalid(change, match):
    with pytest.raises(ScenarioError, match=match):
        scenario_from_json(_with(change))


# req: FR-GL-04
def test_a_scenario_without_revalued_accounts_is_unchanged():
    # Period-end rates alone revalue nothing; without them the result is the documents' alone.
    data = _with(lambda d: [a.pop("revalue", None) for a in d["accounts"]])
    result = _result(data)
    assert not any(e["kind"].startswith("revaluation") for e in result["entries"])
    data["settings"].pop("period_end_rates")
    assert _result(data) == result


# ---------------------------------------------------------------------------- as_of and the schedule
# req: FR-GL-04, ENG-05
def test_as_of_on_the_months_last_day_posts_its_revaluation_but_not_its_reversal():
    def until(day: str):
        def change(d: dict) -> None:
            d["as_of"] = day
            d["documents"] = [doc for doc in d["documents"] if doc["date"] <= day]
        return change

    kinds = [(e["document"], e["kind"]) for e in _result(_with(until("2026-03-31")))["entries"]]
    assert kinds[-1] == ("revaluation-2026-03", "revaluation")
    assert ("revaluation-2026-03", "revaluation_reversal") not in kinds
    kinds = [(e["document"], e["kind"]) for e in _result(_with(until("2026-03-30")))["entries"]]
    assert not any(kind.startswith("revaluation") for _, kind in kinds)  # March has not ended
    kinds = [(e["document"], e["kind"]) for e in _result(_with(until("2026-04-01")))["entries"]]
    assert kinds[-2:] == [("revaluation-2026-03", "revaluation"), ("revaluation-2026-03", "revaluation_reversal")]


# req: FR-GL-04
def test_a_missing_rate_holds_the_whole_month():
    # Without the JPY rate for 31 March, JPY 100,000 on 1022 has no rate: March posts nothing, USD included.
    data = _with(lambda d: d["settings"]["period_end_rates"].pop(1))
    entries = [(e["document"], e["kind"]) for e in _result(data)["entries"]]
    assert ("revaluation-2026-03", "revaluation") not in entries
    assert ("revaluation-2026-04", "revaluation") in entries  # April is revalued on its own


# req: FR-GL-04, ENG-41, INV-05
def test_g4_039_at_account_and_document_level():
    # G4-039 with only 1100 revalued: USD 10,000.00 at 1.35; 31 March at 1.30: Dr 7040 500.00, Cr 1100 500.00 (USD
    # 0.00), reversed on 1 April; 15 April USD 4,000.00 at 1.32: Dr bank 5,280.00, Dr 7030 120.00, Cr 1100 5,400.00;
    # 30 April at 1.33: USD 6,000.00 carried at 8,100.00 is 7,980.00: Dr 7040 120.00, Cr 1100 120.00.
    data = {
        "schema": "fyniq-scenario@1", "name": "g4-039", "base_currency": "SGD", "currencies": {"SGD": 2, "USD": 2},
        "as_of": "2026-04-30",
        "settings": {"accounts": {"receivable": "1100", "fx": "7030", "unrealised_fx": "7040"},
                     "money_accounts": {"1021": "USD"}, "gst": {"rates": []},
                     "period_end_rates": [{"date": "2026-03-31", "currency": "USD", "fx_rate": "1.30", "fx_rate_source": "t"},
                                          {"date": "2026-04-30", "currency": "USD", "fx_rate": "1.33", "fx_rate_source": "t"}]},
        "accounts": [{"code": "1021", "type": "asset", "group": False, "active": True},
                     {"code": "1100", "type": "asset", "group": False, "active": True, "revalue": True},
                     {"code": "4010", "type": "revenue", "group": False, "active": True},
                     {"code": "7030", "type": "expense", "group": False, "active": True},
                     {"code": "7040", "type": "expense", "group": False, "active": True}],
        "documents": [
            {"type": "invoice", "id": "INV-1", "version": 1, "date": "2026-03-10", "currency": "USD", "fx_rate": "1.35",
             "fx_rate_source": "document", "lines": [{"account": "4010", "amount": "10000.00", "gst_code": "OS"}]},
            {"type": "customer_receipt", "id": "RC-1", "version": 1, "date": "2026-04-15", "account": "1021",
             "currency": "USD", "fx_rate": "1.32", "fx_rate_source": "document",
             "parts": [{"document": "INV-1", "amount": "4000.00"}]}],
        "reversals": []}
    r = _result(data)
    assert _lines(r, "revaluation-2026-03", "revaluation") == [("1100", "USD", "0.00", "-500.00", None),
                                                               ("7040", "SGD", "500.00", "500.00", None)]
    assert _lines(r, "revaluation-2026-03", "revaluation_reversal") == [("1100", "USD", "0.00", "500.00", None),
                                                                        ("7040", "SGD", "-500.00", "-500.00", None)]
    assert _lines(r, "RC-1", "post") == [("1021", "USD", "4000.00", "5280.00", None),
                                         ("1100", "USD", "-4000.00", "-5400.00", None), ("7030", "SGD", "120.00", "120.00", None)]
    assert _lines(r, "revaluation-2026-04", "revaluation") == [("1100", "USD", "0.00", "-120.00", None),
                                                               ("7040", "SGD", "120.00", "120.00", None)]
    assert r["trial_balance"]["2026-04"]["1100"] == "-5020.00"  # 500.00 − 5,400.00 − 120.00: USD 6,000.00 at 7,980.00


# req: FR-GL-04, FR-EQ-01
def test_a_directors_account_is_revalued_person_by_person():
    # USD 100.00 lent by P0001 at 1.35 (135.00) and USD 50.00 by P0002 at 1.40 (70.00); 31 March at 1.30: P0001's
    # is −130.00, a gain of 5.00 (Dr 2500 5.00); P0002's −65.00, a gain of 5.00; 7040 −10.00. Each line names its person.
    def lent(doc_id: str, person: str, amount: str, rate: str, base: str) -> dict:
        return {"type": "manual_journal", "id": doc_id, "version": 1, "date": "2026-03-05", "memo": "",
                "lines": [{"account": "1021", "currency": "USD", "amount": amount, "fx_rate": rate, "fx_rate_source": "d"},
                          {"account": "2500", "currency": "USD", "amount": f"-{amount}", "fx_rate": rate,
                           "fx_rate_source": "d", "party": person, "origin": "lent"}]}
    data = {
        "schema": "fyniq-scenario@1", "name": "directors", "base_currency": "SGD", "currencies": {"SGD": 2, "USD": 2},
        "as_of": "2026-03-31", "people": ["P0001", "P0002"],
        "settings": {"accounts": {"unrealised_fx": "7040"},
                     "period_end_rates": [{"date": "2026-03-31", "currency": "USD", "fx_rate": "1.30", "fx_rate_source": "t"}]},
        "accounts": [{"code": "1021", "type": "asset", "group": False, "active": True},
                     {"code": "2500", "type": "liability", "group": False, "active": True, "role": "director-loan",
                      "revalue": True},
                     {"code": "7040", "type": "revenue", "group": False, "active": True}],
        "documents": [lent("JNL-1", "P0001", "100.00", "1.35", "135.00"), lent("JNL-2", "P0002", "50.00", "1.40", "70.00")],
        "reversals": []}
    r = _result(data)
    assert _lines(r, "revaluation-2026-03", "revaluation") == [("2500", "USD", "0.00", "5.00", "P0001"),
                                                               ("2500", "USD", "0.00", "5.00", "P0002"),
                                                               ("7040", "SGD", "-10.00", "-10.00", None)]
    assert r["people"] == {"P0001": {"2500": "-130.00"}, "P0002": {"2500": "-65.00"}}


# ---------------------------------------------------------------------------- generated scenarios
def _flagged(data: dict) -> set[str]:
    return {a["code"] for a in data["accounts"] if a.get("revalue")}


# req: QA-ENG-01
@given(p1_scenarios_with_intent(("fx",)))
def test_the_fx_source_builds_what_it_means(pair):
    data, intent = pair
    ledger = Ledger(scenario_from_json(data))
    rejected = set(ledger.rejected)
    for doc_id, meant in intent.documents.items():
        assert (doc_id in rejected) == (meant == REJECT), (doc_id, intent.kinds[doc_id], dict(ledger.reasons))


# req: FR-GL-04, INV-05
@given(p1_scenarios(("fx",)))
def test_each_revaluation_is_base_only_against_7040_and_reversed_the_next_day(data):
    parsed = scenario_from_json(data)
    ledger = Ledger(parsed)
    flagged = _flagged(data)
    revalued = {e.document: e for e in ledger.entries if e.kind == "revaluation"}
    for entry in ledger.entries:
        if entry.kind not in ("revaluation", "revaluation_reversal"):
            continue
        assert len(entry.lines) >= 2 and money.total([ln.base_amount for ln in entry.lines]).is_zero()
        for ln in entry.lines:
            if ln.account == "7040":
                assert ln.currency == "SGD" and ln.amount == ln.base_amount
            else:  # INV-05: a base-only line, on a revalued account, in another currency
                assert ln.account in flagged and ln.currency != "SGD"
                assert ln.amount.is_zero() and not ln.base_amount.is_zero()
        if entry.kind == "revaluation":
            assert (entry.posting_date + timedelta(days=1)).day == 1 and entry.posting_date <= parsed.as_of
        else:
            original = revalued[entry.document]
            assert entry.posting_date == original.posting_date + timedelta(days=1) <= parsed.as_of
            assert [(ln.account, ln.currency, -ln.amount, -ln.base_amount) for ln in original.lines] == \
                [(ln.account, ln.currency, ln.amount, ln.base_amount) for ln in entry.lines]


# req: FR-GL-04
@given(p1_scenarios(("fx",)))
def test_a_revaluation_brings_each_account_to_the_period_end_rate(data):
    # After a month's revaluation every revalued account but the receivable and payable is at the rate exactly; those
    # two are revalued document by document, so they are within half a cent per item (each rounds on its own).
    parsed = scenario_from_json(data)
    ledger = Ledger(parsed)
    controls = {parsed.settings.accounts["receivable"], parsed.settings.accounts["payable"]}
    items = len(parsed.documents) + 2  # at most a document each and what no document supports, and the total's own
    for entry in ledger.entries:
        if entry.kind != "revaluation":
            continue
        end = entry.posting_date
        rates = {r.currency: r.rate for r in parsed.settings.period_end_rates if r.day == end}
        balances: dict[tuple[str, str], tuple[Decimal, Decimal]] = {}
        for e in ledger.entries:
            if e.posting_date <= end:
                for ln in e.lines:
                    if ln.account in _flagged(data) and ln.currency != "SGD":
                        amount, base = balances.get((ln.account, ln.currency), (Decimal(0), Decimal(0)))
                        balances[(ln.account, ln.currency)] = (amount + ln.amount, base + ln.base_amount)
        for (account, currency), (amount, base) in balances.items():
            if amount.is_zero() and base.is_zero():
                continue
            gap = abs(money.rnd_fx(amount, rates[currency], 2) - base)
            assert gap <= (Decimal("0.005") * items if account in controls else 0), (account, currency, gap)


# req: FR-GL-04
@given(p1_scenarios(("fx",)))
def test_the_revaluation_changes_nothing_else(data):
    # Without the flags the documents, corrections and other scheduled entries post exactly as with them.
    with_flags = Ledger(scenario_from_json(data))
    without = copy.deepcopy(data)
    for account in without["accounts"]:
        account.pop("revalue", None)
    plain = Ledger(scenario_from_json(without))
    assert plain.rejected == with_flags.rejected
    assert plain.entries == [e for e in with_flags.entries if not e.kind.startswith("revaluation")]
    assert revaluations(scenario_from_json(without), plain.entries, plain.books) == []


# req: QA-ENG-02, FR-GL-04, RND-RATE
@pytest.mark.parametrize("shape", ["revaluation", "reversal", "waiting", "zero-amount-item", "base-per-100",
                                   "foreign-per-base", "base-per-unit", "implied-rate", "remainder"])
def test_the_fx_source_draws_each_shape(shape):
    def has(data: dict) -> bool:
        parsed = scenario_from_json(data)
        ledger = Ledger(parsed)
        kinds = [e.kind for e in ledger.entries]
        rows = data["settings"]["period_end_rates"]
        if shape == "revaluation":
            return "revaluation" in kinds
        if shape == "reversal":
            return "revaluation_reversal" in kinds
        if shape in ("base-per-100", "foreign-per-base", "base-per-unit"):
            return "revaluation" in kinds and any(r.get("form") == shape for r in rows)
        if shape == "implied-rate":  # a payment from SGD keeping an unapplied amount (RND-RATE)
            return any(d["type"] == "bill_payment" and "base_amount" in d and "unapplied" in d and d["id"] not in
                       ledger.rejected for d in data["documents"])
        if shape == "remainder":  # a journal's foreign line on 1100 or 2010, revalued
            return "revaluation" in kinds and any(
                d["type"] == "manual_journal" and d["id"] not in ledger.rejected and
                d["lines"][0]["account"] in ("1100", "2010") for d in data["documents"])
        if shape == "zero-amount-item":
            return any(e.kind == "revaluation" and any(ln.amount.is_zero() and ln.account in ("1021", "1022", "1023")
                                                       and _zero_balance(ledger, ln.account, ln.currency, e.posting_date)
                                                       for ln in e.lines) for e in ledger.entries)
        return _a_month_waits(parsed, ledger)  # waiting: a month holding a currency with no rate posts nothing

    find(p1_scenarios(("fx",)), has, settings=_FIND)


def _zero_balance(ledger: Ledger, account: str, currency: str, end: date) -> bool:
    return sum((ln.amount for e in ledger.entries if e.posting_date <= end and not e.kind.startswith("revaluation")
                for ln in e.lines if ln.account == account and ln.currency == currency), Decimal(0)).is_zero()


def _a_month_waits(parsed, ledger: Ledger) -> bool:
    """A month end whose revalued accounts hold a foreign balance in a currency with no rate for it."""
    flagged = {code for code, a in parsed.accounts.items() if a.revalue}
    posted = {e.posting_date for e in ledger.entries if e.kind == "revaluation"}
    rates = {(r.currency, r.day) for r in parsed.settings.period_end_rates}
    for end in sorted({r.day for r in parsed.settings.period_end_rates} - posted):
        totals: dict[tuple[str, str], tuple[Decimal, Decimal]] = {}
        for e in ledger.entries:
            if e.posting_date <= end and not e.kind.startswith("revaluation"):
                for ln in e.lines:
                    if ln.account in flagged and ln.currency != "SGD":
                        amount, base = totals.get((ln.account, ln.currency), (Decimal(0), Decimal(0)))
                        totals[(ln.account, ln.currency)] = (amount + ln.amount, base + ln.base_amount)
        held = {currency for (_, currency), (amount, base) in totals.items() if amount or base}
        if any((currency, end) not in rates for currency in held):
            assert end not in posted  # the oracle held the month back (README: it waits)
            return True
    return False


# req: QA-ENG-02
def test_the_fx_source_has_its_own_kinds_and_is_in_the_default_gate():
    assert FX_OWN <= set(KINDS["fx"])
    makefile = (Path(__file__).resolve().parents[2] / "Makefile").read_text(encoding="utf-8")
    sources = next(line for line in makefile.splitlines() if line.startswith("DIFFERENTIAL_SOURCES ?="))
    assert "fx" in sources.split("=", 1)[1].split(",")  # cmd/scenario-run reads the revaluation's shapes
    assert POST != REJECT
    json.dumps(KINDS["fx"])
