import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { applyAppearance, defaults as appearanceDefaults, theAppearance } from "./appearance";
import { formatAmount, clipboardText, pastedAmount } from "./format/amount";
import { date, dateTime, month, timeZone } from "./format/date";
import { setLocale } from "./format/locale";
import { forgetMode, setMode, useMode } from "./mode";
import { adopt } from "./preferences";
import { setPreferenceSender } from "./preferenceSink";
import { setAppearance } from "./appearance";
import { applyShortcutSettings, defaultSettings, setShortcutSettings, shortcutSettings } from "./shortcuts";
import { applyLedgerView, setLedgerView, theChosenView } from "./view";
import { answer, problem, ownerSession } from "./test/api";

const prefs = (over: Record<string, unknown> = {}) => ({ default_mode: "basic", etag: '"v1"', ...over });
const session = (preferences?: Record<string, unknown>, time_zone = "Asia/Singapore") =>
  ({ ...ownerSession, preferences, time_zone }) as unknown as Parameters<typeof adopt>[0];
const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 40)));
const NBSP = "\u00a0";

// Each test starts as a browser nobody has chosen anything in, and ends once every save it started has been answered.
beforeEach(() => {
  window.localStorage.clear();
  forgetMode();
  applyLedgerView(null);
  applyAppearance(appearanceDefaults);
  applyShortcutSettings(ownerSession.user_id, defaultSettings);
  setLocale({});
});
afterEach(async () => {
  await settle();
  setPreferenceSender(null);
});

