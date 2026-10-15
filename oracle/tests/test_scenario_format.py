"""The scenario format (task 2.1): what is invalid input, and the round trip through JSON."""

import copy
import json
import re
from pathlib import Path

import pytest
from builders import journal, line, scenario
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
    # The README's result was written by hand from its scenario; the oracle must match it.
    assert len(_BLOCKS) == 2
    assert Ledger(scenario_from_json(EXAMPLE)).result() == EXAMPLE_RESULT


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("change", "match"),
    [
        (lambda d: d.pop("reversals"), "reversals"),
        (lambda d: d.update(schema="fyniq-scenario@2"), "fyniq-scenario@1"),
        (lambda d: d.update(extra=1), "extra"),
        (lambda d: d.update(name="has/slash"), "name"),
        (lambda d: d.update(name="has-newline\n"), "name"),
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
