import { describe, expect, it } from "vitest";
import { apply, blockOf, cellProblem, clear, copyTable, fillDown, paste, rowStatus, scrollToRow, setCells, valueFor, windowOf, type GridColumn } from "./entryGridModel";

type Row = { account: string; memo: string; debit: string; credit: string };
const cols: GridColumn<Row>[] = [
  { key: "account", title: "Account", kind: "choice", required: true, choices: [{ value: "6200", label: "6200 Office rent" }, { value: "2010", label: "2010 Accounts payable" }] },
  { key: "memo", title: "Memo", kind: "text" },
  { key: "debit", title: "Debit", kind: "amount" },
  { key: "credit", title: "Credit", kind: "amount" },
];
const blank = (): Row => ({ account: "", memo: "", debit: "", credit: "" });
const rows = (n: number): Row[] => Array.from({ length: n }, blank);

// req: FR-UX-07
// req: NFR-PERF-UI-02
describe("the window of rows drawn", () => {
  it("is a screenful and a few rows beyond, whatever the number of rows", () => {
    expect(windowOf(0, 360, 38, 100_000)).toEqual({ first: 0, last: 16 });
    expect(windowOf(38 * 50_000, 360, 38, 100_000)).toEqual({ first: 49_994, last: 50_016 });
    expect(windowOf(38 * 99_990, 360, 38, 100_000)).toEqual({ first: 99_984, last: 100_000 });
    expect(windowOf(0, 360, 38, 3)).toEqual({ first: 0, last: 3 });
    expect(windowOf(0, 360, 38, 0)).toEqual({ first: 0, last: 0 });
    for (const top of [0, 1, 37, 38, 12_345_678, 38 * 99_999, 1e12]) {
      const w = windowOf(top, 360, 38, 100_000);
      expect(w.last - w.first).toBeLessThanOrEqual(24);
      expect(w.first).toBeGreaterThanOrEqual(0);
      expect(w.last).toBeLessThanOrEqual(100_000);
    }
  });

  it("scrolls only as far as a row needs to be in view", () => {
    expect(scrollToRow(5, 0, 360, 38)).toBe(0);
    expect(scrollToRow(20, 0, 360, 38)).toBe(20 * 38 + 38 - 360);
    expect(scrollToRow(2, 380, 360, 38)).toBe(76);
    expect(blockOf({ r: 5, c: 3 }, { r: 2, c: 1 })).toEqual({ r1: 2, r2: 5, c1: 1, c2: 3 });
  });
});

describe("what a cell may hold, in plain words", () => {
  it("says what is wrong with an amount, a choice and a required cell", () => {
    const row = { ...blank(), account: "6200", debit: "12.345" };
    expect(cellProblem(cols[2]!, row, 2)).toBe("Debit has 3 decimals and this amount has at most 2.");
    expect(cellProblem(cols[2]!, { ...row, debit: "12,5" }, 2)).toContain("is not an amount");
    expect(cellProblem(cols[2]!, { ...row, debit: "-12.50" }, 2)).toBeNull();
    expect(cellProblem(cols[0]!, { ...row, account: "" }, 2)).toBe("Enter the account.");
    expect(cellProblem(cols[0]!, { ...row, account: "9999" }, 2)).toBe("Account is not one of the choices.");
    expect(cellProblem(cols[3]!, row, 2)).toBeNull(); // empty and not required
  });

  it("says whether a row is blank, ready or has problems", () => {
    expect(rowStatus(cols, blank(), 2)).toEqual({ state: "empty", problems: [] });
    expect(rowStatus(cols, { account: "6200", memo: "Rent", debit: "3000.00", credit: "" }, 2).state).toBe("ok");
    expect(rowStatus(cols, { account: "", memo: "x", debit: "1.234", credit: "" }, 2)).toEqual({ state: "error", problems: ["Enter the account.", "Debit has 3 decimals and this amount has at most 2."] });
  });
});

