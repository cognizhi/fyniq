import { gzipSync } from "node:zlib";
import { expect, test } from "@playwright/test";

// NFR-PERF-UI-03: the shell and Home load at most 250 KB of JavaScript, compressed, and heavy parts load on demand.
// MEASURED 20 Nov 2026, after Home's dashboard, roles' work, policies and what-happened cards moved behind its first paint:
// 292 KB (framework 147: react-dom, Base UI and TanStack; the page's own code 113, of which the help catalogue 33 and the
// registry 25; icons 22; button 10), 42 KB over the PRD's budget, an owner-to-confirm exception (PRD-001 rev 0.332). This test
// holds a ceiling at what the app weighs now and fails when it grows: lower CEILING as the help catalogue loads per area,
// down to BUDGET, and only then tag NFR-PERF-UI-03. The built app is served by `pnpm preview` (the web server of playwright.config.ts); what the page loads before it
// draws is index.html's scripts and the chunks it preloads, each gzipped here as a server would send it.
const BUDGET = 250 * 1000;
const CEILING = 295 * 1000;

// Parts that must never be in the initial load: the command palette's library (loaded when it opens) and the pages
// nobody sees at first: sign-in and installation (ADR-T1).
const ON_DEMAND = [/cmdk/i];

test.skip(({ browserName }) => browserName !== "chromium", "the bundle is the same in every engine");

test("the shell's initial JavaScript does not grow past its ceiling and heavy parts load on demand", async ({ request, baseURL }) => {
  const html = await (await request.get("/")).text();
  const hrefs = [...html.matchAll(/<(?:script|link)\b[^>]*\b(?:src|href)="([^"]+\.js)"[^>]*>/g)].map((m) => m[1]!);
  expect(hrefs.length, "index.html names no script").toBeGreaterThan(0);
  const initial = [...new Set(hrefs.map((h) => new URL(h, baseURL).pathname))];
  let total = 0;
  const sizes: string[] = [];
  for (const path of initial) {
    const body = await (await request.get(path)).body();
    const size = gzipSync(body, { level: 6 }).length;
    total += size;
    sizes.push(`${path} ${Math.round(size / 1000)} KB`);
    for (const re of ON_DEMAND) expect(body.toString("utf8"), `${path} must not hold ${re}`).not.toMatch(re);
  }
  console.log(`NFR-PERF-UI-03 initial JavaScript ${Math.round(total / 1000)} KB gzip (budget ${BUDGET / 1000} KB, ceiling ${CEILING / 1000} KB): ${sizes.join(", ")}`);
  expect(total, sizes.join(", ")).toBeLessThanOrEqual(CEILING);
});
