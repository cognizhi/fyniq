import { expect, test, type Page, type Route } from "@playwright/test";

// Home's dashboard and the KPI page against the built web app in a real browser, with every /api call answered by a
// fixture (the Go suites prove the figures): the pulse card and tiles, Expert's three charts drawn by ECharts, a
// definition's popover, and the month's counts entered. FYNIQ_SHOTS, when set, names a folder for screenshots to
// look at the layout by eye.
// req: FR-KPI-01
// req: FR-KPI-02
// req: FR-VIZ-01

const session = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const months = ["2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"];
const periods = months.map((m, i) => ({ id: `p${i}`, starts_on: `${m}-01`, ends_on: `${m}-28`, status: "open", financial_year: 1, version: 1 }));
const kpi = (key: string, name: string, unit: string, month: object, before?: object, source = "ledger") => ({
  key,
  name,
  unit,
  month,
  before,
  source,
  version: `${key}@1`,
  formula: `How ${name.toLowerCase()} is worked out, in words`,
  date_basis: "the accounting month",
});
const kpis = {
  month: "2026-10",
  from: "2026-10-01",
  to: "2026-10-31",
  currency: "SGD",
  ledger_revision: 42,
  platform_version: "0.1.0",
  kpis: [
    kpi("cash", "Cash", "amount", { value: "412380.15" }, { value: "421792.75" }),
    kpi("revenue", "Revenue", "amount", { value: "61200.00" }, { value: "57600.00" }),
    kpi("net_profit_ytd", "Net profit, year to date", "amount", { value: "-182400.00" }, { value: "-150100.00" }),
    kpi("owed_to_us", "Owed to us", "amount", { value: "48250.00" }, { value: "41900.00" }),
    kpi("owed_by_us", "We owe", "amount", { value: "12870.40" }, { value: "15020.00" }),
    kpi("booked_mrr", "Booked MRR", "amount", { value: "52400.00" }, { value: "49340.00" }),
    kpi("contracted_mrr", "Contracted MRR", "amount", { undefined: "no subscription data" }, { undefined: "no subscription data" }, "subscription data"),
    kpi("gross_margin", "Gross margin", "percent", { value: "83.6" }, { value: "83.8" }),
    kpi("spend_sm", "Spend on sales & marketing", "amount", { value: "14200.00" }, { value: "12900.00" }),
    kpi("spend_rd", "Spend on research & development", "amount", { value: "41800.00" }, { value: "40100.00" }),
    kpi("spend_ga", "Spend on general & administrative", "amount", { value: "9300.00" }, { value: "9050.00" }),
    kpi("net_burn", "Net burn", "amount", { value: "9412.60" }, { value: "38214.00" }),
    kpi("runway", "Runway", "months", { value: "10.8" }, { value: "11.2" }),
    kpi("churn", "Customer churn", "percent", { undefined: "no subscription data: enter the month's customer counts" }, undefined, "entered"),
  ],
};
const m = (amount: string) => ({ amount, currency: "SGD" });
const statement = (kind: string, blocks: [string, string[]][]) => ({
  kind,
  statement_title: "Management statements",
  currency: "SGD",
  from: "2026-05-01",
  to: "2026-10-31",
  ledger_revision: 42,
  restated_presentation: false,
  platform_version: "0.1.0",
  columns: [...months.map((x) => ({ key: x, from: `${x}-01`, to: `${x}-28` })), { key: "total", from: "2026-05-01", to: "2026-10-31" }],
  checks: [],
  non_cash: [],
  blocks: blocks.map(([key, totals]) => ({ key, title: key, measure: false, totals: totals.map(m), percents: [], groups: [] })),
});
const pl = statement("pl", [
  ["revenue", ["41000.00", "44500.00", "49800.00", "53100.00", "57600.00", "61200.00", "307200.00"]],
  ["cost_of_sales", ["7100.00", "7600.00", "8200.00", "8800.00", "9300.00", "10000.00", "51000.00"]],
  ["operating_expenses", ["62000.00", "63900.00", "66100.00", "67800.00", "80600.00", "83500.00", "423900.00"]],
  ["net_profit", ["-28100.00", "-27000.00", "-24500.00", "-23500.00", "-32300.00", "-32300.00", "-167700.00"]],
]);
const cf = statement("cf", [["cash_at_end", ["560100.00", "531000.00", "498200.00", "468000.00", "421792.75", "412380.15", "412380.15"]]]);

