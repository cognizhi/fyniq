# Proposal

## Why

- A customer can pay an invoice from a payment link (`p2-bank-feeds-payments`), but has no place to see what they owe,
  pay several invoices, or download a statement (PRD-001 FR-AR-14).
- Subscriptions exist only as recurring invoices and, for Stripe, a mirror: there is no contract with a start, an end,
  an MRR and dated amendments (FR-AR-15), so contracted MRR (FR-KPI-02, FR-KPI-04) has no source of its own.
- A customer who disputes an invoice or promises a date is still chased by the reminders (FR-AR-08, P2 half), and ageing
  lumps disputed and undisputed money.
- Revenue of a fixed-fee project that is earned over time is booked at invoice or by hand (FR-PRJ-11).

## What Changes

- **A customer portal** reached by a signed, expiring, revocable link scoped to one customer (design D1). A new
  external principal that never has a staff session: it reads that customer's issued invoices, their open balance and
  live payment links, downloads an invoice's PDF and a statement of account, and nothing else. Created by a person with a
  right on Invoices, shown once, emailed to the customer's billing email, stored only as a hash. Attack-tested against
  every other principal and tenant, rate limited, PDPA-reviewed, ASVS-tagged.
- **Subscription contracts** (FR-AR-15): a contract with a start, an end, an MRR and dated amendments (upgrade,
  downgrade, cancel) linked to evidence and to the invoices and credit notes each causes; the monthly schedule and
  contracted MRR derived from them; closed periods never rewritten.
- **Disputes and promise-to-pay** (FR-AR-08, P2): a disputed amount with a reason, or a promised date, pauses an
  invoice's reminders; ageing shows disputed and undisputed amounts apart.
- **Revenue over time for fixed-fee projects** (FR-PRJ-11): see tasks section 4.

## Impact

Migrations 001070 to 001099. A public API surface (no session) of its own and one new principal class: listed
`needs-human-review`. The portal widens no scope or grant of any existing principal.
