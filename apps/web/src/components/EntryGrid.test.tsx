import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { EntryGrid } from "./EntryGrid";
import type { GridColumn } from "./entryGridModel";

type Row = { account: string; memo: string; debit: string };
const cols: GridColumn<Row>[] = [
  { key: "account", title: "Account", kind: "choice", required: true, choices: [{ value: "6200", label: "6200 Office rent" }, { value: "2010", label: "2010 Accounts payable" }] },
  { key: "memo", title: "Memo", kind: "text" },
  { key: "debit", title: "Debit", kind: "amount" },
];
const blank = (): Row => ({ account: "", memo: "", debit: "" });

let latest: Row[] = [];
function Harness({ initial, tools = true }: { initial: Row[]; tools?: boolean }) {
  const [rows, setRows] = useState(initial);
  latest = rows;
  return <EntryGrid label="Journal lines" columns={cols} rows={rows} onChange={setRows} blank={blank} tools={tools} viewHeight={190} />;
}
const start = (n = 3, fill: Partial<Row> = {}) => render(<Harness initial={Array.from({ length: n }, () => ({ ...blank(), ...fill }))} />);
const cell = (r: number, c: number) => document.querySelector(`[role="gridcell"][aria-colindex="${c + 2}"]`) && (screen.getAllByRole("row")[r + 1]!.querySelectorAll('[role="gridcell"]')[c + 1] as HTMLElement);
const focus = (el: HTMLElement) => act(() => el.focus()); // a real focus is its own event, so the state it sets is drawn before the next
const active = () => document.querySelector('[data-active="true"]') as HTMLElement;
const key = (el: Element, k: string, mods: Partial<KeyboardEventInit> = {}) => fireEvent.keyDown(el, { key: k, ...mods });
const type = (text: string) => {
  const input = screen.getByRole("textbox") as HTMLInputElement;
  fireEvent.change(input, { target: { value: text } });
  return input;
};

