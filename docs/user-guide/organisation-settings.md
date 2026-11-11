---
key: settings.organisation
title: Organisation settings
mode: basic
roles: [owner, admin, controller]
requirements: [FM-8, FR-ORG-03, FR-ORG-04, FR-TAX-05, FR-TAX-07, FR-ORG-06, FR-ORG-07, FR-ORG-08, FR-GL-14, FR-AP-13, FR-AP-15, FR-AP-16, FR-AP-17, FR-AR-05, FR-BNK-16, FR-FA-03, AUD-06, AUD-10]
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

## GST registration

**GST registration** says whether the company is GST-registered today and, if it is, the return
period today falls in. Below it is each registration Fyniq knows, with the dates it runs.

- An invoice dated on or after the day a registration starts is a tax invoice with GST; before it,
  or after the company deregisters, it carries no GST.
- Returns are quarterly unless IRAS set them monthly. Quarters start in January, April, July and
  October unless IRAS's notice gives other months.

1. Select **Record a registration**.
2. Enter the GST registration number and the date it starts, as IRAS's notice of registration
   gives them, and how often returns are filed.
3. Select **Record the registration**. Confirm it's you if Fyniq asks.

To correct a registration, or to record the day the company is deregistered, select **Change**
next to it. Two registrations cannot cover the same day.

## GST registration monitor

While the company is not registered, **Compliance › GST** tests whether it must register. IRAS
asks a business to register when its taxable supplies are more than $1 million in a calendar year,
or are expected to be more than $1 million in the next 12 months.

- **Warn when the year's taxable supplies reach** sets how early the monitor warns: 80% of the
  threshold unless you change it, from 50 to 99. The warning shows on Checks before either test is
  met.
- Taxable supplies are the standard-rated and zero-rated sales. Exempt and out-of-scope sales, and
  sales of the company's own assets, are left out.

## Cash buffer

**Cash buffer to keep** is how many months of average net burn the company wants to keep in the
bank: 6 unless you change it, from 0 to 36.

- A forecast's **funding gap** is the buffer times its average monthly net burn, less its lowest
  cash, and never below zero. A scenario can set its own buffer.
- The short-term cash flow draws a **buffer line** at the buffer times the average net burn of the
  last three closed months. Fyniq alerts the planners when the projection falls below it.

## Statutory calendar

**Compliance › Calendar** lists every filing and payment deadline from the company's financial years
and GST return periods. Three settings decide what it holds:

- **Pays staff**: turn it on from the first month the company pays anyone. From then, the calendar
  adds the CPF contributions of each month, due by the 14th of the next month, and each year's
  employment income for IR8A and the Auto-Inclusion Scheme, due by 1 March. It is off unless you turn it on.
- **Annual general meeting**: **hold** an AGM within six months after the year's end, the default; or
  **send the financial statements** to every member within five months, so no AGM is held; or
  **dispensed**, when all members passed a resolution to dispense with AGMs. A member may still ask for
  an AGM.
- **Keeps a branch register outside Singapore**: the annual return is due eight months after the
  year's end instead of seven. It is off unless you turn it on.

## Document numbering

**Document numbering** lists each kind of document Fyniq numbers, with the number the next one
takes, such as INV-0042.

1. Select **Change** next to the kind of document.
2. Set the prefix (up to 12 capital letters, digits, slashes or hyphens), the next number and how
   many digits it is padded to.
3. Choose when a document takes its number: **when it posts**, so there are no gaps, or **when it
   is saved**, so an abandoned draft leaves a gap.
4. Select **Save the numbering**.

- Numbers already given never change.
- The next number only moves forward, so no number is given twice.

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
  incoming-funds alerts expire, the AI assistant, details in notification email and the sign-out after inactivity
  need a second approver before they change: they show **Needs a second approver**.
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

## Advances account

Money advanced to a member before a trip waits on the **advances account** until their claims settle
it or they refund what is left: 1250 Staff & director advances in the standard chart.

- Choose another account by its code, such as a staff advances account you added to the chart.
- An advance, a refund of one, and a claim that settles one all post to it.
- A change needs a second approver, as the other accounting settings do.

## Claim deadline

A member claims what they spent within the **claim deadline**: 60 days from the receipt's or the
trip's date by default.

- A line older than that when the claim is submitted is **late**: the claim shows it, the member says
  why before submitting, and finance (the Controller) approves the claim as well as its usual
  approver.
- From 1 to 365 days. A change needs a second approver, as the other money settings do.

## Mileage rates

A member who drives their own car or motorcycle for the company claims the distance at the
organisation's **rate per km** for the vehicle. IRAS prescribes no rate per km, only a reasonable
one, so these are the company's own policy: Fyniq recommends **S$0.60** a km for a car and
**S$0.25** a km for a motorcycle, until the owner or the accountant changes them.

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

## Disposal account

When an asset is sold or written off, the difference between what it fetched and what it was still
worth is a gain or a loss. It goes to the **disposal account**: 7050 Gain / (loss) on disposal of
assets in the standard chart.

- Choose another account by its code, such as separate accounts your accountant prefers.
- A change needs a second approver, as the other accounting settings do.

## Disposal clearing account

