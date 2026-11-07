"""The oracle's driver-based forecast (PRD-001 FR-PLN-02, FR-PLN-03, RND-PLAN; p1-planning design D3, D4, D9).

Written from the rules, not from the Go engine: the workbook's model as the design states it, in exact rational
numbers (``Fraction``), with one rounding per line under RND-PLAN.

* A forecast is a ``fyniq-forecast@1`` document (test/differential/README.md, "Forecasts"): the drivers and plans,
  the books' figures at the start, the scenarios and the statutory figures, all given directly.
* Months 1 to H. Customers and ARPA are exact. A driver line (revenue, direct costs, payroll, other expenses,
  depreciation, grants, interest, a year's tax, collections, money raised and repaid) is rounded once, half away from
  zero, to the currency's minor units, worked from its drivers, the customers and the lines it is a percentage of as
  shown. Totals, profits, cash flows and balances add the rounded lines.
* An average, CAC, LTV and the funding gap are rounded once from their exact value; a percentage is RND-PERCENT
  (1 decimal); a ratio (LTV to CAC, the burn multiple) 2 decimals.
"""

from __future__ import annotations

from dataclasses import dataclass
from fractions import Fraction
from typing import Any

SCHEMA = "fyniq-forecast@1"
RESULT = "fyniq-forecast-result@1"
RAISED = ("Equity", "SAFE", "Loan")  # moved and scaled by the fundraising levers; a grant is not raised
FUNCTIONS = ("COGS", "R&D", "S&M", "G&A")


class ForecastError(ValueError):
    """A forecast document the oracle cannot read, or a horizon outside 12 to 60 months."""


def q(text: Any) -> Fraction:
    """An exact number from its decimal text (never a binary floating-point number)."""
    if isinstance(text, bool) or not isinstance(text, (str, int)):
        raise ForecastError(f"{text!r} is not a decimal string")
    return Fraction(text)


def half_away(value: Fraction, places: int) -> Fraction:
    """round(value, places), half away from zero."""
    scaled = value * 10**places
    units = (abs(scaled) + Fraction(1, 2)).__floor__()
    return Fraction(units if scaled >= 0 else -units, 10**places)


def text(value: Fraction, places: int | None = None) -> str:
    """A number as plain decimal text: at places decimals, or exactly when places is None (a terminating value)."""
    if places is not None:
        value = half_away(value, places)
    den = value.denominator
    twos = fives = 0
    while den % 2 == 0:
        den //= 2
        twos += 1
    while den % 5 == 0:
        den //= 5
        fives += 1
    if den != 1:
        raise ForecastError(f"{value} does not end as a decimal")
    digits = max(twos, fives, places or 0)
    units = value * 10**digits
    assert units.denominator == 1
    n = abs(units.numerator)
    s = str(n).rjust(digits + 1, "0")
    body = s if digits == 0 else s[:-digits] + "." + s[-digits:]
    return ("-" if units.numerator < 0 and n != 0 else "") + body


def month_index(month: str, start: str) -> int:
    """The forecast month of a "YYYY-MM": 1 for the start's own month."""
    y, m = (int(p) for p in month.split("-"))
    sy, sm = (int(p) for p in start.split("-"))
    return (y - sy) * 12 + (m - sm) + 1


def add_months(month: str, n: int) -> str:
    y, m = (int(p) for p in month.split("-"))
    total = y * 12 + (m - 1) + n
    return f"{total // 12:04d}-{total % 12 + 1:02d}"


