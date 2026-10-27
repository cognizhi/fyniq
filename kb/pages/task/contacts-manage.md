---
key: contacts.manage
type: task
title: Keep your contacts, items and prices
summary: Add or change a contact, possible duplicates, a vendor's bank details and their approved changes, a contact's own templates, and items with prices
sources:
  - docs/user-guide/contacts-and-items.md sha256:d59661a5417bcaa5f02afad4db879d8869cf3663dd8b5178892378e0411051cd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-27
aliases:
  - customer
  - vendor
  - supplier
  - new contact
  - duplicate contact
  - item
  - product
  - service
  - price list
  - tax residence
  - foreign supplier
  - early-payment discount
  - supplier discount terms
  - customer template
  - template for one customer
  - bank details
  - change bank account
  - supplier changed banks
  - payee bank details
  - SWIFT code
  - IBAN
  - payments frozen
  - show account number
---

# Keep your contacts, items and prices

## What they are

- **Contacts** lists the customers the company invoices ([[sales.invoices]]) and the vendors whose bills it pays; one contact can be both.
- **Items and prices** lists the products and services it sells, each with a unit price per currency.
- A Bookkeeper, an AR or AP specialist, an Accountant or a Controller adds and changes them; others with a finance role can see them.
- Nothing is deleted: making one inactive keeps its history.

(src: docs/user-guide/contacts-and-items.md#what-it-is)

## Add or change a contact

- **New contact**: tick **A customer**, **A vendor** or both, enter the legal name and what is known (UEN or tax ID, billing and remittance emails, payment terms, currency, Peppol ID, address), then **Add the contact**. Empty payment terms use the organisation's default.
- For a vendor, enter its **Country**, and its **Tax residence** when it is resident for tax somewhere else; a vendor resident outside Singapore may have tax withheld from its payments ([[purchases.bills]]).
- A supplier offering a discount for paying early takes its **Early-payment discount (%)** and **If paid within (days)** of the bill's date, such as 2% within 10 days; its bills then show the discount and the last day to take it.
- Before adding, Fyniq checks for a contact with the same name, UEN or tax ID, or email, and says which and why; check it is a new company, then **Add it anyway**, or use the one there. Such a contact shows **Possible duplicate** in the list.
- The pencil changes a contact; **Save the contact**. Clearing **Active** stops offering it for new documents. Vendor bank details have their own change, which another person approves (below).

(src: docs/user-guide/contacts-and-items.md#add-a-contact) (src: docs/user-guide/contacts-and-items.md#possible-duplicates) (src: docs/user-guide/contacts-and-items.md#change-a-contact)

## Bank details

- A vendor's **Bank details** (the bank icon next to it) are where Fyniq will pay it: one account per currency, at a Singapore bank or one outside Singapore; numbers show their last four characters only.
- The Owner and the Controller approve changes; the Accountant and the AP specialist prepare them; the Auditor and the Tax agent see them masked.
- **Show number** shows the full number once you confirm it's you; the audit trail records each look. The approver of a change can show the new number and the one it replaces, to compare with the letter.

(src: docs/user-guide/contacts-and-items.md#bank-details) (src: docs/user-guide/contacts-and-items.md#see-the-full-number)

## Change a supplier's bank details

- A letter or email saying a supplier changed banks is the most common way payments are diverted, so a change takes effect only when a second person approves it.
- First call the supplier on a number you already had, never one from the letter. Then **Change bank details**: currency, SWIFT/BIC code, name on the account, account number (or IBAN outside Singapore), and a **Call-back note** of who, when and which number; **Save as a draft**.
- Attach the supplier's letter (bank_letter) or a screenshot of their confirmation through a channel you already knew (vendor_confirmation), then **Ask for approval** and confirm it's you.
- While it waits, payments to the vendor are frozen: no payment date can be planned, and a payment recorded anyway is flagged; every founder is told at once.
- Another person who holds the approval right approves it from the Inbox ([[approvals.decide]]), never whoever prepared or asked for it. The approval puts the new account in force and replaces the old one for that currency.

(src: docs/user-guide/contacts-and-items.md#change-a-suppliers-bank-details)

## Their own templates

- **Templates** next to a contact chooses, for each document it receives, the published template its documents and emails use, or **The organisation's default**; then **Save the templates**.
- A customer receives invoices, quotes, credit notes, receipts, statements and reminder letters; a supplier, remittance advice and purchase orders. Only published templates can be chosen ([[settings.templates]]).

(src: docs/user-guide/contacts-and-items.md#their-own-templates)

## Items and prices

- **Contacts → Items and prices**, **New item**: code and name, **Service** or **Product**, unit, revenue account ([[accounting.chart]]), GST code and printed description.
- Under **Unit prices**, a plain decimal per currency, such as 180.00; then **Add the item**.
- The code never changes; changing the prices replaces them all, recorded with the prices before and after.

(src: docs/user-guide/contacts-and-items.md#items-and-prices)

## Common problems

- CT-01, the legal name is blank or too long; CT-02, neither a customer nor a vendor; CT-03, not an email address; CT-04, not a two-letter code such as SG for the country or the tax residence.
- CT-11, the early-payment discount terms are not right: both a percentage above 0 and below 100 and 1 to 365 days, or neither.
- A price that is not a plain decimal: digits with up to 6 decimals, no commas or currency sign.
- PAYEE-11, no bank letter or vendor's confirmation attached: attach one, then ask again. PAYEE-12, payments to the vendor are frozen while a change waits. PAYEE-13, another change of that currency waits.
- PAYEE-07, not an account number the bank takes: a Singapore account is 6 to 17 digits, and an IBAN is checked. PAYEE-06, the bank's country does not fit the SWIFT/BIC code. PAYEE-18, whoever prepared the change cannot approve it.

(src: docs/user-guide/contacts-and-items.md#common-problems)
