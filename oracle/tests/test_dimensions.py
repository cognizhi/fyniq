"""Dimensions (FR-DIM-01, FR-DIM-02; PRD-001 rev 0.171, 0.172): what a line may name, what an account requires, and
what the result shows (test/differential/README.md, "Dimensions", rule 11 and the invalid-scenario rules).

Every figure is worked by hand beside it (QA-ENG-01); the properties check the rules over generated scenarios, and
that each dimension kind of the generator posts or is refused as built.
"""

import copy
from collections import defaultdict
from datetime import date
from decimal import Decimal
from pathlib import Path

import pytest
from builders import (
    ACCOUNTS,
    P1_ACCOUNTS,
    P1_SETTINGS,
    asset,
    bank_line,
    bill,
    bill_line,
    claim,
    invoice,
    invoice_line,
    journal,
    line,
    p1_scenario,
    part,
    scenario,
    settlement,
    trip,
)
from hypothesis import HealthCheck, Phase, find, given, settings
from hypothesis import strategies as st

from oracle import money, strategies_gl, strategies_p1
from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, load_scenario, scenario_from_json
from oracle.strategies import POST, REJECT, scenarios
from oracle.strategies_gl import DIMENSION_KINDS as GL_DIMENSION_KINDS
from oracle.strategies_gl import JOURNAL_DIMENSION_REJECTED, OPENING_DIMENSION_REJECTED
from oracle.strategies_p1 import p1_scenarios, p1_scenarios_with_intent

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden"

# Department and Project in use (OLD out of use), Region out of use though its value is in use.
DIMENSIONS = [
    {"name": "Department", "active": True,
     "values": [{"code": "ENG", "active": True}, {"code": "OPS", "active": True}, {"code": "OLD", "active": False}]},
    {"name": "Project", "active": True, "values": [{"code": "P-001", "active": True}, {"code": "P-002", "active": True}]},
    {"name": "Region", "active": False, "values": [{"code": "SG", "active": True}]},
]
REQUIRES = {"6010": ["Department"], "4010": ["Department", "Project"], "6020": ["Region"]}


def _chart(accounts: list[dict], extra: tuple[dict, ...] = ()) -> list[dict]:
    chart = [{**a, "requires": REQUIRES[a["code"]]} if a["code"] in REQUIRES else dict(a) for a in accounts]
    return chart + [{**a, "requires": REQUIRES[a["code"]]} if a["code"] in REQUIRES else a for a in extra]


CHART = _chart(ACCOUNTS, ({"code": "6020", "type": "expense", "group": False, "active": True},))


def tagged(account: str, currency: str, amount: str, rate: str | None = None, **values: str) -> dict:
    """A journal line naming ``values`` (keyword names with "_" for a space)."""
    out = line(account, currency, amount, rate)
    if values:
        out["dimensions"] = {name.replace("_", " "): code for name, code in values.items()}
    return out


def post(*documents: dict, reversals: tuple = ()) -> tuple[dict, Ledger]:
    data = scenario(*documents, reversals=reversals)
    data["accounts"], data["dimensions"] = CHART, DIMENSIONS
    ledger = Ledger(scenario_from_json(data))
    return ledger.result(), ledger


def entry(result: dict, document: str, kind: str = "post") -> list[tuple]:
    found = next(e for e in result["entries"] if e["document"] == document and e["kind"] == kind)
    return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"], ln.get("dimensions")) for ln in found["lines"]]


ENG, OPS = {"Department": "ENG"}, {"Department": "OPS"}


# ---------------------------------------------------------------------------- what a line may name, what posts
# req: FR-DIM-01, FR-DIM-02
def test_a_journal_naming_values_in_use_posts_them_on_its_lines():
    result, ledger = post(journal("J1", "2026-01-05",
                                  tagged("6010", "SGD", "300.00", Department="ENG"),
                                  tagged("6010", "SGD", "200.00", Project="P-001", Department="OPS"),
                                  line("1010", "SGD", "-500.00")))
    assert ledger.rejected == []
    assert entry(result, "J1") == [
        ("6010", "SGD", "300.00", "300.00", ENG),
        ("6010", "SGD", "200.00", "200.00", {"Department": "OPS", "Project": "P-001"}),
        ("1010", "SGD", "-500.00", "-500.00", None),  # no values: no "dimensions" key
    ]
    # Sets in name order: Department before Project, whatever order the line wrote them in.
    assert result["dimension_balances"] == {
        "2026-01": {"1010": {"": "-500.00"}, "6010": {"Department=ENG": "300.00", "Department=OPS|Project=P-001": "200.00"}},
    }
    assert result["trial_balance"] == {"2026-01": {"1010": "-500.00", "6010": "500.00"}}


