---
key: contacts.manage
title: Keep your contacts, items and prices
mode: basic
roles: [bookkeeper, ar_specialist, ap_specialist, accountant, controller, owner]
requirements: [FR-CT-01, FR-CT-04, FR-ITM-01, FR-AP-19, FR-AP-13]
status: draft            # written with the contacts and items screens (P1); reviewed when sales and purchases arrive
---

# Keep your contacts, items and prices

## What it is

**Contacts** lists the customers the company invoices and the vendors whose bills it pays. One
contact can be both. **Items and prices** lists the products and services the company sells, each
with a unit price per currency.

A Bookkeeper, an AR or AP specialist, an Accountant or a Controller adds and changes them. Everyone
else with a finance role can see them. A contact or an item is never deleted: make it inactive to
keep its history.

## Add a contact

1. Open **Contacts** and select **New contact**.
2. Tick **A customer**, **A vendor** or both, and enter the legal name. Add what you know: UEN or
   tax ID, billing and remittance emails, payment terms, currency, Peppol ID and address.
3. Select **Add the contact**.

Leave the payment terms empty to use the organisation's default.

For a vendor, enter its **Country**, and its **Tax residence** when it is resident for tax somewhere
else. A vendor resident outside Singapore may have tax withheld from its payments: see
[Bills](bills.md).

When a supplier offers a discount for paying early, enter its **Early-payment discount (%)** and
**If paid within (days)** of the bill's date, such as 2% within 10 days. Its bills then show the
discount and the last day to take it.

## Possible duplicates

Before it adds a contact, Fyniq checks for one with the same name, UEN or tax ID, or email. If it
finds one, it shows which and why. Check that it really is a new company, then select **Add it
anyway**, or cancel and use the one you have. A contact that may duplicate another shows **Possible
duplicate** in the list.

## Change a contact

Select the pencil next to the contact, change what you need, and select **Save the contact**.
Clear **Active** to stop offering it for new documents. Vendor bank details have their own change,
which another founder approves.

## Items and prices

1. Open **Contacts → Items and prices** and select **New item**.
2. Enter its code and name, choose **Service** or **Product**, and add its unit, revenue account,
   GST code and printed description.
3. Under **Unit prices**, enter a price for each currency you sell in, as a plain decimal such as
   180.00.
4. Select **Add the item**.

The code never changes. Changing the prices replaces them all, and the change is recorded with the
prices before and after.

## Common problems

| Message | What to do |
|---|---|
| The legal name is blank or too long (CT-01) | Enter the name as registered |
| Neither a customer nor a vendor (CT-02) | Tick A customer, A vendor or both |
| Not an email address (CT-03) | Check the billing or remittance email |
| Not a country code (CT-04) | Use two letters for the country or the tax residence, such as SG |
| The early-payment discount terms are not right (CT-11) | Enter both a percentage above 0 and below 100 and 1 to 365 days, or neither |
| A price that is not a plain decimal | Enter digits with up to 6 decimals, without commas or a currency sign |

## Related

- [Set up and keep the chart of accounts](chart-of-accounts.md)
- [Bills](bills.md)
