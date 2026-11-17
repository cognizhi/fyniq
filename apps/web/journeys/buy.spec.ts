import { enterBill, postBill } from "./books";
import { api, expect, organisation, sgd, test, type Money } from "./harness";

interface Bill {
  number?: string;
  status: string;
  supplier: { id: string };
  total: Money;
  open: Money;
}

// QA-E2E-01's buy journey. A Bookkeeper adds a supplier, enters their bill of two lines as their invoice states them,
// and attaches the invoice; the Accountant, who posts bills (the Bookkeeper prepares them), posts it. It takes the next
// number, and what the company owes the supplier is its total, on the screen and in the API.
// Scenarios: FR-AP-02 "Bill lines", "Attachments" and "Built in part: the bill and its approval (rev 0.76)"; FR-DOC-06
// "Default evidence for bills and manual journals (rev 0.182)"; FR-ORG-04 "A gap-free sequence numbers a document when
// it posts".
// req: QA-E2E-01
test("buy: a bill entered by the Bookkeeper and posted by the Accountant is owed to the supplier", async ({ browser }) => {
  const j = await organisation(browser, "Journey Buy Pte Ltd", [
    { key: "bookkeeper", name: "Bea Bookkeeper", roles: ["bookkeeper"] },
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
  ]);
  const bea = j.people.bookkeeper!.page;
  const { bill, supplier } = await enterBill(bea, "Kopi Cloud Services Pte Ltd", "KCS-2026-114", [
    { account: "6135 SaaS tools", description: "Team plan, November", amount: "1200.00" },
    { account: "5010 Cloud hosting", description: "Compute, November", amount: "386.40" },
  ]);
  const draft = await api<Bill>(bea, `/v1/bills/${bill}`);
  expect(draft).toMatchObject({ status: "draft", supplier: { id: supplier }, total: sgd("1586.40") });
  expect(draft.number).toBeUndefined();
  await expect(bea.getByRole("button", { name: "Post", exact: true })).toHaveCount(0); // the preparer does not post

  const cal = j.people.accountant!.page;
  expect(await postBill(cal, "Kopi Cloud Services Pte Ltd", "KCS-2026-114")).toBe("BILL-0001");
  const posted = await api<Bill>(cal, `/v1/bills/${bill}`);
  expect(posted).toMatchObject({ status: "posted", number: "BILL-0001", total: sgd("1586.40"), open: sgd("1586.40") });

  // What the company owes the supplier: their record shows the bill and its total.
  await cal.goto(`/contacts/${supplier}`);
  await expect(cal.getByText("BILL-0001").first()).toBeVisible();
  await expect(cal.getByText(/1,586\.40/).first()).toBeVisible();
});
