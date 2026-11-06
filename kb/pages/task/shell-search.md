---
key: shell.search
type: task
title: Search the records
summary: Find invoices, bills, journals, claims, quotes, orders, contacts, accounts and items by reference, name, words or amount, such as > 5k
sources:
  - docs/user-guide/search.md sha256:3e294b41eac7310b607a836c4709567087029d9f54018d07457d4c610834b5be
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - global search
  - find an invoice
  - find a document by amount
  - search by amount
  - find a record
  - search by reference
---

# Search the records

## What it is

- **Search** finds invoices, credit notes, bills, vendor credits, journals, expense claims, quotes, sales orders, purchase orders, contacts, accounts and items.
- It looks only in what your role lets you see: an Employee finds their own claims, and a role that does not see bills finds none.

(src: docs/user-guide/search.md#what-it-is)

## Search

- Press ⌘K (Ctrl+K on Windows), type, and choose **Search the records for …**; or open **Search**, type and select **Search**.
- Type a reference or number (INV-0138, AU-200), a name, words of a memo, note or line such as "rent", an amount (1,234.56 or S$1,234.56), or an amount with a sign (> 5k, >= 2,500, < 100); 5k is 5,000 and 2.5m is 2,500,000.
- Results come by kind of record, newest first, with date, status, where the text was found and the amount; select one to open it.
- A whole number on its own, such as 138, finds both what holds those digits and what has that amount.
- An amount is looked for without its sign, in the document's own currency, in what is posted now: a draft has no amount yet, and what a reversal took back is not found. Contacts, accounts and items are found by name, code, UEN or emails, never by amount.

(src: docs/user-guide/search.md#search)

## Narrow the results

- Choose a kind of record, such as **Invoices and credit notes**, to see only its results; each kind shows how many it found, and **All** shows every kind again.
- A count with a + means more were found than are shown: add a word or narrow the amount.
- Move between results with the up and down arrow keys, and open one with Enter.
- Related: [[compliance.evidence]], [[contacts.manage]], [[home.overview]].

(src: docs/user-guide/search.md#narrow-the-results)
