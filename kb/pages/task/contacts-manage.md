---
key: contacts.manage
type: task
title: Keep your contacts, items and prices
summary: Add or change a customer or vendor, what a possible duplicate means, and add items with a unit price per currency
sources:
  - docs/user-guide/contacts-and-items.md sha256:7fd3f897470c89fe0fc7a5a638b485cbbf0f95ebb185e8e1542f591f15ae79cd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-23
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
---

# Keep your contacts, items and prices

## What they are

- **Contacts** lists the customers the company invoices and the vendors whose bills it pays; one contact can be both.
- **Items and prices** lists the products and services it sells, each with a unit price per currency.
- A Bookkeeper, an AR or AP specialist, an Accountant or a Controller adds and changes them; others with a finance role can see them.
- Nothing is deleted: making one inactive keeps its history.

(src: docs/user-guide/contacts-and-items.md#what-it-is)

## Add or change a contact

- **New contact**: tick **A customer**, **A vendor** or both, enter the legal name and what is known (UEN or tax ID, billing and remittance emails, payment terms, currency, Peppol ID, address), then **Add the contact**. Empty payment terms use the organisation's default.
- For a vendor, enter its **Country**, and its **Tax residence** when it is resident for tax somewhere else; a vendor resident outside Singapore may have tax withheld from its payments ([[purchases.bills]]).
- A supplier offering a discount for paying early takes its **Early-payment discount (%)** and **If paid within (days)** of the bill's date, such as 2% within 10 days; its bills then show the discount and the last day to take it.
- Before adding, Fyniq checks for a contact with the same name, UEN or tax ID, or email, and says which and why; check it is a new company, then **Add it anyway**, or use the one there. Such a contact shows **Possible duplicate** in the list.
- The pencil changes a contact; **Save the contact**. Clearing **Active** stops offering it for new documents. Vendor bank details have their own change, which another founder approves ([[approvals.decide]]).

(src: docs/user-guide/contacts-and-items.md#add-a-contact) (src: docs/user-guide/contacts-and-items.md#possible-duplicates) (src: docs/user-guide/contacts-and-items.md#change-a-contact)

## Items and prices

- **Contacts → Items and prices**, **New item**: code and name, **Service** or **Product**, unit, revenue account ([[accounting.chart]]), GST code and printed description.
- Under **Unit prices**, a plain decimal per currency, such as 180.00; then **Add the item**.
- The code never changes; changing the prices replaces them all, recorded with the prices before and after.

(src: docs/user-guide/contacts-and-items.md#items-and-prices)

## Common problems

- CT-01, the legal name is blank or too long; CT-02, neither a customer nor a vendor; CT-03, not an email address; CT-04, not a two-letter code such as SG for the country or the tax residence.
- CT-11, the early-payment discount terms are not right: both a percentage above 0 and below 100 and 1 to 365 days, or neither.
- A price that is not a plain decimal: digits with up to 6 decimals, no commas or currency sign.

(src: docs/user-guide/contacts-and-items.md#common-problems)
