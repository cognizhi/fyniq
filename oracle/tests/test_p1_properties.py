"""Properties of the P1 rule sets over generated scenarios (QA-ENG-01, QA-ENG-02).

The intent of each generated document says whether it was built to post or to be refused; the
properties check the rules agree, and that what posts keeps the ledger's invariants.
"""

import json
from datetime import date
from decimal import Decimal

import pytest
from hypothesis import HealthCheck, Phase, find, given, settings

from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import parse_json, scenario_from_json
from oracle.sources.settle import OpenItem
from oracle.strategies import POST, REJECT
from oracle.strategies_p1 import (CLAIMS_REJECTED, EQUITY_REJECTED, FOUNDERS_REJECTED, KINDS, OVER_OWED, SOURCES, p1_scenarios,
                                  p1_scenarios_with_intent)


# req: QA-ENG-01
@given(p1_scenarios_with_intent())
def test_built_to_post_posts_and_built_to_fail_is_refused(pair):
    data, intent = pair
    ledger = Ledger(scenario_from_json(data))
    rejected = set(ledger.rejected)
    for doc_id, meant in intent.documents.items():
        if meant == POST:
            assert doc_id not in rejected, dict(ledger.reasons)
        elif meant == REJECT:
            assert doc_id in rejected


# req: INV-01
@given(p1_scenarios())
def test_every_entry_balances_in_base_and_every_month_nets_to_zero(data):
    ledger = Ledger(scenario_from_json(data))
    for entry in ledger.entries:
        assert money.total([ln.base_amount for ln in entry.lines]).is_zero()
        assert len(entry.lines) >= 2
    for month in ledger.trial_balance().values():
        assert money.total(list(month.values())).is_zero()


# req: ENG-05
@given(p1_scenarios())
def test_releases_post_up_to_as_of_and_never_before_their_document(data):
    parsed = scenario_from_json(data)
    ledger = Ledger(parsed)
    dates = {d.id: d.date for d in parsed.documents}
    for entry in ledger.entries:
        if entry.kind == "release":
            assert dates[entry.document] <= entry.posting_date <= parsed.as_of


# req: FR-GL-03
@given(p1_scenarios())
def test_a_deferral_never_releases_more_than_it_took(data):
    parsed = scenario_from_json(data)
    ledger = Ledger(parsed)
    deferrals = {parsed.settings.accounts["prepayments"], parsed.settings.accounts["deferred_revenue"]}
    for account in deferrals:
        took = released = Decimal(0)
        for entry in ledger.entries:
            for ln in entry.lines:
                if ln.account == account:
                    if entry.kind == "post":
                        took += ln.base_amount
                    else:
                        released += ln.base_amount
        assert abs(released) <= abs(took)
        assert took == 0 or (released == 0 or (took > 0) != (released > 0))


# req: QA-ENG-01
@given(p1_scenarios())
def test_p1_scenarios_and_results_survive_a_round_trip_and_are_deterministic(data):
    parsed = scenario_from_json(data)
    assert scenario_from_json(parse_json(json.dumps(data))) == parsed
    result = Ledger(parsed).result()
    assert result == Ledger(scenario_from_json(data)).result()
    assert parse_json(json.dumps(result)) == result
    assert date.fromisoformat(data["as_of"]) >= max(date.fromisoformat(d["date"]) for d in data["documents"])


# req: INV-07
@given(p1_scenarios(("purchases",)))
def test_the_ap_sub_ledger_equals_2010_in_each_currency_and_in_base(data):
    # Open bills owe what is left of them; vendor credits and unapplied payments are credits with the
    # supplier. Their sum, read from the oracle's books, is what 2010's lines add up to.
    ledger = Ledger(scenario_from_json(data))
    payable = data["settings"]["accounts"]["payable"]
    by_currency: dict[str, Decimal] = {}
    base = Decimal(0)
    for item in ledger.books.values():
        if not isinstance(item, OpenItem) or item.control != payable:
            continue
        owes = item.type == "bill"
        amount = -item.open if owes else item.total
        by_currency[item.currency] = by_currency.get(item.currency, Decimal(0)) + amount
        base += -item.open_base if owes else item.carrying
    lines = [ln for e in ledger.entries for ln in e.lines if ln.account == payable]
    assert sum((ln.base_amount for ln in lines), Decimal(0)) == base
    for currency in {ln.currency for ln in lines} | set(by_currency):
        assert sum((ln.amount for ln in lines if ln.currency == currency), Decimal(0)) == by_currency.get(currency, 0)


