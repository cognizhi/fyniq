# UAT: reconcile

- **Journey:** reconcile (QA-E2E-01)
- **People:** Bea Bookkeeper (Bookkeeper), Cal Accountant (Accountant)
- **Proves:** FR-BNK-01 (money accounts), FR-BNK-02 (statement formats), FR-BNK-06 (suggested matches and the actions
  on a line), FR-BNK-07 (the reference matching order), FR-BNK-10 (the reconciliation), FM-5 (statement balances from the
  statement, never typed)
- **Organisation:** `make uat-organisation NAME="UAT Reconcile Pte Ltd" PEOPLE="Bea Bookkeeper:bookkeeper;Cal Accountant:accountant"`

The Accountant adds the operating bank account as a money account, pays a bill from it, and imports the bank's
statement of the day. The statement's line is the payment: the queue offers it as the payment already recorded, the
Accountant accepts, and the line is matched to it.

## Before you start

Make the statement file: an OFX statement of account 601234567001 for today, with one debit of 642.80 named
"LUMEN PRINT HOUSE" and memo "OCBC FAST 7781". The browser journey's `ofx()` in `apps/web/journeys/` writes the same
file; any synthetic OFX, CSV or CAMT.053 statement of the same line will do.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `add-bank-account` | Open Banking › accounts › new. Name: "OCBC current". Bank or provider: OCBC. Account in the chart: 1020 OCBC Business Growth. Account number: 601234567001. "Add the account". | "Step 2 of 3": the account is added; its data source comes next. | Screenshot of the account |
| 2 | Bea, Cal | `bill-record` | As in the pay script, steps 1 to 3: Lumen Print House Pte Ltd's bill LPH-0931 of 642.80, posted and paid from 1020 with the reference "OCBC FAST 7781". | The bill is Paid; PAY-0001 is 642.80 on today's date. | Screenshot of the paid bill |
| 3 | Cal | `statement-import` | Open Banking › Import, choose the statement file under "Statement files". | The file's region shows its account, OCBC current, before anything is imported. | Screenshot of the preview |
| 4 | Cal | `statement-import` | Select "Import". | "Imported"; "What it does" says "1 added". | Screenshot of the result |
| 5 | Cal | `statement-import` | Import the same file again. | Nothing is added twice: the line is recognised as already imported. | Screenshot of the result |
| 6 | Cal | `categorise-basic` | Open Banking › Categorise. | The line "OCBC FAST 7781" of OCBC current, with "Money paid, already recorded: PAY-0001" chosen. | Screenshot of the line |
| 7 | Cal | `categorise-basic` | Select "Accept". | "1 done · 0 lines wait". | Screenshot of the queue |
| 8 | Cal | `bank-account` | Open the OCBC current register. | The statement line is matched to PAY-0001; nothing waits to be categorised. | Screenshot of the register |
| 9 | Cal | `bank-reconciliation` | Open the account's reconciliation for this month. | The statement balance comes from the imported statement, with no field to type it (FM-5). The difference is 0.00 once every line is matched; sign it off. | Screenshot of the signed reconciliation |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
