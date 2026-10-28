import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Reports } from "./Reports";
import { FinancialStatement, runOf } from "./Statement";

type Statement = components["schemas"]["FinancialStatement"];
type Period = components["schemas"]["Period"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const m = (amount: string) => ({ amount, currency: "SGD" });
const period = (id: string, starts: string, ends: string, status: Period["status"], year = 1): Period => ({ id, starts_on: starts, ends_on: ends, status, financial_year: year, version: 1 });
const periods = [period("p-sep", "2026-09-01", "2026-09-30", "closed"), period("p-oct", "2026-10-01", "2026-10-31", "open")];
const columns = [{ key: "current", from: "2026-10-01", to: "2026-10-31" }];
const pl: Statement = {
  kind: "pl",
  statement_title: "Management statements",
  currency: "SGD",
  from: "2026-10-01",
  to: "2026-10-31",
  ledger_revision: 42,
  restated_presentation: false,
  platform_version: "0.1.0",
  columns,
  checks: [],
  non_cash: [],
  blocks: [
    {
      key: "revenue",
      title: "Revenue",
      measure: false,
      totals: [m("5000.00")],
      percents: ["100.0"],
      groups: [{ name: "Subscription revenue", totals: [m("5000.00")], percents: ["100.0"], lines: [{ account_id: "a-4010", code: "4010", name: "Subscription revenue", amounts: [m("5000.00")], percents: ["100.0"] }] }],
    },
    { key: "gross_profit", title: "Gross profit", measure: true, totals: [m("4000.00")], percents: ["80.0"], groups: [] },
    { key: "net_profit", title: "Net profit", measure: true, totals: [m("3190.00")], percents: ["63.8"], groups: [] },
  ],
};

afterEach(() => vi.restoreAllMocks());

// req: FR-RPT-02
describe("the statements' periods", () => {
  it("runs this month, last month, the year to date and the financial years from the books' periods", () => {
    const year = [period("a", "2026-01-01", "2026-01-31", "closed", 1), period("b", "2026-02-01", "2026-02-28", "open", 1), period("c", "2027-01-01", "2027-01-31", "open", 2)];
    const none = { from: "", to: "" };
    expect(runOf("month", year, none, "2026-02")).toEqual({ from: "2026-02", to: "2026-02" });
    expect(runOf("last_month", year, none, "2026-02")).toEqual({ from: "2026-01", to: "2026-01" });
    expect(runOf("ytd", year, none, "2026-02")).toEqual({ from: "2026-01", to: "2026-02" });
    expect(runOf("last_year", year, none, "2026-02")).toBeNull();
    expect(runOf("custom", year, { from: "2026-01", to: "" }, "2026-02")).toBeNull();
    // past the books' last month: their last month
    expect(runOf("month", year, none, "2030-05")).toEqual({ from: "2027-01", to: "2027-01" });
    expect(runOf("last_year", year, none, "2030-05")).toEqual({ from: "2026-01", to: "2026-02" });
  });
});

// req: FR-RPT-01
// req: FR-RPT-08
// req: FR-UX-04
describe("a financial statement", () => {
  it("lays out the P&L with its % of revenue, opens an account's lines in the general ledger, and says the month is open; axe finds nothing", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/statements/pl": { status: 200, body: pl } });
    renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant });
    const table = await screen.findByRole("table", { name: /Profit and loss from 1 Oct 2026 to 31 Oct 2026/ });
    expect(screen.getByText(/Management statements · 1 Oct 2026 to 31 Oct 2026 · SGD/)).toBeTruthy();
    expect(within(table).getByRole("columnheader", { name: "% of revenue" })).toBeTruthy();
    const figure = within(table).getAllByRole("link", { name: "5,000.00" })[0]!;
    expect(figure.getAttribute("href")).toMatch(/^\/reports\/general-ledger\?/);
    expect(decodeURIComponent(figure.getAttribute("href")!)).toContain("a-4010");
    expect(within(table).getByRole("rowheader", { name: "Gross profit" })).toBeTruthy();
    expect(within(table).getByText("80.0%")).toBeTruthy();
    expect(screen.getByText(/is not closed yet/)).toBeTruthy();
    expect(screen.getByText(/Read at ledger revision 42/)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/reports/statements/pl")!.query).toContain("columns=total");
    await noViolations(document.body);
  });

  it("shows a failed check, a restated presentation and the cash flow's transactions that moved no money", async () => {
    const cf: Statement = {
      ...pl,
      kind: "cf",
      restated_presentation: true,
      checks: [{ key: "placed", ok: false, title: "Accounts sit in no line of the cash flow", detail: "1999 Unsorted: give each a cash-flow line" }],
      non_cash: [{ date: "2026-10-31", source_type: "manual_journal", source_id: "j1", memo: "Interest on the director's loan", amount: m("30.00") }],
      blocks: [{ key: "net_change", title: "Net change in cash", measure: true, totals: [m("-1880.00")], percents: [""], groups: [] }],
    };
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/statements/cf": { status: 200, body: cf } });
    renderPage(FinancialStatement, { path: "/reports/statements/cf", route: "/reports/statements/$kind", session: accountant });
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Accounts sit in no line of the cash flow")).toBeTruthy();
    expect(screen.getByText("Restated presentation")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Transactions that moved no money" })).toBeTruthy();
    expect(screen.getByText("Interest on the director's loan")).toBeTruthy();
    expect(screen.getByText("−1,880.00")).toBeTruthy();
    await noViolations(document.body);
  });

  it("asks for a comparison and a column per month in Expert mode", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/statements/pl": { status: 200, body: pl } });
    renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant });
    await screen.findByRole("table");
    fireEvent.change(screen.getByLabelText("Compare with"), { target: { value: "prior_year" } });
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/reports/statements/pl" && s.query.includes("compare=prior_year"))).toBe(true));
    fireEvent.click(screen.getByLabelText("A column per month"));
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/reports/statements/pl" && s.query.includes("columns=month"))).toBe(true));
  });
});

