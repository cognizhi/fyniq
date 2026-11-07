"""The option pool and grants (FR-EQ-19, design D9; test/differential/README.md, "The option pool and grants" and "Cap
tables"), figures worked out by hand.

golden/option-pool.json: the founder's 1,000,000 shares of 3010 from 5 Jan 2027; a pool of 100,000 from 10 Jan; P0003's
grant of 48,000 on 1 Feb; a resolution of 150,000 from 1 Mar and P0004's grant of the 102,000 it leaves on 2 Mar.
Fully diluted on 1 Feb the company is 1,000,000 + 48,000 + 52,000 unallocated = 1,100,000: the founder 1,000,000 ÷
1,100,000 = 90.909…% → 90.91, P0003 48,000 ÷ 1,100,000 = 4.3636…% → 4.36. On 2 Mar it is 1,150,000, nothing unallocated:
86.956…% → 86.96, 4.1739…% → 4.17 and 8.8695…% → 8.87.
"""

import json
from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario, scenario_from_json

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden" / "option-pool.json"


def _line(holder, issued, diluted, ownership, share):
    return {"holder": holder, "class": "3010", "issued": issued, "votes": issued, "fully_diluted": diluted,
            "ownership": ownership, "voting": ownership, "diluted": share}


# req: FR-EQ-19
# req: FR-EQ-20
# req: QA-ENG-02
def test_the_pool_covers_its_grants_on_every_day():
    r = Ledger(load_scenario(GOLDEN)).result()
    # OG-0001 comes before any pool; OP-0002 sets aside no shares and OP-0003 shares of 2040; OG-0003 makes 108,000 by
    # 1 Feb, and OG-0004, dated 20 Jan, fits then but makes 108,000 by 1 Feb too; OG-0005 is over 3020, not the pool's
    # class; P0009 is no holder; 1.5 is no whole number; OP-0004's 40,000 from 15 Jan is short of the 48,000 granted by
    # 1 Feb; and once OG-0008 takes the last 102,000, OG-0009's one option is refused.
    assert r["rejected"] == ["OG-0001", "OP-0002", "OP-0003", "OG-0003", "OG-0004", "OG-0005", "OG-0006", "OG-0007",
                             "OP-0004", "OG-0009"]
    assert [e["document"] for e in r["entries"]] == ["BT-0001", "EQ-0001"]  # neither a pool nor a grant posts
    before, first, second = r["cap_tables"]
    assert before == {"at": "2027-01-09", "issued": "1000000", "votes": "1000000", "options": "0", "unallocated": "0",
                      "fully_diluted": "1000000", "lines": [_line("P0001", "1000000", "1000000", "100.00", "100.00")],
                      "holders": [{k: v for k, v in _line("P0001", "1000000", "1000000", "100.00", "100.00").items()
                                   if k != "class"}]}
    assert (first["options"], first["unallocated"], first["fully_diluted"]) == ("48000", "52000", "1100000")
    assert first["lines"] == [_line("P0001", "1000000", "1000000", "100.00", "90.91"),
                              {**_line("P0003", "0", "48000", "0.00", "4.36"), "votes": "0", "voting": "0.00"}]
    assert (second["options"], second["unallocated"], second["fully_diluted"]) == ("150000", "0", "1150000")
    assert [(ln["holder"], ln["diluted"]) for ln in second["lines"]] == [("P0001", "86.96"), ("P0003", "4.17"),
                                                                         ("P0004", "8.87")]


# req: FR-EQ-19
def test_of_two_resolutions_on_one_day_the_later_is_in_force():
    data = json.loads(GOLDEN.read_text())
    data["documents"] = data["documents"][:2] + [
        {"type": "option_pool", "id": "OP-0001", "version": 1, "date": "2027-01-10", "size": "100000", "class": "3010"},
        {"type": "option_pool", "id": "OP-0002", "version": 1, "date": "2027-01-10", "size": "60000", "class": "3010"},
        {"type": "option_grant", "id": "OG-0001", "version": 1, "date": "2027-02-01", "person": "P0003", "number": "60001",
         "class": "3010"},
    ]
    data["cap_tables"] = ["2027-01-10"]
    r = Ledger(scenario_from_json(data)).result()
    assert r["rejected"] == ["OG-0001"]  # the later resolution of 60,000 is in force
    assert r["cap_tables"][0]["unallocated"] == "60000"
