import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { SalaryPayment, SalaryPayments } from "./SalaryPayments";

const payroll = { ...ownerSession, roles: ["payroll_administrator"] } as Session;
const releaser = { ...ownerSession, roles: ["payment_releaser"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const base = {
  id: "s1", number: "SB-0003", run_id: "r1", run_version: 1, wage_month: "2026-10", status: "approved" as const, state: "approved" as const, money_account_id: "a1", payment_date: "2026-10-31",
  method: "giro" as const, line_count: 3, total: sgd("26000.43"), released: 0, paid: 0, waiting: 1, reapproval_needed: false, approval_id: "q1", releases: [], lines_visible: false, checks: [],
  version: 2, lines_version: 1,
};
const lines = [
  { line_id: "l1", person_id: "p1", person_name: "Ana Lim", amount: sgd("9000.11"), account: "DBSSSGSG ••••6789", bank_detail_version: 1, line_version: 1, state: "scheduled" as const, waiting: "held" as const },
  { line_id: "l2", person_id: "p2", person_name: "Cai Yu", amount: sgd("7000.33"), account: "OCBCSGSG ••••3307", bank_detail_version: 2, line_version: 1, state: "released" as const, instruction_id: "SB0003-R1-L01" },
];

const page = (batch: unknown, session: Session) => {
  const seen = answer({ "GET /v1/salary-batches/s1": { status: 200, body: batch, headers: { ETag: '"2-approved"' } } });
  renderPage(SalaryPayment, { path: "/payroll/salary-payments/s1", route: "/payroll/salary-payments/$batchId", session });
  return seen;
};

// req: FR-PAY-11
// part of FR-PPL-17 (the screens; exports and search are the API's)
describe("salary payments", () => {
  it("lists the batches with their state, people and total, and no name; axe finds nothing", async () => {
    answer({ "GET /v1/salary-batches": { status: 200, body: { items: [base, { ...base, id: "s2", number: "SB-0002", state: "paid", wage_month: "2026-09", waiting: 0, paid: 3 }] } } });
    renderPage(SalaryPayments, { path: "/payroll/salary-payments", session: payroll });
    const table = await screen.findByRole("table", { name: "Salary batches" });
    expect(within(table).getByText("Approved, ready to release")).toBeTruthy();
    expect(within(table).getByText("Paid")).toBeTruthy();
    expect(within(table).getByText("3, 1 waiting")).toBeTruthy();
    expect(screen.queryByText(/Ana/)).toBeNull();
    await noViolations(document.body);
  });

  it("shows the lines, the account masked and why each waits, to the Payroll administrator", async () => {
    page({ ...base, state: "released_in_part", released: 1, lines_visible: true, lines }, payroll);
    const table = await screen.findByRole("table", { name: "Salary lines" });
    expect(within(table).getByText("DBSSSGSG ••••6789")).toBeTruthy();
    expect(within(table).getByText("Held for tax clearance")).toBeTruthy();
    expect(within(table).getByText("Released")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Record what became of Cai Yu" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Release" })).toBeNull();
    await noViolations(document.body);
  });

  it("shows the Payment releaser the total, the count and the state, never a line, and lets them release", async () => {
    const seen = page(base, releaser);
    expect(await screen.findByText(/You see the batch's total, the number of people and its release state/)).toBeTruthy();
    expect(screen.queryByRole("table", { name: "Salary lines" })).toBeNull();
    expect(screen.queryByText(/Ana/)).toBeNull();
    answer({ "GET /v1/salary-batches/s1": { status: 200, body: base, headers: { ETag: '"2-approved"' } }, "POST /v1/salary-batches/s1/release": { status: 200, body: { ...base, state: "released_in_part", released: 2 }, headers: { ETag: '"2-approved"' } } });
    fireEvent.click(await screen.findByRole("button", { name: "Release" }));
    await waitFor(() => expect(screen.getByText(/Released the lines that are free to go/)).toBeTruthy());
    expect(seen.length).toBeGreaterThan(0);
  });

  it("says a missing arrangement stops the approval, and offers the Payroll administrator nothing to ask until it is recorded", async () => {
    page({ ...base, status: "open", state: "prepared", checks: [{ severity: "error", code: "PPL-30", message: "How this month's statutory outputs are met is not recorded: itemised payslips.", fix: "Record each in Settings, People and HR" }] }, payroll);
    expect(await screen.findByText(/How this month's statutory outputs are met is not recorded/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(true);
    await noViolations(document.body);
  });

  it("matches a bank debit to released lines, and only when the lines make it to the cent", async () => {
    const lines2 = [
      { ...lines[1] },
      { line_id: "l3", person_id: "p3", person_name: "Dee Ng", amount: sgd("1000.00"), account: "UOBSGSG ••••1111", bank_detail_version: 1, line_version: 1, state: "released" as const, instruction_id: "S3" },
    ];
    const batch = { ...base, state: "released" as const, released: 2, lines_visible: true, lines: lines2 };
    const seen = answer({
      "GET /v1/salary-batches/s1": { status: 200, body: batch, headers: { ETag: '"2-approved"' } },
      "GET /v1/bank-lines": { status: 200, body: { items: [
        { id: "b1", money_account_id: "a1", booking_date: "2026-10-31", amount: sgd("-8000.33"), description: "GIRO SALARY", status: "unreconciled" },
        { id: "b2", money_account_id: "a1", booking_date: "2026-10-30", amount: sgd("500.00"), description: "A receipt", status: "unreconciled" },
      ] } },
      "POST /v1/salary-batches/s1/bank-match": { status: 200, body: { ...batch, state: "paid" } },
    });
    renderPage(SalaryPayment, { path: "/payroll/salary-payments/s1", route: "/payroll/salary-payments/$batchId", session: payroll });
    fireEvent.click(await screen.findByRole("button", { name: "Match a bank debit" }));
    const form = await screen.findByRole("form", { name: "Match a bank debit" });
    const pick = within(form).getByLabelText("The bank debit") as HTMLSelectElement;
    await waitFor(() => expect(within(pick).getAllByRole("option").length).toBe(2)); // the credit is not offered
    fireEvent.change(pick, { target: { value: "b1" } });
    const submit = within(form).getByRole("button", { name: "Match a bank debit" }) as HTMLButtonElement;
    fireEvent.click(within(form).getByLabelText(/Cai Yu/));
    expect(submit.disabled).toBe(true); // 7,000.33 is not 8,000.33
    fireEvent.click(within(form).getByLabelText(/Dee Ng/));
    expect(within(form).getByText(/It matches the debit to the cent/)).toBeTruthy();
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/salary-batches/s1/bank-match")).toBe(true));
    const post = seen.find((x) => x.method === "POST")!;
    expect(post.body).toEqual({ bank_line_id: "b1", line_ids: ["l2", "l3"] });
    expect(await screen.findByText(/Matched the bank debit and marked 2 line\(s\) paid/)).toBeTruthy();
  });
});
