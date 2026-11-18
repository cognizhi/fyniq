import { expect, test, type Page } from "@playwright/test";
import { asAccountant, open, screens, serve, withMode, type Screen } from "./shell";

// The three device classes and responsive layouts from 320 px (p1-app-shell task 5.1; PRD-001 §7.10, MASTER.md §5.1)
// in a real browser: each shell screen at the five reference frames, the 320 px small phone and the landscapes, with no
// sideways page scroll, the class's own navigation and the same safety information in every class; touch targets of
// 44 × 44 px on a touch screen; folding, unfolding and rotating keep the page, the scroll position, the filters and an
// unsaved draft; and on a screen that spans a hinge, a list and its detail split at the hinge.
// req: FR-UX-37
// req: experience/devices-and-motion Compact device class
// req: experience/devices-and-motion Medium device class
// req: experience/devices-and-motion Expanded device class

type Frame = { name: string; width: number; height: number; cls: "compact" | "medium" | "expanded" };

// The five reference frames of §7.10 (QA-UI-07), the narrowest width FR-UX-37 names, and the landscapes.
const frames: Frame[] = [
  { name: "desktop", width: 1440, height: 900, cls: "expanded" },
  { name: "tablet portrait", width: 834, height: 1194, cls: "medium" },
  { name: "foldable unfolded", width: 690, height: 840, cls: "medium" },
  { name: "phone", width: 390, height: 844, cls: "compact" },
  { name: "foldable folded", width: 344, height: 882, cls: "compact" },
  { name: "small phone", width: 320, height: 568, cls: "compact" },
  { name: "laptop", width: 1024, height: 768, cls: "expanded" },
  { name: "phone landscape", width: 844, height: 390, cls: "medium" },
  { name: "foldable folded landscape", width: 882, height: 344, cls: "medium" },
  { name: "tablet landscape", width: 1194, height: 834, cls: "expanded" },
];

// What must show in every class on each screen: its data, statuses, approvals and safety information (the same
// information as FR-UX-24 asks of the modes; only the layout changes).
const cues: Record<string, (string | RegExp)[]> = {
  "Home (app shell, set-up checklist)": ["1 request to approve", "Set up the company"],
  Inbox: ["Keep records for 10 years"],
  "An approval request": ["10 years", "Approve…"],
  "Global search": ["INV-0041 · Acme Analytics Pte Ltd", "3,270.00 SGD"],
  "Invoices list": ["INV-0041", "Overdue"],
  "A financial statement": ["Net profit"],
};

// The navigation each class shows (§7.10): a bottom bar of at most five in compact, a rail of 80 px in medium, the
// sidebar from 1024 px (64 px wide below 1280 px, 240 px from it).
async function navigationOf(page: Page, f: Frame) {
  const side = page.getByRole("navigation", { name: "Main", exact: true });
  const bottom = page.getByRole("navigation", { name: "Main, bottom bar" });
  if (f.cls === "compact") {
    await expect(bottom).toBeVisible();
    await expect(side).toBeHidden();
    const items = await bottom.locator(":scope > a, :scope > button").count();
    expect(items).toBeLessThanOrEqual(5);
    await expect(bottom.getByRole("button", { name: "More" })).toBeVisible();
  } else {
    await expect(bottom).toBeHidden();
    await expect(side).toBeVisible();
    const width = (await side.boundingBox())!.width;
    expect(width).toBe(f.cls === "medium" ? 80 : f.width >= 1280 ? 240 : 64);
  }
}

// noSidewaysScroll: the page never scrolls sideways; a wide table scrolls inside its own region.
async function noSidewaysScroll(page: Page, f: Frame) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth), { message: `${f.name} ${f.width}px` }).toBeLessThanOrEqual(f.width);
}

