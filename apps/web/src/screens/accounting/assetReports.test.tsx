import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { FixedAssets } from "./FixedAssets";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const organisation = { base_currency: "SGD", team_mode: "founder" } as never;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const row = (extra: Record<string, unknown> = {}) => ({
  asset_id: "fa1", number: "FA-0001", name: "MacBook Pro 14", purchased_on: "2026-10-14", opening_cost: sgd("0.00"), additions: sgd("3600.00"), disposals: sgd("0.00"), closing_cost: sgd("3600.00"),
  opening_accumulated: sgd("0.00"), charge: sgd("100.00"), disposed_accumulated: sgd("0.00"), closing_accumulated: sgd("100.00"), opening_nbv: sgd("0.00"), closing_nbv: sgd("3500.00"), ...extra,
});
const movement = (tied = true) => ({
  currency: "SGD", from: "2026-01-01", to: "2026-12-31", tied,
  categories: [{ account_id: "a1510", account_code: "1510", account_name: "Computer equipment", assets: [row()], total: row({ asset_id: undefined, number: undefined, name: "Total" }) }],
  total: row({ asset_id: undefined, number: undefined, name: "Total" }),
  ties: [{ account_code: "1510", account_name: "Computer equipment", ledger: sgd("3600.00"), register: sgd("3600.00"), unexplained: tied ? sgd("0.00") : sgd("100.00") }],
});
const schedule = {
  currency: "SGD", ya: 2027, pack: "sg-capital-allowances@2026.1",
  rows: [{ asset_id: "fa1", number: "FA-0001", name: "MacBook Pro 14", purchased_on: "2026-10-14", cost: sgd("3000.00"), method: "s19a1", first_ya: 2027, brought_forward: sgd("3000.00"),
    initial_allowance: sgd("0.00"), annual_allowance: sgd("1000.00"), allowance: sgd("1000.00"), carried_forward: sgd("2000.00") }],
  apart: [{ asset_id: "fa2", number: "FA-0002", name: "Dell monitor 27", purchased_on: "2026-11-02", cost: sgd("1200.00"), reason: "no_election", fix: "Elect a method on the asset: section 19, 19A(1) or 19A(10A)" }],
  totals: { brought_forward: sgd("3000.00"), initial_allowance: sgd("0.00"), annual_allowance: sgd("1000.00"), allowance: sgd("1000.00"), carried_forward: sgd("2000.00") },
  notes: ["Amounts are in cents [accountant to confirm]."],
};
const base = {
  "GET /v1/fixed-assets": { status: 200, body: { items: [] } },
  "GET /v1/fixed-assets/unregistered": { status: 200, body: { items: [] } },
  "GET /v1/accounts": { status: 200, body: { items: [] } },
};

// req: FR-FA-04
describe("the asset reports on the fixed asset register", () => {
  it("shows the movement schedule with its totals and says when the register ties to the ledger; axe finds nothing", async () => {
    answer({ ...base, "GET /v1/reports/fixed-asset-movement": { status: 200, body: movement() } });
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(await screen.findByRole("tab", { name: "Movements" }));
    const table = await screen.findByRole("table", { name: "Asset movement" });
    expect(within(table).getByText("1510 Computer equipment")).toBeTruthy();
    expect(within(table).getAllByText("3,600.00").length).toBeGreaterThan(1);
    expect(screen.getByText("The register ties to the ledger")).toBeTruthy();
    expect(seen.some((s) => s.path === "/v1/reports/fixed-asset-movement" && /from=\d{4}-01/.test(s.query))).toBe(true);
    await noViolations(document.body);
  });

  it("says when the register does not tie", async () => {
    answer({ ...base, "GET /v1/reports/fixed-asset-movement": { status: 200, body: movement(false) } });
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(await screen.findByRole("tab", { name: "Movements" }));
    expect(await screen.findByText("The register does not tie to the ledger")).toBeTruthy();
  });

  it("shows a year's capital allowances, lists the assets apart with why, and elects a method for one", async () => {
    answer({
      ...base,
      "GET /v1/reports/capital-allowances": { status: 200, body: schedule },
      "GET /v1/fixed-assets/fa2": { status: 200, body: { id: "fa2", number: "FA-0002", name: "Dell monitor 27", version: 1, status: "in_use" }, headers: { ETag: '"1-posted"' } },
      "POST /v1/fixed-assets/fa2/change": { status: 200, body: { id: "fa2", number: "FA-0002", name: "Dell monitor 27", version: 2, status: "in_use" } },
    });
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(await screen.findByRole("tab", { name: "Capital allowances" }));
    const table = await screen.findByRole("table", { name: "Capital allowances" });
    expect(within(table).getByText("Section 19A(1), 3 years")).toBeTruthy();
    expect(within(table).getAllByText("1,000.00").length).toBeGreaterThan(0);
    const apart = screen.getByRole("list", { name: "Assets listed apart" });
    expect(within(apart).getByText(/No method elected yet/)).toBeTruthy();
    expect(screen.getByText(/Amounts are in cents/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Elect a method for FA-0002" }));
    fireEvent.click(await screen.findByRole("button", { name: "Elect" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/fixed-assets/fa2/change")).toBe(true));
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.body).toEqual({ tax_election: { method: "s19a1" } });
    expect(post.headers.get("If-Match")).toBe('"1-posted"');
  });
});
