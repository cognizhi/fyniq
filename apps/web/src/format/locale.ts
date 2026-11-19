// How a person reads numbers and dates (NFR-L10N-01): their own number and date formats, and the organisation's time
// zone, as the session reports them (preferences.ts). English (Singapore) is the language; the defaults are
// "1,234.56" and "12 Oct 2026" in Asia/Singapore. Only characters move: an amount stays the exact decimal string the
// API sent (ENG-12), and what is copied or sent back is always the plain decimal (FR-UX-20).

export type NumberFormat = "comma_dot" | "dot_comma" | "space_comma" | "space_dot";
export type DateFormat = "d_mmm_yyyy" | "dd_mm_yyyy" | "mm_dd_yyyy" | "iso";

export const numberFormats: readonly NumberFormat[] = ["comma_dot", "dot_comma", "space_comma", "space_dot"];
export const dateFormats: readonly DateFormat[] = ["d_mmm_yyyy", "dd_mm_yyyy", "mm_dd_yyyy", "iso"];

const nbsp = "\u00a0"; // a no-break space, so an amount never splits across lines

export const separatorsOf: Readonly<Record<NumberFormat, { group: string; decimal: string }>> = {
  comma_dot: { group: ",", decimal: "." },
  dot_comma: { group: ".", decimal: "," },
  space_comma: { group: nbsp, decimal: "," },
  space_dot: { group: nbsp, decimal: "." },
};

let number: NumberFormat | null = null;
let date: DateFormat = "d_mmm_yyyy";
// The organisation's time zone: every date and time is written in it (FR-ORG-01). A live binding, so a screen that
// imports it reads the current one.
export let timeZone = "Asia/Singapore";
const listeners = new Set<() => void>();

export interface Locale {
  number?: string | null;
  date?: string | null;
  timeZone?: string | null;
}

// setLocale applies the person's formats and the organisation's time zone; a value it does not know is the default.
export function setLocale(l: Locale): void {
  number = numberFormats.find((f) => f === l.number) ?? null;
  date = dateFormats.find((f) => f === l.date) ?? "d_mmm_yyyy";
  timeZone = l.timeZone && validZone(l.timeZone) ? l.timeZone : "Asia/Singapore";
  for (const f of listeners) f();
}

function validZone(z: string): boolean {
  try {
    new Intl.DateTimeFormat("en-SG", { timeZone: z });
    return true;
  } catch {
    return false;
  }
}

// separators are the characters between the thousands and before the decimals, as the person's format writes them.
export function separators(): { group: string; decimal: string } {
  return separatorsOf[number ?? "comma_dot"];
}

// chosenNumberFormat is the person's own choice; null while they have not made one.
export function chosenNumberFormat(): NumberFormat | null {
  return number;
}

export function dateFormat(): DateFormat {
  return date;
}

export function onLocale(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}
