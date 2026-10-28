import { expect, test, type Page, type Route } from "@playwright/test";

// Functional tests of the P0 screens (sign-in, first-run setup) against the built web app, with every
// /api call answered by a fixture: the screens' behaviour, not the API's (the Go suites prove that).
// req: FR-ADM-06
// req: FR-ADM-08
// req: SEC-08

type Answer = { status: number; body?: unknown };
type Handler = Answer | ((body: unknown) => Answer);

async function api(page: Page, handlers: Record<string, Handler>, seen: { method: string; path: string; body: unknown }[] = []) {
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api/, "");
    const key = `${req.method()} ${path}`;
    const body = req.postData() ? JSON.parse(req.postData() as string) : undefined;
    seen.push({ method: req.method(), path, body });
    const h = handlers[key];
    if (!h) return route.fulfill({ status: 599, body: `no fixture for ${key}` });
    const a = typeof h === "function" ? h(body) : h;
    await route.fulfill({
      status: a.status,
      contentType: a.status >= 400 ? "application/problem+json" : "application/json",
      body: a.body === undefined ? "" : JSON.stringify(a.body),
    });
  });
  return seen;
}

const local = { items: [{ id: "local", name: "Local sign-in (development)" }] };
const unauthenticated = { status: 401, body: { code: "API-AUTH-01", status: 401, title: "Unauthorized" } };
const newIdentity = { provider: "local", roles: [], super_admin: false };
const codes = Array.from({ length: 10 }, (_, i) => `CODE${i}-ABCDE-FGHIJ-KLMNO-PQRS`);

test("sign-in lists the providers and keeps the return address", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": unauthenticated, "GET /v1/sign-in-providers": { status: 200, body: local } });
  await page.goto("/sign-in?return_to=%2Fapi%2Fauth%2Flogin%3Flogin_challenge%3Dabc");
  const link = page.getByRole("link", { name: "Continue with Local sign-in (development)" });
  await expect(link).toHaveAttribute("href", "/api/auth/signin?provider=local&return_to=%2Fapi%2Fauth%2Flogin%3Flogin_challenge%3Dabc");
  await expect(page.getByRole("heading", { level: 1, name: "Sign in to Fyniq" })).toBeVisible();
});

test("sign-in never sends the browser off the origin", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": unauthenticated, "GET /v1/sign-in-providers": { status: 200, body: local } });
  await page.goto("/sign-in?return_to=https%3A%2F%2Fevil.test%2F");
  await expect(page.getByRole("link", { name: /Continue with/ })).toHaveAttribute("href", "/api/auth/signin?provider=local&return_to=%2F");
});

for (const [code, words] of [
  ["AUTH-SIGNIN-02", "began in another browser"],
  ["AUTH-SIGNIN-03", "did not pass Fyniq's checks"],
] as const) {
  test(`a refused sign-in (${code}) is explained, and takes focus`, async ({ page }) => {
    await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": unauthenticated, "GET /v1/sign-in-providers": { status: 200, body: local } });
    await page.goto(`/sign-in?signin_error=${code}`);
    const alert = page.getByRole("alert");
    await expect(alert).toContainText(code);
    await expect(alert).toContainText(words);
    await expect(alert).toBeFocused();
  });
}

test("home sends a signed-out browser to sign in, with what the server sent", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": unauthenticated, "GET /v1/sign-in-providers": { status: 200, body: local } });
  await page.goto("/?signin_error=AUTH-SIGNIN-02&return_to=%2Fjournals");
  await expect(page).toHaveURL(/\/sign-in\?signin_error=AUTH-SIGNIN-02&return_to=%2Fjournals$/);
});

test("a new installation opens the setup page", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: false } }, "GET /v1/session": unauthenticated, "GET /v1/sign-in-providers": { status: 200, body: local } });
  await page.goto("/");
  await expect(page).toHaveURL(/\/install$/);
  await expect(page.getByRole("heading", { level: 1, name: "Set up Fyniq" })).toBeVisible();
  await expect(page.getByRole("link", { name: /Continue with Local/ })).toHaveAttribute("href", "/api/auth/signin?provider=local&return_to=%2Finstall");
});

