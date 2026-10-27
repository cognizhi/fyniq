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
