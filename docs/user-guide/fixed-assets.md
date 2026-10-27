---
key: accounting.fixed-assets
title: Keep the fixed asset register
mode: expert
roles: [accountant, controller, owner, bookkeeper, auditor, tax_agent]
requirements: [FR-FA-01, FR-FA-02, FR-FA-03, INV-07]
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
  company is GST-registered (usually SR); the bank line is matched to it later. Or a sales invoice
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
