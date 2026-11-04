"""The pending golden scenarios (test/differential/pending/), figures worked out by hand.

They wait for cmd/scenario-run to read the P1 shapes (pending/README.md); the oracle must match the
hand-worked figures now. Every figure comes from the comment beside it (QA-ENG-01). A case keeps its
hand-worked figures here once cmd/scenario-run reads its source and it moves to golden/ (bank, purchases, sales, claims, assets, founders, statements).
"""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

PENDING = Path(__file__).resolve().parents[2] / "test" / "differential" / "pending"
GOLDEN = PENDING.parent / "golden"
MOVED = {"bank", "purchases", "sales", "claims", "assets", "founders", "statements", "general-ledger"}  # the cases the engine reads now, in golden/


def _run(name: str) -> dict:
    return Ledger(load_scenario((GOLDEN if name in MOVED else PENDING) / f"{name}.json")).result()


def _lines(result: dict, document: str, kind: str = "post", posting_date: str | None = None) -> list[tuple]:
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == kind and posting_date in (None, e["posting_date"]):
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in e["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


# req: QA-ENG-02
def test_every_pending_file_is_listed_in_its_readme():
    listed = (PENDING / "README.md").read_text(encoding="utf-8")
    for path in PENDING.glob("*.json"):
        assert f"`{path.name}`" in listed, path.name


# req: QA-ENG-02
def test_pending_bank():
    r = _run("bank")
    assert r["rejected"] == ["BT-0005", "BT-0006"]  # a part on a money account; a spread on an asset
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("BT-0001", "post", "2026-03-05"),
        ("BT-0002", "post", "2026-03-10"),
        ("BT-0003", "post", "2026-03-12"),
        ("BT-0004", "post", "2026-03-20"),
        ("TR-0001", "post", "2026-04-02"),
        ("TR-0002", "post", "2026-04-03"),
        ("BT-0007", "post", "2026-04-07"),
        ("BT-0002", "release", "2026-03-10"),  # the posting month: the line's own date
        ("BT-0002", "release", "2026-04-01"),  # May's release (1 May) is after as_of
    ]
    # -109.00 on TX: 109 × 9 ÷ 109 = 9.00 of GST; 100.00 of expense.
    assert _lines(r, "BT-0001") == [("1010", "SGD", "-109.00", "-109.00"), ("2200", "SGD", "9.00", "9.00"),
                                    ("6010", "SGD", "100.00", "100.00")]
    # +327.00 on SR: GST 27.00; 300.00 deferred to 2400, released 100.00 a month (ALLOC-EVEN).
    assert _lines(r, "BT-0002") == [("1010", "SGD", "327.00", "327.00"), ("2200", "SGD", "-27.00", "-27.00"),
                                    ("2400", "SGD", "-300.00", "-300.00")]
    assert _lines(r, "BT-0002", "release", "2026-04-01") == [("2400", "SGD", "100.00", "100.00"),
                                                              ("4010", "SGD", "-100.00", "-100.00")]
    # USD -100.01 × 1.3335 = -133.36; parts -66.68 and -66.69 (-133.37); the cent to 7030.
    assert _lines(r, "BT-0003") == [("1021", "USD", "-100.01", "-133.36"), ("6010", "SGD", "66.68", "66.68"),
                                    ("6110", "SGD", "66.69", "66.69"), ("7030", "SGD", "-0.01", "-0.01")]
    # USD -109.00 × 1.35 = -147.15; GST 147.15 × 9 ÷ 109 = 12.15; expense 135.00.
    assert _lines(r, "BT-0004") == [("1021", "USD", "-109.00", "-147.15"), ("2200", "SGD", "12.15", "12.15"),
                                    ("6010", "SGD", "135.00", "135.00")]
    # USD 738.00 × 1.3485 = 995.193 -> 995.19; fee 4.50; 7030 -(995.19 - 1000.00 + 4.50) = 0.31.
    assert _lines(r, "TR-0002") == [("1010", "SGD", "-1000.00", "-1000.00"), ("1021", "USD", "738.00", "995.19"),
                                    ("6460", "SGD", "4.50", "4.50"), ("7030", "SGD", "0.31", "0.31")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "218.00",  # -109.00 + 327.00
            "1021": "-280.51",  # -133.36 - 147.15
            "2200": "-5.85",  # 9.00 - 27.00 + 12.15
            "2400": "-200.00",  # -300.00 + March's 100.00
            "4010": "-100.00",
            "6010": "301.68",  # 100.00 + 66.68 + 135.00
            "6110": "66.69",
            "7030": "-0.01",
        },
        "2026-04": {
            "1010": "-1550.00",  # -500.00 - 1,000.00 - 50.00
            "1011": "500.00",
            "1021": "995.19",
            "2400": "100.00",
            "2999": "50.00",
            "4010": "-100.00",
            "6460": "4.50",
            "7030": "0.31",
        },
    }


