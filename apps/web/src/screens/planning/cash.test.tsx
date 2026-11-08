import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { CashAhead } from "./CashAhead";

type Projection = components["schemas"]["CashProjection"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const m = (amount: string) => ({ amount, currency: "SGD" });
const day = (date: string, close: string, out = "0.00", inflow = "0.00") => ({ date, in: m(inflow), out: m(out), close: m(close) });

// A 30-day projection as the API answers it: 20,300.00 today, the batch on the 5th, a bill on the 13th that takes the
// balance to 20,018.50, below the buffer of 58,200.00 from today.
const projection: Projection = {
  today: "2026-11-02",
  days: 30,
  currency: "SGD",
  opening: m("20300.00"),
  lowest: m("20018.50"),
  lowest_on: "2026-11-13",
  below_buffer_on: "2026-11-02",
  buffer: { months: 6, average_burn: m("9700.00"), line: m("58200.00") },
  movements: [
    { key: "inv-1", kind: "invoice", id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01", number: "INV-0007", contact_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
      contact_name: "Late Payer Pte Ltd", due_date: "2026-11-10", expected_on: "2026-11-17", basis: "history", days_late: 7, amount: m("100.00") },
    { key: "pb-1", kind: "payment_batch", id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d03", number: "PB-0001", expected_on: "2026-11-05", basis: "scheduled", amount: m("-109.00") },
    { key: "bill-1", kind: "bill", id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d04", number: "BILL-0002", contact_name: "Northwind Analytics Pte Ltd",
      due_date: "2026-11-13", expected_on: "2026-11-13", basis: "due", amount: m("-218.00") },
  ],
  daily: [day("2026-11-02", "20300.00"), day("2026-11-05", "20191.00", "109.00"), day("2026-11-13", "20018.50", "218.00"), day("2026-12-01", "20118.50", "0.00", "100.00")],
  weekly: [],
  not_included: [
    { what: "Pay runs and CPF", why: "they join with payroll" },
    { what: "GST returns", why: "they join once the company is registered for GST" },
  ],
};

// ECharts draws on a canvas, which jsdom lacks.
vi.mock("../../components/chart/echarts", () => ({
  init: () => ({ setOption: () => undefined, dispatchAction: () => undefined, getDataURL: () => "", resize: () => undefined, dispose: () => undefined }),
}));

// req: FR-PLN-09
// req: FR-PLN-05
// req: FR-PLN-08
// req: QA-UI-04
describe("Cash ahead", () => {
  it("shows the lowest balance, the buffer crossed, what moves it and what is not included, in Basic without what-ifs; axe finds nothing", async () => {
    answer({ "GET /v1/cash-projection": { status: 200, body: projection } });
    renderPage(CashAhead, { path: "/planning/cash", session: accountant, mode: "basic" });
    expect(await screen.findByRole("heading", { name: "Cash ahead" })).toBeTruthy();
    await screen.findByText("Pay runs and CPF");
    expect(seen[0]!.query).toContain("days=60");
    const lowest = document.querySelector('[data-cash="lowest"]') as HTMLElement;
    expect(lowest.querySelector(".fy-tile__value")!.textContent).toBe("20,018.50 SGD");
    expect(within(lowest).getByText("On 13 Nov 2026")).toBeTruthy();
    expect(screen.getByText("Cash is below the buffer today")).toBeTruthy();
    expect(within(document.querySelector('[data-cash="buffer"]') as HTMLElement).getByText("Below from 2 Nov 2026")).toBeTruthy();
    expect(screen.getByText("Sandbox: nothing here posts")).toBeTruthy();
    const table = screen.getByRole("table", { name: /The receipts and payments expected/ });
    expect(within(table).getByText("INV-0007, Late Payer Pte Ltd, expected")).toBeTruthy();
    expect(within(table).getByText("Due 10 Nov 2026; pays 7 days late on average")).toBeTruthy();
    expect(within(table).getByText("Payment batch PB-0001")).toBeTruthy();
    expect(within(table).getByText("−218.00")).toBeTruthy();
    expect(within(table).queryByRole("columnheader", { name: "What if" })).toBeNull();
    expect(screen.getByText("Pay runs and CPF")).toBeTruthy();
    await noViolations(document.body);
  });

  it("asks for another range or the 13 weeks", async () => {
    answer({ "GET /v1/cash-projection": { status: 200, body: projection } });
    renderPage(CashAhead, { path: "/planning/cash", session: accountant, mode: "basic" });
    await screen.findByText("Pay runs and CPF");
    fireEvent.click(screen.getByRole("button", { name: "13 weeks" }));
    await vi.waitFor(() => expect(seen.some((s) => s.query.includes("days=91"))).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "180 days" }));
    await vi.waitFor(() => expect(seen.some((s) => s.query.includes("days=180"))).toBe(true));
  });

  it("asks a what-if in Expert, which saves nothing, and resets it", async () => {
    answer({
      "GET /v1/cash-projection": { status: 200, body: projection },
      "POST /v1/cash-projection:what-if": { status: 200, body: { ...projection, below_buffer_on: undefined, buffer: { months: 6, line: m("10000.00") } } },
    });
    renderPage(CashAhead, { path: "/planning/cash", session: accountant, mode: "expert" });
    await screen.findByText("Pay runs and CPF");
    fireEvent.change(screen.getByLabelText("Move BILL-0002, Northwind Analytics Pte Ltd, to pay to"), { target: { value: "2026-11-30" } });
    await vi.waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    expect(seen.find((s) => s.method === "POST")!.body).toEqual({ days: 60, moves: [{ key: "bill-1", to: "2026-11-30" }] });
    expect(await screen.findByText("What-if: 1 change")).toBeTruthy();
    expect(await screen.findByText("Never below")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Leave out Payment batch PB-0001" }));
    await vi.waitFor(() =>
      expect(seen.filter((s) => s.method === "POST").at(-1)!.body).toEqual({ days: 60, moves: [{ key: "bill-1", to: "2026-11-30" }, { key: "pb-1", leave_out: true }] }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    await vi.waitFor(() => expect(screen.queryByText(/What-if:/)).toBeNull());
  });
});
