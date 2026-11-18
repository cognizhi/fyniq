import { enterBill, postBill, recordPayment } from "./books";
import { api, expect, organisation, sgd, test, type Money } from "./harness";

interface Bill {
  status: string;
  open: Money;
  payments?: { payment_number?: string; amount: Money; voided: boolean }[];
}

// QA-E2E-01's pay journey. A posted bill is paid from the operating account: the Accountant records the payment that
// left the bank, the bill is paid in full, and the payment takes its own number, on the screen and in the API.
// Scenarios: FR-AP-02 "Built in part: payments recorded by hand (rev 0.77)" and "Payment state from allocations (rev
// 0.77)".
// req: QA-E2E-01
test("pay: the Accountant records the payment of a posted bill, which is then paid in full", async ({ browser }) => {
  const j = await organisation(browser, "Journey Pay Pte Ltd", [
    { key: "bookkeeper", name: "Bea Bookkeeper", roles: ["bookkeeper"] },
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
  ]);
  const { bill } = await enterBill(j.people.bookkeeper!.page, "Lumen Print House Pte Ltd", "LPH-0931", [
    { account: "6140 Printing and stationery", description: "Business cards and letterheads", amount: "642.80" },
  ]);
  const cal = j.people.accountant!.page;
  await postBill(cal, "Lumen Print House Pte Ltd", "LPH-0931");

  await recordPayment(cal, "1020 OCBC Business Growth", "642.80", "OCBC FAST 7781");

  const paid = await api<Bill>(cal, `/v1/bills/${bill}`);
  expect(paid.open).toEqual(sgd("0.00"));
  expect(paid.payments).toEqual([expect.objectContaining({ payment_number: "PAY-0001", amount: sgd("642.80"), voided: false })]);
  await cal.reload();
  await expect(cal.getByText("PAY-0001").first()).toBeVisible();
  await expect(cal.getByText(/^Paid$/).first()).toBeVisible();
});
