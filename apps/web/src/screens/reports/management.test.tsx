import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ManagementReport } from "./ManagementReport";

type Report = components["schemas"]["ManagementReport"];
type Period = components["schemas"]["Period"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const m = (amount: string) => ({ amount, currency: "SGD" });
const period = (id: string, starts: string, ends: string): Period => ({ id, starts_on: starts, ends_on: ends, status: "open", financial_year: 1, version: 1 });
const periods = [period("p-sep", "2026-09-01", "2026-09-30"), period("p-oct", "2026-10-01", "2026-10-31")];
const base = { currency: "SGD", from: "2026-10-01", to: "2026-10-31", ledger_revision: 42, platform_version: "0.1.0" };

// req: FR-RPT-03
describe("the management reports", () => {
  it("shows sales by item with each row's quantity, share and documents, and the total; axe finds nothing", async () => {
    const items: Report = {
      ...base,
      report: "sales_by_item",
      total: m("1000.00"),
      rows: [
        { id: "i1", code: "PLATFORM", name: "Platform", amount: m("500.00"), share: "50.0", quantity: "1", documents: 3 },
        { id: "i2", code: "SUPPORT", name: "Support", amount: m("300.00"), share: "30.0", quantity: "3", documents: 1 },
        { name: "Lines without an item", amount: m("200.00"), share: "20.0", documents: 1 },
      ],
    };
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/sales-by-item": { status: 200, body: items } });
    renderPage(ManagementReport, { path: "/reports/management/sales-by-item", route: "/reports/management/$report", session: accountant, mode: "expert" });
    const table = await screen.findByRole("table", { name: /Sales by item/ });
    const rows = within(table).getAllByRole("row").map((r) => r.textContent);
    expect(rows).toEqual([
      "ItemQuantityDocumentsAmount SGDShare",
      "PLATFORM Platform13500.0050.0%",
      "SUPPORT Support31300.0030.0%",
      "Lines without an item1200.0020.0%",
      "Total1,000.00100.0%",
    ]);
    expect(seen.find((s) => s.path === "/v1/reports/sales-by-item")!.query).toContain("from=");
    expect(screen.getByText(/Read at ledger revision 42/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("opens an expense's general ledger lines over the run, and says when nothing was spent", async () => {
    const top: Report = { ...base, report: "top_expenses", total: m("545.00"), rows: [{ id: "a1", code: "6100", name: "Rent", amount: m("545.00"), share: "100.0", documents: 0 }] };
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/top-expenses": { status: 200, body: top } });
    renderPage(ManagementReport, { path: "/reports/management/top-expenses", route: "/reports/management/$report", session: accountant, mode: "expert" });
    const link = await screen.findByRole("link", { name: "Rent" });
    expect(link.getAttribute("href")).toContain("/reports/general-ledger?");
    expect(link.getAttribute("href")).toContain("account_id");
    expect(screen.getByRole("link", { name: "Top expenses" }).getAttribute("aria-current")).toBe("page");

    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/spend-by-supplier": { status: 200, body: { ...base, report: "spend_by_supplier", total: m("0.00"), rows: [] } },
    });
    renderPage(ManagementReport, { path: "/reports/management/spend-by-supplier", route: "/reports/management/$report", session: accountant, mode: "expert" });
    expect(await screen.findByText("Nothing in these months.")).toBeTruthy();
  });
});
