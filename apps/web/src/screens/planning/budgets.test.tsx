import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BudgetVsActual } from "../reports/BudgetVsActual";
import { Budgets } from "./Budgets";

type View = components["schemas"]["BudgetView"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const accounts = {
  items: [
    { id: "a-subs", code: "4010", name: "Subscriptions", type: "revenue", group: false, active: true, version: 1 },
    { id: "a-rent", code: "6200", name: "Office rent", type: "expense", group: false, active: true, version: 1 },
    { id: "a-bank", code: "1010", name: "DBS current", type: "asset", group: false, active: true, version: 1 },
  ],
};
const periods = {
  items: [
    { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open", financial_year: 1, version: 1 },
    { id: "p11", starts_on: "2026-11-01", ends_on: "2026-11-30", status: "open", financial_year: 1, version: 1 },
  ],
};
const three = (x: string) => [x, x, x];
const view = (status: View["status"], extra: Partial<View> = {}): View => ({
  id: "b1",
  status,
  version: 1,
  currency: "SGD",
  budget: {
    name: "FY2026 budget",
    kind: "original",
    financial_year: "2026-10-01",
    lines: [
      { account_id: "a-subs", amounts: three("10000.00") },
      { account_id: "a-rent", amounts: three("3000.00") },
    ],
  },
  months: ["2026-10", "2026-11", "2026-12"],
  line_totals: ["30000.00", "9000.00"],
  totals: { income: three("10000.00"), costs: three("3000.00"), net: three("7000.00"), income_year: "30000.00", costs_year: "9000.00", net_year: "21000.00" },
  ...extra,
});
const base = {
  "GET /v1/accounts": { status: 200, body: accounts },
  "GET /v1/dimensions": { status: 200, body: { items: [] } },
  "GET /v1/dimension-values": { status: 200, body: { items: [] } },
  "GET /v1/periods": { status: 200, body: periods },
};

// req: FR-PLN-01
// req: FR-RPT-03
// req: QA-UI-05
describe("Budgets", () => {
  it("shows a recorded budget, locked, with the server's totals; axe finds nothing", async () => {
    answer({ ...base, "GET /v1/budgets": { status: 200, body: { items: [view("recorded")] } }, "GET /v1/budgets/b1": { status: 200, body: view("recorded"), headers: { ETag: `"1-posted"` } } });
    renderPage(Budgets, { path: "/planning/budgets", session: accountant, mode: "expert" });
    expect(await screen.findByText("Recorded, locked · version 1")).toBeTruthy();
    const table = screen.getByRole("table", { name: /FY2026 budget, by account and month/ });
    expect(within(table).getByRole("columnheader", { name: "Oct 2026" })).toBeTruthy();
    expect(within(table).getByText("Subscriptions")).toBeTruthy();
    expect(within(table).getAllByText("30,000.00").length).toBe(2); // the line's year, and the year's income
    expect(within(table).getByText("21,000.00")).toBeTruthy();
    expect(within(table).queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Ask for approval" })).toBeNull();
    await noViolations(document.body);
  });

  it("edits a draft, saves it with its ETag and asks for its approval", async () => {
    answer({
      ...base,
      "GET /v1/budgets": { status: 200, body: { items: [view("draft")] } },
      "GET /v1/budgets/b1": { status: 200, body: view("draft"), headers: { ETag: `"1-draft"` } },
      "PUT /v1/budgets/b1": { status: 200, body: view("draft", { version: 2 }), headers: { ETag: `"2-draft"` } },
      "POST /v1/budgets/b1/approval": { status: 202, body: { id: "apr-1" } },
    });
    renderPage(Budgets, { path: "/planning/budgets", session: accountant, mode: "expert" });
    const cell = await screen.findByLabelText("Line 2, Nov 2026");
    fireEvent.change(cell, { target: { value: "3500.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Add a line" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove line 3" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await vi.waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe(`"1-draft"`);
    expect(put.body).toEqual({
      name: "FY2026 budget",
      kind: "original",
      financial_year: "2026-10-01",
      lines: [
        { account_id: "a-subs", dimension_value_ids: [], amounts: three("10000.00") },
        { account_id: "a-rent", dimension_value_ids: [], amounts: ["3000.00", "3500.00", "3000.00"] },
      ],
    });
    expect(await screen.findByText("Saved FY2026 budget.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/budgets/b1/approval")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/budgets/b1/approval")!.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText(/Asked for approval/)).toBeTruthy();
  });

  it("starts a new budget and copies one with growth", async () => {
    answer({
      ...base,
      "GET /v1/budgets": { status: 200, body: { items: [view("recorded")] } },
      "GET /v1/budgets/b1": { status: 200, body: view("recorded"), headers: { ETag: `"1-posted"` } },
      "POST /v1/budgets": { status: 201, body: view("draft", { id: "b2" }) },
      "GET /v1/budgets/b2": { status: 200, body: view("draft", { id: "b2" }), headers: { ETag: `"1-draft"` } },
      "POST /v1/budgets:copy": { status: 201, body: view("draft", { id: "b3" }) },
      "GET /v1/budgets/b3": { status: 200, body: view("draft", { id: "b3" }), headers: { ETag: `"1-draft"` } },
    });
    renderPage(Budgets, { path: "/planning/budgets", session: accountant, mode: "expert" });
    await screen.findByText("Recorded, locked · version 1");
    fireEvent.click(screen.getByRole("button", { name: "New budget" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "FY2026 stretch" } });
    fireEvent.change(within(dialog).getByLabelText("Financial year"), { target: { value: "2026-10-01" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Start it" }));
    await vi.waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/budgets")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/budgets")!.body).toEqual({ name: "FY2026 stretch", kind: "original", financial_year: "2026-10-01", lines: [] });
    await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Copy with growth" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Growth"), { target: { value: "0.05" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Copy it" }));
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/budgets:copy")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/budgets:copy")!.body).toMatchObject({ kind: "original", financial_year: "2026-10-01", growth: "0.05" });
  });
});

// req: FR-RPT-03
describe("Budget vs Actual", () => {
  it("shows each account's actual against its budget, favourable or not, with the totals, and asks by dimension; axe finds nothing", async () => {
    const f = (actual: string, budget: string, variance: string, pct?: string) => ({ actual, budget, variance, ...(pct ? { variance_percent: pct } : {}) });
    answer({
      ...base,
      "GET /v1/reports/budget-vs-actual": {
        status: 200,
        body: {
          currency: "SGD", financial_year: "2026-10-01", from: "2026-10", to: "2026-11", by: "account", budget_id: "b1", budget_name: "FY2026 budget",
          rows: [
            { account_id: "a-subs", code: "4010", name: "Subscriptions", income: true, favourable: false, ...f("18000.00", "20000.00", "-2000.00", "-10.0") },
            { account_id: "a-rent", code: "6200", name: "Office rent", income: false, favourable: false, ...f("6500.00", "6000.00", "500.00", "8.3") },
          ],
          income: f("18000.00", "20000.00", "-2000.00", "-10.0"), costs: f("6500.00", "6000.00", "500.00", "8.3"), net: f("11500.00", "14000.00", "-2500.00", "-17.9"),
          ledger_revision: 42,
        },
      },
    });
    renderPage(BudgetVsActual, { path: "/reports/budget-vs-actual", session: accountant, mode: "expert" });
    const table = await screen.findByRole("table", { name: /Budget vs Actual from Oct 2026 to Nov 2026/ });
    const row = within(table).getByText("Subscriptions").closest("tr")!;
    expect(within(row).getByText("18,000.00")).toBeTruthy();
    expect(within(row).getByText("−10.0%")).toBeTruthy();
    expect(within(row).getByText("Unfavourable")).toBeTruthy();
    expect(within(table).getAllByText("+8.3%").length).toBe(2); // rent, and the costs' total
    expect(screen.getByRole("link", { name: "FY2026 budget" }).getAttribute("href")).toBe("/planning/budgets");
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "By dimension" }));
    await vi.waitFor(() => expect(seen.some((s) => s.query.includes("by=dimension"))).toBe(true));
  });
});
