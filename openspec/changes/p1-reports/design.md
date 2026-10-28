# Design

## Context

- **The package:** `internal/domain/financials`, since `statement` is the customer's statement of account.
- **The balance service (ENG-04)** is the only reader of the ledger. It offers:
  - `Trial` for some periods' movements;
  - `Position` for the balances at a period's end, with earlier years folded into retained earnings (FR-GL-10);
  - `Year` for a financial year's result;
  - `GeneralLedger` for an account's lines;
  - `Control` and `Sources` for control accounts.

  Nothing yet reads the ledger "as at" a revision: the cached balances are the current ones.
- **The chart's attributes (FR-COA-02)** carry what a layout needs:
  - a type;
  - a report group, as free text;
  - a cash-flow line, free text that the standard chart fills from a fixed set;
  - a function, from a fixed vocabulary: COGS, R&D, S&M, G&A or Other;
  - the system roles: depreciation, retained-earnings, income-tax-expense, unrealised-fx and the rest.
- **Organisation accounting (FR-ORG-10)** gives a year's framework and status: "management statements" until the
  policies are approved.
- **The render service** turns a Typst template and data into a PDF, and the document templates already use it. The
  blob store, the outbox and the audit trail are in place.
- **The web app** has no chart library yet. MASTER.md chooses ECharts. The tokens `--chart-1…8` exist, but no script
  validates them.

## Decisions

### D1 Statements by whole months

- A statement's period is a run of whole periods: a month, a quarter, year to date, a financial year, or any run of
  months.
- A range of days is the general ledger report's job. Statements need the balance service's per-period figures and the
  close's months.
- Comparatives (FR-RPT-02) are the run of the same length just before, and the same run a year earlier.
- Columns by month split the run into its months.
- Budget and forecast columns come with `p1-planning`, and dimension columns with dimensions.

### D2 The layout from the chart (rev 0.159) [accountant to confirm]

**Profit and loss.** Each account is placed by its type, function and role, and shown under its report group:

| Section | Accounts |
|---|---|
| Revenue (the streams) | revenue accounts whose function is not Other; discounts and refunds sit inside revenue |
| Cost of sales | expense accounts whose function is COGS |
| Gross profit | revenue less cost of sales, with its margin |
| Operating expenses | expense accounts whose function is R&D, S&M, G&A or none, except depreciation and amortisation; grouped by report group, or by function in Expert |
| EBITDA | gross profit less operating expenses |
| Depreciation and amortisation | the report group of the account with the depreciation role |
| Operating profit | EBITDA less depreciation and amortisation |
| Other income | revenue accounts whose function is Other: grants, interest, FX and rounding, disposals |
| Finance costs and other expenses | expense accounts whose function is Other, except income tax |
| Profit before tax, income tax, net profit | income tax is the account with the income-tax-expense role |

Every line shows its percentage of revenue, with `RND-PERCENT`.

**Balance sheet.** Each account is placed by its cash-flow line, from the standard chart's set:

| Section | Cash-flow lines |
|---|---|
| Current assets | Cash & cash equivalents, Receivables, Other current assets |
| Non-current assets | Capital expenditure, with Depreciation & amortisation (accumulated depreciation) netted |
| Current liabilities | Payables & accruals, Payroll liabilities, Tax liabilities, Deferred revenue |
| Borrowings | Borrowings, apart until the chart records each loan's terms |
| Equity | Share capital, and Dividends & other equity; retained earnings include the year's profit to date |

**Cash flow, indirect method:**
- net profit, plus depreciation and amortisation and the unrealised FX, which are entries that moved no money;
- the change in each working-capital line;
- investing: the change in the capital expenditure lines, net of disposals' book values;
- financing: borrowings, share capital, dividends and other equity;
- the result: the change in cash and cash equivalents.

An entry with no line on a money account that moves a financing or investing account (a cost a director paid
personally, a conversion to shares) is taken out of financing or investing. It is listed apart as a non-cash
transaction.

As built, each entry is classified by the accounts it moves: investing when it moves a capital-expenditure account,
financing when it moves borrowings or equity, operating otherwise. Its cash is what it put on the cash accounts, and the
operating reconciliation equals the operating entries' cash exactly. One consequence is to confirm with the accountant:
equipment bought on account is a non-cash investing entry, and paying the supplier later is operating, since that entry
moves only payables and the bank.

An account whose cash-flow line is outside that set, or a balance-sheet account without one, belongs to no section.
That is a ✖ check, never silently dropped (D3).

### D3 The statements' checks (INV-10, INV-11)

Each statement carries its checks:
- the balance sheet balances (BS_Chk);
- every account sits in exactly one statement (PL_Chk, BS_Uncl), naming any account that does not;
- the cash flow's sections add up to the change in the money accounts (CF_Chk);
- equity's movement reconciles from the opening position to the closing one (BS_EqChk).

The close gains a `statements` check for the month: ⚠ when an account with a balance sits in no section. The chart
allows an account without a cash-flow line, so blocking the close on one would be harsh. The books balancing is
already the close's `trial-balance` check, which blocks it (✖).

### D4 As at a revision (FR-RPT-08, ENG-46)

