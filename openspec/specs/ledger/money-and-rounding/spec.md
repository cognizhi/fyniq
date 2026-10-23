# ledger/money-and-rounding Specification

## Purpose
How Fyniq keeps money, rates and quantities exact from input to export, where the input precision boundary lies, and the named rounding and allocation policies through which every computed amount reaches its currency's minor unit. Derived from PRD-001 §10.3 and §10.4.

## Requirements

### Requirement: ENG-10 No binary floating point for money
The system SHALL use no binary floating point for money, rates or quantities anywhere: database, services, API, front end and exports.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Float types are blocked in money code
- **WHEN** code in a money code path uses a float type
- **THEN** static analysis and lint rules block it
- **AND** a CI test fails on any float in the money modules

#### Scenario: XLSX exports are written from exact decimals
- **WHEN** amounts are exported to XLSX
- **THEN** every amount is written from its exact decimal
- **AND** the export includes a data tab of exact values
- **AND** it shows the engine's stored totals as values

#### Scenario: Values beyond 15 significant digits
- **GIVEN** a numeric XLSX cell becomes binary floating point inside the spreadsheet program, which keeps 15 significant digits
- **WHEN** an exported value has more than 15 significant digits
- **THEN** it is written as text with a note

#### Scenario: Live formulas are never the source of a figure
- **WHEN** an export contains live formulas
- **THEN** they are a convenience only and never the source of a figure

#### Scenario: CSV and JSON exports
- **WHEN** amounts are exported to CSV or JSON
- **THEN** they are exact decimal strings

#### Scenario: G4-014 Large values survive an XLSX round trip
- **GIVEN** a ledger containing 9,999,999,999,999.99 (15 significant digits) and 99,999,999,999,999.99 (16)
- **WHEN** it is exported to XLSX and re-imported
- **THEN** the first survives as a number
- **AND** the second is written as text with a note and survives exactly
- **AND** the stored totals in the sheet equal the engine's

### Requirement: ENG-11 Money is an exact decimal with a currency
Money SHALL be an exact decimal paired with an ISO 4217 currency code and stored at the currency's minor-unit precision; FX rates, quantities and unit prices SHALL be stored at their own stated precision.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Minor-unit precision per currency
- **WHEN** an amount is stored
- **THEN** it is at its currency's minor-unit precision (SGD 2, JPY 0, KWD 3)

#### Scenario: Computed and entered amounts
- **WHEN** an amount is computed
- **THEN** it reaches the minor-unit precision through a named rounding policy
- **AND** an entered amount must already be at that precision (ENG-44)

#### Scenario: FX rates keep at least 10 decimal places
- **WHEN** an FX rate is stored
- **THEN** it keeps at least 10 decimal places
- **AND** a rate below 0.001 therefore keeps fewer than 10 significant digits (open question 30)

#### Scenario: FX rates are base per one unit
- **WHEN** an FX rate is stored
- **THEN** it is base-currency units per one unit of the line's currency, and a manual journal's rate is entered that way, or left out to take the cascade's (rev 0.71, ENG-40)
- **AND** a rate quoted the other way or per 100 units is converted when it is stored (`RND-RATE`), and the original quote is kept beside it

#### Scenario: Quantities and unit prices
- **WHEN** a quantity or unit price is stored
- **THEN** it keeps up to 6 decimal places

### Requirement: ENG-12 Money crosses the API as decimal strings
Money SHALL cross the API as decimal strings (e.g. `"1234.50"`) with its currency, never as JSON numbers.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Amount in an API request or response
- **WHEN** an amount is sent to or returned by the API
- **THEN** it is a decimal string such as `"1234.50"` with its currency
- **AND** it is never a JSON number

### Requirement: ENG-44 Input precision boundary
The system SHALL parse amounts, rates and quantities exactly from their decimal strings (ENG-12) and check them before any rounding or database coercion, rejecting invalid values with a field error and never rounding them silently.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Checked before rounding or coercion
- **WHEN** an amount, rate or quantity is received
- **THEN** it is parsed exactly from its decimal string
- **AND** it is checked before any rounding or database coercion

