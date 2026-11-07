"""SAFEs and their conversion at a priced round (FR-EQ-18, RND-SHARE, Appendix G.3, G4-044; test/differential/README.md,
"SAFEs and their conversion"), figures worked out by hand.

golden/safe-conversion.json is G4-044, then a second round with a discount SAFE, an MFN SAFE and a pre-money SAFE;
golden/safe-conversion-caps.json two post-money SAFEs with different caps and G.3's pre-money SAFE at one round. Each
price is worked with exact fractions, cut down to the SAFE's decimals; RND-SHARE allots the largest n whole shares with
round(n × price) at most the amount.
"""

from decimal import Decimal
from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario, scenario_from_json

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden"


def _run(name):
    return Ledger(load_scenario(GOLDEN / f"{name}.json")).result()


def _lines(result, document):
    for e in result["entries"]:
        if e["document"] == document:
            return [(ln["account"], ln["amount"], ln.get("instrument")) for ln in e["lines"]]
    raise AssertionError(f"no entry for {document}")


def _owners(table):
    return [(h["holder"], h["issued"], h["ownership"]) for h in table["holders"]]


# req: FR-EQ-18
# req: RND-SHARE
# req: FR-EQ-20
# req: QA-ENG-02
def test_g4_044_and_a_later_round():
    r = _run("safe-conversion")
    # BT-0090 puts money on 2700 naming no SAFE, BT-0091 a SAFE never recorded, BT-0092 names SAFE-0001 on a line with
    # no part on 2700; JNL-0001 is a journal on 2700. SAFE-0090 is a convertible note (not in the format), SAFE-0091 a
    # post-money SAFE with no cap, SAFE-0092 a stranger's, SAFE-0093 a discount of 100, SAFE-0094 converts into 2040,
    # SAFE-0095 has 7 decimals. EQ-0099 names a round not posted yet, EQ-0098 is dated before its round, EQ-0097 names a
    # bank line, EQ-0096 finds nothing left to convert, and EQ-0095 finds S$20,000.00 of SAFE-0003's S$30,000.00 on 2700.
    assert r["rejected"] == ["BT-0090", "BT-0091", "BT-0092", "JNL-0001", "SAFE-0090", "SAFE-0091", "SAFE-0092",
                             "SAFE-0093", "SAFE-0094", "SAFE-0095", "EQ-0099", "EQ-0098", "EQ-0097", "EQ-0096", "EQ-0095"]
    # The SAFE's money on 2700, naming it; a recorded SAFE posts nothing.
    assert _lines(r, "BT-0002") == [("1010", "125000.00", None), ("2700", "-125000.00", "SAFE-0001")]
    assert "SAFE-0001" not in {e["document"] for e in r["entries"]}
    # G4-044: f = 125,000 ÷ 2,500,000 = 0.05 and C₀ = 1,000,000, so the cap price is 2,500,000 × 0.95 ÷ 1,000,000 =
    # 2.375, below the round's 3.00. 52,631 × 2.375 = 124,998.625, so S$124,998.63; 52,632 would be 125,001.00.
    assert _lines(r, "EQ-0003") == [("2700", "124998.63", "SAFE-0001"), ("3010", "-124998.63", None)]
    before, round_day, june = r["cap_tables"]
    assert _owners(before) == [("P0001", "1000000", "100.00")]
    # 1,000,000, 200,000 and 52,631 of 1,252,631 are 79.8319…, 15.9664… and 4.2016… per cent.
    assert round_day["issued"] == "1252631"
    assert _owners(round_day) == [("P0001", "1000000", "79.83"), ("P0002", "200000", "15.97"),
                                  ("P0003", "52631", "4.20")]
    # The round of 1 Jun at S$4.00, over C₀ = 1,252,631 (the shares at the end of 31 May):
    # - SAFE-0002, 15% off: 4.00 × 0.85 = 3.40; 50,000 ÷ 3.40 = 14,705.88…, so 14,705 for S$49,997.00, S$3.00 left;
    # - SAFE-0003, MFN at the round's price: 30,000 ÷ 4.00 = 7,500 exactly;
    # - SAFE-0004, pre-money: 3,000,000 ÷ 1,252,631 = 2.394959…, cut to 2.3949; 8,351 × 2.3949 = 19,999.8099, so
    #   S$19,999.81 (8,352 would be 20,002.2648), S$0.19 left.
    assert _lines(r, "EQ-0005") == [("2700", "49997.00", "SAFE-0002"), ("2700", "30000.00", "SAFE-0003"),
                                    ("2700", "19999.81", "SAFE-0004"), ("3010", "-99996.81", None)]
    # 1,383,187 shares on 1 Jun: 1,000,000 is 72.2967…, 300,000 is 21.6890…, 52,631 is 3.8050…, 14,705 is
    # 1.0631…, 7,500 is 0.5422… and 8,351 is 0.6037… per cent.
    assert june["issued"] == "1383187"
    assert _owners(june) == [("P0001", "1000000", "72.30"), ("P0002", "300000", "21.69"), ("P0003", "52631", "3.81"),
                             ("P0004", "14705", "1.06"), ("P0005", "7500", "0.54"), ("P0006", "8351", "0.60")]
    # What 2700 keeps: S$1.37 from G4-044 and S$0.19 from SAFE-0004; SAFE-0002's S$3.00 is repaid by BT-0009.
    assert _lines(r, "BT-0009") == [("1010", "-3.00", None), ("2700", "3.00", "SAFE-0002")]
    assert sum((Decimal(v.get("2700", "0")) for v in r["trial_balance"].values()), Decimal(0)) == Decimal("-1.56")