# req: FR-DIM-01
@pytest.mark.parametrize(
    "lines",
    [
        (line("6010", "SGD", "100.00"), line("1010", "SGD", "-100.00")),  # 6010 requires Department
        (tagged("4010", "SGD", "-100.00", Department="ENG"), line("1010", "SGD", "100.00")),  # 4010 needs Project too
        (tagged("6010", "USD", "100.00", "1.35", Project="P-001"), line("1010", "SGD", "-135.00")),  # a value, not its
    ],
    ids=["no-value", "one-of-two", "another-dimension"],
)
def test_a_line_on_an_account_that_requires_a_dimension_in_use_names_a_value_of_it(lines):
    # DIM-05 (rev 0.171): the line shows ✖ and the journal does not post.
    result, ledger = post(journal("J1", "2026-01-05", *lines))
    assert ledger.reasons == [("J1", "dimension-required")]
    assert result["entries"] == [] and result["dimension_balances"] == {}


# req: FR-DIM-01
@pytest.mark.parametrize(
    "lines",
    [
        # 250.00 moved to ENG from no value: 6010's lines net to zero, so the second needs none (rev 0.172).
        (tagged("6010", "SGD", "250.00", Department="ENG"), line("6010", "SGD", "-250.00")),
        (tagged("6010", "SGD", "100.00", Department="ENG"), tagged("6010", "SGD", "50.00", Department="OPS"),
         line("6010", "SGD", "-150.00")),
        # In USD at one rate: 135.00 and -135.00 in base.
        (tagged("6010", "USD", "100.00", "1.35", Department="ENG"), line("6010", "USD", "-100.00", "1.35")),
    ],
    ids=["one-value", "two-values", "in-usd"],
)
def test_a_journal_moving_an_amount_between_values_may_leave_out_the_value_it_moves_from(lines):
    result, ledger = post(journal("J1", "2026-02-03", *lines))
    assert ledger.rejected == []
    assert result["trial_balance"] == {"2026-02": {}}  # nothing changes the account's total
    sets = result["dimension_balances"]["2026-02"]["6010"]
    assert money.total([Decimal(v) for v in sets.values()]) == 0 and "" in sets and len(sets) == len(lines)


# req: FR-DIM-01
@pytest.mark.parametrize(
    "lines",
    [
        # USD 100.00 at 1.35 is 135.00 and USD -100.00 at 1.34 is -134.00: 6010 nets in dollars, not in base.
        (tagged("6010", "USD", "100.00", "1.35", Department="ENG"), line("6010", "USD", "-100.00", "1.34"),
         line("1010", "SGD", "-1.00")),
        # 6010 nets to zero, 4010 (-50.00) does not: the exception is the account's.
        (tagged("6010", "SGD", "100.00", Department="ENG"), line("6010", "SGD", "-100.00"),
         line("4010", "SGD", "-50.00"), line("1010", "SGD", "50.00")),
    ],
    ids=["not-in-base", "another-account"],
)
def test_the_move_between_values_nets_in_base_on_that_account(lines):
    _, ledger = post(journal("J1", "2026-02-03", *lines))
    assert ledger.reasons == [("J1", "dimension-required")]


# req: FR-DIM-01
@pytest.mark.parametrize(
    ("lines", "reason"),
    [
        ((tagged("6010", "SGD", "100.00", Department="OLD"), line("1010", "SGD", "-100.00")),
         "dimension-value-out-of-use"),
        # SG is in use, but Region is not.
        ((tagged("6010", "SGD", "100.00", Department="ENG", Region="SG"), line("1010", "SGD", "-100.00")),
         "dimension-out-of-use"),
        ((tagged("1010", "SGD", "-100.00", Region="SG"), tagged("6010", "SGD", "100.00", Department="ENG")),
         "dimension-out-of-use"),
        # A move between values is held to DIM-06 too.
        ((tagged("6010", "SGD", "100.00", Department="OLD"), line("6010", "SGD", "-100.00")),
         "dimension-value-out-of-use"),
    ],
    ids=["value-out-of-use", "dimension-out-of-use", "on-any-account", "in-a-move"],
)
def test_a_line_naming_a_value_out_of_use_or_of_a_dimension_out_of_use_refuses_its_journal(lines, reason):
    _, ledger = post(journal("J1", "2026-01-05", *lines))
    assert ledger.reasons == [("J1", reason)]  # DIM-06


# req: FR-DIM-01
def test_a_dimension_out_of_use_requires_nothing():
    # 6020 requires Region, out of use: its line names no value and posts.
    result, ledger = post(journal("J1", "2026-01-05", line("6020", "SGD", "80.00"), line("1010", "SGD", "-80.00")))
    assert ledger.rejected == []
    assert result["dimension_balances"] == {"2026-01": {"1010": {"": "-80.00"}, "6020": {"": "80.00"}}}


