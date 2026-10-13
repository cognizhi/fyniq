# master-data/items Specification

## Purpose
How the products and services the company sells are recorded in Fyniq, with their prices, revenue accounts and GST codes, and how price lists vary them by customer group or currency. Derived from PRD-001 §8.3.

## Requirements

### Requirement: FR-ITM-01 Items
The system SHALL keep items (products and services) with a code, name, printed description, type, unit, price per currency, revenue account, GST code and active flag, and SHALL show each item's quantity and sales for the financial year.

Source: PRD-001 §8.3 · Priority S · Phase P1

#### Scenario: Item details are recorded
- **WHEN** an item is saved with a code, name, printed description, type, unit, revenue account, GST code and active flag
- **THEN** each of those values is stored on the item

#### Scenario: Price per currency
- **WHEN** an item is given a price in more than one currency
- **THEN** each currency keeps its own price

#### Scenario: Financial-year quantity and sales
- **WHEN** a user opens an item
- **THEN** the item's quantity and sales for the financial year are shown

### Requirement: FR-ITM-02 Price lists
The system SHALL support price lists per customer group or per currency.

Source: PRD-001 §8.3 · Priority C · Phase P3

#### Scenario: Price list by customer group
- **GIVEN** a price list for a customer group
- **WHEN** an item is added to a document for a customer in that group
- **THEN** the item's price comes from that price list

#### Scenario: Price list by currency
- **GIVEN** a price list for a currency
- **WHEN** an item is added to a document in that currency
- **THEN** the item's price comes from that price list
