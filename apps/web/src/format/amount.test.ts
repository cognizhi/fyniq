import { describe, expect, it } from "vitest";
import { clipboardAmount, formatAmount } from "./amount";

// req: experience/visual-design Numbers
// req: RND-DISPLAY
// req: FR-UX-20
describe("amounts", () => {
  it("shows negatives with a minus sign or in brackets, as the person prefers", () => {
    expect(formatAmount("-1234.56")).toBe("−1,234.56");
    expect(formatAmount("-1234.56", "parentheses")).toBe("(1,234.56)");
    expect(formatAmount("1234.56", "parentheses")).toBe("1,234.56");
  });

  it("groups thousands and keeps the scale the server sent", () => {
    expect(formatAmount("0.00")).toBe("0.00");
    expect(formatAmount("1000000")).toBe("1,000,000");
    expect(formatAmount("100.5000")).toBe("100.5000");
    expect(formatAmount("-0.00")).toBe("0.00");
  });

  it("never re-rounds: every digit of a large amount survives", () => {
    // A JavaScript number would round this to 12345678901234568.
    expect(formatAmount("12345678901234567.8912")).toBe("12,345,678,901,234,567.8912");
    expect(clipboardAmount("12345678901234567.8912")).toBe("12345678901234567.8912");
  });

  it("copies a plain decimal with an ASCII minus", () => {
    expect(clipboardAmount("-1234.56")).toBe("-1234.56");
    expect(clipboardAmount("+0012.30")).toBe("12.30");
  });

  it("refuses what is not a decimal amount", () => {
    for (const bad of ["1,234.56", "1e3", "", "NaN", "(5.00)"]) expect(() => formatAmount(bad)).toThrow();
  });
});
