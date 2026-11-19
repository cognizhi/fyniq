// en-SG display of money and dates (NM-NFR-04). Money is a decimal string everywhere (golden rule 1): it is grouped and prefixed as text and never goes through a float. The clipboard gets the plain
// decimal with an ASCII minus, whatever is drawn.

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const MINUS = String.fromCharCode(0x2212);

// money draws a decimal string as Singapore dollars: S$4,500.00, with an ASCII minus for a negative amount. A value that is not a decimal string is returned as it came.
export function money(value: string): string {
  const v = value.trim().replace(MINUS, "-");
  const m = /^(-)?(\d+)(?:\.(\d+))?$/.exec(v);
  if (!m) return value;
  const whole = (m[2] ?? "0").replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const cents = m[3] === undefined ? "" : `.${m[3]}`;
  return `${m[1] ? "-" : ""}S$${whole}${cents}`;
}

// plainDecimal is what the clipboard gets for an amount: digits, a point and an ASCII minus, with no currency sign and no thousands separator.
export function plainDecimal(value: string): string {
  const v = value.replace(MINUS, "-").trim();
  const m = /^(-)?\s*(?:S\$)?\s*(-)?([\d,]+(?:\.\d+)?)$/.exec(v);
  if (!m) return value;
  return `${m[1] || m[2] ? "-" : ""}${(m[3] ?? "").replace(/,/g, "")}`;
}

// date draws an ISO day as 25 Jun 2026; anything else is returned as it came.
export function date(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
  if (!m) return iso;
  return `${Number(m[3])} ${MONTHS[Number(m[2]) - 1] ?? m[2]} ${m[1]}`;
}
