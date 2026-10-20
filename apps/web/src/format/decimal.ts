// Exact decimal arithmetic for what a person types before the server answers, such as a journal's
// balance while it is entered (ENG-10, FR-UX-20; MASTER.md "Numbers"). Amounts stay strings; sums use
// BigInt on scaled integers, never a JavaScript number, so nothing is rounded. The server's answer
// is still the figure of record (FR-UX-22): this only says whether what was typed balances.

const plain = /^(-)?(\d+)(?:\.(\d+))?$/;

// typed reads an amount as a person types or pastes it: "1,234.56", "(1,234.56)", "S$1,234.56",
// "SGD 1,234.56", "−1,234.56" or "-1234.56". It answers a plain decimal ("-1234.56"), "" for an empty
// field, or null when it is not an amount.
export function typed(input: string): string | null {
  let s = input.trim().replace(/[\s\u00a0]/g, "");
  if (s === "") return "";
  let negative = false;
  if (s.startsWith("(") && s.endsWith(")")) {
    negative = true;
    s = s.slice(1, -1);
  }
  // One sign at most, before or after a currency: "-S$12", "S$-12".
  const m = /^([−-])?(?:[A-Z]{3}|[A-Z]{0,2}\$)?([−-])?(.*)$/.exec(s);
  if (!m || (m[1] && m[2]) || ((m[1] || m[2]) && negative)) return null;
  negative = negative || Boolean(m[1] || m[2]);
  s = m[3] ?? "";
  if (!/^(?:\d{1,3}(?:,\d{3})+|\d+)?(?:\.\d+)?$/.test(s) || s === "" || s === ".") return null;
  s = s.replace(/,/g, "");
  if (s.startsWith(".")) s = `0${s}`;
  const p = plain.exec(s);
  if (!p) return null;
  const whole = (p[2] ?? "0").replace(/^0+(?=\d)/, "");
  const out = p[3] === undefined ? whole : `${whole}.${p[3]}`;
  return negative && !isZero(out) ? `-${out}` : out;
}

function scaled(value: string): { n: bigint; scale: number } {
  const m = plain.exec(value);
  if (!m) throw new Error(`not a decimal: ${JSON.stringify(value)}`);
  const fraction = m[3] ?? "";
  const n = BigInt((m[2] ?? "0") + fraction);
  return { n: m[1] ? -n : n, scale: fraction.length };
}

function text(n: bigint, scale: number): string {
  const negative = n < 0n;
  const digits = (negative ? -n : n).toString().padStart(scale + 1, "0");
  const whole = digits.slice(0, digits.length - scale);
  const out = scale === 0 ? whole : `${whole}.${digits.slice(digits.length - scale)}`;
  return negative ? `-${out}` : out;
}

// sum adds plain decimals exactly, at the largest scale among them.
export function sum(values: readonly string[]): string {
  const parts = values.filter((v) => v !== "").map(scaled);
  const scale = Math.max(0, ...parts.map((p) => p.scale));
  const total = parts.reduce((t, p) => t + p.n * 10n ** BigInt(scale - p.scale), 0n);
  return text(total, scale);
}

// minus is a − b, exactly.
export function minus(a: string, b: string): string {
  return sum([a, negate(b)]);
}

export function negate(value: string): string {
  if (isZero(value)) return value.replace(/^-/, "");
  return value.startsWith("-") ? value.slice(1) : `-${value}`;
}

export function isZero(value: string): boolean {
  return /^-?0*(\.0*)?$/.test(value);
}

// sign is −1, 0 or 1.
export function sign(value: string): -1 | 0 | 1 {
  if (isZero(value)) return 0;
  return value.startsWith("-") ? -1 : 1;
}
