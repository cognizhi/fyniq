"""Nemura's payroll, the employee-month and the run, in exact decimals (QA-ENG-01; PRD-001 §8.7 and Appendix B.3, PRD-002 ADR-N2).

Written from the rules, never from the Go engine (``internal/nemura/payroll``): PRD-001 FR-PAY-01 to 04, 14, 16, 17, 18, Appendix B.3,
the rounding policies ``RND-CPF``, ``RND-CPF-GRAD`` and ``RND-SDL`` of §10.4 and the statutory packs ``sg-cpf@2026.2``, ``sg-sdl@2026.1``
and ``sg-shg@2026.1`` (``packs/``; their CSV tables are read here, by this module's own reader, and none of the Go code is used).

**The unit is the employee-month** (B.3): every item of one calendar month for one person, across the main run, an off-cycle run and
a correction. A *run* is the set of its people's employee-months, with the totals kept apart (FR-PAY-18) and the entry that posts them
(B.3, "Postings"). A later run or correction recomputes the whole month, and only the difference from what was posted is posted.

The rules this module applies, with the source of each:

* **Wage classes by facts** (FR-PAY-17). An item that is CPF wages is *ordinary* when it is for the month's employment (``for_month``,
  the wage month when it names none) and payable by the 14th of the following month (``payable_on``, the month's pay day when it names
  none), and *additional* otherwise (so pay for another month's employment is additional wages: the CPF Board's two conditions, checked 20 Nov 2026
  [accountant to confirm: an item for the month before that is payable by the 14th is that month's ordinary wages]); an item that is not CPF wages is
  neither. The name (``suggested``) changes no figure.
* **Gross** is every item added (B.3). **Ordinary wages for CPF** are ``min(max(0, ordinary items), OW ceiling)``; the **additional
  wages** of the month are the additional items (not below nothing), limited by the annual ceiling (below).
* **Additional Wage ceiling** (FR-PAY-02, FR-PAY-14): the annual salary ceiling less the year's ordinary wages subject to CPF, per
  employee, per employer, per calendar year, tracked year to date. A month before the last pay month estimates the year's ordinary
  wages as what is counted so far plus this month's ordinary wages subject to CPF for each month left, this one included, and counts
  the additional wages up to that ceiling less the additional wages already counted. The last month (December, or the last month of
  employment) recalculates with the actual ordinary wages: the additional wages paid in the year are counted up to the ceiling less the
  actual ordinary wages; the difference from what was counted is a shortfall added to the month, or an excess that is a refund task.
  A month that pays additional wages to a person CPF applies to, with no year to date, is refused (``needs_year_to_date``, ✖).
* **CPF** (FR-PAY-02, B.3): citizens and permanent residents who are employees; a permanent resident in the first or second year of
  residence is at the graduated rates of both (G/G), or at the full employer rate and graduated employee rate (F/G) after a joint
  application (``joint_election``). The year of residence runs to the end of the month of the first and of the second anniversary of
  ``pr_start``; a resident with no start date is read as past both. The age band changes from the first day of the month after the 55th,
  60th, 65th and 70th birthday. Wages up to S$50 carry nothing; above S$50 to S$500 the flat rate of the wages, all of it the employer's;
  above S$500 to S$750 the flat rate of the wages plus the slope of the wages above S$500, the slope being the employee's share
  (``RND-CPF-GRAD``); above S$750 the rates of the rates table on the subject wages (``RND-CPF``). The month's total wages, which pick the band,
  are the wages subject to CPF: the capped ordinary wages and the additional wages after their ceiling and any true-up (the CPF Board's table notes define "TW: Total
  Wages = OW + AW" with OW capped at the ceiling; whether the AW is the one after the annual ceiling is [accountant to confirm]). In both the **total** is rounded to
  the nearest dollar with 50 cents up, the **employee's share** is rounded down to the dollar, the employer's is the difference, once
  per employee-month.
* **SDL** (FR-PAY-03, ``RND-SDL``): an employee's levy is the rate on the month's remuneration (the gross) rounded to the cent half up,
  held between the pack's minimum and maximum, and nothing when the remuneration is not above nothing; a contractor pays none. The
  employer-month **SDL payable** is the employees' amounts added and rounded **down** to the dollar.
* **SHG** (FR-PAY-04): the fund's amount for the band the month's wages (the gross, before the ceilings) fall in, unless the employee
  opted out; nothing when there are none [accountant to confirm: whether items that are not CPF wages count as "monthly total wages"; the CPF pages do not define it].
* **Deductions** (FR-PAY-16): only the kinds MOM allows; ``other`` needs written consent; the deductions, not counting absence, the
  recovery of advances, loans, overpaid salary or unearned benefits and co-operative society payments, are at most 50% of the salary
  payable (the gross) except on termination (``final``); an advance is recovered in at most 12 instalments of at most 25% of the
  salary each; on final pay an advance is recovered only as far as the employee consented in writing [accountant to confirm: MOM does not say
  whether the 12 and 25% limits also stop the rest of an advance on final pay]. A breach is ✖ with its rule named.
* **Net pay** is the gross less the employee's CPF, the SHG and the deductions; the **amount to disburse** is net pay plus the non-wage
  reimbursements less the monies held (FR-PAY-18). The employer's cost is the gross, the employer's CPF and the SDL.
* **The entry** (B.3): Dr salary expense (the gross); Dr 6020 (employer CPF); Dr 6030 (SDL payable); Dr 2150 (reimbursements); Cr 1250
  (advances recovered); Cr the other deductions' own accounts; Cr 2100 (the amount to disburse plus the monies held); Cr 2110 (the
  employees' and employers' CPF, the SDL payable and the SHG). It balances.

Rules this module has to read and that the sources leave open are marked ``[verify]`` where they are decided; they are listed in the
README of ``test/differential/``, "Payroll months".
"""

