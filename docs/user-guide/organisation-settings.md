---
key: settings.organisation
title: Organisation settings
mode: basic
roles: [owner, admin, controller]
requirements: [FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08]
status: draft
---

# Organisation settings

## What it is

**Settings → Organisation** holds the choices that apply to the whole company: which features it
uses, the default payment terms, the settlement tolerances, the capitalisation threshold, and
where its data is kept and for how long.

- Every setting starts at its **recommended** value, with one line on why and a link to its
  section on this page.
- A setting shows **Recommended** while it holds that value, and **Changed from recommended**
  once it differs.
- The Owner, the Administrator and the Controller change settings; everyone else in the company
  can see them.

## Change a setting

1. Open **Settings → Organisation** and choose the setting.
2. Enter the new value and the date it takes effect: today or a later date, never an earlier one.
3. Select **Preview** to check it, then **Save**.

- A change applies from its date. Fyniq keeps every earlier value with its dates, so what was in
  effect on any day can be shown again.
- **Reset to recommended** records the recommended value as a new change, from the date you
  choose. Nothing is deleted.
- If someone else changed the setting after you opened it, Fyniq asks you to reload it first.

## Settings that need a second approver

- Thresholds, retention and the AI assistant need a second approver before they change.
- Until approvals arrive in Fyniq, these settings keep their recommended values and show
  **Needs a second approver**. Saving a change to one is refused, and nothing changes.

## Features

Switch off what the company does not use, and Fyniq hides it from menus and pages.

| Feature | Recommended | Why |
|---|---|---|
| Multi-currency | On | Start-ups usually pay some bills in other currencies, such as cloud and software subscriptions in US dollars |
| Payroll | Off | Off while the company has no employees and the founders draw no salary; turn it on before the first pay run |
| Projects and time | Off | Off until you track time or costs by project |
| Portals for customers and investors | Off | Off until there are customers or investors to invite |
| AI assistant | Off | AI stays off until an Owner turns it on with a second approver |

## Payment terms

- **Payment terms for invoices**: an invoice to a customer with no terms of its own falls due
  30 days after its date.
- **Payment terms for bills**: a bill from a vendor with no terms of its own falls due 30 days
  after its date.
- A contact's own payment terms always win over these.

## Settlement tolerances

When a payment differs a little from its invoice or bill, the difference can clear to **FX and
rounding** (7030) instead of leaving the document part-paid.

- In the base currency: a difference of up to S$1.00.
- In a foreign currency: the greater of S$1.00 and 3% of the document.
- Never more than S$50 on one document, or S$200 in a month in all.
- Exchange-rate gains and losses are worked out first and are never limited by a tolerance.
- An overpayment above S$1.00 stays with the customer as a credit.
- Anything above the tolerance needs an adjustment with a reason, such as a discount or bank
  charges.

## Capitalisation threshold

- A purchase of S$1,000 or more is offered as a fixed asset; a smaller one is an expense.
- The company's accountant confirms the threshold.

## Data region and retention

- **Data region**: every record is kept and processed in Google Cloud's Singapore region. The
  platform sets it, so it is shown and cannot be changed.
- **Records kept for**: the ledger, documents, files and the audit trail are kept 7 years after
  the end of the financial year they relate to. It can be lengthened, never shortened.
- **AI conversations kept for**: 365 days, then they are deleted. A proposal the AI made is kept
  with the audit trail.
