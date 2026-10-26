---
key: settings.organisation
title: Organisation settings
mode: basic
roles: [owner, admin, controller]
requirements: [FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08, FR-GL-14, FR-AP-13, FR-AP-16, FR-AR-05, FR-BNK-16]
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

- Thresholds, retention, the accounting sign-off, auto-reconcile's limits, the bank charges account, when
  incoming-funds alerts expire, the AI assistant and details in notification email need a second approver before
  they change: they show **Needs a second approver**.
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

## Bill reminders

**Remind of bills due within** says how many days ahead the Inbox lists the bills to pay: 3 days unless
changed, from 0 to 30. A bill is listed once it falls due within that many days, or its planned payment
date does, and stays while it is overdue, until its payment is recorded. Members who record bill payments
see the list. See [Bills](bills.md).

## GST rounding

- **Round GST per line** is off: the GST of a document is worked out once for each GST code, on
  the code's total, and rounded to the cent. Turned on, each line's GST is rounded and the results
  added.
- IRAS accepts either, as long as the company uses the same one on every document.
- On a bill, the input tax is the GST the supplier states. Fyniq works it out again by the method
  chosen here, and shows ⚠ when the two differ, even by a cent, for the approver to check.

## Simplified tax invoices

- **Simplified tax invoices up to S$1,000** is off: every invoice from a GST-registered company is a
  full tax invoice, with the customer's name and address, the amount before GST, the GST rate and the
  GST.
- Turned on, an invoice whose total with GST is S$1,000.00 or less (in SGD, at the invoice's rate) is
  issued as a simplified tax invoice: it leaves out the customer's details and the GST breakdown, and
  says "Price payable includes GST." Larger invoices and credit notes stay full.
- IRAS allows a simplified tax invoice up to S$1,000; a full tax invoice is always valid. See
  [Design your documents with templates](document-templates.md#what-an-invoice-shows).

## Settlement tolerances

When a payment differs a little from its invoice or bill, the difference can clear to **FX and
rounding** (7030) instead of leaving the document part-paid.

- In the base currency: a difference of up to S$1.00.
- In a foreign currency: the greater of S$1.00 and 3% of the document.
- Never more than S$50 on one document, or S$200 in a month in all.
- The difference clears only when the payment or receipt is recorded as settling the document **in
  full**; a part payment leaves the rest open. The receipt or payment shows the difference it cleared.
- Exchange-rate gains and losses are worked out first and are never limited by a tolerance.
- An overpayment above S$1.00 stays with the customer as a credit.
- Anything above the tolerance needs an adjustment with a reason, such as a discount or bank
  charges.

## Auto-reconcile

Some bank lines are reconciled without a click, by Fyniq itself: a line that matches exactly one open
invoice or bill (its amount, its number and its customer or supplier), the other side of a transfer
already recorded, or a line a bank rule meets once a second person has let that rule apply by itself.

- **Auto-reconcile, the largest line**: S$500. A larger line waits in the queue for a person.
- **Auto-reconcile, per rule a day**: S$2,000. Once a rule has reconciled that much in a day, its other
  lines wait for a person until the next day.
- Every line reconciled this way shows what did it and can be undone while its month is open.
- Each week the 5 largest and 5 others picked at random join the weekly review.
- A change to either needs a second approver.

## Bank charges

When you record a transfer between two of your money accounts, a fee the bank or provider reports
(Wise shows one on each conversion) goes to the **bank charges account**: 6460 Bank charges in the
standard chart.

- Choose another account by its code, such as a fees account you added to the chart.
- Across currencies, each side of a transfer is converted at its own currency's rate for the day;
  what is left after the fee goes to **FX and rounding** (7030).
- A change needs a second approver, as the other accounting settings do.

## Incoming-funds alerts

A bank can email an alert as soon as money arrives, a day before the statement line. Sent to your
bank-ingest address, the alert shows the invoice it most likely pays as **likely paid**, on the
invoice and on Home. Nothing is posted, and the invoice stays open until the statement line is
reconciled to it.

- **Incoming-funds alerts expire after**: 7 days. An alert whose invoice no statement line has
  reconciled by then stops showing the invoice as likely paid.
- Anyone who reconciles bank lines can dismiss an alert sooner, with a reason, such as one that
  points to the wrong invoice.
- A change needs a second approver, as the other bank settings do.

## Mileage rates

A member who drives their own car or motorcycle for the company claims the distance at the
organisation's **rate per km** for the vehicle. IRAS prescribes no rate per km: it must be
reasonable, so Fyniq ships these rates **not set**, and a mileage claim waits until the owner or the
accountant sets the rate for its vehicle.

- **Mileage rate for a car, per km** and **Mileage rate for a motorcycle, per km**: in Singapore
  dollars, from 0.01 to 5.00.
- A rate takes effect from a day and is never back-dated: a trip is claimed at the rate in force on
  its date, so a claim keeps its amount when the rate changes later.
- The distance is rounded to 0.1 km and the amount to the cent, trip by trip.
- A mileage line claims no GST.
- A change needs a second approver, as the other money settings do.

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
approver. See [Explain the variances](periods-and-close.md#explain-the-variances).

## Data region and retention

- **Data region**: every record is kept and processed in Google Cloud's Singapore region. The
  platform sets it, so it is shown and cannot be changed.
- **Records kept for**: the ledger, documents, files and the audit trail are kept 7 years after
  the end of the financial year they relate to. It can be lengthened, never shortened.
- **AI conversations kept for**: 365 days, then they are deleted. A proposal the AI made is kept
  with the audit trail.

## Email-in

Bills and receipts sent to your email-in addresses are captured for someone to confirm. Mail from your
members and from your contacts' billing and remittance emails is accepted. Mail from anyone else waits in
quarantine until someone accepts or rejects the sender.

- **Quarantined mail kept for**: 30 days, then quarantined mail nobody accepted is deleted. Set it to 0
  to keep quarantined mail until someone decides.

## Notifications

Everyone gets their notifications in Fyniq, and by email as their own preferences say. This setting decides
how much an email says.

- **Details in notification email**: off. An email says what kind of thing happened, such as "An approval
  waits for you", and links to it in Fyniq; amounts, names and reasons stay in Fyniq. The email provider
  keeps mail outside Singapore, so turn it on only once your email provider's transfer of personal data is
  assessed. With it on, an email shows the same title as the notification in Fyniq.

## Related

- [Set up and keep the chart of accounts](chart-of-accounts.md)
- [Approve a change](approvals.md)
- [Record and approve the accounting framework and policies](accounting-framework.md)