#### Scenario: Too many decimal places
- **WHEN** an amount has more decimal places than its currency's minor units
- **THEN** it is rejected with a field error (FR-INT-07, ENG-23)
- **AND** it is never rounded silently

#### Scenario: Out of range or not a plain decimal
- **WHEN** a value is outside its column's range, or is `NaN`, `Infinity` or in exponent notation
- **THEN** it is rejected with a field error (FR-INT-07, ENG-23)

#### Scenario: Decimal places counted as written
- **WHEN** an amount is written with more decimal places than its currency's minor units, even zeros (`"12345.0"` for JPY)
- **THEN** it is rejected with a field error
- **AND** `"100"` for USD is accepted, and the money-string pattern for each currency states its limit (FR-INT-13)

#### Scenario: Computed base amount out of range
- **WHEN** a computed base amount falls outside its column's range, although the amount and the rate each fit
- **THEN** the document is rejected with a field error on the line (ENG-23)
- **AND** the base amount is never truncated

#### Scenario: Rounding only inside a named policy
- **WHEN** a value is rounded
- **THEN** it is a computed value rounded inside a named policy (§10.4)
- **AND** each policy is code, is tested and is referenced by the entries it produces, and "half-up" in a policy means half away from zero (§10.4)

#### Scenario: JSON Schema for money strings
- **WHEN** the JSON Schema for money strings is published
- **THEN** it states the pattern and the maximum digits (FR-INT-13)

#### Scenario: Imports and pasted values follow the same rule
- **WHEN** values arrive by import (FR-MIG-03, FR-BNK-02, budget XLSX) or are pasted (FR-UX-20)
- **THEN** they are parsed and checked by the same rule

#### Scenario: G4-012 Over-precise SGD amount on every channel
- **WHEN** an SGD amount `"1.230001"` is sent to the API, imported by CSV and pasted into a grid
- **THEN** it is rejected with a field error naming the amount and the 2-decimal limit
- **AND** nothing is stored

### Requirement: ENG-13 No arithmetic across currencies without conversion
Arithmetic on money of different currencies SHALL be a type error unless an explicit conversion, with rate and source, is applied.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Different currencies without a conversion
- **WHEN** code does arithmetic on money of two currencies without a conversion
- **THEN** it is a type error

#### Scenario: Explicit conversion
- **WHEN** one amount is first converted with an explicit rate and its source
- **THEN** arithmetic with the converted amount is allowed

### Requirement: ENG-14 Base amounts computed once
Base-currency amounts SHALL be computed once at posting and stored, and reports SHALL never re-convert historical entries.

Source: PRD-001 §10.3 · Priority M · Phase P0

#### Scenario: Base amount stored at posting
- **WHEN** a line is posted
- **THEN** its base-currency amount is computed and stored with it

#### Scenario: Reports use the stored base amounts
- **WHEN** a report covering historical entries is produced
- **THEN** it uses the stored base amounts
- **AND** it does not re-convert the entries

#### Scenario: A reversal negates the stored base amounts
- **WHEN** an entry is reversed
- **THEN** the reversal's base amounts are the entry's stored base amounts, negated (FR-GL-12)
- **AND** they are not re-converted

### Requirement: RND-FX Foreign amount to base currency
A base amount converted from a foreign amount SHALL be rounded to the base currency's minor units, half-up (rule from the workbook, `ROUND(amt×fx,2)`).

Source: PRD-001 §10.4

#### Scenario: Conversion formula
- **WHEN** a foreign amount is converted at a rate
- **THEN** base amount = round(foreign amount × rate, base minor units), half-up

### Requirement: RND-RATE Rate quotes stored as base per one unit
A rate quoted per 100 units, or as foreign currency per unit of the base currency, SHALL be converted when it is stored to base currency per one unit of the foreign currency, rounded to 10 decimal places half-up, with the original quote kept beside it. It applies from P1, when rates come from the rate feed (ENG-40, FR-GL-05).

