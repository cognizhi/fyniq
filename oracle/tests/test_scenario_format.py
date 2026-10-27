"""The scenario format (task 2.1): what is invalid input, and the round trip through JSON."""

import copy
import json
import re
from decimal import Decimal
from pathlib import Path

import pytest
from builders import bank_line, journal, line, p1_scenario, part, scenario
from hypothesis import given
from jsonschema import Draft202012Validator

from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, check_result, parse_json, scenario_from_json
from oracle.strategies import scenarios

SCHEMAS = Path(__file__).resolve().parents[2] / "test" / "differential"

README = (SCHEMAS / "README.md").read_text()
_BLOCKS = re.findall(r"```json\n(.*?)```", README, flags=re.DOTALL)
EXAMPLE, EXAMPLE_RESULT = (json.loads(block) for block in _BLOCKS[:2])


def _invalid(data, match):
    with pytest.raises(ScenarioError, match=match):
        scenario_from_json(data)


def _changed(change):
    data = copy.deepcopy(EXAMPLE)
    change(data)
    return data


# req: QA-ENG-01
@pytest.mark.parametrize("name", ["scenario.schema.json", "result.schema.json"])
def test_the_schemas_are_valid_draft_2020_12(name):
    Draft202012Validator.check_schema(json.loads((SCHEMAS / name).read_text()))


# req: QA-ENG-01
@pytest.mark.parametrize("name", ["scenario.schema.json", "result.schema.json"])
def test_the_schema_patterns_suit_re2_and_ecma_262(name):
    # Go's regexp (RE2) has no lookaround or backreferences; the Go side validates with this file too.
    text = (SCHEMAS / name).read_text()
    patterns = re.findall(r'"pattern":\s*"([^"]*)"', text) + re.findall(r'"(\^[^"]*\$)":\s*\{', text)
    assert patterns
    for pattern in patterns:
        assert "(?" not in pattern and not re.search(r"\\[1-9]", pattern), pattern
        assert pattern.startswith("^") and pattern.endswith("$"), pattern


# req: QA-ENG-01
def test_the_readme_example_result_is_what_the_oracle_writes():
    # The README's result was written by hand from its scenario; the oracle must match it. The blocks
    # after them are the P1 sources' example documents.
    assert len(_BLOCKS) == 2 + len(P1_EXAMPLES)
    assert Ledger(scenario_from_json(EXAMPLE)).result() == EXAMPLE_RESULT


P1_EXAMPLES = [json.loads(block) for block in _BLOCKS[2:]]


# req: QA-ENG-02
@pytest.mark.parametrize("document", P1_EXAMPLES, ids=lambda d: d["type"])
def test_each_p1_example_in_the_readme_is_a_document_that_posts(document):
    result = Ledger(scenario_from_json(p1_scenario(document))).result()
    assert result["rejected"] == [] and [e["document"] for e in result["entries"]][:1] == [document["id"]]


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("change", "match"),
    [
        (lambda d: d.pop("reversals"), "reversals"),
        (lambda d: d.update(schema="fyniq-scenario@2"), "fyniq-scenario@1"),
        (lambda d: d.update(extra=1), "extra"),
        (lambda d: d.update(name="has/slash"), "name"),
        (lambda d: d.update(name="has-newline\n"), "name"),
        (lambda d: d.update(base_currency="SGD\n"), "base_currency"),
        (lambda d: d["currencies"].update({"USD\n": 2}), "currencies"),
        (lambda d: d["accounts"][0].update(code="1010\n"), "accounts/0/code"),
        (lambda d: d["documents"][0].update(id="JNL-0001\n"), "documents/0/id"),
        (lambda d: d.update(base_currency="EUR"), "base_currency"),
        (lambda d: d["currencies"].update(XAU=5), "currencies"),
        (lambda d: d["currencies"].update(usd=2), "currencies"),
        (lambda d: d["accounts"].append(dict(d["accounts"][0])), "appears twice"),
        (lambda d: d["accounts"][0].update(type="income"), "accounts"),
        (lambda d: d["documents"].append(copy.deepcopy(d["documents"][0])), "appears twice"),
        (lambda d: d["documents"][0].update(type="invoice"), "documents"),
        (lambda d: d["documents"][0].update(id="JNL/0001"), "documents"),
        (lambda d: d["documents"][0].update(version=0), "documents"),
        (lambda d: d["documents"][0].update(date="2026-02-30"), "calendar date"),
        (lambda d: d["documents"][0].update(date="2026-1-5"), "documents"),
        (lambda d: d["documents"][0].update(date="2026-01-05\n"), "YYYY-MM-DD"),
        (lambda d: d["documents"][0]["lines"][0].update(amount=100), "amount"),
        (lambda d: d["documents"][0]["lines"][0].update(fx_rate=None), "fx_rate"),
        (lambda d: d["documents"][0]["lines"][0].update(note="x"), "note"),
        (lambda d: d["reversals"][0].update(date="2026-13-01"), "calendar date"),
    ],
)
def test_a_structurally_invalid_scenario_is_refused_whole(change, match):
    _invalid(_changed(change), match)


