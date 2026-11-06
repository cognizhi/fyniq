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

// clipboardText is what a copy of text on screen carries (FR-UX-20, rev 0.193): each amount as shown, "−1,234.56",
// "(1,234.56)" or "1,234.56", becomes a plain decimal with an ASCII minus; everything else, such as a table's tabs and
// lines or a currency code beside an amount, stays as it is. Brackets count as a negative only around a decimal amount,
// so "(2026)" is left alone.
export function clipboardText(text: string): string {
  const plain = (whole: string, fraction = "") => `${whole.replaceAll(",", "")}${fraction}`;
  return text
    .replace(/\((\d{1,3}(?:,\d{3})+|\d+)(\.\d+)\)/g, (_, w: string, f: string) => `-${plain(w, f)}`)
    .replace(new RegExp(`${minus}(\\d{1,3}(?:,\\d{3})+|\\d+)(\\.\\d+)?`, "g"), (_, w: string, f?: string) => `-${plain(w, f)}`)
    .replace(/(?<![\d,.])(\d{1,3}(?:,\d{3})+)(\.\d+)?(?![\d,])/g, (_, w: string, f?: string) => plain(w, f));
}

// decimalMark is the locale's decimal separator, read from how it writes a sample; the sample is the only number made.
function decimalMark(locale: string): string {
  return new Intl.NumberFormat(locale).formatToParts(1.5).find((p) => p.type === "decimal")?.value ?? ".";
}

// pastedAmount reads an amount pasted into an amount field (FR-UX-20, rev 0.193): "(1,234.56)", "S$1,234.56",
// "−1,234.56", "-1234.56", "SGD 1,234.56" or the locale's own form, such as "1.234,56" where a comma marks the decimals;
// the plain decimal with an ASCII minus, or null when it is not an amount. It moves characters only: no number is made.
export function pastedAmount(text: string, locale: string = typeof navigator === "undefined" ? "en-SG" : navigator.language): string | null {
  let s = text.trim().replace(/[\s\u00a0\u202f]/g, "");
  let negative = false;
  const currency = /^[A-Za-z]{0,3}[$€£¥]|^[A-Za-z]{3}(?=[\d(−-])|[A-Za-z]{3}$/g; // S$, US$, €, SGD before or after
  const sign = () => {
    if (s.startsWith(minus) || s.startsWith("-")) {
      negative = !negative;
      s = s.slice(1);
    }
  };
  s = s.replace(currency, "");
  if (/^\(.*\)$/.test(s)) {
    negative = true;
    s = s.slice(1, -1).replace(currency, "");
  }
  sign();
  s = s.replace(currency, "");
  sign();
  s = decimalMark(locale) === "," ? s.replaceAll(".", "").replace(",", ".") : s.replaceAll(",", "");
  if (!/^\d+(\.\d+)?$/.test(s)) return null;
  const digits = s.replace(/^0+(?=\d)/, "");
  return `${negative && !/^[0.]*$/.test(digits) ? "-" : ""}${digits}`;
}

// installClipboard makes copying and pasting amounts faithful across the app (FR-UX-20): a copy of text on screen
// carries plain decimals, and a paste into an amount field (inputmode decimal) puts the amount read in the field, where it
// shows before the form is saved. A field's own copy, and what is not an amount, are left to the browser.
export function installClipboard(doc: Document = document): () => void {
  const onCopy = (e: ClipboardEvent) => {
    const active = doc.activeElement;
    if (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement || !e.clipboardData) return;
    const selection = doc.getSelection();
    if (!selection || selection.isCollapsed) return;
    const text = selection.toString();
    const plain = clipboardText(text);
    if (plain === text) return;
    e.clipboardData.setData("text/plain", plain);
    e.preventDefault();
  };
  const onPaste = (e: ClipboardEvent) => {
    const field = e.target;
    if (!(field instanceof HTMLInputElement) || field.inputMode !== "decimal" || !e.clipboardData) return;
    const text = e.clipboardData.getData("text/plain");
    const amount = pastedAmount(text);
    if (amount === null || amount === text.trim()) return;
    e.preventDefault();
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(field, amount); // as typed, so React sees it
    field.dispatchEvent(new Event("input", { bubbles: true }));
  };
  doc.addEventListener("copy", onCopy);
  doc.addEventListener("paste", onPaste);
  return () => {
    doc.removeEventListener("copy", onCopy);
    doc.removeEventListener("paste", onPaste);
  };
}
