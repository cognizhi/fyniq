// "=" arithmetic in an amount cell (PRD-001 FR-UX-07): "=1250.50*3-80" gives 3671.50. Amounts stay exact: every operand is a decimal
// string held as a scaled integer (BigInt), so nothing is a JavaScript number and nothing is rounded (golden rule 1, ENG-10).
// + − × and brackets are exact whatever their scale. A division is made only when it comes out exactly within twelve decimals;
// otherwise it is refused and the person types the figure they mean, since a rounded quotient needs a named rounding policy
// (RND-*) that a cell has no way to name.

export type Evaluated = { value: string } | { error: string };

interface Dec {
  n: bigint;
  scale: number;
}

const MAX_DIVISION_DIGITS = 12;
const MAX_LENGTH = 200;

function pow10(n: number): bigint {
  return 10n ** BigInt(n);
}

function align(a: Dec, b: Dec): [bigint, bigint, number] {
  const scale = Math.max(a.scale, b.scale);
  return [a.n * pow10(scale - a.scale), b.n * pow10(scale - b.scale), scale];
}

function text(d: Dec): string {
  const negative = d.n < 0n;
  const digits = (negative ? -d.n : d.n).toString().padStart(d.scale + 1, "0");
  const whole = digits.slice(0, digits.length - d.scale);
  const out = d.scale === 0 ? whole : `${whole}.${digits.slice(digits.length - d.scale)}`;
  return negative && d.n !== 0n ? `-${out}` : out;
}

class Fail extends Error {}

// A tiny recursive-descent parser: expr = term (("+"|"-") term)*; term = factor (("*"|"/") factor)*; factor = "-" factor | "(" expr ")" | number.
class Parser {
  private at = 0;
  constructor(private readonly s: string) {}

  parse(): Dec {
    const v = this.expr();
    this.space();
    if (this.at < this.s.length) throw new Fail(`The "${this.s[this.at]}" is not part of an amount sum. Use digits, a decimal point, + − × ÷ and brackets.`);
    return v;
  }

  private space() {
    while (this.at < this.s.length && /\s/.test(this.s[this.at]!)) this.at++;
  }

  private peek(): string {
    this.space();
    return this.s[this.at] ?? "";
  }

  private expr(): Dec {
    let v = this.term();
    for (;;) {
      const op = this.peek();
      if (op !== "+" && op !== "-" && op !== "−") return v;
      this.at++;
      const r = this.term();
      const [a, b, scale] = align(v, r);
      v = { n: op === "+" ? a + b : a - b, scale };
    }
  }

  private term(): Dec {
    let v = this.factor();
    for (;;) {
      const op = this.peek();
      if (op !== "*" && op !== "/" && op !== "×" && op !== "÷") return v;
      this.at++;
      const r = this.factor();
      if (op === "*" || op === "×") {
        v = { n: v.n * r.n, scale: v.scale + r.scale };
        continue;
      }
      if (r.n === 0n) throw new Fail("A sum cannot divide by zero.");
      // v / r = (v.n * 10^r.scale) / (r.n * 10^v.scale): scale the numerator by ten until it divides by r.n with no remainder,
      // at most twelve times; the quotient then has v's decimals and one more for each tenfold.
      let n = v.n * pow10(r.scale);
      let extra = 0;
      while (n % r.n !== 0n && extra < MAX_DIVISION_DIGITS) {
        n *= 10n;
        extra++;
      }
      if (n % r.n !== 0n) throw new Fail("That division does not come out exactly. Type the amount you mean, rounded the way you need.");
      v = { n: n / r.n, scale: v.scale + extra };
    }
  }

  private factor(): Dec {
    const c = this.peek();
    if (c === "-" || c === "−") {
      this.at++;
      const v = this.factor();
      return { n: -v.n, scale: v.scale };
    }
    if (c === "+") {
      this.at++;
      return this.factor();
    }
    if (c === "(") {
      this.at++;
      const v = this.expr();
      if (this.peek() !== ")") throw new Fail("A bracket is not closed.");
      this.at++;
      return v;
    }
    const m = /^(\d+)(?:\.(\d+))?|^\.(\d+)/.exec(this.s.slice(this.at));
    if (!m) throw new Fail(c === "" ? "The sum ends before it is finished." : `The "${c}" is not a number.`);
    this.at += m[0].length;
    const whole = m[1] ?? "0";
    const fraction = m[2] ?? m[3] ?? "";
    return { n: BigInt(whole + fraction), scale: fraction.length };
  }
}

/**
 * evaluate works out an amount sum such as "=1250.50*3-80" (the leading "=" is optional here), exactly, as a plain decimal with an
 * ASCII minus; or says in plain words why it cannot.
 */
export function evaluate(input: string): Evaluated {
  const s = input.trim().replace(/^=/, "");
  if (s === "") return { error: "Type a sum after the =." };
  if (s.length > MAX_LENGTH) return { error: "That sum is too long." };
  try {
    return { value: text(new Parser(s).parse()) };
  } catch (e) {
    if (e instanceof Fail) return { error: e.message };
    throw e;
  }
}
