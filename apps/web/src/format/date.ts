// Dates and times as people read them (NFR-L10N-01, MASTER.md "Numbers": "12 Oct 2026" by default), in the
// organisation's time zone and the person's date format (locale.ts, set from the session).
import { dateFormat, type DateFormat, timeZone } from "./locale";

export { timeZone };

// What each date format writes, as an Intl locale and its options; the formatters are built once per time zone.
const dayOptions = {
  d_mmm_yyyy: ["en-SG", { day: "numeric", month: "short", year: "numeric" }],
  dd_mm_yyyy: ["en-GB", { day: "2-digit", month: "2-digit", year: "numeric" }],
  mm_dd_yyyy: ["en-US", { day: "2-digit", month: "2-digit", year: "numeric" }],
  iso: ["en-CA", { day: "2-digit", month: "2-digit", year: "numeric" }],
} as const;
const monthOptions = {
  d_mmm_yyyy: ["en-SG", { month: "short", year: "numeric" }],
  dd_mm_yyyy: ["en-GB", { month: "2-digit", year: "numeric" }],
  mm_dd_yyyy: ["en-US", { month: "2-digit", year: "numeric" }],
  iso: ["en-CA", { month: "2-digit", year: "numeric" }],
} as const;

const made = new Map<string, Intl.DateTimeFormat>();
function formatter(kind: "day" | "month" | "time", zone: string = timeZone, as: DateFormat = dateFormat()): Intl.DateTimeFormat {
  const key = `${kind}|${as}|${zone}`;
  let f = made.get(key);
  if (!f) {
    if (kind === "time") f = new Intl.DateTimeFormat("en-SG", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: zone });
    else {
      const [locale, options] = (kind === "day" ? dayOptions : monthOptions)[as];
      f = new Intl.DateTimeFormat(locale, { ...options, timeZone: zone });
    }
    made.set(key, f);
  }
  return f;
}

function parse(iso: string | undefined | null): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

// A calendar day with no time ("2026-10-20") is the same day in every time zone: it is written as it is, not moved by
// the organisation's zone.
const calendarDay = /^\d{4}-\d{2}-\d{2}$/;

// date is "20 Oct 2026"; dateTime is "20 Oct 2026, 14:05"; both "" for no date.
export function date(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? formatter("day", iso && calendarDay.test(iso) ? "UTC" : timeZone).format(d) : "";
}

// dateAs is a date in a given date format, to show a person what each choice looks like.
export function dateAs(iso: string, as: DateFormat): string {
  const d = parse(iso);
  return d ? formatter("day", timeZone, as).format(d) : "";
}

// month is "Nov 2026" for the month "2026-11"; "" for none.
export function month(ym: string | undefined | null): string {
  const d = parse(ym ? `${ym}-01` : null);
  return d ? formatter("month", "UTC").format(d) : "";
}

export function dateTime(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? `${formatter("day").format(d)}, ${formatter("time").format(d)}` : "";
}

// clock is "14:05", for a time later today, such as until when a step-up counts.
export function clock(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? formatter("time").format(d) : "";
}

// browserOf names a browser from what it said about itself ("Chrome on macOS"); the full text
// stays available as the title.
export function browserOf(ua: string | undefined | null): string {
  if (!ua) return "A browser that did not say";
  const name = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\//.test(ua)
      ? "Firefox"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : /curl|Go-http-client|python/i.test(ua)
            ? "A program"
            : "A browser";
  const os = /iPhone|iPad/.test(ua)
    ? "iOS"
    : /Android/.test(ua)
      ? "Android"
      : /Mac OS X|Macintosh/.test(ua)
        ? "macOS"
        : /Windows/.test(ua)
          ? "Windows"
          : /Linux/.test(ua)
            ? "Linux"
            : "";
  return os ? `${name} on ${os}` : name;
}
