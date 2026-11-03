"""Director's accounts: each line names its person, and each person's totals (FR-EQ-01, B.7; PRD-001 rev 0.167).

Worked by hand. Mei Lin (P0001) lends S$1,500.00 into the bank on 5 January and is repaid S$400.00 on 9
January: on 2500 she has −1,500.00 and +400.00, a total of −1,100.00 (the company owes her S$1,100.00).
Arjun (P0002) pays S$300.00 of costs personally on 7 January, kept as a loan (−300.00 on 2500), and has
S$250.00 of fees owed on 2120 (−250.00). Mei Lin's loan journal is then reversed on 12 January, so her
total is +400.00: she owes the company S$400.00.
"""

import pytest
from builders import journal, line, scenario

from oracle.ledger import Ledger
from oracle.scenario import scenario_from_json

DIRECTOR_ACCOUNTS = [
    {"code": "2120", "type": "liability", "group": False, "active": True, "role": "deferred-remuneration"},
    {"code": "2500", "type": "liability", "group": False, "active": True, "role": "director-loan"},
]


def person(account: str, amount: str, party: str | None, origin: str | None) -> dict:
    out = line(account, "SGD", amount)
    if party is not None:
        out["party"] = party
    if origin is not None:
        out["origin"] = origin
    return out


def run(*documents: dict, reversals: tuple = ()) -> tuple[dict, Ledger]:
    data = scenario(*documents, reversals=reversals, accounts=DIRECTOR_ACCOUNTS)
    data["people"] = ["P0001", "P0002"]
    ledger = Ledger(scenario_from_json(data))
    return ledger.result(), ledger


LENT = journal("JNL-0001", "2026-01-05", line("1010", "SGD", "1500.00"), person("2500", "-1500.00", "P0001", "lent"))
COSTS = journal("JNL-0002", "2026-01-07", line("6010", "SGD", "300.00"), person("2500", "-300.00", "P0002", "paid_personally"))
REPAID = journal("JNL-0003", "2026-01-09", person("2500", "400.00", "P0001", "paid_out"), line("1010", "SGD", "-400.00"))
FEES = journal("JNL-0004", "2026-01-10", line("6010", "SGD", "250.00"), person("2120", "-250.00", "P0002", "fee"))


# req: FR-EQ-01
# req: QA-ENG-01
def test_each_persons_totals_on_the_directors_accounts():
    result, _ = run(LENT, COSTS, REPAID, FEES)
    assert result["rejected"] == []
    assert result["people"] == {"P0001": {"2500": "-1100.00"}, "P0002": {"2120": "-250.00", "2500": "-300.00"}}
    # The person is on the director's account's line, and only there.
    lent = result["entries"][0]["lines"]
    assert "party" not in lent[0] and lent[1]["party"] == "P0001"
    # The sub-ledger adds up to the accounts.
    assert result["trial_balance"]["2026-01"]["2500"] == "-1400.00"


# req: FR-EQ-01
# req: FR-GL-12
def test_a_reversal_keeps_the_person():
    result, _ = run(LENT, REPAID, reversals=(("JNL-0001", "2026-01-12"),))
    reversal = result["entries"][-1]
    assert reversal["kind"] == "reversal" and reversal["lines"][1]["party"] == "P0001"
    assert result["people"] == {"P0001": {"2500": "400.00"}}


# req: FR-EQ-01
def test_nobody_named_leaves_people_out():
    result, _ = run(journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00"), line("4010", "SGD", "-10.00")))
    assert "people" not in result


# req: FR-EQ-01
# req: QA-ENG-01
@pytest.mark.parametrize(
    ("lines", "reason"),
    [
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", None, None)), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0009", "lent")), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0001", None)), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0001", "gift")), "director-line-without-person"),
        ((person("1010", "10.00", "P0001", "lent"), person("2500", "-10.00", "P0001", "lent")), "person-on-another-account"),
        ((person("1010", "10.00", None, "lent"), person("2500", "-10.00", "P0001", "lent")), "person-on-another-account"),
    ],
)
def test_a_line_without_its_person_or_a_person_elsewhere_is_rejected(lines, reason):
    result, ledger = run(journal("JNL-0001", "2026-01-05", *lines))
    assert result["rejected"] == ["JNL-0001"]
    assert result["entries"] == [] and "people" not in result
    assert ledger.reasons == [("JNL-0001", reason)]