// req: FR-UX-07
// req: FR-UX-20
// req: NFR-A11Y-01
describe("EntryGrid", () => {
  it("is a labelled grid with headers, row and column counts and a status on each row, and axe finds nothing", async () => {
    start(2, { account: "6200", debit: "10.00" });
    const grid = screen.getByRole("grid", { name: "Journal lines" });
    expect(grid.getAttribute("aria-rowcount")).toBe("3");
    expect(grid.getAttribute("aria-colcount")).toBe("4");
    expect(within(grid).getAllByRole("columnheader").map((h) => h.textContent?.replace(/\s*\*$/, ""))).toEqual(["Status", "Account", "Memo", "Debit"]);
    expect(within(grid).getAllByText("Ready")).toHaveLength(2);
    expect(within(grid).getAllByRole("row")[1]!.getAttribute("aria-rowindex")).toBe("2");
    await noViolations(document.body);
  });

  it("draws only the rows in view of a hundred thousand, and the last ones when scrolled there", () => {
    const rows = Array.from({ length: 100_000 }, (_, i) => ({ account: "6200", memo: `Line ${i + 1}`, debit: "1.00" }));
    render(<EntryGrid label="Big" columns={cols} rows={rows} onChange={() => undefined} blank={blank} viewHeight={190} />);
    expect(screen.getAllByRole("row").length).toBeLessThan(30);
    expect(screen.getByRole("grid").getAttribute("aria-rowcount")).toBe("100001");
    expect(screen.getByText("Line 1")).toBeTruthy();
    const body = screen.getByTestId("grid-body");
    body.scrollTop = 38 * 99_990;
    fireEvent.scroll(body);
    expect(screen.getByText("Line 100000")).toBeTruthy();
    expect(screen.queryByText("Line 1")).toBeNull();
    expect(screen.getAllByRole("row").length).toBeLessThan(30);
    expect(screen.getByText("100,000 rows")).toBeTruthy();
  });

  it("moves with the arrow keys, extends a block with Shift, and holds one tab stop", () => {
    start(4);
    expect(document.querySelectorAll('[role="gridcell"][tabindex="0"]')).toHaveLength(1);
    focus(cell(0, 0)!);
    key(cell(0, 0)!, "ArrowRight");
    key(active(), "ArrowDown");
    expect(active().getAttribute("aria-colindex")).toBe("3");
    expect(active().id).toBe(cell(1, 1)!.id);
    key(active(), "ArrowDown", { shiftKey: true });
    key(active(), "ArrowRight", { shiftKey: true });
    expect(document.querySelectorAll('[role="gridcell"][aria-selected="true"]')).toHaveLength(4);
    key(active(), "Escape");
    expect(document.querySelectorAll('[aria-selected="true"]')).toHaveLength(0);
    key(active(), "Home");
    expect(active().id).toBe(cell(2, 0)!.id);
    key(active(), "End", { ctrlKey: true });
    expect(active().id).toBe(cell(3, 2)!.id);
    key(active(), "Home", { ctrlKey: true });
    expect(active().id).toBe(cell(0, 0)!.id);
    expect(document.querySelectorAll('[role="gridcell"][tabindex="0"]')).toHaveLength(1);
  });

  it("starts editing when the person types, commits with Enter and goes down, and cancels with Escape", () => {
    start(3);
    focus(cell(0, 1)!);
    key(cell(0, 1)!, "ArrowDown");
    key(active(), "R");
    const input = type("Rent");
    key(input, "Enter");
    expect(latest[1]!.memo).toBe("Rent");
    expect(screen.getByRole("status").textContent).toBe("Memo, row 2, set to Rent"); // a change is announced politely (WCAG 4.1.3)
    expect(active().id).toBe(cell(2, 1)!.id);
    key(active(), "Enter");
    key(screen.getByRole("textbox"), "Escape");
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(latest[2]!.memo).toBe("");
  });

  it("works out a sum in an amount cell exactly, and says why one cannot be", () => {
    start(2);
    focus(cell(0, 2)!);
    key(cell(0, 0)!, "ArrowRight");
    key(active(), "ArrowRight");
    key(active(), "Enter");
    key(type("=1250.50*3-80"), "Enter");
    expect(latest[0]!.debit).toBe("3671.50");
    key(active(), "ArrowUp");
    key(active(), "Enter");
    key(type("=10/3"), "Enter");
    expect(screen.getByRole("alert").textContent).toContain("does not come out exactly");
    expect(screen.getByRole("textbox").getAttribute("aria-invalid")).toBe("true");
    expect(latest[0]!.debit).toBe("3671.50");
    key(screen.getByRole("textbox"), "Escape");
  });

  it("reads a pasted amount in the forms people paste, and flags one that does not fit in the row's status", () => {
    start(2);
    focus(cell(0, 2)!);
    key(cell(0, 0)!, "ArrowRight");
    key(active(), "ArrowRight");
    key(active(), "Enter");
    key(type("(1,234.56)"), "Enter");
    expect(latest[0]!.debit).toBe("-1234.56");
    key(active(), "ArrowUp");
    key(active(), "Enter");
    key(type("12.345"), "Enter");
    expect(within(screen.getAllByRole("row")[1]!).getByText("2 problems")).toBeTruthy();
  });

  it("shows a pasted table before setting it, sets it on Apply, and leaves the grid as it was on Escape", () => {
    start(2);
    const grid = screen.getByRole("grid");
    focus(cell(0, 0)!);
    const clip = (text: string) => ({ clipboardData: { getData: () => text } });
    fireEvent.paste(grid, clip("6200\tRent\t(3,000.00)\n2010\tBill\tabc\n6200\tThird\t5"));
    const preview = screen.getByRole("group", { name: "Paste preview" });
    expect(within(preview).getByText(/Paste 9 cells from row 1, Account\?/)).toBeTruthy();
    expect(within(preview).getByText(/1 row is added at the end|1 rows are added at the end/)).toBeTruthy();
    expect(within(preview).getByText("Does not fit")).toBeTruthy();
    expect(latest.map((r) => r.memo)).toEqual(["", ""]); // nothing is set before Apply
    key(preview, "Escape");
    expect(screen.queryByRole("group", { name: "Paste preview" })).toBeNull();
    expect(latest.map((r) => r.memo)).toEqual(["", ""]);
    fireEvent.paste(grid, clip("6200\tRent\t(3,000.00)\n2010\tBill\tabc\n6200\tThird\t5"));
    fireEvent.click(screen.getByRole("button", { name: "Apply paste" }));
    expect(latest).toEqual([{ account: "6200", memo: "Rent", debit: "-3000.00" }, { account: "2010", memo: "Bill", debit: "abc" }, { account: "6200", memo: "Third", debit: "5" }]);
    expect(screen.getByText(/Pasted 9 cells/)).toBeTruthy();
    // One cell pasted goes to the cell's editor, where it shows as it will be kept.
    focus(cell(0, 2)!);
    key(cell(0, 0)!, "ArrowRight");
    key(active(), "ArrowRight");
    fireEvent.paste(grid, clip("S$99.50"));
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("99.50");
  });

  it("fills down with Ctrl+D, clears with Delete and undoes and redoes with Ctrl+Z and Ctrl+Y", () => {
    start(3);
    focus(cell(0, 1)!);
    key(cell(0, 1)!, "Enter");
    key(type("Rent"), "Enter");
    key(active(), "ArrowUp");
    key(active(), "ArrowDown", { shiftKey: true });
    key(active(), "ArrowDown", { shiftKey: true });
    key(active(), "d", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["Rent", "Rent", "Rent"]);
    key(active(), "z", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["Rent", "", ""]);
    key(active(), "y", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["Rent", "Rent", "Rent"]);
    key(active(), "Delete"); // the filled block is still selected: all three are cleared
    expect(latest.map((r) => r.memo)).toEqual(["", "", ""]);
    key(active(), "z", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["Rent", "Rent", "Rent"]);
    key(active(), "z", { ctrlKey: true, shiftKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["", "", ""]);
    key(active(), "z", { ctrlKey: true });
    key(active(), "z", { ctrlKey: true });
    key(active(), "z", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["", "", ""]); // back to where it began: the first change was "Rent" in one cell
  });

  it("sets every selected cell to what is typed with Ctrl+Enter", () => {
    start(3);
    focus(cell(0, 1)!);
    key(cell(0, 1)!, "ArrowDown", { shiftKey: true });
    key(active(), "ArrowDown", { shiftKey: true });
    key(active(), "Enter");
    key(type("Same"), "Enter", { ctrlKey: true });
    expect(latest.map((r) => r.memo)).toEqual(["Same", "Same", "Same"]);
  });

  it("copies a block as the table a spreadsheet takes, with plain decimals and an ASCII minus", () => {
    render(<Harness initial={[{ account: "6200", memo: "Rent", debit: "-3000.00" }, { account: "2010", memo: "", debit: "5.50" }]} />);
    focus(cell(0, 0)!);
    key(cell(0, 0)!, "a", { ctrlKey: true });
    let copied = "";
    fireEvent.copy(screen.getByRole("grid"), { clipboardData: { setData: (_: string, t: string) => (copied = t) } });
    expect(copied).toBe("6200 Office rent\tRent\t-3000.00\n2010 Accounts payable\t\t5.50");
  });

  it("adds a row when Tab or Down goes past the last filled one, and lets Tab leave the grid when the last row is blank", () => {
    start(1, { account: "6200" });
    focus(cell(0, 2)!);
    key(cell(0, 0)!, "End");
    const proceed = fireEvent.keyDown(active(), { key: "Tab" });
    expect(proceed).toBe(false); // the grid took the key and made a new row
    expect(latest).toHaveLength(2);
    expect(active().id).toBe(cell(1, 0)!.id);
    key(active(), "End");
    const leave = fireEvent.keyDown(active(), { key: "Tab" });
    expect(leave).toBe(true); // the last row is blank: the key goes on to leave the grid
    expect(latest).toHaveLength(2);
  });

  it("has an Undo, Redo, Fill down, Clear and Add row toolbar, which Basic mode leaves out", () => {
    const { unmount } = start(2);
    const bar = screen.getByRole("toolbar", { name: "Journal lines: tools" });
    expect((within(bar).getByRole("button", { name: /Undo/ }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(bar).getByRole("button", { name: /Add row/ }));
    expect(latest).toHaveLength(3);
    expect((within(bar).getByRole("button", { name: /Undo/ }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(within(bar).getByRole("button", { name: /Undo/ }));
    expect(latest).toHaveLength(2);
    unmount();
    render(<Harness initial={[blank()]} tools={false} />);
    expect(screen.queryByRole("toolbar")).toBeNull();
  });

  it("refuses to change a read-only grid", () => {
    render(<EntryGrid label="Read only" columns={cols} rows={[{ account: "6200", memo: "Rent", debit: "1.00" }]} onChange={() => { throw new Error("changed"); }} blank={blank} readOnly />);
    focus(cell(0, 1)!);
    key(cell(0, 1)!, "Enter");
    key(cell(0, 1)!, "x");
    key(cell(0, 1)!, "Delete");
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});
