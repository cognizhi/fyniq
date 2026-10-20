import { describe, expect, it } from "vitest";
import { isZero, minus, negate, sign, sum, typed } from "./decimal";

// req: FR-UX-20
describe("amounts as typed", () => {
  it("reads what people type and paste", () => {
    expect(typed("1,234.56")).toBe("1234.56");
    expect(typed("(1,234.56)")).toBe("-1234.56");
    expect(typed("S$1,234.56")).toBe("1234.56");
    expect(typed("SGD 1,234.56")).toBe("1234.56");
    expect(typed("−1,234.56")).toBe("-1234.56");
    expect(typed("-S$12")).toBe("-12");
    expect(typed(" 0012.50 ")).toBe("12.50");
    expect(typed(".5")).toBe("0.5");
    expect(typed("")).toBe("");
    expect(typed("-0.00")).toBe("0.00");
  });

  it("refuses what is not an amount", () => {
    for (const bad of ["abc", "1,23.4", "1.2.3", "12a", ".", "1e5", "--1", "(1", "(-1)", "S$-1-"]) expect(typed(bad)).toBeNull();
  });
});

// req: ENG-10
describe("exact sums", () => {
  it("adds without rounding, at the largest scale", () => {
    expect(sum(["0.1", "0.2"])).toBe("0.3");
    expect(sum(["1234.56", "-1234.56"])).toBe("0.00");
    expect(sum(["99999999999999999.99", "0.01"])).toBe("100000000000000000.00");
    expect(sum(["10", "0.005"])).toBe("10.005");
    expect(sum([])).toBe("0");
    expect(minus("2582.25", "2065.80")).toBe("516.45");
    expect(minus("1", "1.50")).toBe("-0.50");
  });

  it("tells the sign", () => {
    expect(sign("-0.01")).toBe(-1);
    expect(sign("0.00")).toBe(0);
    expect(sign("3")).toBe(1);
    expect(isZero("-0.0")).toBe(true);
    expect(negate("0.00")).toBe("0.00");
    expect(negate("-5")).toBe("5");
  });
});