# req: FR-EQ-18
# req: RND-SHARE
# req: QA-ENG-02
def test_two_caps_and_a_pre_money_safe_at_one_round():
    r = _run("safe-conversion-caps")
    assert r["rejected"] == []
    # C₀ = 1,000,000 at the end of 11 Mar, the day before the round; the issue of 13 Mar is after it. f = 0.05 + 0.05:
    # - SAFE-0001: 2,500,000 × 0.9 ÷ 1,000,000 = 2.25 < 3.00; 55,555 × 2.25 = 124,998.75 (55,556 = 125,001.00);
    # - SAFE-0002: 5,000,000 × 0.9 ÷ 1,000,000 = 4.50 > 3.00, so 3.00; 83,333 × 3 = 249,999.00;
    # - SAFE-0003, pre-money with 20% off: 2,500,000 ÷ 1,000,000 = 2.50 against 3.00 × 0.80 = 2.40; 52,083 × 2.40 =
    #   124,999.20 (G.3).
    assert _lines(r, "EQ-0003") == [("2700", "124998.75", "SAFE-0001"), ("2700", "249999.00", "SAFE-0002"),
                                    ("2700", "124999.20", "SAFE-0003"), ("3010", "-499996.95", None)]
    round_day, converted = r["cap_tables"]
    assert _owners(round_day) == [("P0001", "1000000", "90.91"), ("P0002", "100000", "9.09")]
    # 1,291,971 shares: 1,000,000 is 77.4011…, 101,000 is 7.8175…, 55,555 is 4.3000…, 83,333 is 6.4501… and 52,083 is
    # 4.0313… per cent.
    assert converted["issued"] == "1291971"
    assert _owners(converted) == [("P0001", "1000000", "77.40"), ("P0002", "101000", "7.82"), ("P0003", "55555", "4.30"),
                                  ("P0004", "83333", "6.45"), ("P0005", "52083", "4.03")]


def _company(*documents, accounts=()):
    return scenario_from_json({
        "schema": "fyniq-scenario@1", "name": "safes", "base_currency": "SGD", "currencies": {"SGD": 2},
        "as_of": "2027-12-31",
        "settings": {"accounts": {"gst": "2200"}, "money_accounts": {"1010": "SGD"}, "books_start": "2027-01-01"},
        "accounts": [{"code": "1010", "type": "asset", "group": False, "active": True},
                     {"code": "2040", "type": "liability", "group": False, "active": True,
                      "role": "share-application-monies"},
                     {"code": "2200", "type": "liability", "group": False, "active": True},
                     {"code": "3010", "type": "equity", "group": False, "active": True, "role": "share-capital"},
                     *accounts],
        "people": ["P0001"], "subscribers": ["P0002", "P0003"],
        "documents": list(documents), "reversals": []})


_HOLDING = {"code": "2700", "type": "liability", "group": False, "active": True, "role": "convertible-instruments"}


def _money(id_, day, amount, part, **names):
    return {"type": "bank_transaction", "id": id_, "version": 1, "date": day, "account": "1010", "currency": "SGD",
            "amount": amount, **names, "parts": [{"account": part, "amount": amount, "gst_code": "NA"}]}