Source: PRD-001 §10.4

#### Scenario: Quote per n units of the foreign currency
- **WHEN** a rate is quoted as base currency per n units of the foreign currency (n = 1 or 100)
- **THEN** it is stored as round(quote ÷ n, 10), half-up

#### Scenario: Quote the other way
- **WHEN** a rate is quoted as foreign currency per one unit of the base currency
- **THEN** it is stored as round(1 ÷ quote, 10), half-up

#### Scenario: Every stored rate is checked against its quote (rev 0.70)
- **WHEN** a rate of the feed or of an organisation is stored
- **THEN** its quote and its form (base-per-unit, base-per-100, foreign-per-base) are kept beside it
- **AND** the database refuses a stored rate that is not its quote under `RND-RATE`

#### Scenario: The original quote is kept
- **WHEN** a converted rate is stored
- **THEN** the original quote and its form are kept beside it
- **AND** the stored rate is within the error bound of open question 30

#### Scenario: A manual journal's rate is entered base per unit
- **WHEN** a manual journal's rate is entered
- **THEN** it is entered as base currency per one unit of the line's currency (ENG-11), so no conversion applies

### Requirement: RND-GST-EXCL GST on a document
GST on a document SHALL be rounded to the cent, half-up, computed per subtotal by default or per line as an organisation setting, applied consistently (rules from the workbook and IRAS: per line or per subtotal if done consistently, the total rounded to the nearest cent).

Source: PRD-001 §10.4

#### Scenario: Per subtotal by default
- **GIVEN** the organisation has not chosen per-line computation
- **WHEN** GST on a document is computed
- **THEN** GST = round(net base amount per GST code × rate, 2), half-up
- **AND** each GST code is computed once per invoice

#### Scenario: Per line as an organisation setting
- **GIVEN** the organisation has chosen per-line computation
- **WHEN** GST on a document is computed
- **THEN** it is computed per line

#### Scenario: Applied consistently
- **WHEN** documents are posted
- **THEN** the chosen method is applied consistently to all of them

#### Scenario: Nearest cent, not five cents
- **WHEN** the GST total is rounded
- **THEN** it is rounded to the nearest cent
- **AND** 5-cent rounding applies only to cash payment

### Requirement: RND-GST-INCL GST extracted from a GST-inclusive amount
GST extracted from a GST-inclusive bank amount SHALL be rounded to the cent, half-up, and the net SHALL be the gross less that GST (rule from the workbook).

Source: PRD-001 §10.4

#### Scenario: Extraction formula
- **WHEN** GST is extracted from a GST-inclusive bank amount
- **THEN** GST = round(gross × rate ÷ (1 + rate), 2), half-up
- **AND** net = gross − GST

### Requirement: RND-CPF CPF rounding
Total CPF SHALL be rounded to the dollar half-up, the employee share rounded down to the dollar and the employer share the difference, on the employee-month total (rules from the CPF Board, re-checked each January, and the workbook).

Source: PRD-001 §10.4

#### Scenario: CPF formula
- **WHEN** CPF is computed
- **THEN** total CPF = round((capped OW + AW) × (ER% + EE%), 0), half-up (50 cents up)
- **AND** EE share = round down to the dollar
- **AND** ER = total − EE

#### Scenario: Employee-month total, never per run
- **GIVEN** an employee paid in more than one run in a calendar month
- **WHEN** CPF is computed
- **THEN** the policy is applied to the employee-month total (all runs of the calendar month together), never per run

#### Scenario: Pinned result for an employee aged 55 or under
- **GIVEN** the G.2 employee aged ≤ 55 with gross 7,600.00 in March (2026 CPF rates)
- **WHEN** CPF is computed
- **THEN** EE CPF is 1,520 and ER CPF is 1,292

