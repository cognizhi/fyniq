# UAT: record

- **Journey:** record (QA-E2E-01)
- **People:** Cal Accountant (Accountant), Ann Owner (Owner and Controller)
- **Proves:** FR-GL-01 (manual journals that balance), FR-GL-02 (an accrual reversing on the first of the next month),
  FR-GL-12 (posted documents never edited in place), FR-DOC-06 (a journal's evidence), FR-APR-01 (approval above the
  threshold), FR-GL-11 (the general ledger)
- **Organisation:** `make uat-organisation NAME="UAT Record Pte Ltd" PEOPLE="Cal Accountant:accountant;Ann Owner:owner,controller"`

The Accountant records a month-end accrual as a manual journal, with its evidence and a reversal on the first of the
next month. It posts, by itself or with the Controller's approval, and the general ledger and the trial balance show it.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `journals` | Switch to Expert. Open Accounting › Journals. | "Journals", with "Manual journals. A posted journal is never edited: it is reversed, with a reason." The sample company's journals are listed with their number, date, description, preparer and status. | Screenshot of the list |
| 2 | Cal | `journal-editor` | Select "New journal". Date: today. "What the journal is for": "Accrued accounting fee, this month". Line 1: 6410 Accounting & audit fees, debit 1,200.00. Line 2: 2020 Accrued expenses, credit 1,200.00. Reversal date: the first day of next month. Select "Check". | "It balances and can post. Nothing was saved." Debits equal credits, 1,200.00 each. | Screenshot of the checked journal |
| 3 | Cal | `journal-editor` | Change line 2's credit to 1,100.00 and select "Check". Then put it back to 1,200.00. | The journal is refused, naming the difference of 100.00 between debits and credits: it cannot post unbalanced. | Screenshot of the refusal |
| 4 | Cal | `evidence-panel` | "Save as a draft". In the Evidence panel, attach a PDF of the accountant's engagement letter (synthetic) as "supporting calculation". | "Saved." The file is listed under the journal's evidence. | Screenshot of the evidence |
| 5 | Cal | `journal-editor` | Select "Post". If Fyniq asks for approval, select "Ask for approval". | Either it posts and takes the next number, or "Asked for approval: the journal posts when another person approves it." | Screenshot of the result |
| 6 | Ann | `inbox` | Only if step 5 asked for approval: open the Inbox, open the journal's request, select "Approve…" and confirm. | "Approved: the change is made." The request shows what approving posts: the two lines. | Screenshot of the approval |
| 7 | Cal | `journals` | Open the journal from Journals. | Posted, with its number, Cal as preparer and the reversal date "Reverses by itself" on the first of next month. No Edit of the posted lines: only "Reverse", with a reason. | Screenshot of the posted journal |
| 8 | Cal | `general-ledger` | Open Reports › General ledger for 6410, this month. | The journal's line of 1,200.00 debit, with its number and date, opening the journal when selected. | Screenshot of the ledger |
| 9 | Cal | `financial-statement` | Open the trial balance, as at today. | 6410 and 2020 each moved by 1,200.00 from step 1's figures; total debits equal total credits. | Screenshot, and the trial balance exported as CSV |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
