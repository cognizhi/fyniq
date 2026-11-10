import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Dashboard } from "../Dashboard";
import { varianceText } from "../planning/summary";
import { Kpis } from "./Kpis";

type KPIs = components["schemas"]["KPIs"];
type KPI = components["schemas"]["KPIItem"];
type Statement = components["schemas"]["FinancialStatement"];
type Period = components["schemas"]["Period"];
type PlanningSummary = components["schemas"]["PlanningSummary"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const period = (id: string, starts: string, ends: string): Period => ({ id, starts_on: starts, ends_on: ends, status: "open", financial_year: 1, version: 1 });
const periods = [period("p-sep", "2026-09-01", "2026-09-30"), period("p-oct", "2026-10-01", "2026-10-31")];

const kpi = (key: string, name: string, unit: KPI["unit"], month: KPI["month"], before?: KPI["before"], source = "ledger"): KPI => ({
  key,
  name,
  unit,
  month,
  before,
  source,
  version: `${key}@1`,
  formula: `How ${name} is worked out`,
  date_basis: "the accounting month",
});

// The figures are the server's, as GET /v1/kpis answers them: the page shows them and works nothing out (FR-UX-22).
const october: KPIs = {
  month: "2026-10",
  from: "2026-10-01",
  to: "2026-10-31",
  currency: "SGD",
  ledger_revision: 42,
  platform_version: "0.1.0",
  counts: { month: "2026-10", headcount: 8, entered_at: "2026-11-02T02:00:00Z", entered_by: { id: "u1", kind: "user", name: "Mei Lin" }, version: 1 },
  kpis: [
    kpi("cash", "Cash", "amount", { value: "92500.00" }, { value: "95000.00" }),
    kpi("revenue", "Revenue", "amount", { value: "6000.00" }, { value: "4000.00" }),
    kpi("net_profit_ytd", "Net profit, year to date", "amount", { value: "-6100.00" }, { value: "-3600.00" }),
    kpi("owed_to_us", "Owed to us", "amount", { value: "2000.00" }, { value: "0.00" }),
    kpi("owed_by_us", "We owe", "amount", { value: "600.00" }, { value: "0.00" }),
    kpi("booked_mrr", "Booked MRR", "amount", { value: "5000.00" }, { value: "4000.00" }),
    kpi("contracted_mrr", "Contracted MRR", "amount", { undefined: "no subscription data" }, { undefined: "no subscription data" }, "subscription data"),
    kpi("gross_margin", "Gross margin", "percent", { value: "80.0" }, { value: "75.0" }),
    kpi("spend_sm", "Spend on sales & marketing", "amount", { value: "1200.00" }, { value: "900.00" }),
    kpi("spend_rd", "Spend on research & development", "amount", { value: "3000.00" }, { value: "3000.00" }),
    kpi("spend_ga", "Spend on general & administrative", "amount", { value: "700.00" }, { value: "650.00" }),
    kpi("net_burn", "Net burn", "amount", { value: "2500.00" }, { value: "-500.00" }),
    kpi("runway", "Runway", "months", { value: "24.7" }, { words: "Not burning" }),
    kpi("churn", "Customer churn", "percent", { undefined: "no subscription data: enter the month's customer counts" }, { undefined: "no subscription data: enter the month's customer counts" }, "entered"),
    kpi("headcount", "Headcount", "count", { value: "8", entered: true }, { undefined: "no headcount entered for the month" }, "entered"),
  ],
};

// ECharts draws on a canvas, which jsdom lacks: its module records what it is asked to draw. A dynamic import can race
// the module's mock and load the real one, so ECharts' core is mocked too: either way a chart draws on the fake.
const { drawn, fake } = vi.hoisted(() => {
  const drawn: { name: string; data: (number | null)[] }[][] = [];
  const fake = () => ({
    setOption: (o: { series: { name: string; data: (number | null)[] }[] }) => drawn.push(o.series.map((s) => ({ name: s.name, data: s.data }))),
    dispatchAction: () => undefined,
    getDataURL: () => "",
    resize: () => undefined,
    dispose: () => undefined,
  });
  return { drawn, fake };
});
vi.mock("../../components/chart/echarts", () => ({ init: fake }));
vi.mock("echarts/core", () => ({ init: fake, use: () => undefined }));

// req: FR-KPI-02
// req: FR-KPI-03
describe("the KPI page", () => {
  it("shows the month's KPIs beside the month before, says why a KPI has no figure, and keeps Basic to its rows; axe finds nothing", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": { status: 200, body: october } });
    renderPage(Kpis, { path: "/reports/kpis", session: accountant, mode: "basic" });
    const table = await screen.findByRole("table", { name: "The KPIs of Oct 2026 beside Sept 2026" });
    expect(seen.find((s) => s.path === "/v1/kpis")!.query).toBe("?month=2026-10");
    const row = (key: string) => table.querySelector(`tr[data-kpi="${key}"]`)!;
    expect(within(row("cash") as HTMLElement).getAllByRole("cell").map((c) => c.textContent)).toEqual(["Cash", "92,500.00 SGD", "95,000.00 SGD", "How Cash is worked outReads the accounting month.", "The ledgercash@1"]);
    expect(row("net_profit_ytd").textContent).toContain("−6,100.00 SGD");
    expect(row("gross_margin").textContent).toContain("80.0%");
    expect(row("runway").textContent).toContain("24.7 months");
    expect(row("runway").textContent).toContain("Not burning");
    // never 0: "—" with why (FR-KPI-03; G4-047's words)
    expect(row("churn").textContent).toContain("No figure: no subscription data: enter the month's customer counts");
    expect(within(row("churn") as HTMLElement).getAllByRole("cell")[1]!.textContent).toBe("—");
    // Basic's rows and Expert's: the formula and source columns, and the rows beyond Basic's, are Expert's
    expect(row("cash").className).toBe("");
    expect(row("spend_sm").className).toBe("fy-expert-only");
    expect(row("churn").className).toBe("fy-expert-only");
    expect(screen.getByText("More in Expert")).toBeTruthy();
    // both MRR measures lead, explained
    const tiles = document.querySelectorAll(".fy-grid--tiles [data-kpi]");
    expect(Array.from(tiles).map((t) => t.getAttribute("data-kpi"))).toEqual(["booked_mrr", "contracted_mrr", "net_burn", "runway"]);
    expect(screen.getByText(/Booked MRR is the subscription revenue the ledger recognised in the month/)).toBeTruthy();
    expect(within(document.querySelector('.fy-grid--tiles [data-kpi="contracted_mrr"]') as HTMLElement).getByText("No figure: no subscription data")).toBeTruthy();
    await noViolations(document.body);
  });

  it("opens a KPI's definition on its ⓘ, and Esc closes it", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": { status: 200, body: october } });
    renderPage(Kpis, { path: "/reports/kpis", session: accountant });
    const about = await screen.findByRole("button", { name: "About Runway" });
    fireEvent.click(about);
    expect(about.getAttribute("aria-expanded")).toBe("true");
    const note = screen.getByRole("note", { name: "" });
    expect(note.textContent).toContain("How Runway is worked out");
    expect(note.textContent).toContain("Definition runway@1");
    await noViolations(document.body);
    fireEvent.keyDown(about, { key: "Escape" });
    expect(about.getAttribute("aria-expanded")).toBe("false");
  });

  // req: FR-KPI-02
  it("lets an Accountant enter the month's counts: the three customer counts together, whole, saved with If-Match", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "PUT /v1/kpi-inputs/2026-10": (body) => ({ status: 200, body: { ...(body as object), month: "2026-10", entered_at: "2026-11-03T02:00:00Z", version: 2 } }),
    });
    renderPage(Kpis, { path: "/reports/kpis", session: accountant });
    const card = await screen.findByRole("region", { name: "Customer counts and headcount, Oct 2026" });
    expect(within(card).getByText(/Entered by Mei Lin/)).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Change" }));
    fireEvent.change(within(card).getByLabelText("Paying customers"), { target: { value: "10" } });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    expect(await within(card).findByText("New customers: enter it with the other two customer counts, or clear all three")).toBeTruthy();
    expect(within(card).getByLabelText("New customers").getAttribute("aria-invalid")).toBe("true");
    fireEvent.change(within(card).getByLabelText("New customers"), { target: { value: "2.5" } });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    expect(await within(card).findByText("New customers: enter a whole number of zero or more")).toBeTruthy();
    expect(seen.some((s) => s.method === "PUT")).toBe(false);
    fireEvent.change(within(card).getByLabelText("New customers"), { target: { value: "2" } });
    fireEvent.change(within(card).getByLabelText("Customers lost"), { target: { value: "1" } });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    expect(await within(card).findByText("Oct 2026's counts are saved. The KPIs that use them are worked out again.")).toBeTruthy();
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe('"1"');
    expect(put.body).toEqual({ paying_customers: 10, new_customers: 2, lost_customers: 1, headcount: 8 });
    expect(seen.filter((s) => s.path === "/v1/kpis").length).toBe(2); // read again
  });

  it("sends If-Match 0 for a month without counts, and shows the API's refusal", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: { ...october, counts: undefined } },
      "PUT /v1/kpi-inputs/2026-10": problem(412, "API-PRECONDITION-01", "Someone changed the counts meanwhile.", "Read them again."),
    });
    renderPage(Kpis, { path: "/reports/kpis", session: accountant });
    const card = await screen.findByRole("region", { name: "Customer counts and headcount, Oct 2026" });
    expect(within(card).getByText("Nothing entered for Oct 2026.")).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Enter" }));
    fireEvent.change(within(card).getByLabelText("Headcount"), { target: { value: "8" } });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    expect(await within(card).findByRole("alert")).toBeTruthy();
    expect(seen.find((s) => s.method === "PUT")!.headers.get("If-Match")).toBe('"0"');
  });

  it("offers no editor to a role that may not enter the counts", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": { status: 200, body: october } });
    renderPage(Kpis, { path: "/reports/kpis", session: bookkeeper });
    const card = await screen.findByRole("region", { name: "Customer counts and headcount, Oct 2026" });
    expect(within(card).queryByRole("button", { name: /Change|Enter/ })).toBeNull();
    expect(within(card).getByText(/An Owner, Controller or Accountant enters them/)).toBeTruthy();
  });
});

