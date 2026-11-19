import { expect, test } from "@playwright/test";

// req: NM-ARC-01, NM-UX-02
// The sign-in journey with no Fyniq running: a fresh installation says it is not set up; the person who installs it sets it up with the one-time
// code and the local sign-in page, lands on People home as the Owner, signs out, and signing back in (with the provider only) lands there again.
// The setup code comes from `make people-setup-code`, passed in NEMURA_SETUP_CODE.
const code = process.env.NEMURA_SETUP_CODE ?? "";

test("a fresh installation is set up with its one-time code, then the Owner signs in and out", async ({ page }) => {
  test.skip(!code, "NEMURA_SETUP_CODE is not set: run scripts/test-journeys-people.sh");

  await page.goto("/");
  await expect(page).toHaveURL(/\/sign-in$/);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expect(page.getByText(/not set up yet/)).toBeVisible();
  await expect(page.getByRole("link", { name: /Continue with/ })).toHaveCount(0);

  // A wrong code is refused in plain words and sets nothing up.
  await page.getByRole("link", { name: "Set up Nemura" }).click();
  await page.getByLabel("Setup code").fill("not-the-code");
  await page.getByLabel("Organisation name").fill("Journey Pte Ltd");
  await page.getByLabel("Your name").fill("Jo Journey");
  await page.getByRole("button", { name: "Continue to sign-in" }).click();
  await page.locator("#subject").fill("jo-journey");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/sign-in\?signin_error=NM_SIGNIN_08$/);
  await expect(page.getByRole("alert")).toContainText("wrong, was used already or has expired");

  // The right code makes the first organisation and its Owner.
  await page.getByRole("link", { name: "Set up Nemura" }).click();
  await page.getByLabel("Setup code").fill(code);
  await page.getByLabel("Organisation name").fill("Journey Pte Ltd");
  await page.getByLabel("Your name").fill("Jo Journey");
  await page.getByRole("button", { name: "Continue to sign-in" }).click();
  await page.locator("#subject").fill("jo-journey");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/people$/);
  await expect(page.getByRole("heading", { name: "People home" })).toBeVisible();
  await expect(page.getByText("Owner", { exact: true })).toBeVisible();
  const session = await page.request.get("/api/v1/session");
  expect(session.ok()).toBe(true);
  expect(await session.json()).toMatchObject({ roles: ["owner"], landing: "/people" });

  // Signing out ends the session.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/$|\/sign-in$/);
  expect((await page.request.get("/api/v1/session")).status()).toBe(401);

  // Signing in again needs only the provider: setup is done, so the page no longer offers it.
  await page.goto("/sign-in");
  await expect(page.getByRole("link", { name: "Set up Nemura" })).toHaveCount(0);
  await page.getByRole("link", { name: /Continue with/ }).click();
  await page.locator("#subject").fill("jo-journey");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/people$/);
  await expect(page.getByRole("heading", { name: "People home" })).toBeVisible();
});