# req: FR-DIM-02, FR-GL-12, ENG-14
def test_a_void_keeps_each_lines_values_so_every_set_nets_to_zero():
    result, _ = post(journal("J1", "2026-01-05",
                             tagged("6010", "USD", "100.00", "1.35", Department="ENG"),
                             tagged("6010", "SGD", "200.00", Department="OPS", Project="P-001"),
                             line("1010", "SGD", "-335.00")),
                     reversals=(("J1", "2026-03-02"),))
    assert entry(result, "J1", "reversal") == [
        ("6010", "USD", "-100.00", "-135.00", ENG),  # negated as stored (ENG-14), its values kept
        ("6010", "SGD", "-200.00", "-200.00", {"Department": "OPS", "Project": "P-001"}),
        ("1010", "SGD", "335.00", "335.00", None),
    ]
    assert result["dimension_balances"] == {
        "2026-01": {"1010": {"": "-335.00"}, "6010": {"Department=ENG": "135.00", "Department=OPS|Project=P-001": "200.00"}},
        "2026-03": {"1010": {"": "335.00"},
                    "6010": {"Department=ENG": "-135.00", "Department=OPS|Project=P-001": "-200.00"}},
    }


# req: FR-DIM-02
def test_a_scenario_that_declares_no_dimensions_has_no_dimension_balances():
    data = scenario(journal("J1", "2026-01-05", line("6010", "SGD", "1.00"), line("1010", "SGD", "-1.00")))
    result = Ledger(scenario_from_json(data)).result()
    assert "dimension_balances" not in result
    assert all("dimensions" not in ln for e in result["entries"] for ln in e["lines"])
    # The oracle's reading: an empty list declares none either (README: "present when the scenario declares
    # dimensions").
    data["dimensions"] = []
    assert "dimension_balances" not in Ledger(scenario_from_json(data)).result()


# ---------------------------------------------------------------------------- the general ledger's shapes
P1_CHART = _chart(P1_ACCOUNTS, ({"code": "6020", "type": "expense", "group": False, "active": True},))
START = "2026-01-01"


def p1_post(*documents: dict, corrections: tuple = (), accounts: list[dict] | None = None,
            as_of: str = "2026-06-30") -> tuple[dict, Ledger]:
    data = p1_scenario(*documents, as_of=as_of, settings={**P1_SETTINGS, "books_start": START},
                       accounts=accounts or P1_CHART)
    data["dimensions"], data["reversals"] = DIMENSIONS, list(corrections)
    ledger = Ledger(scenario_from_json(data))
    return ledger.result(), ledger


def balances(*lines: dict, doc_id: str = "OB") -> dict:
    return {"type": "opening_balances", "id": doc_id, "version": 1, "date": START, "memo": "", "lines": list(lines)}


# req: FR-GL-06, FR-DIM-01, FR-DIM-02
def test_the_opening_balances_need_not_name_what_an_account_requires_and_keep_each_set_apart():
    result, ledger = p1_post(balances(
        line("1010", "SGD", "25000.00"),
        tagged("6010", "SGD", "300.00", Department="ENG"),
        tagged("6010", "SGD", "200.00", Department="OPS"),
        line("6010", "SGD", "100.00"),  # no value: the opening balances are not held to DIM-05
        tagged("6010", "SGD", "50.00", Department="ENG"),  # with the first: one balance of 350.00
        line("3010", "SGD", "-25650.00"),
    ))
    assert ledger.rejected == []
    # Canonical form (README, "P1 entries"): one line per account, currency, person and set, sorted, "" first.
    assert entry(result, "OB") == [
        ("1010", "SGD", "25000.00", "25000.00", None),
        ("3010", "SGD", "-25650.00", "-25650.00", None),
        ("6010", "SGD", "100.00", "100.00", None),
        ("6010", "SGD", "350.00", "350.00", ENG),
        ("6010", "SGD", "200.00", "200.00", OPS),
    ]
    assert result["dimension_balances"]["2026-01"]["6010"] == {"": "100.00", "Department=ENG": "350.00",
                                                               "Department=OPS": "200.00"}


# req: FR-GL-06, FR-DIM-01
def test_the_opening_balances_are_held_to_values_in_use():
    _, ledger = p1_post(balances(line("1010", "SGD", "100.00"), tagged("3010", "SGD", "-100.00", Department="OLD")))
    assert ledger.reasons == [("OB", "dimension-value-out-of-use")]  # DIM-06