const m = (amount: string) => ({ amount, currency: "SGD" });
const columns = [
  { key: "2026-09", from: "2026-09-01", to: "2026-09-30" },
  { key: "2026-10", from: "2026-10-01", to: "2026-10-31" },
  { key: "total", from: "2026-09-01", to: "2026-10-31" },
];
const statement = (kind: Statement["kind"], blocks: [string, string[]][]): Statement => ({
  kind,
  statement_title: "Management statements",
  currency: "SGD",
  from: "2026-09-01",
  to: "2026-10-31",
  ledger_revision: 42,
  restated_presentation: false,
  platform_version: "0.1.0",
  columns,
  checks: [],
  non_cash: [],
  blocks: blocks.map(([key, totals]) => ({ key, title: key, measure: false, totals: totals.map(m), percents: [], groups: [] })),
});
const pl = statement("pl", [
  ["revenue", ["4000.00", "6000.00", "10000.00"]],
  ["cost_of_sales", ["800.00", "1200.00", "2000.00"]],
  ["operating_expenses", ["3900.00", "4900.00", "8800.00"]],
  ["net_profit", ["-700.00", "-100.00", "-800.00"]],
]);
const cf = statement("cf", [["cash_at_end", ["95000.00", "92500.00", "92500.00"]]]);

