---
key: accounting.currencies
title: Exchange rates
mode: expert
roles: [accountant, controller, owner, auditor, tax_agent]
requirements: [FR-GL-05, ENG-40, RND-RATE]
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

An Accountant or a Controller adds and withdraws rates. The Owner, the Auditor and the Tax agent
can see them. The page is in Expert mode.

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

## Common problems

| Message | What to do |
|---|---|
| No exchange rate for this currency (FX-01) | Add the rate for the day or the month, or type the rate on the journal line |
| The exchange rate is not valid (FX-02) | Use a currency other than the base, a rate above zero with up to 10 decimal places, and the first day for a monthly rate |
| The rate is withdrawn already (FX-03) | Nothing to do: postings no longer take it |
| Withdrawing a rate needs a reason (FX-04) | Say why the rate is wrong or no longer used |
| The exchange rate does not change that way (STORAGE-FX-01) | Withdraw the rate and add another |

## Related

- [Prepare, post and reverse journals](journals.md)
- [Open the financial year and close the periods](periods-and-close.md)
