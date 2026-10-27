import { clipboardAmount } from "./amount";

// csvCell is one cell of an export (FR-UX-20, RPT exports): plain decimals with an ASCII minus, and text that a
// spreadsheet would read as a formula made inert with a leading apostrophe.
export function csvCell(v: string, amount = false): string {
  let s = amount && v ? clipboardAmount(v) : v;
  if (!amount && /^[=+\-@\t\r]/.test(s)) s = `'${s}`;
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}
