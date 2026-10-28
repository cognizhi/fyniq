"""The golden scenarios the Go gate posts (test/differential/golden/), figures worked out by hand.

``decided-rules.json`` holds one document or reversal for each shape PRD-001 rev 0.24 decided
(test/differential/README.md); every figure comes from the comment beside it (QA-ENG-01).
"""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden"


def _run(name: str) -> tuple[dict, Ledger]:
    ledger = Ledger(load_scenario(GOLDEN / f"{name}.json"))
    return ledger.result(), ledger


def _lines(result: dict, document: str, kind: str = "post") -> list[tuple]:
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == kind:
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in e["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


# req: QA-ENG-02
def test_every_golden_file_is_listed_in_its_readme():
    listed = (GOLDEN / "README.md").read_text(encoding="utf-8")
    for path in GOLDEN.glob("*.json"):
        assert "shrunk-" in path.name or f"`{path.name}`" in listed, path.name


# req: QA-ENG-02
def test_decided_rules_posts_what_was_worked_by_hand():
    r, _ = _run("decided-rules")
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("JNL-0001", "post", "2026-01-05"),
        ("JNL-0005", "post", "2026-01-21"),
        ("JNL-0001", "reversal", "2026-03-02"),  # the reversal on 4 Jan was refused
    ]
    # JPY 4 × 0.001 = 0.004, below half a cent: 0.00, and the line still posts. JPY 5 × 0.001 =
    # 0.005, exactly half: 0.01, away from zero. The SGD line balances the cent.
    assert _lines(r, "JNL-0001") == [
        ("6010", "JPY", "4", "0.00"),
        ("6110", "JPY", "5", "0.01"),
        ("1010", "SGD", "-0.01", "-0.01"),
    ]
    # USD 999 999 999 999 999.99 × 10 = 9 999 999 999 999 999.90: sixteen integer digits fit.
    assert _lines(r, "JNL-0005") == [
        ("1020", "USD", "999999999999999.99", "9999999999999999.90"),
        ("4010", "USD", "-999999999999999.99", "-9999999999999999.90"),
    ]
    # The stored lines negated, never re-converted; a base of 0.00 stays 0.00, not -0.00.
    assert _lines(r, "JNL-0001", "reversal") == [
        ("6010", "JPY", "-4", "0.00"),
        ("6110", "JPY", "-5", "-0.01"),
        ("1010", "SGD", "0.01", "0.01"),
    ]
    assert r["rejected"] == ["JNL-0002", "JNL-0003", "JNL-0004", "JNL-0006", "JNL-0007", "JNL-0008",
                             "JNL-0001/reversal"]
    # 6010 nets 0.00 in each month and is left out.
    assert r["trial_balance"] == {
        "2026-01": {"1010": "-0.01", "1020": "9999999999999999.90", "4010": "-9999999999999999.90", "6110": "0.01"},
        "2026-03": {"1010": "0.01", "6110": "-0.01"},
    }


# req: ENG-24
def test_decided_rules_refuses_each_document_for_its_own_rule():
    _, ledger = _run("decided-rules")
    assert ledger.reasons == [
        ("JNL-0002", "rate-on-base-line"),  # rate 1, source "base", on an SGD line (ENG-03)
        ("JNL-0003", "rate-on-base-line"),  # the source alone (ENG-03)
        # USD 1 000 000 000 000 000.00 × 10 = 10 000 000 000 000 000.00: seventeen integer digits,
        # refused, never truncated, though the journal balances in dollars and in base (ENG-44)
        ("JNL-0004", "base-out-of-range"),
        ("JNL-0006", "share-capital-account"),  # INV-13
        ("JNL-0007", "amount-too-many-decimals"),  # "12345.0": one decimal more than JPY has (ENG-44)
        # USD 0.01 × 1.5 = 0.015 -> 0.02, twice; -0.02 × 1.5 = -0.03; 0.02 + 0.02 - 0.03 = 0.01 (ENG-24 rule 7)
        ("JNL-0008", "unbalanced-base"),
        ("JNL-0001/reversal", "reversal-before-entry"),  # 4 Jan, before the entry of 5 Jan (FR-GL-12)
    ]
