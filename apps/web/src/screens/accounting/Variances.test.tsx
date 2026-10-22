import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PeriodClose } from "./PeriodClose";

type Variance = components["schemas"]["Variance"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const owner = { ...ownerSession, roles: ["owner"] } as Session;
const oct = { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "closed" as const, financial_year: 1, closed_revision: 40, version: 2 };
const nov = { id: "p11", starts_on: "2026-11-01", ends_on: "2026-11-30", status: "open" as const, financial_year: 1, version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const rent: Variance = {
  account_id: "a6100", code: "6100", name: "Rent", type: "expense",
  current: sgd("8000.00"), previous: sgd("5000.00"), change: sgd("3000.00"), change_percent: "60.0",
  needs_explanation: true, explained: false,
};
const sales: Variance = {
  account_id: "a4000", code: "4000", name: "Sales", type: "revenue",
  current: sgd("-12500.00"), previous: sgd("-12000.00"), change: sgd("-500.00"), change_percent: "-4.2",
  needs_explanation: false, explained: false,
};
const software: Variance = {
  account_id: "a6420", code: "6420", name: "Software subscriptions", type: "expense",
  current: sgd("2400.00"), previous: sgd("200.00"), change: sgd("2200.00"), change_percent: "1100.0",
  needs_explanation: true, explained: false,
  explanation: { id: "x1", explanation: "The annual licence renewed.", current: sgd("2390.00"), previous: sgd("200.00"), written_by: { kind: "user", id: "u1", name: "Ah Kow" }, written_at: "2026-12-01T01:00:00Z" },
};
const page = (items: Variance[], unexplained: number) => ({
  period: nov, previous_period: oct, threshold_amount: sgd("1000.00"), threshold_percent: "10", unexplained, items,
});
const at = { path: "/accounting/close/p11", route: "/accounting/close/$periodId" };
const close = {
  "GET /v1/periods/p11/close": { status: 200, body: { period: nov, you_may_attest: false }, headers: { ETag: `"1-open"` } },
  "GET /v1/periods/p11/checklist": { status: 200, body: { period: nov, template_version: 0, done: 0, skipped: 0, open: 0, tasks: [] } },
};

// req: FR-GL-14
// req: C.2 #57
describe("the close's variances", () => {
  it("lists the variances to explain against the previous month, and explains one for the totals shown; axe finds nothing", async () => {
    let explained = false;
    answer({
      ...close,
      "GET /v1/periods/p11/variances": () => ({
        status: 200,
        body: explained ? page([sales, { ...rent, explained: true, explanation: { id: "x2", explanation: "The office moved.", current: rent.current, previous: rent.previous, written_by: { kind: "user", id: "u3", name: "Grace Ho" }, written_at: "2026-12-02T01:00:00Z" } }, software], 1) : page([sales, rent, software], 2),
      }),
      "POST /v1/periods/p11/variances/a6100/explanation": () => {
        explained = true;
        return { status: 200, body: rent };
      },
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    expect(await screen.findByText("2 variances need an explanation before the close.")).toBeTruthy();
    expect(screen.getByText("More than SGD 1,000.00 and 10% against October 2026")).toBeTruthy();
    const table = screen.getByRole("table");
    expect(within(table).getByText("Rent")).toBeTruthy();
    expect(within(table).getByText("+60.0%")).toBeTruthy();
    expect(within(table).getByText("Needs an explanation")).toBeTruthy();
    // A stale explanation shows what it explained, and asks again.
    expect(within(table).getByText("The totals changed")).toBeTruthy();
    expect(within(table).getByText(/Explained for 200.00 and 2,390.00/)).toBeTruthy();
    // Sales moved within the thresholds: shown on request, as the trial balance signs it.
    expect(within(table).queryByText("Sales")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show every income and expense account (3)" }));
    expect(within(screen.getByRole("table")).getByText("Sales")).toBeTruthy();
    expect(screen.getByText("−12,500.00")).toBeTruthy();
    expect(screen.getByText("−4.2%")).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Explain 6100 Rent" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/October 2026: 5,000.00; November 2026: 8,000.00 SGD/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Explain" }));
    expect(await within(dialog).findByText(/Say why the account moved/)).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST")).toEqual([]);
    fireEvent.change(within(dialog).getByLabelText("Why it moved"), { target: { value: "  The office moved.  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Explain" }));
    expect(await screen.findByText("Explained 6100 Rent. Run the checks again so they see it.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "POST")!;
    expect(sent.body).toEqual({ current: sgd("8000.00"), previous: sgd("5000.00"), explanation: "The office moved." });
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText("1 variance needs an explanation before the close.")).toBeTruthy();
    expect(screen.getByText(/The office moved. · Grace Ho/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Explain 6100 Rent" })).toBeNull();
  });

  it("shows the refusal when the totals moved since the page read them", async () => {
    answer({
      ...close,
      "GET /v1/periods/p11/variances": { status: 200, body: page([rent], 1) },
      "POST /v1/periods/p11/variances/a6100/explanation": problem(409, "CLOSE-10", "The account's totals are now 8010.00 and 5000.00 SGD.", "Read the variances again."),
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Explain 6100 Rent" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why it moved"), { target: { value: "The office moved." } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Explain" }));
    expect(await within(dialog).findByText("The explanation was not saved")).toBeTruthy();
  });

  it("lets only those who run the checks explain, and says when there is nothing to compare", async () => {
    answer({ ...close, "GET /v1/periods/p11/variances": { status: 200, body: page([rent], 1) } });
    renderPage(PeriodClose, { ...at, session: owner });
    expect(await screen.findByText("Needs an explanation")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Explain/ })).toBeNull();

    answer({
      ...close,
      "GET /v1/periods/p11/variances": { status: 200, body: { period: nov, threshold_amount: sgd("1000.00"), threshold_percent: "10", unexplained: 0, items: [] } },
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    expect(await screen.findByText("No earlier month to compare: November 2026 has no variances to explain.")).toBeTruthy();
  });
});
