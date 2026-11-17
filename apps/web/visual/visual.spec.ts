import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { routes } from "../src/registry";
import { statePath, type Setup } from "./setup";

// The visual snapshots (QA-UI-07, QA-RPT-04; p1-launch-readiness design D5): the key pages (Home, the Inbox, a financial
// statement, an invoice and Settings) of the sample company, and with VISUAL_ALL=1 every route, at the five reference
// frames of §7.10, in light and dark,
// each picture's SHA-256 against visual/baselines.json. The business date is fixed on the stack and the clock in the
// browser, motion is reduced and the caret hidden, so a run draws what the last did; a picture that differs, or has no
// baseline, fails the run, and is left with every hash the run saw in test-results/visual/. While no baseline is
// approved at all, a run only records.
// req: QA-UI-07
// req: QA-RPT-04

const out = resolve(import.meta.dirname, "../test-results/visual");
const baselines = JSON.parse(readFileSync(resolve(import.meta.dirname, "baselines.json"), "utf8")) as Record<string, string>;
const now = new Date("2026-12-31T10:00:00+08:00"); // the stack's business date (scripts/visual.sh), at 10:00 in Singapore

// The five reference frames (§7.10, FR-UX-37).
const frames = [
  { name: "desktop", width: 1440, height: 900 },
  { name: "tablet", width: 834, height: 1194 },
  { name: "foldable-open", width: 690, height: 840 },
  { name: "phone", width: 390, height: 844 },
  { name: "foldable-folded", width: 344, height: 882 },
] as const;

// The visual setup's signed-in person and draft invoice (visual/setup.ts), read once the setup has run.
const setup = () => JSON.parse(readFileSync(statePath, "utf8")) as Setup;

// The key pages of QA-UI-07 (rev 0.284); the invoice is the draft the setup made.
const keyPages: { name: string; path: (s: Setup) => string }[] = [
  { name: "home", path: () => "/" },
  { name: "inbox", path: () => "/inbox" },
  { name: "statement", path: () => "/reports/statements/pl" },
  { name: "invoice", path: (s) => s.invoice },
  { name: "settings", path: () => "/settings" },
];

// Every route (rev 0.284: nightly and before each release, VISUAL_ALL=1): the key pages, then each signed-in route of the
// registry whose path names no record, under its registry ID.
const keyPaths = ["/", "/inbox", "/reports/statements/pl", "/settings"];
const pages =
  process.env.VISUAL_ALL === "1"
    ? [
        ...keyPages,
        ...routes
          .filter((r) => "signedIn" in r && r.signedIn && !r.path.includes("$") && !keyPaths.includes(r.path))
          .map((r) => ({ name: r.id.replace(/[^a-z0-9-]/g, "-"), path: () => r.path })),
      ]
    : keyPages;

// draw opens path at the frame and returns the whole page's picture, once it has settled: the signed-in frame and its
// heading, no request in flight, its fonts loaded and no skeleton left. The run reads faster than a person, so a page
// the API's per-minute limit refused (429, FR-INT-16) is opened again after its Retry-After. The viewport then grows to
// the page's height, so the sidebar and the header are drawn once, where they stand, not where a scrolled capture puts
// them.
async function draw(page: Page, path: string, frame: (typeof frames)[number]): Promise<Buffer> {
  for (let attempt = 1; ; attempt++) {
    let wait = 0;
    const watch = (r: import("@playwright/test").Response) => {
      if (r.status() === 429) wait = Math.max(wait, Number(r.headers()["retry-after"] ?? 60) * 1000 + 1000);
    };
    page.on("response", watch);
    await page.setViewportSize({ width: frame.width, height: frame.height });
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 }).first()).toBeVisible();
    await page.waitForLoadState("networkidle");
    page.off("response", watch);
    if (!wait) break;
    if (attempt === 4) throw new Error(`${path} at ${frame.name}: the API kept refusing the page's reads (429)`);
    await page.waitForTimeout(wait);
  }
  await expect(page.getByRole("navigation", { name: /^Main/ }).first()).toBeAttached();
  await page.evaluate(() => document.fonts.ready);
  await expect(page.locator(".fy-skeleton")).toHaveCount(0);
  const height = await page.evaluate(() => Math.ceil(document.documentElement.scrollHeight));
  await page.setViewportSize({ width: frame.width, height: Math.max(frame.height, height) });
  await page.waitForLoadState("networkidle");
  return page.screenshot({ animations: "disabled", caret: "hide", scale: "css" });
}

for (const key of pages) {
  for (const theme of ["light", "dark"] as const) {
    test(`${key.name} in ${theme}, at the five frames`, async ({ browser }) => {
      const state = setup();
      const context = await browser.newContext({ storageState: state.storageState, colorScheme: theme, reducedMotion: "reduce", locale: "en-SG",
        timezoneId: "Asia/Singapore" });
      const page = await context.newPage();
      await page.clock.setFixedTime(now);
      await page.addInitScript((t) => {
        window.localStorage.setItem("fyniq.appearance", JSON.stringify({ theme: t, contrast: "system", density: "comfortable", motion: "reduce" }));
        window.localStorage.setItem("fyniq.mode", "expert");
      }, theme);
      const path = key.path(state);
      const seen: Record<string, string> = {};
      const changed: string[] = [];
      mkdirSync(out, { recursive: true });
      for (const frame of frames) {
        const id = `${key.name}/${frame.name}/${theme}`;
        const png = await draw(page, path, frame);
        const hash = createHash("sha256").update(png).digest("hex");
        seen[id] = hash;
        if (baselines[id] !== hash) {
          writeFileSync(resolve(out, `${id.replaceAll("/", "-")}.png`), png);
          changed.push(`${id}: ${baselines[id] ?? "no baseline"} → ${hash}`);
        }
      }
      writeFileSync(resolve(out, `hashes-${key.name}-${theme}.json`), `${JSON.stringify(seen, null, 2)}\n`);
      await context.close();
      // Until the first baselines are approved, a run only records what it drew (the hashes are linux/amd64's, as CI
      // draws them, so CI makes them; design D5's bootstrap).
      if (Object.keys(baselines).length === 0) {
        test.info().annotations.push({ type: "no baselines yet", description: "approve the run's hashes with scripts/visual.py approve" });
        return;
      }
      expect(changed, "pictures that differ from visual/baselines.json (approve them with scripts/visual.py approve)").toEqual([]);
    });
  }
}
