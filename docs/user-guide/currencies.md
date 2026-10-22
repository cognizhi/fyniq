---
key: accounting.currencies
title: Exchange rates
mode: expert
roles: [accountant, controller, owner, auditor, tax_agent]
requirements: [FR-GL-04, FR-GL-05, ENG-40, ENG-43, RND-RATE]
status: draft            # written with the Currencies page (P1, p1-multi-currency-fx); reviewed with the accountant
---

# Exchange rates

## What it is

**Accounting → Currencies** keeps the rates that turn other currencies into the base currency
(SGD for a Singapore company). Every rate is how much of the base currency one unit of the other
currency buys, kept to 10 decimal places.

Rates come from two places:

- the **daily feed**, the same for every company, published for business days only; and
- **your rates**: a rate for a day, which overrides the feed's for that day, and the **monthly
  table**, a rate for a whole month.

**Money accounts in other currencies** shows each bank, card, cash or wallet account's balance in
another currency, both in that currency and in the books in the base currency.

The **Revaluation** tab restates each month's balances in other currencies at the month-end rate.

An Accountant or a Controller adds and withdraws rates and revalues a month. The Owner, the
Auditor and the Tax agent can see them. The page is in Expert mode.

## How a posting finds its rate

A journal line in another currency with no rate of its own takes its rate for the journal's date.
Fyniq looks, in order, for:

1. your rate for that day;
2. the daily feed's rate for that day, or its latest rate up to 7 days before, as the feed has
   none on weekends and public holidays;
3. your monthly rate for that month. When the month has more than one, Fyniq takes their average
   and shows ⚠ until you withdraw all but one. An average never sets a GST amount.

With none of these, the line cannot post: ✖ **No exchange rate for this currency** (FX-01).

The line records the rate it took and where it came from, such as "MAS daily feed of 2026-10-16",
and keeps it: a rate added or withdrawn later never changes a posted line.

To see what a posting would take, enter the currency and the date under **Find the rate a posting
takes** and select **Find**.

## Add a rate

1. Select **Add a rate**.
2. Choose whether it is for **a day** or **a month**.
3. Enter the currency, the day or month, and the rate.
4. Choose how the rate is quoted: base currency for 1 unit, base currency for 100 units (as some
   sources quote yen), or units for 1 of the base currency. Fyniq turns it into base currency for
   1 unit and keeps your quote beside it.
5. Add where it came from, if it helps, such as the bank's board rate, and select **Add a rate**.

A new rate for a day replaces your earlier rate for that day, which is withdrawn with the reason
"Replaced by a new rate for the same day". A rate is never changed: withdraw it and add another.

## Import rates from a file

1. Select **Import a file**.
2. Choose a CSV file whose first line names the columns `currency`, `date` and `quote`, and `note`
   if you keep one. Write dates as `2026-10-16`.
3. Choose whether each rate is for its day or its month, and how the rates are quoted.
4. Fyniq says how many rates it read and lists any line it cannot read. Select **Import**.

Each rate is entered on its own: a rate that cannot be entered is listed with its line and why, and
the others are kept. A file can hold up to 250 rates.

## Withdraw a rate

Select **Withdraw** beside the rate, say why, and select **Withdraw** again. Postings no longer
take it; postings that took it keep it. **Show withdrawn rates** lists it with who withdrew it,
when and why.

## Revalue a month

At each month's end, balances in other currencies on accounts revalued at month end, such as a
USD bank account or a receivable in euros, are restated at the rate for the month's last day.
Which accounts are revalued is set in the chart ([Chart of accounts](chart-of-accounts.md)).

1. Open the **Revaluation** tab and choose the **Month**: it starts on the latest open month that
   has ended.
2. Check each balance: what it is in its currency, what it is in the books, the month-end rate and
   where it comes from, and the gain or loss. **Posting preview** shows the entry, and **Not
   revalued** lists the balances that stay at the rates they were recorded at.
3. Select **Revalue** with the month's name.

The gain or loss posts on the month's last day against 7040 Unrealised FX gain or loss, and
reverses on the next month's first day, so a later payment measures its realised gain or loss
against the invoice's own rate. If the next month is not open yet, the reversal posts once it is.

A month can be revalued from its last day while it is open. If a currency has no rate for that
day, Fyniq says which, and **Add a rate** enters it. If anything dated in the month or before is
posted after the month is revalued, revalue it again: the new run posts only what changed. The
month's close refuses a month that has balances to revalue and is not revalued since its last
posting.

## Common problems

| Message | What to do |
|---|---|
| No exchange rate for this currency (FX-01) | Add the rate for the day or the month, or type the rate on the journal line |
| The exchange rate is not valid (FX-02) | Use a currency other than the base, a rate above zero with up to 10 decimal places, and the first day for a monthly rate |
| The rate is withdrawn already (FX-03) | Nothing to do: postings no longer take it |
| Withdrawing a rate needs a reason (FX-04) | Say why the rate is wrong or no longer used |
| The exchange rate does not change that way (STORAGE-FX-01) | Withdraw the rate and add another |
| The period is not open (FX-05) | Revalue a month while it is open, or ask for it to be reopened |
| The period has not ended (FX-06) | Revalue the month from its last day; until then, look at its preview |
| No account takes unrealised FX (FX-07) | Load 7040 Unrealised FX gain or loss from the standard chart |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Open the financial year and close the periods](periods-and-close.md)
- [Set up and keep the chart of accounts](chart-of-accounts.md)