# req: FR-GL-06, FR-GL-12, FR-DIM-01
def test_an_edit_of_the_opening_balances_is_held_to_what_they_are_held_to():
    # The oracle's reading of "an edit's replacement is held to both rules as a journal is": an edit is held to its
    # own document's rules (README, "Edits": "a journal's rules ..., or the opening balances' rules"), so the opening
    # balances' edit needs values in use (DIM-06) but not what an account requires (DIM-05).
    opening = balances(line("1010", "SGD", "100.00"), line("3010", "SGD", "-100.00"))

    def edit(version: int, *lines: dict) -> dict:
        return {"document": "OB", "edit": {"version": version, "date": START, "memo": "", "lines": list(lines)}}

    result, ledger = p1_post(opening, corrections=(
        edit(2, tagged("6010", "SGD", "100.00", Department="OLD"), line("3010", "SGD", "-100.00")),
        edit(2, line("6010", "SGD", "100.00"), line("3010", "SGD", "-100.00")),
    ))
    assert ledger.reasons == [("OB/edit", "dimension-value-out-of-use")]
    assert entry(result, "OB", "replacement") == [("3010", "SGD", "-100.00", "-100.00", None),
                                                  ("6010", "SGD", "100.00", "100.00", None)]


# req: FR-GL-12, FR-GL-02, FR-DIM-01, FR-DIM-02
def test_an_edit_is_held_to_both_rules_and_its_reversals_keep_the_values():
    accrual = journal("J1", "2026-01-31", tagged("6010", "SGD", "300.00", Department="ENG"), line("2010", "SGD", "-300.00"))
    accrual["reverse_on"] = "2026-02-01"

    def edit(*lines: dict) -> dict:
        return {"document": "J1", "edit": {"version": 2, "date": "2026-01-31", "memo": "", "reverse_on": "2026-02-01",
                                           "lines": list(lines)}}

    result, ledger = p1_post(accrual, corrections=(
        edit(line("6010", "SGD", "300.00"), line("2010", "SGD", "-300.00")),  # DIM-05: refused, nothing changes
        edit(tagged("6010", "SGD", "300.00", Department="OPS"), line("2010", "SGD", "-300.00")),
    ))
    assert ledger.reasons == [("J1/edit", "dimension-required")]
    assert entry(result, "J1", "reversal") == [("6010", "SGD", "-300.00", "-300.00", ENG),
                                               ("2010", "SGD", "300.00", "300.00", None)]
    assert entry(result, "J1", "auto_reversal") == [("6010", "SGD", "-300.00", "-300.00", OPS),  # the replacement's
                                                    ("2010", "SGD", "300.00", "300.00", None)]
    assert result["dimension_balances"] == {
        "2026-01": {"2010": {"": "-300.00"}, "6010": {"Department=OPS": "300.00"}},  # ENG: 300.00 - 300.00, left out
        "2026-02": {"2010": {"": "300.00"}, "6010": {"Department=OPS": "-300.00"}},
    }


# ---------------------------------------------------------------------------- the P1 sources
# req: FR-DIM-01, FR-AR-03, FR-AR-04, FR-AP-02, FR-AP-03, FR-AP-07, FR-BNK-05
@pytest.mark.parametrize(
    "document",
    [
        invoice("D1", "2026-03-05", "SGD", invoice_line("4010", "1000.00", "ZR")),
        invoice("D1", "2026-03-05", "SGD", invoice_line("4020", "10.00", "ZR"), invoice_line("4010", "90.00", "ZR"),
                kind="credit_note"),
        bill("D1", "2026-03-05", "SGD", bill_line("6010", "500.00", "NR")),
        # A spread line by its own account: 6010 requires Department, though the cost goes to 1210 first.
        bill("D1", "2026-03-05", "SGD", bill_line("6010", "1200.00", "NR", spread_months=12)),
        bill("D1", "2026-03-05", "SGD", bill_line("6010", "50.00", "NR"), kind="vendor_credit"),
        bank_line("D1", "2026-03-05", "1010", "SGD", "-50.00", part("6010", "-50.00", "OP")),
        claim("D1", "2026-03-05", "SGD", bill_line("6010", "30.00", "OP")),
        claim("D1", "2026-03-05", "SGD", trip("6010", "12.5")),  # a trip is a line the claimant enters too
    ],
    ids=["invoice", "credit-note", "bill", "bill-spread", "vendor-credit", "bank-line", "claim", "claim-trip"],
)
def test_a_line_a_person_enters_on_a_p1_document_on_a_requiring_account_refuses_it(document):
    # The P1 sources carry no dimension values in the format yet, so a line on an account that requires a dimension in
    # use cannot name one (DIM-05).
    _, ledger = p1_post(document)
    assert ledger.reasons == [("D1", "dimension-required")]


