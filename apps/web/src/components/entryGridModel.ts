import { pastedAmount } from "../format/amount";
import { isZero } from "../format/decimal";

// The model of the entry grid (PRD-001 FR-UX-07, FR-UX-20), apart from its drawing so that every rule can be tested without a
// browser: which rows are on screen, what a cell is allowed to hold, and each change as a list of cell patches that can be undone.
// Rows are records of text. An amount is a plain decimal string with an ASCII minus and is never made a number (golden rule 1).

export interface GridColumn<R extends Record<string, string>> {
  key: keyof R & string;
  title: string;
  kind: "text" | "amount" | "choice";
  /** The column's width as a CSS grid track, such as "12rem" or "minmax(10rem, 1fr)". */
  width?: string;
  choices?: { value: string; label: string }[];
  required?: boolean;
  /** A further rule for one cell: what is wrong in plain words, or null. */
  check?: (value: string, row: R) => string | null;
}

export interface Cell {
  r: number;
  c: number;
}

export interface Block {
  r1: number;
  r2: number;
  c1: number;
  c2: number;
}

/** A change to one cell, with what it was, so that it can be undone. */
export interface Patch {
  r: number;
  key: string;
  from: string;
  to: string;
}

/** A change as one step of the history: the cells it set and the rows it added after the last. */
export interface Change {
  patches: Patch[];
  grew: number;
}

export const MAX_ROWS = 100_000;

/** blockOf is the rectangle between two cells, whichever is first. */
export function blockOf(a: Cell, b: Cell): Block {
  return { r1: Math.min(a.r, b.r), r2: Math.max(a.r, b.r), c1: Math.min(a.c, b.c), c2: Math.max(a.c, b.c) };
}

/** windowOf is the rows to draw: those in view and a few beyond, so a scroll does not show blanks. [first, last). */
export function windowOf(scrollTop: number, viewHeight: number, rowHeight: number, total: number, overscan = 6): { first: number; last: number } {
  if (total <= 0) return { first: 0, last: 0 };
  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const last = Math.min(total, Math.ceil((scrollTop + viewHeight) / rowHeight) + overscan);
  return { first: Math.min(first, total), last: Math.max(Math.min(first, total), last) };
}

/** scrollToRow is the scroll position that shows row r, moving as little as it can. */
export function scrollToRow(r: number, scrollTop: number, viewHeight: number, rowHeight: number): number {
  const top = r * rowHeight;
  if (top < scrollTop) return top;
  if (top + rowHeight > scrollTop + viewHeight) return Math.max(0, top + rowHeight - viewHeight);
  return scrollTop;
}

const plain = /^-?\d+(\.\d+)?$/;

/** cellProblem is what is wrong with one cell, in plain words (ENG-23), or null. An empty cell is wrong only where one is required. */
export function cellProblem<R extends Record<string, string>>(col: GridColumn<R>, row: R, scale: number): string | null {
  const value = row[col.key] ?? "";
  if (value === "") return col.required ? `Enter the ${col.title.toLowerCase()}.` : null;
  if (col.kind === "amount") {
    if (!plain.test(value)) return `${col.title} is not an amount: use digits and a decimal point, or start a sum with =.`;
    const decimals = value.split(".")[1]?.length ?? 0;
    if (decimals > scale) return `${col.title} has ${decimals} decimals and this amount has at most ${scale}.`;
  }
  if (col.kind === "choice" && col.choices && !col.choices.some((c) => c.value === value)) return `${col.title} is not one of the choices.`;
  return col.check?.(value, row) ?? null;
}

export type RowState = "empty" | "ok" | "error";

/** rowStatus says whether a row is blank, ready, or has problems, and what they are. */
export function rowStatus<R extends Record<string, string>>(cols: readonly GridColumn<R>[], row: R, scale: number): { state: RowState; problems: string[] } {
  if (cols.every((c) => (row[c.key] ?? "") === "")) return { state: "empty", problems: [] };
  const problems = cols.map((c) => cellProblem(c, row, scale)).filter((p): p is string => p !== null);
  return { state: problems.length > 0 ? "error" : "ok", problems };
}

/** apply sets the patches' cells in rows, or undoes them, and drops the rows the change added; it returns new rows. */
export function apply<R extends Record<string, string>>(rows: readonly R[], change: Change, undo: boolean, blank: () => R): R[] {
  let next = rows.slice();
  if (undo) next = next.slice(0, Math.max(0, next.length - change.grew));
  else for (let i = 0; i < change.grew; i++) next.push(blank());
  const touched = new Map<number, R>();
  for (const p of change.patches) {
    const row = touched.get(p.r) ?? { ...next[p.r]! };
    (row as Record<string, string>)[p.key] = undo ? p.from : p.to;
    touched.set(p.r, row);
  }
  for (const [r, row] of touched) if (r < next.length) next[r] = row;
  return next;
}