// req: FR-KPI-01
// req: FR-UX-04
// req: FR-VIZ-01
describe("Home's dashboard", () => {
  it("shows the pulse card and Basic's tiles, each opening what is behind it, and no charts in Basic; axe finds nothing", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": { status: 200, body: october } });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "basic" });
    const pulse = await screen.findByRole("region", { name: "Company pulse, Oct 2026" });
    expect(within(pulse).getByRole("link", { name: "SGD 92,500.00" }).getAttribute("href")).toBe("/reports/statements/bs");
    expect(within(pulse).getByRole("link", { name: "24.7 months" }).getAttribute("href")).toBe("/reports/statements/cf");
    expect(within(pulse).getByText("Sept 2026: −500.00 SGD")).toBeTruthy(); // net burn the month before: it generated cash
    expect(screen.getByRole("link", { name: "2,000.00 SGD" }).getAttribute("href")).toBe("/sales/statements");
    expect(screen.getByRole("link", { name: "600.00 SGD" }).getAttribute("href")).toBe("/purchases/bills");
    expect(document.querySelector('[data-kpi="revenue"]')!.className).toContain("fy-expert-only");
    expect(screen.getByRole("link", { name: /All KPIs/ }).getAttribute("href")).toBe("/reports/kpis");
    expect(seen.some((s) => s.path.startsWith("/v1/reports/statements"))).toBe(false);
    await noViolations(document.body);
  });

  it("says Not burning instead of a runway", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: { ...october, kpis: october.kpis.map((k) => (k.key === "runway" ? { ...k, month: { words: "Not burning" } } : k)) } },
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "basic" });
    const pulse = await screen.findByRole("region", { name: "Company pulse, Oct 2026" });
    expect(within(pulse).getByRole("link", { name: "Not burning" })).toBeTruthy();
    expect(within(pulse).getByText("The last three months brought in more cash than they used")).toBeTruthy();
  });

  it("draws Expert's charts from the P&L and cash flow at the KPIs' ledger revision, and costs by function from the KPIs", async () => {
    drawn.length = 0;
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/cf": { status: 200, body: cf },
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "expert" });
    expect(await screen.findByRole("img", { name: /Revenue, costs and profit by month\. In Oct 2026, revenue was 6,000\.00, costs 6,100\.00 and net profit −100\.00\./ })).toBeTruthy();
    expect(await screen.findByRole("img", { name: /Cash by month\. Cash went from 95,000\.00 at the end of Sept 2026 to 92,500\.00 at the end of Oct 2026\./ })).toBeTruthy();
    expect(await screen.findByRole("img", { name: /Costs by function/ })).toBeTruthy();
    for (const kind of ["pl", "cf"]) {
      const q = seen.find((s) => s.path === `/v1/reports/statements/${kind}`)!.query;
      expect(q).toContain("from=2026-09");
      expect(q).toContain("to=2026-10");
      expect(q).toContain("columns=month");
      expect(q).toContain("as_at_revision=42");
    }
    await vi.waitFor(() => expect(drawn.length).toBe(3));
    expect(drawn).toContainEqual([
      { name: "Revenue", data: [4000, 6000] },
      { name: "Costs", data: [4700, 6100] },
      { name: "Net profit", data: [-700, -100] },
    ]);
    expect(drawn).toContainEqual([{ name: "Cash at the month end", data: [95000, 92500] }]);
    expect(drawn).toContainEqual([
      { name: "Oct 2026", data: [1200, 3000, 700] },
      { name: "Sept 2026", data: [900, 3000, 650] },
    ]);
    await noViolations(document.body);
  });

  it("is left out when the person's roles may not read the KPIs", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": problem(403, "API-AUTHZ-01") });
    const { container } = renderPage(Dashboard, { path: "/", session: bookkeeper, mode: "basic" });
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/kpis")).toBe(true));
    await vi.waitFor(() => expect(container.querySelector(".fy-skeleton")).toBeNull());
    expect(screen.queryByRole("region", { name: /Company pulse/ })).toBeNull();
  });
});

