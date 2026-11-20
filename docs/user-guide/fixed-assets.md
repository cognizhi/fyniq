---
key: accounting.fixed-assets
title: Keep the fixed asset register
mode: expert
roles: [accountant, controller, owner, bookkeeper, auditor, tax_agent]
requirements: [FR-FA-01, FR-FA-02, FR-FA-03, FR-FA-04, INV-07]
status: draft            # written with the fixed asset register (P1); reviewed with the accountant's chart-of-accounts review
---

# Keep the fixed asset register

## What it is

**Accounting → Fixed assets** lists the things the company owns and uses for more than a year:
laptops, furniture, a fit-out, purchased software. Each asset shows its category (the asset account
it sits on, such as 1510 Computer equipment), what it cost, its useful life, its accumulated
depreciation and its net book value: what it is still worth in the books.

- An Accountant registers, changes and disposes of assets. The Owner, the Controller, the
  Bookkeeper, the Auditor and the Tax agent can see them.
- The page is in Expert mode.
- Registering an asset posts nothing: its cost is already on the asset account, put there by the
  bill or bank line that bought it.

## Register an asset

An asset is registered from the line of a posted bill, or of a categorised bank line, that put its
cost on an asset account.

1. Open **Not in the register yet**. It lists every such line no asset was registered from, with
   ⚠ **At or above** when its cost is at or above the capitalisation threshold (S$1,000 unless
   changed in Settings → Organisation).
2. Choose **Register** on the line.
3. Check its useful life in months: the form offers the asset account's default life, such as 36
   for 1510 Computer equipment, and you change it if this asset lasts longer or shorter. Enter its
   residual value if it will be worth something at the end. Its cost is what the line put on the account, in Singapore dollars,
   whatever the bill's currency.
4. Depreciation starts in the month it was bought; choose a later month if it went into use later.

A posted bill with such a line shows ⚠ until the line is registered, with a link to register it. A
line is registered once.

## Assets bought before the books start

An asset the company had before its books in Fyniq start is in the opening balances: its cost on
the asset account and its accumulated depreciation on 1590. Choose **Register one bought earlier**
and enter both, as the opening balances carry them, with the day it was bought. Its depreciation
posts from the books start; the months before it are in the opening balances.

## Depreciation, month by month

Each month, on its last day, an asset's depreciation posts by itself: Dr 6610 Depreciation, Cr 1590
Accumulated depreciation. It is the cost less the residual value, spread evenly over the months of
its life; the cents are spread so the months add up exactly.

- The asset's schedule shows each month **Posted**, **Waiting** or **Projected**. A month in the
  future is never posted.
- A month **Waiting** has ended but its period is not open. Open the period, and it posts within the
  hour; the months after it wait for it.
- Registering an asset late posts its months already due at once.

## Change an asset

Choose **Change** to rename it, or to change its useful life or residual value. A new life or
residual value applies to the months not posted yet: what has posted stays, and what is left is
spread over the months left. Its start month, and the figures of one bought before the books start,
change only while none of its months has posted.

## Sell or write off an asset

Choose **Sell or write off**, then:

- **The whole asset or part of it.** For part, enter the part of its cost that went; its
  depreciation goes with it in proportion, and the rest depreciates on.
- **The proceeds.** Money received into a bank, cash or wallet account, with its GST code when the
  company is GST-registered (usually SR); a code given is always one for supplies (SR, ZR, ES or
  OS). The bank line is matched to it later. Or a sales invoice
  to the buyer, whose line is on the disposal clearing account (1160 Asset disposal clearing in the
  standard chart). Or none, for a write-off.

**Check** shows the gain or loss and the entry before anything is saved; **Record** posts it. The
gain or loss goes to the disposal account, 7050 Gain / (loss) on disposal of assets. Every month of
depreciation before the disposal's month must have posted. A disposal recorded late, after its
month's depreciation posted, reverses that month.

## Void a disposal

A disposal recorded by mistake is voided, with a reason: its entry is reversed and the asset is as
it was, depreciating again from the disposal's month. In Founder mode another person approves the
void. A disposal is voided only before the asset changes again, or, after a part was sold, before
what was left depreciates again.