# req: QA-ENG-02
def test_pending_purchases():
    r = _run("purchases")
    assert r["rejected"] == ["BILL-0005", "PAY-0004"]  # a spread on an asset; 900.00 of 808.00 open
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("BILL-0001", "post", "2026-03-05"),
        ("BILL-0002", "post", "2026-03-06"),
        ("BILL-0003", "post", "2026-03-15"),
        ("BILL-0004", "post", "2026-03-16"),
        ("VC-0001", "post", "2026-03-17"),
        ("PAY-0001", "post", "2026-03-20"),
        ("PAY-0002", "post", "2026-03-25"),
        ("PAY-0003", "post", "2026-04-02"),
        ("BILL-0003", "release", "2026-03-15"),  # January to March, on the bill's date
        ("BILL-0003", "release", "2026-04-01"),
    ]
    # G4-023: Dr 6010 1,340.00, Dr 2200 121.50 as stated, Cr 2010 1,460.60, Cr 7030 0.90.
    assert _lines(r, "BILL-0001") == [("2010", "USD", "-1090.00", "-1460.60"), ("2200", "SGD", "121.50", "121.50"),
                                      ("6010", "USD", "1000.00", "1340.00"), ("7030", "SGD", "-0.90", "-0.90")]
    # ⌊1,234.56⌋ × 15% = 185.10 withheld; 2010 the rest, 1,049.46.
    assert _lines(r, "BILL-0004") == [("2010", "SGD", "-1049.46", "-1049.46"), ("2330", "SGD", "-185.10", "-185.10"),
                                      ("6010", "SGD", "1234.56", "1234.56")]
    # Rev 0.77: Dr 2010 1,460.60, Cr 1021 1,090.00 × 1.30 = 1,417.00, Cr 7030 43.60.
    assert _lines(r, "PAY-0001") == [("1021", "USD", "-1090.00", "-1417.00"), ("2010", "USD", "1090.00", "1460.60"),
                                     ("7030", "SGD", "-43.60", "-43.60")]
    # 1,089.50 in full: 1,090.00 settled, the 0.50 within S$1.00 a gain on 7030.
    assert _lines(r, "PAY-0002") == [("1010", "SGD", "-1089.50", "-1089.50"), ("2010", "SGD", "1090.00", "1090.00"),
                                     ("7030", "SGD", "-0.50", "-0.50")]
    assert _lines(r, "BILL-0003", "release", "2026-03-15") == [("1210", "SGD", "-300.00", "-300.00"),
                                                                 ("6010", "SGD", "300.00", "300.00")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "-1089.50",
            "1021": "-1417.00",
            "1210": "900.00",  # 1,200.00 less three months of 100.00
            "2010": "-2302.96",  # BILL-0003 1,308.00 + BILL-0004 1,049.46 - VC-0001 54.50, still open
            "2200": "315.00",  # 121.50 + 90.00 + 108.00 - 4.50
            "2330": "-185.10",
            "6010": "3824.56",  # 1,340.00 + 1,000.00 + 300.00 + 1,234.56 - 50.00
            "7030": "-45.00",  # -0.90 - 43.60 - 0.50
        },
        "2026-04": {"1010": "-520.00", "1210": "-100.00", "2010": "520.00", "6010": "100.00"},
    }


# req: QA-ENG-02
def test_pending_sales():
    r = _run("sales")
    # CN-0003's 0.01 takes INV-0001's credit notes to 32.87 of its 32.86 (rev 0.155); USD 11.00 of 10.95
    # open; a spread on an expense.
    assert r["rejected"] == ["CN-0003", "RC-0003", "INV-0006"]
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("DP-0001", "post", "2026-03-01"),
        ("INV-0005", "post", "2026-03-01"),
        ("INV-0001", "post", "2026-03-02"),
        ("INV-0002", "post", "2026-03-03"),
        ("INV-0003", "post", "2026-03-15"),
        ("RC-0001", "post", "2026-03-20"),
        ("INV-0004", "post", "2026-03-31"),
        ("CN-0001", "post", "2026-04-01"),
        ("RC-0002", "post", "2026-04-02"),
        ("CN-0002", "post", "2026-04-03"),
        ("RF-0001", "post", "2026-04-10"),
        ("WO-0001", "post", "2026-04-30"),
        ("INV-0003", "release", "2026-03-15"),
        ("INV-0003", "release", "2026-04-01"),
    ]
    # USD 10.05 at 1.5: 15.08 net, 1.36 GST on it, USD 10.95 owed (16.43), 7030 0.01.
    assert _lines(r, "INV-0002") == [("1100", "USD", "10.95", "16.43"), ("2200", "SGD", "-1.36", "-1.36"),
                                     ("4010", "USD", "-10.05", "-15.08"), ("7030", "SGD", "0.01", "0.01")]
    # G4-038: USD 9,710.00 at 1.32 = 12,817.20 against 13,108.50 at 1.35: 291.30 realised loss.
    assert _lines(r, "RC-0001") == [("1021", "USD", "9710.00", "12817.20"), ("1100", "USD", "-9710.00", "-13108.50"),
                                    ("7030", "SGD", "291.30", "291.30")]
    # 100.00 of the 1,090.00 deposit: GST 90 × 100 ÷ 1,090 = 8.26 back off 2200; 2410 91.74; nothing left owed.
    assert _lines(r, "INV-0004") == [("2200", "SGD", "8.26", "8.26"), ("2410", "SGD", "91.74", "91.74"),
                                     ("4020", "SGD", "-100.00", "-100.00")]
    # 545.00 of the 990.00 left: its base 545.00 and GST 81.74 × 545 ÷ 990 = 44.998... -> 45.00.
    assert _lines(r, "RF-0001") == [("1010", "SGD", "-545.00", "-545.00"), ("2200", "SGD", "45.00", "45.00"),
                                    ("2410", "SGD", "500.00", "500.00")]
    # INV-0001's 32.86 (30.15 + 2.71 GST per subtotal) less CN-0001's 10.95 leaves 21.91 to credit:
    # 20.10 + 1.81 (1.809) uses all of it.
    assert _lines(r, "CN-0002") == [("1100", "SGD", "-21.91", "-21.91"), ("2200", "SGD", "1.81", "1.81"),
                                    ("4010", "SGD", "20.10", "20.10")]
    # The USD 290.00 left, carried at 13,500.00 - 13,108.50 = 391.50, written off to 7030.
    assert _lines(r, "WO-0001") == [("1100", "USD", "-290.00", "-391.50"), ("7030", "SGD", "391.50", "391.50")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "1090.00",
            "1021": "12817.20",
            "1100": "1748.79",  # 32.86 + 16.43 + 1,308.00 + 13,500.00 - 13,108.50
            "2200": "-193.81",  # -90.00 - 2.71 - 1.36 - 108.00 + 8.26
            "2400": "-1100.00",  # -1,200.00 + March's 100.00
            "2410": "-908.26",  # -1,000.00 + 91.74
            "4010": "-13645.23",  # -30.15 - 15.08 - 13,500.00 - March's 100.00
            "4020": "-100.00",
            "7030": "291.31",  # 0.01 + 291.30
        },
        "2026-04": {
            "1010": "-512.64",  # 32.36 - 545.00
            "1100": "-457.22",  # -10.95 - 32.86 - 21.91 - 391.50
            "2200": "47.71",  # 0.90 + 1.81 + 45.00
            "2400": "100.00",
            "2410": "500.00",
            "4010": "-69.85",  # 10.05 + 20.10 - April's 100.00
            "7030": "392.00",  # 0.50 + 391.50
        },
    }


