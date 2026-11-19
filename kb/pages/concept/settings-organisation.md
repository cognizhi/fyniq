---
key: settings.organisation
type: concept
title: Organisation settings and their recommended values
summary: Company-wide settings: GST and its monitor, cash buffer, numbering, features, AI, terms, tolerances, banking, claims, thresholds, disposals, email-in, retention
sources:
  - docs/user-guide/organisation-settings.md sha256:7f86a43f3739d814e511cf4503b70459cdf95ebfee0a33ce3b66d36efa8e080a
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - company secretary assistant
  - scheduled agent
  - pause the agent
  - quiet hours
  - AI payroll access
  - AI task types
  - changes the AI may propose
  - AI apps through MCP
  - connect Claude Code
  - connect Claude Desktop
  - AI app limits
  - let the AI propose changes
  - assistant limits
  - too many questions
  - fields the AI never reads
  - what the AI never sees
  - turn off animations
  - motion
  - data protection officer
  - DPO contact
  - GST registration
  - GST registration number
  - GST registration threshold
  - must I register for GST
  - statutory calendar
  - cash buffer
  - funding gap
  - months of burn
  - pays staff
  - AGM
  - branch register
  - deregistered
  - document numbering
  - invoice number prefix
  - next number
  - gap-free numbering
  - feature switches
  - switch off the AI
  - AI kill switch
  - AI fallbacks
  - route falls back
  - default settings
  - tolerance
  - retention period
  - purge after retention
  - IP address kept
  - data residency
  - self-approval threshold
  - idle timeout
  - evidence rules
  - required evidence
  - signed out automatically
  - break-glass payment cap
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

- **Settings → Organisation** (Expert mode) holds the profile documents print (their look is the templates', [[settings.templates]], and the logo is [[settings.brand]]'s) and the choices that apply to the whole company.
- Each setting starts at its recommended value, with a reason and a link to the guide.
- It shows **Recommended** while it holds that value, **Changed from recommended** once it differs.
- The Owner, the Administrator and the Controller change them; others in the company can see them. How: [[settings.change]].