@dataclass
class Statutory:
    """The figures the forecast takes from the packs, the same for every month and Year of Assessment given."""

    employer_cpf: Fraction
    ow_ceiling: Fraction
    sdl_rate: Fraction
    sdl_min: Fraction
    sdl_max: Fraction
    tax_rate: Fraction
    exemptions: dict[str, list[tuple[Fraction, Fraction]]]
    rebates: dict[int, tuple[Fraction, Fraction]]

    @staticmethod
    def read(doc: dict) -> Statutory:
        sdl = doc["sdl"]
        return Statutory(
            q(doc["employer_cpf"]), q(doc["ow_ceiling"]), q(sdl["rate"]), q(sdl["minimum"]), q(sdl["maximum"]),
            q(doc["tax_rate"]),
            {k: [(q(b), q(p)) for b, p in tiers] for k, tiers in doc["exemptions"].items()},
            {int(ya): (q(r["percent"]), q(r["cap"])) for ya, r in doc.get("rebates", {}).items()},
        )

    def sdl(self, wages: Fraction) -> Fraction:
        if wages <= 0:
            return Fraction(0)
        return min(self.sdl_max, max(self.sdl_min, wages * self.sdl_rate))

    def exempt(self, scheme: str, chargeable: Fraction) -> Fraction:
        out, left = Fraction(0), max(Fraction(0), chargeable)
        for band, percent in self.exemptions[scheme]:
            part = min(left, band)
            out += part * percent
            left -= part
        return out

    def rebate(self, ya: int, tax: Fraction) -> Fraction:
        if ya not in self.rebates:
            return Fraction(0)
        percent, cap = self.rebates[ya]
        return min(max(tax, Fraction(0)) * percent, cap)


def _levers(doc: dict) -> dict[str, Fraction | int]:
    out: dict[str, Fraction | int] = {}
    for k in ("new_customers", "churn", "price", "services", "opex", "direct_costs", "fund_amount"):
        out[k] = q(doc[k])
    out["hire_delay"] = int(doc["hire_delay"])
    out["fund_delay"] = int(doc["fund_delay"])
    return out