def _round(founder_shares="1000000", price="3.00", safe=None):
    safe = safe or {"form": "post_money_safe", "valuation_cap": "2500000"}
    return [
        _money("BT-0001", "2027-01-05", "10000.00", "2040", person="P0001"),
        {"type": "share_issue_cash", "id": "EQ-0001", "version": 1, "date": "2027-01-05", "person": "P0001",
         "class": "3010", "shares": founder_shares, "price": "0.01"},
        {"type": "instrument", "id": "SAFE-0001", "version": 1, "date": "2027-02-01", "person": "P0003",
         "amount": "125000.00", "class": "3010", "price_decimals": 4, **safe},
        _money("BT-0002", "2027-02-03", "125000.00", "2700", instrument="SAFE-0001"),
        _money("BT-0003", "2027-02-26", "300000.00", "2040", person="P0002"),
        {"type": "share_issue_cash", "id": "EQ-0002", "version": 1, "date": "2027-03-12", "person": "P0002",
         "class": "3010", "shares": "100000", "price": price},
        {"type": "safe_conversion", "id": "EQ-0003", "version": 1, "date": "2027-03-12", "round": "EQ-0002"},
    ]


# req: FR-EQ-18
# req: RND-SHARE
def test_the_shapes_a_conversion_refuses():
    # No instruments account, so the SAFE's money cannot come in and nothing converts.
    r = Ledger(_company(*_round())).result()
    assert "BT-0002" in r["rejected"] and "EQ-0003" in r["rejected"]
    # SAFEs that take the whole company: 2,500,000 at a S$2,500,000 cap is f = 1.
    big = _round(safe={"form": "post_money_safe", "valuation_cap": "125000"})
    assert Ledger(_company(*big, accounts=[_HOLDING])).result()["rejected"] == ["EQ-0003"]
    # A price cut to nothing: a cap of 1 over 1,000,000 shares is 0.000001, cut to 0 decimals.
    tiny = _round(safe={"form": "pre_money_safe", "valuation_cap": "1"})
    tiny[2]["price_decimals"] = 0
    assert Ledger(_company(*tiny, accounts=[_HOLDING])).result()["rejected"] == ["EQ-0003"]
    # No whole share: S$125,000.00 at a pre-money cap price of 250,000 (a cap of 250,000,000,000 over 1,000,000 shares)
    # is below one share, and the round's 300,000.00 price is higher still.
    dear = _round(price="300000.00", safe={"form": "pre_money_safe", "valuation_cap": "250000000000"})
    dear[4] = _money("BT-0003", "2027-02-26", "30000000000.00", "2040", person="P0002")
    assert Ledger(_company(*dear, accounts=[_HOLDING])).result()["rejected"] == ["EQ-0003"]
    # The plain case converts: 2.375 again, with the round at 3.00.
    plain = Ledger(_company(*_round(), accounts=[_HOLDING])).result()
    assert plain["rejected"] == [] and _lines(plain, "EQ-0003") == [("2700", "124998.63", "SAFE-0001"),
                                                                      ("3010", "-124998.63", None)]


# req: FR-EQ-05
# req: FR-EQ-18
def test_a_founders_balance_becomes_their_safe():
    r = _run("safe-from-a-founder")
    # DS-0090 names a SAFE P0003 holds; DS-0091 would take SAFE-0001 to S$1,000.01 (600.00 held + 400.01); P0002 is owed
    # nothing and holds no SAFE; SAFE-0009 was never recorded; 3010 is not a director's account; and once 2500 is settled
    # in full, nothing is owed for DS-0095's S$0.01.
    assert r["rejected"] == ["DS-0090", "DS-0091", "DS-0092", "DS-0093", "DS-0094", "DS-0095"]
    assert _lines(r, "DS-0001") == [("2500", "600.00", None), ("2700", "-600.00", "SAFE-0001")]
    assert _lines(r, "DS-0002") == [("2500", "400.00", None), ("2700", "-400.00", "SAFE-0001")]
    # 2500 nets to nothing (-1,000.00 lent, +600.00 and +400.00 settled); 2700 holds the SAFE's S$1,000.00.
    tb = r["trial_balance"]
    assert sum((Decimal(m.get("2500", "0")) for m in tb.values()), Decimal(0)) == Decimal("0.00")
    assert sum((Decimal(m.get("2700", "0")) for m in tb.values()), Decimal(0)) == Decimal("-1000.00")
