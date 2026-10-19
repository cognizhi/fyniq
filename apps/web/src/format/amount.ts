// Amounts on screen and on the clipboard (PRD-001 §7.4 "Numbers", FR-UX-20, RND-DISPLAY; MASTER.md
// §4). Money arrives from the API as a decimal string with the currency's scale (ENG-12). These
// functions only move characters: they never parse the amount into a JavaScript number, so nothing
// is re-rounded and no figure loses digits beyond 15 significant ones.

export type Negatives = "minus" | "parentheses";

const decimal = /^([+-])?(\d+)(?:\.(\d+))?$/;
const minus = "−"; // "−", the typographic minus sign

interface Parts {
  negative: boolean;
  whole: string;
  fraction?: string;
}

function parts(value: string): Parts {
  const m = decimal.exec(value.trim());
  if (!m) throw new Error(`not a decimal amount: ${JSON.stringify(value)}`);
  const whole = (m[2] ?? "0").replace(/^0+(?=\d)/, "");
  const fraction = m[3];
  const zero = /^0*$/.test(whole + (fraction ?? ""));
  return { negative: m[1] === "-" && !zero, whole, fraction };
}

// formatAmount shows a decimal string with thousands separators and the person's choice for
// negatives: "−1,234.56" or "(1,234.56)". The sign or the brackets always show, so colour is never
// the only cue.
export function formatAmount(value: string, negatives: Negatives = "minus"): string {
  const p = parts(value);
  const grouped = p.whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const shown = p.fraction === undefined ? grouped : `${grouped}.${p.fraction}`;
  if (!p.negative) return shown;
  return negatives === "parentheses" ? `(${shown})` : `${minus}${shown}`;
}

// clipboardAmount is what a copy or an export carries: a plain decimal with an ASCII "-", no
// separators and no brackets (FR-UX-20).
export function clipboardAmount(value: string): string {
  const p = parts(value);
  return `${p.negative ? "-" : ""}${p.whole}${p.fraction === undefined ? "" : `.${p.fraction}`}`;
}