from __future__ import annotations

import csv
import os
from datetime import date, timedelta
from decimal import ROUND_DOWN, ROUND_FLOOR, ROUND_HALF_UP, Context, Decimal
from functools import cache
from pathlib import Path
from typing import Any

from .scenario import ScenarioError, schema_error

EXACT = Context(prec=80)
ZERO = Decimal(0)
CENT = Decimal("0.01")
DOLLAR = Decimal(1)

DEFAULT_PACKS = {"cpf": "sg-cpf@2026.2", "sdl": "sg-sdl@2026.1", "shg": "sg-shg@2026.1"}

ORDINARY = "ordinary"
ADDITIONAL = "additional"
NOT_WAGES = "none"

# The kinds of deduction MOM allows (FR-PAY-16) and the ones the 50% limit does not count.
KINDS = ("absence", "damage_or_loss", "accommodation", "recovery", "cpf", "income_tax", "court_order", "cooperative", "other")
EXEMPT_FROM_HALF = ("absence", "recovery", "cooperative")
HALF = Decimal("0.5")
QUARTER = Decimal("0.25")
MAX_ADVANCE_INSTALMENTS = 12

RESIDENCIES = ("citizen", "permanent_resident", "foreigner")
FUNDS = ("CDAC", "ECF", "MBMF", "SINDA")


class PayrollError(ScenarioError):
    """The document is not a valid employee-month or run (the oracle exits with status 2)."""


# --- decimals ----------------------------------------------------------------------------------------------------------------------


def _d(text: Any, what: str) -> Decimal:
    try:
        return EXACT.create_decimal(str(text))
    except ArithmeticError as exc:
        raise PayrollError(f"{what}: {text!r} is not a decimal") from exc


def _money(text: Any, what: str) -> Decimal:
    value = _d(text, what)
    if value != value.quantize(CENT):
        raise PayrollError(f"{what}: {text} has more than two decimals")
    return value


def cents_half_up(value: Decimal) -> Decimal:
    return value.quantize(CENT, rounding=ROUND_HALF_UP, context=EXACT)


def dollars_half_up(value: Decimal) -> Decimal:
    """``RND-CPF``: to the nearest dollar, 50 cents rounding up."""
    return value.quantize(DOLLAR, rounding=ROUND_HALF_UP, context=EXACT)


def dollars_down(value: Decimal) -> Decimal:
    """The employee's share (``RND-CPF``) and the SDL payable (``RND-SDL``): down to the dollar."""
    return value.quantize(DOLLAR, rounding=ROUND_FLOOR, context=EXACT)


