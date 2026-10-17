# Proposal

## Why

Every P1 module posts to accounts, carries dimensions, names a contact or an item, and reads the
organisation's settings. P0 left all of them minimal: `fyniq.account` holds only what the posting
engine needs, and there are no contacts, items, dimensions or settings. This change is the first of
wave 1 in the P1–P3 build plan (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`); it may start once
question 26's comparison is signed (§17.4).

## What Changes

- **Organisation settings (§8.1):**
  - the profile, with the logo, the payment details for documents and the footers (FR-ORG-01);
  - the financial year, books start date, base currency and time zone (FR-ORG-02);
  - GST status with its effective dates, and the accounting basis (FR-ORG-03);
  - defaults and gap-free number sequences per document type (FR-ORG-04);
  - feature switches (FR-ORG-06);
  - the data region and retention, shown to the Owner (FR-ORG-07);
  - every §20 policy and evidence rule shipped at its recommended value, with a reason, a reset and
    an effective date (FR-ORG-08);
  - the company logo and accent colour, applied everywhere at once, with the SVG reduced to a safe
    subset (FR-ORG-09).
- **Chart of accounts (§8.2):**
  - the workbook's 83-account Singapore start-up chart pre-loaded and editable (FR-COA-01);
  - every account attribute (FR-COA-02);
  - protected system accounts (FR-COA-03);
  - deactivating, or merging by an approved journal-preserving merge, an account with postings
    (FR-COA-04);
  - the parent and child hierarchy, with posting only to leaves (FR-COA-05);
  - Department, Project and up to 8 custom dimensions, required per account where set (FR-DIM-01);
  - dimension values on every ledger line (FR-DIM-02).
- **Master data (§8.3):**
  - contacts (FR-CT-01), with their live balances and activity (FR-CT-02);
  - vendor bank details as high-risk data: dual control, a notice to every founder, and payments
    frozen until approval (FR-CT-03);
  - duplicate detection (FR-CT-04);
  - items, with prices per currency (FR-ITM-01).
- **The logical data model (§11.4, §11.6), all 14 unnumbered requirements of `data/data-model`:**
  - claimed with tests. Most of them were built by `p0-database-tenancy-migrations` and are tagged
    now;
  - the rest are added: effective-dated reference data with temporal keys, the explicit list of
    global tables, and version counters on mutable records.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `organisation/settings`: FR-ORG-01…04 and 06…09;
- `ledger/chart-of-accounts`: FR-COA-01…05, FR-DIM-01 and 02;
- `master-data/contacts`: FR-CT-01…04;
- `master-data/items`: FR-ITM-01;
- `data/data-model`: 14 requirements.

FR-ORG-05, the statutory packs, belongs to `p1-statutory-packs`. FR-CT-02's figures come from the
ledger as each module arrives; this change builds the panel and the balances that exist.

## Impact

- **Code:**
  - migrations for settings, accounts, dimensions, contacts, items and sequences;
  - the storage port and its PostgreSQL adapter, with the conformance suite;
  - domain packages for each area;
  - the API operations in `contracts/openapi/fyniq.v1.yaml`;
  - the generated client;
  - the screens.
- **Screens, as designed in `docs/design/screens/`:** chart-of-accounts, contact-record,
  contacts-list, items, logo-brand and organisation-settings.
- **Dependencies:** the approval engine for FR-CT-03's dual control and FR-COA-04's merge. Until
  `p1-approval-engine` lands, both refuse with a clear message rather than skipping the control.
- **Reviewer:** the accountant, on the chart and its attributes.