# req: FR-DIM-01, FR-AR-17, FR-FA-02
def test_the_other_p1_sources_and_scheduled_entries_post_without_the_check():
    # 6010 (the write-off's account) and 6610 (depreciation) require Department, as do 1011 and 1100; none of these
    # lines names a value, and each posts as the system's own postings do.
    accounts = _chart(P1_ACCOUNTS, ({"code": "6020", "type": "expense", "group": False, "active": True},))
    for account in accounts:
        if account["code"] in ("6610", "1100", "1011"):
            account["requires"] = ["Department"]
    result, ledger = p1_post(
        bill("B1", "2026-01-10", "SGD", bill_line("6020", "400.00", "NR")),  # 6020 requires only Region, out of use
        invoice("I1", "2026-01-12", "SGD", invoice_line("4020", "1000.00", "ZR")),
        {"type": "write_off", "id": "W1", "version": 1, "date": "2026-01-20", "invoice": "I1", "amount": "400.00",
         "account": "6010"},
        settlement("customer_receipt", "R1", "2026-01-25", "1010", "SGD", ("I1", "600.00")),
        asset("FA1", "2026-01-15", "3600.00", 36),
        # The oracle's reading: a bank line's own money account (1011) is the statement's, not a line a person enters;
        # its parts are.
        bank_line("BT1", "2026-01-28", "1011", "SGD", "-20.00", part("6110", "-20.00", "OP")),
        accounts=accounts, as_of="2026-02-28",
    )
    assert ledger.rejected == []
    assert entry(result, "W1") == [("1100", "SGD", "-400.00", "-400.00", None), ("6010", "SGD", "400.00", "400.00", None)]
    # 3,600.00 over 36 months: 100.00 a month from January, on the month's last day, up to as_of.
    assert [(e["kind"], e["posting_date"]) for e in result["entries"] if e["document"] == "FA1"] == [
        ("depreciation", "2026-01-31"), ("depreciation", "2026-02-28")]
    assert result["dimension_balances"]["2026-01"]["6610"] == {"": "100.00"}


# ---------------------------------------------------------------------------- invalid scenarios
def _base() -> dict:
    data = scenario(journal("J1", "2026-01-05", tagged("6010", "SGD", "1.00", Department="ENG"),
                            line("1010", "SGD", "-1.00")))
    data["accounts"], data["dimensions"] = copy.deepcopy(CHART), copy.deepcopy(DIMENSIONS)
    return data


def _changed(change) -> dict:
    data = _base()
    change(data)
    return data


def _edit(data: dict, dims: dict) -> None:
    data["reversals"] = [{"document": "J1", "edit": {"version": 2, "date": "2026-01-05", "memo": "", "lines": [
        {**line("6010", "SGD", "1.00"), "dimensions": dims}, line("1010", "SGD", "-1.00")]}}]


def _opening(data: dict, dims: dict) -> None:
    data["as_of"], data["settings"] = "2026-01-31", {"books_start": "2026-01-01"}
    data["documents"].insert(0, balances({**line("1010", "SGD", "1.00"), "dimensions": dims},
                                         line("2010", "SGD", "-1.00")))


# req: FR-DIM-01, QA-ENG-02
@pytest.mark.parametrize(
    ("change", "match"),
    [
        (lambda d: d["dimensions"].append({"name": "Department", "active": False, "values": []}),
         "two dimensions are named"),
        (lambda d: d["dimensions"][0]["values"].append({"code": "ENG", "active": False}), "two values with the code"),
        (lambda d: d["accounts"][1].update(requires=["Product line"]), "requires 'Product line'"),
        (lambda d: d["documents"][0]["lines"][0]["dimensions"].update({"Product line": "A"}), "not in dimensions"),
        (lambda d: d["documents"][0]["lines"][0]["dimensions"].update(Department="HR"), "not a value of Department"),
        (lambda d: d["documents"][0]["lines"][0]["dimensions"].update(Department="eng"), "not a value of Department"),
        (lambda d: _edit(d, {"Project": "P-003"}), "reversals/0/edit.*not a value of Project"),
        (lambda d: _opening(d, {"Unit": "X"}), "documents/0.*not in dimensions"),
        # Python's re lets "$" match before a final newline; RE2 does not (README, "Invalid scenario").
        (lambda d: d["dimensions"][2].update(name="Region\n"), "does not match the schema's pattern"),
        (lambda d: d["dimensions"][2]["values"][0].update(code="SG\n"), "does not match the schema's pattern"),
        # The P1 sources carry no dimension values in the format (README, "Dimensions"): the schema refuses them.
        (lambda d: d.update(settings={}, as_of="2026-01-31", documents=[
            invoice("I1", "2026-01-05", "SGD", {**invoice_line("4020", "1.00", "ZR"), "dimensions": ENG})]),
         "scenario.schema.json"),
    ],
    ids=["two-names", "two-codes", "requires-undeclared", "line-dimension-undeclared", "line-code-undeclared",
         "codes-match-exactly", "edit-code-undeclared", "opening-dimension-undeclared", "name-with-newline",
         "code-with-newline", "values-on-an-invoice"],
)
def test_a_rule_of_dimensions_broken_makes_the_scenario_invalid(change, match):
    with pytest.raises(ScenarioError, match=match):
        scenario_from_json(_changed(change))