def cents_down(value: Decimal) -> Decimal:
    return value.quantize(CENT, rounding=ROUND_DOWN, context=EXACT)


def fmt(value: Decimal) -> str:
    return format(value.quantize(CENT, context=EXACT), "f")


# --- the packs ---------------------------------------------------------------------------------------------------------------------


def _packs_root() -> Path:
    return Path(os.environ.get("FYNIQ_PACKS_DIR") or Path(__file__).resolve().parents[3] / "packs")


def _day(text: str) -> date | None:
    return date.fromisoformat(text) if text else None


def _in_force(row: dict[str, str], month: date) -> bool:
    start, end = _day(row["effective_from"]), _day(row["effective_to"])
    return (start is None or start <= month) and (end is None or month <= end)


@cache
def _table(pack_id: str, name: str) -> tuple[dict[str, str], ...]:
    name_, _, version = pack_id.partition("@")
    path = _packs_root() / name_ / version / name
    try:
        with path.open(newline="", encoding="utf-8") as handle:
            return tuple(csv.DictReader(handle))
    except OSError as exc:
        raise PayrollError(f"{pack_id} holds no {name}") from exc


def _pack_ids(doc: dict[str, Any]) -> dict[str, str]:
    ids = dict(DEFAULT_PACKS)
    for key, value in (doc.get("packs") or {}).items():
        if key not in ids or not isinstance(value, str):
            raise PayrollError(f"packs: {key!r} is not one of {sorted(ids)}")
        ids[key] = value
    return ids


# --- the person --------------------------------------------------------------------------------------------------------------------


def _passed(birth: date, month: date, years: int) -> bool:
    """Whether the person is above ``years`` of age for the wage month: from the first day of the month after the birthday. A 29 February
    birthday falls on 1 March in a year with no 29 February [accountant to confirm: the CPF Board says nothing; 1 March never under-pays, as the younger band's rates are never lower]."""
    year = birth.year + years
    try:
        birthday = date(year, birth.month, birth.day)
    except ValueError:
        birthday = date(year, 3, 1)
    return (birthday.year, birthday.month) < (month.year, month.month)


def pr_year(start: date, month: date) -> int:
    """The year of permanent residence: the first until the end of the month of the first anniversary, the second until the end of the
    month of the second, the third (full rates) after."""
    here = (month.year, month.month)
    if here <= (start.year + 1, start.month):
        return 1
    return 2 if here <= (start.year + 2, start.month) else 3


def cpf_group(person: dict[str, Any], month: date) -> str | None:
    """The rate group of the sg-cpf pack, or None when CPF does not apply."""
    if not person.get("employee", True) or person["residency"] == "foreigner":
        return None
    if person["residency"] == "citizen" or not person.get("pr_start"):
        return "sc_spr3"
    year = pr_year(date.fromisoformat(person["pr_start"]), month)
    if year >= 3:
        return "sc_spr3"
    return f"spr{year}_{'fg' if person.get('joint_election') else 'gg'}"


def _age_matches(row: dict[str, str], birth: date, month: date) -> bool:
    above, to = row["age_above"], row["age_to"]
    return (not above or _passed(birth, month, int(above))) and (not to or not _passed(birth, month, int(to)))


# --- CPF ---------------------------------------------------------------------------------------------------------------------------


def _ceiling(pack_id: str, name: str, month: date) -> Decimal:
    rows = [r for r in _table(pack_id, "ceilings.csv") if r["ceiling"] == name and _in_force(r, month)]
    if len(rows) != 1:
        raise PayrollError(f"{pack_id} holds no single {name} ceiling for {month:%Y-%m}")
    return _d(rows[0]["amount"], name)


def _rates(pack_id: str, group: str, birth: date, month: date) -> tuple[Decimal, Decimal]:
    rows = [r for r in _table(pack_id, "rates.csv") if r["group"] == group and _age_matches(r, birth, month) and _in_force(r, month)]
    if len(rows) != 1:
        raise PayrollError(f"{pack_id} holds no single rate row for {group} in {month:%Y-%m}")
    return _d(rows[0]["employer"], "employer rate"), _d(rows[0]["employee"], "employee rate")