describe("changes and their undo", () => {
  it("sets cells, leaves out those already so, and undoes and redoes exactly, with the rows it added", () => {
    const before = rows(2);
    const change = setCells(before, [{ r: 0, key: "memo", value: "Rent" }, { r: 0, key: "debit", value: "" }, { r: 1, key: "credit", value: "5.00" }]);
    expect(change.patches).toEqual([{ r: 0, key: "memo", from: "", to: "Rent" }, { r: 1, key: "credit", from: "", to: "5.00" }]);
    const after = apply(before, change, false, blank);
    expect(after[0]!.memo).toBe("Rent");
    expect(before[0]!.memo).toBe(""); // the rows given are never changed
    expect(apply(after, change, true, blank)).toEqual(before);
    const grew = { patches: [{ r: 2, key: "memo", from: "", to: "New" }], grew: 1 };
    const three = apply(after, grew, false, blank);
    expect(three).toHaveLength(3);
    expect(three[2]!.memo).toBe("New");
    expect(apply(three, grew, true, blank)).toHaveLength(2);
  });

  it("fills down from the top of the selection, or from the row above a single row", () => {
    const base = rows(4).map((r, i) => ({ ...r, memo: i === 0 ? "Rent" : "", debit: i === 0 ? "100.00" : "" }));
    const down = apply(base, fillDown(base, cols, { r1: 0, r2: 3, c1: 1, c2: 2 }), false, blank);
    expect(down.map((r) => r.memo)).toEqual(["Rent", "Rent", "Rent", "Rent"]);
    expect(down.map((r) => r.debit)).toEqual(["100.00", "100.00", "100.00", "100.00"]);
    expect(down.map((r) => r.account)).toEqual(["", "", "", ""]);
    const one = apply(base, fillDown(base, cols, { r1: 1, r2: 1, c1: 1, c2: 1 }), false, blank);
    expect(one.map((r) => r.memo)).toEqual(["Rent", "Rent", "", ""]);
    expect(fillDown(base, cols, { r1: 0, r2: 0, c1: 1, c2: 1 }).patches).toEqual([]); // nothing above the first row
  });

  it("clears a block", () => {
    const base = rows(2).map((r) => ({ ...r, memo: "x", debit: "1.00" }));
    const out = apply(base, clear(base, cols, { r1: 0, r2: 1, c1: 1, c2: 2 }), false, blank);
    expect(out.every((r) => r.memo === "" && r.debit === "")).toBe(true);
  });
});

describe("a paste from a spreadsheet", () => {
  it("reads amounts in every form the person might paste, and choices by value or name", () => {
    expect(valueFor(cols[2]!, "(1,234.56)")).toBe("-1234.56");
    expect(valueFor(cols[2]!, "S$1,234.56")).toBe("1234.56");
    expect(valueFor(cols[2]!, "−12.50")).toBe("-12.50");
    expect(valueFor(cols[0]!, "6200 office rent")).toBe("6200");
    expect(valueFor(cols[0]!, "2010")).toBe("2010");
    expect(valueFor(cols[1]!, "Rent\r")).toBe("Rent");
  });

  it("sets the cells from where it lands, adds rows past the end, and flags what does not fit", () => {
    const base = rows(2);
    const p = paste(base, cols, { r: 1, c: 0 }, [["6200", "Rent", "(3,000.00)", ""], ["2010", "Bill", "abc", "1.234"], ["", "", "", ""]], 2);
    expect(p.cells).toBe(12);
    expect(p.added).toBe(2);
    expect(p.flagged.map((f) => [f.r, f.c, f.value])).toEqual([[2, 2, "abc"], [2, 3, "1.234"]]);
    const out = apply(base, p.change, false, blank);
    expect(out).toHaveLength(4);
    expect(out[1]).toEqual({ account: "6200", memo: "Rent", debit: "-3000.00", credit: "" });
    expect(out[2]!.debit).toBe("abc"); // set as pasted, its row says what is wrong
    expect(rowStatus(cols, out[2]!, 2).state).toBe("error");
    expect(apply(out, p.change, true, blank)).toEqual(base);
  });

  it("leaves out the rows past the limit and the columns past the last", () => {
    const p = paste(rows(1), cols, { r: 0, c: 3 }, [["1.00", "extra"], ["2.00", "extra"], ["3.00", "extra"]], 2, 2);
    expect(p.leftOutRows).toBe(1);
    expect(p.leftOutColumns).toBe(1);
    expect(p.cells).toBe(2);
    expect(p.added).toBe(1);
  });
});

describe("a copy", () => {
  it("carries plain decimals with an ASCII minus, a choice by its name, and no negative zero", () => {
    const base: Row[] = [{ account: "6200", memo: "Rent", debit: "3000.00", credit: "-0.00" }, { account: "2010", memo: "", debit: "-12.50", credit: "" }];
    expect(copyTable(base, cols, { r1: 0, r2: 1, c1: 0, c2: 3 })).toEqual([["6200 Office rent", "Rent", "3000.00", "0.00"], ["2010 Accounts payable", "", "-12.50", ""]]);
  });
});
