"""Properties of the P1 rule sets over generated scenarios (QA-ENG-01, QA-ENG-02).

The intent of each generated document says whether it was built to post or to be refused; the
properties check the rules agree, and that what posts keeps the ledger's invariants.
"""

import json
from datetime import date
from decimal import Decimal

from hypothesis import given

from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import parse_json, scenario_from_json
from oracle.strategies import POST, REJECT
from oracle.strategies_p1 import SOURCES, p1_scenarios, p1_scenarios_with_intent


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


# req: QA-ENG-02
def test_every_source_has_generator_kinds():
    assert SOURCES == ("bank",)