(src: docs/user-guide/organisation-settings.md#what-it-is)

## Data Protection Officer

- The company names a Data Protection Officer (DPO) and makes their business contact public (PDPA). In **Edit the profile**, choose the member who is the DPO, normally the Compliance officer, and enter the name and business email the company publishes; for an outside firm, choose no member and enter its name and email.
- Once saved, every email Fyniq sends ends with the contact, and documents give it in their footer when their template is the default or a version published after it was saved; issued documents stay as they were.
- The **Data Protection Officer** card shows the text for the company's website, with **Copy the text**, and warns when the member named does not hold the Compliance officer role.

(src: docs/user-guide/organisation-settings.md#data-protection-officer)

## GST registration

- **GST registration** says whether the company is GST-registered today and the return period today falls in, and lists each registration with its dates.
- An invoice dated on or after a registration's first day is a tax invoice with GST; before it, or after deregistration, it carries no GST.
- **Record a registration** takes the GST registration number and start date from IRAS's notice of registration, and how often returns are filed (quarterly unless IRAS set monthly; quarters from January, April, July and October unless the notice says otherwise); **Change** corrects one or records the day it ends. Two registrations never cover the same day.

(src: docs/user-guide/organisation-settings.md#gst-registration)

## GST registration monitor

- While the company is not registered, **Compliance › GST** tests whether it must register: IRAS asks a business to register when its taxable supplies are more than $1 million in a calendar year, or are expected to be more than $1 million in the next 12 months.
- **Warn when the year's taxable supplies reach** sets how early the monitor warns on Checks, before either test is met: 80% of the threshold unless changed, from 50 to 99.
- Taxable supplies are the standard-rated and zero-rated sales; exempt and out-of-scope sales, and sales of the company's own assets, are left out.

The monitor itself: [[compliance.gst]].

(src: docs/user-guide/organisation-settings.md#gst-registration-monitor)

## Cash buffer

- **Cash buffer to keep** is how many months of average net burn the company wants to keep in the bank: 6 unless changed, from 0 to 36.
- A forecast's **funding gap** is the buffer times its average monthly net burn, less its lowest cash, never below zero; a scenario can set its own buffer.
- The short-term cash flow's **buffer line** is the buffer times the average net burn of the last three closed months, and Fyniq alerts the planners when the projection falls below it.

(src: docs/user-guide/organisation-settings.md#cash-buffer)

## Statutory calendar

- **Compliance › Calendar** lists every filing and payment deadline from the company's financial years and GST return periods; three settings decide what it holds.
- **Pays staff**: from the first month the company pays anyone, the calendar adds each month's CPF contributions, due by the 14th of the next month, and each year's employment income for IR8A and the Auto-Inclusion Scheme, due by 1 March; off unless turned on.
- **Annual general meeting**: **hold** an AGM within six months after the year's end (the default), **send the financial statements** to every member within five months so no AGM is held, or **dispensed** when all members resolved to dispense with AGMs; a member may still ask for an AGM.
- **Keeps a branch register outside Singapore**: the annual return is due eight months after the year's end instead of seven; off unless turned on.

The calendar itself: [[compliance.calendar]].

(src: docs/user-guide/organisation-settings.md#statutory-calendar)

## Document numbering

- **Document numbering** lists each kind of numbered document with the number the next one takes, such as INV-0042.
- **Change** sets the prefix (up to 12 capital letters, digits, slashes or hyphens), the next number, the digits it is padded to, and whether a document takes its number when it posts (no gaps) or when it is saved (an abandoned draft leaves a gap).
- Numbers already given never change, and the next number only moves forward, so no number is given twice.

(src: docs/user-guide/organisation-settings.md#document-numbering)

## Features

- Switching a feature off hides it from menus and pages.
- Recommended: **Multi-currency** on; **Payroll**, **Projects and time** and **Portals for customers and investors** off.
- The **AI assistant** is off until an Owner turns it on with a second approver; switching it off takes effect at once ([[settings.ai]]).
- **AI apps through MCP** is off until an Owner turns it on with a second approver; switching it off takes effect at once.

(src: docs/user-guide/organisation-settings.md#features)

## Motion

- Short transitions help people follow what changed: a page or panel fading in, rows entering, a count changing.
- An Administrator can turn **Motion** off for the whole organisation: every screen then shows its final state at once, for everyone. It is on by default.
- Each person can still choose **Reduce motion** in the Appearance menu, and a device set to reduce motion always wins.

(src: docs/user-guide/organisation-settings.md#motion)

## AI routes

- Each AI agent runs on a route: a model on a connection first, then the fallbacks it may use when the first fails.
- **Fallbacks a day before a route needs attention**: 5 unless changed, from 1 to 1,000. When an agent's route falls back more often than this in a day, **Checks** shows it ([[checks.overview]]), so someone looks at the route's first step.
- Changing it needs a second approver.

(src: docs/user-guide/organisation-settings.md#ai-routes)

## AI and personal data

- Some data never reaches the AI, whatever is set: NRIC and FIN numbers, full bank account numbers, passwords and other secrets, and home addresses. Fyniq takes them out of everything the assistant reads, and masks an NRIC or a bank account number found in text, keeping only its last characters.
- **AI payroll access**: off unless changed. Pay, such as salaries, stays away from the AI; turned on, the AI reads pay only for people who may see payroll themselves. Turning it on needs a second approver; turning it off takes effect at once.
- **Fields the AI never reads**: none unless changed. Add field names of your own, separated by commas, such as `notes, memo`, to keep them from the AI too; a field can be added to the list, never taken off the never-send list. Changing it needs a second approver.

(src: docs/user-guide/organisation-settings.md#ai-and-personal-data)

## AI limits

- The assistant answers within limits, so a script or a slip cannot run up the AI's cost; each needs a second approver to change.
- **Questions a person may ask the assistant a minute**: 20 unless changed, from 1 to 1,000. **A day**: 200 unless changed, from 1 to 100,000, counted over the last 24 hours. Past either, the assistant says so and answers again once it eases.
- **Reads of the books for one question**: 8 unless changed, from 1 to 20. A question needing more is answered in part, saying so; ask about one thing at a time.
- **Questions in one conversation**: 40 unless changed, from 2 to 200. Past 20, the oldest questions are summarised, keeping the figures they cited; at the limit, start a new conversation.

(src: docs/user-guide/organisation-settings.md#ai-limits)

## AI task types

- The AI never changes the books itself: it may only propose a change of a kind the release lists, a **task type**. A member who may approve that kind of change decides, and only then does Fyniq make it, in that member's name ([[ai.proposals]]).
- **Changes the AI may propose**: none unless changed. Enter the task types to allow, separated by commas, as the release names them, such as `sec.create_task`; a task type the release does not have is refused.
- Adding a task type needs a second approver. Taking one off, or all of them, takes effect at once, with no second approver: the AI stops offering it.
- Some changes can never be proposed, whatever is set here: approving or rejecting anything, payments, payee bank details, users, roles and team mode, approval policies, the AI's own settings, statutory parameters, reopening a period, voiding or deleting, filing with an authority, exporting the company's data, and the audit trail.

(src: docs/user-guide/organisation-settings.md#ai-task-types)

## AI apps through MCP

- Some AI apps, such as Claude Code or Claude Desktop, can work with the books on a person's behalf over the Model Context Protocol (MCP), using the same read tools as the assistant in Fyniq. They can read what that person may read and propose changes for a person to approve in Fyniq; they cannot approve, pay, export or change settings.
- **AI apps through MCP**: off unless changed. Turning it on needs a second approver; turning it off takes effect at once and cuts every AI app off.
- **AI apps that may connect**: none unless changed. Enter the apps to allow, such as `claude-code`, separated by commas; you can list the apps the release has registered, and an app that registered itself by the ID shown when it asked, and an app that is not listed cannot connect. Adding an app needs a second approver; taking one off takes effect at once, with no second approver, and cuts that app off.
- **AI apps may register themselves**: off unless changed. An AI app can ask to be registered, on its own, only while an Owner has turned this on with a second approver. A registered app is limited to the default read access, is not verified, and cannot connect until an Owner adds it to the apps that may connect. A registration nobody approves expires after 7 days, and an approved app nobody uses after 30. Turning it off takes effect at once.
- Each person also has to opt in before an app can act for them, and sees what the app may read when they allow it.
- The release only holds these settings: no AI app can connect until the MCP server is opened for the deployment, separately, after a security review. Each person connects and disconnects apps under **Connected apps** ([[settings.connected-apps]]).

(src: docs/user-guide/organisation-settings.md#ai-apps-through-mcp)

## AI app limits

- An AI app works within limits, so a script or a slip cannot run up load or cost; each needs a second approver to change and has a highest value the platform allows.
- **Requests a person's AI app may make a minute**: 60 unless changed, from 1 to 600. **Reads and proposals an hour**: 600 unless changed, from 1 to 10,000. **A day**: 5,000 unless changed, from 1 to 100,000. Past one, the app is told to wait and works again once it eases.
- **Proposals an AI app may make for a person in an hour**: 30 unless changed, from 1 to 1,000.
- **The most rows one answer may hold**: 500 unless changed, from 1 to 5,000. **The most kilobytes**: 256 unless changed, from 16 to 1,024. A tool's own limits are lower; an answer larger than a limit is cut, and what was left out is counted and said.

(src: docs/user-guide/organisation-settings.md#ai-app-limits)

## Company secretary assistant

- The company secretary assistant checks the company's obligations on a schedule, reports, raises tasks and drafts reminders for people to approve ([[ai.proposals]]); its workspace is under Compliance ([[compliance.secretary]]). It is not the company secretary and never files, signs or marks an obligation as met.
- **Company secretary assistant**: off unless changed; turning it on needs a second approver, and its runs then act for the Owner who turned it on. Turning it off takes effect at once.
- **Paused** stops its runs at once and keeps its schedule; the statutory calendar's reminders keep coming.
- Its schedule, in Singapore time: the **Deadline and lodgement monitor** daily at 07:00, the **Full cross-check** on Mondays at 08:00, and runs after each month's close and one month after the financial year ends. A schedule is five cron fields, or `off`.
- **Events that start a run**: a period closed, shares issued or transferred, a filing recorded, and deadline windows 30, 14, 7 and 1 days ahead.
- **Quiet hours** (22:00 to 07:00): a run still runs and records; its emails wait until they end.
- **Who it tells** and **How it tells them** (the Inbox and email); its tasks belong to the first of those roles. The Owner hears of a deadline 7 days ahead.
- **Reads one run may make** (20) and **Tokens one run may use** (200000) cap a run, beside the agent's monthly budget.
- Every one of these, except pausing, needs a second approver.

(src: docs/user-guide/organisation-settings.md#company-secretary-assistant)

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

## Lost disputes

- When a customer's card payment is disputed and lost, the money the provider took back waits in the **disputes** account. Fyniq prepares a draft journal that writes it off to the **dispute losses** account, and it is posted like any other journal.
- **Lost disputes written off as an expense, up to**: S$100. Up to this amount the draft needs nothing more; above it, the person confirms that they are writing off more than this and says why, because the invoice's revenue stays as invoiced until it is credited.
- A dispute fee alone is about S$15, so under S$100 it costs more to find the invoice than to write it off. The accountant is asked about the GST on a lost dispute and about amounts above the limit.
- A change needs a second approver, as the other accounting settings do.

(src: docs/user-guide/organisation-settings.md#lost-disputes)

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
- IRAS prescribes no rate per km, only a reasonable one, so the rates are the company's own policy: recommended **S$0.60** a km for a car and **S$0.25** a km for a motorcycle, until the owner or the accountant changes them.
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
- It is **1160 Asset disposal clearing** in the standard chart unless the accountant names another balance-sheet account; its balance should be nil at month end.
- A chart loaded before 1160 joined the standard chart lacks it: add 1160 under **Accounting → Chart of accounts**, or name another account, before the first asset is sold on an invoice.
- An asset sold for money received into a bank account does not use it: the disposal records the money, and bank import matches it. A change needs a second approver.

(src: docs/user-guide/organisation-settings.md#disposal-clearing-account)

## Accounting sign-off

- While this setting is on, a member with the Accountant role confirms the accounting framework and approves each accounting policy only after confirming it's them with a passkey or authenticator app, as for other approvals ([[settings.accounting]]).
- It is on by default; turning it off needs a second approver, like the other thresholds.

(src: docs/user-guide/organisation-settings.md#accounting-sign-off)

## Break-glass payment cap

- In Founder mode, while the other founder is unreachable, an Owner may start break-glass and release payment batches waiting for approval alone, up to this total: S$20,000 by default ([[settings.team-mode]]).
- The cap a break-glass starts with stays with it; a later change applies to the next one.
- Changing the cap needs a second approver.

(src: docs/user-guide/organisation-settings.md#break-glass-payment-cap)

## Evidence rules

- **Evidence for bills**: a bill posts only with the supplier's invoice or receipt attached.
- **Evidence for manual journals**: a manual journal whose debits are above S$1,000 posts only with a supporting calculation or document attached; **Evidence for manual journals above** sets the amount.
- **Evidence for statutory filings and payments**: an item of the statutory calendar is recorded as filed only with its acknowledgement attached, such as IRAS's or ACRA's acknowledgement of a return, CPF Board's of a payment, or an AGM's minutes.
- Each is **required** by default; **recommended** lets the record post without a file and **Checks** lists it; **optional** asks for nothing. Changing any of them needs a second approver.
- A record's **Evidence** panel lists **Rules for this activity**: a cross and **Required** while a required item has no file, a warning and **Recommended** while a recommended one has none, and **Attached** once a file meets it. A bill, an expense claim and a founder's cost are recommended to carry a business purpose note; a change of a supplier's bank details, a call-back note; a manual journal, an explanation; a filing, its working papers. Say what a file is when you attach it and it meets that item. Where evidence is optional, the panel says it asks for none.
- Files go in the record's **Evidence** panel ([[purchases.bills]], [[accounting.journals]]); **Accounting › Evidence** lists the records still missing theirs ([[compliance.evidence]]).

(src: docs/user-guide/organisation-settings.md#evidence-rules)

## Self-approval threshold

- In Founder mode a founder posts a journal of up to S$2,000 alone; it is flagged as self-approved and listed in the weekly review, where another founder reviews it.
- Above the threshold, posting a journal needs another founder's approval, and the one who prepared it never approves it ([[approvals.decide]]).
- Changing the threshold needs a second approver.

(src: docs/user-guide/organisation-settings.md#self-approval-threshold)

## Sign out after inactivity

- A session ends after 30 minutes without using Fyniq, by default; sign in again to go on. Whatever happens, a session ends 12 hours after signing in.
- It can be set from 5 to 60 minutes; changing it needs a second approver.
- Someone in several organisations has a session that follows the organisation they are signed in to ([[preferences.security]]).

(src: docs/user-guide/organisation-settings.md#sign-out-after-inactivity)

## Hold after an account recovery

- After a person recovers their account, they cannot approve, release payments, change bank details, roles or sign-ins, use break-glass or confirm others' recoveries for 24 hours, by default.
- It can be set from 24 to 72 hours; changing it needs a second approver.
- Someone in several organisations is held for the longest of theirs.

(src: docs/user-guide/organisation-settings.md#hold-after-an-account-recovery)

## Variances at the close

- Before a month closes, each income and expense account's total for the month is compared with the previous month's.
- A change needs a short explanation when it is more than **both** S$1,000 and 10% of the previous month's total; when the previous month's total is zero, the amount alone decides.
- The same thresholds apply to the comparison with the month's budget, once its financial year has an approved one.
- Changing either needs a second approver. Explaining them at the close: [[accounting.periods]].

(src: docs/user-guide/organisation-settings.md#variances-at-the-close)

## Founders' money

- What the company owes a founder is settled by one of four routes: keep it as a loan, repay it,
  convert it to shares, or waive it; the Settle a balance page offers the defaults first, and
  another person approves each settlement.
  (src: docs/user-guide/organisation-settings.md#founders-money)
- **Default route:** recommended *keep*, an interest-free loan repayable on demand until the first
  priced round. **Default price basis** to convert: recommended *latest priced round*, or
  *valuation* when there is none. Each change needs a second approver.
  (src: docs/user-guide/organisation-settings.md#founders-money)
- **Flag a conversion not registered by ACRA after:** recommended 14 days, matching ACRA's guidance
  for a return of allotment; no second approver.
  (src: docs/user-guide/organisation-settings.md#founders-money)

## Data region and retention

- Data region: Google Cloud's Singapore region, set by the platform and read-only.
- Records kept for: documents and files 7 years after the end of their financial year, the ledger and the audit trail at least as long; this only lengthens.
- Purge the ledger and the audit trail after retention: off, so both are kept for the life of the company. Switched on, the oldest year past its retention is purged, its balances carried forward so later reports are unchanged, after a signed record of what goes; reports before it can no longer be run, and a purge cannot be undone. Each purge is listed on the audit trail ([[compliance.audit-trail]]).
- Security events' IP address and device kept for: 400 days. A sign-in, revealing a full bank number and an export keep them that long, then they are erased and the event stays.
- AI conversations are kept 365 days; proposals stay with the audit trail.
- **Settings → Data and export** shows these read only, beside the full export of everything the organisation keeps ([[settings.data]]).

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