/** setCells is a change that sets cell (r, key) to each value, leaving out those already so. */
export function setCells<R extends Record<string, string>>(rows: readonly R[], cells: { r: number; key: string; value: string }[], grew = 0): Change {
  const patches: Patch[] = [];
  for (const { r, key, value } of cells) {
    const from = rows[r]?.[key] ?? "";
    if (from !== value) patches.push({ r, key, from, to: value });
  }
  return { patches, grew };
}

/** fillDown copies the top cell of each column of the block down it (Ctrl+D); a single row copies the row above it. */
export function fillDown<R extends Record<string, string>>(rows: readonly R[], cols: readonly GridColumn<R>[], b: Block): Change {
  const single = b.r1 === b.r2;
  const source = single ? b.r1 - 1 : b.r1;
  if (source < 0) return { patches: [], grew: 0 };
  const cells: { r: number; key: string; value: string }[] = [];
  for (let c = b.c1; c <= b.c2; c++) {
    const key = cols[c]!.key;
    for (let r = single ? b.r1 : b.r1 + 1; r <= b.r2; r++) cells.push({ r, key, value: rows[source]![key] ?? "" });
  }
  return setCells(rows, cells);
}

/** clear is a change that empties the block's cells. */
export function clear<R extends Record<string, string>>(rows: readonly R[], cols: readonly GridColumn<R>[], b: Block): Change {
  const cells: { r: number; key: string; value: string }[] = [];
  for (let r = b.r1; r <= b.r2; r++) for (let c = b.c1; c <= b.c2; c++) cells.push({ r, key: cols[c]!.key, value: "" });
  return setCells(rows, cells);
}

/** valueFor reads text typed or pasted into a cell as that column holds it: an amount as a plain decimal, a choice by its value or its name. */
export function valueFor<R extends Record<string, string>>(col: GridColumn<R>, text: string): string {
  const t = text.replace(/\r$/, "");
  if (col.kind === "amount") return pastedAmount(t) ?? t.trim();
  if (col.kind === "choice") {
    const wanted = t.trim().toLowerCase();
    return col.choices?.find((c) => c.value.toLowerCase() === wanted || c.label.toLowerCase() === wanted)?.value ?? t.trim();
  }
  return t;
}

export interface Pasted {
  change: Change;
  /** How many cells the paste sets, and how many rows it adds. */
  cells: number;
  added: number;
  /** Cells the pasted text does not fit as that column holds it, which the paste still sets and the row's status shows. */
  flagged: { r: number; c: number; text: string; value: string; problem: string }[];
  /** What the paste leaves out: rows past the limit, and columns past the last. */
  leftOutRows: number;
  leftOutColumns: number;
}

/** paste maps a table of text onto the grid from the cell at (r, c): each cell read as its column holds it, rows added for what runs past the end. */
export function paste<R extends Record<string, string>>(rows: readonly R[], cols: readonly GridColumn<R>[], at: Cell, table: string[][], scale: number, max = MAX_ROWS): Pasted {
  const room = Math.max(0, max - at.r);
  const taken = table.slice(0, room);
  const grew = Math.max(0, at.r + taken.length - rows.length);
  const cells: { r: number; key: string; value: string }[] = [];
  const flagged: Pasted["flagged"] = [];
  let leftOutColumns = 0;
  taken.forEach((line, i) => {
    line.forEach((text, j) => {
      const c = at.c + j;
      const col = cols[c];
      if (!col) {
        if (i === 0) leftOutColumns++;
        return;
      }
      const value = valueFor(col, text);
      cells.push({ r: at.r + i, key: col.key, value });
      const probe = { ...(rows[at.r + i] ?? {}), [col.key]: value } as R;
      const problem = cellProblem({ ...col, required: false }, probe, scale);
      if (problem) flagged.push({ r: at.r + i, c, text, value, problem });
    });
  });
  // The rows past the end are new: their cells start from nothing, so a value equal to a blank is no patch.
  const base = rows.concat(Array.from({ length: grew }, () => ({}) as R));
  const change = setCells(base, cells, grew);
  return { change, cells: cells.length, added: grew, flagged, leftOutRows: table.length - taken.length, leftOutColumns };
}

/** copyTable is the text rows of a block, ready for tableText: amounts as plain decimals with an ASCII minus (FR-UX-20). */
export function copyTable<R extends Record<string, string>>(rows: readonly R[], cols: readonly GridColumn<R>[], b: Block): string[][] {
  const out: string[][] = [];
  for (let r = b.r1; r <= b.r2; r++) {
    const line: string[] = [];
    for (let c = b.c1; c <= b.c2; c++) {
      const col = cols[c]!;
      const v = rows[r]?.[col.key] ?? "";
      line.push(col.kind === "amount" && plain.test(v) ? (isZero(v) ? v.replace(/^-/, "") : v) : col.kind === "choice" ? (col.choices?.find((x) => x.value === v)?.label ?? v) : v);
    }
    out.push(line);
  }
  return out;
}
