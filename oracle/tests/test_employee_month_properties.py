"""Properties of Nemura's pay runs on generated runs (QA-ENG-03): the identities of PRD-001 Appendix B.3 hold for every run, whatever the
facts, and what the rules say cannot matter does not (the order of people and items, how one month's pay is split across runs)."""

import json
import subprocess
import sys
from decimal import Decimal

from hypothesis import given, strategies as st

from oracle import employee_month as em
from oracle.strategies_payroll_month import generate, payroll_runs

CENT = Decimal("0.01")


def d(text):
    return Decimal(text)


# req: QA-ENG-03, FR-PAY-18
@given(payroll_runs())
def test_a_runs_entry_balances_and_its_totals_follow_from_its_people(doc):
    out = em.payroll_run(doc)
    t = {k: d(v) for k, v in out["totals"].items()}
    ok = [p for p in out["people"] if "error" not in p]
    assert out["balanced"] is True
    assert t["gross"] == sum((d(p["gross"]) for p in ok), Decimal(0))
    assert t["net"] == t["gross"] - t["employee_cpf"] - t["shg"] - t["deductions"]
    assert t["disburse"] == t["net"] + t["reimbursements"] - t["held"]
    assert t["sdl_payable"] == t["sdl_payable"].to_integral_value() and 0 <= t["sdl_employees"] - t["sdl_payable"] < 1  # rounded down once
    assert t["employer_cost"] == t["gross"] + t["employer_cpf"] + t["sdl_payable"]
    p = {k: (d(v["debit"]), d(v["credit"])) for k, v in out["postings"].items()}
    assert p["2110"][1] == t["employee_cpf"] + t["employer_cpf"] + t["sdl_payable"] + t["shg"]
    assert p["2100"][1] == t["disburse"] + t["held"]
    assert p["1250"][1] + p["deductions"][1] == t["deductions"]


# req: QA-ENG-03, FR-PAY-02
@given(payroll_runs())
def test_each_months_cpf_is_in_whole_dollars_and_its_shares_add_up(doc):
    overridden = {e["id"] for e in doc["people"] if any(o["field"].startswith("cpf_") for o in e.get("overrides", []))}
    for person in em.payroll_run(doc)["people"]:
        if "error" in person:
            continue
        total, employee, employer = d(person["cpf_total"]), d(person["cpf_employee"]), d(person["cpf_employer"])
        assert d(person["net"]) == d(person["gross"]) - employee - d(person["shg"]) - d(person["deductions"])
        if person["id"] in overridden:  # an override is the person's figure (FR-PAY-06): not a whole dollar, and the total follows the shares
            assert total == employee + employer
            continue
        assert total == total.to_integral_value() and employee == employee.to_integral_value()
        assert employee <= total and employer == total - employee
        assert d(person["net"]) == d(person["gross"]) - employee - d(person["shg"]) - d(person["deductions"])
        assert d(person["ow_subject"]) >= 0 and d(person["aw_subject"]) >= 0
        for key, value in person.items():
            if key not in ("id", "violations", "difference"):
                assert d(value) == d(value).quantize(CENT)  # never finer than a cent


# req: QA-ENG-03, FR-PAY-01
@given(payroll_runs(), st.randoms(use_true_random=False))
def test_the_order_of_people_and_of_items_changes_nothing(doc, rnd):
    shuffled = json.loads(json.dumps(doc))
    rnd.shuffle(shuffled["people"])
    for entry in shuffled["people"]:
        rnd.shuffle(entry["items"])
    a, b = em.payroll_run(doc), em.payroll_run(shuffled)
    assert a["totals"] == b["totals"] and a["postings"] == b["postings"]
    assert {p["id"]: p for p in a["people"]} == {p["id"]: p for p in b["people"]}


# req: QA-ENG-03, FR-PAY-02 (G4-003)
@given(st.integers(0, 3_000_000), st.integers(0, 3_000_000), st.sampled_from(["1990-01-01", "1969-01-01", "1964-01-01", "1959-01-01", "1950-01-01"]))
def test_one_months_ordinary_pay_split_across_runs_gives_the_same_month(a, b, born):
    def month(items):
        return em.employee_month_doc({
            "schema": "fyniq-payroll-month@1", "name": "t", "month": "2026-06-01", "person": {"residency": "citizen", "birth_date": born},
            "items": items, "expect": {}})

    def text(cents):
        return f"{cents // 100}.{cents % 100:02d}"

    one = month([{"key": "x", "name": "x", "amount": text(a + b), "cpf_wages": True}])
    two = month([{"key": "x", "name": "x", "amount": text(a), "cpf_wages": True}, {"key": "y", "name": "y", "amount": text(b), "cpf_wages": True}])
    assert one == two


# req: QA-ENG-03, FR-PAY-01
@given(payroll_runs())
def test_a_later_run_with_the_same_facts_posts_nothing(doc):
    for entry in doc["people"]:
        entry["posted"] = {k: v for k, v in entry.items() if k in ("items", "ytd", "last_month", "reimbursements", "deductions", "final", "held", "overrides")}
    for person in em.payroll_run(doc)["people"]:
        if "difference" in person:
            assert all(d(v) == 0 for v in person["difference"].values())


# req: QA-ENG-02
def test_the_command_line_works_a_run_out_and_generates_valid_runs(tmp_path):
    runs = generate(3, 6)
    assert len(runs) == 6 and len({json.dumps(r, sort_keys=True) for r in runs}) == 6
    path = tmp_path / "run.json"
    path.write_text(json.dumps(runs[0]))
    done = subprocess.run([sys.executable, "-m", "oracle", "payroll", str(path)], capture_output=True, text=True, check=False)
    assert done.returncode == 0, done.stderr
    assert json.loads(done.stdout)["schema"] == "fyniq-payroll-run-result@1"
    bad = tmp_path / "bad.json"
    bad.write_text(json.dumps({**runs[0], "month": "2026-06-02"}))
    refused = subprocess.run([sys.executable, "-m", "oracle", "payroll", str(bad)], capture_output=True, text=True, check=False)
    assert refused.returncode == 2 and refused.stdout == ""
    out = tmp_path / "gen"
    made = subprocess.run([sys.executable, "-m", "oracle", "generate-payrolls", "--seed", "1", "--count", "5", "--out", str(out)],
                          capture_output=True, text=True, check=False)
    assert made.returncode == 0 and len(list(out.glob("*.json"))) == 5