#### Scenario: Pinned result with OW capped and a bonus
- **GIVEN** the G.2 employee with OW capped at 8,000 plus a 5,000 bonus, gross 15,000.00 in March (2026 CPF rates)
- **WHEN** CPF is computed
- **THEN** EE CPF is 2,600 and ER CPF is 2,210

#### Scenario: G4-004 Over-70 employee paid in two runs
- **GIVEN** a citizen over 70 who earns S$2,500 in a month, split into two runs
- **WHEN** CPF is computed
- **THEN** total is 313, EE 125 and ER 188 (as G.2), not 312 / 124

### Requirement: RND-CPF-GRAD CPF graduated range
For total wages from S$50 to S$750 a month (the graduated range), total and employee CPF SHALL follow the CPF Board formulas for the wage band, then the dollar rounding of `RND-CPF` (CPF Board, re-checked each January).

Source: PRD-001 §10.4

#### Scenario: Wage in the graduated range
- **GIVEN** an employee's total wages for the month are between S$50 and S$750
- **WHEN** CPF is computed
- **THEN** the total and employee contributions follow the CPF Board formulas for the wage band
- **AND** then the dollar rounding of `RND-CPF` is applied

#### Scenario: G4-006 Wage of S$700 against the workbook
- **GIVEN** a wage of S$700 (G.2)
- **WHEN** CPF is computed
- **THEN** the Fyniq result is derived by hand from the CPF Board graduated table and differs from the workbook's 140 / 119
- **AND** the difference is in the migration difference register

### Requirement: RND-SDL SDL rounding
SDL SHALL be computed per employee to the cent within its minimum and maximum, and the employer-month payable SHALL be their sum rounded down to the dollar (CPF Board: 0.25%, min S$2, max S$11.25, Jul 2026; workbook).

Source: PRD-001 §10.4

#### Scenario: Per employee
- **WHEN** SDL is computed for an employee
- **THEN** SDL = clamp(round(total remuneration of the calendar month × rate, 2), min, max)
- **AND** it is 0 when the remuneration is ≤ 0

#### Scenario: Per employer-month
- **WHEN** the SDL payable for an employer-month is computed
- **THEN** it is the sum of the employee amounts, rounded down to the dollar (CPF Board: "add up the SDL calculated for each employee and then round the total amount down")

#### Scenario: The difference under S$1
- **WHEN** the employer-month total is rounded down
- **THEN** the difference (under S$1) is never payable and is not carried forward
- **AND** its posting is open question 27

#### Scenario: Pinned per-employee amounts
- **GIVEN** the G.2 March employees
- **WHEN** SDL is computed per employee
- **THEN** wage 700 gives 2.00, gross 2,500.00 gives 6.25 and gross 9,000.00 gives 11.25

#### Scenario: Pinned employer-month payables
- **GIVEN** the G.2 per-employee SDL totals S$70.75 in March and S$13.25 in April
- **WHEN** the employer-month payables are computed
- **THEN** the March SDL payable is S$70 and April's is S$13

### Requirement: ALLOC-EVEN Even split over periods
Splitting an amount into n periods (spreads, deferrals, depreciation) SHALL use cumulative rounding so the parts sum exactly to the whole; the accountant confirms the policy at the P1 rule review (A3).

Source: PRD-001 §10.4

#### Scenario: Cumulative rounding formula
- **WHEN** an amount is split into n periods
- **THEN** period k = round(amount × k ÷ n) − round(amount × (k − 1) ÷ n), each rounded half away from zero to the minor unit

#### Scenario: Parts sum to the whole within tight bounds
- **WHEN** an amount is split into n periods
- **THEN** the parts sum exactly to the whole
- **AND** each period is within one minor unit of amount ÷ n
- **AND** each cumulative total is within half a minor unit of amount × k ÷ n

#### Scenario: Negative amounts mirror positive ones
- **WHEN** a negative amount is split into n periods
- **THEN** the parts are the mirror image of those of the positive amount