# req: INV-07
@given(p1_scenarios(("sales",)))
def test_the_ar_sub_ledger_equals_1100_in_each_currency_and_in_base(data):
    # Open invoices are owed; credit notes and unapplied receipts are credits with the customer.
    ledger = Ledger(scenario_from_json(data))
    receivable = data["settings"]["accounts"]["receivable"]
    by_currency: dict[str, Decimal] = {}
    base = Decimal(0)
    for item in ledger.books.values():
        if not isinstance(item, OpenItem) or item.control != receivable:
            continue
        owed = item.type == "invoice"
        by_currency[item.currency] = by_currency.get(item.currency, Decimal(0)) + (item.open if owed else -item.total)
        base += item.open_base if owed else -item.carrying
    lines = [ln for e in ledger.entries for ln in e.lines if ln.account == receivable]
    assert sum((ln.base_amount for ln in lines), Decimal(0)) == base
    for currency in {ln.currency for ln in lines} | set(by_currency):
        assert sum((ln.amount for ln in lines if ln.currency == currency), Decimal(0)) == by_currency.get(currency, 0)


# req: INV-07
@given(p1_scenarios(("claims",)))
def test_open_claims_equal_2150_and_what_is_left_of_advances_equals_1250(data):
    from oracle.sources.claims import Advance

    ledger = Ledger(scenario_from_json(data))
    accounts = data["settings"]["accounts"]
    owed = [i for i in ledger.books.values() if isinstance(i, OpenItem) and i.control == accounts["claims_payable"]]
    left = [a.left for a in ledger.books.values() if isinstance(a, Advance)]
    lines = [ln for e in ledger.entries for ln in e.lines]
    assert sum((ln.base_amount for ln in lines if ln.account == accounts["claims_payable"]), Decimal(0)) == \
        -sum((i.open_base for i in owed), Decimal(0))
    assert sum((ln.base_amount for ln in lines if ln.account == accounts["advances"]), Decimal(0)) == sum(left, Decimal(0))


# req: INV-07
@given(p1_scenarios(("assets",)))
def test_1590_holds_the_depreciation_of_the_assets_still_in_the_register(data):
    # A disposal clears what its asset's months put on 1590, so 1590 is minus what the live assets posted;
    # no asset depreciates beyond its cost less residual, nor in or after the month it was disposed of.
    from oracle.sources.assets import Asset

    ledger = Ledger(scenario_from_json(data))
    accumulated = data["settings"]["accounts"]["accumulated_depreciation"]
    posted: dict[str, Decimal] = {}
    for entry in ledger.entries:
        if entry.kind == "depreciation":
            posted[entry.document] = posted.get(entry.document, Decimal(0)) - sum(
                (ln.base_amount for ln in entry.lines if ln.account == accumulated), Decimal(0))
    live = {doc_id: a for doc_id, a in ledger.books.items() if isinstance(a, Asset) and not a.disposed}
    balance = sum((ln.base_amount for e in ledger.entries for ln in e.lines if ln.account == accumulated), Decimal(0))
    assert balance == -sum((posted.get(doc_id, Decimal(0)) for doc_id in live), Decimal(0))
    for doc_id, total in posted.items():
        asset = ledger.books[doc_id]
        assert total <= sum((part for _, part in asset.months), Decimal(0))


# req: QA-ENG-02
def test_every_source_has_generator_kinds():
    assert SOURCES == ("bank", "purchases", "sales", "claims", "assets", "founders", "general-ledger", "fx", "equity")
    assert set(SOURCES) == set(KINDS)


# What refuses each rejected kind of the equity source (README, "Cash share issues" and "Share transfers").
EQUITY_REASONS = {"equity-issue-unpaid": "unpaid", "equity-transfer-too-many": "more-than-held",
                  "equity-transfer-to-self": "same-holder", "equity-transfer-not-share-capital": "not-share-capital",
                  # a SAFE refused for the one rule it breaks (README, "SAFEs and their conversion")
                  "equity-safe-bad": frozenset({"no-cap", "bad-term", "not-share-capital", "bad-precision",
                                                "not-a-holder"}),
                  "equity-safe-money-unnamed": "not-a-recorded-safe", "equity-safe-conversion-unfunded": "money-not-held",
                  # the option pool (README, "The option pool and grants")
                  "equity-grant-beyond": "pool-short", "equity-grant-no-pool": "no-pool", "equity-pool-short": "pool-short",
                  "equity-event-too-many": "more-than-left", "equity-forfeit-vested": "vested",
                  "equity-exercise-not-vested": "not-vested"}