test("first-run setup: the code, the company, the recovery codes once, then a second Super admin", async ({ page }) => {
  const seen = await api(page, {
    "GET /v1/setup": { status: 200, body: { done: false } },
    "GET /v1/session": { status: 200, body: newIdentity },
    "POST /v1/setup": { status: 201, body: { user_id: "u-1", organisation_id: "o-1", installation_organisation_id: "i-1", recovery_codes: codes } },
    "POST /v1/super-admins/invitations": { status: 403, body: { code: "API-AUTHZ-05", status: 403, detail: "Step-up required" } },
  });
  await page.goto("/install");
  await page.getByLabel("Your name").fill("Mei Ling Tan");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("One-time setup code").fill("ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Company name").fill("Cognizhi Pte Ltd");
  await page.getByRole("button", { name: "Create organisation" }).click();

  const setup = seen.find((r) => r.method === "POST" && r.path === "/v1/setup");
  expect(setup?.body).toEqual({ setup_code: "ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z", display_name: "Mei Ling Tan", organisation: { name: "Cognizhi Pte Ltd", base_currency: "SGD" } });
  await expect(page.getByRole("list", { name: "Recovery codes" }).getByRole("listitem")).toHaveCount(10);
  const go = page.getByRole("button", { name: "Continue" });
  await expect(go).toBeDisabled();
  await page.getByLabel("I have kept these codes somewhere safe, offline.").check();
  await go.click();

  await page.getByRole("button", { name: "Create an invitation" }).click();
  await expect(page.getByRole("link", { name: "Sign in again" })).toHaveAttribute("href", "/api/auth/signin?provider=local&return_to=%2Finstall%3Fstep%3Dsecond-admin");
  await page.getByRole("button", { name: "Later" }).click();
  await expect(page.getByText("This page is closed for good")).toBeVisible();

  // Nothing secret left the page's memory: not in storage, not in the address bar (SEC-15).
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }) + document.cookie);
  expect(stored).not.toContain("ABCDE");
  expect(stored).not.toContain("CODE0");
  expect(page.url()).not.toContain("ABCDE");
});

test("a wrong setup code sends the person back to it, with the reason in focus", async ({ page }) => {
  await api(page, {
    "GET /v1/setup": { status: 200, body: { done: false } },
    "GET /v1/session": { status: 200, body: newIdentity },
    "POST /v1/setup": { status: 409, body: { code: "API-SETUP-01", status: 409, detail: "The setup code is not valid." } },
  });
  await page.goto("/install");
  await page.getByLabel("Your name").fill("Mei Ling Tan");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("One-time setup code").fill("WRONG-WRONG-WRONG-WRONG-WRONG");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Company name").fill("Cognizhi Pte Ltd");
  await page.getByRole("button", { name: "Create organisation" }).click();
  await expect(page.getByRole("alert")).toBeFocused();
  await expect(page.getByRole("alert")).toContainText("setup code isn't right");
  await expect(page.getByLabel("One-time setup code")).toHaveAttribute("aria-invalid", "true");
});

test("after setup, the setup page is closed for good", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": unauthenticated });
  await page.goto("/install");
  await expect(page.getByRole("heading", { level: 1, name: "This installation is set up" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/sign-in");
});

test("a Super admin back from a step-up gets the invitation code", async ({ page }) => {
  await api(page, {
    "GET /v1/setup": { status: 200, body: { done: true } },
    "GET /v1/session": { status: 200, body: { ...newIdentity, user_id: "u-1", super_admin: true, stepped_up_until: "2026-10-15T09:05:00Z" } },
    "POST /v1/super-admins/invitations": { status: 201, body: { code: "INVIT-ABCDE-FGHIJ-KLMNO-PQRST", expires_at: "2026-10-22T09:00:00Z" } },
  });
  await page.goto("/install?step=second-admin");
  await page.getByRole("button", { name: "Create an invitation" }).click();
  await expect(page.getByLabel("Invitation code")).toHaveText("INVIT-ABCDE-FGHIJ-KLMNO-PQRST");
});

test("someone signed in without an account joins by invitation and keeps their codes", async ({ page }) => {
  const seen = await api(page, {
    "GET /v1/setup": { status: 200, body: { done: true } },
    "GET /v1/session": { status: 200, body: newIdentity },
    // A Super admin's code is no company's invitation: the company's endpoint refuses it first.
    "POST /v1/members": { status: 409, body: { code: "API-SUPERADMIN-01", status: 409 } },
    "POST /v1/super-admins": ({ invitation_code }: any) =>
      invitation_code === "INVIT-ABCDE-FGHIJ-KLMNO-PQRST"
        ? { status: 201, body: { user_id: "u-2", recovery_codes: codes } }
        : { status: 409, body: { code: "API-SUPERADMIN-01", status: 409 } },
  });
  await page.goto("/sign-in");
  await expect(page.getByRole("heading", { name: "Joining by invitation" })).toBeVisible();
  await page.getByLabel("Invitation code").fill("WRONG-ABCDE-FGHIJ-KLMNO-PQRST");
  await page.getByLabel("Your name").fill("Wei Jie Lim");
  await page.getByRole("button", { name: "Accept the invitation" }).click();
  await expect(page.getByRole("alert")).toContainText("not valid, was used already or has expired");
  await page.getByLabel("Invitation code").fill("INVIT-ABCDE-FGHIJ-KLMNO-PQRST");
  await page.getByRole("button", { name: "Accept the invitation" }).click();
  await expect(page.getByRole("list", { name: "Recovery codes" }).getByRole("listitem")).toHaveCount(10);
  expect(seen.filter((r) => r.path === "/v1/super-admins")).toHaveLength(2);
  expect(seen.filter((r) => r.path === "/v1/members")).toHaveLength(2);
});

test("home sends someone signed in without an account to the invitation", async ({ page }) => {
  await api(page, { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: newIdentity } });
  await page.goto("/");
  await expect(page).toHaveURL(/\/sign-in$/);
  await expect(page.getByRole("heading", { name: "Joining by invitation" })).toBeVisible();
});