for (const s of screens) {
  test(`${s.name}: every device class from 320 px, with no sideways page scroll`, async ({ page }) => {
    await page.setViewportSize({ width: frames[0]!.width, height: frames[0]!.height });
    await open(page, s);
    for (const f of frames) {
      await page.setViewportSize({ width: f.width, height: f.height });
      await noSidewaysScroll(page, f);
      if (s.path !== "/no-such-page") await navigationOf(page, f);
      for (const cue of cues[s.name] ?? []) await expect(page.getByText(cue, { exact: typeof cue === "string" }).first(), `${cue} at ${f.name}`).toBeVisible();
    }
  });
}

for (const path of ["/sign-in", "/install"]) {
  test(`${path}, outside the app frame, from 320 px with no sideways page scroll`, async ({ page }) => {
    await serve(page, { "GET /v1/session": { status: 401, body: { code: "API-AUTH-01", status: 401 } }, "GET /v1/setup": { status: 200, body: { done: path === "/sign-in" } } });
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    for (const f of frames) {
      await page.setViewportSize({ width: f.width, height: f.height });
      await noSidewaysScroll(page, f);
    }
  });
}

test("a wide statement scrolls inside its own region on a phone, its first column kept", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await open(page, screens.find((s) => s.name === "A financial statement")!);
  const table = page.getByRole("table", { name: /Profit and loss/ });
  const region = table.locator("xpath=ancestor::*[contains(@class,'fy-tablewrap')][1]");
  const scroll = await region.evaluate((e) => ({ inner: e.scrollWidth, outer: e.clientWidth, overflow: getComputedStyle(e).overflowX }));
  expect(scroll.inner).toBeGreaterThan(scroll.outer);
  expect(["auto", "scroll"]).toContain(scroll.overflow);
  const first = table.getByRole("rowheader", { name: "Net profit" });
  const before = (await first.boundingBox())!.x;
  await region.evaluate((e) => e.scrollBy({ left: 200 }));
  expect((await first.boundingBox())!.x).toBeCloseTo(before, 0); // the sticky first column stays put
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
});

