import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PeriodClose } from "./PeriodClose";

type Year = components["schemas"]["YearEnd"];

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const oct = { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open" as const, financial_year: 1, version: 1 };
const sep = { id: "p09", starts_on: "2027-09-01", ends_on: "2027-09-30", status: "open" as const, financial_year: 1, version: 1 };
const retained = { account_id: "a3100", code: "3100", name: "Retained earnings", start: sgd("0.00"), posted: sgd("-300.00"), end: sgd("-1250.00") };
// The first year so far: S$1,000.00 of income and S$50.00 of expenses, a S$950.00 profit; S$300.00 was posted to retained
// earnings (the opening balances), so S$1,250.00 goes into the next year.
const year: Year = {
  number: 1, starts_on: "2026-10-01", ends_on: "2027-09-30", periods: 12, periods_closed: 0, closed: false, last_period: sep,
  income: sgd("-1000.00"), expenses: sgd("50.00"), result: sgd("-950.00"), retained_earnings: retained,
};
const page = (y: Year) => ({
  "GET /v1/periods/p10/close": { status: 200, body: { period: oct, you_may_attest: false }, headers: { ETag: `"1-open"` } },
  "GET /v1/periods/p10/checklist": { status: 200, body: { period: oct, template_version: 0, done: 0, skipped: 0, open: 0, tasks: [] } },
  "GET /v1/periods/p10/variances": { status: 200, body: { period: oct, threshold_amount: sgd("1000.00"), threshold_percent: "10", unexplained: 0, items: [] } },
  "GET /v1/periods/p10/year-end": { status: 200, body: y },
});
const at = { path: "/accounting/close/p10", route: "/accounting/close/$periodId" };
const card = async () => within((await screen.findByRole("heading", { name: "Year end" })).closest("section")!);
const row = (c: ReturnType<typeof within>, name: string) => c.getByRole("rowheader", { name }).closest("tr")!.textContent;

// req: FR-GL-10
describe("the year end on the close page", () => {
  it("shows the year so far and the retained earnings it carries forward; axe finds nothing", async () => {
    answer(page(year));
    renderPage(PeriodClose, { ...at, session: controller });
    const c = await card();
    expect(await c.findByText("0 of 12 months closed")).toBeTruthy();
    expect(c.getByText((_, el) => el?.tagName === "P" && el.textContent === "Financial year 1: 1 Oct 2026 to 30 Sept 2027. It closes with September 2027.")).toBeTruthy();
    expect(c.getByRole("link", { name: "September 2027" }).getAttribute("href")).toBe("/accounting/close/p09");
    expect(c.getByText("Income so far").nextElementSibling!.textContent).toBe("SGD 1,000.00");
    expect(c.getByText("Expenses so far").nextElementSibling!.textContent).toBe("SGD 50.00");
    expect(c.getByText("Profit so far").nextElementSibling!.textContent).toBe("SGD 950.00");
    expect(c.getByText("3100 Retained earnings, in SGD")).toBeTruthy();
    expect(row(c, "At the start of the year")).toContain("0.00");
    expect(row(c, "Posted to it in the year")).toContain("300.00");
    expect(row(c, "The year's profit so far")).toContain("950.00");
    expect(row(c, "Into the next year")).toContain("1,250.00");
    expect(c.getByText(/Nothing is posted at year end/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("says when the year is closed, and shows a loss as a loss", async () => {
    answer(page({ ...year, periods: 1, periods_closed: 1, closed: true, last_period: { ...oct, status: "closed" }, income: sgd("-100.00"), expenses: sgd("250.00"),
      result: sgd("150.00"), retained_earnings: { ...retained, posted: sgd("0.00"), end: sgd("150.00") } }));
    renderPage(PeriodClose, { ...at, session: controller });
    const c = await card();
    expect(await c.findByText("Year closed")).toBeTruthy();
    expect(c.getByText(/Its last month is closed, so its figures are final\./)).toBeTruthy();
    expect(c.getByText("Loss").nextElementSibling!.textContent).toBe("SGD 150.00");
    expect(row(c, "The year's loss")).toContain("−150.00");
    expect(row(c, "Into the next year")).toContain("−150.00");
  });

  it("says this month's close closes the year, and warns when no account holds retained earnings", async () => {
    answer(page({ ...year, periods: 1, last_period: oct, retained_earnings: undefined }));
    renderPage(PeriodClose, { ...at, session: controller });
    const c = await card();
    expect(await c.findByText(/Closing this month closes the year\./)).toBeTruthy();
    expect(c.getByText("No account holds retained earnings")).toBeTruthy();
    expect(c.queryByRole("table")).toBeNull();
  });
});
