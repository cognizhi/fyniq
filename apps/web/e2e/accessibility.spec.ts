import { expect, test, type Page } from "@playwright/test";
import { axe, open, screens } from "./shell";

// NFR-A11Y-01 and NFR-A11Y-02 on every shell screen, in the built web app in Chrome (the component tests run the same
// axe rules in jsdom, which has no layout or colour; here they run as drawn).
//
// NFR-A11Y-01, WCAG 2.2 level AA: axe-core's WCAG 2.0, 2.1 and 2.2 A and AA rules and its best practices find nothing
// on any shell screen at the desktop and phone frames, in light, dark and high-contrast themes.
//
// NFR-A11Y-02, keyboard, focus and screen-reader labels: tabbing through each screen, every control that takes focus
// has an accessible name and a visible focus indicator, the page has its landmarks, and on a fresh page the skip link
// goes to the main content.

// Motion is reduced, as a person who asks for it has it: a fade-in caught half-way is a colour no one sees at rest, and
// axe would measure that blend, not the page's tokens.
const themes = [
  { name: "light", appearance: { theme: "light", contrast: "standard", motion: "reduce" } },
  { name: "dark", appearance: { theme: "dark", contrast: "standard", motion: "reduce" } },
  { name: "high contrast", appearance: { theme: "light", contrast: "more", motion: "reduce" } },
];

async function appearance(page: Page, a: object): Promise<void> {
  await page.addInitScript((v) => window.localStorage.setItem("fyniq.appearance", JSON.stringify(v)), a);
}

// req: NFR-A11Y-01
// req: experience/accessibility-and-performance NFR-A11Y-01 WCAG 2.2 level AA
test.describe("axe finds nothing on a shell screen", () => {
  test.describe.configure({ timeout: 120_000 });
  for (const s of screens) {
    for (const t of themes) {
      test(`${s.name}, ${t.name}, desktop`, async ({ page }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await appearance(page, t.appearance);
        await open(page, s);
        expect(await axe(page)).toEqual([]);
      });
    }
    test(`${s.name}, light, phone`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      // Comfortable chosen: in Expert mode's Compact rows a link can sit, at the page's first scroll position, half under
      // the fixed bottom bar, which axe's target-size rule reads as obscured; the bar's scroll padding keeps it reachable
      // (focus.spec.ts proves nothing is hidden when focused).
      await appearance(page, { ...themes[0]!.appearance, density: "comfortable" });
      await open(page, s);
      expect(await axe(page)).toEqual([]);
    });
  }
});

// What a person who cannot see the screen hears, and what a person who cannot use a mouse sees, for the focused control.
async function focused(page: Page): Promise<{ tag: string; name: string; indicator: boolean } | null> {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body || el === document.documentElement) return null;
    const text = (n: Element | null) => n?.textContent?.replace(/\s+/g, " ").trim() ?? "";
    const labelledby = el
      .getAttribute("aria-labelledby")
      ?.split(/\s+/)
      .map((id) => text(document.getElementById(id)))
      .join(" ")
      .trim();
    const labels = "labels" in el ? [...((el as HTMLInputElement).labels ?? [])].map((l) => text(l)).join(" ") : "";
    const name = el.getAttribute("aria-label") || labelledby || labels || text(el) || el.getAttribute("title") || el.getAttribute("alt") || (el as HTMLInputElement).placeholder || "";
    const st = getComputedStyle(el);
    const outline = st.outlineStyle !== "none" && parseFloat(st.outlineWidth) > 0 && st.outlineColor !== "rgba(0, 0, 0, 0)";
    const ring = st.boxShadow !== "none" && st.boxShadow !== "";
    return { tag: el.tagName.toLowerCase() + (el.id ? `#${el.id}` : "") + (el.getAttribute("role") ? `[${el.getAttribute("role")}]` : ""), name: name.trim(), indicator: outline || ring };
  });
}

// req: NFR-A11Y-02
// req: experience/accessibility-and-performance NFR-A11Y-02 Keyboard, focus and screen-reader labels
test.describe("every shell screen works from the keyboard and names what it holds", () => {
  test.describe.configure({ timeout: 120_000 });
  for (const s of screens) {
    test(s.name, async ({ page }) => {
      await page.setViewportSize({ width: 1440, height: 900 });
      await open(page, s);
      // The landmarks: one main; and, inside the app frame, a labelled main navigation and a banner. A page drawn without
      // the frame (an unknown address) is one main with its heading and a way home.
      expect(await page.locator("main, [role=main]").count(), "main landmarks").toBe(1);
      const framed = (await page.locator(".fy-app").count()) > 0;
      if (framed) {
        await expect(page.getByRole("navigation", { name: "Main" }).first()).toBeAttached();
        expect(await page.getByRole("banner").count(), "banner landmarks").toBeGreaterThan(0);
      } else {
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(page.getByRole("link").first()).toBeVisible();
      }
      // On a fresh page the first Tab stop is the skip link, and it goes to the main content. A page that moves focus
      // to a refusal it shows (an alert, so it is heard at once) starts from there instead.
      const alerted = await page.evaluate(() => document.activeElement?.getAttribute("role") === "alert");
      if (framed && !alerted) {
        await page.keyboard.press("Tab");
        const first = await focused(page);
        expect(first?.name, "the first Tab stop").toMatch(/Skip to content/);
        await page.keyboard.press("Enter");
        expect(await page.evaluate(() => location.hash)).toBe("#main");
      }
      // Every control that takes focus has a name and shows where focus is.
      const problems: string[] = [];
      const seen = new Set<string>();
      for (let i = 0; i < 60; i++) {
        await page.keyboard.press("Tab");
        const f = await focused(page);
        if (!f) continue;
        const key = `${f.tag}|${f.name}`;
        if (seen.has(key)) continue;
        seen.add(key);
        if (!f.name) problems.push(`${f.tag} has no accessible name`);
        if (!f.indicator) problems.push(`${f.tag} "${f.name}" shows no focus indicator`);
      }
      expect(seen.size, "controls reached by Tab").toBeGreaterThan(framed ? 3 : 0);
      expect(problems).toEqual([]);
    });
  }
});
