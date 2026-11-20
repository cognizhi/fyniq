---
key: accounting.fixed-assets
type: task
title: Keep the fixed asset register
summary: How an Accountant registers assets from purchases or opening balances, how depreciation posts monthly, and how to sell, write off or void
sources:
  - docs/user-guide/fixed-assets.md sha256:cc161795289ad9308a7e1ee1000e37b3873af7e4bbb0911239754620a3b9c63c
data_class: general
taint: trusted
packs:
  - sg-capital-allowances@2026.1
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - fixed asset
  - asset register
  - depreciation
  - net book value
  - capitalise
  - dispose of an asset
  - write off an asset
  - sell an asset
  - wear and tear
  - depreciation of a laptop
  - depreciation of equipment
  - capital allowance
  - capital allowances
  - section 19A
  - movement schedule
  - tax depreciation
  - NBV movement
---

# Keep the fixed asset register

## What it is

- **Accounting → Fixed assets** (Expert mode) lists what the company owns and uses for more than a year: each asset's category (the asset account it sits on, such as 1510 Computer equipment), cost, useful life, accumulated depreciation and net book value.
- An Accountant registers, changes and disposes of assets; the Owner, the Controller, the Bookkeeper, the Auditor and the Tax agent can see them.
- Registering posts nothing: the cost is already on the asset account, put there by the bill or bank line that bought it ([[purchases.bills]]).

(src: docs/user-guide/fixed-assets.md#what-it-is)

## Register an asset

- Open **Not in the register yet**: every line of a posted bill or categorised bank line on an asset account that no asset came from, with ⚠ **At or above** when its cost reaches the capitalisation threshold (S$1,000 unless changed, [[settings.organisation]]).
- Choose **Register**, check the useful life in months (the form offers the asset account's default life, such as 36 for 1510 Computer equipment; change it if this asset lasts longer or shorter) and enter any residual value; the cost is what the line put on the account, in Singapore dollars.
- Depreciation starts in the purchase month, or a later month when it went into use later. A posted bill with such a line shows ⚠ with a link to register it; a line is registered once.

(src: docs/user-guide/fixed-assets.md#register-an-asset)

## Assets bought before the books start

- Choose **Register one bought earlier** and enter the cost and accumulated depreciation the opening balances carry for it, with the day it was bought ([[accounting.opening-balances]]).
- Its depreciation posts from the books start; earlier months are in the opening balances.

(src: docs/user-guide/fixed-assets.md#assets-bought-before-the-books-start)

## Monthly depreciation

- On each month's last day an asset's depreciation posts by itself: Dr 6610 Depreciation, Cr 1590 Accumulated depreciation; the cost less the residual value is spread evenly over its life, the cents spread so the months add up exactly.
- The schedule shows each month **Posted**, **Waiting** or **Projected**; the future is never posted.
- A month **Waiting** has ended but its period is not open: open it and it posts within the hour, the later months after it ([[accounting.periods]]). An asset registered late posts its months already due at once.

(src: docs/user-guide/fixed-assets.md#depreciation-month-by-month)

## Change an asset

- **Change** renames it, or changes its useful life or residual value for the months not posted yet; what has posted stays, and what is left spreads over the months left.
- Its start month, and the figures of one bought before the books start, change only while none of its months has posted.

(src: docs/user-guide/fixed-assets.md#change-an-asset)

## Sell or write off an asset

- **Sell or write off**: the whole asset or part of its cost (its depreciation goes with it in proportion, the rest depreciates on), and the proceeds: money received with its GST code when GST-registered (usually SR; a code given is always one for supplies, SR, ZR, ES or OS), a sales invoice through the disposal clearing account (1160 Asset disposal clearing in the standard chart), or none for a write-off.
- **Check** shows the gain or loss and the entry before anything is saved; **Record** posts it, the gain or loss to 7050 Gain / (loss) on disposal of assets.
- Every month before the disposal's month must have posted; one recorded late reverses its own month's depreciation.

(src: docs/user-guide/fixed-assets.md#sell-or-write-off-an-asset)

## Void a disposal

- Voiding, with a reason, reverses the disposal's entry and restores the asset, which depreciates again from the disposal's month; in Founder mode another person approves it ([[approvals.decide]]).
- It is voided only before the asset changes again, or, after a part was sold, before what was left depreciates again.

(src: docs/user-guide/fixed-assets.md#void-a-disposal)

## Void an asset registered by mistake

- **Void the asset**, with a reason, is for an asset registered from the wrong line or that was never an asset: each month of its depreciation that posted is reversed on its own date, it leaves the register for **Voided**, and its line shows again under **Not in the register yet**.
- It waits while a disposal of the asset stands (void the disposal first) or while a month to reverse is in a closed period (reopen it first); in Founder mode another person approves it ([[approvals.decide]]).
- An asset sold, scrapped or lost is not voided but sold or written off, so its gain or loss shows.

(src: docs/user-guide/fixed-assets.md#void-an-asset-registered-by-mistake)

## The movement schedule

- **Movement** shows, for a run of months, each asset's cost and accumulated depreciation at the start, what an asset bought in the run added, what the depreciation charged, what a disposal took off, and the cost and accumulated depreciation at the end, with the net book value, by asset account.
- The columns foot exactly, and the register is tied to the ledger at the end of the run: what it does not explain is shown as unexplained, never hidden.

(src: docs/user-guide/fixed-assets.md#the-movement-schedule)

## The capital allowance schedule

- **Capital allowances** is the tax schedule of a year of assessment: what each asset claims under the method elected for it with **Capital allowance** in its side-peek, which an Accountant does. It posts nothing and changes no depreciation.
- Section 19: 20% of the cost at once, then the other 80% over a working life the company elects, 6 or 12 years for a prescribed life of 12 years or less, 6, 12 or 16 for 16 years; irrevocable once made. Section 19A(1): a third of the cost in each of 3 years of assessment. Section 19A(10A): the whole cost in one year, for an asset of at most S$5,000 each, the year's claims at most S$30,000. It takes whole assets in the order they were bought.
- The year of assessment is the year after the financial year the asset was bought in ends.
- An asset Fyniq cannot place is listed apart with why, never guessed: none elected, bought before year of assessment 2023, a disposal stands, a working life not allowed, or over the limits of section 19A(10A). Balancing allowances and charges, hire purchase and section 19A(2) are the tax agent's.
- The rules come from the statutory pack with IRAS's page as its source; amounts are in cents; the tax agent confirms them.

(src: docs/user-guide/fixed-assets.md#the-capital-allowance-schedule)

## At the close

- When a month closes, each asset account is checked against the cost of its assets and of the purchases not in the register yet, and 1590 against their accumulated depreciation.
- ✖ when they differ by anything no document explains (report it, never adjust it by journal); ⚠ for a manual journal to one of them, or a purchase not in the register yet.

(src: docs/user-guide/fixed-assets.md#the-register-at-the-close)

## Related

- The capitalisation threshold and the disposal accounts: [[settings.organisation]]. Bills: [[purchases.bills]]. The close: [[accounting.periods]].

(src: docs/user-guide/fixed-assets.md#related)