async function serve(page: Page) {
  const seen: { key: string; headers: Record<string, string>; body: unknown }[] = [];
  let counts: object | undefined;
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api/, "");
    const key = `${req.method()} ${path}`;
    seen.push({ key, headers: req.headers(), body: req.postData() ? JSON.parse(req.postData() as string) : undefined });
    const json = (status: number, body: unknown) => route.fulfill({ status, contentType: status >= 400 ? "application/problem+json" : "application/json", body: JSON.stringify(body) });
    switch (key) {
      case "GET /v1/setup":
        return json(200, { done: true });
      case "GET /v1/session":
        return json(200, session);
      case "GET /v1/organisation":
        return json(200, organisation);
      case "GET /v1/periods":
        return json(200, { items: periods });
      case "GET /v1/kpis":
        return json(200, { ...kpis, ...(counts ? { counts } : {}) });
      case "GET /v1/reports/statements/pl":
        return json(200, pl);
      case "GET /v1/reports/statements/cf":
        return json(200, cf);
      case "PUT /v1/kpi-inputs/2026-10":
        counts = { ...(JSON.parse(req.postData() as string) as object), month: "2026-10", entered_at: "2026-11-02T02:00:00Z", entered_by: { id: session.user_id, kind: "user", name: "Mei Ling Tan" }, version: 1 };
        return json(200, counts);
      case "GET /v1/approvals":
      case "GET /v1/notifications":
      case "GET /v1/banners":
      case "GET /v1/journals/waiting-reversals":
        return json(200, { items: [] });
      case "GET /v1/review-log":
        return json(200, { items: [], unreviewed: 0, overdue: 0 });
      default:
        return route.fulfill({ status: 403, contentType: "application/problem+json", body: JSON.stringify({ code: "API-AUTHZ-01", status: 403 }) });
    }
  });
  return seen;
}

const shot = async (page: Page, name: string) => {
  if (process.env.FYNIQ_SHOTS) await page.screenshot({ path: `${process.env.FYNIQ_SHOTS}/${name}.png`, fullPage: true });
};

test("Home shows the pulse card and tiles, and Expert draws the three charts", async ({ page }) => {
  const seen = await serve(page);
  await page.goto("/");
  const pulse = page.getByRole("region", { name: "Company pulse, Oct 2026" });
  await expect(pulse.getByRole("link", { name: "SGD 412,380.15" })).toBeVisible();
  await expect(pulse.getByRole("link", { name: "10.8 months" })).toBeVisible();
  await expect(page.getByRole("link", { name: "48,250.00 SGD" })).toBeVisible();
  await expect(page.getByRole("link", { name: "61,200.00 SGD" })).toBeHidden(); // Expert's tile
  await shot(page, "home-basic");

  // a definition opens over the card, at the kit's width, inside the window, even from the right-hand figure
  const inside = async (name: string) => {
    await pulse.getByRole("button", { name }).click();
    const note = page.getByRole("note").filter({ hasText: "worked out, in words" });
    await expect(note).toBeVisible();
    const box = (await note.boundingBox())!;
    const width = page.viewportSize()!.width;
    expect(box.width).toBeGreaterThanOrEqual(Math.min(300, width - 32) - 1);
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width);
    return note;
  };
  const note = await inside("About Runway");
  await shot(page, "home-definition");
  await page.keyboard.press("Escape");
  await expect(note).toBeHidden();
  await page.keyboard.press("Escape");
  await expect(await inside("About Net burn")).toBeVisible();
  await page.keyboard.press("Escape");

  await page.getByRole("radio", { name: "Expert" }).click();
  await expect(page.getByRole("link", { name: "61,200.00 SGD" })).toBeVisible();
  for (const name of [/^Revenue, costs and profit by month\./, /^Cash by month\./, /^Costs by function\./]) {
    const plot = page.getByRole("img", { name });
    await expect(plot).toBeVisible();
    await expect(plot.locator("canvas").first()).toBeVisible();
  }
  const pls = seen.filter((s) => s.key === "GET /v1/reports/statements/pl");
  expect(pls.length).toBeGreaterThan(0);
  await shot(page, "home-expert");

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(pulse).toBeVisible();
  // no sideways scroll once the charts have redrawn at the phone's width
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await shot(page, "home-phone");
  await expect(await inside("About Net burn")).toBeVisible();
  await shot(page, "home-phone-definition");
});

test("the KPI page lists the month's KPIs, and an Owner enters the month's counts", async ({ page }) => {
  const seen = await serve(page);
  await page.goto("/reports/kpis");
  await expect(page.getByRole("heading", { level: 1, name: "Start-up KPIs" })).toBeVisible();
  await expect(page.getByRole("table", { name: "The KPIs of Oct 2026 beside Sept 2026" })).toBeVisible();
  await shot(page, "kpis");
  const card = page.getByRole("region", { name: "Customer counts and headcount, Oct 2026" });
  await card.getByRole("button", { name: "Enter" }).click();
  await card.getByLabel("Paying customers").fill("4");
  await card.getByLabel("New customers").fill("1");
  await card.getByLabel("Customers lost").fill("0");
  await card.getByLabel("Headcount").fill("8");
  await card.getByRole("button", { name: "Save" }).click();
  await expect(card.getByText("Oct 2026's counts are saved. The KPIs that use them are worked out again.")).toBeVisible();
  await expect(card.getByText(/Entered by Mei Ling Tan/)).toBeVisible();
  const put = seen.find((s) => s.key === "PUT /v1/kpi-inputs/2026-10")!;
  expect(put.headers["if-match"]).toBe('"0"');
  expect(put.body).toEqual({ paying_customers: 4, new_customers: 1, lost_customers: 0, headcount: 8 });
  await shot(page, "kpis-entered");
});
