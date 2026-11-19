---
key: shell.shortcuts
title: Use Fyniq from the keyboard
mode: basic
roles: [all]
requirements: [FR-UX-06, FR-UX-03, FR-UX-23]
---

# Use Fyniq from the keyboard

## What it is

Fyniq has keys to go to each area, to start each kind of record and to move through a form. Press
**?** to see every shortcut you can use, or open **Keyboard shortcuts** from the command palette.
Shortcuts never fire while you type in a field.

In Basic mode, four work: Ctrl+K (⌘K on a Mac) for search and commands, Ctrl+J (⌘J) for the
assistant, **Alt+Shift+M** (Option+Shift+M) to switch between Basic and Expert mode, and **?** for the shortcut
sheet. Expert mode adds the rest below.

## Go to an area

Press **G**, then the area's letter, such as **G** then **B** for Banking. The sheet lists the areas
you can open: Home (G H), the Inbox (G I), Contacts (G C), Sales (G S), Purchases (G P), Expense
claims (G X), Banking (G B), Reports (G R), Accounting (G A), Equity (G E), Planning (G L),
Compliance (G M) and Settings (G T).

## Start a new record

Press **C**, then the record's letter, such as **C** then **I** for a new invoice, **C** then **B**
for a new bill, **C** then **J** for a new journal and **C** then **E** for a new expense claim. The
sheet lists every kind of record your access lets you create, with its keys.

## Move through a form

In an invoice, quote, bill, purchase order, journal or expense claim:

- **Enter** moves to the next field, so you can type a whole line without the mouse. In a list that
  is open under a field, Enter still picks what is highlighted.
- **Ctrl+Enter** (⌘Enter on a Mac) saves it.

## Work in a grid

A grid is the spreadsheet-like table for bulk work, such as journal lines or opening balances. Click a cell, or Tab into the grid; it has one tab stop,
the active cell, and Tab at its last cell goes on to the next thing on the page.

- **Arrow keys** move between cells; with **Shift** they select a block. **Home**, **End**, **Page Up** and **Page Down** go further, and **Ctrl+Home**
  and **Ctrl+End** go to the first and last cell. **Tab** and **Shift+Tab** move across the row.
- Type to start editing a cell, or press **Enter** or **F2** to edit what it holds. **Enter** saves it and moves down, **Escape** cancels, and
  **Delete** clears the selected cells. **Ctrl+Enter** sets every selected cell to what you typed.
- In an amount cell, start a sum with **=**, such as **=1250.50*3-80**; Fyniq works it out exactly and keeps the result. A division is made only when it comes
  out exact: type the amount you mean otherwise.
- **Ctrl+D** fills down: the top cell of the selection is copied down it, or the cell above if you selected one.
- **Ctrl+Z** undoes and **Ctrl+Y** redoes, up to 200 steps.
- **Ctrl+C** copies the selected cells for a spreadsheet, with plain decimals and a minus sign. Paste cells from Excel or Google Sheets: Fyniq shows what it
  read, flags any cell that does not fit its column and says how many rows it adds, and sets them only when you choose **Apply paste** or press **Enter**;
  **Escape** leaves the grid as it was. Amounts such as (1,234.56), S$1,234.56 and −1,234.56 are understood.
- Each row says whether it is ready or what is wrong. A grid holds up to 100,000 rows, and shows only the rows in view.

## Act on selected rows

When you select rows on a page, such as bank lines in the categorise grid, open the command palette:
what you can do to the selected rows comes first, such as **Accept the selected suggestions**. It is
the same as the page's own button.

## Turn them off or change the keys

Single-character shortcuts can get in the way if you use speech input or press keys by accident.

1. Press **?**, or open **Settings › Profile and security › Keyboard**.
2. Turn **Single-character shortcuts** off. Ctrl+K, Ctrl+J and Alt+Shift+M still work.
3. Or select **Change the keys**, type one key or two for any shortcut, such as G B, and select
   **Save the keys**. Fyniq says when two shortcuts would share keys, or when one key would start
   another's two.
4. **Use the usual keys** puts them back.

Your choice applies to you only. Fyniq keeps it with your profile, so it is the same on every device you sign in on.

## The Inbox's keys

While a request in the Inbox has the focus, **J** and **K** move between requests, **E** opens one,
and **A** or **R** open its approval or rejection to confirm. See [Approve or reject](approvals.md).
