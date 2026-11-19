---
key: settings.organisation
title: Organisation settings
mode: basic
roles: [owner, admin, controller]
requirements: [CMP-PD-01, FM-8, FR-ORG-03, FR-ORG-04, FR-TAX-05, FR-TAX-07, FR-ORG-06, FR-ORG-07, FR-ORG-08, FR-GL-14, FR-AP-13, FR-AP-15, FR-AP-16, FR-AP-17, FR-AR-05, FR-BNK-16, FR-FA-03, AUD-06, AUD-10, AI-32, AI-35, AI-43, AI-90]
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

## Data Protection Officer

Under the PDPA the company names a Data Protection Officer (DPO) and makes their business contact public. In **Edit
the profile**, choose the member who is the DPO, normally the one who holds the Compliance officer role, and enter the
name and business email address the company publishes for them. If the DPO is an outside firm, choose no member and
enter its name and email.

Once the contact is saved:
- every email Fyniq sends ends with it;
- documents give it in their footer when their template is the default one, or a version published after it was saved;
  documents already issued stay as they were;
- the **Data Protection Officer** card shows the text to publish on the company's website, with **Copy the text**.

A member named as the DPO needs the name and email too. If they do not hold the Compliance officer role, the card warns
you: they can be the DPO, but they cannot keep the data protection registers until they hold it.

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
  incoming-funds alerts expire, the AI assistant, the AI's access to pay, the fields it never reads, its limits and
  the changes it may propose, AI apps and the limits on them, details in notification email and the sign-out after inactivity need a second approver before they change: they show
  **Needs a second approver**.
- For one of these, the button says **Ask for approval** instead of **Save**.
- Saving a change to one asks for approval. The setting keeps its value until another member who
  approves settings, such as an Owner, approves it (see [Approve a change](approvals.md)).
- The AI assistant is the exception when you switch it **off**: that takes effect at once, with no second
  approver, and the AI stops: new requests are refused, and a reply being written stops within seconds.
  Switching it back on still needs a second approver. AI payroll access turns off at once the same way, and so does
  taking a change off the changes the AI may propose.

## Features

Switch off what the company does not use, and Fyniq hides it from menus and pages.

