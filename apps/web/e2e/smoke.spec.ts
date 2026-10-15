import { expect, test } from "@playwright/test";

// req: DEV-01 (the web part of the local stack loads)
test("the web shell loads with the design tokens and no console errors", async ({ page }) => {
  const errors: string[] = [];
  page.on("console", (m) => {
    // The API may be down in this smoke test; its absence is reported on the page, not as an error.
    if (m.type() === "error" && !m.location().url.includes("/api/")) errors.push(`${m.text()} (${m.location().url})`);
  });
  page.on("pageerror", (e) => errors.push(e.message));
  // The API is down here whatever runs on the machine: the shell must still load and say so.
  await page.route("**/api/**", (route) => route.fulfill({ status: 503, body: "" }));
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "Fyniq" })).toBeVisible();
  await expect(page.getByRole("status")).not.toHaveText("Checking the API…");
  const background = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--background").trim());
  expect(background, "tokens.css should define --background").not.toBe("");
  expect(errors).toEqual([]);
});
