import { expect, test, type Page } from "@playwright/test";
import { axe, serve, session } from "./shell";

// req: QA-UI-06
// req: G4-104
// req: FR-UX-34
// QA-UI-06 and G4-104: each role's first-run tour (FR-UX-34) completed by keyboard alone, with reduced motion and with
// animation, with no axe violation, and with no write but the person's own tour state; and a tour left part-way
// resumed where it stopped.

interface State {
  tour: string;
  step: number;
  offer: boolean;
  etag: string;
}

const roles: Record<string, { roles: string[]; steps: number }> = {
  founder: { roles: ["owner", "controller", "administrator"], steps: 7 },
  accountant: { roles: ["accountant"], steps: 7 },
  employee: { roles: ["employee"], steps: 5 },
  approver: { roles: ["approver"], steps: 7 },
  "board viewer": { roles: ["board_viewer"], steps: 4 },
};

// signIn serves the shell's fixtures as a person of these roles with this tour state, and keeps every save of it and
// every request that is not a read.
async function signIn(page: Page, as: string[], state: State) {
  const saves: { tour: string; step: number }[] = [];
  let current = state;
  const none = { status: 200, body: { items: [] } };
  const seen = await serve(page, {
    // what an employee's own pages read, so they show as a person with nothing yet sees them
    "GET /v1/expense-claims": none,
    "GET /v1/card-lines": none,
    "GET /v1/cash-advances": none,
    "GET /v1/session": { status: 200, body: { ...session, roles: as } },
    "GET /v1/help-state": () => ({ status: 200, body: current }),
    "PUT /v1/help-state/tour": (_url, body) => {
      const b = body as { tour: string; step: number };
      saves.push(b);
      current = { tour: b.tour, step: b.step, offer: b.tour === "started", etag: `"${saves.length}"` };
      return { status: 200, body: current, headers: { ETag: current.etag } };
    },
  });
  return { saves, writes: () => seen.filter((k) => !k.startsWith("GET ")) };
}

// keyTo presses Tab until the button of that name has the focus, as a person on the keyboard does, and Shift+Tab once
// Tab stops moving (Firefox leaves the page after its last control instead of wrapping); in WebKit on a Mac, Option+Tab,
// as Safari moves Tab between form fields only.
async function keyTo(page: Page, name: string, browser: string) {
  const tab = browser === "webkit" && process.platform === "darwin" ? "Alt+Tab" : "Tab";
  let key = tab;
  let last = "";
  const seen: string[] = [];
  for (let i = 0; i < 120; i++) {
    const [at, text] = await page.evaluate(() => {
      const e = document.activeElement as HTMLElement | null;
      if (e && !e.dataset.keyed) e.dataset.keyed = String(Math.random()); // to tell one control from the next
      return [e?.dataset.keyed ?? "", e?.textContent?.trim() ?? ""];
    });
    if (text === name) return;
    if (at === last && key === tab) key = `Shift+${tab}`;
    last = at;
    seen.push(text.slice(0, 24));
    await page.keyboard.press(key);
  }
  throw new Error(`the keyboard never reached ${name}: ${seen.join(" | ")}`);
}

const fresh: State = { tour: "not_started", step: 0, offer: true, etag: '"none"' };

for (const [who, r] of Object.entries(roles)) {
  for (const motion of ["reduce", "no-preference"] as const) {
    test(`the ${who}'s tour, by keyboard, ${motion === "reduce" ? "with reduced motion" : "with animation"}: only the tour state is written`, async ({ page, browserName }) => {
      await page.emulateMedia({ reducedMotion: motion });
      const { saves, writes } = await signIn(page, r.roles, fresh);
      await page.goto("/");
      await expect(page.getByRole("button", { name: "Start the tour" })).toBeVisible();
      await keyTo(page, "Start the tour", browserName);
      await page.keyboard.press("Enter");
      const card = page.getByRole("dialog", { name: /./ });
      await expect(card).toContainText(`1 of ${r.steps}`);
      // axe measures contrast as drawn, so once the page's and the tour's entrances have run
      await page.waitForFunction(() => document.getAnimations().every((a) => a.playState !== "running"));
      expect(await axe(page)).toEqual([]);
      const spot = page.locator(".fy-tour-spot");
      const duration = await spot.evaluate((el) => getComputedStyle(el).transitionDuration);
      if (motion === "reduce") expect(duration.split(",").every((d) => parseFloat(d) === 0)).toBe(true);
      else expect(duration.split(",").some((d) => parseFloat(d) > 0)).toBe(true);
      for (let i = 1; i < r.steps; i++) {
        await page.keyboard.press("ArrowRight");
        await expect(card).toContainText(`${i + 1} of ${r.steps}`);
      }
      await expect(page.getByRole("button", { name: "Done" })).toBeVisible();
      await page.keyboard.press("ArrowRight");
      await expect(card).toHaveCount(0);
      await expect.poll(() => saves.at(-1)).toEqual({ tour: "finished", step: r.steps - 1 });
      expect(saves.slice(0, r.steps).map((s) => s.step)).toEqual([...Array(r.steps).keys()]);
      expect(new Set(writes())).toEqual(new Set(["PUT /v1/help-state/tour"]));
      await expect(page.getByRole("button", { name: "Start the tour" })).toHaveCount(0);
    });
  }
}

test("a tour left part-way is offered again and resumes at the step shown last; Esc ends it there", async ({ page, browserName }) => {
  const { saves, writes } = await signIn(page, roles.founder!.roles, { tour: "started", step: 2, offer: true, etag: '"2"' });
  await page.goto("/");
  await keyTo(page, "Resume the tour", browserName);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "Your Inbox" })).toContainText("3 of 7");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect.poll(() => saves.at(-1)).toEqual({ tour: "skipped", step: 2 });
  expect(new Set(writes())).toEqual(new Set(["PUT /v1/help-state/tour"]));
});
