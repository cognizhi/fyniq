import { describe, expect, it } from "vitest";
import { clipboardAmount, clipboardText, formatAmount, installClipboard, pastedAmount } from "./amount";

// req: experience/visual-design Numbers
// req: RND-DISPLAY
// req: FR-UX-20
// req: experience/navigation Copy and export
// req: experience/navigation Paste formats
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

  it("copies what is on screen with each amount as a plain decimal, a table's tabs kept (rev 0.193)", () => {
    expect(clipboardText("Rent\t−1,234.56\t(2,000.00)\t1,090.00 SGD\n2026\t(2026)\t12.5")).toBe("Rent\t-1234.56\t-2000.00\t1090.00 SGD\n2026\t(2026)\t12.5");
    expect(clipboardText("INV-0138 on 14 Oct 2026")).toBe("INV-0138 on 14 Oct 2026");
    expect(clipboardText("12,345,678,901,234,567.8912")).toBe("12345678901234567.8912");
  });

  it("reads a pasted amount in the forms people copy, and the locale's own (rev 0.193)", () => {
    for (const [pasted, want] of [
      ["(1,234.56)", "-1234.56"],
      ["S$1,234.56", "1234.56"],
      ["−1,234.56", "-1234.56"],
      ["-S$1,234.56", "-1234.56"],
      ["S$(1,234.56)", "-1234.56"],
      ["SGD 1,234.56", "1234.56"],
      ["1,234.56 USD", "1234.56"],
      [" 1 234.56 ", "1234.56"],
      ["0012.30", "12.30"],
      ["(0.00)", "0.00"],
      ["1234", "1234"],
    ] as const) {
      expect(pastedAmount(pasted, "en-SG"), pasted).toBe(want);
    }
    expect(pastedAmount("1.234,56", "de-DE")).toBe("1234.56");
    expect(pastedAmount("€1.234,56", "de-DE")).toBe("1234.56");
    for (const bad of ["abc", "1.2.3", "", "1e3", "12-3"]) expect(pastedAmount(bad, "en-SG"), bad).toBeNull();
  });

  it("puts a pasted amount in an amount field as read, and copies text on screen as plain decimals", () => {
    const stop = installClipboard();
    const field = document.createElement("input");
    field.inputMode = "decimal";
    document.body.append(field);
    let seen = "";
    field.addEventListener("input", () => (seen = field.value));
    const paste = new Event("paste", { bubbles: true, cancelable: true }) as ClipboardEvent;
    Object.defineProperty(paste, "clipboardData", { value: { getData: () => "(1,234.56)" } });
    field.dispatchEvent(paste);
    expect(paste.defaultPrevented).toBe(true);
    expect(field.value).toBe("-1234.56");
    expect(seen).toBe("-1234.56");
    const cell = document.createElement("span");
    cell.textContent = "−1,234.56";
    document.body.append(cell);
    field.blur();
    const range = document.createRange();
    range.selectNodeContents(cell);
    document.getSelection()?.removeAllRanges();
    document.getSelection()?.addRange(range);
    let copied = "";
    const copy = new Event("copy", { bubbles: true, cancelable: true }) as ClipboardEvent;
    Object.defineProperty(copy, "clipboardData", { value: { setData: (_: string, v: string) => (copied = v) } });
    document.dispatchEvent(copy);
    expect(copied).toBe("-1234.56");
    stop();
    field.remove();
    cell.remove();
  });
});
