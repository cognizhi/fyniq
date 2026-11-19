import { describe, expect, it } from "vitest";
import { date, money, plainDecimal } from "./format";

const MINUS = String.fromCharCode(0x2212);

// req: NM-NFR-04
describe("en-SG display", () => {
  it("draws money as Singapore dollars from a decimal string, grouped, with an ASCII minus, without a float", () => {
    expect(money("4500.00")).toBe("S$4,500.00");
    expect(money("1234567.5")).toBe("S$1,234,567.5");
    expect(money("0.07")).toBe("S$0.07");
    expect(money("-204.55")).toBe("-S$204.55");
    expect(money(`${MINUS}204.55`)).toBe("-S$204.55");
    expect(money("12345678901234567890.12")).toBe("S$12,345,678,901,234,567,890.12");
    expect(money("n/a")).toBe("n/a");
  });
  it("gives the clipboard the plain decimal with an ASCII minus", () => {
    expect(plainDecimal("S$4,500.00")).toBe("4500.00");
    expect(plainDecimal("-S$204.55")).toBe("-204.55");
    expect(plainDecimal(`${MINUS}204.55`)).toBe("-204.55");
    expect(plainDecimal("1,234.5")).toBe("1234.5");
    expect(plainDecimal("text")).toBe("text");
  });
  it("draws a day as 25 Jun 2026", () => {
    expect(date("2026-06-25")).toBe("25 Jun 2026");
    expect(date("2027-02-08")).toBe("8 Feb 2027");
    expect(date("soon")).toBe("soon");
  });
});
