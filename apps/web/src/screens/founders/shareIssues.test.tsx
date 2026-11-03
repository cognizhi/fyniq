import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DirectorSettlement } from "./DirectorSettlement";
import { ShareIssues } from "./ShareIssues";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const mei = {
  contact_id: "c-mei", legal_name: "Mei Lin Tan", director: true, shareholder: true, member_user_id: undefined, active: true,
  balances: [{ account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" as const, balance: sgd("-1000.00") }],
  net: sgd("-1000.00"), in_debit: false,
};
const people = { as_at: "2026-11-03", currency: "SGD", items: [mei], unkeyed: sgd("0.00") };
const account = (id: string, code: string, name: string, type: string, extra = {}) => ({ id, code, name, type, active: true, group: false, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1, ...extra });
const chart = { items: [account("acc-2500", "2500", "Loan from director / shareholder", "liability", { role: "director-loan" }), account("acc-3010", "3010", "Ordinary share capital", "equity", { role: "share-capital" })] };
const subscription = { id: "si-1", holder_id: "c-mei", holder_name: "Mei Lin Tan", class_account_id: "acc-3010", class_code: "3010", shares: "10000", price: "1.00",
  amount: sgd("10000.00"), consideration: "cash", resolution_refs: "Incorporation subscription", lodgement_ref: "AA2025010200123", effective_on: "2025-01-02",
  before_books: true, recorded_at: "2026-11-03T09:00:00Z" };

// req: FR-EQ-08
// req: equity/founders-money Share capital only from the register
describe("the register of share issues", () => {
  it("lists every allotment and records shares issued before the books start; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/share-issues": { status: 200, body: { items: [subscription] } },
      "GET /v1/director-accounts": { status: 200, body: people },
      "GET /v1/accounts": { status: 200, body: chart },
      "POST /v1/share-issues": { status: 201, body: { ...subscription, id: "si-2", shares: "500", effective_on: "2025-06-01" } },
    });
    renderPage(ShareIssues, { path: "/equity/transactions" });
    const table = await screen.findByRole("table", { name: "Register of share issues" });
    expect(within(table).getByText("10,000")).toBeTruthy();
    expect(within(table).getByText("Before the books start: no entry")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Record shares issued before the books start" }));
    await screen.findByRole("option", { name: "Mei Lin Tan" });
    await screen.findByRole("option", { name: "3010 Ordinary share capital" });
    fireEvent.change(screen.getByLabelText("Holder"), { target: { value: "c-mei" } });
    fireEvent.change(screen.getByLabelText("Class"), { target: { value: "acc-3010" } });
    fireEvent.change(screen.getByLabelText("Shares"), { target: { value: "500" } });
    fireEvent.change(screen.getByLabelText("Price per share"), { target: { value: "1.00" } });
    fireEvent.change(screen.getByLabelText("Issued on"), { target: { value: "2025-06-01" } });
    fireEvent.change(screen.getByLabelText("Resolutions"), { target: { value: "Board resolution 1 Jun 2025" } });
    fireEvent.click(screen.getByRole("button", { name: "Record the issue" }));
    expect(await screen.findByText(/Recorded 500 shares of Mei Lin Tan, issued on/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/share-issues")?.body).toEqual({
      holder_id: "c-mei", class_account_id: "acc-3010", shares: "500", price: "1.00", consideration: "cash", resolution_refs: "Board resolution 1 Jun 2025", effective_on: "2025-06-01",
    });
  });

  it("offers recording only to an Owner or the Controller", async () => {
    answer({ "GET /v1/share-issues": { status: 200, body: { items: [subscription] } } });
    renderPage(ShareIssues, { path: "/equity/transactions", session: { ...ownerSession, roles: ["accountant"] } as Session });
    await screen.findByRole("table", { name: "Register of share issues" });
    expect(screen.queryByRole("button", { name: "Record shares issued before the books start" })).toBeNull();
  });
});

// req: FR-EQ-09
// req: equity/founders-money Shareholders' approval lodged within 14 days
describe("a conversion's ACRA steps", () => {
  it("shows what is due and records the approval lodged; axe finds nothing", async () => {
    const approved = {
      id: "ds-3", number: "DS-0003", status: "decided", version: 2, date: "2026-10-15", contact_id: "c-mei", legal_name: "Mei Lin Tan", account_id: "acc-2500", account_code: "2500",
      amount: sgd("1000.00"), owed: sgd("1000.00"), route: "convert", deducted_earlier: false, agreements: 4, checks: [], accountant_review: false, ledger_impact: [],
      conversion: { class_account_id: "acc-3010", price: "1.20", price_basis: "latest_priced_round", capacity: "lender", measurement: "carrying" },
      shares: { shares: "833", converted: sgd("999.60"), remainder: sgd("0.40") }, lapses_on: "2027-06-30",
      acra: { agm_due_on: "2027-06-30", resolved_on: "2026-10-06", steps: [{ id: "st-1", kind: "resolved", on: "2026-10-06", reference: "Written resolution", recorded_at: "2026-10-15T09:00:00Z" }],
        flags: [{ code: "FOUNDER-38", kind: "due", message: "the shareholders' approval to issue the shares is due to be lodged with ACRA by 20 Oct 2026", fix: "Lodge it, attach ACRA's acknowledgement and record the day it was lodged", due_on: "2026-10-20" }] },
    };
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: people },
      "GET /v1/accounts": { status: 200, body: chart },
      "GET /v1/director-settlements": { status: 200, body: { items: [approved], default_route: "keep" } },
      "GET /v1/director-settlements/ds-3": { status: 200, body: approved, headers: { ETag: '"2-posted"' } },
      "GET /v1/director-settlements/ds-3/evidence": { status: 200, body: { items: [] } },
      "POST /v1/director-settlements/ds-3/acra-steps": { status: 201, body: approved },
    });
    renderPage(DirectorSettlement, { path: "/accounting/directors/c-mei/settle", route: "/accounting/directors/$contactId/settle" });
    fireEvent.click(await screen.findByRole("button", { name: /DS-0003 · Convert to shares/ }));
    expect(await screen.findByText(/due to be lodged with ACRA by 20 Oct 2026/)).toBeTruthy();
    expect(screen.getByText("Due")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Step"), { target: { value: "approval_lodged" } });
    fireEvent.change(screen.getByLabelText("On"), { target: { value: "2026-10-12" } });
    fireEvent.change(screen.getByLabelText("ACRA reference (optional)"), { target: { value: "T26A001" } });
    fireEvent.click(screen.getByRole("button", { name: "Record the step" }));
    expect(await screen.findByText(/Recorded: approval lodged with acra on/)).toBeTruthy();
    const sent = seen.find((s) => s.path === "/v1/director-settlements/ds-3/acra-steps");
    expect(sent?.body).toEqual({ kind: "approval_lodged", on: "2026-10-12", reference: "T26A001" });
    expect(sent?.headers.get("If-Match")).toBe('"2-posted"');
  });
});
