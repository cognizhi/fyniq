---
key: settings.organisation
title: Organisation settings
mode: basic
roles: [owner, admin, controller]
requirements: [FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08, FR-GL-14]
status: draft
---

# Organisation settings

## What it is

**Settings → Organisation** holds the choices that apply to the whole company: which features it
uses, the default payment terms, the settlement tolerances, the capitalisation threshold, the
variances the close asks you to explain, and where its data is kept and for how long.

- Every setting starts at its **recommended** value, with one line on why and a link to its
  section on this page.
- A setting shows **Recommended** while it holds that value, and **Changed from recommended**
  once it differs.
- The Owner, the Administrator and the Controller change settings; everyone else in the company
  can see them.

## The profile

The **Profile** at the top of the page is what documents print about the company: its legal name,
UEN, addresses, contact details, and the bank and PayNow details customers pay to. It also says
when each financial year starts, when the books start and the company's time zone.

Once the first financial year is open in **Accounting → Periods and close**, when the books start,
the month the year starts in and the end of the first year no longer change.

1. Select **Edit the profile**.
2. Change the fields. A field left empty is cleared.
3. Select **Save the profile**. If someone else saved it meanwhile, Fyniq asks you to reload first.

## Change a setting

1. Open **Settings → Organisation** (Expert mode) and select **Change** next to the setting.
2. Enter the new value and the date it takes effect: today or a later date, never an earlier one.
3. Select **Preview** to check it, then **Save**. Confirm it's you if Fyniq asks.

- A change applies from its date. Fyniq keeps every earlier value with its dates, so what was in
  effect on any day can be shown again.
- **Reset to recommended** records the recommended value as a new change, from the date you
  choose. Nothing is deleted.
- If someone else changed the setting after you opened it, Fyniq asks you to reload it first.

## Settings that need a second approver

- Thresholds, retention, the accounting sign-off and the AI assistant need a second approver before they
  change: they show **Needs a second approver**.
- For one of these, the button says **Ask for approval** instead of **Save**.
- Saving a change to one asks for approval. The setting keeps its value until another member who
  approves settings, such as an Owner, approves it (see [Approve a change](approvals.md)).

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
- The company's accountant confirms the threshold, by approving the capitalisation threshold policy under
  **Settings → Accounting framework and policies**.

## Accounting sign-off

- A member with the Accountant role confirms the accounting framework and approves each accounting
  policy under **Settings → Accounting framework and policies**. While this setting is on, Fyniq first
  asks them to confirm it's them with their passkey or authenticator app, as for other approvals.
- It is on by default. Turning it off needs a second approver, like the other thresholds.

## Self-approval threshold

- In Founder mode a founder posts a journal of up to S$2,000 alone. It is flagged as self-approved
  and listed in the weekly review, where another founder reviews it.
- Above the threshold, posting a journal needs another founder's approval, and the one who prepared
  it never approves it.
- Changing the threshold needs a second approver.

## Variances at the close

Before a month closes, Fyniq compares each income and expense account's total for the month with
the previous month's. A change needs a short explanation when it is more than **both** of these:

- **Variance to explain, amount**: S$1,000.
- **Variance to explain, percentage of the previous month**: 10% of the previous month's total.

When the previous month's total is zero, the amount alone decides. Changing either needs a second
approver. See [Close a month](periods-and-close.md#close-a-month).

## Data region and retention

- **Data region**: every record is kept and processed in Google Cloud's Singapore region. The
  platform sets it, so it is shown and cannot be changed.
- **Records kept for**: the ledger, documents, files and the audit trail are kept 7 years after
  the end of the financial year they relate to. It can be lengthened, never shortened.
- **AI conversations kept for**: 365 days, then they are deleted. A proposal the AI made is kept
  with the audit trail.

## Related

- [Set up and keep the chart of accounts](chart-of-accounts.md)
- [Approve a change](approvals.md)
- [Record and approve the accounting framework and policies](accounting-framework.md)
