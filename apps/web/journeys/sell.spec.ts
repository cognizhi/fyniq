import { api, contact, expect, organisation, sgd, test, type Money } from "./harness";

interface Invoice {
  id: string;
  number?: string;
  status: string;
  customer: { id: string };
  net: Money;
  gst: Money;
  total: Money;
  balance: Money;
  gst_charged: boolean;
}

// QA-E2E-01's sell journey. A Bookkeeper adds a customer and prepares an invoice of two lines as a draft. The
// Accountant, who issues invoices (the Bookkeeper prepares them), finds its total above the self-approval threshold and
// asks for approval; the founder who is the Controller, who approves invoices (Appendix D), approves it in the Inbox,
// and it is issued. It takes the next number, and what the customer owes is its total, on the screen and in the API.
// Scenarios: FR-AR-03 "Invoice lines", "Approval state", "Ledger state" and "Invoice API and approvals as built (rev
// 0.117)"; FR-ORG-04 "A gap-free sequence numbers a document when it posts"; FR-ORG-03 "GST status follows its
// effective dates"; FR-APR-01 "Conditions decide the steps"; FR-APR-02 "What approving would post now".
// req: QA-E2E-01
test("sell: an invoice prepared, asked to issue, approved by the Controller; the customer owes it", async ({ browser }) => {
  const j = await organisation(browser, "Journey Sell Pte Ltd", [
    { key: "bookkeeper", name: "Bea Bookkeeper", roles: ["bookkeeper"] },
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
    { key: "owner", name: "Ann Owner", roles: ["owner", "controller"] },
  ]);
  const { page } = j.people.bookkeeper!;
  const customer = await contact(page, "Harbourline Logistics Pte Ltd", { customer: true });

  await page.goto("/sales/invoices/new");
  await page.getByLabel("Customer", { exact: true }).selectOption({ label: "Harbourline Logistics Pte Ltd" });
  await page.getByLabel("Customer's reference").fill("PO-7781");
  const first = page.getByRole("group", { name: "Line 1" });
  await first.getByLabel("Description").fill("Implementation services, November");
  await first.getByLabel("Quantity").fill("2");
  await first.getByLabel(/^Unit price before GST/).fill("1500.00");
  await first.getByLabel("Revenue account").selectOption({ label: "4020 Services revenue" });
  await first.getByLabel("GST code").selectOption("SR");
  await page.getByRole("button", { name: "Add a line" }).click();
  const second = page.getByRole("group", { name: "Line 2" });
  await second.getByLabel("Description").fill("Subscription, November");
  await second.getByLabel(/^Unit price before GST/).fill("249.50");
  await second.getByLabel("Revenue account").selectOption({ label: "4010 Subscription revenue" });
  await second.getByLabel("GST code").selectOption("SR");
  await page.getByRole("button", { name: "Save as a draft" }).click();

  await expect(page).toHaveURL(/\/sales\/invoices\/[0-9a-f-]{36}$/);
  const id = page.url().split("/").pop()!;
  const draft = await api<Invoice>(page, `/v1/invoices/${id}`);
  expect(draft).toMatchObject({ status: "draft", customer: { id: customer }, net: sgd("3249.50"), gst: sgd("0.00"), total: sgd("3249.50"), gst_charged: false });
  expect(draft.number).toBeUndefined();
  await expect(page.getByText("This invoice adds SGD 3,249.50 to what Harbourline Logistics Pte Ltd owes.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Issue", exact: true })).toHaveCount(0); // the preparer does not issue

  const cal = j.people.accountant!.page;
  await cal.goto("/sales/invoices");
  await cal.getByRole("button", { name: "Drafts" }).click();
  const row = cal.getByRole("table", { name: "Invoices" }).getByRole("row", { name: /Harbourline Logistics Pte Ltd PO-7781/ });
  await expect(row.getByRole("cell", { name: "3,249.50 SGD" })).toBeVisible();
  await row.getByRole("link", { name: "Draft" }).click();
  await expect(cal).toHaveURL(new RegExp(`/sales/invoices/${id}$`));
  await cal.getByRole("button", { name: "Issue", exact: true }).click();
  await expect(cal.getByText("above the self-approval threshold of 2000.00")).toBeVisible();
  await cal.getByRole("button", { name: "Ask for approval" }).click();
  await expect(cal.getByText("Asked for approval: it is issued when another person approves it.")).toBeVisible();
  expect(await api<Invoice>(cal, `/v1/invoices/${id}`)).toMatchObject({ status: "draft" });

  const ann = j.people.owner!.page;
  await ann.goto("/inbox");
  await ann.getByRole("link", { name: /Harbourline Logistics Pte Ltd/ }).first().click();
  await expect(ann).toHaveURL(/\/approvals\/[0-9a-f-]{36}$/);
  await ann.getByRole("button", { name: "Approve…" }).click();
  await ann.getByRole("dialog").getByRole("button", { name: /^Approve/ }).click();
  await expect(ann.getByText("Approved: the change is made.")).toBeVisible();

  const issued = await api<Invoice>(cal, `/v1/invoices/${id}`);
  expect(issued).toMatchObject({ status: "posted", number: "INV-0001", total: sgd("3249.50"), balance: sgd("3249.50") });
  await cal.reload();
  await expect(cal.getByRole("heading", { level: 1 })).toContainText("INV-0001");

  // What the customer owes: their record shows the invoice and its total, as the API's does.
  await cal.goto(`/contacts/${customer}`);
  await expect(cal.getByText("INV-0001").first()).toBeVisible();
  await expect(cal.getByText(/3,249\.50/).first()).toBeVisible();
});
