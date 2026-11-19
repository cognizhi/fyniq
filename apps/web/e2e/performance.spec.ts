import { expect, test, type Page } from "@playwright/test";
import { open, screens, serve } from "./shell";

// NFR-PERF-UI-01: page load and interaction budgets, measured in Chrome on the built web app with every /api call
// answered by a fixture, so what is measured is the app's own loading and rendering (the API's budgets are its own,
// NFR-PERF-API). "A mid-range laptop over 4G" is Chrome's emulation of a 4G link (9 Mbps down, 1.5 Mbps up, 85 ms
// round trip), on CI's runner, about a mid-range laptop's speed, or on a developer's faster machine at half its speed
// (FYNIQ_PERF_CPU sets the slowdown). Largest Contentful Paint is read from each cold load (no cache) and
// its 75th percentile kept under 1.5 s; an interaction's next paint, from the Event Timing API, under 200 ms; and a change
// of route whose data is cached, from the click to the new page drawn, under 300 ms.

test.skip(({ browserName }) => browserName !== "chromium", "network and CPU emulation are Chrome's (CDP)");

const LCP = 1500;
const INP = 200;
const ROUTE = 300;

async function midRange(page: Page, cold: boolean) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.emulateNetworkConditions", { offline: false, latency: 85, downloadThroughput: (9e6 / 8) | 0, uploadThroughput: (1.5e6 / 8) | 0 });
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: cold });
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: Number(process.env.FYNIQ_PERF_CPU ?? (process.env.CI ? 1 : 2)) });
}

// lcp is the page's Largest Contentful Paint so far, in milliseconds from the navigation's start.
function lcp(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      new Promise<number>((done) => {
        new PerformanceObserver((list) => {
          const all = list.getEntries();
          done(all[all.length - 1]?.startTime ?? 0);
        }).observe({ type: "largest-contentful-paint", buffered: true });
      }),
  );
}

const p75 = (xs: number[]) => [...xs].sort((a, b) => a - b)[Math.ceil(xs.length * 0.75) - 1] ?? 0;

// req: NFR-PERF-UI-01
// req: experience/accessibility-and-performance NFR-PERF-UI-01 Page load and interaction budgets
test.describe("page load and interaction budgets", () => {
  test.describe.configure({ timeout: 120_000 });

  test(`the 75th percentile Largest Contentful Paint of the shell's pages stays under ${LCP} ms over 4G`, async ({ page }) => {
    const pages = screens.filter((s) => ["Home (app shell, set-up checklist)", "Invoices list", "Inbox", "Settings"].includes(s.name));
    const seen: number[] = [];
    for (let round = 0; round < 2; round++) {
      for (const s of pages) {
        await midRange(page, true);
        await open(page, s);
        seen.push(await lcp(page));
        await page.unrouteAll({ behavior: "ignoreErrors" });
      }
    }
    console.log(`NFR-PERF-UI-01 LCP p75 ${Math.round(p75(seen))} ms of ${seen.map((x) => Math.round(x)).join(", ")}`);
    expect(seen.every((x) => x > 0)).toBe(true);
    expect(p75(seen), `LCP of each load: ${seen.map((x) => Math.round(x)).join(", ")} ms`).toBeLessThanOrEqual(LCP);
  });

  test(`an interaction paints its result within ${INP} ms`, async ({ page }) => {
    await midRange(page, false);
    await open(page, screens.find((s) => s.name === "Home (app shell, set-up checklist)")!);
    await page.evaluate(() => {
      const w = window as unknown as { slowest: number };
      w.slowest = 0;
      new PerformanceObserver((list) => {
        for (const e of list.getEntries() as (PerformanceEntry & { interactionId?: number })[]) {
          if (e.interactionId) w.slowest = Math.max(w.slowest, e.duration);
        }
      }).observe({ type: "event", buffered: true, durationThreshold: 16 } as PerformanceObserverInit);
    });
    // Switching the mode redraws the whole page; the palette and the Help menu open over it.
    await page.getByRole("radio", { name: "Expert" }).click();
    await expect(page.getByRole("radio", { name: "Expert" })).toHaveAttribute("aria-checked", "true");
    await page.getByRole("button", { name: /Search and commands/ }).first().click();
    await expect(page.getByRole("combobox")).toBeVisible();
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Help" }).click();
    await expect(page.getByRole("menu")).toBeVisible();
    await page.keyboard.press("Escape");
    await page.waitForTimeout(300); // the last event's entry is reported after its paint
    const slowest = await page.evaluate(() => (window as unknown as { slowest: number }).slowest);
    console.log(`NFR-PERF-UI-01 slowest interaction ${Math.round(slowest)} ms`);
    expect(slowest, `the slowest interaction took ${Math.round(slowest)} ms`).toBeLessThanOrEqual(INP);
  });

  test(`a change of route whose data is cached draws the new page within ${ROUTE} ms`, async ({ page }) => {
    await midRange(page, false);
    await serve(page);
    await page.goto("/");
    await expect(page.getByRole("list", { name: "Set up the company" })).toBeVisible();
    await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Sales" }).click();
    await expect(page.getByRole("table", { name: "Invoices" })).toBeVisible();
    // Both pages' data is cached now: go back and forth, from the click to the other page's content drawn.
    // From the click to the other page's heading drawn in place of this one's, at its address.
    const go = (to: string, path: string) =>
      page.evaluate(
        ([link, where]) =>
          new Promise<number>((done) => {
            const heading = () => document.querySelector("main h1")?.textContent ?? "";
            const before = heading();
            const start = performance.now();
            const a = [...document.querySelectorAll<HTMLAnchorElement>("nav[aria-label='Main'] a")].find((x) => x.querySelector(".fy-nav__label")?.textContent?.trim() === link);
            if (!a) throw new Error(`no link ${link}`);
            a.click();
            const wait = () => (location.pathname === where && heading() && heading() !== before ? requestAnimationFrame(() => done(performance.now() - start)) : requestAnimationFrame(wait));
            wait();
          }),
        [to, path] as const,
      );
    const times: number[] = [];
    for (let i = 0; i < 3; i++) {
      times.push(await go("Home", "/"));
      times.push(await go("Sales", "/sales/invoices"));
    }
    console.log(`NFR-PERF-UI-01 route changes ${times.map((x) => Math.round(x)).join(", ")} ms`);
    expect(Math.max(...times), `each change of route: ${times.map((x) => Math.round(x)).join(", ")} ms`).toBeLessThanOrEqual(ROUTE);
  });
});