# req: FR-DIM-01
def test_values_out_of_use_and_requirements_of_dimensions_out_of_use_are_valid_input():
    # Whether a value is in use, and what an account requires, are posting rules: the scenario stays valid.
    scenario_from_json(_changed(lambda d: d["documents"][0]["lines"][0]["dimensions"].update(Department="OLD")))
    scenario_from_json(_changed(lambda d: d["documents"][0]["lines"][1].update(dimensions={"Region": "SG"})))


# ---------------------------------------------------------------------------- the golden case
# req: QA-ENG-02, FR-DIM-01, FR-DIM-02, FR-GL-02, FR-GL-06, FR-GL-12
def test_golden_dimensions_posts_what_was_worked_by_hand():
    # test/differential/golden/dimensions.json: Department (ENG, SALES; OLD out of use) and Project in use, Cost
    # centre out of use; 4010 requires Department, 6010 Department and Project, 6020 Cost centre. as_of 31 March.
    ledger = Ledger(load_scenario(GOLDEN / "dimensions.json"))
    r = ledger.result()
    assert ledger.reasons == [
        ("JNL-0002", "dimension-required"),  # 6010 with a project and no department (DIM-05)
        ("JNL-0005", "dimension-required"),  # USD 100.00 at 1.35 and -100.00 at 1.34: 6010 nets to 1.00 in base
        ("JNL-0006", "dimension-value-out-of-use"),  # OLD (DIM-06)
        ("JNL-0007", "dimension-out-of-use"),  # CC10, of Cost centre, out of use (DIM-06)
        ("INV-0001", "dimension-required"),  # an invoice line on 4010: the format's invoices carry no values
        ("JNL-0010/edit", "dimension-required"),  # the replacement held to DIM-05; nothing changes
        ("OB-0001/edit", "dimension-value-out-of-use"),  # the opening balances' edit held to DIM-06
    ]
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("OB-0001", "post", "2026-01-01"),
        ("JNL-0001", "post", "2026-01-20"),
        ("JNL-0003", "post", "2026-01-31"),  # 500.00 moved to ENG from no value: 6010 nets to zero (rev 0.172)
        ("JNL-0004", "post", "2026-02-10"),
        ("JNL-0008", "post", "2026-02-20"),  # 6020 requires only Cost centre, out of use
        ("JNL-0009", "post", "2026-02-28"),
        ("JNL-0010", "post", "2026-03-10"),
        ("JNL-0001", "reversal", "2026-03-15"),
        ("JNL-0010", "reversal", "2026-03-10"),
        ("JNL-0010", "replacement", "2026-03-10"),
        ("JNL-0009", "auto_reversal", "2026-03-01"),
    ]
    eng1, eng2 = {"Department": "ENG", "Project": "P-001"}, {"Department": "ENG", "Project": "P-002"}
    sales2, sales = {"Department": "SALES", "Project": "P-002"}, {"Department": "SALES"}
    # Canonical form: 6010's 1,500.00 and 250.00 for ENG/P-001 net into 1,750.00; its 500.00 with none stays apart
    # (not held to DIM-05), first.
    assert entry(r, "OB-0001") == [
        ("1010", "SGD", "20000.00", "20000.00", None), ("3010", "SGD", "-10000.00", "-10000.00", None),
        ("3100", "SGD", "-10250.00", "-10250.00", None), ("4010", "SGD", "-2000.00", "-2000.00", sales),
        ("6010", "SGD", "500.00", "500.00", None), ("6010", "SGD", "1750.00", "1750.00", eng1)]
    assert entry(r, "JNL-0004") == [("6010", "USD", "100.00", "135.00", eng2), ("1021", "USD", "-100.00", "-135.00", None)]
    assert entry(r, "JNL-0001", "reversal") == [("6010", "SGD", "-1200.00", "-1200.00", eng1),
                                                ("6010", "SGD", "-800.00", "-800.00", sales2),
                                                ("1010", "SGD", "2000.00", "2000.00", None)]
    assert entry(r, "JNL-0010", "reversal") == [("1010", "SGD", "-1000.00", "-1000.00", None),
                                                ("4010", "SGD", "1000.00", "1000.00", sales)]
    assert entry(r, "JNL-0010", "replacement") == [("1010", "SGD", "1000.00", "1000.00", None),
                                                   ("4010", "SGD", "-600.00", "-600.00", sales),
                                                   ("4010", "SGD", "-400.00", "-400.00", {"Department": "ENG"})]
    assert entry(r, "JNL-0009", "auto_reversal") == [("6010", "SGD", "-600.00", "-600.00", sales2),
                                                     ("2050", "SGD", "600.00", "600.00", None)]
    assert r["trial_balance"] == {
        # 20,000 - 2,000; 500 + 1,750 + 1,200 + 800 + 500 - 500
        "2026-01": {"1010": "18000.00", "3010": "-10000.00", "3100": "-10250.00", "4010": "-2000.00", "6010": "4250.00"},
        "2026-02": {"1010": "-400.00", "1021": "-135.00", "2050": "-600.00", "6010": "735.00", "6020": "400.00"},
        # 1010: 1,000 + 2,000 - 1,000 + 1,000; 6010: -1,200 - 800 - 600; 4010: -1,000 + 1,000 - 600 - 400
        "2026-03": {"1010": "3000.00", "2050": "600.00", "4010": "-1000.00", "6010": "-2600.00"},
    }
    assert r["dimension_balances"] == {
        "2026-01": {
            "1010": {"": "18000.00"}, "3010": {"": "-10000.00"}, "3100": {"": "-10250.00"},
            "4010": {"Department=SALES": "-2000.00"},
            # "" is 500.00 - 500.00, left out; ENG/P-001 1,750 + 1,200; ENG/P-002 the 500.00 moved
            "6010": {"Department=ENG|Project=P-001": "2950.00", "Department=ENG|Project=P-002": "500.00",
                     "Department=SALES|Project=P-002": "800.00"},
        },
        "2026-02": {
            "1010": {"": "-400.00"}, "1021": {"": "-135.00"}, "2050": {"": "-600.00"},
            "6010": {"Department=ENG|Project=P-002": "135.00", "Department=SALES|Project=P-002": "600.00"},
            "6020": {"": "400.00"},
        },
        "2026-03": {
            "1010": {"": "3000.00"}, "2050": {"": "600.00"},
            "4010": {"Department=ENG": "-400.00", "Department=SALES": "-600.00"},  # SALES: -1,000 + 1,000 - 600
            "6010": {"Department=ENG|Project=P-001": "-1200.00", "Department=SALES|Project=P-002": "-1400.00"},
        },
    }


