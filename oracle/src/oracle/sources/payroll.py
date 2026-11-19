"""A pay run from the people platform (``payroll_run@1``), from PRD-001 FR-PAY-07, FR-PAY-18, FR-PPL-04, FR-PPL-11, §10.2 and
Appendix B.3 ("Pay run: salary account by department (gross); 6020 employer CPF; 6030 SDL | 2100 salaries payable (net pay);
2110 CPF & SDL payable (EE + ER CPF + SDL + SHG)"), and p1-payroll-link design D7. Written from those rules, not from the engine.

The document is one *version* of a run's wage month (``run`` names the run, ``version`` counts up) stating the month's **full**
figures: each department's gross (``departments``: its salary expense ``account`` and ``gross``), the month's employee CPF,
employer CPF, SDL and SHG totals, the deductions taken from net pay (``deductions``: an ``account`` they are recovered through
and an ``amount``) and the claims already booked that are paid with salary (``reimbursements``, which clear 2150). Nothing
here is per person: the ledger holds no per-person pay (FR-PAY-13).

The signed amounts of the month as it stands (debit positive):

* every department's salary account: its gross; 6020 (``employer_cpf``): employer CPF; 6030 (``sdl``): SDL; 2150
  (``claims_payable``): the reimbursements;
* 2110 (``cpf_sdl_payable``): minus employee CPF, employer CPF, SDL and SHG;
* each deduction's account: minus its amount;
* 2100 (``salaries_payable``): minus what is left to pay: the gross less employee CPF, SHG and the deductions, plus the
  reimbursements (FR-PAY-18: "net pay + reimbursements"; held monies stay payable and change no entry).

**What posts** is the difference between those amounts and what the run's earlier versions posted (FR-PPL-11): per account, the
new amount less the sum of every earlier entry of the run on that account, the lines whose difference is not zero, in account
code order; no line at all when nothing changed. A version no newer than the latest posted of its run is refused as stale. The
entry posts on the document's ``date``, which the caller sets in the wage month or, when it is closed, in the open month, and which
may not be before the wage month. Deductions may not exceed the pay, and every amount is in the base currency.
"""

from __future__ import annotations

from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source

# A run's posted state in ``books`` is kept under a key no document ID can take: an ID never has a "/".
_STATE = "payroll/"


def _state(books: dict[str, Any], run: str) -> dict[str, Any]:
    return books.setdefault(_STATE + run, {"version": 0, "totals": {}})


def payroll_run(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    wage_month = date.fromisoformat(d["wage_month"] + "-01")
    if doc.date < wage_month:
        raise Rejected("posting-before-wage-month")
    state = _state(books, d["run"])
    version = d["version"]
    if version <= state["version"]:
        raise Rejected("stale-version")
    zero = money.ZERO

    amounts: dict[str, Decimal] = {}  # account code -> signed amount of the month as it stands

    def put(code: str, value: Decimal) -> None:
        amounts[code] = amounts.get(code, zero) + value

    gross = zero
    for dept in d["departments"]:
        account = r.entered(dept["account"])
        if (account.type != "expense" or r.is_system(dept["account"], "employer_cpf", "sdl")):
            raise Rejected("not-a-salary-account")
        amount = r.amount(dept["gross"], r.base, sign=1, zero=True)
        gross += amount
        put(dept["account"], amount)
    employee = r.amount(d["employee_cpf"], r.base, sign=1, zero=True)
    employer = r.amount(d["employer_cpf"], r.base, sign=1, zero=True)
    sdl = r.amount(d["sdl"], r.base, sign=1, zero=True)
    shg = r.amount(d["shg"], r.base, sign=1, zero=True)
    reimbursed = r.amount(d.get("reimbursements", "0"), r.base, sign=1, zero=True)
    deducted = zero
    for ded in d.get("deductions", []):
        account = r.account(ded["account"])
        if (account.type not in ("asset", "liability") or ded["account"] in r.settings.money_accounts
                or account.role in ("director-loan", "deferred-remuneration", "gst-control", "salaries-payable")
                or ded["account"] == r.system("salaries_payable")):  # the net pay is 2100's, however the chart names the role
            raise Rejected("deduction-account-not-allowed")
        amount = r.amount(ded["amount"], r.base, sign=1)
        deducted += amount
        put(ded["account"], money.negate(amount))
    left_to_pay = gross - employee - shg - deducted
    if left_to_pay < 0:
        raise Rejected("deductions-more-than-pay")
    left_to_pay += reimbursed
    put(r.system("employer_cpf"), employer)
    put(r.system("sdl"), sdl)
    if reimbursed > 0:
        put(r.system("claims_payable"), reimbursed)
    put(r.system("cpf_sdl_payable"), money.negate(employee + employer + sdl + shg))
    put(r.system("salaries_payable"), money.negate(left_to_pay))

    codes = sorted(set(amounts) | set(state["totals"]))
    lines = []
    for code in codes:
        diff = amounts.get(code, zero) - state["totals"].get(code, zero)
        if diff != 0:
            lines.append(r.base_line(code, diff))

    def remember() -> None:
        state["version"] = version
        for line in lines:
            state["totals"][line.account] = state["totals"].get(line.account, zero) + line.base_amount

    return Posting(lines, effects=[remember])
