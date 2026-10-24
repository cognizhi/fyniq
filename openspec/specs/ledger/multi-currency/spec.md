# ledger/multi-currency Specification

## Purpose
How Fyniq handles foreign currencies: where the exchange rate for a posting comes from, realised and unrealised FX, GST on foreign-currency documents reported in SGD, and foreign-currency bank accounts with both currency and base balances. Derived from PRD-001 §10.8.

## Requirements

### Requirement: ENG-40 Rate cascade at posting
At posting, the system SHALL take the exchange rate from a cascade: the document's own rate, then the daily rate feed for the document date, then the organisation's monthly table, and otherwise SHALL stop with an error.

Source: PRD-001 §10.8 · Priority M · Phase P1

#### Scenario: Document's own rate first
- **GIVEN** a foreign-currency document that carries its own rate
- **WHEN** it is posted
- **THEN** the document's own rate is used

#### Scenario: Daily feed, then the monthly table
- **GIVEN** a foreign-currency document without its own rate
- **WHEN** it is posted
- **THEN** the rate comes from the daily rate feed for the document date
- **AND** if the feed has none, from the organisation's monthly table

#### Scenario: No rate anywhere
- **GIVEN** no rate in the document, the daily feed or the monthly table
- **WHEN** the document is posted
- **THEN** it cannot post, with the error "✖ No exchange rate for this currency"

#### Scenario: The organisation's rate for the day comes first (rev 0.70)
- **GIVEN** a foreign-currency document without its own rate, and the organisation's rate for the document date
- **WHEN** it is posted
- **THEN** it takes the organisation's rate, before the feed's, recorded as "organisation rate of" the date

#### Scenario: The feed's latest rate within a week (rev 0.70)
- **GIVEN** no feed rate for the document date, such as a Sunday, and one up to 7 days earlier
- **WHEN** a foreign-currency document without its own rate is posted
- **THEN** it takes the feed's latest rate within those 7 days, and the source names that rate's own date
- **AND** with none in the 7 days it falls to the monthly table [accountant to confirm the look-back]

#### Scenario: A manual journal takes the cascade's rate (rev 0.71)
- **GIVEN** a manual journal's foreign-currency line without a typed rate
- **WHEN** its checks run, and when it posts
- **THEN** the line takes the rate the cascade finds for the journal's date, and the entry records it with its source
- **AND** with no rate anywhere the line's check is FX-01 and posting is refused; a typed rate stays the document's own

#### Scenario: Duplicate monthly-table rates
- **GIVEN** the monthly table holds more than one rate for a currency and month
- **WHEN** a posting takes the rate from the monthly table
- **THEN** it uses the average of the duplicates, as in the workbook, and the line records "monthly table, average of n entries"
- **AND** the duplicate shows a warning (⚠) on Checks C.1 #9 until resolved

#### Scenario: An average never sets a GST amount
- **GIVEN** a rate that is an average of duplicates
- **WHEN** a GST amount is determined
- **THEN** the average does not set it (ENG-42)

#### Scenario: G4-025 Two monthly-table rates in one month
- **GIVEN** two monthly-table rates for USD in the same month
- **WHEN** a USD document is converted from the monthly table
- **THEN** a warning (⚠) shows on Checks until resolved
- **AND** book conversion uses the recorded average with its source
- **AND** no GST amount is derived from it

### Requirement: ENG-41 Realised and unrealised FX
The system SHALL post realised FX on settlement to 7030 and SHALL revalue unrealised FX at month end (FR-GL-04).

Source: PRD-001 §10.8 · Priority M · Phase P1

#### Scenario: Realised FX on settlement
- **GIVEN** a foreign-currency document settled at a rate different from its stored rate
- **WHEN** the settlement posts
- **THEN** the realised FX difference posts to 7030

#### Scenario: Unrealised FX at month end
- **WHEN** a month ends with open foreign-currency items
- **THEN** the unrealised FX is revalued as FR-GL-04 sets out