# req: QA-ENG-02, FR-EQ-17, FR-EQ-18, FR-EQ-20
@pytest.mark.parametrize("kind", sorted(set(KINDS["equity"])))
def test_the_equity_source_draws_each_of_its_kinds_as_meant(kind):
    # Each kind is drawn, posts or is refused as the generator meant, and a refused one for its own rule.
    data, intent = find(p1_scenarios_with_intent(("equity",)), lambda pair: kind in pair[1].kinds.values(),
                        settings=_FIND)
    ledger = Ledger(scenario_from_json(data))
    reasons = dict(ledger.reasons)
    for doc_id, built in intent.kinds.items():
        if built == kind:
            assert (doc_id in ledger.rejected) == (intent.documents[doc_id] == REJECT), reasons
            assert (intent.documents[doc_id] == REJECT) == (kind in EQUITY_REJECTED)
            expected = EQUITY_REASONS.get(kind)
            assert reasons.get(doc_id) in (expected if isinstance(expected, frozenset) else {expected}), (doc_id, reasons)


# req: QA-ENG-02, FR-EQ-20
@given(p1_scenarios(("equity", "founders")))
@settings(max_examples=60, deadline=None, suppress_health_check=list(HealthCheck))
def test_a_cap_table_adds_up(data):
    # Every line holds shares or options; each holder's figures are the sums of their lines', and the day's those of
    # every line, its fully diluted total with what the pool leaves, never below zero (README, "Cap tables").
    for table in Ledger(scenario_from_json(data)).result()["cap_tables"]:
        lines = table["lines"]
        assert all(Decimal(ln["fully_diluted"]) >= Decimal(ln["issued"]) >= 0 and Decimal(ln["fully_diluted"]) > 0
                   for ln in lines)
        assert sum((Decimal(ln["fully_diluted"]) - Decimal(ln["issued"]) for ln in lines), Decimal(0)) == Decimal(table["options"])
        assert Decimal(table["unallocated"]) >= 0
        for key in ("issued", "votes", "fully_diluted"):
            pool = Decimal(table["unallocated"]) if key == "fully_diluted" else Decimal(0)
            assert sum((Decimal(ln[key]) for ln in lines), pool) == Decimal(table[key])
            for h in table["holders"]:
                assert sum((Decimal(ln[key]) for ln in lines if ln["holder"] == h["holder"]), Decimal(0)) == Decimal(h[key])
        assert [h["holder"] for h in table["holders"]] == sorted({ln["holder"] for ln in lines})


# req: QA-ENG-02, FR-EQ-21
@given(p1_scenarios(("equity",)))
@settings(max_examples=60, deadline=None, suppress_health_check=list(HealthCheck))
def test_a_round_adds_up(data):
    # Every share a round issues is an investor's or a conversion's; each holder's shares after are before and theirs;
    # the totals after are the holders' sums, fully diluted with the options before and the top-up (README, "Rounds").
    for r in Ledger(scenario_from_json(data)).result().get("rounds", []):
        if r is None:
            continue
        new = sum(int(i["shares"]) for i in r["issues"]) + sum(int(c["shares"]) for c in r["conversions"])
        assert new == int(r["new_shares"])
        assert sum(int(h["issued_after"]) for h in r["holders"]) == int(r["issued_after"]) == int(r["issued_before"]) + new
        gained = sum(int(h["fully_diluted_after"]) - int(h["fully_diluted_before"]) for h in r["holders"])
        assert gained == new
        top_up = int(r["unallocated_after"]) - int(r["unallocated_before"])
        assert int(r["fully_diluted_after"]) == int(r["fully_diluted_before"]) + new + top_up


# req: QA-ENG-02, FR-EQ-03, FR-EQ-04, FR-EQ-05, FR-EQ-06, FR-EQ-07
@pytest.mark.parametrize("kind", sorted(set(KINDS["founders"])))
def test_the_founders_source_draws_each_of_its_kinds_as_meant(kind):
    # Each kind of the founders' source is drawn, and the oracle posts or refuses it as the generator meant.
    data, intent = find(p1_scenarios_with_intent(("founders",)), lambda pair: kind in pair[1].kinds.values(),
                        settings=settings(max_examples=2000, database=None, phases=[Phase.generate],
                                          suppress_health_check=list(HealthCheck)))
    ledger = Ledger(scenario_from_json(data))
    reasons = dict(ledger.reasons)
    for doc_id, built in intent.kinds.items():
        if built == kind:
            assert (doc_id in ledger.rejected) == (intent.documents[doc_id] == REJECT), reasons
            assert (intent.documents[doc_id] == REJECT) == (kind in FOUNDERS_REJECTED)
            # A waiver, repayment or conversion of more than is owed is refused for that (README, "P1 readings"), and
            # every other kind is refused for its own rule: the oracle checks what is owed last, so a kind refused for
            # what is owed would have drawn on more than is owed.
            assert (reasons.get(doc_id) == "more-than-owed") == (kind in OVER_OWED), (doc_id, reasons)


