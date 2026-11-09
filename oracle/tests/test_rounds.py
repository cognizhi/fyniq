"""Rounds modelled (FR-EQ-21; test/differential/README.md, "Rounds"), figures worked out by hand.

golden/rounds.json: 1,000,000 founder shares (P0001); the pre-money SAFE SAFE-0001 of S$125,000 with a S$2,500,000 cap
and a 20% discount (P0003); the post-money SAFE SAFE-0002 of S$125,000 with a S$2,500,000 cap (P0004).
"""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden" / "rounds.json"


def _holders(r):
    return [(h["holder"], h["ownership_after"], h["diluted_after"], h["dilution"]) for h in r["holders"]]


# req: FR-EQ-21
# req: RND-SHARE
# req: QA-ENG-02
def test_g3s_round_and_its_variants():
    g3, after, before, stranger, unknown, small = Ledger(load_scenario(GOLDEN)).result()["rounds"]
    # G.3: min(2.5, 3.00 × 0.8 = 2.40) = 2.40, so RND-SHARE gives 52,083 shares for S$124,999.20 (S$0.80 left);
    # S$600,000 ÷ 3.00 = 200,000. 1,000,000 ÷ 1,252,083 = 79.866…% → 79.87, 100 − 79.866… = 20.133… → 20.13 points.
    assert (g3["price"], g3["pre_money"], g3["post_money"], g3["new_shares"]) == ("3", "3000000.00", "3756249.00", "252083")
    assert g3["conversions"] == [{"instrument": "SAFE-0001", "price": "2.4", "shares": "52083", "converted": "124999.20", "left": "0.80"}]
    assert g3["issues"] == [{"person": "P0002", "shares": "200000", "amount": "600000.00", "left": "0.00"}]
    assert _holders(g3) == [("P0001", "79.87", "79.87", "20.13"), ("P0002", "15.97", "15.97", "-15.97"), ("P0003", "4.16", "4.16", "-4.16")]
    assert g3["holders"][0]["pro_rata"] == "252083"  # the founder holds all before: entitled to all the new shares
    # By a pre-money valuation of S$3,000,000 over 1,000,000: S$3 a share again; 200,000 more in the pool after the
    # money: 1,452,083 fully diluted, 3 × 1,452,083 = 4,356,249.00; the founder 1,000,000 ÷ 1,452,083 = 68.866…%.
    assert (after["price"], after["post_money"], after["fully_diluted_after"], after["unallocated_after"]) == ("3", "4356249.00", "1452083", "200000")
    assert _holders(after)[0] == ("P0001", "79.87", "68.87", "31.13")
    # The top-up before the money: 3,000,000 ÷ 1,200,000 = 2.5; P0002's S$600,000 buys 240,000 and P0001's S$100,000
    # 40,000; the post-money SAFE's cap price 2,500,000 × 0.95 ÷ 1,000,000 = 2.375 < 2.5: 52,631 shares, S$1.37 left.
    assert (before["price"], before["pre_money"], before["new_shares"], before["post_money"]) == ("2.5", "3000000.00", "332631", "3831577.50")
    assert [(i["person"], i["shares"]) for i in before["issues"]] == [("P0002", "240000"), ("P0001", "40000")]
    assert before["conversions"][0]["price"] == "2.375" and before["conversions"][0]["left"] == "1.37"
    assert _holders(before)[0] == ("P0001", "78.04", "67.86", "32.14")
    # P0009 is no person or subscriber; SAFE-0009 was never recorded; S$1.00 buys no share at S$3.00.
    assert (stranger, unknown, small) == (None, None, None)
