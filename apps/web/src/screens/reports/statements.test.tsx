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

// ECharts draws on a canvas, which jsdom lacks: its module records what it is asked to draw.
const drawn: { option: { series: { name: string; data: (number | null)[] }[] } | null } = { option: null };
vi.mock("../../components/chart/echarts", () => ({
  init: () => ({ setOption: (o: typeof drawn.option) => (drawn.option = o), dispatchAction: () => undefined, getDataURL: () => "", resize: () => undefined, dispose: () => undefined }),
}));

// req: FR-VIZ-01
// req: FR-VIZ-06
describe("the P&L's monthly trend", () => {
  it("draws revenue, gross profit and net profit a line each when the P&L has a column per month, and its table reads the same figures; axe finds nothing", async () => {
    const months = [
      { key: "2026-09", from: "2026-09-01", to: "2026-09-30" },
      { key: "2026-10", from: "2026-10-01", to: "2026-10-31" },
      { key: "total", from: "2026-09-01", to: "2026-10-31" },
    ];
    const byMonth: Statement = {
      ...pl,
      from: "2026-09-01",
      columns: months,
      blocks: [
        { key: "revenue", title: "Revenue", measure: false, totals: [m("4000.00"), m("5000.00"), m("9000.00")], percents: ["", "", ""], groups: [] },
        { key: "gross_profit", title: "Gross profit", measure: true, totals: [m("3000.00"), m("4000.00"), m("7000.00")], percents: ["", "", ""], groups: [] },
        { key: "net_profit", title: "Net profit", measure: true, totals: [m("-500.00"), m("3190.00"), m("2690.00")], percents: ["", "", ""], groups: [] },
      ],
    };
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/reports/statements/pl": { status: 200, body: byMonth } });
    renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant, mode: "expert" });
    expect(screen.queryByRole("img", { name: /The trend, month by month/ })).toBeNull(); // a single column draws no trend
    fireEvent.click(await screen.findByLabelText("A column per month"));
    const plot = await screen.findByRole("img", { name: /The trend, month by month\. Net profit went from −500\.00 in Sept 2026 to 3,190\.00 in Oct 2026\./ });
    expect(plot).toBeTruthy();
    await vi.waitFor(() => expect(drawn.option).not.toBeNull());
    expect(drawn.option!.series.map((x) => [x.name, x.data])).toEqual([
      ["Revenue", [4000, 5000]],
      ["Gross profit", [3000, 4000]],
      ["Net profit", [-500, 3190]],
    ]);
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("radio", { name: "Table" }));
    const table = screen.getByRole("table", { name: "The trend, month by month" });
    expect(within(table).getAllByRole("row").map((r) => r.textContent)).toEqual([
      "MonthRevenueGross profitNet profit",
      "Sept 20264,000.003,000.00−500.00",
      "Oct 20265,000.004,000.003,190.00",
    ]);
  });
});

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
  it("opens the export dialog, downloads the format chosen at the revision shown, says when it cannot, and asks again for an expired export; axe finds nothing", async () => {
    const file = { export_id: "x1", file_name: "profit-and-loss-2026-10.pdf", media_type: "application/pdf", sha256: "ab", size_bytes: 3, rows: 1, ledger_revision: 42, content_base64: btoa("abc") };
    const expired = { id: "x9", report: "general_ledger", format: "xlsx", from: "2026-10-01", to: "2026-10-31", status: "expired", requested_at: "2026-10-30T09:00:00Z" };
    const exports = { status: 200, body: { items: [expired] } };
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/pl/export": { status: 200, body: file },
      "GET /v1/report-exports": exports,
    });
    const create = vi.fn(() => "blob:x");
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    renderPage(FinancialStatement, { path: "/reports/statements/pl", route: "/reports/statements/$kind", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Export" }));
    const dialog = await screen.findByRole("dialog", { name: "Export the profit and loss" });
    expect(within(dialog).getByText(/Exports what is on screen: 1 Oct 2026 to 31 Oct 2026, read at ledger revision 42/)).toBeTruthy();
    const formats = within(dialog).getByRole("radiogroup", { name: "Format" });
    expect(within(formats).getByRole("radio", { name: /Excel/ }).getAttribute("aria-checked")).toBe("true");
    await within(dialog).findByRole("list", { name: "My exports" });
    await noViolations(document.body);

    // The arrow keys move between the formats; the PDF downloads the file the server wrote.
    fireEvent.keyDown(within(formats).getByRole("radio", { name: /Excel/ }), { key: "ArrowRight" });
    expect(within(formats).getByRole("radio", { name: /PDF/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(within(dialog).getByRole("button", { name: "Export PDF" }));
    await vi.waitFor(() => expect(click).toHaveBeenCalled());
    const q = seen.find((s) => s.path === "/v1/reports/statements/pl/export")!.query;
    expect(q).toContain("format=pdf");
    expect(q).toContain("as_at_revision=42");
    expect(create).toHaveBeenCalled();
    await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    // A PDF the render service cannot make now says so, in the dialog.
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/pl/export": { status: 503, body: { code: "REPORT-08", title: "The PDF could not be made now", status: 503, detail: "The render service is busy.", fix: "Try again in a moment" } },
      "GET /v1/report-exports": exports,
      "POST /v1/report-exports": { status: 202, body: { ...expired, id: "x10", status: "pending" } },
    });
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    const again = await screen.findByRole("dialog", { name: "Export the profit and loss" });
    fireEvent.click(within(again).getByRole("radio", { name: /PDF/ }));
    fireEvent.click(within(again).getByRole("button", { name: "Export PDF" }));
    expect(await within(again).findByText("The profit and loss could not be exported")).toBeTruthy();

    // An expired export asks again for the same report.
    fireEvent.click(await within(again).findByRole("button", { name: "Ask again for General ledger" }));
    await vi.waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/report-exports")).toBe(true));
    expect(seen.find((x) => x.method === "POST" && x.path === "/v1/report-exports")!.body).toEqual({ report: "general_ledger", from: "2026-10-01", to: "2026-10-31", format: "xlsx" });
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
      "GET /v1/report-exports": { status: 200, body: { items: [] } },
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

// req: FR-RPT-09
// req: FR-RPT-10
describe("the accountant exports", () => {
  it("asks for an export with its days, lists mine and downloads a ready one; axe finds nothing", async () => {
    const ready = { id: "x1", report: "trial_balance", format: "csv", to: "2026-10-31", status: "ready", requested_at: "2026-10-31T09:00:00Z", file_name: "trial-balance-2026-10-31.csv",
      media_type: "text/csv", size_bytes: 3, rows: 1, sha256: "ab", ledger_revision: 42, ready_at: "2026-10-31T09:00:05Z", expires_at: "2026-11-01T09:00:05Z" };
    const failed = { id: "x2", report: "general_ledger", format: "xlsx", from: "2026-10-01", to: "2027-01-31", status: "failed", requested_at: "2026-10-31T09:00:00Z",
      why: "the range crosses the start of a financial year: export each year apart" };
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/reports/statements/pl": { status: 200, body: pl },
      "GET /v1/reports/statements/bs": { status: 200, body: pl },
      "GET /v1/reports/statements/cf": { status: 200, body: pl },
      "GET /v1/report-exports": { status: 200, body: { items: [ready, failed] } },
      "POST /v1/report-exports": { status: 202, body: { ...ready, id: "x3", status: "pending" } },
      "GET /v1/report-exports/x1/file": { status: 200, body: { export_id: "x1", file_name: ready.file_name, media_type: "text/csv", sha256: "ab", size_bytes: 3, rows: 1, ledger_revision: 42, content_base64: btoa("abc") } },
    });
    Object.assign(URL, { createObjectURL: vi.fn(() => "blob:x"), revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    renderPage(Reports, { path: "/reports", session: accountant });
    const list = await screen.findByRole("list", { name: "My exports" });
    expect(within(list).getByText(/Ready until/)).toBeTruthy();
    expect(within(list).getByText(/crosses the start of a financial year/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Report"), { target: { value: "journals" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-31" } });
    fireEvent.click(screen.getByRole("button", { name: "Prepare the export" }));
    await vi.waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/report-exports")).toBe(true));
    const sent = seen.find((x) => x.method === "POST" && x.path === "/v1/report-exports")!.body as Record<string, unknown>;
    expect(sent).toEqual({ report: "journals", from: "2026-10-01", to: "2026-10-31", format: "xlsx" });
    fireEvent.click(within(list).getByRole("button", { name: "Download trial-balance-2026-10-31.csv" }));
    await vi.waitFor(() => expect(click).toHaveBeenCalled());
  });
});
