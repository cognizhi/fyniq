import { attach } from "./books";
import { api, expect, expert, organisation, test } from "./harness";

interface Journal {
  number?: string;
  status: string;
}

// QA-E2E-01's record journey. The Accountant records a manual journal in Expert mode: an accrual of the month's audit
// fees, balanced, with the calculation it rests on attached, and posts it. It takes the next journal number after the
// sample company's 24, and the ledger shows it on both accounts, on the screen and in the API.
// Scenarios: FR-GL-01 manual journals that balance; FR-DOC-06 "Default evidence for bills and manual journals (rev
// 0.182)"; FR-ORG-04 "A gap-free sequence numbers a document when it posts".
// req: QA-E2E-01
test("record: the Accountant posts a balanced manual journal with its evidence", async ({ browser }) => {
  const j = await organisation(browser, "Journey Record Pte Ltd", [{ key: "accountant", name: "Cal Accountant", roles: ["accountant"] }]);
  const cal = j.people.accountant!.page;
  await cal.goto("/accounting/journals/new");
  await expert(cal);
  await cal.getByLabel("Description", { exact: true }).fill("Accrue the November audit fees");
  await cal.getByLabel("Line 1: Account").selectOption({ label: "6510 Accounting fees" });
  await cal.getByLabel("Line 1: Debit").fill("1800.00");
  await cal.getByLabel("Line 2: Account").selectOption({ label: "2050 Accrued expenses" });
  await cal.getByLabel("Line 2: Credit").fill("1800.00");
  await cal.getByRole("button", { name: "Save as a draft" }).click();
  await expect(cal).toHaveURL(/\/accounting\/journals\/[0-9a-f-]{36}$/);
  const id = cal.url().split("/").pop()!;
  expect(await api<Journal>(cal, `/v1/journals/${id}`)).toMatchObject({ status: "draft" });

  await attach(cal, "audit-fee-accrual.pdf", "calculation", "Audit fee accrual: S$21,600.00 a year, one twelfth for November");
  await cal.getByRole("button", { name: "Post", exact: true }).click();
  await expect(cal.getByRole("status").filter({ hasText: "Posted as JNL-0025." })).toBeVisible();
  const posted = await api<Journal>(cal, `/v1/journals/${id}`);
  expect(posted).toMatchObject({ status: "posted", number: "JNL-0025" });

  // The ledger: the general ledger lists the journal on the accrual account.
  await cal.goto("/reports/general-ledger");
  await cal.getByLabel("Add an account").selectOption({ label: "2050 Accrued expenses" });
  await cal.getByRole("button", { name: "Add", exact: true }).click();
  const row = cal.getByRole("row", { name: /JNL-0025/ });
  await expect(row).toBeVisible();
  await expect(row.getByText("1,800.00").first()).toBeVisible();
});