# What refuses each "claim-paid-" kind (README, "Expense claims": `paid` on a base-currency claim or a trip, not above
# zero, or not a valid base-currency amount).
PAID_REASONS = {
    "claim-paid-on-base": {"paid-on-base-claim"},
    "claim-paid-on-trip": {"paid-on-trip"},
    "claim-paid-invalid": {"zero-amount", "wrong-sign", "amount-too-many-decimals", "amount-not-plain-decimal"},
}
_FIND = settings(max_examples=2000, database=None, phases=[Phase.generate], suppress_health_check=list(HealthCheck))


def _claims_drawn(kind):
    return find(p1_scenarios_with_intent(("claims",)), lambda pair: kind in pair[1].kinds.values(), settings=_FIND)


# req: QA-ENG-02, FR-AP-07, FR-AP-16, FR-AP-17
@pytest.mark.parametrize("kind", sorted(set(KINDS["claims"])))
def test_the_claims_source_draws_each_of_its_kinds_as_meant(kind):
    # Each kind of the claims source is drawn, and the oracle posts or refuses it as the generator meant; a kind built to
    # be refused is meant to be refused, and a "claim-paid-" kind is refused for its `paid`.
    data, intent = _claims_drawn(kind)
    ledger = Ledger(scenario_from_json(data))
    reasons = dict(ledger.reasons)
    for doc_id, built in intent.kinds.items():
        if built == kind:
            assert (doc_id in ledger.rejected) == (intent.documents[doc_id] == REJECT), reasons
            if kind in CLAIMS_REJECTED:
                assert intent.documents[doc_id] == REJECT
            if kind in PAID_REASONS:
                assert reasons[doc_id] in PAID_REASONS[kind], (doc_id, reasons)


def _posted_claim(data, ledger, test):
    return any(d["type"] == "expense_claim" and d["id"] not in ledger.rejected and test(d) for d in data["documents"])


# req: FR-AP-17
@pytest.mark.parametrize(
    "shape",
    [
        lambda d: any("paid" in ln for ln in d["lines"]),  # a receipt line stating what was paid (rev 0.169)
        lambda d: d["currency"] != "SGD" and "advance" in d,  # a foreign claim settling an advance (UC-EXP-03)
        lambda d: d["currency"] != "SGD" and "advance" in d and any("paid" in ln for ln in d["lines"]),
    ],
    ids=["paid", "foreign-advance", "paid-advance"],
)
def test_the_claims_source_posts_paid_lines_and_foreign_claims_settling_advances(shape):
    find(p1_scenarios(("claims",)), lambda data: _posted_claim(data, Ledger(scenario_from_json(data)), shape),
         settings=_FIND)


# req: FR-AP-17
@given(p1_scenarios(("claims",)))
def test_a_claim_owes_what_was_paid_or_the_line_at_the_rate_and_7030_takes_the_rest(data):
    # A posted claim in another currency (receipts only: a trip is refused there) credits 2150 and 1250 together with,
    # per receipt line, its `paid` or else RND-FX of its net plus GST at the claim's rate, summed (rev 0.169).
    ledger = Ledger(scenario_from_json(data))
    accounts = data["settings"]["accounts"]
    for doc in data["documents"]:
        if doc["type"] != "expense_claim" or doc["id"] in ledger.rejected or doc["currency"] == "SGD":
            continue
        rate = Decimal(doc["fx_rate"])
        owed = Decimal(0)
        for ln in doc["lines"]:
            if "paid" in ln:
                owed += Decimal(ln["paid"])
            else:
                owed += money.rnd_fx(Decimal(ln["amount"]) + Decimal(ln.get("gst", "0")), rate, 2)
        entry = next(e for e in ledger.entries if e.document == doc["id"] and e.kind == "post")
        credited = -sum((ln.base_amount for ln in entry.lines
                         if ln.account in (accounts["claims_payable"], accounts["advances"])), Decimal(0))
        assert credited == owed, (doc, entry)


# req: FR-EQ-01
@given(p1_scenarios(("founders",)))
def test_each_line_on_a_directors_account_names_a_person_and_their_totals_add_up_to_it(data):
    result = Ledger(scenario_from_json(data)).result()
    director = {a["code"] for a in data["accounts"] if a.get("role") in ("director-loan", "deferred-remuneration")}
    lines = [ln for e in result["entries"] for ln in e["lines"]]
    assert all((ln["account"] in director) == ("party" in ln) for ln in lines)
    assert all(ln["party"] in data["people"] for ln in lines if "party" in ln)
    for code in director:
        posted = sum((Decimal(ln["base_amount"]) for ln in lines if ln["account"] == code), Decimal(0))
        assert posted == sum((Decimal(p.get(code, "0")) for p in result.get("people", {}).values()), Decimal(0))