## Void an asset registered by mistake

An asset registered from the wrong line, or something that was never an asset, is voided from its
panel with **Void the asset** and a reason.

- Each month of its depreciation that posted is reversed on its own date, so those months are as
  they were before it was registered.
- It leaves the register and is listed under **Voided**, with its months and their reversals.
- Its bill or bank line shows again under **Not in the register yet**, to register as it should be.
- A void waits while a disposal of the asset stands: void the disposal first. It waits too while a
  month to reverse is in a closed period: reopen that period first.
- In Founder mode another person approves the void.
- An asset that was sold, scrapped or lost is not voided: choose **Sell or write off**, so its gain
  or loss shows.

## The movement schedule

**Movement** shows, for a run of months, each asset's cost and accumulated depreciation at the start, what was added by an asset bought in the run, what the depreciation charged, what a disposal took off, and the cost and accumulated depreciation at the end, with the net book value. It is the schedule for the financial statements, by asset account.

- The columns foot exactly: closing cost is opening cost plus additions less disposals, and closing accumulated depreciation is opening plus the charge less disposals.
- The register is tied to the ledger at the end of the run. When it does not tie, the schedule says what is unexplained, such as a purchase on an asset account that is not in the register yet. It never hides the difference.

## The capital allowance schedule

**Capital allowances** is the tax schedule of a year of assessment: what each asset claims under the method the company elected for it. It posts nothing, and the depreciation in the books is not changed by it. Elect a method on an asset with **Capital allowance** in its side-peek (an Accountant does).

- Section 19: 20% of the cost at once, the initial allowance, and the other 80% in annual allowances over a working life the company elects. A prescribed life in the Sixth Schedule of 12 years or less allows 6 or 12 years, and a prescribed life of 16 years allows 6, 12 or 16. The election is irrevocable once made.
- Section 19A(1): a third of the cost in each of 3 years of assessment.
- Section 19A(2): the whole cost in one year, for a computer or prescribed automation equipment, with no cap and no limit of cost. That an asset is one is the company's election for it, which the tax agent confirms.
- Section 19A(10A): the whole cost in one year, for an asset of at most S$5,000 each, the year's claims at most S$30,000. The cap takes whole assets in the order they were bought. One it cannot take is listed apart, to be claimed another way.
- The year of assessment is the year after the financial year the asset was bought in ends: an asset bought in the year ending in 2026 first claims in year of assessment 2027.
- An asset sold whole takes no allowance in the year of its sale. That year its row shows the tax written down value (the cost less the allowances of the years before), the day it was sold and the sale proceeds net of GST, and a **balancing allowance** (the proceeds are lower than the written down value, tax deductible) or a **balancing charge** (the proceeds are higher, taxable, at most what was allowed before). The year after, it has no row. The year's totals add the balancing allowances and charges. The tax agent confirms that no allowance is also due in the year of a sale.
- An asset Fyniq cannot place is listed apart with the reason and is never guessed: no method elected yet, bought before year of assessment 2023, a part of it disposed of, a working life that is not allowed, or over the limits of section 19A(10A). Hire purchase (its principal payments are not recorded), a tax written down value brought forward into a one-year write-off, deferred claims and a transfer between related companies are the tax agent's.
- The rules come from a statutory pack with IRAS's page as its source, and the amounts are in cents. The tax agent confirms them.

## The register at the close

When a month closes, the register is checked against its accounts: each asset account against the
cost of its assets and of the purchases not in the register yet, and 1590 against their accumulated
depreciation.

- ✖ when they differ by anything no document explains: report it, never adjust it by journal.
- ⚠ when a manual journal posted to one of them, or a purchase on an asset account is not in the
  register yet.

## Related

- The capitalisation threshold, the disposal account and the disposal clearing account:
  [Organisation settings](organisation-settings.md).
- Entering the bill that bought an asset: [Bills](bills.md). The close: [Periods and the close](periods-and-close.md).
