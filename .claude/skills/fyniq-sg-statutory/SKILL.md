---
name: fyniq-sg-statutory
description: Keep the Singapore statutory parameters of Fyniq correct, as effective-dated, versioned parameter packs (packs/, named <pack>@<year>.<n>) with official sources, dual approval and hand-pinned tests. Covers GST (rate, codes, F5 mapping, registration threshold), InvoiceNow, CPF (rates by residency, age band and PR year, OW and AW ceilings, graduated rates, rounding), SDL, SHG funds, levies, corporate income tax (SUTE/PTE, rebates, YA packs) and the statutory calendar (GST F5, CPF, AIS/IR8A, IR21, ECI, Form C-S/C, ACRA). Use every January, after each Singapore Budget, whenever an official page changes, and when someone says "update for 2027", "the CPF rate changed", "a payroll or GST figure looks wrong", "add a GST code", "new deadline", "InvoiceNow phase-in" or "add another country".
---

# Singapore statutory packs

Statutory values change every year: CPF rates and ceilings, CIT rebates, InvoiceNow phases,
deadlines. The workbook held one value per parameter, so a change recalculated closed months.
Fyniq stores every statutory value as **data in an effective-dated, versioned pack**. The
engine picks the version in force at each document's tax point or pay month (ENG-30/31).
Changing a future rate never changes the past.

> **Status:** no code exists yet; P0/P1 build it. The paths and commands below are the contract
> (PRD §10.6, §12.4, §12.10). Keep this skill in step when the code lands.

## Principle: official sources only

Never take a rate from memory, a blog or a model's answer. For every value, open the official
page, note the **effective date** and the **retrieval date**, and record the URL in the pack. If
a source is unreachable or ambiguous, **stop and ask**: a wrong CPF rate silently misstates every
payslip.

| Topic | Official source (search these titles on the site) |
|---|---|
| GST rate, registration, filing, F5, tax invoices, bad-debt relief | IRAS (iras.gov.sg): "Current GST rates", "Do I need to register for GST", "Filing GST", "Invoicing customers", "Claiming bad debt relief" |
| InvoiceNow | IRAS: "GST InvoiceNow Requirement"; IMDA: InvoiceNow, accredited Access Points |
| Corporate income tax, SUTE/PTE, rebates, ECI, Form C-S/C | IRAS: "Corporate Income Tax Rate, Rebates & Tax Exemption Schemes"; the year's Budget statement |
| CPF rates, OW and AW ceilings, rounding, due dates | CPF Board (cpf.gov.sg): "CPF contribution rates" tables, "Ordinary and Additional Wages", "How much CPF contributions to pay" |
| SDL | CPF Board / SkillsFuture Singapore: "Skills Development Levy" |
| SHG funds (CDAC, ECF, MBMF, SINDA) | CPF Board: "Self-Help Group Funds" |
| Foreign worker levies, payslips, KETs | MOM (mom.gov.sg) |
| AIS / IR8A, IR21 | IRAS: "Auto-Inclusion Scheme", "Tax clearance for non-citizen employees (IR21)" |
| Annual return, AGM, XBRL, register of controllers | ACRA (acra.gov.sg) |

## Packs

| Pack | Holds | Selected by |
|---|---|---|
| `sg-gst` | Rate history, GST codes with direction and standard-rated flag, F5 box mapping, registration threshold and test rules | Document tax point |
| `sg-invoicenow` | Phase-in schedule by registrant class and turnover band; in-scope document types | Registration date and class |
| `sg-cpf` | Rates by residency (SC, SPR year 1/2/3+), age band and wage band; graduated formulae for S$50–750; OW ceiling; annual salary ceiling (AW ceiling = ceiling − OW subject to CPF YTD); rounding rule | Pay month |
| `sg-sdl` | Rate, minimum, maximum | Pay month |
| `sg-shg` | Tables per fund by wage band; opt-out rules | Pay month |
| `sg-levy` | Foreign worker levy rates by pass type and tier | Month |
| `sg-cit` | Rate, SUTE/PTE tiers and conditions, rebate and cap per YA, Form C-S / C-S Lite thresholds | Year of Assessment |
| `sg-calendar` | Rules that generate deadlines from the FY, GST periods and payroll (CMP-02) | Organisation settings |

