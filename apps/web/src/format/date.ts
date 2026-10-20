// Dates and times as people read them (NFR-L10N-01, MASTER.md "Numbers": "12 Oct 2026"), in the
// organisation's time zone. Provisional (20 Oct 2026): the zone is Asia/Singapore, every
// organisation's today, until the session carries the organisation's own (FR-ORG-01).
export const timeZone = "Asia/Singapore";

const day = new Intl.DateTimeFormat("en-SG", { day: "numeric", month: "short", year: "numeric", timeZone });
const time = new Intl.DateTimeFormat("en-SG", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone });

function parse(iso: string | undefined | null): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

// date is "20 Oct 2026"; dateTime is "20 Oct 2026, 14:05"; both "" for no date.
export function date(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? day.format(d) : "";
}

export function dateTime(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? `${day.format(d)}, ${time.format(d)}` : "";
}

// clock is "14:05", for a time later today, such as until when a step-up counts.
export function clock(iso: string | undefined | null): string {
  const d = parse(iso);
  return d ? time.format(d) : "";
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