// req: FR-RPT-05
// req: FR-RPT-09
describe("exporting a statement", () => {
  it("downloads the file the server wrote, at the revision shown, and says when it cannot", async () => {
    const file = { export_id: "x1", file_name: "profit-and-loss-2026-10.xlsx", media_type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", sha256: "ab", size_bytes: 3, rows: 1, ledger_revision: 42, content_base64: btoa("abc") };
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/pl/export": { status: 200, body: file },
    });
    const create = vi.fn(() => "blob:x");
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Export to Excel" }));
    await vi.waitFor(() => expect(click).toHaveBeenCalled());
    const q = seen.find((s) => s.path === "/v1/reports/statements/pl/export")!.query;
    expect(q).toContain("format=xlsx");
    expect(q).toContain("as_at_revision=42");
    expect(create).toHaveBeenCalled();

    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/pl/export": { status: 422, body: { code: "REPORT-04", title: "A revision later than the ledger", status: 422, detail: "as_at_revision is later than the ledger's revision" } },
    });
    fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
    expect(await screen.findByText("The profit and loss could not be exported")).toBeTruthy();
  });
});

// req: QA-UI-04
describe("a statement in both modes", () => {
  it("asks the API the same in Basic and in Expert", async () => {
    const asked = async (mode: "basic" | "expert") => {
      answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/statements/pl": { status: 200, body: pl } });
      const view = renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant, mode });
      await screen.findByRole("table");
      const q = seen.find((s) => s.path === "/v1/reports/statements/pl")!.query;
      view.unmount();
      return q;
    };
    expect(await asked("basic")).toBe(await asked("expert"));
  });
});

// req: FR-RPT-03
describe("the reports index", () => {
  it("shows the essentials with this month's figures and finds a report; axe finds nothing", async () => {
    const bs: Statement = { ...pl, kind: "bs", blocks: [{ key: "equity", title: "Equity", measure: false, totals: [m("13190.00")], percents: [""], groups: [] }] };
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/bs": { status: 200, body: bs },
      "GET /v1/reports/statements/cf": { status: 403, body: { code: "API-AUTHZ-01", title: "Forbidden", status: 403 } },
    });
    renderPage(Reports, { path: "/reports", session: accountant });
    expect(await screen.findByText("3,190.00")).toBeTruthy();
    expect(await screen.findByText("13,190.00")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Cash flow" }).getAttribute("href")).toBe("/reports/statements/cf");
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Find a report"), { target: { value: "ledger" } });
    expect(screen.queryByRole("link", { name: "Profit and loss" })).toBeNull();
    expect(screen.getByRole("link", { name: /General ledger/ })).toBeTruthy();
  });
});
