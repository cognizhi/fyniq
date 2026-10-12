# Page override · Reports and financial statements

Overrides MASTER.md for statements and management reports (FR-RPT-01…03, FR-UX-04).

## Layout
- Toolbar: report name, period picker, comparison (none / prior period / prior year / budget),
  "as at" ledger version (FR-RPT-08), export menu (XLSX, PDF, CSV), "Ask about this".
- The statement is a semantic table (TanStack) with a sticky header row and sticky first
  column; group rows can be collapsed; totals weight 600 with a top rule.
- Every figure is a button that opens the drill-down: breakdown → ledger lines → source document
  → history (Trace panel); breadcrumbs keep the report context.

## Basic vs Expert
- Basic: the curated report set (P&L, balance sheet, cash and runway, owed to us / we owe, GST
  summary) with a plain-English explanation under each section and no account codes.
- Expert: account codes, comparatives side by side, variance columns (amount and %), dimension
  pivots, TB, GL and journals.

## Density and print
Compact by default in Expert mode. Print styles: no chrome, repeated header rows, page numbers.
The PDF export uses the Typst templates, not the browser's print.

## Rules
No colour coding of positive and negative numbers by default (user setting). Variance columns
use sign and arrow icons. The figures never animate.