describe("preferences kept on the server", () => {
  // req: FR-UX-23
  it("applies what the session says, so the same choices show on every device", () => {
    answer({});
    adopt(session(prefs({ mode: "expert", ledger_view: "accountant", theme: "dark", density: "compact", shortcuts_on: false, shortcut_keys: { "nav.home": "hh" } })));
    const { result } = renderHook(() => useMode(["owner"]));
    expect(result.current).toBe("expert");
    expect(theChosenView()).toBe("accountant");
    expect(theAppearance().theme).toBe("dark");
    expect(theAppearance().density).toBe("compact");
    expect(shortcutSettings(ownerSession.user_id)).toEqual({ on: false, keys: { "nav.home": "hh" } });
  });

  // req: FR-UX-23
  it("sends a choice to the server with the ETag the session gave, and the next with the one it returned", async () => {
    const seen = answer({
      "PATCH /v1/preferences": (_, req) => ({ status: 200, body: { default_mode: "basic", etag: req.headers.get("If-Match") === '"v1"' ? '"v2"' : '"v3"' }, headers: { ETag: req.headers.get("If-Match") === '"v1"' ? '"v2"' : '"v3"' } }),
    });
    adopt(session(prefs()));
    act(() => setMode("expert"));
    act(() => setLedgerView("accountant"));
    await settle();
    const saves = seen.filter((s) => s.method === "PATCH");
    expect(saves.map((s) => s.body)).toEqual([{ mode: "expert" }, { ledger_view: "accountant" }]);
    expect(saves.map((s) => s.headers.get("If-Match"))).toEqual(['"v1"', '"v2"']);
    expect(saves.every((s) => s.headers.get("Idempotency-Key"))).toBe(true);
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert"); // the browser's copy draws the first paint
  });

  // req: FR-UX-23
  it("reads the preferences again and sends a stale choice once more", async () => {
    let first = true;
    const seen = answer({
      "PATCH /v1/preferences": () => {
        if (first) {
          first = false;
          return problem(412, "API-IF-MATCH-02");
        }
        return { status: 200, body: { default_mode: "basic", etag: '"v4"' }, headers: { ETag: '"v4"' } };
      },
      "GET /v1/preferences": { status: 200, body: { default_mode: "basic", etag: '"v3"' }, headers: { ETag: '"v3"' } },
    });
    adopt(session(prefs()));
    act(() => setAppearance({ theme: "dark" }));
    await settle();
    expect(seen.map((s) => `${s.method} ${s.headers.get("If-Match") ?? ""}`)).toEqual(['PATCH "v1"', "GET ", 'PATCH "v3"']);
    expect(seen.filter((s) => s.method === "PATCH").every((s) => JSON.stringify(s.body) === '{"theme":"dark"}')).toBe(true);
  });

  // req: FR-UX-23
  it("keeps a choice the person made in this browser before the server kept any, by sending it once", async () => {
    window.localStorage.setItem("fyniq.mode", "expert");
    applyLedgerView("accountant");
    const seen = answer({ "PATCH /v1/preferences": { status: 200, body: { default_mode: "basic", etag: '"v2"' }, headers: { ETag: '"v2"' } } });
    adopt(session(prefs()));
    await settle();
    expect(seen.filter((s) => s.method === "PATCH").map((s) => s.body)).toEqual([{ mode: "expert", ledger_view: "accountant" }]);
    // A server's own choice wins over the browser's.
    applyLedgerView(null);
    window.localStorage.setItem("fyniq.mode", "basic");
    forgetMode();
    window.localStorage.setItem("fyniq.mode", "basic");
    const again = answer({});
    adopt(session(prefs({ mode: "expert" })));
    await settle();
    expect(again).toEqual([]);
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
  });

  // req: FR-UX-23
  it("sends nothing before sign-in, and nothing when the session carries no preferences", async () => {
    const seen = answer({});
    act(() => setMode("expert"));
    act(() => setShortcutSettings("someone", { on: false, keys: {} }));
    adopt(session(undefined));
    act(() => setMode("basic"));
    await settle();
    expect(seen).toEqual([]);
  });

  // req: FR-UX-27
  it("starts a person in the organisation's mode for their roles until they choose", () => {
    answer({});
    adopt(session(prefs({ default_mode: "expert" })));
    const { result } = renderHook(() => useMode(["bookkeeper"]));
    expect(result.current).toBe("expert"); // the Administrator set it, whatever the web app's own table says
    act(() => setMode("basic"));
    expect(result.current).toBe("basic"); // their own choice wins
  });

  // req: NFR-L10N-01
  it("writes numbers and dates in the person's formats and the organisation's time zone", () => {
    answer({});
    expect(formatAmount("-1234567.5")).toBe("−1,234,567.5");
    adopt(session(prefs({ number_format: "dot_comma", date_format: "dd_mm_yyyy" }), "America/New_York"));
    expect(formatAmount("-1234567.50")).toBe("−1.234.567,50");
    expect(formatAmount("1234.56", "parentheses")).toBe("1.234,56");
    adopt(session(prefs({ number_format: "dot_comma", date_format: "dd_mm_yyyy", negative_format: "parentheses" }), "America/New_York"));
    expect(formatAmount("-1234567.50")).toBe("(1.234.567,50)"); // the person's way of writing a negative
    expect(formatAmount("-1234567.50", "minus")).toBe("−1.234.567,50"); // unless the screen names one
    adopt(session(prefs({ number_format: "dot_comma", date_format: "dd_mm_yyyy" }), "America/New_York"));
    expect(formatAmount("-1234.56", "parentheses")).toBe("(1.234,56)");
    expect(timeZone).toBe("America/New_York");
    expect(date("2026-10-21T02:30:00Z")).toBe("20/10/2026"); // evening of the 20th in New York
    expect(dateTime("2026-10-21T02:30:00Z")).toBe("20/10/2026, 22:30");
    expect(date("2026-10-21")).toBe("21/10/2026"); // a calendar day is the same day in every zone
    expect(month("2026-11")).toBe("11/2026");
    adopt(session(prefs({ number_format: "space_comma", date_format: "iso" })));
    expect(formatAmount("1234567.5")).toBe(`1${NBSP}234${NBSP}567,5`);
    expect(date("2026-10-21")).toBe("2026-10-21");
    adopt(session(prefs({ date_format: "mm_dd_yyyy" })));
    expect(date("2026-10-21")).toBe("10/21/2026");
    adopt(session(prefs()));
    expect(formatAmount("1234.5")).toBe("1,234.5");
    expect(date("2026-10-21")).toBe("21 Oct 2026");
  });

  // req: FR-UX-20
  // req: NFR-L10N-01
  it("copies and pastes the plain decimal whatever format the amounts show in", () => {
    answer({});
    for (const [number, shown] of [
      ["comma_dot", "1,234.56"],
      ["dot_comma", "1.234,56"],
      ["space_comma", `1${NBSP}234,56`],
      ["space_dot", `1${NBSP}234.56`],
    ] as const) {
      adopt(session(prefs({ number_format: number })));
      expect(formatAmount("1234.56")).toBe(shown);
      expect(clipboardText(`Total ${shown} SGD`)).toBe("Total 1234.56 SGD");
      expect(clipboardText(`Owed −${shown}, paid (${shown})`)).toBe("Owed -1234.56, paid -1234.56");
      expect(clipboardText("Invoice 2026, 12 items")).toBe("Invoice 2026, 12 items");
      expect(pastedAmount(shown)).toBe("1234.56");
      expect(pastedAmount(`(${shown})`)).toBe("-1234.56");
    }
  });
});
