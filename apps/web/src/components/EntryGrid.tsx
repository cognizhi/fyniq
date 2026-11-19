import { CheckCircle2, CircleX, Eraser, Plus, Redo2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent, type ReactNode } from "react";
import { useDensity } from "../appearance";
import { evaluate } from "../format/arithmetic";
import { formatCount } from "../format/amount";
import { parseTable, tableText } from "../format/table";
import { useStatusPhrase } from "../shell/status";
import { cn } from "../lib/utils";
import { Icon } from "../ui";
import { Button } from "./ui/button";
import { apply, blockOf, cellProblem, clear, copyTable, fillDown, MAX_ROWS, paste, rowStatus, scrollToRow, setCells, valueFor, windowOf, type Block, type Cell, type Change, type GridColumn, type Pasted } from "./entryGridModel";

export type { GridColumn } from "./entryGridModel";

// The entry grid (PRD-001 FR-UX-07, FR-UX-20, NFR-A11Y-01; MASTER.md "Data grid"): the keyboard-first, spreadsheet-like grid for bulk
// work, such as journal lines, bank categorisation, budgets, opening balances and imports. It takes rows of text, draws only the rows
// in view so that a hundred thousand are as light as ten, and holds no state of its own about them: every change goes up through
// onChange as new rows, and comes back as props.
//
//  - Arrow keys move the active cell, with Shift they select a block, Home, End, Page Up and Down and Ctrl+Home and End go further.
//    Tab and Shift+Tab move across the row; at the last cell they leave the grid, so the keyboard is never trapped (WCAG 2.1.2).
//  - Typing starts editing a cell, Enter or F2 edits what it holds, Enter commits and moves down, Escape cancels, Delete clears the block.
//    Ctrl+Enter sets every cell of the selected block to what was typed (multi-cell edit); Ctrl+D fills down.
//  - In an amount cell, "=1250.50*3-80" is worked out exactly (never as a floating-point number) and the result is kept.
//  - A paste from Excel or Google Sheets is read as a table and shown, with the cells that do not fit flagged, before anything is set;
//    Apply or Enter sets them, Escape leaves the grid as it was. A copy puts plain decimals with an ASCII minus on the clipboard.
//  - Ctrl+Z and Ctrl+Y undo and redo, up to 200 steps. Each row says in words and an icon whether it is ready or what is wrong.
//
// It is an ARIA grid with one tab stop: the active cell, its row and column numbers announced by their positions. The change to the
// choice of AG Grid in MASTER.md is recorded in p1-app-shell's design.md: the range selection, fill and paste this needs are AG Grid
// Enterprise features, so the Community edition would have meant building them anyway, on top of a large dependency.

const HISTORY = 200;

type Props<R extends Record<string, string>> = {
  /** The grid's accessible name. */
  label: string;
  columns: readonly GridColumn<R>[];
  rows: readonly R[];
  onChange: (rows: R[]) => void;
  /** A new, empty row. */
  blank: () => R;
  /** The most decimals an amount has: the currency's scale. */
  scale?: number;
  maxRows?: number;
  /** The height of the scrolling body in pixels. */
  viewHeight?: number;
  /** Hides the toolbar's tools, as Basic mode does; the keyboard still works. */
  tools?: boolean;
  readOnly?: boolean;
  /** A line of help shown under the grid, the keys it takes by default. */
  help?: ReactNode;
};

const printable = (e: KeyboardEvent) => e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey;