def run(doc: dict, scenario: dict, with_funding: bool) -> dict[str, Any]:
    """One scenario of a forecast, with or without its funding plan."""
    horizon = int(doc["horizon"])
    if not 12 <= horizon <= 60:
        raise ForecastError(f"a horizon of {horizon} months is outside 12 to 60")
    places = int(doc["minor_units"])
    start = doc["start"]
    st = Statutory.read(doc["statutory"])
    lv = _levers(scenario["levers"])
    rev_in, costs = doc["revenue"], doc["costs"]
    books = doc["books"]
    buffer = q(scenario["buffer_months"]) if scenario.get("buffer_months") is not None else q(doc["buffer_months"])

    def money(x: Fraction) -> Fraction:
        return half_away(x, places)

    # The plans, placed in forecast months.
    people = []
    for h in doc.get("headcount", []):
        begins = month_index(h["start"], start)
        if begins > 1:
            begins += lv["hire_delay"]  # a planned hire waits; the team already there does not
        ends = month_index(h["end"], start) if h.get("end") else None
        people.append((h["function"], begins, ends, q(h["salary"]), bool(h["cpf"])))
    funding = []
    for f in doc.get("funding", []) if with_funding else []:
        when = max(1, month_index(f["month"], start))
        amount = q(f["amount"])
        if f["type"] in RAISED:
            when += lv["fund_delay"]
            amount *= lv["fund_amount"]
        funding.append((f["type"], when, amount, q(f.get("rate") or "0"), max(1, int(f.get("tenor") or 1))))
    capex = [(max(1, month_index(c["month"], start)), q(c["amount"]), int(c["life"])) for c in doc.get("capex", [])]
    existing_dep = [q(x) for x in books.get("depreciation", [])]
    opex_lines = doc.get("opex", [])

    raise_rate = q(costs["salary_increase"])

    def payroll(function: str, n: int, year: int) -> Fraction:
        total = Fraction(0)
        for fn, begins, ends, salary, cpf in people:
            if fn != function or salary == 0 or n < begins or (ends is not None and n > ends):
                continue
            pay = salary * (1 + raise_rate) ** (year - 1)
            employer = min(pay, st.ow_ceiling) * st.employer_cpf if cpf else Fraction(0)
            total += pay + employer + st.sdl(pay)
        return total

    churn = q(rev_in["churn"]) * lv["churn"]
    months: list[dict[str, Any]] = []
    revenue_of: dict[int, Fraction] = {}
    customers = q(rev_in["customers"])
    for n in range(1, horizon + 1):
        year = (n - 1) // 12 + 1
        opening = customers
        new = q(rev_in["new_customers"]) * (1 + q(rev_in["new_growth"])) ** (n - 1) * lv["new_customers"]
        lost = opening * churn
        customers = opening + new - lost
        arpa = q(rev_in["arpa"]) * (1 + q(rev_in["price_increase"])) ** (year - 1) * lv["price"]

        subscription = money(customers * arpa)
        services = money(q(rev_in["services"]) * (1 + q(rev_in["services_growth"])) ** (n - 1) * lv["services"])
        other = money(q(rev_in["other"]))
        revenue = subscription + services + other
        revenue_of[n] = revenue

        direct = lv["direct_costs"]
        hosting = money(subscription * q(costs["hosting"]) * direct)
        ai = money(revenue * q(costs["ai"]) * direct)
        contractors = money(services * q(costs["contractors"]) * direct)
        fees = money(revenue * q(costs["fees"]) * direct)
        cogs_pay = money(payroll("COGS", n, year))
        direct_costs = hosting + ai + contractors + fees + cogs_pay
        gross = revenue - direct_costs
        rd, sm, ga = (money(payroll(f, n, year)) for f in ("R&D", "S&M", "G&A"))
        staff = rd + sm + ga
        headcount = sum(1 for _, b, e, _, _ in people if b <= n and (e is None or n <= e))
        opex = [
            money((q(o["base"]) * (1 + q(o["growth"])) ** (n - 1) + q(o["percent"]) * revenue) * lv["opex"])
            for o in opex_lines
        ]
        opex_total = sum(opex, Fraction(0))
        ebitda = gross - staff - opex_total

        dep = existing_dep[n - 1] if n - 1 < len(existing_dep) else Fraction(0)
        dep += sum((amt / life for when, amt, life in capex if life >= 1 and when <= n <= when + life - 1), Fraction(0))
        depreciation = money(dep)
        grants = money(sum((a for kind, when, a, _, _ in funding if kind == "Grant" and when == n), Fraction(0)))
        # interest on what is left of each loan at the start of the month: equal repayments from the month after
        interest = money(
            sum(
                (
                    amount * Fraction(tenor - (n - when - 1), tenor) * rate / 12
                    for kind, when, amount, rate, tenor in funding
                    if kind == "Loan" and when < n <= when + tenor
                ),
                Fraction(0),
            )
        )
        pbt = ebitda - depreciation + grants - interest
        months.append(
            dict(
                n=n, date=add_months(start, n - 1), year=year,
                customers_open=opening, new_customers=new, lost_customers=lost, customers_close=customers, arpa=arpa,
                subscription=subscription, services=services, other=other, revenue=revenue,
                hosting=hosting, ai=ai, contractors=contractors, fees=fees, payroll_cogs=cogs_pay,
                direct_costs=direct_costs, gross_profit=gross,
                payroll_rd=rd, payroll_sm=sm, payroll_ga=ga, payroll=staff, headcount=headcount,
                opex=opex, opex_total=opex_total, ebitda=ebitda, depreciation=depreciation, grants=grants,
                interest=interest, profit_before_tax=pbt,
            )
        )

    # Tax by 12-month forecast year, for the YA after the calendar year the forecast year ends in.
    taxes = []
    losses = q(doc["tax"]["losses_brought_forward"])
    start_up_years = int(doc["tax"]["start_up_years"])
    for year in range(1, (horizon + 11) // 12 + 1):
        pbt = sum((m["profit_before_tax"] for m in months if m["year"] == year), Fraction(0))
        ya = int(add_months(start, 12 * year - 1)[:4]) + 1
        scheme = "sute" if year <= start_up_years else "pte"
        chargeable = max(Fraction(0), pbt - losses)
        exempt = st.exempt(scheme, chargeable)
        gross_tax = (chargeable - exempt) * st.tax_rate
        rebate = st.rebate(ya, gross_tax)
        tax = money(max(Fraction(0), gross_tax - rebate))
        carried = max(Fraction(0), losses - max(Fraction(0), pbt)) + max(Fraction(0), -pbt)
        taxes.append(
            dict(year=year, ya=ya, scheme=scheme, profit_before_tax=pbt, losses_brought_forward=losses,
                 chargeable=chargeable, exempt=exempt, taxable=chargeable - exempt, gross_tax=gross_tax,
                 rebate=rebate, tax=tax, losses_carried_forward=carried)
        )
        losses = carried

    collection = [q(x) for x in rev_in["collection"]]
    lag = int(doc["tax"]["payment_lag"])
    cash = q(books["cash"]) + q(doc.get("cash_adjustment") or "0")
    opening_cash = cash
    for m in months:
        n = m["n"]
        m["tax"] = taxes[m["year"] - 1]["tax"] if (n % 12 == 0 or n == horizon) else Fraction(0)
        m["net_profit"] = m["profit_before_tax"] - m["tax"]
        owed = {1: Fraction(6, 10), 2: Fraction(4, 10)}.get(n, Fraction(0)) * q(books["receivables"])
        m["collections"] = money(
            sum((collection[k] * revenue_of.get(n - k, Fraction(0)) for k in range(3)), Fraction(0)) + owed
        )
        m["suppliers"] = -(m["hosting"] + m["ai"] + m["contractors"] + m["fees"] + m["opex_total"]) - (
            q(books["payables"]) if n == 1 else Fraction(0)
        )
        m["payroll_paid"] = -(m["payroll"] + m["payroll_cogs"])
        m["grants_received"] = m["grants"]
        m["interest_paid"] = -m["interest"]
        paid_year = (n - lag) // 12 if n > lag and (n - lag) % 12 == 0 else None
        m["tax_paid"] = -taxes[paid_year - 1]["tax"] if paid_year and paid_year <= len(taxes) else Fraction(0)
        m["operating"] = sum(
            (m[k] for k in ("collections", "suppliers", "payroll_paid", "grants_received", "interest_paid", "tax_paid")),
            Fraction(0),
        )
        m["capex_paid"] = -money(sum((a for when, a, _ in capex if when == n), Fraction(0)))
        m["equity"] = money(sum((a for k, w, a, _, _ in funding if k in ("Equity", "SAFE") and w == n), Fraction(0)))
        m["loans_drawn"] = money(sum((a for k, w, a, _, _ in funding if k == "Loan" and w == n), Fraction(0)))
        m["loan_repayments"] = -money(
            sum((a / t for k, w, a, _, t in funding if k == "Loan" and w < n <= w + t), Fraction(0))
        )
        m["net_cash_flow"] = m["operating"] + m["capex_paid"] + m["equity"] + m["loans_drawn"] + m["loan_repayments"]
        m["cash_open"] = cash
        cash += m["net_cash_flow"]
        m["cash_close"] = cash
        m["net_burn"] = max(Fraction(0), -(m["operating"] + m["capex_paid"]))
        m["arr"] = m["subscription"] * 12
        sales_line = next((i for i, o in enumerate(opex_lines) if o.get("group") == "sales_marketing"), None)
        m["sales_marketing"] = m["payroll_sm"] + (m["opex"][sales_line] if sales_line is not None else Fraction(0))

    for i, m in enumerate(months):
        rev, gp = m["revenue"], m["gross_profit"]
        m["gross_margin"] = gp / rev if rev else None
        m["ltv"] = m["arpa"] * (gp / rev) / churn if rev and churn else None
        m["cac"] = m["sales_marketing"] / m["new_customers"] if m["new_customers"] else None
        m["ltv_to_cac"] = m["ltv"] / m["cac"] if m["ltv"] is not None and m["cac"] else None
        growth = m["arr"] - months[i - 1]["arr"] if i else None
        m["burn_multiple"] = m["net_burn"] / growth if growth is not None and growth > 0 else None

    negative = [m for m in months if m["cash_close"] < 0]
    profitable = [m for m in months if m["revenue"] > 0 and m["ebitda"] >= 0]
    lowest = min(m["cash_close"] for m in months)
    burn12 = sum((m["net_burn"] for m in months[:12]), Fraction(0))
    key = dict(
        cash_out_month=negative[0]["date"] if negative else None,
        runway_months=negative[0]["n"] - 1 if negative else horizon,
        runway_beyond=not negative,
        minimum_cash=lowest,
        minimum_month=next(m["date"] for m in months if m["cash_close"] == lowest),
        break_even=profitable[0]["date"] if profitable else None,
        average_burn=money(burn12 / 12),
        funding_gap=money(max(Fraction(0), buffer * burn12 / 12 - lowest)),
        revenue_next_12=sum((m["revenue"] for m in months[:12]), Fraction(0)),
        arr_end=months[-1]["arr"],
        customers_end=months[-1]["customers_close"],
    )
    years = []
    for year in range(1, 6):
        row: dict[str, Any] = {"year": year}
        inside = [m for m in months if m["year"] == year]
        for k in ("revenue", "gross_profit", "ebitda", "net_profit", "net_burn"):
            row[k] = sum((m[k] for m in inside), Fraction(0)) if inside else None
        end = months[12 * year - 1] if 12 * year <= horizon else None
        for k, src in (("cash", "cash_close"), ("customers", "customers_close"), ("arr", "arr"), ("headcount", "headcount")):
            row[k] = end[src] if end else None
        years.append(row)
    return dict(
        scenario=scenario["name"], funding=with_funding, opening_cash=opening_cash,
        collections_ok=sum(collection) == 1, months=months, taxes=taxes, years=years, key=key,
    )


_MONTH_RATIOS = {"cac": 2, "ltv": 2, "ltv_to_cac": 2, "burn_multiple": 2}


def render(result: dict[str, Any], places: int) -> dict[str, Any]:
    """A run as fyniq-forecast-result@1 JSON: amounts at minor units, exact counts in full, ratios rounded."""

    def value(k: str, v: Any) -> Any:
        if v is None or isinstance(v, (bool, str)) or (isinstance(v, int) and not isinstance(v, Fraction)):
            return v
        if isinstance(v, list):
            return [value(k, x) for x in v]
        if k == "gross_margin":
            return text(v * 100, 1)
        if k in _MONTH_RATIOS:
            return text(v, _MONTH_RATIOS[k])
        if k in ("customers_open", "new_customers", "lost_customers", "customers_close", "arpa", "customers",
                 "customers_end", "exempt", "taxable", "gross_tax", "rebate", "chargeable",
                 "losses_brought_forward", "losses_carried_forward", "profit_before_tax"):
            return text(v)
        return text(v, places)

    def block(d: dict[str, Any]) -> dict[str, Any]:
        return {k: value(k, v) for k, v in d.items()}

    return {
        **{k: value(k, v) for k, v in result.items() if k not in ("months", "taxes", "years", "key")},
        "months": [block(m) for m in result["months"]],
        "taxes": [block(t) for t in result["taxes"]],
        "years": [block(y) for y in result["years"]],
        "key": block(result["key"]),
    }


def forecast(doc: dict) -> dict[str, Any]:
    """Every scenario of a forecast document, each with and without its funding plan."""
    from oracle.scenario import schema_error

    problem = schema_error("forecast.schema.json", doc)
    if problem is not None:
        raise ForecastError(problem)
    places = int(doc["minor_units"])
    runs = []
    for scenario in doc["scenarios"]:
        for with_funding in (True, False):
            runs.append(render(run(doc, scenario, with_funding), places))
    return {"schema": RESULT, "name": doc.get("name", ""), "runs": runs}