An asset sold on a sales invoice is invoiced to the **disposal clearing account**: the invoice's line
goes to it, and the disposal clears it. The gain or loss then falls in the month of the disposal,
whatever the invoice's month.

- It is **1160 Asset disposal clearing** in the standard chart, unless the company's accountant names
  another balance-sheet account. Its balance should be nil at month end.
- A chart loaded before 1160 was added to the standard chart does not have it: add 1160 under
  **Accounting → Chart of accounts**, or name another account here, before the first asset is sold on
  an invoice.
- An asset sold for money received into a bank account does not use it: the disposal records the
  money, and bank import matches it.
- A change needs a second approver.

## Accounting sign-off

- A member with the Accountant role confirms the accounting framework and approves each accounting
  policy under **Settings → Accounting framework and policies**. While this setting is on, Fyniq first
  asks them to confirm it's them with their passkey or authenticator app, as for other approvals.
- It is on by default. Turning it off needs a second approver, like the other thresholds.

## Break-glass payment cap

- In Founder mode, while the other founder is unreachable, an Owner may start break-glass and
  release payment batches waiting for approval alone, up to this total: S$20,000 by default.
- The cap a break-glass starts with stays with it; a later change applies to the next one.
- Changing the cap needs a second approver.
- How break-glass works: [Team mode](team-mode.md#break-glass).

## Self-approval threshold

- In Founder mode a founder posts a journal of up to S$2,000 alone. It is flagged as self-approved
  and listed in the weekly review, where another founder reviews it.
- Above the threshold, posting a journal needs another founder's approval, and the one who prepared
  it never approves it.
- Changing the threshold needs a second approver.

## Sign out after inactivity

- A session ends after 30 minutes without using Fyniq, by default; sign in again to go on. Whatever
  happens, a session ends 12 hours after you signed in.
- It can be set from 5 to 60 minutes. Changing it needs a second approver.
- If you belong to several organisations, your session follows the organisation you are signed in to.

## Evidence rules

Some records need a file to show they are real before they post:

- **Evidence for bills**: a bill posts only with the supplier's invoice or receipt attached.
- **Evidence for manual journals**: a manual journal whose debits are above S$1,000 posts only with a
  supporting calculation or document attached. **Evidence for manual journals above** sets the amount.
- **Evidence for statutory filings and payments**: an item of the statutory calendar is recorded as
  filed only with its acknowledgement attached, such as IRAS's or ACRA's acknowledgement of a return,
  CPF Board's of a payment, or an AGM's minutes.

Each is **required** by default. **Recommended** lets the record post without a file, and **Checks**
lists it (missing recommended evidence). **Optional** asks for nothing. Changing any of these needs a
second approver. Attach files in the record's **Evidence** panel. **Accounting › Evidence** lists the
records still missing theirs ([Find evidence](evidence-search.md)).

## Variances at the close

Before a month closes, Fyniq compares each income and expense account's total for the month with
the previous month's. A change needs a short explanation when it is more than **both** of these:

- **Variance to explain, amount**: S$1,000.
- **Variance to explain, percentage of the previous month**: 10% of the previous month's total.

When the previous month's total is zero, the amount alone decides. The same thresholds apply to
the comparison with the month's budget, once its financial year has an approved one. Changing
either needs a second approver. See [Explain the variances](periods-and-close.md#explain-the-variances).

## Founders' money

What the company owes a founder (money lent, costs paid personally, pay or fees owed) is settled
by one of four routes: keep it as a loan, repay it, convert it to shares, or waive it. Two settings,
each needing a second approver, set what the Settle a balance page offers first, and a third flags a
conversion ACRA has not registered:

- **Default route:** recommended *keep*, an interest-free loan repayable on demand until the first
  priced round. No tax or CPF falls on money never received, and nobody's ownership moves at an
  arbitrary price.
- **Default price basis** to convert to shares: recommended *latest priced round*, the price outside
  investors paid; *valuation* when there is none.
- **Flag a conversion not registered by ACRA after:** recommended 14 days, matching ACRA's
  guidance for a return of allotment. The Act sets no deadline for private companies. This one
  needs no second approver.

The founders and the accountant still choose each settlement, which another person approves.

## Data region and retention

- **Data region**: every record is kept and processed in Google Cloud's Singapore region. The
  platform sets it, so it is shown and cannot be changed.
- **Records kept for**: documents and files are kept 7 years after the end of the financial year they relate to, and
  the ledger and the audit trail at least as long. It can be lengthened, never shortened.
- **Purge the ledger and the audit trail after retention**: off. While it is off, the ledger and the audit trail are
  kept for the life of the company. Switched on, a financial year is purged once **Records kept for** has passed since
  it ended, oldest year first:
  - its journal entries, with every balance carried forward to the next year, so each report from then on is
    unchanged;
  - the audit events of that time.

  A signed record of what was purged comes first. A report dated before it, or as at an earlier ledger revision, can no
  longer be run. A purge cannot be undone. Each purge is listed under **Purges and restores** on the audit trail.
- **Security events' IP address and device kept for**: 400 days. A sign-in, revealing a full bank number and an export
  record the IP address and device they came from. After this many days those two details are erased; the event stays
  in the audit trail.
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
