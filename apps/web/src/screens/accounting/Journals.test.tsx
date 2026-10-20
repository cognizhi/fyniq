import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Journals } from "./Journals";
import { Periods } from "./Periods";

type Summary = components["schemas"]["JournalSummary"];
type Period = components["schemas"]["Period"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const sum = (id: string, extra: Partial<Summary>): Summary => ({ id, status: "draft", version: 1, lines: 2, debits: [{ amount: "1200.00", currency: "SGD" }], ...extra });

// req: FR-GL-01
describe("the journals list", () => {
  it("lists journals with their totals, people and status, narrowed by status and text; axe finds nothing", async () => {
    answer({
      "GET /v1/journals": (_b, req) => {
        const q = new URL(req.url).searchParams;
        if (q.get("status") === "posted") return { status: 200, body: { items: [sum("j2", { status: "posted", number: "JNL-0001", memo: "Rent" })] } };
        return {
          status: 200,
          body: {
            items: [
              sum("j1", { date: "2026-10-31", memo: "Audit fee accrual", prepared_by: { kind: "user", id: "u1", name: "Ah Kow" } }),
              sum("j2", { status: "posted", number: "JNL-0001", date: "2026-10-20", memo: "Rent", posted_by: { kind: "user", id: "u2", name: "Wei Ling" } }),
              sum("j3", { debits: [{ amount: "100.00", currency: "USD" }, { amount: "135.00", currency: "SGD" }] }),
            ],
            next_cursor: "more",
          },
        };
      },
    });
    renderPage(Journals, { path: "/accounting/journals", session: accountant });
    const table = await screen.findByRole("table", { name: "Journals" });
    expect(within(table).getByText("Audit fee accrual")).toBeTruthy();
    expect(within(table).getByRole("link", { name: "JNL-0001" }).getAttribute("href")).toBe("/accounting/journals/j2");
    expect(within(table).getByText("100.00 USD · 135.00 SGD")).toBeTruthy();
    expect(within(table).getByText("Wei Ling")).toBeTruthy();
    expect(screen.getByRole("link", { name: "New journal" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Show more" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Posted" }));
    await waitFor(() => expect(screen.queryByText("Audit fee accrual")).toBeNull());
    fireEvent.change(screen.getByLabelText("Find a journal"), { target: { value: "rent" } });
    await waitFor(() => expect(seen.some((s) => s.query.includes("q=rent") && s.query.includes("status=posted"))).toBe(true));
  });

  it("teaches what a journal is when there are none, and offers no button to an Owner", async () => {
    answer({ "GET /v1/journals": { status: 200, body: { items: [] } } });
    renderPage(Journals, { path: "/accounting/journals" });
    expect(await screen.findByText("No journals yet")).toBeTruthy();
    expect(screen.getByText("An Accountant or a Tax agent prepares journals.")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "New journal" })).toBeNull();
  });
});

const period = (id: string, starts: string, ends: string, extra: Partial<Period> = {}): Period => ({ id, starts_on: starts, ends_on: ends, status: "open", financial_year: 1, version: 1, ...extra });

// req: FR-ORG-02
// req: FR-GL-08
describe("periods and close", () => {
  it("opens the first financial year; axe finds nothing", async () => {
    let opened = false;
    const year = { number: 1, starts_on: "2026-10-15", ends_on: "2027-03-31", periods: 6, may_open: true };
    answer({
      "GET /v1/periods": () =>
        opened
          ? { status: 200, body: { items: [period("p1", "2026-10-15", "2026-10-31"), period("p2", "2026-11-01", "2026-11-30", { status: "closed" })], next_year: { ...year, number: 2, starts_on: "2027-04-01", ends_on: "2028-03-31", periods: 12, may_open: false } } }
          : { status: 200, body: { items: [], next_year: year } },
      "POST /v1/periods:open-year": () => {
        opened = true;
        return { status: 201, body: { year, periods: [period("p1", "2026-10-15", "2026-10-31")] } };
      },
    });
    renderPage(Periods, { path: "/accounting/close", session: accountant });
    expect(await screen.findByText("Open the first financial year")).toBeTruthy();
    expect(screen.getByText(/15 Oct 2026 to 31 Mar 2027, 6 periods/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Open the year" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Open the year" }));
    expect(await screen.findByText("The year is open: 1 period.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.body).toEqual({ starts_on: "2026-10-15" });
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    const table = await screen.findByRole("table", { name: "Financial year 1" });
    expect(within(table).getByText("October 2026, from 15 Oct 2026")).toBeTruthy();
    expect(within(table).getByText("Closed")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open the year" }).hasAttribute("disabled")).toBe(true); // the second year starts too far ahead
  });

  it("asks for the books start first, and lets an Owner look but not open", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: [] } } });
    renderPage(Periods, { path: "/accounting/close" });
    expect(await screen.findByText("Set when the books start first")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to Settings › Organisation" }).getAttribute("href")).toBe("/settings/organisation");

    answer({ "GET /v1/periods": { status: 200, body: { items: [], next_year: { number: 1, starts_on: "2026-10-01", ends_on: "2026-12-31", periods: 3, may_open: true } } } });
    renderPage(Periods, { path: "/accounting/close", session: { ...ownerSession, roles: ["owner"] } as Session });
    expect(await screen.findByText("An Accountant or a Controller opens the year.")).toBeTruthy();
  });
});
