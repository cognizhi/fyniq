import { expect, test } from "@playwright/test";
import { api, organisation } from "./harness";

// The harness's own journey (p1-launch-readiness task 5.1): the Owner of a fresh organisation with the sample company
// signs in through the local sign-in page and lands in it, and the screen and the API agree on the organisation and on
// its chart, which the sample loaded.
test("the Owner of a journey's organisation lands in it, and the screen and the API agree", async ({ browser }) => {
  const j = await organisation(browser, "Journey Smoke Pte Ltd", [{ key: "owner", name: "Ann Owner", roles: ["owner"] }]);
  const { page } = j.people.owner!;
  const session = await api<{ organisation_id: string; roles: string[] }>(page, "/v1/session");
  expect(session.organisation_id).toBe(j.organisation);
  expect(session.roles).toContain("owner");
  const org = await api<{ name: string }>(page, "/v1/organisation");
  expect(org.name).toMatch(/^Journey Smoke Pte Ltd/);
  await expect(page.getByText(org.name).first()).toBeVisible();

  const chart = await api<{ items: { code: string; name: string }[] }>(page, "/v1/accounts?page_size=500");
  expect(chart.items.length).toBeGreaterThan(20);
  await page.goto("/accounting/chart-of-accounts");
  const first = chart.items[0]!;
  await expect(page.getByText(first.name).first()).toBeVisible();
});