# ---------------------------------------------------------------------------- properties
GENERATED = st.one_of(scenarios(), p1_scenarios(("general-ledger",)), p1_scenarios(("general-ledger", "purchases",
                                                                                     "sales", "bank", "claims")))


def _requires(data: dict) -> dict[str, set[str]]:
    active = {d["name"] for d in data.get("dimensions", []) if d["active"]}
    return {a["code"]: set(a.get("requires", ())) & active for a in data["accounts"]}


# req: FR-DIM-02, QA-ENG-01
@given(GENERATED)
def test_each_months_sets_of_an_account_add_up_to_its_trial_balance(data):
    result = Ledger(scenario_from_json(data)).result()
    if "dimensions" not in data:
        assert "dimension_balances" not in result
        assert all("dimensions" not in ln for e in result["entries"] for ln in e["lines"])
        return
    sets = result["dimension_balances"]
    assert set(sets) == set(result["trial_balance"])
    for month, accounts in result["trial_balance"].items():
        for account in set(accounts) | set(sets[month]):
            total = money.total([Decimal(v) for v in sets[month].get(account, {}).values()])
            assert total == Decimal(accounts.get(account, "0")), (month, account)
            assert all(Decimal(v) != 0 for v in sets[month].get(account, {}).values())


# req: FR-DIM-01, FR-DIM-02
@given(GENERATED)
def test_what_posts_names_values_in_use_and_what_its_account_requires_or_moves_between_values(data):
    ledger = Ledger(scenario_from_json(data))
    declared = {d["name"]: d for d in data.get("dimensions", [])}
    requires = _requires(data)
    journals = {d["id"] for d in data["documents"] if d["type"] in ("manual_journal", "opening_balances")}
    for entry_ in ledger.entries:
        if entry_.kind not in ("post", "replacement"):
            continue
        for ln in entry_.lines:
            for name, code in ln.dimensions:  # DIM-06
                assert declared[name]["active"] and any(v["code"] == code and v["active"] for v in declared[name]["values"])
        if entry_.document not in journals:  # a P1 entry: no values, and nothing a person entered on a requiring account
            assert all(not ln.dimensions for ln in entry_.lines)
            continue
        if any(d["id"] == entry_.document and d["type"] == "opening_balances" for d in data["documents"]):
            continue  # not held to DIM-05
        for ln in entry_.lines:  # DIM-05, or a move between values on that account (rev 0.172)
            if requires.get(ln.account, set()) - {name for name, _ in ln.dimensions}:
                assert money.total([x.base_amount for x in entry_.lines if x.account == ln.account]).is_zero()


# req: FR-DIM-02, FR-GL-12, ENG-14
@given(GENERATED)
def test_a_reversed_journal_nets_to_zero_in_every_set(data):
    ledger = Ledger(scenario_from_json(data))
    totals: dict[str, dict[tuple, Decimal]] = defaultdict(lambda: defaultdict(lambda: money.ZERO))
    reversed_ = {e.document for e in ledger.entries if e.kind in ("reversal", "auto_reversal")}
    replaced = {e.document for e in ledger.entries if e.kind == "replacement"}
    for e in ledger.entries:
        if e.document in reversed_ and e.document not in replaced:
            for ln in e.lines:
                key = (ln.account, ln.currency, ln.dimensions)
                totals[e.document][key] = money.EXACT.add(totals[e.document][key], ln.base_amount)
    for doc_id, sets in totals.items():
        assert all(v == 0 for v in sets.values()), doc_id