test("the compact More sheet holds the navigation, the appearance, About and sign out", async ({ page }) => {
  await page.setViewportSize({ width: 344, height: 882 });
  await open(page, screens[0]!);
  await page.getByRole("navigation", { name: "Main, bottom bar" }).getByRole("button", { name: "More" }).click();
  const sheet = page.getByRole("dialog", { name: "More" });
  await expect(sheet.getByRole("link", { name: "Settings" })).toBeVisible();
  await sheet.getByRole("radiogroup", { name: /Theme/ }).getByRole("radio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(sheet.getByRole("button", { name: "Sign out" })).toBeVisible();
  await sheet.getByRole("button", { name: "About Fyniq" }).click();
  await expect(page.getByRole("dialog", { name: "About Fyniq" })).toBeVisible();
});

test("the compact header names the mode and switches it", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await open(page, screens[0]!);
  await page.getByRole("button", { name: "Experience mode: Basic. Switch to Expert" }).click();
  await expect(page.getByRole("button", { name: "Experience mode: Expert. Switch to Basic" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => window.localStorage.getItem("fyniq.mode"))).toBe("expert");
});

// Touch targets (FR-UX-37): on a touch screen every control's hit area is at least 44 × 44 px, in the header, the
// navigation and the page, though it may look smaller (an ⓘ button, a link that names a list row). A link inside a
// sentence is exempt, as WCAG 2.2 SC 2.5.8 exempts it.
test.describe("on a touch screen", () => {
  test.skip(({ browserName }) => browserName !== "chromium", "touch emulation (isMobile) is Chromium's");
  test.use({ hasTouch: true, isMobile: true });

  for (const f of frames.filter((x) => ["phone", "small phone", "tablet portrait"].includes(x.name))) {
    for (const s of screens.filter((x) => ["Home (app shell, set-up checklist)", "Inbox", "Global search", "Profile and security", "Settings"].includes(x.name))) {
      test(`${s.name} at ${f.name}: every control is at least 44 × 44 px`, async ({ page }) => {
        await page.setViewportSize({ width: f.width, height: f.height });
        await open(page, s);
        expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
        // A control's hit area: a 44 px span across and down through its centre reaches it (or the label that
        // activates it), once it is scrolled into view. The probes stand half a pixel inside the span, as two 44 px rows
        // that touch meet on a pixel boundary. A disabled control takes no touch, so it needs no target.
        const small = await page.evaluate(() => {
          const seen: string[] = [];
          const half = 21;
          for (const control of document.querySelectorAll<HTMLElement>("a[href], button, input:not([type=hidden]), select, textarea, [role=button], [role=radio], [role=tab], [role=menuitem]")) {
            // a radio or a checkbox is reached through its label, which is its target
            const label = control instanceof HTMLInputElement && ["radio", "checkbox"].includes(control.type) ? control.labels?.[0] : undefined;
            const e: HTMLElement = label ?? control;
            if (e.getClientRects().length === 0 || getComputedStyle(e).visibility === "hidden" || e.closest("[aria-hidden=true], [inert]")) continue;
            if (e.classList.contains("fy-sr") || e.closest(".fy-sr")) continue; // the skip link, until it is focused
            if (control.matches(":disabled, [aria-disabled=true]")) continue;
            if (e.tagName === "A" && !e.matches(".fy-stretch, .fy-list__item, .fy-nav__item, .fy-bottom__item, .fy-btn, .fy-brand, .fy-tile, .fy-chip, [data-slot=button]") && e.closest("p, .fy-help, .fy-list__meta, dd, .fy-prose")) continue; // a link in a sentence
            e.scrollIntoView({ block: "center", inline: "center" });
            const r = e.getBoundingClientRect();
            const [cx, cy] = [r.left + r.width / 2, r.top + r.height / 2];
            const misses: string[] = [];
            const reaches = (x: number, y: number) => {
              const hit = document.elementFromPoint(x, y);
              const ok = !!hit && (hit === e || e.contains(hit));
              if (!ok) misses.push(`${Math.round(x)},${Math.round(y)}→${hit?.tagName.toLowerCase()}.${String(hit?.className ?? "").slice(0, 30)} "${(hit?.textContent ?? "").trim().slice(0, 20)}"`);
              return ok;
            };
            // the middles of the square's sides: a rounded corner is no part of a hit area
            const corners = [[cx - half, cy], [cx + half, cy], [cx, cy - half], [cx, cy + half]];
            if (!corners.map(([x, y]) => reaches(x!, y!)).every(Boolean)) seen.push(`${e.tagName.toLowerCase()} "${(e.getAttribute("aria-label") ?? e.textContent ?? "").trim().slice(0, 40)}" ${Math.round(r.width)}×${Math.round(r.height)} misses ${misses.join(" ")}`);
          }
          window.scrollTo(0, 0);
          return seen;
        });
        expect(small).toEqual([]);
      });
    }
  }
});

// Folding, unfolding or rotating keeps the page, the scroll position, the filters and an unsaved draft (FR-UX-37): the
// layout changes by CSS alone, so nothing is drawn again from scratch.
test("folding, unfolding and rotating keep the page, its filters, its scroll position and an unsaved draft", async ({ page }) => {
  await page.setViewportSize({ width: 344, height: 882 });
  const s: Screen = screens.find((x) => x.name === "Global search")!;
  await open(page, s);
  await page.getByRole("group", { name: "Kinds of record" }).getByRole("button", { name: /Contacts/ }).click();
  const filter = page.getByRole("group", { name: "Kinds of record" }).getByRole("button", { name: /Contacts/ });
  const pressed = await filter.getAttribute("aria-pressed");
  for (const [width, height] of [
    [690, 840], // unfolded
    [840, 690], // rotated
    [344, 882], // folded again
  ] as const) {
    await page.setViewportSize({ width, height });
    await expect(page).toHaveURL(/\/search\?q=acme/);
    expect(await filter.getAttribute("aria-pressed")).toBe(pressed);
  }

  // An unsaved draft and the scroll position, on the journal editor, which an Accountant prepares.
  await serve(page, asAccountant);
  await withMode(page, "expert");
  await page.goto("/accounting/journals/new");
  const memo = page.getByLabel(/Memo|Narration|Description/).first();
  await memo.fill("Accrual for October's audit fee");
  await page.evaluate(() => window.scrollTo(0, 120));
  const top = await memo.evaluate((e) => e.getBoundingClientRect().top);
  for (const [width, height] of [
    [690, 840],
    [840, 690],
    [344, 882],
  ] as const) {
    await page.setViewportSize({ width, height });
    await expect(memo).toHaveValue("Accrual for October's audit fee");
    await expect(page).toHaveURL(/\/accounting\/journals\/new$/);
    const now = await memo.evaluate((e) => e.getBoundingClientRect());
    expect(now.top, "the draft's field stays in view").toBeGreaterThanOrEqual(0);
    expect(now.bottom).toBeLessThanOrEqual(height);
  }
  expect(top).toBeGreaterThan(-1000);
});

// On a screen that spans a hinge (a dual-screen device), the list and its detail split at the hinge and nothing is
// placed across it (CSS viewport segments; Chromium's DevTools protocol emulates the hinge).
test.describe("on a screen that spans a hinge", () => {
  test.skip(({ browserName }) => browserName !== "chromium", "hinge emulation is Chromium's");

  test("the controls list and a control's detail sit one on each side of the hinge", async ({ page }) => {
    const control = (id: string, status: string) => ({
      id, section: "F.1", control: `Control ${id}`, owner: "controller", frameworks: ["soc2"], mapping: "SOC2 PI1", frequency: "CI, N", status,
      detail: "Each of its 2 check(s) passed.", checked_at: "2026-11-03T20:35:00Z",
      checks: [{ frequency: "CI", how: "make test", kind: "ci", state: "running", status: "pass", detail: "Passed in CI.", keeps: "CI artefact test-report" }],
    });
    await serve(page, {
      "GET /v1/compliance/controls": { status: 200, body: { items: [control("CTL-FIN-01", "pass"), control("CTL-FIN-04", "pass")], frameworks: [{ key: "soc2", title: "SOC 2", controls: 2 }], release: "v1.2.0" } },
    });
    await withMode(page, "expert");
    const cdp = await page.context().newCDPSession(page);
    // two screens of 680 px with a 20 px hinge between them, side by side
    await cdp.send("Emulation.setDeviceMetricsOverride", { width: 1380, height: 900, deviceScaleFactor: 1, mobile: false, displayFeature: { orientation: "vertical", offset: 680, maskLength: 20 } });
    await page.goto("/compliance/controls");
    await page.getByRole("button", { name: /CTL-FIN-04/ }).click();
    expect(await page.evaluate(() => matchMedia("(horizontal-viewport-segments: 2)").matches)).toBe(true);
    const panes = page.locator(".fy-listdetail");
    await expect(panes).toHaveAttribute("data-hinge", "split");
    const list = (await panes.locator(".fy-listdetail__list").boundingBox())!;
    const detail = (await panes.locator(".fy-listdetail__detail").boundingBox())!;
    expect(list.x + list.width).toBeLessThanOrEqual(680.5);
    expect(detail.x).toBeGreaterThanOrEqual(699.5);
    // nothing in either pane reaches across the hinge
    const across = await page.evaluate(() =>
      [...document.querySelectorAll(".fy-listdetail *")].filter((e) => {
        const r = e.getBoundingClientRect();
        return r.width > 0 && r.left < 699.5 && r.right > 680.5;
      }).length,
    );
    expect(across).toBe(0);
  });
});
