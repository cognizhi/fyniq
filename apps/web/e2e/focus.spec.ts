import { expect, test, type Page } from "@playwright/test";
import { open, screens } from "./shell";

// NFR-A11Y-03, focus not obscured (WCAG 2.2 SC 2.4.11): tabbing through a screen, each control that takes focus is
// drawn where the person sees it, never under the sticky header, the compact bottom bar, the AI panel or a toast,
// whose own controls aside. The kit's scroll padding keeps a control the browser scrolls to clear of the sticky
// regions. Checked on the shell's screens at the desktop and phone frames, and with the AI panel open.

// obscured names the focused element when an overlay it is not part of hides it (SC 2.4.11, AA: not entirely hidden):
// the sticky header, the compact bottom bar and the AI panel by what is drawn at the control's centre, so the stacking
// order counts (the skip link is drawn above the header); a toast, which lets clicks through and so is not hit-tested,
// by whether it covers the control entirely.
async function obscured(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done))); // the frame the person sees
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return null;
    const name = () => el.getAttribute("aria-label") ?? el.textContent?.trim().slice(0, 40) ?? el.tagName;
    const x = Math.min(Math.max(r.left + r.width / 2, 0), window.innerWidth - 1);
    const y = Math.min(Math.max(r.top + r.height / 2, 0), window.innerHeight - 1);
    const hit = document.elementFromPoint(x, y);
    if (hit && !el.contains(hit) && !hit.contains(el)) {
      const o = hit.closest(".fy-top, .fy-bottom, .fy-ai");
      if (o && !o.contains(el)) return `${el.tagName.toLowerCase()} "${name()}" under ${o.className.toString().slice(0, 30)}`;
    }
    for (const o of document.querySelectorAll("[data-sonner-toaster] > *, .fy-toast")) {
      const st = getComputedStyle(o);
      if (o.contains(el) || st.pointerEvents !== "none" || st.visibility === "hidden" || st.display === "none" || Number(st.opacity) === 0) continue;
      const b = o.getBoundingClientRect();
      if (Math.min(r.right, b.right) - Math.max(r.left, b.left) >= r.width - 0.5 && Math.min(r.bottom, b.bottom) - Math.max(r.top, b.top) >= r.height - 0.5) {
        return `${el.tagName.toLowerCase()} "${name()}" under a toast`;
      }
    }
    return null;
  });
}

async function tabThrough(page: Page, presses = 50): Promise<string[]> {
  const found: string[] = [];
  for (let i = 0; i < presses; i++) {
    await page.keyboard.press("Tab");
    const o = await obscured(page);
    if (o && !found.includes(o)) found.push(o);
  }
  return found;
}

const frames = [
  { name: "desktop", width: 1440, height: 900 },
  { name: "phone", width: 390, height: 844 },
];

const checked = screens.filter((s) => ["Home (app shell, set-up checklist)", "Invoices list", "A financial statement", "Settings"].includes(s.name));

// req: NFR-A11Y-03
// req: experience/accessibility-and-performance NFR-A11Y-03 Focus not obscured
for (const f of frames) {
  for (const s of checked) {
    test(`${s.name} at the ${f.name} frame: no focused control is hidden under a sticky region`, async ({ page }) => {
      await page.setViewportSize({ width: f.width, height: f.height });
      await open(page, s);
      expect(await tabThrough(page)).toEqual([]);
    });
  }
}

test("with the AI panel open, the page's focused controls stay clear of it", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const home = checked[0]!;
  await open(page, home);
  const assistant = page.getByRole("button", { name: "Assistant" });
  test.skip(!(await assistant.isVisible()), "the assistant is not part of this person's shell");
  await assistant.click();
  await expect(page.getByRole("complementary", { name: "Assistant" })).toBeVisible();
  await page.getByRole("link", { name: "Skip to content" }).focus();
  expect(await tabThrough(page, 40)).toEqual([]);
});

test("finds a focused control a toast covers", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await open(page, checked[0]!);
  await page.evaluate(() => {
    const toaster = document.createElement("section");
    toaster.setAttribute("data-sonner-toaster", "");
    const t = document.createElement("div");
    t.style.cssText = "position:fixed;inset:0;z-index:9999;background:transparent;pointer-events:none";
    toaster.append(t);
    document.body.append(toaster);
  });
  expect((await tabThrough(page, 5)).length).toBeGreaterThan(0);
});