| Feature | Recommended | Why |
|---|---|---|
| Multi-currency | On | Start-ups usually pay some bills in other currencies, such as cloud and software subscriptions in US dollars |
| Payroll | Off | Off while the company has no employees and the founders draw no salary; turn it on before the first pay run |
| Projects and time | Off | Off until you track time or costs by project |
| Portals for customers and investors | Off | Off until there are customers or investors to invite |
| AI assistant | Off | AI stays off until an Owner turns it on with a second approver; switching it off takes effect at once |
| AI apps through MCP | Off | No AI app reaches your books until an Owner turns it on with a second approver; see [AI apps through MCP](#ai-apps-through-mcp) |

## Motion

Fyniq uses a few short transitions so you can follow what changed: a page or panel fading in, rows
entering, a count changing. An Administrator can turn **Motion** off for the whole organisation; every
screen then shows its final state at once, for everyone. It is on by default, and the change is kept in
the setting's history.

Each person can still choose **Reduce motion** in the Appearance menu, and a device set to reduce motion
always wins.

## AI routes

- Each AI agent runs on a route: a model on a connection first, then the fallbacks it may use when the first fails.
- **Fallbacks a day before a route needs attention**: 5 unless changed, from 1 to 1,000. When an agent's route falls
  back more often than this in a day, **Checks** shows it, so someone looks at the route's first step.
- Changing it needs a second approver.

## AI and personal data

- Some data never reaches the AI, whatever is set here: NRIC and FIN numbers, full bank account numbers, passwords and
  other secrets, and home addresses. Fyniq takes them out of everything the assistant reads, and masks an NRIC or a
  bank account number it finds in text, keeping only its last characters.
- **AI payroll access**: off unless changed. Pay, such as salaries, stays away from the AI. Turned on, the AI reads pay
  only for people who may see payroll themselves. Turning it on needs a second approver; turning it off takes effect
  at once.
- **Fields the AI never reads**: none unless changed. Add field names of your own, separated by commas, such as
  `notes, memo`, to keep them from the AI too. You can add to the list, never take a field off the list above.
  Changing it needs a second approver.

## AI limits

- The assistant answers within limits, so a script or a slip cannot run up the AI's cost. Each needs a second approver
  to change.
- **Questions a person may ask the assistant a minute**: 20 unless changed, from 1 to 1,000. **A day**: 200 unless
  changed, from 1 to 100,000, counted over the last 24 hours. Past either, the assistant says so and answers again
  once it eases.
- **Reads of the books for one question**: 8 unless changed, from 1 to 20. A question needing more is answered in
  part, saying so; ask about one thing at a time.
- **Questions in one conversation**: 40 unless changed, from 2 to 200. Past 20, the oldest questions are summarised,
  keeping the figures they cited; at the limit, start a new conversation.

## AI task types

- The AI never changes the books itself. It may only propose a change of a kind this release lists, a **task type**.
  A member who may approve that kind of change decides, and only then does Fyniq make it, in that member's name.
- **Changes the AI may propose**: none unless changed. Enter the task types to allow, separated by commas, as the
  release names them, such as `sec.create_task`. A task type the release does not have is refused.
- Adding a task type needs a second approver. Taking one off, or all of them, takes effect at once, with no second
  approver: the AI stops offering it.
- Some changes can never be proposed, whatever is set here: approving or rejecting anything, payments, payee bank
  details, users, roles and team mode, approval policies, the AI's own settings, statutory parameters, reopening a
  period, voiding or deleting, filing with an authority, exporting the company's data, and the audit trail.

## AI apps through MCP

- Some AI apps, such as Claude Code or Claude Desktop, can work with your books on a person's behalf over the Model
  Context Protocol (MCP), using the same read tools as the assistant in Fyniq. They can read what that person may read,
  and propose changes for a person to approve in Fyniq; they cannot approve, pay, export or change settings.
- **AI apps through MCP**: off unless changed. Turning it on needs a second approver; turning it off takes effect at
  once and cuts every AI app off.
- **AI apps that may connect**: none unless changed. Enter the apps to allow, such as `claude-code`, separated by
  commas. You can list the apps this release has registered, and an app that registered itself, by the ID shown when it
  asked; an app that is not listed cannot connect. Adding an app needs a second approver. Taking one off takes effect at
  once, with no second approver, and cuts that app off.
- **AI apps may register themselves**: off unless changed. An AI app can ask to be registered, on its own, only while an
  Owner has turned this on with a second approver. A registered app is limited to the default read access, is not
  verified, and cannot connect until an Owner adds it to the apps that may connect. A registration nobody approves
  expires after 7 days, and an approved app nobody uses after 30. Turning it off takes effect at once.
- Each person also has to opt in before an app can act for them, and sees what the app may read when they allow it.
- This release only holds these settings. No AI app can connect until the MCP server is opened for the deployment, which
  is done separately, after a security review.

## AI app limits

- An AI app works within limits, so a script or a slip cannot run up load or cost. Each needs a second approver to
  change, and each has a highest value the platform allows.
- **Requests a person's AI app may make a minute**: 60 unless changed, from 1 to 600. **Reads and proposals an hour**:
  600 unless changed, from 1 to 10,000. **A day**: 5,000 unless changed, from 1 to 100,000. Past one, the app is told
  to wait and works again once it eases.
- **Proposals an AI app may make for a person in an hour**: 30 unless changed, from 1 to 1,000.
- **The most rows one answer may hold**: 500 unless changed, from 1 to 5,000. **The most kilobytes**: 256 unless
  changed, from 16 to 1,024. A tool's own limits are lower; an answer larger than a limit is cut, and what was left out is
  counted and said.

## Company secretary assistant

- The **company secretary assistant** checks the company's obligations on a schedule, reports, raises tasks and drafts
  reminders for people to approve. It is not the company secretary, and it never files, signs or marks an obligation
  as met.
- **Company secretary assistant**: off unless changed. Turning it on needs a second approver; its runs then act for
  the Owner who turned it on. Turning it off takes effect at once.
- **Paused**: pausing stops its runs at once and keeps its schedule. The statutory calendar's reminders keep coming.
- Its schedule, in Singapore time:
  - **Deadline and lodgement monitor**: daily at 07:00 (`0 7 * * *`);
  - **Full cross-check**: Mondays at 08:00 (`0 8 * * 1`);
  - **Run after each month's close** and **Run one month after the financial year ends**: on.
  A schedule is five cron fields, minute hour day month weekday, or `off`.
- **Events that start a run**: a period closed, shares issued or transferred, a filing recorded, and deadline windows,
  30, 14, 7 and 1 days ahead.
- **Quiet hours**: 22:00 to 07:00. A run in them still runs and records; its emails wait until they end.
- **Who it tells** (the Owner and the Company secretary) and **How it tells them** (the Inbox and email). Its tasks
  belong to the first of these roles. **Tell the Owner this many days before a deadline**: 7.
- **Reads one run may make**: 20. **Tokens one run may use**: 200000, beside the agent's monthly budget in AI settings.
- Every one of these, except pausing, needs a second approver.

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

## Lost disputes

When a customer's card payment is disputed and lost, the money Stripe took back waits in your **disputes** account. Fyniq
prepares a draft journal that writes it off to your **dispute losses** account, and you post it like any other journal.

- **Lost disputes written off as an expense, up to**: S$100. Up to this amount the draft needs nothing more. Above it, you
  confirm that you are writing off more than this and say why, because the invoice's revenue stays as invoiced until you
  credit it. A dispute fee alone is about S$15, so under S$100 it costs more to find the invoice than to write it off.
- Ask your accountant about the GST on a lost dispute and about amounts above the limit.
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

## Hold after an account recovery

- After a person recovers their account, they cannot approve, release payments, change bank details,
  roles or sign-ins, use break-glass or confirm others' recoveries for 24 hours, by default.
- It can be set from 24 to 72 hours. Changing it needs a second approver.
- If they belong to several organisations, the longest hold of theirs applies.

## Evidence rules

Some records need a file to show they are real before they post:

- **Evidence for bills**: a bill posts only with the supplier's invoice or receipt attached.
- **Evidence for manual journals**: a manual journal whose debits are above S$1,000 posts only with a
  supporting calculation or document attached. **Evidence for manual journals above** sets the amount.
- **Largest evidence file**: a file larger than this is refused when you attach it. It is 25 MB by
  default, which is the most Fyniq takes; you can lower it, and a change takes effect at once.
- **Evidence for statutory filings and payments**: an item of the statutory calendar is recorded as
  filed only with its acknowledgement attached, such as IRAS's or ACRA's acknowledgement of a return,
  CPF Board's of a payment, or an AGM's minutes.

Each is **required** by default. **Recommended** lets the record post without a file, and **Checks**
lists it (missing recommended evidence). **Optional** asks for nothing. Changing any of these needs a
second approver. Attach files in the record's **Evidence** panel. **Accounting › Evidence** lists the
records still missing theirs ([Find evidence](evidence-search.md)).

### What each record's panel shows

A record's **Evidence** panel lists **Rules for this activity**: what the record rests on, with a
cross and **Required** while a required item has no file, a warning and **Recommended** while a
recommended one has none, and **Attached** once a file meets it. A bill, an expense claim and a
founder's cost are recommended to carry a business purpose note; a change of a supplier's bank
details, a call-back note; a manual journal, an explanation; a filing, its working papers. Say what a
file is when you attach it (for example "business purpose note" or "explanation") and it meets that
item. Where your organisation made evidence optional, the panel says it asks for none.

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

**Settings → Data and export** shows these read only, beside the full export of everything the organisation keeps
([See where your data lives, and take a full export](data-export.md)).

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
