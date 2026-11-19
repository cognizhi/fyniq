import { describe, expect, it } from "vitest";
import { parseTable, tableText } from "./table";

// req: FR-UX-07
// req: FR-UX-20
describe("the table a spreadsheet copies", () => {
  it("reads tabs between cells and line breaks between rows, with either line ending and a trailing one", () => {
    expect(parseTable("a\tb\tc\n1\t2\t3\n")).toEqual([["a", "b", "c"], ["1", "2", "3"]]);
    expect(parseTable("a\tb\r\n1\t2\r\n")).toEqual([["a", "b"], ["1", "2"]]);
    expect(parseTable("12.50")).toEqual([["12.50"]]);
    expect(parseTable("1,234.56")).toEqual([["1,234.56"]]); // one amount, not two cells
    expect(parseTable("  \n ")).toEqual([]);
    expect(parseTable("")).toEqual([]);
  });

  it("keeps empty cells and rows, which are blanks the paste leaves as they are", () => {
    expect(parseTable("a\t\tc\n\t\t\nx\ty\tz")).toEqual([["a", "", "c"], ["", "", ""], ["x", "y", "z"]]);
  });

  it("reads a quoted cell as one, with its tabs, line breaks and doubled quotes", () => {
    expect(parseTable('"Rent, Oct"\t"say ""hi"""\t"two\nlines"\n1\t2\t3')).toEqual([["Rent, Oct", 'say "hi"', "two\nlines"], ["1", "2", "3"]]);
    expect(parseTable('"a\tb"\tc')).toEqual([["a\tb", "c"]]);
  });

  it("reads comma-separated lines when there is no tab", () => {
    expect(parseTable("Rent,3000.00\nSalaries,12000.00")).toEqual([["Rent", "3000.00"], ["Salaries", "12000.00"]]);
    expect(parseTable('"Rent, Oct",3000.00\nTax,10')).toEqual([["Rent, Oct", "3000.00"], ["Tax", "10"]]);
  });

  it("writes cells as a spreadsheet would, quoting what needs it, and reads them back", () => {
    const rows = [["Rent", "-3000.00"], ['say "hi"', "two\nlines"], ["a\tb", ""]];
    expect(tableText(rows)).toBe('Rent\t-3000.00\n"say ""hi"""\t"two\nlines"\n"a\tb"\t');
    expect(parseTable(tableText(rows))).toEqual(rows);
  });

  it("refuses a paste of millions of cells, which is not a paste", () => {
    expect(() => parseTable("x\t".repeat(2_100_000))).toThrow(RangeError);
  });
});
