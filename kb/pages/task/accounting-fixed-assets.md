---
key: accounting.fixed-assets
type: task
title: Keep the fixed asset register
summary: How an Accountant registers assets from purchases or opening balances, how depreciation posts monthly, and how to sell, write off or void
sources:
  - docs/user-guide/fixed-assets.md sha256:26e4f0ae6ce29ae1d7dfa46db7d884957fb67bfd5f6062cd2d4a4f326976db99
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-27
aliases:
  - fixed asset
  - asset register
  - depreciation
  - net book value
  - capitalise
  - dispose of an asset
  - write off an asset
  - sell an asset
---

# Keep the fixed asset register

## What it is

- **Accounting → Fixed assets** (Expert mode) lists what the company owns and uses for more than a year: each asset's category (the asset account it sits on, such as 1510 Computer equipment), cost, useful life, accumulated depreciation and net book value.
- An Accountant registers, changes and disposes of assets; the Owner, the Controller, the Bookkeeper, the Auditor and the Tax agent can see them.
- Registering posts nothing: the cost is already on the asset account, put there by the bill or bank line that bought it ([[purchases.bills]]).

(src: docs/user-guide/fixed-assets.md#what-it-is)

## Register an asset

- Open **Not in the register yet**: every line of a posted bill or categorised bank line on an asset account that no asset came from, with ⚠ **At or above** when its cost reaches the capitalisation threshold (S$1,000 unless changed, [[settings.organisation]]).
- Choose **Register**, enter the useful life in months (such as 36 for a laptop) and any residual value; the cost is what the line put on the account, in Singapore dollars.
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

- **Sell or write off**: the whole asset or part of its cost (its depreciation goes with it in proportion, the rest depreciates on), and the proceeds: money received with its GST code when GST-registered (usually SR), a sales invoice through the disposal clearing account, or none for a write-off.
- **Check** shows the gain or loss and the entry before anything is saved; **Record** posts it, the gain or loss to 7050 Gain / (loss) on disposal of assets.
- Every month before the disposal's month must have posted; one recorded late reverses its own month's depreciation.

(src: docs/user-guide/fixed-assets.md#sell-or-write-off-an-asset)

## Void a disposal

- Voiding, with a reason, reverses the disposal's entry and restores the asset, which depreciates again from the disposal's month; in Founder mode another person approves it ([[approvals.decide]]).
- It is voided only before the asset changes again, or, after a part was sold, before what was left depreciates again.

(src: docs/user-guide/fixed-assets.md#void-a-disposal)

## At the close

- When a month closes, each asset account is checked against the cost of its assets and of the purchases not in the register yet, and 1590 against their accumulated depreciation.
- ✖ when they differ by anything no document explains (report it, never adjust it by journal); ⚠ for a manual journal to one of them, or a purchase not in the register yet.

(src: docs/user-guide/fixed-assets.md#the-register-at-the-close)

## Related

- The capitalisation threshold and the disposal accounts: [[settings.organisation]]. Bills: [[purchases.bills]]. The close: [[accounting.periods]].

(src: docs/user-guide/fixed-assets.md#related)
