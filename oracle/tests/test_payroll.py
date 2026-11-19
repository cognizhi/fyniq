"""Pay runs from the people platform (payroll_run@1), figures worked out by hand from PRD-001 FR-PAY-07, FR-PAY-18, FR-PPL-11
and Appendix B.3 (test/differential/golden/payroll.json). Every figure comes from the comment beside it (QA-ENG-01)."""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden" / "payroll.json"


def _result() -> dict:
    return Ledger(load_scenario(GOLDEN)).result()


def _lines(result: dict, document: str) -> list[tuple[str, str]]:
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == "post":
            return [(ln["account"], ln["base_amount"]) for ln in e["lines"]]
    raise AssertionError(f"no entry for {document}")


# part of FR-PAY-07 (the posting rule; tagged with the verified path of p1-payroll-link 5.3)
def test_the_october_run_posts_by_department_with_no_person():
    # gross 10,000.00 + 20,000.00; employee CPF 3,000.00; SHG 20.00; an advance of 500.00 recovered; 300.00 of claims paid with salary.
    # 2100 = 30,000.00 - 3,000.00 - 20.00 - 500.00 + 300.00 = 26,780.00
    # 2110 = 3,000.00 + 5,100.00 (employer CPF) + 75.00 (SDL) + 20.00 = 8,195.00
    # Dr 300.00 + 20,000.00 + 10,000.00 + 5,100.00 + 75.00 = 35,475.00 = Cr 500.00 + 26,780.00 + 8,195.00
    assert _lines(_result(), "PR-0001") == [("1250", "-500.00"), ("2100", "-26780.00"), ("2110", "-8195.00"), ("2150", "300.00"),
                                           ("6011", "20000.00"), ("6013", "10000.00"), ("6020", "5100.00"), ("6030", "75.00")]


# part of FR-PAY-07 (the posting rule; tagged with the verified path of p1-payroll-link 5.3)
def test_a_correction_posts_only_the_difference_in_the_open_month():
    # Version 2 states R&D at 20,200.00 and employer CPF at 5,134.00: 6011 +200.00, 6020 +34.00; 2100 falls by the 200.00 more to pay,
    # 2110 by the 34.00 more employer CPF. It is dated 5 Nov: the wage month October is closed, so the difference posts in November.
    r = _result()
    assert _lines(r, "PR-0002") == [("2100", "-200.00"), ("2110", "-34.00"), ("6011", "200.00"), ("6020", "34.00")]
    assert [e["posting_date"] for e in r["entries"] if e["document"] == "PR-0002"] == ["2026-11-05"]


# part of FR-PAY-07 (the posting rule; tagged with the verified path of p1-payroll-link 5.3)
def test_a_version_that_changes_nothing_posts_nothing_and_an_old_one_is_refused():
    # Version 3 repeats version 2's figures: no line differs, so no entry. Version 1 again is not newer than version 3's run: stale.
    r = _result()
    assert all(e["document"] != "PR-0003" for e in r["entries"])
    assert "PR-0003" not in r["rejected"] and "PR-0004" in r["rejected"]


# part of FR-PAY-07 (the posting rule; tagged with the verified path of p1-payroll-link 5.3)
def test_another_run_stands_alone_and_the_refusals():
    r = _result()
    # 9,000.50 gross; net = 9,000.50 - 1,800.10 = 7,200.40; 2110 = 1,800.10 + 1,530.09 + 22.50 = 3,352.69
    assert _lines(r, "PR-0005") == [("2100", "-7200.40"), ("2110", "-3352.69"), ("6013", "9000.50"), ("6020", "1530.09"), ("6030", "22.50")]
    # PR-0006: deductions of 900.00 beyond 1,000.00 - 100.00 - 5.00 = 895.00; PR-0007 a money account as a department; PR-0008 before the
    # wage month; PR-0009 a director's account as a deduction; PR-0010 the employer CPF account as a department.
    assert r["rejected"] == ["PR-0004", "PR-0006", "PR-0007", "PR-0008", "PR-0009", "PR-0010"]


# part of FR-PAY-07 (the posting rule); B.3: the net pay is 2100's, so a deduction is recovered through its own account, never 2100
def test_a_deduction_cannot_be_recovered_through_salaries_payable():
    # test/differential/golden/payroll-deduction-on-salaries-payable.json: gross 0.01 with a deduction of 0.01 on 2100, which the settings name as salaries payable
    # although the account carries no role of its own: refused, as the engine refuses it (the full differential found the oracle posting it).
    result = Ledger(load_scenario(GOLDEN.with_name("payroll-deduction-on-salaries-payable.json"))).result()
    assert result["rejected"] == ["PR-0001"] and result["entries"] == []
