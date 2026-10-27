"""The pending golden scenarios (test/differential/pending/), figures worked out by hand.

They wait for cmd/scenario-run to read the P1 shapes (pending/README.md); the oracle must match the
hand-worked figures now. Every figure comes from the comment beside it (QA-ENG-01).
"""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

PENDING = Path(__file__).resolve().parents[2] / "test" / "differential" / "pending"


def _run(name: str) -> dict:
    return Ledger(load_scenario(PENDING / f"{name}.json")).result()


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
    assert r["rejected"] == ["RC-0003", "INV-0006"]  # USD 11.00 of 10.95 open; a spread on an expense
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
            "1100": "-435.31",  # -10.95 - 32.86 - 391.50
            "2200": "45.90",  # 0.90 + 45.00
            "2400": "100.00",
            "2410": "500.00",
            "4010": "-89.95",  # 10.05 - April's 100.00
            "7030": "392.00",  # 0.50 + 391.50
        },
    }


# req: QA-ENG-02
def test_pending_claims():
    r = _run("claims")
    assert r["rejected"] == ["RE-0003", "EC-0005"]  # EC-0004 is paid; a trip in USD
    assert [e["document"] for e in r["entries"]] == ["ADV-0001", "EC-0001", "EC-0002", "EC-0003", "EC-0004", "RE-0001",
                                                     "RE-0002", "ADV-0002", "ADV-0002R"]
    # 100.00 + 9.00 GST and 12.35 km -> 12.4 × 0.60 = 7.44: 116.44 taken from the 500.00 advance.
    assert _lines(r, "EC-0001") == [("1250", "SGD", "-116.44", "-116.44"), ("2200", "SGD", "9.00", "9.00"),
                                    ("6010", "SGD", "100.00", "100.00"), ("6110", "SGD", "7.44", "7.44")]
    # 450.00: the 383.56 left on the advance, 66.44 owed on 2150.
    assert _lines(r, "EC-0002") == [("1250", "SGD", "-383.56", "-383.56"), ("2150", "SGD", "-66.44", "-66.44"),
                                    ("6010", "SGD", "450.00", "450.00")]
    # 7.25 km -> 7.3 × 0.25 = 1.825 -> 1.83, and 100 km × 0.60 = 60.00.
    assert _lines(r, "EC-0004") == [("2150", "SGD", "-61.83", "-61.83"), ("6110", "SGD", "61.83", "61.83")]
    # USD 50.00 carried at 67.50, reimbursed at 1.33 = 66.50: 1.00 to 7030.
    assert _lines(r, "RE-0001") == [("1010", "SGD", "-66.50", "-66.50"), ("2150", "USD", "50.00", "67.50"),
                                    ("7030", "SGD", "-1.00", "-1.00")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "-694.77",  # -500.00 - 66.50 - 128.27; 1250 and 2150 are back at nothing
            "2200": "9.00",
            "6010": "617.50",  # 100.00 + 450.00 + 67.50
            "6110": "69.27",  # 7.44 + 61.83
            "7030": "-1.00",
        },
        "2026-04": {},  # 300.00 advanced and refunded
    }


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