# req: QA-ENG-02
# req: FR-EQ-02
def test_pending_claims():
    r = _run("claims")
    # EC-0004 is paid; a trip in USD; a loan settling an advance; a loan to no director; a loan reimbursed.
    assert r["rejected"] == ["RE-0003", "EC-0005", "EC-0007", "EC-0008", "RE-0004"]
    assert [e["document"] for e in r["entries"]] == ["ADV-0001", "EC-0001", "EC-0002", "EC-0003", "EC-0004", "RE-0001",
                                                     "RE-0002", "ADV-0002", "ADV-0002R", "EC-0006"]
    # 100.00 + 9.00 GST and 12.35 km -> 12.4 × 0.60 = 7.44: 116.44 taken from the 500.00 advance.
    assert _lines(r, "EC-0001") == [("1250", "SGD", "-116.44", "-116.44"), ("2200", "SGD", "9.00", "9.00"),
                                    ("6010", "SGD", "100.00", "100.00"), ("6110", "SGD", "7.44", "7.44")]
    # 450.00: the 383.56 left on the advance, 66.44 owed on 2150.
    assert _lines(r, "EC-0002") == [("1250", "SGD", "-383.56", "-383.56"), ("2150", "SGD", "-66.44", "-66.44"),
                                    ("6010", "SGD", "450.00", "450.00")]
    # 7.25 km -> 7.3 × 0.25 = 1.825 -> 1.83, and 100 km × 0.60 = 60.00.
    assert _lines(r, "EC-0004") == [("2150", "SGD", "-61.83", "-61.83"), ("6110", "SGD", "61.83", "61.83")]
    # USD 50.00 at 1.35 = 67.50 to 6010; what the company owes is fixed in SGD (FR-AP-17, SFRS(I) 1-21): 2150 -67.50.
    assert _lines(r, "EC-0003") == [("2150", "SGD", "-67.50", "-67.50"), ("6010", "USD", "50.00", "67.50")]
    # Reimbursed in SGD with the S$67.50 owed and no rate: no FX, nothing to 7030.
    assert _lines(r, "RE-0001") == [("1010", "SGD", "-67.50", "-67.50"), ("2150", "SGD", "67.50", "67.50")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "-695.77",  # -500.00 - 67.50 - 128.27; 1250 and 2150 are back at nothing
            "2200": "9.00",
            "6010": "617.50",  # 100.00 + 450.00 + 67.50
            "6110": "69.27",  # 7.44 + 61.83
        },
        # 300.00 advanced and refunded; P0001 paid 200.00 + 18.00 GST and keeps it as a loan (FR-EQ-02, B.7).
        "2026-04": {"2200": "18.00", "2500": "-218.00", "6010": "200.00"},
    }
    # The 2500 line names the person; what the company owes them stays there.
    entry = next(e for e in r["entries"] if e["document"] == "EC-0006")
    assert [(ln["account"], ln.get("party")) for ln in entry["lines"]] == [("2200", None), ("2500", "P0001"),
                                                                          ("6010", None)]
    assert r["people"] == {"P0001": {"2500": "-218.00"}}