- **One snapshot:** every statement reads one snapshot and returns the ledger revision visible in it.
- **As at a revision:** reads only the lines with a revision up to it, through the database function
  `fyniq.balances_at(period, revision)`, which the balance service alone calls (`balance.At`, `TrialAt`, `PositionAt`).
  Revisions follow commit order, so a statement reads the ledger's revision first and every figure at it: one
  snapshot, under READ COMMITTED too.
- **Presentation (rev 0.159):** a re-run shows today's account names and groups. It is labelled "restated
  presentation" when an account was created or changed after the revision, as the audit trail and the account's
  version show.
- **The record:** the response carries the revision, the parameters and the platform version (REL-05). An export
  records them on its row.

### D5 Exports (FR-RPT-05, FR-RPT-09, FR-RPT-10)

- **Writers:** one per format, beside the report:
  - **CSV:** UTF-8 and CRLF. Amounts are plain decimals with an ASCII minus. A text cell is quoted, and is prefixed with
    an apostrophe when it starts with =, +, -, @, a tab, CR or LF.
  - **XLSX:** a small writer of our own, a zip of SpreadsheetML as the bank-file reader already reads it. It writes typed
    number cells with the currency's decimals, the statement sheet with its groups and a data tab, and never writes a
    formula from text. It adds no dependency to the image.
  - **PDF:** a Typst statement template through the render service, titled with the framework, with the revision in the
    footer.
- **When:** a statement's CSV or XLSX downloads at once. PDFs and the accountant exports (GL, TB, journals, the fixed
  asset schedule, and AR and AP detail) run on the outbox.
- **The background run** stores the file in the blob store, marks the export ready, and tells the requester (FR-NTF).
- **The link** works for 24 hours (a setting), and only for the requester when signed in. Each download checks their
  rights again and redirects to a storage URL valid for 60 seconds.
- **The audit trail** records who exported what, the filters and the row count, for every export, foreground or
  background.

### D6 Management reports (FR-RPT-03)

- Built now, from documents and the balance service:
  - sales by customer and by item;
  - spend by supplier;
  - the top expenses.
- They link to the AR and AP ageing and the customer statements, which exist.
- Arriving with their changes:
  - cash and runway, with the net burn and runway of the KPI set (FR-KPI-02, `p1-dashboard-kpis`), so runway has one
    definition;
  - Budget vs Actual, the GST return, KPIs and projects.

### D7 Charts (FR-VIZ-01…08)

- **The component:** one `Chart` on ECharts, loaded on demand. It takes a form chosen by the data's job (stat tile,
  line, bar, waterfall), one y-axis, and series coloured from `--chart-1…8` in the entity's fixed order.
- **More than 8 series** fold into Other.
- **Each chart has:**
  - tooltips;
  - a legend, plus direct labels when there are 4 series or fewer;
  - a table view toggle;
  - PNG export and data export through D5's CSV;
  - text in text colours and recessive axes.
- **Status colours** come only with an icon and a label.
- **First uses:** the P&L's monthly trend and the top expenses.
- **The palette** is checked by a script in `make check` (FR-VIZ-07): each pair of `--chart-1…8` is told apart under
  the three common colour-vision deficiencies, and each colour has 3:1 against the light and dark surfaces.
  - (29 Oct 2026, correcting the line above to MASTER.md §10's dataviz method, which it contradicted) The script runs the
    method's checks in all four themes: the lightness band, the chroma floor, adjacent-pair separation under protanopia
    and deuteranopia (the method's simulation; tritanopia, which the method only reports, is left out), and the normal-vision
    floor, each failing the build. Contrast below 3:1 is reported, not failed: the light theme's aqua, yellow and
    magenta are 2.2 to 2.8:1 on white, which the method allows because every chart is also read through its table view
    and direct labels (FR-VIZ-06), so the colour is never the only way to a value (WCAG 1.4.11).

### D8 Trace (FR-UX-04)

Every figure is a button. It opens the general ledger report for that figure's accounts and months, which already shows
each line and its source.

### D9 Permissions

- **The statements, trial balance and accountant exports:** V on "Reports, dashboards" without a qualifier, as the
  general ledger is.
- **The board pack qualifier:** reads the P&L, balance sheet and cash flow, but not the trial balance or the exports.
  Policies know only "unqualified" today, so the board pack's view arrives with the board pack. Until then a qualified
  reader is refused, as for the general ledger.
- **Sales, purchases and payroll qualifiers:** read the management reports of their area only.
- **Exports:** never change the rights. A background export's download is checked again.

## Risks

- **The layout is provisional.** It is tagged [accountant to confirm], and the pages show the FR-UX-41 banner through
  the review register.
- **A chart without the standard cash-flow lines fails the statements' checks.** That is intended, and the check says
  which account and how to fix it.
- **The XLSX writer is ours.** Its output is checked by opening it in the tests (unzip and parse) and against
  Excel's schema rules. A real Excel and Google Sheets open is a `[live-verify]` step.
- **Performance of "as at" reads over years.** They sum lines by period at a revision. NFR-PERF budgets are checked with
  the sample company in `p1-operations-dr`.
