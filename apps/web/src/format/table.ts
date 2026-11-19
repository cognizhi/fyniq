// The table a spreadsheet puts on the clipboard (PRD-001 FR-UX-07, FR-UX-20): Excel and Google Sheets copy cells as text with a tab
// between cells and a line break between rows, and put a cell that holds a tab, a line break or a quote in quotes with the quotes
// doubled. parseTable reads that back into rows of cells, and reads a block of comma-separated values too, such as a CSV pasted as
// text; tableText writes cells the same way for a copy. Cells stay text: amounts are read by pastedAmount, never made numbers here.

const MAX_CELLS = 2_000_000;

function split(text: string, sep: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  let started = false; // the cell began with a quote, so its quotes are the spreadsheet's
  let cells = 0;
  const endCell = () => {
    row.push(cell);
    cell = "";
    started = false;
    if (++cells > MAX_CELLS) throw new RangeError("too many cells");
  };
  for (let i = 0; i < text.length; i++) {
    const c = text[i]!;
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') {
        cell += '"';
        i++;
      } else if (c === '"') {
        quoted = false;
      } else {
        cell += c;
      }
      continue;
    }
    if (c === '"' && cell === "" && !started) {
      quoted = true;
      started = true;
    } else if (c === sep) {
      endCell();
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && text[i + 1] === "\n") i++;
      endCell();
      rows.push(row);
      row = [];
    } else {
      cell += c;
    }
  }
  if (cell !== "" || row.length > 0 || started) {
    endCell();
    rows.push(row);
  }
  return rows;
}

/** parseTable reads pasted text as rows of cells; [] for nothing to paste. A trailing line break adds no empty row. */
export function parseTable(text: string): string[][] {
  if (text.trim() === "") return [];
  const hasTab = text.includes("\t");
  const rows = split(text, hasTab ? "\t" : text.includes(",") && /\n/.test(text.trim()) ? "," : "\t");
  // A one-line paste with commas but no tab is one cell: "1,234.56" is an amount, not two cells.
  return rows.length === 1 && !hasTab ? [[text.replace(/\r?\n$/, "")]] : rows;
}

/** tableText writes rows of cells as a spreadsheet would copy them. */
export function tableText(rows: readonly (readonly string[])[]): string {
  return rows.map((r) => r.map((c) => (/[\t\n\r"]/.test(c) ? `"${c.replaceAll('"', '""')}"` : c)).join("\t")).join("\n");
}
