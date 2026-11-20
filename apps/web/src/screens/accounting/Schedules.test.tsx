import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Schedules } from "./Schedules";

type Report = components["schemas"]["Schedules"];
type Holding = components["schemas"]["ScheduleHolding"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const m = (amount: string) => ({ amount, currency: "SGD" });
const prepaid: Holding = {
  kind: "prepayment",
  account_id: "a-1210",
  account_code: "1210",
  account_name: "Prepayments",
  balance: m("1304.00"),
  schedules: m("654.00"),
  direct: m("600.00"),
  explained: [{ source_type: "manual_journal", amount: m("50.00") }],
  unexplained: m("0.00"),
  status: "explained",
  open: 1,
};
const deferred: Holding = {
  kind: "deferred_revenue",
  account_id: "a-2400",
  account_code: "2400",
  account_name: "Deferred revenue",
  balance: m("27000.00"),
  schedules: m("27100.00"),
  direct: m("0.00"),
  explained: [],
  unexplained: m("-100.00"),
  status: "differs",
  open: 1,
};
const report: Report = {
  as_at: "2026-10-31",
  currency: "SGD",
  holdings: [prepaid, deferred],
  schedules: [
    {
      kind: "prepayment",
      source_type: "bill",
      source_id: "b1",
      number: "BILL-0866",
      line: 1,
      description: "Jira, four months",
      account_id: "a-6100",
      account_code: "6100",
      account_name: "Software",
      total: m("1308.00"),
      recognised: m("654.00"),
      remaining: m("654.00"),
      months_posted: 2,
      months: [
        { month: "2026-09", date: "2026-10-14", amount: m("327.00"), status: "posted", entry_id: "e1" },
        { month: "2026-10", date: "2026-10-14", amount: m("327.00"), status: "posted", entry_id: "e2" },
        { month: "2026-11", date: "2026-11-01", amount: m("327.00"), status: "waiting" },
        { month: "2026-12", date: "2026-12-01", amount: m("327.00"), status: "projected" },
      ],
    },
    {
      kind: "deferred_revenue",
      source_type: "invoice",
      source_id: "i1",
      number: "INV-0118",
      line: 1,
      description: "Acme Analytics, annual platform fee",
      account_id: "a-4100",
      account_code: "4100",
      account_name: "Subscription revenue",
      total: m("36000.00"),
      recognised: m("9000.00"),
      remaining: m("27000.00"),
      months_posted: 3,
      months: [{ month: "2026-08", date: "2026-08-01", amount: m("3000.00"), status: "posted", entry_id: "e3" }],
    },
  ],
};

afterEach(() => vi.restoreAllMocks());

// req: FR-GL-03
// req: ENG-05
// req: INV-07
describe("the schedules page", () => {
  it("lists the schedules by kind, ties the holding accounts and shows a schedule's months; axe finds nothing", async () => {
    answer({ "GET /v1/reports/schedules": { status: 200, body: report } });
    renderPage(Schedules, { path: "/accounting/schedules", session: accountant });
    const table = await screen.findByRole("table", { name: /Schedules as at 31 Oct 2026/ });
    expect(within(table).getByText("Prepaid costs")).toBeTruthy();
    expect(within(table).getByText("Income received in advance")).toBeTruthy();
    expect(within(table).getByRole("link", { name: "BILL-0866" }).getAttribute("href")).toBe("/purchases/bills/b1");
    expect(within(table).getByRole("link", { name: "INV-0118" }).getAttribute("href")).toBe("/sales/invoices/i1");
    expect(within(table).getByText("Sept 2026–Dec 2026 · 2 of 4")).toBeTruthy();
    expect(screen.getByText("Ties to the ledger at 31 Oct 2026")).toBeTruthy();
    expect(screen.getByText(/600.00 entered on 1210 directly/)).toBeTruthy();
    expect(screen.getByText(/50.00 from manual journals/)).toBeTruthy();
    expect(screen.getByText("Differs by −100.00: the close refuses it")).toBeTruthy();
    const peek = screen.getByRole("complementary", { name: "Jira, four months" });
    expect(within(peek).getAllByText("Posted")).toHaveLength(2);
    expect(within(peek).getByText("Waiting")).toBeTruthy();
    expect(within(peek).getByText("Future")).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/reports/schedules")!.query).toContain("status=open");
    await noViolations(document.body);

    fireEvent.click(within(table).getByRole("button", { name: "Acme Analytics, annual platform fee" }));
    expect(await screen.findByRole("complementary", { name: "Acme Analytics, annual platform fee" })).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Show finished schedules"));
    await vi.waitFor(() => expect(seen.some((s) => s.path === "/v1/reports/schedules" && s.query.includes("status=all"))).toBe(true));
  });

  it("teaches how a schedule begins when nothing is spread", async () => {
    answer({ "GET /v1/reports/schedules": { status: 200, body: { ...report, holdings: [], schedules: [] } } });
    renderPage(Schedules, { path: "/accounting/schedules", session: accountant });
    expect(await screen.findByRole("heading", { name: "Nothing is spread over months" })).toBeTruthy();
    await noViolations(document.body);
  });
});

// req: FR-RPT-13
describe("the roll-forward tab on Schedules", () => {
  it("shows the schedules' movement and what each customer was billed, collected and recognised, with its check", async () => {
    const m = (amount: string) => ({ amount, currency: "SGD" });
    const row = (name: string, o: string, a: string, r: string, c: string) => ({ party_id: "c1", party_name: name, opening: m(o), added: m(a), released: m(r), reversed: m("0.00"), closing: m(c) });
    answer({
      "GET /v1/reports/schedules": { status: 200, body: report },
      "GET /v1/reports/deferral-roll-forward": {
        status: 200,
        body: {
          currency: "SGD", from: "2026-10-01", to: "2026-10-31",
          holdings: [{ kind: "deferred_revenue", account_code: "2400", account_name: "Deferred revenue", rows: [row("Acme Analytics Pte Ltd", "0.00", "1200.00", "800.00", "400.00")], total: row("Total", "0.00", "1200.00", "800.00", "400.00"), ledger_balance: m("400.00"), direct: m("0.00"), unexplained: m("0.00"), ties: true }],
          customers: [
            { party_id: "c1", party_name: "Acme Analytics Pte Ltd", billed: m("1100.00"), collected: m("400.00"), recognised: m("700.00"), released: m("800.00"), direct: m("-100.00"), deferred_closing: m("400.00"), receivable_movement: m("700.00"), other_movement: m("0.00"), unexplained: m("0.00"), explained: true },
            { party_id: "c2", party_name: "Beta Pte Ltd", billed: m("50.00"), collected: m("0.00"), recognised: m("50.00"), released: m("0.00"), direct: m("50.00"), deferred_closing: m("0.00"), receivable_movement: m("80.00"), other_movement: m("30.00"), unexplained: m("30.00"), explained: false },
          ],
        },
      },
    });
    renderPage(Schedules, { path: "/accounting/schedules", session: accountant, mode: "expert" });
    fireEvent.click(await screen.findByRole("tab", { name: "Roll-forward" }));
    const deferred = await screen.findByRole("table", { name: "Deferred revenue by customer" });
    expect(within(deferred).getAllByText("1,200.00").length).toBeGreaterThan(0);
    expect(screen.getByText(/Ties to 2400 in the ledger/)).toBeTruthy();
    const customers = screen.getByRole("table", { name: "Customers billed, collected and recognised" });
    expect(within(customers).getByText("Billed less collected explains it")).toBeTruthy();
    expect(within(customers).getByText(/30\.00 not explained by documents/)).toBeTruthy();
    expect(seen.some((s) => s.path === "/v1/reports/deferral-roll-forward" && /from=\d{4}-\d{2}/.test(s.query))).toBe(true);
    await noViolations(document.body);
  });
});
