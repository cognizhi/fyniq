# Page override · Home

Overrides MASTER.md for the Home page (PRD §7.1, FR-KPI-01, FR-CTL-02).

## Purpose
"Today": what the money is doing and what needs me. The page shows work, not only charts.

## Layout (≥ 1024 px)
1. **Pulse card** (full width): cash today, runway in months, books health. It is one of the two
   places `--ai-gradient` may appear, as a 4 px top band only, never behind text. Figures at
   `--fs-3xl`, tabular; a number ticker on first load only (none with reduced motion).
2. **Needs attention** (left, 7 of 12 columns): the top 5 Inbox items grouped by type, each with a
   status chip, amount and due date; "Open Inbox (23)".
3. **Stat tiles** (right, 5 of 12 columns, 2 × 3 grid): burn, revenue MTD, gross margin, owed to
   us, we owe, GST due. Each drills down (FR-UX-04).
4. **Cash chart** below: 13 months of cash (line, one series, no legend) with a forecast segment
   dashed and labelled "Projection" (ENG-05).
5. **Upcoming deadlines** list (CPF, GST F5, ECI), with ⏰ chips.

## Basic vs Expert
- Basic: pulse card, needs attention, four tiles (cash, burn, owed to us, we owe), deadlines.
- Expert: adds gross margin and GST tiles, the cash chart with drill-down to ledger lines, and
  unrealised FX.

## Density
Comfortable regardless of the user's density setting (Home is a reading page).

## Accessibility
The pulse figures are real text, not canvas. The chart has a one-sentence summary ("Cash fell 8%
in September to S$412,300") and a table view.
