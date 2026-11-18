import { expect, test, type Page, type Route } from "@playwright/test";

// A check fires, its item opens the record's page, the record is fixed, and the check turns OK (p1-checks task 5.3),
// against the built web app in a real browser with every /api call answered by a fixture (the Go suites prove what the
// checks find): C.2 #6 finds an external person still a member after their roles ended; the Owner opens them from
// Books health, removes them on Members and roles, and the next run of the checks is OK.
// req: FR-CTL-01
// req: C.2 #6
// req: FR-ADM-01

const session = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const ann = { id: "m-1", user_id: session.user_id, display_name: "Ann Tan", roles: [{ role: "owner" }], expired_roles: [] };
const joanne = { id: "m-2", user_id: "u-2", display_name: "Joanne Lee", roles: [], expired_roles: [{ role: "company_secretary", expires_at: "2026-09-30T15:59:59Z" }] };

const run = { trigger: "request", started_at: "2026-11-02T02:00:00Z", finished_at: "2026-11-02T02:00:02Z", ledger_revision: 42, checks: 1 };
const check = (status: "error" | "ok") => ({
  key: "controls.external-users-past-expiry",
  ref: "C.2 #6",
  area: "Controls",
  worst: "error",
  title: "No external user stays a member past their end date",
  explain: "An external user's roles end at their end date (SEC-06).",
  rules: "SEC-06, FR-ADM-01",
  nightly: false,
  status,
  count: status === "error" ? 1 : 0,
  items: status === "error" ? [{ kind: "member", id: "m-2", label: "Joanne Lee", detail: "Roles ended: company_secretary on 30 Sep 2026" }] : [],
  items_hidden: false,
  items_area: "Users, roles, team mode",
  detail: status === "error" ? "1 external user(s) still members after their roles ended" : "No member stays past the end of their roles",
  fix: status === "error" ? "Remove them on Settings › Members, or give them a role again with a new end date" : undefined,
  ledger_revision: 42,
  run_at: "2026-11-02T02:00:00Z",
  trigger: "request",
  owner: { role: "owner", version: 0 },
});
const checks = (removed: boolean) => ({
  health: removed ? { score: "ok", problems: 0, warnings: 0, due: 0, ok: 1, not_assessed: 0 } : { score: "problems", problems: 1, warnings: 0, due: 0, ok: 0, not_assessed: 0 },
  may_name_owners: true,
  may_review: true,
  run_requested: false,
  last_run: run,
  last_nightly_run: run,
  checks: [check(removed ? "ok" : "error")],
});

async function serve(page: Page) {
  const seen: string[] = [];
  let removed = false;
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api/, "");
    const key = `${req.method()} ${path}`;
    seen.push(key);
    const json = (status: number, body: unknown) => route.fulfill({ status, contentType: status >= 400 ? "application/problem+json" : "application/json", body: JSON.stringify(body) });
    switch (key) {
      case "GET /v1/setup":
        return json(200, { done: true });
      case "GET /v1/session":
        return json(200, session);
      case "GET /v1/organisation":
        return json(200, organisation);
      case "GET /v1/organisation/brand": // the header shows the logo (FR-ORG-09): none uploaded
        return json(200, { version: 0, accent_options: [] });
      case "GET /v1/checks":
        return json(200, checks(removed));
      case "GET /v1/members":
        return json(200, { items: removed ? [ann] : [ann, joanne] });
      case "DELETE /v1/members/m-2":
        removed = true;
        return json(200, { ...joanne, roles: [], expired_roles: [], removed: true });
      case "GET /v1/team-mode":
        return json(200, { mode: "founder", version: 0, blocks_conflicts: false, conflicts: [], overrides: [], history: [] });
      case "GET /v1/members/invitations":
      case "GET /v1/approvals":
      case "GET /v1/notifications":
      case "GET /v1/banners":
      case "GET /v1/operations": // the header's operations tray (FR-INT-11)
        return json(200, { items: [] });
      default:
        return route.fulfill({ status: 403, contentType: "application/problem+json", body: JSON.stringify({ code: "API-AUTHZ-01", status: 403 }) });
    }
  });
  return seen;
}

test("a check's item opens its page, is fixed there, and the check turns OK", async ({ page }) => {
  const seen = await serve(page);
  await page.goto("/checks");
  const row = page.locator('[data-check="controls.external-users-past-expiry"]');
  await expect(row.getByText("No external user stays a member past their end date")).toBeVisible();
  await expect(row.getByText("Blocks the close")).toBeVisible();

  // The item opens the members' page, where Joanne's roles show as ended.
  await row.getByRole("link", { name: "Joanne Lee" }).click();
  await expect(page).toHaveURL(/\/settings\/members$/);
  await expect(page.getByText("Roles ended", { exact: true }).first()).toBeVisible();

  // The Owner removes her, alone, as the company's only other member.
  await page.getByRole("button", { name: /Change roles: Joanne Lee/ }).click();
  await page.getByRole("button", { name: "Remove from the company" }).click();
  await page.getByRole("dialog", { name: "Remove Joanne Lee from the company?" }).getByRole("button", { name: "Remove from the company" }).click();
  await expect(page.getByRole("button", { name: /Change roles: Joanne Lee/ })).toBeHidden();
  expect(seen).toContain("DELETE /v1/members/m-2");

  // Books health is OK again: the check found nothing, and nothing stops the close.
  await page.goto("/checks");
  const after = page.locator('[data-check="controls.external-users-past-expiry"]');
  await expect(after.getByText("No member stays past the end of their roles")).toBeVisible();
  await expect(after.getByText("Blocks the close")).toBeHidden();
  await expect(page.getByText(/\d+ stops? the close\./)).toBeHidden();
});