def cpf_split(pack_id: str, group: str, birth: date, month: date, subject: Decimal) -> tuple[Decimal, Decimal]:
    """(total, employee's share) in whole dollars for the month's total wages, which are the wages subject to CPF (the capped ordinary wages
    and the additional wages after their ceiling and any true-up: the pack's own definition): ``RND-CPF`` above S$750, ``RND-CPF-GRAD`` up to it."""
    rows = [r for r in _table(pack_id, "low_wages.csv") if r["group"] == group and _age_matches(r, birth, month) and _in_force(r, month)]
    if len(rows) != 1:
        raise PayrollError(f"{pack_id} holds no single graduated row for {group} in {month:%Y-%m}")
    low = rows[0]
    nil_to, flat_to, graduated_to = (_d(low[k], k) for k in ("nil_to", "flat_to", "graduated_to"))
    if subject <= nil_to:
        return ZERO, ZERO
    if subject <= flat_to:
        return dollars_half_up(subject * _d(low["flat_rate"], "flat_rate")), ZERO
    if subject <= graduated_to:
        slope = _d(low["slope"], "slope") * (subject - flat_to)
        return dollars_half_up(subject * _d(low["flat_rate"], "flat_rate") + slope), dollars_down(slope)
    employer, employee = _rates(pack_id, group, birth, month)
    return dollars_half_up(subject * (employer + employee)), dollars_down(subject * employee)


def cpf_refund(pack_id: str, group: str, birth: date, month: date, wages: Decimal) -> tuple[Decimal, Decimal]:
    """(refund total, the employee's share) of the additional wages a true-up finds counted beyond the annual ceiling (FR-PAY-14), whatever band the month's
    own wages are in: the refund is contributions, so the total is the wages at the full table's total rate, to the nearest dollar with 50 cents up (the
    total's rule in ``RND-CPF``) [accountant to confirm: the CPF Board's calculator rounds monthly, its automatic refund over the year]; the employee's share is the
    CPF Board's formula, ``refund / total rate x employee rate``, "cents should be dropped", so down to the dollar (its worked example: $2,260 / 32.5% x 17% =
    $1,182.15 is $1,182; cpf.gov.sg "Why does the excess CPF from the AW ceiling calculator differ ...", checked 20 Nov 2026)."""
    employer, employee = _rates(pack_id, group, birth, month)
    total = dollars_half_up(wages * (employer + employee))
    return total, dollars_down(total / (employer + employee) * employee)


# --- SDL and SHG -------------------------------------------------------------------------------------------------------------------


def sdl_employee(pack_id: str, month: date, remuneration: Decimal) -> Decimal:
    """``RND-SDL``, per employee: clamp(round(remuneration x rate, 2), minimum, maximum); nothing when the remuneration is not above nothing."""
    if remuneration <= 0:
        return ZERO
    rows = [r for r in _table(pack_id, "levy.csv") if _in_force(r, month)]
    if len(rows) != 1:
        raise PayrollError(f"{pack_id} holds no single levy for {month:%Y-%m}")
    levy = rows[0]
    amount = cents_half_up(remuneration * _d(levy["rate"], "rate"))
    return min(max(amount, _d(levy["minimum"], "minimum")), _d(levy["maximum"], "maximum"))


def sdl_payable(amounts: list[Decimal]) -> Decimal:
    """``RND-SDL``, per employer-month: the employees' amounts added, then rounded down to the dollar."""
    return dollars_down(sum(amounts, ZERO))


def shg_fund_amount(pack_id: str, month: date, fund: str, wages: Decimal) -> Decimal:
    """The fund's contribution for the band the total wages fall in: a band 'up to' an amount includes it."""
    if wages <= 0:
        return ZERO
    rows = [
        r
        for r in _table(pack_id, "bands.csv")
        if r["fund"] == fund
        and _in_force(r, month)
        and (not r["wage_above"] or wages > _d(r["wage_above"], "wage_above"))
        and (not r["wage_to"] or wages <= _d(r["wage_to"], "wage_to"))
    ]
    if len(rows) != 1:
        raise PayrollError(f"{pack_id} holds no single band of {fund} for {wages} in {month:%Y-%m}")
    return _d(rows[0]["amount"], "amount")


# --- the month ---------------------------------------------------------------------------------------------------------------------


