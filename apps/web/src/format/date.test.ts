import { describe, expect, it } from "vitest";
import { browserOf, clock, date, dateTime } from "./date";

// req: NFR-L10N-01
describe("dates and browsers as people read them", () => {
  it("writes dates in the organisation's time zone", () => {
    expect(date("2026-10-20T17:30:00Z")).toBe("21 Oct 2026");
    expect(dateTime("2026-10-20T06:05:00Z")).toBe("20 Oct 2026, 14:05");
    expect(clock("2026-10-20T06:05:00Z")).toBe("14:05");
    expect(date(undefined)).toBe("");
    expect(dateTime("not a date")).toBe("");
  });

  it("names a browser from its user agent", () => {
    expect(browserOf("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15")).toBe("Safari on macOS");
    expect(browserOf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/141.0 Safari/537.36 Edg/141.0")).toBe("Edge on Windows");
    expect(browserOf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/141.0 Safari/537.36")).toBe("Chrome on Linux");
    expect(browserOf("Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Version/18.0 Mobile Safari/604.1")).toBe("Safari on iOS");
    expect(browserOf("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")).toBe("Firefox on Linux");
    expect(browserOf(undefined)).toBe("A browser that did not say");
  });
});
