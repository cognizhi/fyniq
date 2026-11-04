import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { checkCost, PreIncorporationCosts } from "./PreIncorporationCosts";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const waiting = {
  id: "pic-1",
  number: "PIC-0001",
  status: "needs_decision",
  version: 1,
  date: "2025-08-28",
  supplier: "Corporate secretary",
  description: "Incorporation: ACRA fee and constitution",
  contact_id: "c-mei",
  paid_by: "Mei Lin Tan",
  gross: sgd("1315.00"),
  receipts: 1,
  checks: [{ severity: "warning", code: "FOUNDER-06", field: "decision", message: "it needs the accountant's decision, and posts nothing until then", fix: "Ask the accountant to decide its treatment and account" }],
  ledger_impact: [],
};
const decided = {
  ...waiting,
  id: "pic-2",
  number: "PIC-0002",
  status: "decided",
  date: "2025-06-15",
  supplier: "Figma",
  description: "Annual plan",
  contact_id: "c-arjun",
  paid_by: "Arjun Rao",
  gross: sgd("763.00"),
  gst: sgd("63.00"),
  checks: [],
  decision: { treatment: "deductible", account_id: "acc-6400", account_code: "6400", account_name: "Software", claim_gst: false, post_on: "2026-01-01", decided_on: "2026-02-03", decided_by: { kind: "user", id: "u-grace", name: "Grace Ho" } },
};
const page = {
  items: [waiting, decided],
  owed: [
    { contact_id: "c-mei", legal_name: "Mei Lin Tan", decided: sgd("0.00"), needs_decision: sgd("1315.00"), total: sgd("1315.00") },
    { contact_id: "c-arjun", legal_name: "Arjun Rao", decided: sgd("763.00"), needs_decision: sgd("0.00"), total: sgd("763.00") },
  ],
  total: sgd("2078.00"),
  books_start: "2026-01-01",
};
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;

// req: FR-EQ-03
// req: equity/founders-money Recorded with the original date and flagged
describe("Costs before incorporation", () => {
  it("lists each cost with its decision, what they come to for each founder, and axe finds nothing", async () => {
    answer({ "GET /v1/pre-incorporation-costs": { status: 200, body: page } });
    renderPage(PreIncorporationCosts, { path: "/accounting/pre-incorporation", mode: "basic" });
    const table = await screen.findByRole("table", { name: "Costs before incorporation" });
    expect(within(table).getByText("Needs decision")).toBeTruthy();
    expect(within(table).getByText("Deductible")).toBeTruthy();
    expect(within(table).getByText("2,078.00")).toBeTruthy();
    const owed = screen.getByRole("heading", { name: "Owed to the founders" }).closest("section") as HTMLElement;
    expect(within(owed).getByText(/1,315.00 after the accountant decides/)).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: /Needs decision/ }));
    expect(within(screen.getByRole("table")).queryByText("Annual plan")).toBeNull();
    await noViolations(document.body);
  });

  it("checks a cost before it is sent: dated before the books start, with its person and amount", () => {
    const f = { date: "2026-01-01", supplier: "", description: "Lawyer", contact_id: "", gross: "-5", gst: "abc" };
    const { draft, errors } = checkCost(f, "SGD", "2026-01-01");
    expect(draft).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["contact_id", "date", "gross", "gst", "supplier"]);
    const ok = checkCost({ date: "2025-08-28", supplier: "Lawyer", description: "Agreement", contact_id: "c-mei", gross: "S$2,180.00", gst: "180" }, "SGD", "2026-01-01");
    expect(ok.draft).toEqual({ date: "2025-08-28", supplier: "Lawyer", description: "Agreement", contact_id: "c-mei", gross: sgd("2180.00"), gst: sgd("180") });
  });

  it("lets the accountant decide a cost over its version, and offers no Add to them", async () => {
    const seen = answer({
      "GET /v1/pre-incorporation-costs": { status: 200, body: page },
      "GET /v1/pre-incorporation-costs/pic-1": { status: 200, body: waiting, headers: { ETag: '"1-draft"' } },
      "GET /v1/pre-incorporation-costs/pic-1/evidence": { status: 200, body: { items: [] } },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "acc-6910", code: "6910", name: "Incorporation costs", type: "expense", group: false, active: true, version: 1 }] } },
      "POST /v1/pre-incorporation-costs/pic-1/decide": { status: 200, body: { ...waiting, status: "decided", decision: { ...decided.decision, treatment: "capital" } } },
    });
    renderPage(PreIncorporationCosts, { path: "/accounting/pre-incorporation", session: accountant });
    await screen.findByRole("table", { name: "Costs before incorporation" });
    expect(screen.queryByRole("button", { name: "Add a cost" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Incorporation: ACRA fee and constitution" }));
    fireEvent.click(await screen.findByRole("button", { name: "Decide" }));
    fireEvent.change(await screen.findByLabelText("Income-tax treatment"), { target: { value: "capital" } });
    await screen.findByRole("option", { name: "6910 Incorporation costs" });
    fireEvent.change(screen.getByLabelText("Account"), { target: { value: "acc-6910" } });
    fireEvent.click(screen.getByRole("button", { name: "Decide and post" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const post = seen.find((s) => s.method === "POST");
    expect(post?.headers.get("If-Match")).toBe('"1-draft"');
    expect(post?.body).toEqual({ treatment: "capital", account_id: "acc-6910", claim_gst: false });
  });
});