**Layout** (planned): `packs/<pack>/<year>.<n>/`:

```
packs/sg-cpf/2027.1/
  pack.yaml        # id: sg-cpf@2027.1, effective_from, effective_to, supersedes, verifier, approver
  rates.csv        # data tables (decimals as strings, NUMERIC(9,6) on load)
  SOURCES.md       # every value → official URL, page date, retrieval date, quoted line
  pinned_test.yaml # hand-calculated cases for this version (QA-ENG-08)
```

- **Name:** `<pack>@<year>.<n>`: `sg-cpf@2027.1` is the first 2027 version; a Budget-day
  correction becomes `sg-cpf@2027.2`.
- **Never edit an activated version.** Corrections are a new version with an `effective_from`.
- Each version records the source URL, retrieval date, verifier and approver (ENG-30). Activation
  needs **dual approval** (SOD-10) and green pinned tests for its effective date (ENG-32,
  CTL-FIN-06).
- Packs are released independently of the platform (§12.10). Each organisation sees a
  "parameter update available" task with a diff (ENG-33).

**ACRA pack** (rev 0.6): `sg-acra@<year>.<n>` holds the corporate deadlines and rules that the
AI company secretary and the checks read (FR-CS-02): AGM and financial-statement timing, the
annual return, changes of officers, registered office and controllers, share-issue approvals and
their lapse, the exempt-private-company and XBRL filing categories, and audit exemption. The
agent never states a deadline from memory (`agents/company-secretary/SKILL.md`). Source notes are
in `docs/prd/research/`.

## Routine: January, Budget day, and whenever a source changes

1. **Collect** the new values with their sources and effective dates. Summarise anything
   ambiguous for the user before writing a pack.
2. **Write a new pack version** (never edit the old one); fill `SOURCES.md` for every value.
3. **Pin the tests by hand** (QA-ENG-08). Derive the expected payslips and GST amounts from the
   **official tables**, independently of the pack data. Cover every age band, both PR graduated
   years, the S$50–750 graduated range, the OW ceiling, the AW ceiling across a year, a birthday
   month transition, SDL minimum and maximum, and each SHG band. Cross-check at least one case
   with the CPF Board's contribution calculator.
4. **Check the logic still fits the law.** A new F5 box, a new GST code (e.g. reverse charge), a
   new CPF rule or a new InvoiceNow class is a **code** change too: follow
   **fyniq-accounting-domain**, with a new rule version.
5. **Update the statutory calendar rules** if any deadline moved, and the Checks C.2 #13–16
   thresholds.
6. **Run** `make test` (pinned tests and the differential tests on golden data; arrives in P0).
   CI fails when the current year's pack is missing, unsourced or fails its pinned tests
   (CTL-FIN-06), and in January when the CPF tables for the new year are missing.
7. **Update the PRD** when a fact in §14.2 changes (it is a dated snapshot), and remove `[verify]`
   tags you have now verified.
8. **Commit:** `feat(packs): sg-cpf@2027.1 — 2027 CPF rates and ceilings`. The body lists each
   changed value with its source and effective date. Under **Upgrade notes:** say whether
   organisations must accept the update task, and which payslips or returns will differ.
   release-please writes the version and the CHANGELOG; never edit them by hand.

## Facts as of 12 Oct 2026 (snapshot from PRD §14.2; re-verify before use)

