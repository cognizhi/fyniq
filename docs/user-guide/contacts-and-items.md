---
key: contacts.manage
title: Keep your contacts, items and prices
mode: basic
roles: [bookkeeper, ar_specialist, ap_specialist, accountant, controller, owner, auditor, tax_agent]
requirements: [FR-CT-01, FR-CT-03, FR-CT-04, FR-ITM-01, FR-AP-19, FR-AP-13, FR-TPL-05]
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
which another person approves: see [Change a supplier's bank details](#change-a-suppliers-bank-details).

## Bank details

A vendor's **Bank details** are where Fyniq will pay it: one account for each currency, a Singapore
bank's or a bank's outside Singapore. Select the bank icon next to a vendor to see them. Account
numbers show only their last four characters.

The Owner and the Controller approve changes. The Accountant and the AP specialist prepare them. The
Auditor and the Tax agent see them masked.

## Change a supplier's bank details

A letter or email saying a supplier has changed banks is the most common way fraudsters divert
payments. So a change never takes effect until a second person approves it.

1. Call the supplier on a number you already had, never one from the letter, and confirm the new
   account.
2. Open the vendor's **Bank details** and select **Change bank details**.
3. Enter the currency, the bank's SWIFT/BIC code, the name on the account and the account number
   (for a bank outside Singapore, the account number or IBAN). In **Call-back note**, write who
   you called, when, and the number you used. Select **Save as a draft**.
4. Under **Evidence**, attach the supplier's letter (bank_letter), or a screenshot of their
   confirmation through a channel you already knew (vendor_confirmation).
5. Select **Ask for approval**, and confirm it's you.

While the change waits, payments to the vendor are frozen: no payment date can be planned for its
bills, and a payment you record anyway is flagged. Every founder is told at once. Another person
who holds the approval right approves it from the **Inbox**. It cannot be the person who prepared
it or asked for it. The approval puts the new account in force and replaces the old one for that
currency.

## See the full number

Select **Show number** next to an account, and confirm it's you. The audit trail records each look.
The approver of a change can show both the new number and the one it replaces, to compare them with
the letter.

## Their own templates

A customer or a supplier can have its own look. Select **Templates** next to the contact, then choose,
for each document it receives, the published template its documents and emails use, or **The
organisation's default**. Select **Save the templates**. A customer receives invoices, quotes, credit
notes, receipts, statements and reminder letters; a supplier receives remittance advice and purchase
orders. Only published templates can be chosen: see [Design your documents with
templates](document-templates.md).

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
| No bank letter or vendor's confirmation is attached (PAYEE-11) | Attach the letter as bank_letter, or a screenshot of their confirmation as vendor_confirmation, then ask again |
| Payments to the vendor are frozen (PAYEE-12) | A change of its bank details waits: have it approved, rejected or withdrawn, then plan the payment |
| Another change of that currency waits (PAYEE-13) | Finish or withdraw the change already asked for |
| Not an account number the bank takes (PAYEE-07) | A Singapore account is 6 to 17 digits; check an IBAN against the letter, a character is wrong |
| The bank's country does not fit (PAYEE-06) | The SWIFT/BIC code's 5th and 6th letters are the bank's country: choose a Singapore bank only for SG |
| You prepared it, so another person approves it (PAYEE-18) | Ask another person who holds the approval right |

## Related

- [Set up and keep the chart of accounts](chart-of-accounts.md)
- [Bills](bills.md)
