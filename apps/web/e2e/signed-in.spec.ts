import { expect, test, type Page, type Route } from "@playwright/test";

// Journeys through the signed-in screens against the built web app, with every /api call answered by
// a fixture (the Go suites prove the API): the layout, the Inbox and an approval's confirmation, and
// the second-factor gate.
// req: FR-UX-03
// req: FR-UX-40
// req: SEC-01

type Answer = { status: number; body?: unknown; headers?: Record<string, string> };

async function api(page: Page, handlers: Record<string, Answer>) {
  const seen: string[] = [];
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const path = decodeURIComponent(new URL(req.url()).pathname.replace(/^\/api/, ""));
    const key = `${req.method()} ${path}`;
    seen.push(key);
    const a = handlers[key];
    if (!a) return route.fulfill({ status: 599, body: `no fixture for ${key}` });
    await route.fulfill({ status: a.status, headers: a.headers, contentType: a.status >= 400 ? "application/problem+json" : "application/json", body: a.body === undefined ? "" : JSON.stringify(a.body) });
  });
  return seen;
}

const me = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01";
const session = {
  provider: "local",
  user_id: me,
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller", "administrator"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const request = {
  id: "a-1",
  object_type: "organisation_setting",
  object_key: "retention.records_years",
  operation: "organisation_setting.change",
  summary: "Keep records for 10 years",
  policy: "founder/organisation_setting@2026-10-20.1",
  version: 2,
  status: "pending",
  requested_at: "2026-10-20T01:00:00Z",
  requested_by: "someone-else",
  requested_by_name: "Ben Ong",
  expires_at: "2026-10-27T01:00:00Z",
  current_step: 1,
  steps: [{ quorum: 1, roles: ["owner"], users: [], approvals: 0 }],
  signatures: [],
  diff: [{ field: "Records kept for", before: "7 years", after: "10 years" }],
  envelope: "{}",
  envelope_hash: "cd".repeat(32),
  you_may_approve: true,
};
const signedIn = {
  "GET /v1/setup": { status: 200, body: { done: true } },
  "GET /v1/session": { status: 200, body: session },
  "GET /v1/organisation": { status: 200, body: organisation },
};

test("the Inbox opens a request, whose Approve opens the confirmation and decides nothing", async ({ page }) => {
  const seen = await api(page, {
    ...signedIn,
    "GET /v1/approvals": { status: 200, body: { items: [request] } },
    "GET /v1/review-log": { status: 200, body: { items: [], unreviewed: 0, overdue: 0 } },
    "GET /v1/approvals/a-1": { status: 200, body: request, headers: { ETag: '"2"' } },
  });
  await page.goto("/inbox");
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Inbox" })).toHaveAttribute("aria-current", "page");
  await page.getByRole("list", { name: "To approve" }).getByRole("link", { name: /Keep records for 10 years/ }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Keep records for 10 years" })).toBeVisible();
  await page.getByRole("button", { name: "Approve…" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("cd".repeat(32))).toBeVisible();
  await expect(dialog.getByRole("cell", { name: "10 years" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  expect(seen.filter((k) => k.startsWith("POST"))).toEqual([]);
});

test("a session that passed no second factor sees only how to confirm it's you", async ({ page }) => {
  const { step_up_method: _, ...single } = session;
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: single } });
  await page.goto("/settings/profile");
  await expect(page.getByRole("heading", { level: 1, name: "Confirm it's you" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in again" })).toHaveAttribute("href", "/api/auth/signin?provider=local&return_to=%2Fsettings%2Fprofile");
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
});

test("Basic mode hides Expert pages from the sidebar; the switch shows them", async ({ page }) => {
  await api(page, {
    ...signedIn,
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    "GET /v1/review-log": { status: 200, body: { items: [], unreviewed: 0, overdue: 0 } },
    "GET /v1/organisation/profile": { status: 403, body: { code: "API-AUTHZ-01", status: 403 } },
    "GET /v1/accounts": { status: 403, body: { code: "API-AUTHZ-01", status: 403 } },
    "GET /v1/contacts": { status: 200, body: { items: [] } },
    "GET /v1/members": { status: 200, body: { items: [] } },
    "GET /v1/members/invitations": { status: 200, body: { items: [] } },
    "GET /v1/session/passkeys": { status: 200, body: { items: [] } },
  });
  await page.goto("/");
  const side = page.getByRole("navigation", { name: "Main" });
  await expect(side.getByRole("link", { name: "Contacts" })).toBeVisible();
  await expect(side.getByRole("link", { name: "Accounting" })).toHaveCount(0);
  await page.getByRole("radio", { name: "Expert" }).click();
  await expect(side.getByRole("link", { name: "Accounting" })).toBeVisible();
});