#### Scenario: Built as the engine's rule (rev 0.74)
- **WHEN** a settlement of a foreign-currency document posts
- **THEN** its realised FX is `RND-FX`(the amount settled × the settlement rate) − `RND-FX`(the amount settled × the stored rate), with no tolerance

### Requirement: ENG-42 GST on foreign-currency documents
The system SHALL report GST on foreign-currency documents in SGD: on sales, converted at the organisation's GST exchange-rate source; on purchases, the SGD GST stated on the supplier's tax invoice or the import permit.

Source: PRD-001 §10.8 · Priority M · Phase P1

#### Scenario: Sales GST converted at the GST exchange-rate source
- **WHEN** GST on a foreign-currency sale is reported
- **THEN** it is converted at the organisation's GST exchange-rate source (an IRAS-approved source, updated at least every three months and used consistently for at least a year), then `RND-FX` and `RND-GST-EXCL`

#### Scenario: Tax invoices show SGD
- **WHEN** a tax invoice is issued in a foreign currency
- **THEN** it shows net, GST and gross in SGD

#### Scenario: Invoices as built (rev 0.116)
- **WHEN** a foreign-currency invoice is issued
- **THEN** its nets in SGD under `RND-FX` bear `RND-GST-EXCL`, credited to 2200, and the cents against its total at the rate go to 7030
- **AND** until the organisation's GST exchange-rate source exists, the invoice's own rate stands in for it

#### Scenario: Purchase input tax as stated
- **WHEN** a foreign-currency bill or import is recorded
- **THEN** the input tax is the SGD GST stated on the supplier's tax invoice, or on the import permit for imports
- **AND** it is never a recomputation at the book rate

#### Scenario: No SGD GST stated on the tax invoice
- **GIVEN** a foreign-currency tax invoice that states no SGD GST
- **WHEN** the bill is recorded
- **THEN** the bill still posts and the GST stays in the cost (as for code BL)
- **AND** the line shows ⚠ "no SGD GST stated on the tax invoice; input tax not claimed"

#### Scenario: What the document stores
- **WHEN** a foreign-currency document is saved
- **THEN** it stores its currency amounts, the book rate and its source, the GST rate source and the stated SGD GST

#### Scenario: GST difference to 7030
- **GIVEN** the stated SGD GST differs from the GST at the book rate
- **WHEN** the bill posts
- **THEN** the difference posts to 7030 on the same entry

#### Scenario: G4-023 USD bill with stated SGD GST
- **GIVEN** a USD bill: net 1,000.00 + GST 90.00 = 1,090.00; the supplier's invoice states GST S$121.50; book rate 1.34
- **WHEN** the bill posts
- **THEN** Dr expense 1,340.00; Dr 2200 121.50 (as stated, not 120.60); Cr AP 1,460.60; Cr 7030 0.90
- **AND** the entry balances
- **AND** Box 7 shows 121.50

#### Scenario: The difference keeps the entry balanced (rev 0.74)
- **GIVEN** a foreign-currency bill with a stated SGD GST
- **WHEN** it posts
- **THEN** the difference to 7030 is the net at the book rate + the stated GST − the gross at the book rate, so a cent of `RND-FX` between them goes to 7030 too
- **AND** the sales' half waits for `RND-GST-EXCL` (with `p1-sales`)

### Requirement: ENG-43 Foreign-currency bank accounts
Bank accounts in foreign currency SHALL keep both currency and base balances, and SHALL be reconciled in the account currency.

Source: PRD-001 §10.8 · Priority M · Phase P1

#### Scenario: Both balances kept
- **WHEN** a foreign-currency bank account's balance is shown
- **THEN** both its currency balance and its base balance are shown

#### Scenario: Reconciliation in the account currency
- **WHEN** a foreign-currency bank account is reconciled
- **THEN** the reconciliation is in the account currency

#### Scenario: Shown in both currencies (rev 0.74)
- **WHEN** Accounting › Currencies or `listForeignMoneyAccounts` is read
- **THEN** each money account's balance in each other currency shows in the currency and in the books, at the end of the period that holds today