#### Scenario: Whole cents, changed from the workbook
- **WHEN** a split is posted
- **THEN** each period is in whole cents, where the workbook used unrounded monthly amounts

#### Scenario: G4-015 One dollar over six months
- **WHEN** S$1.00 is spread over 6 months, and −S$1.00
- **THEN** the months are 0.17, 0.16, 0.17, 0.17, 0.16, 0.17 (sum 1.00), and the mirror image for −1.00
- **AND** every month is within S$0.01 of 1/6 and every month-end balance within S$0.005

### Requirement: ALLOC-PRORATA Split by weights
Splitting an amount by weights (allocations, partial payments across documents) SHALL use the largest-remainder method with ties broken by stable order (new).

Source: PRD-001 §10.4

#### Scenario: Largest-remainder method
- **WHEN** an amount is split by weights
- **THEN** the parts are found by the largest-remainder method

#### Scenario: Ties broken by stable order
- **GIVEN** two parts with equal remainders
- **WHEN** the amount is split by weights
- **THEN** the tie is broken by stable order

### Requirement: RND-SHARE Shares allotted for an amount
Shares allotted for an amount at a price per share SHALL be the largest whole number whose rounded value does not exceed the amount, with no fractional shares and the remainder left on the source balance (new, FR-EQ-06; pinned in Appendix G.3).

Source: PRD-001 §10.4

#### Scenario: Share formula
- **WHEN** shares are allotted for an amount at a price per share
- **THEN** n is the largest whole number of shares for which round(n × price, 2) ≤ amount (half-up)
- **AND** converted amount = round(n × price, 2)
- **AND** the remainder stays on the source balance, with no fractional shares

#### Scenario: Pinned conversion at S$1.20 a share
- **GIVEN** a balance of 12,345.67 converted at S$1.20 a share
- **WHEN** the shares are allotted
- **THEN** 10,288 shares are allotted, Dr 2500 12,345.60, Cr 3010 12,345.60
- **AND** 0.07 stays owed

#### Scenario: Pinned conversion at S$0.0375 a share
- **GIVEN** a balance of 12,345.67 converted at S$0.0375 a share
- **WHEN** the shares are allotted
- **THEN** 329,217 shares are allotted and 12,345.64 is converted
- **AND** 0.03 stays owed

### Requirement: RND-MILEAGE Mileage amount
A mileage amount SHALL be the distance rounded to 0.1 km times the rate, then rounded to the cent, per trip (new, FR-AP-16).

Source: PRD-001 §10.4

#### Scenario: Mileage formula per trip
- **WHEN** the mileage amount of a trip is computed
- **THEN** mileage amount = round(distance in km, to 0.1 km, half-up) × rate, then rounded to the cent, half-up
- **AND** it is computed per trip

### Requirement: RND-DISPLAY Display never re-rounds
Display SHALL never re-round stored amounts, and totals SHALL be sums of stored amounts, so every report foots and cross-foots exactly (new).

Source: PRD-001 §10.4

#### Scenario: Stored amounts shown as stored
- **WHEN** a stored amount is displayed
- **THEN** it is not re-rounded

#### Scenario: Reports foot and cross-foot
- **WHEN** a report shows totals
- **THEN** each total is the sum of the stored amounts it covers
- **AND** the report foots and cross-foots exactly

### Requirement: RND-PERCENT Percentage shown
A percentage shown SHALL be the part as a percentage of the whole's size, rounded to one decimal, half-up, with the part's sign; it SHALL be for showing only, never compared by a rule (new, FR-GL-14).

Source: PRD-001 §10.4

#### Scenario: Percentage formula
- **WHEN** a percentage of a whole is shown
- **THEN** percentage = round(part ÷ the size of the whole × 100, 1 decimal), half-up, with the part's sign
- **AND** a whole of zero has no percentage

#### Scenario: Rules compare exact amounts
- **WHEN** a rule compares a change with a percentage threshold
- **THEN** it compares the exact amounts, never a rounded percentage