export function EntryGrid<R extends Record<string, string>>({ label, columns, rows, onChange, blank, scale = 2, maxRows = MAX_ROWS, viewHeight = 360, tools = true, readOnly = false, help }: Props<R>) {
  const id = useId();
  const density = useDensity();
  const rowHeight = density === "compact" ? 30 : 38;
  const body = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [active, setActive] = useState<Cell>({ r: 0, c: 0 });
  const [anchor, setAnchor] = useState<Cell | null>(null);
  const [editing, setEditing] = useState<{ text: string; problem?: string } | null>(null);
  const [pending, setPending] = useState<{ at: Cell; table: string[][]; result: Pasted } | null>(null);
  const [said, setSaid] = useState<string | null>(null);
  const history = useRef<{ past: Change[]; future: Change[] }>({ past: [], future: [] });
  const [, bump] = useState(0);
  const refocus = useRef(false);

  const total = rows.length;
  const block: Block = useMemo(() => blockOf(active, anchor ?? active), [active, anchor]);
  const { first, last } = windowOf(scrollTop, viewHeight, rowHeight, total);
  const template = `9rem ${columns.map((c) => c.width ?? (c.kind === "amount" ? "10rem" : "minmax(10rem, 1fr)")).join(" ")}`;
  const status = useStatusPhrase(said);
  const cellId = (r: number, c: number) => `${id}-r${r}-c${c}`;

  const commit = useCallback(
    (change: Change, phrase?: string) => {
      if (change.patches.length === 0 && change.grew === 0) return;
      onChange(apply(rows, change, false, blank));
      history.current.past = [...history.current.past.slice(-(HISTORY - 1)), change];
      history.current.future = [];
      bump((n) => n + 1);
      if (phrase) setSaid(phrase);
    },
    [rows, onChange, blank],
  );
  const undo = () => {
    const change = history.current.past.pop();
    if (!change) return;
    history.current.future.push(change);
    onChange(apply(rows, change, true, blank));
    bump((n) => n + 1);
    setSaid(`Undone: ${change.patches.length} ${change.patches.length === 1 ? "cell" : "cells"}`);
  };
  const redo = () => {
    const change = history.current.future.pop();
    if (!change) return;
    history.current.past.push(change);
    onChange(apply(rows, change, false, blank));
    bump((n) => n + 1);
    setSaid(`Redone: ${change.patches.length} ${change.patches.length === 1 ? "cell" : "cells"}`);
  };

  const go = (to: Cell, extend = false) => {
    const r = Math.max(0, Math.min(total - 1, to.r));
    const c = Math.max(0, Math.min(columns.length - 1, to.c));
    if (extend) setAnchor(anchor ?? active);
    else setAnchor(null);
    setActive({ r, c });
    const top = scrollToRow(r, scrollTop, viewHeight, rowHeight);
    if (top !== scrollTop) {
      setScrollTop(top);
      if (body.current) body.current.scrollTop = top;
    }
    refocus.current = true;
  };

  // The active cell holds the one tab stop: focus follows it after the rows it is in are drawn.
  useEffect(() => {
    if (refocus.current && !editing && !pending) {
      refocus.current = false;
      document.getElementById(cellId(active.r, active.c))?.focus({ preventScroll: true });
    }
  });

  const startEdit = (text: string) => {
    if (readOnly) return;
    setEditing({ text });
  };

  // finish ends an edit: the text read as the column holds it (a sum worked out), set on the cell, or on every cell of the block.
  const finish = (all: boolean, move: Cell | null) => {
    if (!editing) return;
    const col = columns[active.c]!;
    let text = editing.text;
    if (col.kind === "amount" && text.trim().startsWith("=")) {
      const out = evaluate(text);
      if ("error" in out) {
        setEditing({ text, problem: out.error });
        return;
      }
      text = out.value;
    }
    const value = valueFor(col, text);
    const cells = all
      ? Array.from({ length: block.r2 - block.r1 + 1 }, (_, i) => ({ r: block.r1 + i, key: col.key, value }))
      : [{ r: active.r, key: col.key, value }];
    commit(setCells(rows, cells), all && cells.length > 1 ? `${cells.length} cells set` : undefined);
    setEditing(null);
    if (move) go(move);
    else refocus.current = true;
  };

  const addRow = (focus: boolean) => {
    if (total >= maxRows || readOnly) return;
    commit({ patches: [], grew: 1 });
    if (focus) {
      const r = total;
      setActive({ r, c: 0 });
      setAnchor(null);
      setScrollTop((t) => {
        const top = scrollToRow(r, t, viewHeight, rowHeight);
        if (body.current) body.current.scrollTop = top;
        return top;
      });
      refocus.current = true;
    }
  };
  const lastIsBlank = total > 0 && rowStatus(columns, rows[total - 1]!, scale).state === "empty";

  const onGridKey = (e: KeyboardEvent) => {
    if (editing || pending || total === 0) return;
    const ctrl = e.ctrlKey || e.metaKey;
    const shift = e.shiftKey;
    const at = active;
    const page = Math.max(1, Math.floor(viewHeight / rowHeight) - 1);
    const step = (dr: number, dc: number) => {
      e.preventDefault();
      go({ r: at.r + dr, c: at.c + dc }, shift);
    };
    switch (e.key) {
      case "ArrowDown":
        if (ctrl) {
          e.preventDefault();
          go({ r: total - 1, c: at.c }, shift);
        } else if (at.r === total - 1 && !shift && !lastIsBlank && !readOnly) {
          e.preventDefault();
          addRow(true);
        } else step(1, 0);
        return;
      case "ArrowUp":
        if (ctrl) {
          e.preventDefault();
          go({ r: 0, c: at.c }, shift);
        } else step(-1, 0);
        return;
      case "ArrowRight":
        if (ctrl) {
          e.preventDefault();
          go({ r: at.r, c: columns.length - 1 }, shift);
        } else step(0, 1);
        return;
      case "ArrowLeft":
        if (ctrl) {
          e.preventDefault();
          go({ r: at.r, c: 0 }, shift);
        } else step(0, -1);
        return;
      case "Home":
        e.preventDefault();
        go(ctrl ? { r: 0, c: 0 } : { r: at.r, c: 0 }, shift);
        return;
      case "End":
        e.preventDefault();
        go(ctrl ? { r: total - 1, c: columns.length - 1 } : { r: at.r, c: columns.length - 1 }, shift);
        return;
      case "PageDown":
        step(page, 0);
        return;
      case "PageUp":
        step(-page, 0);
        return;
      case "Tab": {
        const next = shift ? (at.c > 0 ? { r: at.r, c: at.c - 1 } : at.r > 0 ? { r: at.r - 1, c: columns.length - 1 } : null) : at.c < columns.length - 1 ? { r: at.r, c: at.c + 1 } : at.r < total - 1 ? { r: at.r + 1, c: 0 } : null;
        if (next) {
          e.preventDefault();
          go(next);
        } else if (!shift && !lastIsBlank && !readOnly) {
          e.preventDefault();
          addRow(true);
        } // otherwise Tab leaves the grid
        return;
      }
      case "Enter":
      case "F2":
        e.preventDefault();
        startEdit(rows[at.r]?.[columns[at.c]!.key] ?? "");
        return;
      case "Delete":
      case "Backspace":
        e.preventDefault();
        if (!readOnly) commit(clear(rows, columns, block), `${(block.r2 - block.r1 + 1) * (block.c2 - block.c1 + 1)} cells cleared`);
        return;
      case "Escape":
        if (anchor) {
          e.preventDefault();
          setAnchor(null);
        }
        return;
      default:
    }
    if (ctrl && e.key.toLowerCase() === "a") {
      e.preventDefault();
      setAnchor({ r: 0, c: 0 });
      setActive({ r: total - 1, c: columns.length - 1 });
      refocus.current = true;
    } else if (ctrl && e.key.toLowerCase() === "d") {
      e.preventDefault();
      if (!readOnly) commit(fillDown(rows, columns, block), "Filled down");
    } else if (ctrl && e.key.toLowerCase() === "z") {
      e.preventDefault();
      if (shift) redo();
      else undo();
    } else if (ctrl && e.key.toLowerCase() === "y") {
      e.preventDefault();
      redo();
    } else if (printable(e) && !readOnly) {
      e.preventDefault();
      startEdit(e.key);
    }
  };

  const onEditKey = (e: KeyboardEvent) => {
    e.stopPropagation();
    if (e.key === "Enter") {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) finish(true, null);
      else finish(false, e.shiftKey ? { r: active.r - 1, c: active.c } : { r: active.r + 1, c: active.c });
      if (!e.ctrlKey && !e.metaKey && !e.shiftKey && active.r === total - 1 && !readOnly && editing && editing.text.trim() !== "") {
        // Enter on the last row, once it holds something, goes on to a new one.
        window.setTimeout(() => addRow(true), 0);
      }
    } else if (e.key === "Tab") {
      const right = !e.shiftKey;
      const next = right ? (active.c < columns.length - 1 ? { r: active.r, c: active.c + 1 } : active.r < total - 1 ? { r: active.r + 1, c: 0 } : null) : active.c > 0 ? { r: active.r, c: active.c - 1 } : active.r > 0 ? { r: active.r - 1, c: columns.length - 1 } : null;
      if (next) {
        e.preventDefault();
        finish(false, next);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      setEditing(null);
      refocus.current = true;
    }
  };

  const onCopy = (e: ClipboardEvent) => {
    if (editing) return;
    e.preventDefault();
    e.clipboardData.setData("text/plain", tableText(copyTable(rows, columns, block)));
  };

  // A paste is read as a table and shown before it is set (FR-UX-20); one cell goes into the cell's editor instead, where it shows as typed.
  const onPaste = (e: ClipboardEvent) => {
    if (editing || readOnly || total === 0) return;
    const text = e.clipboardData.getData("text/plain");
    e.preventDefault();
    let table: string[][];
    try {
      table = parseTable(text);
    } catch {
      setSaid("That paste is too large to read");
      return;
    }
    if (table.length === 0) return;
    if (table.length === 1 && table[0]!.length === 1) {
      startEdit(valueFor(columns[active.c]!, table[0]![0]!));
      return;
    }
    setPending({ at: { r: block.r1, c: block.c1 }, table, result: paste(rows, columns, { r: block.r1, c: block.c1 }, table, scale, maxRows) });
  };
  const applyPaste = () => {
    if (!pending) return;
    commit(pending.result.change, `Pasted ${formatCount(pending.result.cells)} cells`);
    setPending(null);
    refocus.current = true;
  };
  const cancelPaste = () => {
    setPending(null);
    refocus.current = true;
  };

  const rowsOnScreen = [];
  for (let r = first; r < last; r++) {
    const row = rows[r]!;
    const s = rowStatus(columns, row, scale);
    rowsOnScreen.push(
      <div key={r} role="row" aria-rowindex={r + 2} className="absolute start-0 grid w-full items-stretch border-b border-border" style={{ top: r * rowHeight, height: rowHeight, gridTemplateColumns: template }}>
        <div role="gridcell" className="flex items-center px-2" aria-colindex={1} id={`${id}-status-${r}`}>
          {s.state === "ok" && (
            <span className="fy-chip fy-chip--good">
              <Icon of={CheckCircle2} /> Ready
            </span>
          )}
          {s.state === "error" && (
            <span className="fy-chip fy-chip--critical" title={s.problems.join(" ")}>
              <Icon of={CircleX} /> {s.problems.length === 1 ? "1 problem" : `${s.problems.length} problems`}
            </span>
          )}
          {s.state === "empty" && <span className="fy-small fy-secondary">Empty</span>}
        </div>
        {columns.map((col, c) => {
          const here = active.r === r && active.c === c;
          const inBlock = r >= block.r1 && r <= block.r2 && c >= block.c1 && c <= block.c2 && (anchor !== null || here);
          const problem = cellProblem(col, row, scale);
          const value = row[col.key] ?? "";
          const shown = col.kind === "choice" ? (col.choices?.find((x) => x.value === value)?.label ?? value) : value;
          return (
            <div
              key={col.key}
              id={cellId(r, c)}
              role="gridcell"
              aria-colindex={c + 2}
              aria-selected={inBlock && anchor !== null ? true : undefined}
              aria-readonly={readOnly || undefined}
              aria-invalid={problem ? true : undefined}
              aria-describedby={problem ? `${id}-status-${r}` : undefined}
              tabIndex={here && !editing ? 0 : -1}
              data-active={here || undefined}
              onFocus={(e) => {
                // Focus arriving on a cell, by Tab or a click, makes it the active one.
                if (e.target === e.currentTarget && !editing && !here) setActive({ r, c });
              }}
              onMouseDown={(e) => {
                if (editing || pending) return;
                if (e.shiftKey) go({ r, c }, true);
                else {
                  setActive({ r, c });
                  setAnchor(null);
                }
              }}
              onDoubleClick={() => {
                setActive({ r, c });
                startEdit(value);
              }}
              className={cn(
                "flex items-center px-2 outline-none",
                col.kind === "amount" ? "fy-num justify-end" : "justify-start",
                inBlock && anchor !== null && "bg-primary-subtle",
                here && "outline-2 -outline-offset-2 outline-primary",
                problem && "border-b-2 border-destructive",
              )}
            >
              {here && editing ? (
                <input
                  // eslint-disable-next-line jsx-a11y/no-autofocus -- the cell's editor opens because the person began to type in it
                  autoFocus
                  className={cn("h-full w-full min-w-0 bg-transparent outline-none", col.kind === "amount" && "text-end")}
                  aria-label={`${col.title}, row ${r + 1}`}
                  aria-invalid={editing.problem ? true : undefined}
                  aria-describedby={editing.problem ? `${id}-edit-problem` : undefined}
                  value={editing.text}
                  onChange={(e) => setEditing({ text: e.target.value })}
                  onKeyDown={onEditKey}
                  onBlur={() => editing && finish(false, null)}
                  onFocus={(e) => e.currentTarget.setSelectionRange(e.currentTarget.value.length, e.currentTarget.value.length)}
                />
              ) : (
                <span className="truncate">{shown}</span>
              )}
            </div>
          );
        })}
      </div>,
    );
  }

  const can = { undo: history.current.past.length > 0, redo: history.current.future.length > 0 };
  return (
    <div className="fy-stack fy-stack--2">
      {tools && (
        <div className="fy-row fy-row--wrap gap-2" role="toolbar" aria-label={`${label}: tools`}>
          <Button size="sm" variant="ghost" type="button" onClick={undo} disabled={!can.undo || readOnly} title="Undo the last change (Ctrl+Z)">
            <Icon of={Undo2} /> Undo
          </Button>
          <Button size="sm" variant="ghost" type="button" onClick={redo} disabled={!can.redo || readOnly} title="Redo (Ctrl+Y)">
            <Icon of={Redo2} /> Redo
          </Button>
          <Button size="sm" variant="ghost" type="button" onClick={() => commit(fillDown(rows, columns, block), "Filled down")} disabled={readOnly || total === 0} title="Copy the top cell of the selection down it, or the cell above (Ctrl+D)">
            Fill down
          </Button>
          <Button size="sm" variant="ghost" type="button" onClick={() => commit(clear(rows, columns, block))} disabled={readOnly || total === 0} title="Empty the selected cells (Delete)">
            <Icon of={Eraser} /> Clear
          </Button>
          <Button size="sm" variant="ghost" type="button" onClick={() => addRow(true)} disabled={readOnly || total >= maxRows} title="Add an empty row at the end">
            <Icon of={Plus} /> Add row
          </Button>
          <span className="fy-small fy-secondary ms-auto" aria-live="off">
            {formatCount(total)} {total === 1 ? "row" : "rows"}
          </span>
        </div>
      )}
      {pending && (
        <div className="fy-banner fy-banner--info" role="group" aria-label="Paste preview" onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            cancelPaste();
          } else if (e.key === "Enter" && e.target === e.currentTarget) {
            e.preventDefault();
            applyPaste();
          }
        }} tabIndex={-1} ref={(el) => el?.focus({ preventScroll: true })}>
          <div className="fy-banner__text fy-stack fy-stack--2">
            <strong>
              Paste {formatCount(pending.result.cells)} cells from row {pending.at.r + 1}, {columns[pending.at.c]!.title}?
            </strong>
            <span className="fy-small">
              {pending.table.length} {pending.table.length === 1 ? "row" : "rows"} of up to {Math.max(...pending.table.map((l) => l.length))} {pending.table.some((l) => l.length > 1) ? "columns" : "column"}
              {pending.result.added > 0 ? `; ${formatCount(pending.result.added)} rows are added at the end` : ""}.
            </span>
            {pending.result.leftOutRows > 0 && <span className="fy-small">{formatCount(pending.result.leftOutRows)} rows past the limit of {formatCount(maxRows)} are left out.</span>}
            {pending.result.leftOutColumns > 0 && <span className="fy-small">{pending.result.leftOutColumns} columns past the last are left out.</span>}
            <div className="overflow-x-auto">
              <table className="fy-table fy-table--compact" aria-label="What would be set">
                <tbody>
                  {pending.table.slice(0, 5).map((line, i) => (
                    <tr key={i}>
                      {line.map((text, j) => {
                        const col = columns[pending.at.c + j];
                        const bad = pending.result.flagged.find((f) => f.r === pending.at.r + i && f.c === pending.at.c + j);
                        return (
                          <td key={j} className={col?.kind === "amount" ? "fy-num" : undefined}>
                            {col ? valueFor(col, text) : <span className="fy-secondary">left out</span>}
                            {bad && (
                              <span className="fy-chip fy-chip--critical ms-2" title={bad.problem}>
                                <Icon of={CircleX} /> Does not fit
                              </span>
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {pending.table.length > 5 && <span className="fy-small fy-secondary">…and {formatCount(pending.table.length - 5)} more rows.</span>}
            {pending.result.flagged.length > 0 && (
              <span className="fy-small">
                {pending.result.flagged.length} {pending.result.flagged.length === 1 ? "cell does" : "cells do"} not fit its column. They are set as pasted and their rows say what is wrong.
              </span>
            )}
          </div>
          <div className="fy-banner__action fy-row gap-2">
            <Button variant="primary" size="sm" type="button" onClick={applyPaste}>
              Apply paste
            </Button>
            <Button variant="ghost" size="sm" type="button" onClick={cancelPaste}>
              Cancel
            </Button>
          </div>
        </div>
      )}
      <div
        role="grid"
        aria-label={label}
        aria-rowcount={total + 1}
        aria-colcount={columns.length + 1}
        aria-multiselectable={true}
        aria-describedby={`${id}-keys`}
        className="rounded-md border border-border bg-surface-1"
        onKeyDown={onGridKey}
        onCopy={onCopy}
        onPaste={onPaste}
      >
        <div role="row" aria-rowindex={1} className="grid border-b border-border-strong bg-surface-2 font-medium" style={{ gridTemplateColumns: template }}>
          <div role="columnheader" aria-colindex={1} className="px-2 py-2">
            Status
          </div>
          {columns.map((col, c) => (
            <div key={col.key} role="columnheader" aria-colindex={c + 2} className={cn("px-2 py-2", col.kind === "amount" && "text-end")}>
              {col.title}
              {col.required && (
                <span className="fy-req" aria-hidden="true">
                  {" "}
                  *
                </span>
              )}
            </div>
          ))}
        </div>
        <div ref={body} className="relative overflow-auto" style={{ height: Math.min(viewHeight, Math.max(rowHeight, total * rowHeight)) }} onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)} data-testid="grid-body">
          <div role="rowgroup" className="relative" style={{ height: total * rowHeight }}>
            {rowsOnScreen}
          </div>
          {total === 0 && <p className="fy-secondary p-3">No rows yet. Add a row, or paste from a spreadsheet.</p>}
        </div>
      </div>
      {editing?.problem && (
        <p className="fy-error" id={`${id}-edit-problem`} role="alert">
          {editing.problem}
        </p>
      )}
      {status}
      <p className="fy-help" id={`${id}-keys`}>
        {help ?? (
          <>
            Arrow keys move, Enter edits, Escape cancels, Tab goes across, Delete clears. In an amount, start a sum with =. Ctrl+D fills down, Ctrl+Z undoes, and you can paste
            cells from a spreadsheet. Type to start editing; {formatCount(maxRows)} rows at most.
          </>
        )}
      </p>
    </div>
  );
}
