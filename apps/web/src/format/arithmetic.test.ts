import { describe, expect, it } from "vitest";
import { evaluate } from "./arithmetic";

const ok = (s: string) => {
  const r = evaluate(s);
  if (!("value" in r)) throw new Error(`${s}: ${r.error}`);
  return r.value;
};

// req: FR-UX-07
// req: ENG-10
describe("= arithmetic in an amount cell", () => {
  it("adds, subtracts and multiplies exactly, keeping the scale the figures have", () => {
    expect(ok("=1250.50*3-80")).toBe("3671.50");
    expect(ok("=0.1+0.2")).toBe("0.3"); // a float would say 0.30000000000000004
    expect(ok("=2.50+2.50")).toBe("5.00");
    expect(ok("=1.25*1.25")).toBe("1.5625");
    expect(ok("=100-100.00")).toBe("0.00");
    expect(ok("= 12 + 3 * 4")).toBe("24");
    expect(ok("=(12+3)*4")).toBe("60");
    expect(ok("=-5+2")).toBe("-3");
    expect(ok("=5*-2")).toBe("-10");
    expect(ok("=−5+2")).toBe("-3"); // the typographic minus
    expect(ok("=3×4")).toBe("12");
    expect(ok("=.5+.25")).toBe("0.75");
  });

  it("keeps digits beyond 15 significant ones", () => {
    expect(ok("=123456789012345678.99+0.01")).toBe("123456789012345679.00");
    expect(ok("=9007199254740993*3")).toBe("27021597764222979");
  });

  it("divides only when the quotient is exact", () => {
    expect(ok("=10/4")).toBe("2.5");
    expect(ok("=100/8")).toBe("12.5");
    expect(ok("=1/8")).toBe("0.125");
    expect(ok("=7.50/2.5")).toBe("3.00");
    expect(ok("=9/3")).toBe("3");
    expect(ok("=3000.00/12")).toBe("250.00");
    expect(ok("=0/5")).toBe("0");
    const third = evaluate("=10/3");
    expect("error" in third && third.error).toContain("does not come out exactly");
    const zero = evaluate("=5/0");
    expect("error" in zero && zero.error).toContain("divide by zero");
  });

  it("says what is wrong in plain words", () => {
    for (const [s, part] of [["=", "Type a sum"], ["=12+", "ends before"], ["=(1+2", "bracket"], ["=1,5+2", "not part of an amount sum"], ["=abc", "not a number"], ["=2 3", "not part of an amount sum"]] as const) {
      const r = evaluate(s);
      expect("error" in r && r.error).toContain(part);
    }
    const long = evaluate(`=${"1+".repeat(150)}1`);
    expect("error" in long && long.error).toContain("too long");
  });
});
