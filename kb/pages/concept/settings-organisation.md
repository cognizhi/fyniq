---
key: settings.organisation
type: concept
title: Organisation settings and their recommended values
summary: Company-wide settings and recommended values: features, terms, GST, tolerances, banking, claims, mileage, thresholds, asset disposals, email-in, retention
sources:
  - docs/user-guide/organisation-settings.md sha256:3f25c5cb0ac69d8bd925870237e43993cbafcd4e4a37ef47608631d11b8b2e7f
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-27
aliases:
  - feature switches
  - default settings
  - tolerance
  - retention period
  - data residency
  - self-approval threshold
  - self-approved
  - variance threshold
  - flux analysis
  - quarantine
  - email-in
  - notification email
  - simplified tax invoice
  - bank charges account
  - transfer fee
  - auto-reconcile
  - reconciled without a click
  - incoming-funds alert
  - likely paid
  - alert expiry
  - advances account
  - staff advance
  - claim deadline
  - late claim
  - mileage rate
  - rate per km
  - disposal account
  - gain or loss on disposal
  - disposal clearing account
---

# Organisation settings and their recommended values

## What they are

- **Settings → Organisation** (Expert mode) holds the profile documents print (their look is the templates', [[settings.templates]]) and the choices that apply to the whole company.
- Each setting starts at its recommended value, with a reason and a link to the guide.
- It shows **Recommended** while it holds that value, **Changed from recommended** once it differs.
- The Owner, the Administrator and the Controller change them; others in the company can see them. How: [[settings.change]].

(src: docs/user-guide/organisation-settings.md#what-it-is)

## Features

- Switching a feature off hides it from menus and pages.
- Recommended: **Multi-currency** on; **Payroll**, **Projects and time** and **Portals for customers and investors** off.
- The **AI assistant** is off until an Owner turns it on with a second approver.

(src: docs/user-guide/organisation-settings.md#features)

## Payment terms

- Invoices and bills with no terms of their own fall due 30 days after their date.
- A contact's own payment terms always win.

(src: docs/user-guide/organisation-settings.md#payment-terms)

## Bill reminders

- **Remind of bills due within**: how many days ahead the Inbox lists the bills to pay, 3 days unless changed, from 0 to 30.
- A bill is listed once it falls due within that many days, or its planned payment date does, and stays while it is overdue, until its payment is recorded; members who record bill payments see the list ([[purchases.bills]]).

(src: docs/user-guide/organisation-settings.md#bill-reminders)

## GST rounding

- **Round GST per line** is off: a document's GST is worked out once per GST code, on the code's total, and rounded to the cent; turned on, each line's GST is rounded and the results added.
- IRAS accepts either, as long as the company uses the same one on every document.
- On a bill, the input tax is the GST the supplier states; Fyniq works it out again by the chosen method and shows ⚠ when the two differ, even by a cent, for the approver to check.

(src: docs/user-guide/organisation-settings.md#gst-rounding)

## Simplified tax invoices

- **Simplified tax invoices up to S$1,000** is off: every invoice from a GST-registered company is a full tax invoice, with the customer's name and address, the amount before GST, the GST rate and the GST.
- Turned on, an invoice whose total with GST is S$1,000.00 or less (in SGD, at the invoice's rate) is issued as a simplified tax invoice: no customer details or GST breakdown, and the words "Price payable includes GST." Larger invoices and credit notes stay full.
- IRAS allows a simplified tax invoice up to S$1,000; a full tax invoice is always valid. What an invoice shows: [[settings.templates]].

(src: docs/user-guide/organisation-settings.md#simplified-tax-invoices)

## Settlement tolerances

- A small difference between a payment and its document clears to **FX and rounding** (7030) instead of leaving it part-paid.
- Base currency: up to S$1.00. Foreign currency: the greater of S$1.00 and 3% of the document.
- Never more than S$50 on one document, or S$200 in a month in all.
- The difference clears only when the payment or receipt is recorded as settling the document **in full**; a part payment leaves the rest open, and the receipt or payment shows the difference it cleared.
- Exchange-rate gains and losses come first and are never limited by a tolerance.
- An overpayment above S$1.00 stays with the customer as a credit; anything above tolerance needs a reason-coded adjustment.

(src: docs/user-guide/organisation-settings.md#settlement-tolerances)

## Auto-reconcile

- Some bank lines are reconciled by Fyniq itself: a line that matches exactly one open invoice or bill (its amount, its number and its customer or supplier), the other side of a transfer already recorded, or a line a bank rule meets once a second person has let that rule apply by itself.
- **Auto-reconcile, the largest line**: S$500; a larger line waits in the queue for a person.
- **Auto-reconcile, per rule a day**: S$2,000; once a rule has reconciled that much in a day, its other lines wait until the next day.
- Every line reconciled this way shows what did it and can be undone while its month is open; each week the 5 largest and 5 others picked at random join the weekly review.
- A change to either needs a second approver.

(src: docs/user-guide/organisation-settings.md#auto-reconcile)

## Bank charges

- A fee the bank or provider reports on a transfer between the company's money accounts (Wise shows one on each conversion) goes to the **bank charges account**: 6460 Bank charges in the standard chart.
- Another account can be chosen by its code, such as a fees account added to the chart.
- Across currencies each side of a transfer is converted at its own currency's rate for the day; what is left after the fee goes to **FX and rounding** (7030).
- A change needs a second approver, as the other accounting settings do.

(src: docs/user-guide/organisation-settings.md#bank-charges)

## Incoming-funds alerts

- A bank's email that money arrived, sent to the bank-ingest address a day before the statement line, shows the invoice it most likely pays as **likely paid**, on the invoice and on Home ([[sales.invoices]]).
- Nothing is posted: the invoice stays open until the statement line is reconciled to it.
- **Incoming-funds alerts expire after**: 7 days; an alert whose invoice no statement line has reconciled by then stops showing it as likely paid.
- Anyone who reconciles bank lines can dismiss an alert sooner, with a reason, such as one that points to the wrong invoice.
- A change needs a second approver, as the other bank settings do.

(src: docs/user-guide/organisation-settings.md#incoming-funds-alerts)

## Advances account

- Money advanced to a member before a trip waits on the **advances account** until their claims settle it or they refund what is left: 1250 Staff & director advances in the standard chart.
- Choose another account by its code; an advance, a refund of one, and a claim that settles one all post to it.
- A change needs a second approver, as the other accounting settings do.

(src: docs/user-guide/organisation-settings.md#advances-account)

## Claim deadline

- A member claims what they spent within the **claim deadline**: 60 days from the receipt's or the trip's date by default.
- A line older than that when the claim is submitted is **late**: the claim shows it, the member says why before submitting, and finance (the Controller) approves the claim as well as its usual approver.
- From 1 to 365 days; a change needs a second approver, as the other money settings do.

(src: docs/user-guide/organisation-settings.md#claim-deadline)

## Mileage rates

- A member who drives their own car or motorcycle for the company claims the distance at the organisation's **rate per km** for the vehicle.
- IRAS prescribes no rate per km; it must be reasonable, so both rates ship **not set**, and a mileage claim waits until the owner or the accountant sets its vehicle's rate.
- Each rate is in Singapore dollars, from 0.01 to 5.00, and takes effect from a day, never back-dated: a trip is claimed at the rate in force on its date.
- The distance is rounded to 0.1 km and the amount to the cent, trip by trip; a mileage line claims no GST.
- A change needs a second approver, as the other money settings do.

(src: docs/user-guide/organisation-settings.md#mileage-rates)

## Capitalisation threshold

- A purchase of S$1,000 or more is offered as a fixed asset; a smaller one is an expense. Registering one: [[accounting.fixed-assets]].
- The company's accountant confirms it by approving the capitalisation threshold policy under **Settings → Accounting framework and policies** ([[settings.accounting]]).

(src: docs/user-guide/organisation-settings.md#capitalisation-threshold)

## Disposal account

- When an asset is sold or written off, the difference between what it fetched and what it was still worth is a gain or a loss; it goes to the **disposal account**: 7050 Gain / (loss) on disposal of assets in the standard chart.
- Choose another account by its code, such as separate accounts the accountant prefers; a change needs a second approver.

(src: docs/user-guide/organisation-settings.md#disposal-account)

## Disposal clearing account

- An asset sold on a sales invoice is invoiced to the **disposal clearing account**: the invoice's line goes to it, and the disposal clears it, so the gain or loss falls in the month of the disposal, whatever the invoice's month.
- It starts **not set**: the accountant names a balance-sheet account for it before the first asset is sold on an invoice.
- An asset sold for money received into a bank account does not use it: the disposal records the money, and bank import matches it. A change needs a second approver.

(src: docs/user-guide/organisation-settings.md#disposal-clearing-account)

## Accounting sign-off

- While this setting is on, a member with the Accountant role confirms the accounting framework and approves each accounting policy only after confirming it's them with a passkey or authenticator app, as for other approvals ([[settings.accounting]]).
- It is on by default; turning it off needs a second approver, like the other thresholds.

(src: docs/user-guide/organisation-settings.md#accounting-sign-off)

## Self-approval threshold

- In Founder mode a founder posts a journal of up to S$2,000 alone; it is flagged as self-approved and listed in the weekly review, where another founder reviews it.
- Above the threshold, posting a journal needs another founder's approval, and the one who prepared it never approves it ([[approvals.decide]]).
- Changing the threshold needs a second approver.

(src: docs/user-guide/organisation-settings.md#self-approval-threshold)

## Variances at the close

- Before a month closes, each income and expense account's total for the month is compared with the previous month's.
- A change needs a short explanation when it is more than **both** S$1,000 and 10% of the previous month's total; when the previous month's total is zero, the amount alone decides.
- Changing either needs a second approver. Explaining them at the close: [[accounting.periods]].

(src: docs/user-guide/organisation-settings.md#variances-at-the-close)

## Data region and retention

- Data region: Google Cloud's Singapore region, set by the platform and read-only.
- Records (ledger, documents, files, audit trail) are kept 7 years after the end of their financial year; this only lengthens.
- AI conversations are kept 365 days; proposals stay with the audit trail.

(src: docs/user-guide/organisation-settings.md#data-region-and-retention)

## Email-in

- Bills and receipts sent to the email-in addresses are captured for someone to confirm; mail from members and from contacts' billing and remittance emails is accepted, anyone else's waits in quarantine until someone accepts or rejects the sender.
- **Quarantined mail kept for**: 30 days, then unaccepted quarantined mail is deleted; 0 keeps it until someone decides.

(src: docs/user-guide/organisation-settings.md#email-in)

## Notifications

- Everyone gets notifications in Fyniq, and by email as their own preferences say; this setting decides how much an email says.
- **Details in notification email** is off: an email says what kind of thing happened, such as "An approval waits for you", and links to it in Fyniq; amounts, names and reasons stay in Fyniq.
- The email provider keeps mail outside Singapore, so turn it on only once the provider's transfer of personal data is assessed; on, an email shows the same title as the notification in Fyniq. Changing it needs a second approver.

(src: docs/user-guide/organisation-settings.md#notifications)

## Related

- Setting up the accounts themselves: [[accounting.chart]]. Approving a change: [[approvals.decide]]. The accounting framework and policies: [[settings.accounting]].

(src: docs/user-guide/organisation-settings.md#related)