### Requirement: RND-LINE Line amount
A line's amount SHALL be round(quantity × unit price, the currency's minor units), half away from zero, computed exactly before its one rounding; part of a line taken after others SHALL be RND-LINE of all taken so far less RND-LINE of what was taken before (rev 0.87; provisional).

Source: PRD-001 §10.4

#### Scenario: Line amount formula
- **WHEN** a line has a quantity and a unit price
- **THEN** its amount = round(quantity × unit price, the currency's minor units), half away from zero (3 × 33.333333 SGD is 100.00; 2.5 × 1.25 SGD is 3.13)

#### Scenario: Parts of a line add up exactly
- **GIVEN** a purchase order line billed in parts (FR-AP-01)
- **WHEN** each part's amount is worked out
- **THEN** it is RND-LINE of everything taken so far, this part included, less RND-LINE of what was taken before
- **AND** the parts add up to the line's amount exactly, each within one minor unit of its own quantity × price (three parts of one of 3 × 33.333333 are 33.33, 33.34 and 33.33)

### Requirement: RND-DISCOUNT Early-payment discount
An early-payment discount a supplier offers on a bill (FR-AP-13) SHALL be round(total × percent ÷ 100, the currency's minor units), half away from zero, computed exactly before its one rounding, the percent above 0 and below 100 with at most two decimals (rev 0.93; provisional).

Source: PRD-001 §10.4

#### Scenario: Discount formula
- **WHEN** a bill's supplier offers an early-payment discount
- **THEN** the discount = round(total × percent ÷ 100, the minor units), half away from zero (2% of S$1,090.00 is 21.80; 2.5% of S$1,234.56 is 30.86)

### Requirement: RND-WHT Withholding tax
Withholding tax on a payment to a non-resident supplier (FR-AP-19) SHALL be round(⌊gross⌋ × rate ÷ 100, the base currency's minor units), half away from zero, in the base currency, where gross is the payment before GST in the base currency and ⌊⌋ rounds it down to the dollar (rev 0.90; provisional).

Source: PRD-001 §10.4

#### Scenario: Tax on the gross rounded down to the dollar
- **WHEN** tax is withheld from a payment
- **THEN** the tax = round(⌊gross⌋ × rate ÷ 100, the minor units), half away from zero (S$1,234.56 at 17% is 1,234 × 17% = 209.78; S$5,000.00 at 15% is 750.00)

#### Scenario: A bill in another currency
- **GIVEN** a bill in another currency
- **WHEN** tax is withheld from it
- **THEN** the gross is converted at its rate (`RND-FX`) for the tax, and the supplier's own amount withheld = round(gross × rate ÷ 100, its minor units)
- **AND** the cents between that at the book rate and the tax go to 7030 (USD 1,000.37 at 1.3456 and 17%: tax S$228.82, USD 170.06 withheld)

### Requirement: RND-GST-SHARE Share of a line's GST
The input tax claimed of a line's GST when only a share of it is claimable (FR-AP-20: its business share) SHALL be round(GST × share ÷ 100, the currency's minor units), half away from zero, computed exactly before its one rounding, the rest of the GST staying in the line's cost (rev 0.89; provisional).

Source: PRD-001 §10.4

#### Scenario: Share formula
- **WHEN** a line's GST is claimed for a share
- **THEN** the claim = round(GST × share ÷ 100, the currency's minor units), half away from zero (60% of 9.00 is 5.40; 50% of 7.65 is 3.83, with 3.82 kept in the cost)
- **AND** a share of 100 claims the GST whole and 0 claims none

#### Scenario: A bill in another currency
- **GIVEN** a bill in another currency whose tax invoice states its GST in SGD (ENG-42)
- **WHEN** a line is claimed for a share
- **THEN** the claim is the share of the GST stated in SGD, the cost keeps the share's remainder of the line's own GST at the book rate
- **AND** the difference goes to 7030