# req: QA-ENG-02
# req: FR-EQ-03
# req: FR-EQ-04
# req: FR-EQ-05
# req: FR-EQ-07
def test_pending_founders():
    r = _run("founders")
    # On the books start; a person who is not a director; on a director's account; posted before the books start; GST
    # claimed when the receipt shows none.
    # Then a fee voted to someone who is not a director, and one posted to the director's account itself.
    # A waiver of an account that is not a director's, and one crediting a money account, are refused too; so are a
    # repayment from an account that is not money, and one of an account that is not a director's. Then a conversion
    # to 3300, which is not share capital, and one of 0.50, too small for one share at 1.20.
    assert r["rejected"] == ["PIC-0004", "PIC-0005", "PIC-0006", "PIC-0007", "PIC-0008", "DF-0002", "DF-0003", "DS-0002", "DS-0003", "DR-0003",
                             "DR-0004", "SI-0005", "SI-0006"]
    # 1,315.00 with no GST shown: all to 6910, owed to P0001 on 2500.
    assert _lines(r, "PIC-0001") == [("2500", "SGD", "-1315.00", "-1315.00"), ("6910", "SGD", "1315.00", "1315.00")]
    # 3,815.00 with 315.00 GST claimed while registered from the books start: 3,500.00 to 1600, 315.00 to 2200.
    assert _lines(r, "PIC-0002") == [("1600", "SGD", "3500.00", "3500.00"), ("2200", "SGD", "315.00", "315.00"),
                                     ("2500", "SGD", "-3815.00", "-3815.00")]
    # 763.00 with 63.00 GST not claimed, decided on 3 Feb: the GST stays in the cost.
    assert _lines(r, "PIC-0003") == [("2500", "SGD", "-763.00", "-763.00"), ("6400", "SGD", "763.00", "763.00")]
    # FR-EQ-04: P0002's 6,000.00 fee voted on 31 Mar: Dr 6050, Cr 2120 owed to them.
    assert _lines(r, "DF-0001") == [("2120", "SGD", "-6000.00", "-6000.00"), ("6050", "SGD", "6000.00", "6000.00")]
    assert r["trial_balance"] == {
        "2026-01": {"1600": "3500.00", "2200": "315.00", "2500": "-5130.00", "6910": "1315.00"},  # 1,315.00 + 3,815.00
        "2026-02": {"2500": "-763.00", "6400": "763.00"},
        # The fee, the waiver, and 500.00 + 2,815.00 repaid from the bank.
        "2026-03": {"1010": "-3315.00", "2120": "-4000.00", "2500": "2315.00", "3300": "-1000.00", "6050": "6000.00"},
        # The conversions: 2500 99.60 + 1,000.00 + 299.60; 2120 1,498.90; 3010 99.60 + 750.00 + 321.00 + 1,498.90.
        "2026-04": {"2120": "1498.90", "2500": "1399.20", "3010": "-2669.50", "4900": "-250.00", "7060": "21.40"},
    }
    # FR-EQ-07: P0001 waives 1,000.00 of their 2,078.00 as a shareholder: Dr 2500 naming them, Cr 3300.
    assert _lines(r, "DS-0001") == [("2500", "SGD", "1000.00", "1000.00"), ("3300", "SGD", "-1000.00", "-1000.00")]
    # FR-EQ-05: P0002 repaid 2,000.00 of the fee and 815.00 of the cost in one payment: Dr 2120 and 2500 naming them, Cr 1010.
    assert _lines(r, "DR-0002") == [("1010", "SGD", "-2815.00", "-2815.00"), ("2120", "SGD", "2000.00", "2000.00"),
                                    ("2500", "SGD", "815.00", "815.00")]
    # FR-EQ-06, RND-SHARE: 100.00 at 1.20 is 83.33 shares, so 83: 83 × 1.20 = 99.60 converted (84 would be 100.80),
    # Dr 2500 naming P0001, Cr 3010; the 0.40 left stays owed.
    assert _lines(r, "SI-0001") == [("2500", "SGD", "99.60", "99.60"), ("3010", "SGD", "-99.60", "-99.60")]
    # 1,000.00 at 2.00 is 500 shares, 1,000.00 converted; on the fair-value basis at 1.50 they are worth 750.00, which
    # 3010 takes: the debt extinguished above the shares' value is a gain of 250.00 to 4900.
    assert _lines(r, "SI-0002") == [("2500", "SGD", "1000.00", "1000.00"), ("3010", "SGD", "-750.00", "-750.00"),
                                    ("4900", "SGD", "-250.00", "-250.00")]
    # 300.00 at 0.70 is 428.57 shares, so 428: 428 × 0.70 = 299.60 converted (429 would be 300.30), 0.40 left owed; at
    # a fair value of 0.75 they are worth 428 × 0.75 = 321.00: a loss of 321.00 - 299.60 = 21.40 to 7060.
    assert _lines(r, "SI-0003") == [("2500", "SGD", "299.60", "299.60"), ("3010", "SGD", "-321.00", "-321.00"),
                                    ("7060", "SGD", "21.40", "21.40")]
    # From P0002's fees on 2120: 1,500.00 at 1.30 is 1,153.85 shares, so 1,153: 1,153 × 1.30 = 1,498.90 converted
    # (1,154 would be 1,500.20), 1.10 left owed.
    assert _lines(r, "SI-0004") == [("2120", "SGD", "1498.90", "1498.90"), ("3010", "SGD", "-1498.90", "-1498.90")]
    entry = next(e for e in r["entries"] if e["document"] == "SI-0004")
    assert [(ln["account"], ln.get("party")) for ln in entry["lines"]] == [("2120", "P0002"), ("3010", None)]
    # P0001: 1,315.00 + 763.00 - 1,000.00 - 500.00 = 578.00 owed, less 99.60 and 299.60 converted: 178.80.
    # P0002: the fee's 4,000.00 left on 2120 less 1,498.90 converted; the cost's 3,000.00 on 2500 less 1,000.00.
    assert r["people"] == {"P0001": {"2500": "-178.80"}, "P0002": {"2120": "-2501.10", "2500": "-2000.00"}}


# req: QA-ENG-02
def test_pending_assets():
    r = _run("assets")
    assert r["rejected"] == ["AD-0003", "FA-0005"]  # FA-0001 is gone; 6010 is not an asset account
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("AD-0001", "post", "2026-04-10"),
        ("AD-0002", "post", "2026-05-05"),
        ("FA-0001", "depreciation", "2026-01-31"),
        ("FA-0002", "depreciation", "2026-01-31"),  # November and December 2025 are in the opening balances
        ("FA-0004", "depreciation", "2026-01-31"),
        ("FA-0001", "depreciation", "2026-02-28"),
        ("FA-0002", "depreciation", "2026-02-28"),
        ("FA-0004", "depreciation", "2026-02-28"),
        ("FA-0001", "depreciation", "2026-03-31"),
        ("FA-0002", "depreciation", "2026-03-31"),
        ("FA-0003", "depreciation", "2026-03-31"),  # from 1 March
        ("FA-0004", "depreciation", "2026-03-31"),
        ("FA-0002", "depreciation", "2026-04-30"),  # FA-0001 disposed of in April
        ("FA-0003", "depreciation", "2026-04-30"),
        ("FA-0004", "depreciation", "2026-04-30"),
        ("FA-0002", "depreciation", "2026-05-31"),  # FA-0004 disposed of in May
        ("FA-0003", "depreciation", "2026-05-31"),
    ]
    # 3,270.00 on SR: GST 270.00; accumulated January to March 300.00; carrying 3,300.00 against 3,000.00 net.
    assert _lines(r, "AD-0001") == [("1010", "SGD", "3270.00", "3270.00"), ("1500", "SGD", "-3600.00", "-3600.00"),
                                    ("1590", "SGD", "300.00", "300.00"), ("2200", "SGD", "-270.00", "-270.00"),
                                    ("7050", "SGD", "300.00", "300.00")]
    # (2,000.00 - 200.00) ÷ 18 = 100.00 a month, January to April 400.00; carrying 1,600.00 against 1,500.00.
    assert _lines(r, "AD-0002") == [("1160", "SGD", "1500.00", "1500.00"), ("1510", "SGD", "-2000.00", "-2000.00"),
                                    ("1590", "SGD", "400.00", "400.00"), ("7050", "SGD", "100.00", "100.00")]
    assert r["trial_balance"] == {
        "2026-01": {"1590": "-300.00", "6610": "300.00"},
        "2026-02": {"1590": "-300.00", "6610": "300.00"},
        "2026-03": {"1590": "-633.33", "6610": "633.33"},  # 100.00 × 3 + 333.33
        "2026-04": {"1010": "3270.00", "1500": "-3600.00", "1590": "-233.34", "2200": "-270.00",
                    "6610": "533.34", "7050": "300.00"},  # 1590: +300.00 cleared, -(100.00 + 333.34 + 100.00)
        "2026-05": {"1160": "1500.00", "1510": "-2000.00", "1590": "-33.33", "6610": "433.33",
                    "7050": "100.00"},  # 1590: +400.00 cleared, -(100.00 + 333.33)
    }