def _first(text: str, what: str) -> date:
    try:
        day = date.fromisoformat(text)
    except ValueError as exc:
        raise PayrollError(f"{what}: {text!r} is not a date") from exc
    return day


def wage_class(item: dict[str, Any], month: date, pay_day: date) -> str:
    """FR-PAY-17: by the item's facts, never its name."""
    if not item["cpf_wages"]:
        return NOT_WAGES
    for_month = _first(item["for_month"], "for_month") if item.get("for_month") else month
    payable_on = _first(item["payable_on"], "payable_on") if item.get("payable_on") else pay_day
    due = date(month.year + (month.month == 12), month.month % 12 + 1, 14)  # the 14th of the month following the wage month
    return ORDINARY if (for_month.year, for_month.month) == (month.year, month.month) and payable_on <= due else ADDITIONAL


def last_day(month: date) -> date:
    return date(month.year + (month.month == 12), month.month % 12 + 1, 1) - timedelta(days=1)


def _facts(doc: dict[str, Any]) -> dict[str, Any]:
    """The facts of a month, read and checked: ``{items, ytd, last_month, reimbursements, deductions, final, held}``."""
    items = []
    for raw in doc["items"]:
        items.append({**raw, "amount": _money(raw["amount"], f"item {raw['key']}")})
    ytd = None
    if doc.get("ytd") is not None:
        ytd = {k: _d(doc["ytd"][k], f"ytd.{k}") for k in ("ow", "aw_subject", "aw_paid")}
    deductions = []
    for raw in doc.get("deductions") or []:
        if raw["kind"] not in KINDS:
            raise PayrollError(f"deduction {raw['key']}: {raw['kind']!r} is not a kind of deduction")
        deductions.append(
            {
                "key": raw["key"],
                "kind": raw["kind"],
                "amount": _money(raw["amount"], f"deduction {raw['key']}"),
                "consent": bool(raw.get("consent", False)),
                "instalment": int(raw.get("instalment", 0)),
                "is_advance": bool(raw.get("is_advance", False)),
            }
        )
    return {
        "items": items,
        "ytd": ytd,
        "last_month": bool(doc.get("last_month", False)),
        "reimbursements": [_money(x, "reimbursement") for x in doc.get("reimbursements") or []],
        "deductions": deductions,
        "final": bool(doc.get("final", False)),
        "held": _money(doc["held"], "held") if doc.get("held") is not None else ZERO,
    }


def check_deductions(salary: Decimal, deductions: list[dict[str, Any]], final: bool) -> list[dict[str, str]]:
    """FR-PAY-16: the rules a month's deductions break, each with its rule named (a blocking error)."""
    found: list[dict[str, str]] = []
    counted = ZERO
    for d in deductions:
        if d["kind"] == "other" and not d["consent"]:
            found.append({"key": d["key"], "rule": "consent"})
        if d["kind"] not in EXEMPT_FROM_HALF:
            counted += d["amount"]
        if d["is_advance"]:
            if final:
                if not d["consent"]:
                    found.append({"key": d["key"], "rule": "advance-final-consent"})
            else:
                if d["instalment"] > MAX_ADVANCE_INSTALMENTS:
                    found.append({"key": d["key"], "rule": "advance-instalments"})
                if d["amount"] > salary * QUARTER:
                    found.append({"key": d["key"], "rule": "advance-quarter"})
    if not final and counted > salary * HALF:
        found.append({"key": "", "rule": "half"})
    return found


