import { attach } from "./books";
import { api, expect, expert, organisation, test } from "./harness";

interface Period {
  id: string;
  starts_on: string;
  status: string;
}

// QA-E2E-01's close journey. January 2026 closes with two people: the Accountant runs the month's checks, recording
// the ledger as they find it, and the founder who is the Controller, who did not run them, attests the figures and
// closes the month. Nothing posts into it afterwards: a journal dated in January is refused, on the screen and in the
// API.
// Scenarios: FR-GL-08 "Posting into a closed period is refused"; the close checklist, a task skipped with its reason
// (rev 0.68), and the month attested by a founder who did not run the checks.
// req: QA-E2E-01
test("close: the Accountant runs January's checks, the Controller attests and closes it, and it takes no posting", async ({ browser }) => {
  const j = await organisation(browser, "Journey Close Pte Ltd", [
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
    { key: "owner", name: "Ann Owner", roles: ["owner", "controller"] },
  ]);
  const cal = j.people.accountant!.page;
  const periods = await api<{ items: Period[] }>(cal, "/v1/periods?page_size=50");
  const january = periods.items.find((p) => p.starts_on === "2026-01-01")!;
  expect(january.status).toBe("open");

  await cal.goto("/accounting/close");
  await expert(cal);
  await cal.getByRole("link", { name: "January 2026" }).click();
  await expect(cal).toHaveURL(new RegExp(`/accounting/close/${january.id}$`));
  // The checklist: each task done, but payroll's, which does not apply (payroll comes after go-live), skipped with why.
  const checklist = cal.getByRole("list", { name: "Checklist" });
  for (const task of ["Sales, purchases and expense claims entered", "Bank, card and wallet accounts reconciled", "Accruals and prepayments reviewed", "Foreign-currency balances revalued", "The month reviewed"]) {
    await checklist.getByRole("button", { name: `Done: ${task}` }).click();
    await expect(checklist.getByRole("button", { name: `Open again: ${task}` })).toBeVisible();
  }
  await checklist.getByRole("button", { name: "Skip: Payroll and CPF posted" }).click();
  const skip = cal.getByRole("dialog", { name: 'Skip "Payroll and CPF posted" this month?' });
  await skip.getByLabel("Why it does not apply this month").fill("No employees on the payroll yet");
  await skip.getByRole("button", { name: /^Skip/ }).click();
  await expect(cal.getByText("6 of 6 tasks done or skipped")).toBeVisible();
  await cal.getByRole("button", { name: "Run the checks" }).click();
  await expect(cal.getByText(/^Checks run by Cal Accountant/)).toBeVisible();
  await expect(cal.getByText("The checks found something that blocks the close.")).toHaveCount(0);

  const ann = j.people.owner!.page;
  await ann.goto(`/accounting/close/${january.id}`);
  await expert(ann);
  await ann.getByLabel("What you attest").fill("January 2026's figures are complete and correct, as the checks show.");
  await ann.getByRole("button", { name: "Attest and close" }).click();
  await expect(ann.getByRole("status").filter({ hasText: "January 2026 is closed." })).toBeVisible();
  const listed = await api<{ items: Period[] }>(ann, "/v1/periods?page_size=50");
  const one = await api<Period>(ann, `/v1/periods/${january.id}`);
  expect([listed.items.find((p) => p.id === january.id)!.status, one.status]).toEqual(["closed", "closed"]);

  // A journal dated in January stays a draft: posting it into the closed month is refused.
  await cal.goto("/accounting/journals/new");
  await cal.getByLabel("Date", { exact: true }).fill("2026-01-20");
  await cal.getByLabel("Description", { exact: true }).fill("A January correction, after the close");
  await cal.getByLabel("Line 1: Account").selectOption({ label: "6510 Accounting fees" });
  await cal.getByLabel("Line 1: Debit").fill("100.00");
  await cal.getByLabel("Line 2: Account").selectOption({ label: "2050 Accrued expenses" });
  await cal.getByLabel("Line 2: Credit").fill("100.00");
  await cal.getByRole("button", { name: "Save as a draft" }).click();
  await expect(cal).toHaveURL(/\/accounting\/journals\/[0-9a-f-]{36}$/);
  const draft = cal.url().split("/").pop()!;
  await attach(cal, "correction.pdf", "calculation", "A January correction");
  await cal.getByRole("button", { name: "Post", exact: true }).click();
  await expect(cal.getByRole("alert")).toContainText(/closed/i);
  expect((await api<{ status: string }>(cal, `/v1/journals/${draft}`)).status).toBe("draft");
});
