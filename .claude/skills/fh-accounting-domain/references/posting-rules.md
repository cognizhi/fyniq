# Posting rules in detail

Source: PRD-001 rev 0.2 §10.2 and Appendix B. Conventions: debit positive, credit negative, in base
currency; the period is the month of the posting date. "Rate" is the GST rate in force at the
document date, and only when the organisation is GST-registered at that date and the code is
standard-rated (SR, TX, IM). Otherwise it is 0.

When this file and the PRD disagree, the PRD wins. Fix this file in the same pull request.

## B.1 Bank, card and cash transactions

| Leg | Account | Amount |
|---|---|---|
| Money account | Bank / card / cash | + signed gross (money in positive, money out negative) |
| GST | 2200 GST control | − GST, where GST = `RND-GST-INCL`(signed gross) |
| Category (no spread) | Category account | − (signed gross − GST) |
| Category (spread over n months; P&L categories only) | 2400 (money in) or 1210 (money out) in the posting month, then monthly releases from the deferral account to the category from the posting month for n months (`ALLOC-EVEN`) | |
| Settlement | Category 1100 / 2010 with a matched document | Clears AR/AP; a difference within tolerance goes to 7030 |

**Tolerances** (organisation settings; workbook defaults): base currency S$1.00; foreign currency
the greater of S$1.00 and 3% of the document total.

**Matching** (FR-BNK-07): exact reference, then normalised (Unicode NFKC, trimmed, non-breaking
spaces removed, case-insensitive), then amount and contact. One payment can settle many documents
and many payments one document; partial payments are allowed; overpayments become credits.
Transfers between money accounts are recorded once and matched on both feeds (FR-BNK-09).

## B.2 Invoices and bills (posted; not drafts, not opening items)

| Leg | Invoice | Bill |
|---|---|---|
| Net (base) | − `RND-FX`(net in document currency × rate) to revenue, or to 2400 when the service months > 1 for P&L categories | + to expense or asset, or to 1210 when spread |
| GST | − `RND-GST-EXCL`(net base) to 2200 | + to 2200 |
| Control | + total to 1100 | − total to 2010 |
| Settlement difference | When settled within tolerance (or marked fully paid): write-off = total − all payments, posted in the month of the **last** payment: AR −, 7030 + | AP +, 7030 − |
| Spread | The deferral account takes the full net in the posting month; monthly releases from the service start month (`ALLOC-EVEN`) | Same, with 1210 |

- Credit notes and vendor credits are documents with negative totals (or linked credit documents)
  and post the reverse.
- Opening items carry their open balance into AR/AP without touching revenue or expense.
- GST on foreign-currency documents is computed and reported in SGD: `RND-FX` first, then
  `RND-GST-EXCL` (ENG-42).

## B.3 Payroll (per employee per pay month)

| Item | Rule |
|---|---|
| Gross | basic + allowances + overtime/other ordinary pay + bonus/commission − unpaid leave |
| Ordinary wages for CPF | min(max(0, basic + allowances + OT − unpaid leave), OW ceiling) |
| Additional wages for CPF | bonus/commission, limited by the annual AW ceiling tracked year to date (**new**: the workbook did not enforce it) |
| CPF (Citizens and PRs) | `RND-CPF`, with rates by residency, age band and PR year (**new**: PR graduated rates) and graduated rates below the full-rate wage floor (**new**) |
| SDL | `RND-SDL`, all employees, foreigners included |
| SHG | From the SHG table by fund and wage (**new**: the workbook used a flat amount) unless opted out |
| Net pay | gross − EE CPF − SHG |
| Postings | Dr salary account (by department) gross; Dr 6020 ER CPF; Dr 6030 SDL; Cr 2100 net pay; Cr 2110 EE CPF + ER CPF + SDL + SHG |

The age band changes from the first day of the month **after** the birthday. Rates and ceilings
come from the `sg-cpf`, `sg-sdl` and `sg-shg` packs in force for the pay month
(**fh-sg-statutory**).

## B.4 Depreciation

Monthly = `ALLOC-EVEN`(cost − residual over the useful life in months). The start month is the
depreciate-from date or the purchase month. The end month is start + life − 1, or the month before
disposal. Posted from the later of the start and the books start; earlier depreciation is part of
the opening balances. Dr 6610, Cr 1590.

## New in FH Cloud

| Source | Debit | Credit | Notes |
|---|---|---|---|
| FX revaluation (month end) | Open FC items and FC bank balances, or unrealised FX loss | Unrealised FX gain, or the items | Auto-reverses on day 1 of the next period; account code still to be assigned |
| Asset disposal | 1590, Bank or AR (proceeds), loss on disposal | Asset cost, gain on disposal, 2200 if applicable | The workbook left disposals to a manual journal |
| Income tax provision | 8010 | 2300 | Proposed from the tax working paper; an approved journal |
| Bad-debt write-off | Bad-debt expense; 2200 (relief, when eligible) | 1100 | Approval-gated (SOD-9); relief needs written off + 12 months (or insolvency), claimed within 5 years of the supply |
| Payment-provider payout | Bank (net payout); 5040 payment fees | Provider clearing account | Gross receipts settle invoices from the clearing account |
| Edits of posted documents | Reversal of the previous version's entries + entries of the new version | | The workbook recalculated in place; the platform keeps both (FR-GL-12) |
| Opening balances | As entered at books start | | Must net to zero (Check C.1 #6) |

## FX rate cascade (ENG-40)

The document's own rate → the daily rate feed for the document date → the organisation's monthly
table (average of duplicates, as in the workbook) → error "✖ No exchange rate for this currency".
The rate source is stored on every converted amount (FR-GL-05). Realised FX on settlement goes to
7030; unrealised revaluation at month end (ENG-41, FR-GL-04).

## GST codes (workbook set; extend per current IRAS guidance [verify])

| Code | Standard-rated | Direction | F5 |
|---|---|---|---|
| SR | Yes | Supply | Box 1 value, Box 6 GST |
| ZR | No | Supply | Box 2 |
| ES | No | Supply | Box 3 |
| OS | No | Supply | Not reported |
| TX | Yes | Purchase | Box 5 value, Box 7 GST |
| IM | Yes | Purchase | Box 5 value, Box 7 GST |
| ZP | No | Purchase | Box 5 |
| BL | No | Purchase | Box 5 (GST not claimable; included in the cost) |
| NR, EP, OP | No | Purchase | Not reported |
| NA | No | None | Not reported |

Reverse-charge and other current IRAS codes are added as an `sg-gst` pack version, not as code.