def employee_month(person: dict[str, Any], month: date, facts: dict[str, Any], packs: dict[str, str], pay_day: date | None = None) -> dict[str, Any]:
    """One employee-month. Returns the figures as decimals, or ``{"error": "needs_year_to_date"}``."""
    pay_day = pay_day or last_day(month)
    cpf_id, sdl_id, shg_id = packs["cpf"], packs["sdl"], packs["shg"]
    items = facts["items"]
    classes = [wage_class(it, month, pay_day) for it in items]
    gross = sum((it["amount"] for it in items), ZERO)
    raw_ow = sum((it["amount"] for it, c in zip(items, classes) if c == ORDINARY), ZERO)
    raw_aw = sum((it["amount"] for it, c in zip(items, classes) if c == ADDITIONAL), ZERO)
    employee = person.get("employee", True)
    birth = date.fromisoformat(person["birth_date"])
    group = cpf_group(person, month)
    pays_aw = any(c == ADDITIONAL and it["amount"] != 0 for it, c in zip(items, classes))

    ow_subject = aw_subject = refund_wages = ZERO
    if group is not None:
        ow_subject = min(max(ZERO, raw_ow), _ceiling(cpf_id, "ow_monthly", month))
        ytd = facts["ytd"]
        if pays_aw and ytd is None:
            return {"error": "needs_year_to_date"}
        if ytd is not None:
            annual = _ceiling(cpf_id, "annual_salary", month)
            if facts["last_month"]:
                allowed = max(ZERO, annual - (ytd["ow"] + ow_subject))
                counted_now = min(ytd["aw_paid"] + max(ZERO, raw_aw), allowed)
                delta = counted_now - ytd["aw_subject"]
                aw_subject, refund_wages = (delta, ZERO) if delta >= 0 else (ZERO, -delta)
            else:
                estimate = ytd["ow"] + ow_subject * (13 - month.month)
                room = max(ZERO, annual - estimate - ytd["aw_subject"])
                aw_subject = min(max(ZERO, raw_aw), room)
        else:
            aw_subject = ZERO

    subject = ow_subject + aw_subject
    cpf_total = cpf_employee = ZERO
    refund_total = refund_employee = ZERO
    if group is not None:
        cpf_total, cpf_employee = cpf_split(cpf_id, group, birth, month, subject)
        if refund_wages > 0:
            refund_total, refund_employee = cpf_refund(cpf_id, group, birth, month, refund_wages)
    cpf_employer = cpf_total - cpf_employee

    sdl = sdl_employee(sdl_id, month, gross) if employee else ZERO
    shg = ZERO
    if employee and person.get("shg_fund") and not person.get("shg_opt_out"):
        shg = shg_fund_amount(shg_id, month, person["shg_fund"], gross)

    deductions_total = sum((d["amount"] for d in facts["deductions"]), ZERO)
    reimbursements = sum(facts["reimbursements"], ZERO)
    net = gross - cpf_employee - shg - deductions_total
    return {
        "gross": gross,
        "ow_subject": ow_subject,
        "aw_subject": aw_subject,
        "cpf_total": cpf_total,
        "cpf_employee": cpf_employee,
        "cpf_employer": cpf_employer,
        "sdl": sdl,
        "shg": shg,
        "deductions": deductions_total,
        "advances": sum((d["amount"] for d in facts["deductions"] if d["is_advance"]), ZERO),
        "reimbursements": reimbursements,
        "held": facts["held"],
        "net": net,
        "disburse": net + reimbursements - facts["held"],
        "employer_cost": gross + cpf_employer + sdl,
        "refund_wages": refund_wages,
        "refund_total": refund_total,
        "refund_employee": refund_employee,
        "violations": check_deductions(gross, facts["deductions"], facts["final"]),
    }


# --- the run -----------------------------------------------------------------------------------------------------------------------

FIGURES = ("gross", "ow_subject", "aw_subject", "cpf_total", "cpf_employee", "cpf_employer", "sdl", "shg", "deductions", "reimbursements",
           "held", "net", "disburse", "employer_cost", "refund_wages", "refund_total", "refund_employee")
DIFFERENCE = ("gross", "cpf_employee", "cpf_employer", "sdl", "shg", "deductions", "reimbursements", "net", "disburse")


def run_totals(months: list[dict[str, Any]], sdl_id: str) -> dict[str, Decimal]:
    """A run's totals, kept apart (FR-PAY-18); the SDL payable is the employers' month total rounded down once (``RND-SDL``) and is what
    the employer's cost and the entry carry."""
    def add(key: str) -> Decimal:
        return sum((m[key] for m in months), ZERO)

    payable = sdl_payable([m["sdl"] for m in months])
    return {
        "gross": add("gross"),
        "employee_cpf": add("cpf_employee"),
        "employer_cpf": add("cpf_employer"),
        "sdl_employees": add("sdl"),
        "sdl_payable": payable,
        "shg": add("shg"),
        "deductions": add("deductions"),
        "advances": add("advances"),
        "reimbursements": add("reimbursements"),
        "held": add("held"),
        "net": add("net"),
        "disburse": add("disburse"),
        "employer_cost": add("gross") + add("cpf_employer") + payable,
    }


