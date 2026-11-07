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

import pytest

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


EVENTS = GOLDEN.parent / "option-events.json"


# req: FR-EQ-19
# req: QA-ENG-02
def test_forfeitures_and_expiries_give_options_back_to_the_pool():
    r = Ledger(load_scenario(EVENTS)).result()
    # OG-0003 when the pool is full; OG-0007's cliff of 13 months past its 12 of vesting; OE-0001 forfeits one of OG-0002,
    # which vested all on its day; OE-0002 takes 48,001 of 48,000; OE-0003 is before its grant; OE-0004 names no grant;
    # OE-0005 is no kind of event; OG-0004 on 14 Mar comes the day before OE-0006 gave 40,000 back; OE-0007 takes 8,001
    # of the 8,000 left; OP-0002's 91,999 is short of the 92,000 granted and kept by 21 Mar; and OE-0009 forfeits 2,601
    # when 2,200 of OG-0006's 4,800 are vested: 11 whole months from 31 Mar 2027 to 29 Feb 2028 (the last day of a
    # month too short for the 31st), past the 6-month cliff, 4,800 × 11 ÷ 24 = 2,200.
    assert r["rejected"] == ["OG-0003", "OG-0007", "OE-0001", "OE-0002", "OE-0003", "OE-0004", "OE-0005", "OG-0004",
                             "OE-0007", "OP-0002", "OE-0009"]
    assert [e["document"] for e in r["entries"]] == ["BT-0001", "EQ-0001"]  # no event posts
    full, forfeited, lapsed, leap = r["cap_tables"]
    diluted = {t["at"]: [(ln["holder"], ln["fully_diluted"], ln["diluted"]) for ln in t["lines"]] for t in r["cap_tables"]}
    # 1 Feb: 48,000 + 52,000 granted, the pool full: 4.36% and 4.73% of 1,100,000.
    assert (full["options"], full["unallocated"], full["fully_diluted"]) == ("100000", "0", "1100000")
    assert diluted["2027-02-01"] == [("P0001", "1000000", "90.91"), ("P0003", "48000", "4.36"), ("P0004", "52000", "4.73")]
    # 15 Mar: 40,000 of OG-0001 forfeited and granted again to P0004 (OG-0005): 8,000 ÷ 1,100,000 = 0.727…% → 0.73.
    assert (forfeited["options"], forfeited["unallocated"]) == ("100000", "0")
    assert diluted["2027-03-15"] == [("P0001", "1000000", "90.91"), ("P0003", "8000", "0.73"), ("P0004", "92000", "8.36")]
    # 20 Mar: OG-0001's last 8,000 lapse, so P0003 has no line, and the pool leaves 8,000.
    assert (lapsed["options"], lapsed["unallocated"], lapsed["fully_diluted"]) == ("92000", "8000", "1100000")
    assert diluted["2027-03-20"] == [("P0001", "1000000", "90.91"), ("P0004", "92000", "8.36")]
    # 29 Feb 2028: OG-0006's 4,800 less the 2,600 not vested, forfeited: 2,200, 0.20%; 5,800 unallocated.
    assert (leap["options"], leap["unallocated"], leap["fully_diluted"]) == ("94200", "5800", "1100000")
    assert diluted["2028-02-29"] == [("P0001", "1000000", "90.91"), ("P0003", "2200", "0.20"), ("P0004", "92000", "8.36")]


# req: FR-EQ-19
@pytest.mark.parametrize(("granted", "day", "months"), [
    ("2027-01-31", "2027-02-27", 0), ("2027-01-31", "2027-02-28", 1),  # February's last day stands for the 31st
    ("2027-01-31", "2027-03-30", 1), ("2027-01-31", "2027-03-31", 2),
    ("2027-03-31", "2028-02-29", 11), ("2027-01-15", "2027-01-15", 0), ("2027-01-15", "2028-01-14", 11),
])
def test_a_month_passes_on_the_grants_day_or_a_short_months_last(granted, day, months):
    from datetime import date

    from oracle import captable

    assert captable._months(date.fromisoformat(granted), date.fromisoformat(day)) == months
    g = captable.Grant("P0003", "3010", 4800, date.fromisoformat(granted), cliff=1, months=24)
    assert captable.vested(g, date.fromisoformat(day)) == (0 if months < 1 else 4800 * months // 24)


EXERCISES = GOLDEN.parent / "option-exercise.json"


# req: FR-EQ-19
# req: QA-ENG-02
def test_an_exercise_is_an_issue_for_cash_within_what_is_vested():
    r = Ledger(load_scenario(EXERCISES)).result()
    # EQ-0003 once P0004's S$2,000.00 paid for EQ-0002's 4,000 at S$0.50; EQ-0004 before OG-0001's 12-month cliff;
    # EQ-0005 of OG-0003, at no price; EQ-0006 of no grant; EQ-0007 of 1.5 options; EQ-0008 of 12,001 when 12,000 are
    # vested on 20 Jan 2028 (12 whole months from 15 Jan 2027, × 48,000 ÷ 48); OE-0001 forfeits 36,001 when 36,000 are
    # not vested; OE-0003 lets lapse one of a grant with nothing left.
    assert r["rejected"] == ["EQ-0003", "EQ-0004", "EQ-0005", "EQ-0006", "EQ-0007", "EQ-0008", "OE-0001", "OE-0003"]
    lines = {e["document"]: [(ln["account"], ln["amount"], ln.get("party")) for ln in e["lines"]] for e in r["entries"]}
    # Each exercise posts as a cash issue: Dr 2040 naming the holder · Cr 3010, shares × the exercise price.
    assert lines["EQ-0002"] == [("2040", "2000.00", "P0004"), ("3010", "-2000.00", None)]
    assert lines["EQ-0009"] == [("2040", "1200.00", "P0003"), ("3010", "-1200.00", None)]
    first, last = r["cap_tables"]
    # 2 Feb 2027: P0004 holds 4,000 shares and 6,000 + 1,000 options: 11,000 ÷ 1,100,000 = 1.00%; the pool leaves
    # 100,000 − 59,000 = 41,000.
    assert (first["issued"], first["options"], first["unallocated"], first["fully_diluted"]) == ("1004000", "55000", "41000", "1100000")
    assert [(ln["holder"], ln["issued"], ln["fully_diluted"], ln["diluted"]) for ln in first["lines"]] == [
        ("P0001", "1000000", "1000000", "90.91"), ("P0003", "0", "48000", "4.36"), ("P0004", "4000", "11000", "1.00")]
    # 22 Jan 2028: P0003's 12,000 exercised, 36,000 forfeited; P0004's 6,000 left of OG-0002 lapse; the pool has the
    # 42,000 back, 83,000 unallocated; the exercised options never return.
    assert (last["issued"], last["options"], last["unallocated"], last["fully_diluted"]) == ("1016000", "1000", "83000", "1100000")
    assert [(ln["holder"], ln["issued"], ln["fully_diluted"], ln["diluted"]) for ln in last["lines"]] == [
        ("P0001", "1000000", "1000000", "90.91"), ("P0003", "12000", "12000", "1.09"), ("P0004", "4000", "5000", "0.45")]