| Area | Value |
|---|---|
| GST | 9% since 1 Jan 2024 (8% in 2023); registration threshold S$1m (retrospective by calendar year; prospective test); voluntary registrants stay ≥ 2 years; F5 due 1 month after the period end |
| Tax invoices | Within 30 days; simplified up to S$1,000; GST per line or per subtotal, **consistently**; foreign-currency invoices show net, GST and gross in SGD |
| InvoiceNow | Mandatory for new voluntary registrants from 1 Apr 2026; new compulsory and existing registrants ≤ S$200k from 1 Apr 2028; ≤ S$1m 2029; ≤ S$4m 2030; all others 2031. Covers sales **and** purchases. **Must be live before Cognizhi registers voluntarily.** F5 filing waits until every in-scope return item has an **IRAS acknowledgement ID** (proof of receipt) or a recorded exclusion, with a dual-approved contingency for outages (FR-TAX-10/11, PRD rev 0.8). |
| CPF 2026 | ≤55: 17/20; 55–60: 16/18; 60–65: 12.5/12.5; 65–70: 9/7.5; >70: 7.5/5 (employer/employee %); OW ceiling S$8,000/month; annual salary ceiling S$102,000; due at month end, pay by the 14th |
| SDL | 0.25% of remuneration, min S$2, max S$11.25, all employees |
| CIT | 17%; SUTE and PTE tiers; YA2026 rebate 50% capped at S$40k incl. a S$2k cash grant (conditions); ECI within 3 months of FY end; Form C-S/C by 30 Nov |
| Payroll records | Itemised payslips (12 items) within 3 working days; KETs within 14 days; AIS by 1 March from 5 employees; IR21 ≥ 1 month before cessation |
| Shares and directors' balances (rev 0.3) | Issuing shares needs approval in general meeting (s161; a written resolution for private companies), lodged within 14 days; a private company's allotment takes effect when ACRA updates the register of members, so it cannot be backdated (the 14-day return in s63A is for public companies); shares not paid in cash need the contract or a statement of particulars (s63B); s162 bans loans to directors except in exempt private companies; salary is taxed when due, director's fees when the director becomes entitled; CPF is due at month end on wages due, none on fees voted at a general meeting. Details: PRD §8.22, §14.2 and `docs/prd/research/founders-money-sources-2026-10-12.md` |

## Pinned payroll seed (Appendix G.2; 2026 rates, from the workbook)

| Employee | Gross | EE CPF | ER CPF | SDL | Net |
|---|---|---|---|---|---|
| Age ≤ 55, Mar | 7,600.00 | 1,520 | 1,292 | 11.25 | 6,077 |
| Age 55–60 | 5,000.00 | 900 | 800 | 11.25 | 4,100 |
| Age 60–65, PR | 4,000.00 | 500 | 500 | 10.00 | 3,500 |
| Age 65–70 | 3,000.00 | 225 | 270 | 7.50 | 2,775 |
| Age > 70 | 2,500.00 | 125 | 188 | 6.25 | 2,375 |
| Foreigner | 9,000.00 | 0 | 0 | 11.25 | 9,000 |
| OW 8,000 cap + 5,000 bonus | 15,000.00 | 2,600 | 2,210 | 11.25 | 12,400 |

The workbook's "wage 700" row did **not** apply graduated rates. Fyniq does (FR-PAY-02), so
re-derive that row by hand from the CPF Board's graduated table before pinning it. Do not copy the
workbook value.

## Other countries (FR-TAX-09)

The engine is country-neutral. A country is a set of new packs (`my-sst`, `my-epf`, …), its tax
codes and return formats, its payroll calculator with pinned tests, and its statutory calendar
rules. Do not add country branches in engine code.

## Definition of done

- [ ] A new pack version (no edit of an activated one), with `SOURCES.md` for every value.
- [ ] Pinned tests derived by hand from the official tables, green for the effective date.
- [ ] Code changes (new code, box or rule) went through **fyniq-accounting-domain** with a new rule version.
- [ ] Calendar and Checks thresholds updated; PRD §14.2 snapshot refreshed.
- [ ] Dual approval recorded for activation; a Conventional Commit with upgrade notes.