def postings(t: dict[str, Decimal]) -> dict[str, dict[str, Decimal]]:
    """B.3's entry for a run's totals, by account: ``salary`` is the salary expense (by department in Fyniq), ``deductions`` the other
    deductions' own accounts."""
    lines = {
        "salary": {"debit": t["gross"], "credit": ZERO},
        "6020": {"debit": t["employer_cpf"], "credit": ZERO},
        "6030": {"debit": t["sdl_payable"], "credit": ZERO},
        "2150": {"debit": t["reimbursements"], "credit": ZERO},
        "1250": {"debit": ZERO, "credit": t["advances"]},
        "deductions": {"debit": ZERO, "credit": t["deductions"] - t["advances"]},
        "2100": {"debit": ZERO, "credit": t["disburse"] + t["held"]},
        "2110": {"debit": ZERO, "credit": t["employee_cpf"] + t["employer_cpf"] + t["sdl_payable"] + t["shg"]},
    }
    return lines


def balanced(lines: dict[str, dict[str, Decimal]]) -> bool:
    return sum((v["debit"] for v in lines.values()), ZERO) == sum((v["credit"] for v in lines.values()), ZERO)


def _shown(figures: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in figures.items():
        if key == "violations":
            out[key] = value
        elif key in FIGURES:
            out[key] = fmt(value)
    return out


def month_result(doc: dict[str, Any], person_doc: dict[str, Any], month: date, packs: dict[str, str], pay_day: date | None) -> dict[str, Any]:
    """One person of a run or one golden month: the shown figures, ``{"error": code}``, and the difference from ``posted`` when it has one."""
    done = employee_month(person_doc, month, _facts(doc), packs, pay_day)
    if "error" in done:
        return {"error": done["error"]}
    result = _shown(done)
    posted = doc.get("posted")
    if posted is not None:
        before = employee_month(person_doc, month, _facts(posted), packs, pay_day)
        if "error" not in before:
            result["difference"] = {k: fmt(done[k] - before[k]) for k in DIFFERENCE}
    return result


def employee_month_doc(doc: dict[str, Any]) -> dict[str, Any]:
    """A ``fyniq-payroll-month@1`` document worked out: the figures its ``expect`` names are the ones to compare."""
    problem = schema_error("payroll.schema.json", doc)
    if problem:
        raise PayrollError(problem)
    month = _first(doc["month"], "month")
    if month.day != 1:
        raise PayrollError("month: not the first day of a month")
    pay_day = _first(doc["pay_day"], "pay_day") if doc.get("pay_day") else None
    return month_result(doc, doc["person"], month, _pack_ids(doc), pay_day)


def payroll_run(doc: dict[str, Any]) -> dict[str, Any]:
    """A ``fyniq-payroll-run@1`` document: each person's employee-month, the run's totals and the entry that posts them."""
    problem = schema_error("payroll-run.schema.json", doc)
    if problem:
        raise PayrollError(problem)
    month = _first(doc["month"], "month")
    if month.day != 1:
        raise PayrollError("month: not the first day of a month")
    pay_day = _first(doc["pay_day"], "pay_day") if doc.get("pay_day") else None
    packs = _pack_ids(doc)
    people: list[dict[str, Any]] = []
    counted: list[dict[str, Any]] = []
    seen: set[str] = set()
    for entry in doc["people"]:
        if entry["id"] in seen:
            raise PayrollError(f"person {entry['id']} is listed twice: one employee-month holds every item of the month")
        seen.add(entry["id"])
        done = employee_month(entry["person"], month, _facts(entry), packs, pay_day)
        shown = month_result(entry, entry["person"], month, packs, pay_day)
        people.append({"id": entry["id"], **shown})
        if "error" not in done:
            counted.append(done)
    totals = run_totals(counted, packs["sdl"])
    lines = postings(totals)
    return {
        "schema": "fyniq-payroll-run-result@1",
        "people": people,
        "totals": {k: fmt(v) for k, v in totals.items() if k != "advances"},
        "postings": {k: {"debit": fmt(v["debit"]), "credit": fmt(v["credit"])} for k, v in lines.items()},
        "balanced": balanced(lines),
    }