def _section(total: str, groups: dict[str, str] | None = None) -> dict:
    return {"total": total, "groups": groups or {}}


_NO_PROFIT = {
    "revenue": _section("0.00"), "cost_of_sales": _section("0.00"), "gross_profit": "0.00",
    "operating_expenses": {"total": "0.00", "groups": {}, "functions": {}}, "ebitda": "0.00",
    "depreciation_and_amortisation": _section("0.00"), "operating_profit": "0.00", "other_income": _section("0.00"),
    "finance_costs_and_other_expenses": _section("0.00"), "profit_before_tax": "0.00", "income_tax": "0.00",
    "net_profit": "0.00",
}  # no percentages: a revenue of zero has none (RND-PERCENT)
_NO_CASH_FLOW = {"net_profit": "0.00", "depreciation_and_amortisation": "0.00", "investing_and_financing_results": "0.00",
                 "working_capital": {}, "other_accounts": "0.00", "total": "0.00"}


# req: FR-RPT-01, INV-10, INV-11, RND-PERCENT, QA-ENG-02
def test_pending_statements():
    r = _run("statements")
    assert r["rejected"] == []
    assert len(r["entries"]) == 23  # 22 journals and JNL-0010's reversal on 5 February
    s = r["statements"]
    assert list(s) == ["2026-01", "2026-02", "2026-03", "2026-04"]  # March has no entry and is still stated

    jan = s["2026-01"]
    assert jan["profit_and_loss"] == {
        "revenue": _section("1200.00", {"Subscriptions": "1200.00"}),
        "cost_of_sales": _section("-199.80", {"Hosting": "-199.80"}),
        "gross_profit": "1000.20",  # 1,200.00 - 199.80
        "operating_expenses": {
            "total": "-3399.99",  # salaries 2,000.00, marketing 500.00, rent 800.00 (paid by a director), software 99.99
            "groups": {"6300": "-99.99", "Marketing": "-500.00", "Rent": "-800.00", "Salaries": "-2000.00"},
            "functions": {"R&D": "-2000.00", "S&M": "-500.00", "G&A": "-800.00", "Other": "-99.99"},  # 6300 has none
        },
        "ebitda": "-2399.79",  # 1,000.20 - 3,399.99; depreciation is below it
        "depreciation_and_amortisation": _section("-83.33", {"Depreciation & amortisation": "-83.33"}),
        "operating_profit": "-2483.12",
        "other_income": _section("0.00"),
        "finance_costs_and_other_expenses": _section("0.00"),
        "profit_before_tax": "-2483.12",
        "income_tax": "0.00",
        "net_profit": "-2483.12",
        "percent_of_revenue": {  # of 1,200.00, to 1 decimal, half away from zero
            "revenue": "100.0",
            "cost_of_sales": "-16.7",  # -16.65 exactly: away from zero
            "gross_profit": "83.4",  # 83.35 exactly
            "operating_expenses": "-283.3",  # -283.3325
            "ebitda": "-200.0",  # -199.9825
            "depreciation_and_amortisation": "-6.9",  # -6.94416…
            "operating_profit": "-206.9",  # -206.92666…
            "other_income": "0.0",
            "finance_costs_and_other_expenses": "0.0",
            "profit_before_tax": "-206.9",
            "income_tax": "0.0",
            "net_profit": "-206.9",
        },
    }
    # Bank 10,000.00 - 3,270.00 + 1,000.00 - 199.80 - 2,000.00 - 99.99 = 5,430.21; receivable 1,308.00 - 1,000.00;
    # the laptop 3,000.00 less 83.33; GST paid 270.00 less 108.00 charged is a debit under current liabilities;
    # the director's loan 10,000.00 + 800.00; retained earnings hold the month's loss.
    assert jan["balance_sheet"] == {
        "current_assets": _section("5738.21", {"Cash and bank": "5430.21", "Trade receivables": "308.00"}),
        "non_current_assets": _section("2916.67", {"Property, plant & equipment": "2916.67"}),
        "total_assets": "8654.88",
        "current_liabilities": _section("338.00", {"GST": "-162.00", "Trade payables": "500.00"}),
        "borrowings": _section("10800.00", {"Loan from director": "10800.00"}),
        "total_liabilities": "11138.00",
        "equity": _section("-2483.12", {"Retained earnings": "-2483.12"}),
        "total_liabilities_and_equity": "8654.88",
    }
    # JNL-0001 is financing (+10,000.00) and JNL-0002 investing (-3,270.00, its GST inside); JNL-0008 moved no money.
    # Operating: -2,483.12 + 83.33 depreciation + 800.00 (the rent in a financing entry) - 308.00 receivables
    # + 108.00 GST charged + 500.00 payables = -1,299.79, the operating entries' cash: 1,000.00 - 199.80 - 2,000.00 - 99.99.
    assert jan["cash_flow"] == {
        "operating": {"net_profit": "-2483.12", "depreciation_and_amortisation": "83.33",
                      "investing_and_financing_results": "800.00",
                      "working_capital": {"Receivables": "-308.00", "Payables & accruals": "500.00",
                                          "Tax liabilities": "108.00"},
                      "other_accounts": "0.00", "total": "-1299.79"},
        "investing": "-3270.00",
        "financing": "10000.00",
        "net_change": "5430.21",
        "opening_cash": "0.00",
        "closing_cash": "5430.21",
        "non_cash": [{"document": "JNL-0008", "kind": "post", "posting_date": "2026-01-28", "section": "financing"}],
    }
    assert jan["equity_movement"] == {"opening": "0.00", "profit": "-2483.12", "share_capital": "0.00",
                                      "share_capital_non_cash": "0.00", "other": "0.00", "closing": "-2483.12"}
    assert jan["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    feb = s["2026-02"]
    assert feb["profit_and_loss"] == {
        "revenue": _section("2000.00", {"Services": "2000.00"}),
        "cost_of_sales": _section("0.00"),
        "gross_profit": "2000.00",
        "operating_expenses": {"total": "99.99", "groups": {"6300": "99.99"}, "functions": {"Other": "99.99"}},  # reversed
        "ebitda": "2099.99",
        "depreciation_and_amortisation": _section("-40.00", {"Depreciation & amortisation": "-40.00"}),  # 6620, same group
        "operating_profit": "2059.99",
        "other_income": _section("1533.33", {"Gains & losses on disposal": "33.33", "Grants & subsidies": "1500.00"}),
        "finance_costs_and_other_expenses": _section("-50.00", {"Finance costs": "-50.00"}),
        "profit_before_tax": "3543.32",  # 2,059.99 + 1,533.33 - 50.00
        "income_tax": "-100.00",
        "net_profit": "3443.32",
        "percent_of_revenue": {  # of 2,000.00
            "revenue": "100.0", "cost_of_sales": "0.0", "gross_profit": "100.0",
            "operating_expenses": "5.0",  # 4.9995
            "ebitda": "105.0",  # 104.9995
            "depreciation_and_amortisation": "-2.0",
            "operating_profit": "103.0",  # 102.9995
            "other_income": "76.7",  # 76.6665
            "finance_costs_and_other_expenses": "-2.5",
            "profit_before_tax": "177.2",  # 177.166
            "income_tax": "-5.0",
            "net_profit": "172.2",  # 172.166
        },
    }
    # Bank 5,430.21 - 500.00 - 1,200.00 + 600.00 + 1,500.00 + 2,950.00 - 1,050.00 - 300.00 + 99.99 = 7,530.20, and
    # 2,180.00 in 1020; the laptop gone and 40.00 of amortisation left on 1590; GST 162.00 debit - 180.00 = 18.00 owed;
    # the loan 10,800.00 - 1,000.00 - 2,000.00; retained earnings -2,483.12 + 3,443.32 - 300.00 of dividend.
    assert feb["balance_sheet"] == {
        "current_assets": _section("11218.20", {"Cash and bank": "9710.20", "Prepayments": "1200.00",
                                                "Trade receivables": "308.00"}),
        "non_current_assets": _section("-40.00", {"Property, plant & equipment": "-40.00"}),
        "total_assets": "11178.20",
        "current_liabilities": _section("718.00", {"Deferred revenue": "600.00", "GST": "18.00",
                                                   "Income tax payable": "100.00"}),  # trade payables back at nothing
        "borrowings": _section("7800.00", {"Loan from director": "7800.00"}),
        "total_liabilities": "8518.00",
        "equity": _section("2660.20", {"Capital contribution": "2000.00", "Retained earnings": "660.20"}),
        "total_liabilities_and_equity": "11178.20",
    }
    # The sale (+2,950.00) is investing with its 33.33 gain taken out of operating; the repayment with interest
    # (-1,050.00) and the dividend (-300.00) are financing, the interest put back; the conversion moved no money.
    # Operating: 3,443.32 + 40.00 + 16.67 - 1,200.00 prepaid - 500.00 paid to the supplier + 280.00 tax (180.00 GST,
    # 100.00 income tax) + 600.00 paid ahead = 2,679.99 = -500.00 - 1,200.00 + 2,180.00 + 600.00 + 1,500.00 + 99.99.
    assert feb["cash_flow"] == {
        "operating": {"net_profit": "3443.32", "depreciation_and_amortisation": "40.00",
                      "investing_and_financing_results": "16.67",
                      "working_capital": {"Other current assets": "-1200.00", "Payables & accruals": "-500.00",
                                          "Tax liabilities": "280.00", "Deferred revenue": "600.00"},
                      "other_accounts": "0.00", "total": "2679.99"},
        "investing": "2950.00",
        "financing": "-1350.00",
        "net_change": "4279.99",
        "opening_cash": "5430.21",
        "closing_cash": "9710.20",
        "non_cash": [{"document": "JNL-0018", "kind": "post", "posting_date": "2026-02-26", "section": "financing"}],
    }
    # The capital contribution 2,000.00 less the dividend 300.00.
    assert feb["equity_movement"] == {"opening": "-2483.12", "profit": "3443.32", "share_capital": "0.00",
                                      "share_capital_non_cash": "0.00", "other": "1700.00", "closing": "2660.20"}
    assert feb["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    mar = s["2026-03"]
    assert mar["profit_and_loss"] == _NO_PROFIT
    assert mar["balance_sheet"] == feb["balance_sheet"]
    assert mar["cash_flow"] == {"operating": _NO_CASH_FLOW, "investing": "0.00", "financing": "0.00",
                                "net_change": "0.00", "opening_cash": "9710.20", "closing_cash": "9710.20", "non_cash": []}
    assert mar["equity_movement"] == {"opening": "2660.20", "profit": "0.00", "share_capital": "0.00",
                                      "share_capital_non_cash": "0.00", "other": "0.00", "closing": "2660.20"}
    assert mar["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    # 50.00 paid to 1990, which has no cash-flow line: it sits in no section, so the balance sheet is 50.00 short
    # and `balances` fails, naming it. The cash flow keeps its movement in operating's other accounts, -(+50.00), so
    # operating is the 50.00 that left the bank and `ties` holds (D3, rev 0.159).
    apr = s["2026-04"]
    assert apr["profit_and_loss"] == _NO_PROFIT
    assert apr["balance_sheet"]["current_assets"] == _section(
        "11168.20", {"Cash and bank": "9660.20", "Prepayments": "1200.00", "Trade receivables": "308.00"})
    assert (apr["balance_sheet"]["total_assets"], apr["balance_sheet"]["total_liabilities_and_equity"]) == (
        "11128.20", "11178.20")
    assert apr["cash_flow"] == {"operating": {**_NO_CASH_FLOW, "other_accounts": "-50.00", "total": "-50.00"},
                                "investing": "0.00", "financing": "0.00", "net_change": "-50.00",
                                "opening_cash": "9710.20", "closing_cash": "9660.20", "non_cash": []}
    assert apr["checks"] == {"balances": False, "ties": True, "equity": True, "unplaced": ["1990"]}


# req: FR-KPI-02, FR-KPI-03, RND-KPI, QA-ENG-02
def test_pending_kpis():
    k = _run("statements")["kpis"]
    assert list(k) == ["2026-01", "2026-02", "2026-03", "2026-04"]
    # January: 4010 is the subscription stream; 10 paying customers, all new; 3 people.
    assert k["2026-01"] == {
        "booked_mrr": "1200.00", "arr": "14400.00", "contracted_mrr": None,
        "revenue_growth": None,  # the books' first month: no month before it
        "gross_margin": "83.4",  # 1,000.20 ÷ 1,200.00 = 83.35%
        "spend_by_function": {"R&D": "2000.00", "S&M": "500.00", "G&A": "883.33"},  # rent 800.00 + depreciation 83.33
        "net_burn": "4569.79",  # -(-1,299.79 operating - 3,270.00 investing)
        "runway": "1.2",  # 5,430.21 ÷ 4,569.79 (January alone) = 1.188
        "arpa": "120.00",  # 1,200.00 ÷ 10
        "churn": None,  # 10 - 10 + 0 = no customers at the start of the month
        "cac": "50.00",  # 500.00 ÷ 10
        "ltv": None, "ltv_to_cac": None,  # no churn
        "cac_payback": "0.5",  # 50.00 ÷ (120.00 × 0.8335) = 0.4999
        "burn_multiple": None,  # the books' first month: no month before it, so no new ARR
        "current_ratio": "17.0", "quick_ratio": "17.0",  # 5,738.21 ÷ 338.00; cash and receivables are all of it
        "working_capital": "5400.21",
        "debt_to_equity": None,  # equity -2,483.12
        "dso": "7.7",  # 308.00 ÷ (1,200.00 ÷ 30)
        "dpo": "4.2",  # 500.00 ÷ ((199.80 + 3,399.99) ÷ 30) = 4.167
        "ebitda_margin": "-200.0", "net_margin": "-206.9",
        "headcount": "3", "revenue_per_employee": "4800.00",  # 1,200.00 × 12 ÷ 3
    }
    # February: services only, so booked MRR is 0.00 (the stream exists); the month made cash.
    assert k["2026-02"] == {
        "booked_mrr": "0.00", "arr": "0.00", "contracted_mrr": None,
        "revenue_growth": "66.7",  # (2,000.00 - 1,200.00) ÷ 1,200.00
        "gross_margin": "100.0",
        "spend_by_function": {"R&D": "0.00", "S&M": "0.00", "G&A": "0.00"},  # 6300 and 6620 have no function
        "net_burn": "-5629.99",  # -(2,679.99 + 2,950.00)
        "runway": "not-burning",  # (4,569.79 - 5,629.99) ÷ 2 ≤ 0
        "arpa": "0.00",
        "churn": "20.0",  # 2 ÷ (12 - 4 + 2)
        "cac": "0.00",  # no S&M spend
        "ltv": "0.00",  # 0.00 × 100% ÷ 20%
        "ltv_to_cac": None,  # a CAC of 0
        "cac_payback": None,  # ARPA × margin is 0
        "burn_multiple": None,  # not burning
        "current_ratio": "15.6",  # 11,218.20 ÷ 718.00 = 15.624
        "quick_ratio": "14.0",  # (9,710.20 + 308.00) ÷ 718.00 = 13.953
        "working_capital": "10500.20",
        "debt_to_equity": "3.2",  # 8,518.00 ÷ 2,660.20
        "dso": "5.8",  # 308.00 ÷ ((1,200.00 + 2,000.00) ÷ 2 ÷ 30) = 5.775
        "dpo": "0.0",  # nothing owed to suppliers
        "ebitda_margin": "105.0", "net_margin": "172.2",
        "headcount": "4", "revenue_per_employee": "6000.00",
    }
    # March: no entry and no customer counts; headcount entered.
    mar = k["2026-03"]
    assert (mar["booked_mrr"], mar["revenue_growth"], mar["gross_margin"], mar["net_margin"]) == ("0.00", "-100.0", None, None)
    assert (mar["net_burn"], mar["runway"]) == ("0.00", "not-burning")
    assert [mar[key] for key in ("arpa", "churn", "cac", "ltv", "ltv_to_cac", "cac_payback")] == [None] * 6  # not entered
    assert mar["dso"] == "8.7"  # 308.00 ÷ (3,200.00 ÷ 3 ÷ 30) = 8.6625
    assert (mar["headcount"], mar["revenue_per_employee"]) == ("4", "0.00")
    # April: counts without headcount; the 50.00 to an unplaced account is in operating, so April burns 50.00; the
    # February-to-April average, (-5,629.99 + 0.00 + 50.00) ÷ 3, is still not above zero.
    apr = k["2026-04"]
    assert (apr["revenue_growth"], apr["net_burn"], apr["runway"]) == (None, "50.00", "not-burning")
    assert apr["burn_multiple"] is None  # ARR 0.00, not grown from March's 0.00
    assert (apr["arpa"], apr["churn"], apr["cac"], apr["ltv"]) == ("0.00", "0.0", None, None)  # no new customers
    assert (apr["current_ratio"], apr["quick_ratio"], apr["working_capital"]) == ("15.6", "13.9", "10450.20")
    assert apr["dso"] == "13.9"  # February to April: 308.00 ÷ (2,000.00 ÷ 3 ÷ 30) = 13.86
    # February to April's costs: -99.99 (the software reversed) + 0.00 + 0.00, not above zero: DPO undefined.
    assert apr["dpo"] is None
    assert (apr["headcount"], apr["revenue_per_employee"]) == (None, None)


# req: QA-ENG-02, FR-GL-02, FR-GL-06, FR-GL-12, INV-13
def test_pending_general_ledger():
    # README, "Opening balances, automatic reversals and edits". as_of is 15 April 2026; the books start on 1 January.
    r = _run("general-ledger")
    assert r["rejected"] == [
        "JNL-0004",  # reverse_on on its own date (JOURNAL-11)
        "JNL-0005",  # a manual journal to share capital (INV-13)
        "OB-0001/edit",  # the bank in USD (LEDGER-17)
        "OB-0001/edit",  # version 3 while the balances are at 1
        "OB-0001/edit",  # does not net to zero (500.00 over)
        "JNL-0002/edit",  # version 3 while the journal is at 1
        "JNL-0006/reversal",  # 5 February: before the edit's replacement (10 February), the entry it would reverse
        "JNL-0006/edit",  # after its void
        "JNL-0004/edit",  # it never posted
        "JNL-0099/edit",  # no such journal
        "JNL-0001/edit",  # its new reverse_on is its own date (JOURNAL-11)
        "JNL-0001/edit",  # its new content reaches share capital (INV-13)
    ]
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("OB-0001", "post", "2026-01-01"),  # the documents in order
        ("JNL-0001", "post", "2026-01-31"),
        ("JNL-0002", "post", "2026-02-28"),
        ("JNL-0003", "post", "2026-03-31"),
        ("JNL-0006", "post", "2026-01-10"),
        ("JNL-0007", "post", "2026-04-10"),
        ("JNL-0008", "post", "2026-04-10"),
        ("JNL-0009", "post", "2026-03-31"),
        ("OB-0001", "reversal", "2026-01-01"),  # the corrections in order; an edit's two entries at the books' start
        ("OB-0001", "replacement", "2026-01-01"),
        ("JNL-0002", "reversal", "2026-02-28"),  # the replaced entry on its own date
        ("JNL-0002", "replacement", "2026-02-28"),
        ("JNL-0003", "reversal", "2026-03-31"),  # its void
        ("JNL-0006", "reversal", "2026-01-10"),  # January made right ...
        ("JNL-0006", "replacement", "2026-02-10"),  # ... and the new version in February
        ("JNL-0006", "reversal", "2026-02-12"),  # the void reverses the replacement
        ("JNL-0007", "reversal", "2026-04-10"),
        ("JNL-0007", "replacement", "2026-04-10"),  # without reverse_on: nothing reverses by itself
        ("JNL-0001", "auto_reversal", "2026-02-01"),  # the scheduler, by date: version 1 (both edits refused)
        ("JNL-0002", "auto_reversal", "2026-03-01"),  # version 2's entry, on version 2's reverse_on
        ("JNL-0009", "auto_reversal", "2026-04-01"),  # JNL-0003's was voided; JNL-0008's (1 May) is after as_of
    ]
    opening = [("1010", "SGD", "25000.00", "25000.00"), ("1500", "SGD", "3600.00", "3600.00"),
               ("1590", "SGD", "-600.00", "-600.00"), ("2500", "SGD", "-8000.00", "-8000.00"),
               ("3010", "SGD", "-10000.00", "-10000.00"), ("3100", "SGD", "-10000.00", "-10000.00")]
    assert _lines(r, "OB-0001") == opening  # canonical form, sorted by account; share capital takes its line
    assert _lines(r, "OB-0001", "reversal") == [(a, c, _neg(x), _neg(b)) for a, c, x, b in opening]
    assert _lines(r, "OB-0001", "replacement") == [
        ("1010", "SGD", "25000.00", "25000.00"), ("1500", "SGD", "3600.00", "3600.00"),
        ("1590", "SGD", "-600.00", "-600.00"), ("2050", "SGD", "-500.00", "-500.00"),
        ("2500", "SGD", "-8000.00", "-8000.00"), ("3010", "SGD", "-10000.00", "-10000.00"),
        ("3100", "SGD", "-9500.00", "-9500.00")]
    ob = next(e for e in r["entries"] if e["document"] == "OB-0001")
    assert [ln.get("party") for ln in ob["lines"]] == [None, None, None, "P0001", None, None]
    assert _lines(r, "JNL-0002", "reversal") == [("6020", "SGD", "-800.00", "-800.00"), ("2050", "SGD", "800.00", "800.00")]
    assert _lines(r, "JNL-0002", "auto_reversal") == [("6020", "SGD", "-850.00", "-850.00"),
                                                       ("2050", "SGD", "850.00", "850.00")]
    assert _lines(r, "JNL-0006", "reversal", "2026-02-12") == [("6010", "SGD", "-330.00", "-330.00"),
                                                                ("1010", "SGD", "330.00", "330.00")]
    # USD 100.00 × 1.3472 = 134.72; the automatic reversal negates it as stored, never re-converted (ENG-14).
    assert _lines(r, "JNL-0009", "auto_reversal") == [("6010", "USD", "-100.00", "-134.72"),
                                                       ("2050", "SGD", "134.72", "134.72")]
    assert r["trial_balance"] == {
        "2026-01": {
            "1010": "25000.00",  # 25,000 − 25,000 + 25,000 (the balances, reversed, again); − 300 + 300 (JNL-0006)
            "1500": "3600.00",
            "1590": "-600.00",
            "2050": "-1700.00",  # −500 (the edited balances) − 1,200 (JNL-0001)
            "2500": "-8000.00",
            "3010": "-10000.00",
            "3100": "-9500.00",
            "6010": "1200.00",  # 1,200 + 300 − 300
        },
        "2026-02": {
            "2050": "350.00",  # −800 + 800 − 850 (JNL-0002 and its edit) + 1,200 (JNL-0001 reversed by itself)
            "6010": "-1200.00",  # 330 − 330 (JNL-0006's replacement and void) − 1,200
            "6020": "850.00",  # 800 − 800 + 850
        },
        "2026-03": {
            "2050": "715.28",  # −500 + 500 (JNL-0003 and its void) − 134.72 (JNL-0009) + 850 (JNL-0002 reversed)
            "6010": "134.72",  # 500 − 500 + 134.72
            "6020": "-850.00",
        },
        "2026-04": {
            "2050": "-205.28",  # −200 + 200 − 250 (JNL-0007 edited) − 90 (JNL-0008) + 134.72 (JNL-0009 reversed)
            "6010": "115.28",  # 200 − 200 + 250 − 134.72
            "6020": "90.00",
        },
    }
    assert r["people"] == {"P0001": {"2500": "-8000.00"}}  # −8,000 + 8,000 − 8,000


def _neg(text: str) -> str:
    return text[1:] if text.startswith("-") else f"-{text}"
