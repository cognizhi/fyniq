import { api, expect, expert, join, organisation, test } from "./harness";

// QA-E2E-01's set-up journey. The Owner of a new company with the sample books follows Home's set-up checklist: gives
// themself the Controller's role to keep the books (the Owner's own approves; Appendix D), enters the company's legal
// name and UEN in the organisation's profile, then invites a Bookkeeper, who signs in and joins with the code the Owner
// gives them. Each step shows on Home's checklist and in what the API returns.
// Scenarios: FR-UX-10 "Set-up checklist"; FR-ORG-01 "Profile details are recorded"; FR-ADM-01 "An invitation is a
// one-time code", "Roles and scopes" and "The Owner administers alone until a second member joins".
// req: QA-E2E-01
test("set-up: the Owner records the profile and invites a Bookkeeper, who joins", async ({ browser }) => {
  const j = await organisation(browser, "Journey Set-up Pte Ltd", [{ key: "owner", name: "Ann Owner", roles: ["owner"] }]);
  const { page } = j.people.owner!;
  const checklist = page.getByRole("list", { name: "Set up the company" });
  await expect(checklist.getByRole("link", { name: /legal name and UEN .* To do$/ })).toBeVisible();
  await expect(checklist.getByRole("link", { name: /Invite the other founders .* To do$/ })).toBeVisible();

  // The roles: the Owner approves; to keep the books they give themself the Controller's role, at once while alone.
  await expect(checklist.getByRole("link", { name: /Give yourself the roles .* To do$/ })).toBeVisible();
  await checklist.getByRole("link", { name: /Give yourself the roles/ }).click();
  await expect(page).toHaveURL(/\/settings\/members$/);
  await expert(page);
  await page.getByRole("button", { name: "Change roles: Ann Owner" }).click();
  const roles = page.getByRole("dialog", { name: "Ann Owner's roles" });
  await roles.getByLabel("Role to add").selectOption("controller");
  await roles.getByRole("button", { name: "Add role" }).click();
  await expect(roles.getByRole("list", { name: "Their roles" }).getByText("Controller")).toBeVisible();
  await roles.getByRole("button", { name: /Close|Done/ }).first().click();
  expect((await api<{ roles: string[] }>(page, "/v1/session")).roles.sort()).toEqual(["controller", "owner"]);

  // The profile: from the checklist's step to the organisation's page.
  await page.goto("/");
  await checklist.getByRole("link", { name: /legal name and UEN/ }).click();
  await expect(page).toHaveURL(/\/settings\/organisation$/);
  await page.getByRole("button", { name: "Edit the profile" }).click();
  await page.getByLabel("Legal name").fill("Journey Set-up Pte. Ltd.");
  await page.getByLabel("UEN", { exact: true }).fill("202612345K");
  await page.getByRole("button", { name: "Save the profile" }).click();
  const profile = page.getByRole("region", { name: "Profile" });
  await expect(profile.getByText("Journey Set-up Pte. Ltd.")).toBeVisible();
  const saved = await api<{ legal_name?: string; uen?: string }>(page, "/v1/organisation/profile");
  expect(saved).toMatchObject({ legal_name: "Journey Set-up Pte. Ltd.", uen: "202612345K" });

  // The invitation: a one-time code, shown once, for the Bookkeeper to accept.
  await page.goto("/settings/members");
  await page.getByRole("button", { name: "Invite", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Invite someone" });
  await dialog.getByLabel("Name").fill("Bea Bookkeeper");
  await dialog.getByRole("checkbox", { name: /^Bookkeeper/ }).check();
  await dialog.getByRole("button", { name: "Create invitation" }).click();
  const shown = page.getByRole("dialog", { name: "Give them this code" });
  const code = (await shown.locator("code").textContent())!.trim();
  expect(code.length).toBeGreaterThanOrEqual(20);
  const pending = await api<{ items: { display_name: string; roles: string[] }[] }>(page, "/v1/members/invitations");
  expect(pending.items).toEqual([expect.objectContaining({ display_name: "Bea Bookkeeper", roles: ["bookkeeper"] })]);
  await shown.getByRole("button", { name: /Done|Close/ }).first().click();

  const bea = await join(browser, `bea-${Date.now().toString(36)}`, "Bea Bookkeeper", code);
  const session = await api<{ organisation_id: string; roles: string[] }>(bea.page, "/v1/session");
  expect(session).toMatchObject({ organisation_id: j.organisation, roles: ["bookkeeper"] });

  // The Owner sees both on the members page and in the API, and Home's checklist has the two steps done.
  await page.reload();
  await expect(page.getByRole("list", { name: "People" }).getByText("Bea Bookkeeper")).toBeVisible();
  const members = await api<{ items: { display_name: string; roles: { role: string }[] }[] }>(page, "/v1/members");
  expect(members.items.map((m) => [m.display_name, m.roles.map((r) => r.role).sort()])).toEqual(
    expect.arrayContaining([
      ["Ann Owner", ["controller", "owner"]],
      ["Bea Bookkeeper", ["bookkeeper"]],
    ]),
  );
  await page.goto("/");
  await expect(checklist.getByRole("link", { name: /Give yourself the roles .* Done$/ })).toBeVisible();
  await expect(checklist.getByRole("link", { name: /legal name and UEN .* Done$/ })).toBeVisible();
  await expect(checklist.getByRole("link", { name: /Invite the other founders .* Done$/ })).toBeVisible();
});