# req: QA-ENG-02
def test_a_rate_without_its_source_makes_the_scenario_invalid():
    # PRD-001 rev 0.24 (ENG-03, ENG-40): a converted amount always travels with its source, so a rate
    # without one is malformed input, not a journal to reject. The schema says so for both sides.
    _invalid(_changed(lambda d: d["documents"][0]["lines"][0].pop("fx_rate_source")), "fx_rate_source")
    # Only one way round: a source without a rate leaves a line without its rate (rule 5), so the
    # journal is rejected, and so is the reversal of it.
    data = _changed(lambda d: d["documents"][0]["lines"][0].pop("fx_rate"))
    assert Ledger(scenario_from_json(data)).result()["rejected"] == ["JNL-0001", "JNL-0001/reversal"]


# req: QA-ENG-02
def test_an_account_may_carry_the_share_capital_role_and_no_other():
    share_capital = {"code": "3010", "type": "equity", "group": False, "active": True, "role": "share-capital"}
    parsed = scenario_from_json(_changed(lambda d: d["accounts"].append(share_capital)))
    assert parsed.accounts["3010"].role == "share-capital"
    assert parsed.accounts["1010"].role is None
    _invalid(_changed(lambda d: d["accounts"][0].update(role="capital")), "accounts/0/role")
    _invalid(_changed(lambda d: d["accounts"][0].update(role=None)), "accounts/0/role")


# req: QA-ENG-01
def test_json_is_read_strictly():
    text = json.dumps(EXAMPLE)
    assert scenario_from_json(parse_json(text)).name == "short-name"
    with pytest.raises(ScenarioError, match="duplicate key"):
        parse_json('{"a": 1, "a": 2}')
    with pytest.raises(ScenarioError, match="NaN"):
        parse_json('{"a": NaN}')
    with pytest.raises(ScenarioError, match="not JSON"):
        parse_json("{")
    # A JSON number never becomes binary floating point: it is a Decimal, which then fails the schema.
    data = parse_json(text.replace('"100.00"', "100.00"))
    assert repr(data["documents"][0]["lines"][0]["amount"]) == "Decimal('100.00')"
    with pytest.raises(ScenarioError, match="amount"):
        scenario_from_json(data)


# req: QA-ENG-01
def test_a_result_that_breaks_its_schema_is_an_oracle_defect():
    result = Ledger(scenario_from_json(EXAMPLE)).result()
    result["entries"][0]["lines"][0]["amount"] = "1E+2"
    with pytest.raises(RuntimeError, match="invalid result"):
        check_result(result)


# req: QA-ENG-01
@given(scenarios())
def test_scenarios_and_results_survive_a_round_trip_through_json(data):
    parsed = scenario_from_json(data)
    assert scenario_from_json(parse_json(json.dumps(data))) == parsed
    result = Ledger(parsed).result()
    assert parse_json(json.dumps(result)) == result


# req: QA-ENG-01
def test_the_builders_make_valid_scenarios():
    doc = journal("J-1", "2026-01-01", line("1010", "SGD", "1.00"), line("4010", "SGD", "-1.00"))
    assert scenario_from_json(scenario(doc)).documents[0].id == "J-1"


# ---------------------------------------------------------------------------- P1 sources
P1 = p1_scenario(bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-10.00")), as_of="2026-03-31")


def _p1(change):
    data = copy.deepcopy(P1)
    change(data)
    return data


# req: QA-ENG-02
def test_a_p1_scenario_reads_its_settings_and_as_of():
    parsed = scenario_from_json(P1)
    assert parsed.as_of.isoformat() == "2026-03-31"
    assert parsed.settings.accounts["gst"] == "2200"
    assert parsed.settings.money_accounts["1021"] == "USD"
    assert [r.percent for r in parsed.settings.gst.rates] == [Decimal("8"), Decimal("9")]
    assert parsed.documents[0].type == "bank_transaction"
    assert scenario_from_json(parse_json(json.dumps(P1))) == parsed


# req: QA-ENG-02
@pytest.mark.parametrize(
    ("change", "match"),
    [
        (lambda d: d.pop("settings"), "needs settings"),
        (lambda d: d.pop("as_of"), "needs as_of"),
        (lambda d: d.update(as_of="2026-03-04"), "as_of"),
        (lambda d: d.update(reversals=[{"document": "BT-1", "date": "2026-03-06"}]), "a reversal names a manual journal"),
        (lambda d: d["settings"]["accounts"].update(gst="2201"), "settings/accounts/gst"),
        (lambda d: d["settings"]["accounts"].update(suspense="2999"), "settings"),
        (lambda d: d["settings"]["money_accounts"].update({"1010": "XAU"}), "settings/money_accounts/1010"),
        (lambda d: d["settings"]["money_accounts"].update({"1999": "SGD"}), "settings/money_accounts"),
        (lambda d: d["settings"]["gst"]["rates"].append({"from": "2024-01-01", "percent": "7"}), "same day"),
        (lambda d: d["settings"]["gst"]["rates"][0].update(percent="9%"), "percent"),
        (lambda d: d["settings"]["gst"]["rates"][0].update(percent="9\n"), "percent"),
        (lambda d: d["settings"]["gst"].update(registered_from="2025-02-29"), "calendar date"),
        (lambda d: d["documents"][0].update(date="2026-02-30"), "calendar date"),
        (lambda d: d["documents"][0]["parts"][0].update(spread_months="12"), "spread_months"),
        (lambda d: d["documents"][0].update(lines=[]), "lines"),
        (lambda d: d["documents"][0].update(fx_rate="1.5"), "fx_rate_source"),
        (lambda d: d["documents"][0].update(type="bank_transfer"), "documents"),
    ],
)
def test_a_p1_scenario_that_breaks_the_format_is_refused_whole(change, match):
    _invalid(_p1(change), match)