// Home's tiles from the plans, as GET /v1/planning/summary answers them: the year to date from January against the budget,
// and the recorded forecast's cash-out month without its funding, with it beside.
const planned: PlanningSummary = {
  month: "2026-10",
  currency: "SGD",
  budget: {
    budget_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01",
    name: "FY2026 budget",
    from: "2026-01",
    revenue: { actual: "54000.00", budget: "60000.00", variance: "-6000.00", variance_percent: "-10.0" },
    costs: { actual: "57000.00", budget: "60000.00", variance: "-3000.00", variance_percent: "-5.0" },
  },
  forecast: {
    forecast_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01",
    name: "FY2027 plan",
    scenario: "Base",
    cash_out_month: "2027-07",
    runway_months: 9,
    runway_beyond: false,
    runway_beyond_with_funding: true,
    note: "Without the funding planned: none of it is received yet",
  },
};

// req: FR-KPI-01
// req: FR-PLN-03
describe("Home's tiles from the plans", () => {
  it("shows revenue and costs against the budget in Expert and the cash-out month without the funding in both modes; axe finds nothing", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "GET /v1/planning/summary": { status: 200, body: planned },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/cf": { status: 200, body: cf },
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "expert" });
    const revenue = (await screen.findByText("Revenue against budget")).closest("[data-planning]") as HTMLElement;
    expect(revenue.dataset.planning).toBe("home.revenue-vs-budget");
    expect(seen.find((s) => s.path === "/v1/planning/summary")!.query).toContain("month=2026-10");
    expect(revenue.className).toContain("fy-expert-only");
    expect(revenue.querySelector(".fy-tile__value")!.textContent).toBe("54,000.00 SGD");
    expect(within(revenue).getByText("Jan 2026 to Oct 2026. Budget 60,000.00, 10.0% under: unfavourable")).toBeTruthy();
    const costs = document.querySelector('[data-planning="home.costs-vs-budget"]') as HTMLElement;
    expect(within(costs).getByText("Jan 2026 to Oct 2026. Budget 60,000.00, 5.0% under: favourable")).toBeTruthy();
    const out = document.querySelector('[data-planning="home.forecast-cash-out"]') as HTMLElement;
    expect(out.className).not.toContain("fy-expert-only");
    expect(out.querySelector(".fy-tile__value")!.textContent).toBe("Jul 2027");
    expect(
      within(out).getByText("Without the funding planned: none of it is received yet. Base scenario of FY2027 plan. With the funding: Not within the forecast"),
    ).toBeTruthy();
    await noViolations(document.body);
  });

  it("says why a tile has no figure", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "GET /v1/planning/summary": {
        status: 200,
        body: { month: "2026-10", currency: "SGD", budget_missing: "No approved budget for the year from 1 Jan 2026", forecast_missing: "No forecast is recorded yet" },
      },
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "basic" });
    expect((await screen.findAllByText("No figure: No approved budget for the year from 1 Jan 2026")).length).toBe(2);
    expect(screen.getByText("No figure: No forecast is recorded yet")).toBeTruthy();
  });

  it("is left out when the person's roles may not read the plans", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "GET /v1/planning/summary": problem(403, "API-AUTHZ-01"),
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "basic" });
    await screen.findByRole("region", { name: "Company pulse, Oct 2026" });
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/planning/summary")).toBe(true));
    expect(document.querySelector("[data-planning]")).toBeNull();
  });

  it("shows the lowest balance of the next 60 days, opening Cash ahead, and when it falls below the buffer", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: october },
      "GET /v1/planning/summary": { status: 200, body: planned },
      "GET /v1/cash-projection": {
        status: 200,
        body: { today: "2026-11-02", days: 60, currency: "SGD", opening: { amount: "20300.00", currency: "SGD" }, lowest: { amount: "20018.50", currency: "SGD" },
          lowest_on: "2026-11-13", below_buffer_on: "2026-11-02", buffer: { months: 6 }, movements: [], daily: [], weekly: [], not_included: [] },
      },
    });
    renderPage(Dashboard, { path: "/", session: accountant, mode: "basic" });
    const tile = (await screen.findByText("Cash ahead")).closest("[data-planning]") as HTMLElement;
    expect(within(tile).getByRole("link", { name: "20,018.50 SGD" }).getAttribute("href")).toBe("/planning/cash");
    expect(within(tile).getByText("Lowest in the next 60 days, on 13 Nov 2026")).toBeTruthy();
    expect(within(tile).getByText("Below the buffer from 2 Nov 2026")).toBeTruthy();
    expect(seen.find((x) => x.path === "/v1/cash-projection")!.query).toContain("days=60");
  });

  it("words a variance by whether it is favourable, from the server's percentage", () => {
    expect(varianceText({ actual: "12.00", budget: "10.00", variance: "2.00", variance_percent: "20.0" }, true)).toBe("Budget 10.00, 20.0% over: favourable");
    expect(varianceText({ actual: "12.00", budget: "10.00", variance: "2.00", variance_percent: "20.0" }, false)).toBe("Budget 10.00, 20.0% over: unfavourable");
    expect(varianceText({ actual: "10.00", budget: "10.00", variance: "0.00", variance_percent: "0.0" }, false)).toBe("Budget 10.00, on budget");
    expect(varianceText({ actual: "5.00", budget: "0.00", variance: "5.00" }, false)).toBe("Budget 0.00: nothing budgeted");
  });
});
