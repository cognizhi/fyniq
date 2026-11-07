---
key: shell.search
title: Search the records
mode: basic
roles: [all]
requirements: [FR-UX-02, SEC-34]
---

# Search the records

## What it is

**Search** finds invoices, credit notes, bills, vendor credits, journals, expense claims, quotes,
sales orders, purchase orders, contacts, accounts and items. It looks only in what your role lets
you see: an Employee finds their own claims, and a role that does not see bills finds none.

## Search

1. Press ⌘K (Ctrl+K on Windows) and type what you are looking for, then choose **Search the records
   for …**. Or open **Search** and type it there, then select **Search**.
2. Type one of:
   - a reference or number, such as INV-0138 or AU-200;
   - a name, such as a customer's or a supplier's;
   - words of a memo, a note or a line, such as "rent";
   - an amount, such as 1,234.56 or S$1,234.56, to find the documents with a posted line of that
     amount;
   - an amount with a sign, such as > 5k, >= 2,500 or < 100, to find the documents above or below it.
     5k is 5,000 and 2.5m is 2,500,000.
3. The results come by kind of record, newest first, each with its date, its status, where the text
   was found and its amount. Select one to open it.

- A whole number on its own, such as 138, finds both what holds those digits and what has that
  amount.
- An amount is looked for without its sign, in the document's own currency, in what is posted now:
  a draft has no amount yet, and what a reversal took back is not found.
- Contacts, accounts and items are found by their names, codes, UEN or emails, never by an amount.

## Narrow the results

- Choose a kind of record, such as **Invoices and credit notes**, to see only its results; each kind
  shows how many it found. Choose **All** to see every kind again.
- When a kind shows a count with a +, it found more than are shown: add a word, or narrow the amount.
- Move between the results with the up and down arrow keys, and open one with Enter.

## Related

- [Find evidence](evidence-search.md)
- [Keep your contacts, items and prices](contacts-and-items.md)
