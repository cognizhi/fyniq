"""The cap table (FR-EQ-20; test/differential/README.md, "Cap tables" and "Share transfers"), figures worked out by hand.

golden/cap-table.json is G.3's round: 1,000,000 founder shares, the investor's 200,000 at S$3.00 and 52,083 at S$2.40
(the SAFE's, issued for cash until the format has instruments), all on 3010, one vote a share; then a transfer of
50,000 from the founder to the investor and 10,000 preference shares on 3020, with no votes. Each percentage is
part ÷ total × 100 to 2 decimals half-up, worked with exact fractions.
"""

import json
from pathlib import Path

import pytest

from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, load_scenario, scenario_from_json

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden" / "cap-table.json"


def _line(holder, share_class, issued, votes, ownership, voting):
    return {"holder": holder, "class": share_class, "issued": issued, "votes": votes, "fully_diluted": issued,
            "ownership": ownership, "voting": voting, "diluted": ownership}


def _holder(holder, issued, votes, ownership, voting):
    return {"holder": holder, "issued": issued, "votes": votes, "fully_diluted": issued, "ownership": ownership,
            "voting": voting, "diluted": ownership}


# req: FR-EQ-20
# req: FR-EQ-17
# req: QA-ENG-02
def test_the_cap_table_at_each_day():
    r = Ledger(load_scenario(GOLDEN)).result()
    # 52,084 is one more than P0003 holds; P0002 holds nothing on 1 Mar (their shares come on 12 Mar); a transfer to
    # oneself; 2040 is not share capital; P0009 is no holder; 1.5 is no whole number; and EQ-0013's one share on 30 Apr
    # is held that day but would leave P0003 at -1 from 10 May, when EQ-0012 moved all 52,083 (FY288: any day).
    assert r["rejected"] == ["EQ-0006", "EQ-0007", "EQ-0008", "EQ-0009", "EQ-0010", "EQ-0011", "EQ-0013"]
    # The transfers post nothing; the issues post Dr 2040 · Cr share capital (shares x price).
    assert [e["document"] for e in r["entries"]] == ["BT-0001", "EQ-0001", "BT-0002", "BT-0003", "EQ-0002", "EQ-0003",
                                                     "BT-0004", "EQ-0005"]
    day_before, round_day, april, may = r["cap_tables"]
    # 11 Mar: the founder alone.
    assert day_before == {"at": "2027-03-11", "issued": "1000000", "votes": "1000000", "options": "0", "unallocated": "0", "fully_diluted": "1000000",
                          "lines": [_line("P0001", "3010", "1000000", "1000000", "100.00", "100.00")],
                          "holders": [_holder("P0001", "1000000", "1000000", "100.00", "100.00")]}
    # 12 Mar, G.3: 1,000,000, 200,000 and 52,083 of 1,252,083 are 79.8669…, 15.9733… and 4.1597… per cent.
    assert round_day["issued"] == round_day["votes"] == round_day["fully_diluted"] == "1252083"
    assert round_day["lines"] == [_line("P0001", "3010", "1000000", "1000000", "79.87", "79.87"),
                                  _line("P0002", "3010", "200000", "200000", "15.97", "15.97"),
                                  _line("P0003", "3010", "52083", "52083", "4.16", "4.16")]
    # 15 Apr: 1,262,083 issued, 1,252,083 voting (the preference shares carry none). 950,000 ÷ 1,262,083 = 75.2724…;
    # 250,000 is 19.8085…; 10,000 is 0.7923…; 52,083 is 4.1267…. Votes: 950,000 ÷ 1,252,083 = 75.8736…, 250,000 is
    # 19.9667…, 52,083 is 4.1597…. P0002's 260,000 is 20.6009… of the shares, from the exact sum.
    assert (april["issued"], april["votes"], april["fully_diluted"]) == ("1262083", "1252083", "1262083")
    assert april["lines"] == [_line("P0001", "3010", "950000", "950000", "75.27", "75.87"),
                              _line("P0002", "3010", "250000", "250000", "19.81", "19.97"),
                              _line("P0002", "3020", "10000", "0", "0.79", "0.00"),
                              _line("P0003", "3010", "52083", "52083", "4.13", "4.16")]
    assert april["holders"] == [_holder("P0001", "950000", "950000", "75.27", "75.87"),
                                _holder("P0002", "260000", "250000", "20.60", "19.97"),
                                _holder("P0003", "52083", "52083", "4.13", "4.16")]
    # 31 May: P0003's 52,083 moved to the founder on 10 May, so P0003 has no line; 1,002,083 is 79.3995… of the shares
    # and 80.0332… of the votes.
    assert may["lines"] == [_line("P0001", "3010", "1002083", "1002083", "79.40", "80.03"),
                            _line("P0002", "3010", "250000", "250000", "19.81", "19.97"),
                            _line("P0002", "3020", "10000", "0", "0.79", "0.00")]
    assert [h["holder"] for h in may["holders"]] == ["P0001", "P0002"]


# req: FR-EQ-20
def test_a_conversion_moves_its_shares_and_no_vote_is_no_percentage():
    data = json.loads(GOLDEN.read_text())
    data["accounts"].append({"code": "2500", "type": "liability", "group": False, "active": True, "role": "director-loan"})
    data["accounts"][3]["votes_per_share"] = "0"  # 3010 now carries no votes either
    data["documents"] = [
        {"type": "opening_balances", "id": "OB", "version": 1, "date": "2027-01-01", "memo": "Loan",
         "lines": [{"account": "1010", "currency": "SGD", "amount": "100.00"},
                   {"account": "2500", "currency": "SGD", "amount": "-100.00", "party": "P0001", "origin": "lent"}]},
        # RND-SHARE: 100.00 at 1.20 allots 83 shares (99.60), 0.40 left owed.
        {"type": "director_conversion", "id": "SI-0001", "version": 1, "date": "2027-02-01", "person": "P0001",
         "account": "2500", "class": "3010", "amount": "100.00", "price": "1.20"},
    ]
    data["settings"]["accounts"]["director_loan"] = "2500"
    data["cap_tables"] = ["2027-01-31", "2027-02-01"]
    r = Ledger(scenario_from_json(data)).result()
    assert r["rejected"] == []
    before, after = r["cap_tables"]
    assert before == {"at": "2027-01-31", "issued": "0", "votes": "0", "options": "0", "unallocated": "0", "fully_diluted": "0", "lines": [], "holders": []}
    assert after["lines"] == [{"holder": "P0001", "class": "3010", "issued": "83", "votes": "0", "fully_diluted": "83",
                               "ownership": "100.00", "voting": None, "diluted": "100.00"}]


# req: FR-EQ-20
def test_votes_per_share_belong_to_share_capital():
    data = json.loads(GOLDEN.read_text())
    data["accounts"][1]["votes_per_share"] = "1"  # 2040, share application monies
    with pytest.raises(ScenarioError, match="votes_per_share"):
        scenario_from_json(data)
