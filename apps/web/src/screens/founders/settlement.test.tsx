import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DirectorSettlement } from "./DirectorSettlement";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const loan = { account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" as const };
const mei = {
  contact_id: "c-mei",
  legal_name: "Mei Lin Tan",
  director: true,
  shareholder: true,
  member_user_id: ownerSession.user_id,
  active: true,
  balances: [{ ...loan, balance: sgd("-1000.00") }],
  net: sgd("-1000.00"),
  in_debit: false,
};
const people = { as_at: "2026-11-03", currency: "SGD", items: [mei], unkeyed: sgd("0.00") };
const account = (id: string, code: string, name: string, type: string, extra = {}) => ({ id, code, name, type, active: true, group: false, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1, ...extra });
const chart = {
  items: [
    account("acc-1010", "1010", "DBS current", "asset", { money_account: true }),
    account("acc-2500", "2500", "Loan from director / shareholder", "liability", { role: "director-loan" }),
    account("acc-3300", "3300", "Capital contribution", "equity"),
    account("acc-4010", "4010", "Other income", "revenue"),
  ],
};
const settlement = (over = {}) => ({
  id: "ds-1", status: "draft", version: 1, date: "2026-11-03", contact_id: "c-mei", legal_name: "Mei Lin Tan", account_id: "acc-2500", account_code: "2500",
  amount: sgd("400.00"), owed: sgd("1000.00"), route: "waive", waiver: { credit_account_id: "acc-3300", capacity: "shareholder", reason: "Waived ahead of the seed round" },
  deducted_earlier: false, agreements: 1, checks: [], accountant_review: false,
  ledger_impact: [
    { line_no: 1, account_id: "acc-2500", amount: sgd("400.00"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("400.00"), party_id: "c-mei" },
    { line_no: 2, account_id: "acc-3300", amount: sgd("-400.00"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("-400.00") },
  ],
  ...over,
});
const page = () => renderPage(DirectorSettlement, { path: "/accounting/directors/c-mei/settle", route: "/accounting/directors/$contactId/settle" });

// req: FR-EQ-05
// req: FR-EQ-07
// req: FM-9
// req: equity/founders-money Waive
// req: equity/founders-money Ledger impact before approval
describe("Settle a director's balance", () => {
  it("starts on the default route, drafts a waiver with what it posts, then asks another person over its version; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: people },
      "GET /v1/accounts": { status: 200, body: chart },
      "GET /v1/director-settlements": { status: 200, body: { items: [], default_route: "keep" } },
      "POST /v1/director-settlements": { status: 201, body: settlement(), headers: { ETag: '"1-draft"' } },
      "GET /v1/director-settlements/ds-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/director-settlements/ds-1/approval": { status: 202, body: { id: "apr-7", object_type: "director_settlement", status: "pending" } },
      "GET /v1/director-settlements/ds-1": { status: 200, body: settlement({ status: "waiting", approval_asked: "apr-7" }), headers: { ETag: '"1-draft"' } },
    });
    page();
    expect(await screen.findByRole("heading", { name: "Settle Mei Lin Tan's balance" })).toBeTruthy();
    expect((await screen.findByRole("radio", { name: /Keep as a loan/ })).getAttribute("aria-checked")).toBe("true");
    expect((screen.getByRole("radio", { name: /Convert to shares/ }) as HTMLButtonElement).disabled).toBe(true);
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("radio", { name: /Waive/ }));
    fireEvent.change(screen.getByLabelText("Amount (SGD)"), { target: { value: "400" } });
    const credit = screen.getByLabelText("Credit to") as HTMLSelectElement;
    expect(within(credit).queryByText(/1010/)).toBeNull(); // never a money account
    fireEvent.change(credit, { target: { value: "acc-3300" } });
    fireEvent.change(screen.getByLabelText("Why they waive it"), { target: { value: "Waived ahead of the seed round" } });
    fireEvent.click(screen.getByRole("button", { name: "Save the draft" }));

    const impact = (await screen.findByRole("heading", { name: "What it posts" })).closest("section") as HTMLElement;
    expect(within(impact).getByText(/400.00 SGD stops being owed to Mei Lin Tan and goes to Capital contribution/)).toBeTruthy();
    expect(within(impact).getAllByText("400.00")).toHaveLength(2); // Dr 2500, Cr 3300
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/director-settlements")?.body).toEqual({
      contact_id: "c-mei", account_id: "acc-2500", amount: sgd("400"), route: "waive",
      waiver: { credit_account_id: "acc-3300", capacity: "shareholder", reason: "Waived ahead of the seed round" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Asked for approval. Another person approves it, never Mei Lin Tan/)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/director-settlements/ds-1/approval")?.headers.get("If-Match")).toBe('"1-draft"');
    expect(await screen.findByText("Awaiting approval")).toBeTruthy();
    await noViolations(document.body);
  });

  // req: equity/founders-money Repay
  it("says why repaying is refused while the person has no approved account", async () => {
    answer({
      "GET /v1/director-accounts": { status: 200, body: people },
      "GET /v1/accounts": { status: 200, body: chart },
      "GET /v1/director-settlements": { status: 200, body: { items: [], default_route: "keep" } },
      "POST /v1/director-settlements": {
        status: 422,
        body: { type: "urn:fyniq:problem:FOUNDER-32", title: "No account to repay the person into", status: 422, code: "FOUNDER-32",
          detail: "Mei Lin Tan has no approved SGD account at a Singapore bank to repay into.", fix: "Have their bank details entered and approved first" },
      },
    });
    page();
    fireEvent.click(await screen.findByRole("radio", { name: /Repay in cash/ }));
    expect(screen.getByText(/Paid by the next payment batch into Mei Lin Tan's approved bank account/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Amount (SGD)"), { target: { value: "300" } });
    fireEvent.click(screen.getByRole("button", { name: "Save the draft" }));
    expect(await screen.findByText(/no approved SGD account at a Singapore bank/)).toBeTruthy();
  });

  it("shows a decided repayment read-only, with what batches repaid", async () => {
    const decided = settlement({ id: "ds-2", number: "DS-0002", status: "decided", route: "repay", waiver: undefined, ledger_impact: [], amount: sgd("300.00"), repaid: sgd("300.00"),
      repay_to: { currency: "SGD", kind: "singapore", bic: "DBSSSGSG", bank_country: "SG", account_name: "TAN MEI LIN", number_kind: "account", account_number_last4: "4321" } });
    answer({
      "GET /v1/director-accounts": { status: 200, body: people },
      "GET /v1/accounts": { status: 200, body: chart },
      "GET /v1/director-settlements": { status: 200, body: { items: [decided], default_route: "keep" } },
      "GET /v1/director-settlements/ds-2": { status: 200, body: decided, headers: { ETag: '"2-posted"' } },
      "GET /v1/director-settlements/ds-2/evidence": { status: 200, body: { items: [] } },
    });
    page();
    fireEvent.click(await screen.findByRole("button", { name: /DS-0002 · Repay in cash/ }));
    expect(await screen.findByText(/Paid into DBSSSGSG ••••4321/)).toBeTruthy();
    expect(screen.getByText(/Repaid by payment batches so far: 300.00 SGD of 300.00 SGD/)).toBeTruthy();
    expect(screen.getByText("Decided as DS-0002.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  });
});