_FIND = settings(max_examples=3000, database=None, phases=[Phase.generate], suppress_health_check=list(HealthCheck))


# req: QA-ENG-02, FR-DIM-01
@pytest.mark.parametrize(
    "reason", ["dimension-required", "dimension-value-out-of-use", "dimension-out-of-use"])
def test_the_manual_journal_generator_draws_each_dimension_rule(reason):
    # A quarter of the generated scenarios declare dimensions, and each of rule 11's refusals is drawn. (Whether each
    # journal posts or is refused as built is test_properties' check, over these scenarios too.)
    find(scenarios(), lambda data: reason in dict(Ledger(scenario_from_json(data)).reasons).values(), settings=_FIND)


def _moved(data: dict) -> bool:
    requires = _requires(data)
    for e in Ledger(scenario_from_json(data)).entries:
        lines = [ln for ln in e.lines if requires.get(ln.account) - {n for n, _ in ln.dimensions}] \
            if e.kind == "post" else []
        if lines and any(ln.dimensions for ln in e.lines if ln.account == lines[0].account):
            return True
    return False


# req: QA-ENG-02, FR-DIM-01
def test_the_manual_journal_generator_draws_a_move_between_values_that_posts():
    find(scenarios(), lambda data: "dimensions" in data and _moved(data), settings=_FIND)


@st.composite
def _opening_case(draw: st.DrawFn) -> tuple[dict, str, str]:
    """The general ledger source's opening balances with dimensions, drawn directly (their refusal for a value out of
    use is rare among the source's scenarios): a scenario holding them, the kind and the intent."""
    dims = draw(strategies_gl.dimensions())
    kind = draw(st.sampled_from(sorted(set(strategies_gl.opening_kinds(dims)))))
    document, meant, _ = draw(strategies_gl.opening_balances("OB-0001", date(2026, 1, 1), kind, dims))
    accounts = strategies_p1._accounts() + strategies_p1._founders_accounts() + strategies_gl.accounts()
    data = {
        "schema": "fyniq-scenario@1", "name": "opening", "base_currency": "SGD",
        "currencies": dict(strategies_gl.CURRENCIES), "as_of": "2026-01-31", "settings": {"books_start": "2026-01-01"},
        "people": list(strategies_gl.PEOPLE), "accounts": accounts + strategies_gl.dimension_accounts(dims),
        "dimensions": dims.declared, "documents": [document], "reversals": [],
    }
    return data, kind, meant


# req: QA-ENG-02, FR-DIM-01, FR-GL-06
@given(_opening_case())
def test_the_general_ledger_sources_opening_balances_with_dimensions_post_or_are_refused_as_built(case):
    data, kind, meant = case
    ledger = Ledger(scenario_from_json(data))
    assert ledger.rejected == ([] if meant == POST else ["OB-0001"]), ledger.reasons
    if kind in OPENING_DIMENSION_REJECTED:
        assert ledger.reasons[0][1] in ("dimension-value-out-of-use", "dimension-out-of-use")


# req: QA-ENG-02, FR-DIM-01, FR-GL-06
def test_the_general_ledger_source_draws_opening_balances_naming_a_value_out_of_use():
    find(_opening_case(), lambda case: case[1] in OPENING_DIMENSION_REJECTED, settings=_FIND)


# req: QA-ENG-02, FR-DIM-01
@pytest.mark.parametrize("kind", sorted(set(GL_DIMENSION_KINDS)))
def test_the_general_ledger_source_draws_each_dimension_kind_as_meant(kind):
    def drawn(pair):
        return kind in pair[1].kinds.values()

    data, intent = find(p1_scenarios_with_intent(("general-ledger",)), drawn, settings=_FIND)
    ledger = Ledger(scenario_from_json(data))
    reasons = dict(ledger.reasons)
    for doc_id, built in intent.kinds.items():
        if built == kind:
            assert (doc_id in ledger.rejected) == (intent.documents[doc_id] == REJECT), reasons
            if kind in JOURNAL_DIMENSION_REJECTED:
                assert intent.documents[doc_id] == REJECT and reasons[doc_id].startswith("dimension-"), reasons
            if intent.documents[doc_id] == REJECT and kind == "document-dimension-required":
                assert reasons[doc_id] == "dimension-required"
    refused = [key for key in ledger.rejected if "/" in key]
    assert refused == [key for key, meant, _ in intent.corrections if meant == REJECT]

