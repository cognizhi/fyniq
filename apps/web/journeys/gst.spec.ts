import { attach } from "./books";
import { api, expect, expert, organisation, test } from "./harness";

interface Calendar {
  deadlines: { key: string; area: string; title: string; due_on?: string; status: string }[];
}

// QA-E2E-01's GST journey, as far as the launch goes: the registration monitor and the statutory calendar (the F5
// return waits for the company's GST registration, the owner's decision, after go-live). The Controller sets the
// financial year, so the calendar works out the company's deadlines. The Accountant records a forecast of the next 12
// months' taxable supplies above IRAS's threshold, with the signed contract it rests on: the monitor then says the
// company must register, and the calendar gains the registration's deadline, on the screen and in the API.
// Scenarios: FR-TAX-05 "Prospective test" and "A forecast with its evidence (rev 0.208)"; FR-TAX-07 "Obligations
// covered" and "Dates follow the FY and GST periods"; CMP-02 "Calendar follows the organisation's settings"; FR-ORG-02
// "Books start date and time zone are recorded".
// req: QA-E2E-01
test("GST: a forecast above the threshold makes the company liable to register, and the calendar says by when", async ({ browser }) => {
  const j = await organisation(browser, "Journey GST Pte Ltd", [
    { key: "owner", name: "Ann Owner", roles: ["owner", "controller"] },
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
  ]);
  const ann = j.people.owner!.page;
  await ann.goto("/settings/organisation");
  await expert(ann);
  await ann.getByRole("button", { name: "Edit the profile" }).click();
  await ann.getByLabel("Books start").fill("2026-01-01");
  await ann.getByLabel("First financial year ends").fill("2026-12-31");
  await ann.getByRole("button", { name: "Save the profile" }).click();
  await expect(ann.getByRole("region", { name: "Profile" }).getByText("1 Jan 2026")).toBeVisible();

  const before = await api<Calendar>(ann, "/v1/compliance/calendar");
  expect(before.deadlines.length).toBeGreaterThan(0);
  expect(before.deadlines.some((d) => /^Apply to register for GST/.test(d.title))).toBe(false);
  await ann.goto("/compliance/calendar");
  await expect(ann.getByText(before.deadlines[0]!.title).first()).toBeVisible();

  const cal = j.people.accountant!.page;
  await cal.goto("/compliance/gst");
  const monitor = cal.getByRole("region", { name: "Registration monitor" });
  await expect(monitor.getByText("Below the threshold")).toBeVisible();
  await cal.getByRole("button", { name: "Record a forecast" }).click();
  const dialog = cal.getByRole("dialog", { name: "Record a forecast" });
  await dialog.getByLabel("Day of the forecast").fill(new Date().toLocaleDateString("en-CA", { timeZone: "Asia/Singapore" }));
  await dialog.getByLabel("Taxable supplies expected, S$").fill("1250000.00");
  await dialog.getByLabel("What it rests on").fill("Signed contracts with three customers for the next 12 months");
  await dialog.getByRole("button", { name: "Save the forecast" }).click();
  await expect(dialog).toBeHidden();
  await attach(cal, "contracts.pdf", "Signed contract", "Signed contracts, S$1,250,000.00 over 12 months");
  await cal.getByRole("button", { name: "Record", exact: true }).click();
  await expect(monitor.getByText("Must register")).toBeVisible();
  const m = await api<{ status: string }>(cal, "/v1/compliance/gst-monitor");
  expect(m.status).toBe("liable");

  const after = await api<Calendar>(ann, "/v1/compliance/calendar");
  const register = after.deadlines.find((d) => /^Apply to register for GST/.test(d.title));
  expect(register, JSON.stringify(after.deadlines.map((d) => d.title))).toBeDefined();
  await ann.reload();
  await expect(ann.getByText(register!.title).first()).toBeVisible();
});
