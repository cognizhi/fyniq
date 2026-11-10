import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Forecast } from "./Forecast";

type Plan = components["schemas"]["ForecastPlan"];
type Run = components["schemas"]["ForecastRun"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const id = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01";
const levers = { new_customers: "1", churn: "1", price: "1", services: "1", opex: "1", direct_costs: "1", fund_amount: "1", hire_delay: 0, fund_delay: 0 };
const plan: Plan = {
  name: "FY2027 plan", horizon: 12, cash_adjustment: "0.00",
  revenue: { customers: "0", new_customers: "0", new_growth: "0", churn: "0", arpa: "0.00", price_increase: "0", services: "5000.00", services_growth: "0", other: "0.00",
    collection: { this_month: "1", next_month: "0", month_after: "0" } },
  costs: { hosting: "0", ai: "0", contractors: "0", fees: "0", salary_increase: "0.04" },
  opex: [{ label: "Rent", report_group: "Premises & office", base: "3000.00", growth: "0", percent: "0" }],
  headcount: [], capex: [], funding: [{ type: "Equity", month: "2027-03", amount: "50000.00" }],
  tax: { start_up_years: 3, losses_brought_forward: "0.00", payment_lag: 9 },
  scenarios: [{ name: "Base", levers }, { name: "Worst", levers: { ...levers, new_customers: "0.7", hire_delay: 3 } }],
};
const key = (out: string | undefined, runway: number, beyond: boolean) => ({
  cash_out_month: out, runway_months: runway, runway_beyond: beyond, minimum_cash: "15161.00", minimum_month: "2027-11", average_burn: "7819.92",
  funding_gap: beyond ? "31758.50" : "81758.50", buffer_months: 6, revenue_next_12: "60000.00", arr_end: "0.00", customers_end: "0.0",
});
const run = (scenario: string, withFunding: boolean): Run => ({
  scenario, with_funding: withFunding, collections_ok: true,
  key_results: withFunding ? key(undefined, 12, true) : key("2027-07", 7, false),
  years: [{ year: 1, revenue: "60000.00", gross_profit: "60000.00", ebitda: "-94839.00", net_profit: "-94839.00", net_burn: "93839.00", cash: "15161.00", customers: "0.0", headcount: 1 }],
  tax_years: [],
  months: [
    { month: "2026-12", cash: withFunding ? "50496.75" : "50496.75" },
    { month: "2027-01", cash: withFunding ? "44193.50" : "44193.50" },
  ] as Run["months"],
});
const results = {
  forecast_id: id, currency: "SGD", on: "2026-12-05",
  books: { last_actual: "2026-11", start: "2026-12", cash: "59000.00", receivables: "4000.00", payables: "3000.00", hints: [], ledger_revision: 42 },
  packs: ["sg-cit@2026.1", "sg-cpf@2026.1", "sg-sdl@2026.1"],
  runs: [run("Base", true), run("Base", false), run("Worst", true), run("Worst", false)],
  sandbox: "A forecast never posts: nothing here changes the books (FR-PLN-08).",
};

// ECharts draws on a canvas, which jsdom lacks.
vi.mock("../../components/chart/echarts", () => ({
  init: () => ({ setOption: () => undefined, dispatchAction: () => undefined, getDataURL: () => "", resize: () => undefined, dispose: () => undefined }),
}));

// req: FR-PLN-02
// req: FR-PLN-03
// req: FR-PLN-07
// req: FR-PLN-08
describe("Forecast", () => {
  it("shows a draft's plan and each scenario's results without the funding planned first, and compares them; axe finds nothing", async () => {
    answer({
      "GET /v1/forecasts": { status: 200, body: { items: [{ id, status: "draft", version: 1, plan }] } },
      [`GET /v1/forecasts/${id}`]: { status: 200, body: { id, status: "draft", version: 1, plan }, headers: { ETag: `"1-draft"` } },
      [`GET /v1/forecasts/${id}/results`]: { status: 200, body: results },
    });
    renderPage(Forecast, { path: "/planning/forecast", session: accountant, mode: "expert" });
    const runway = await screen.findByText("Runway, without the funding planned");
    const tile = runway.closest("[data-forecast]") as HTMLElement;
    expect(tile.querySelector(".fy-tile__value")!.textContent).toBe("7 months");
    expect(within(tile).getByText("With it: 12+ months")).toBeTruthy();
    expect(within(document.querySelector('[data-forecast="cash-out"]') as HTMLElement).getByText("Jul 2027")).toBeTruthy();
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("FY2027 plan");
    expect((screen.getByLabelText("Other expenses 1: What it is") as HTMLInputElement).value).toBe("Rent");
    expect((screen.getByLabelText("Worst: Hiring delay (months)") as HTMLInputElement).value).toBe("3");
    const compare = screen.getByRole("table", { name: /Every scenario's key results/ });
    expect(within(compare).getAllByRole("row")).toHaveLength(5);
    fireEvent.click(screen.getByRole("button", { name: "Worst" }));
    expect(await screen.findByRole("img", { name: /Cash by month, Worst/ })).toBeTruthy();
    await noViolations(document.body);
  });

  it("saves a draft with its ETag and records it", async () => {
    answer({
      "GET /v1/forecasts": { status: 200, body: { items: [{ id, status: "draft", version: 1, plan }] } },
      [`GET /v1/forecasts/${id}`]: { status: 200, body: { id, status: "draft", version: 1, plan }, headers: { ETag: `"1-draft"` } },
      [`GET /v1/forecasts/${id}/results`]: { status: 200, body: results },
      [`PUT /v1/forecasts/${id}`]: { status: 200, body: { id, status: "draft", version: 2, plan: { ...plan, horizon: 24 } }, headers: { ETag: `"2-draft"` } },
      [`POST /v1/forecasts/${id}/record`]: { status: 200, body: { id, status: "recorded", version: 2, plan: { ...plan, horizon: 24 } }, headers: { ETag: `"2-posted"` } },
    });
    renderPage(Forecast, { path: "/planning/forecast", session: accountant, mode: "expert" });
    await screen.findByText("Runway, without the funding planned");
    fireEvent.change(screen.getByLabelText("Months"), { target: { value: "24" } });
    fireEvent.click(screen.getByRole("button", { name: "Add to funding" }));
    fireEvent.change(screen.getByLabelText("Funding 2: Amount"), { target: { value: "20000.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await vi.waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe(`"1-draft"`);
    expect((put.body as Plan).horizon).toBe(24);
    expect((put.body as Plan).funding[1]).toEqual({ type: "Equity", month: "", amount: "20000.00" });
    fireEvent.click(screen.getByRole("button", { name: "Record" }));
    await vi.waitFor(() => expect(seen.some((s) => s.path === `/v1/forecasts/${id}/record`)).toBe(true));
    expect(seen.find((s) => s.path === `/v1/forecasts/${id}/record`)!.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
