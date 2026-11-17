import { expect, test, type Page, type Route } from "@playwright/test";

// Motion, switchable (FR-UX-38; QA-UI-07's motion clause): with the organisation's motion Off, with a person's Reduce
// motion, and with the device's prefers-reduced-motion, no animation or transition runs: every element's computed
// animation and transition durations are 0. With motion on, the same page does move, so the check can tell.
// req: QA-UI-07
// req: FR-UX-38

type Answer = { status: number; body?: unknown };

const session = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller", "administrator"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
  motion: "on",
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };

// open serves the signed-in shell, with the organisation's motion as given, and opens the Inbox, a page that enters with
// the page transition and holds buttons and links with their hover transitions.
async function open(page: Page, motion: "on" | "off"): Promise<void> {
  const answers: Record<string, Answer> = {
    "GET /v1/setup": { status: 200, body: { done: true } },
    "GET /v1/session": { status: 200, body: { ...session, motion } },
    "GET /v1/organisation": { status: 200, body: organisation },
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    "GET /v1/review-log": { status: 200, body: { items: [], unreviewed: 0, overdue: 0 } },
  };
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const a = answers[`${req.method()} ${decodeURIComponent(new URL(req.url()).pathname.replace(/^\/api/, ""))}`] ?? { status: 200, body: { items: [] } };
    await route.fulfill({ status: a.status, contentType: "application/json", body: JSON.stringify(a.body) });
  });
  await page.goto("/inbox");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
}

// settled waits until the stylesheet has resolved the base motion token on the page to want seconds (0.18 with motion
// on, 0 otherwise; the build writes 180ms as .18s), so a check never reads the page before its styles apply.
async function settled(page: Page, want: number): Promise<void> {
  await expect
    .poll(() =>
      page.evaluate(() => {
        const v = getComputedStyle(document.querySelector(".fy-page") ?? document.body).getPropertyValue("--duration-base").trim();
        return v === "" ? null : v.endsWith("ms") ? parseFloat(v) / 1000 : parseFloat(v);
      }),
    )
    .toBe(want);
}

// moving lists the elements whose computed animation or transition lasts longer than 0 s.
function moving(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const seconds = (v: string) => Math.max(0, ...v.split(",").map((x) => (x.trim().endsWith("ms") ? parseFloat(x) / 1000 : parseFloat(x)) || 0));
    const out: string[] = [];
    for (const el of [document.documentElement, ...document.querySelectorAll("*")]) {
      for (const pseudo of [null, "::before", "::after"]) {
        const s = getComputedStyle(el, pseudo);
        const animated = s.animationName !== "none" && seconds(s.animationDuration) > 0;
        if (animated || (s.transitionProperty !== "none" && seconds(s.transitionDuration) > 0)) {
          out.push(`${el.tagName.toLowerCase()}.${String(el.getAttribute("class") ?? "").slice(0, 40)}${pseudo ?? ""} ${s.animationDuration}/${s.transitionDuration}`);
        }
      }
    }
    return out;
  });
}

test("with motion on, the page moves", async ({ page }) => {
  await open(page, "on");
  await settled(page, 0.18);
  expect(await moving(page)).not.toEqual([]);
});

test("the organisation's motion Off stops every animation and transition", async ({ page }) => {
  await open(page, "off");
  await expect(page.locator("html")).toHaveAttribute("data-motion", "off");
  await settled(page, 0);
  expect(await moving(page)).toEqual([]);
});

test("a person's Reduce motion stops every animation and transition", async ({ page }) => {
  await page.addInitScript(() => window.localStorage.setItem("fyniq.appearance", JSON.stringify({ motion: "reduce" })));
  await open(page, "on");
  await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");
  await settled(page, 0);
  expect(await moving(page)).toEqual([]);
});

test("the device's prefers-reduced-motion stops every animation and transition", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await open(page, "on");
  await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");
  await settled(page, 0);
  expect(await moving(page)).toEqual([]);
});
