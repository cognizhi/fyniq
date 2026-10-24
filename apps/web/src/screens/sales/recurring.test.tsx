import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { RecurringInvoicePage, checkRecurringInvoice } from "./RecurringInvoicePage";
import { RecurringInvoices, repeatsOf } from "./RecurringInvoices";

type RecurringInvoice = components["schemas"]["RecurringInvoice"];

const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const standard: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "standard", version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const reads = {
  "GET /v1/accounts": { status: 200, body: { items: [{ id: "a4010", code: "4010", name: "Software revenue", type: "revenue", active: true, group: false, version: 1 }] } },
  "GET /v1/contacts": { status: 200, body: { items: [{ id: "c1", legal_name: "Acme Analytics Pte Ltd", customer: true, vendor: false, active: true, version: 1 }] } },
  "GET /v1/items": { status: 200, body: { items: [] } },
  "GET /v1/sales/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", rate: "0.09", codes: [{ code: "SR", description: "Standard-rated supply", rated: true }] } },
};
const monthly: RecurringInvoice = {
  id: "r1",
  name: "Analytics subscription, Acme",
  status: "active",
  schedule: { every_months: 1, day_of_month: 1, starts_on: "2026-11-01" },
  next_on: "2026-11-01",
  customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
  currency: "SGD",
  lines: [{ description: "Analytics platform", quantity: "1", unit_price: "800.00", account_id: "a4010", gst_code: "SR", net: sgd("800.00") }],
  net: sgd("800.00"),
  version: 1,
  content_version: 1,
  edited_at: "2026-10-24T09:00:00Z",
  checks: [{ severity: "warning", code: "RECURRING-UNAPPROVED", title: "Awaiting approval", detail: "It is set to issue and send but its content is not approved." }],
  mode: "issue_and_send",
  issues_and_sends: false,
};
const weekly: RecurringInvoice = { ...monthly, id: "r2", name: "Weekly cleaning", schedule: { every_weeks: 2, starts_on: "2026-11-02" }, mode: "draft", checks: [] };
const read = (r: RecurringInvoice) => ({ status: 200, body: r, headers: { ETag: `"${r.version}"` } });

// req: FR-AR-07
describe("Sales › Recurring", () => {
  it("says each schedule in words, weekly to yearly", () => {
    expect(repeatsOf(monthly.schedule)).toBe("Monthly on the 1st");
    expect(repeatsOf(weekly.schedule)).toBe("Every two weeks on Monday");
  });

  it("lists the recurring invoices with what the scheduler does; axe finds nothing", async () => {
    answer({ "GET /v1/recurring-invoices": { status: 200, body: { items: [monthly, weekly] } } });
    const { container } = renderPage(RecurringInvoices, { path: "/sales/recurring", session: bookkeeper });
    const table = await screen.findByRole("table", { name: "Recurring invoices" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Issue and send, awaiting approval");
    expect(rows[2]!.textContent).toContain("Makes drafts");
    expect(rows[2]!.textContent).toContain("Every two weeks on Monday");
    await noViolations(container);
  });
});

// req: FR-AR-07
// req: FR-AUT-01
describe("A recurring invoice", () => {
  it("reads its schedule: a day of the month for a monthly one, none for a weekly one", () => {
    const line = { key: "l1", item_id: "", description: "x", quantity: "1", unit_price: "800", discount: "", account_id: "a4010", gst_code: "SR", spread_months: "", spread_from: "" };
    const form = { name: "Retainer", every: "m1" as const, day_of_month: "31", starts_on: "2026-11-01", ends_on: "", customer_id: "c1", reference: "", currency: "SGD", notes: "", mode: "draft" as const, lines: [line] };
    expect(checkRecurringInvoice(form, "SGD").input?.schedule).toEqual({ every_months: 1, day_of_month: 31, starts_on: "2026-11-01", ends_on: undefined });
    expect(checkRecurringInvoice({ ...form, every: "w2", day_of_month: "" }, "SGD").input?.schedule).toEqual({ every_weeks: 2, starts_on: "2026-11-01", ends_on: undefined });
    expect(checkRecurringInvoice({ ...form, day_of_month: "32" }, "SGD").errors.day_of_month).toBe("Enter a day from 1 to 31.");
    expect(checkRecurringInvoice({ ...form, name: " " }, "SGD").errors.name).toBe("Name it, such as Analytics subscription, Acme.");
  });

  it("set to issue and send, asks another person to approve its content; axe finds nothing", async () => {
    const sent = answer({ ...reads, "GET /v1/recurring-invoices/r1": read(monthly), "POST /v1/recurring-invoices/r1/approval": { status: 202, body: { id: "ap1" } } });
    const { container } = renderPage(RecurringInvoicePage, { path: "/sales/recurring/r1", route: "/sales/recurring/$recurringInvoiceId", session: bookkeeper, organisation: standard });
    expect(await screen.findByText(/not approved/)).toBeTruthy();
    expect((screen.getByLabelText("What the scheduler does") as HTMLSelectElement).value).toBe("issue_and_send");
    await noViolations(container);
    fireEvent.click(screen.getAllByRole("button", { name: "Ask for approval" })[0]!);
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/recurring-invoices/r1/approval")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/recurring-invoices/r1/approval")!.headers.get("If-Match")).toBe(`"1"`);
  });

  it("is saved with its mode", async () => {
    const sent = answer({ ...reads, "GET /v1/recurring-invoices/r2": read(weekly), "PUT /v1/recurring-invoices/r2": { status: 200, body: { ...weekly, version: 2 }, headers: { ETag: `"2"` } } });
    renderPage(RecurringInvoicePage, { path: "/sales/recurring/r2", route: "/sales/recurring/$recurringInvoiceId", session: bookkeeper, organisation: standard });
    await screen.findByRole("option", { name: "Acme Analytics Pte Ltd" });
    await screen.findByRole("option", { name: /4010 Software revenue/ });
    await screen.findByRole("option", { name: /^SR:/ });
    fireEvent.change(screen.getByLabelText("What the scheduler does"), { target: { value: "issue_and_send" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]!);
    await waitFor(() => expect(sent.some((s) => s.method === "PUT" && s.path === "/v1/recurring-invoices/r2")).toBe(true));
    expect(sent.find((s) => s.method === "PUT")!.body).toMatchObject({ mode: "issue_and_send", schedule: { every_weeks: 2, starts_on: "2026-11-02" }, customer_id: "c1" });
  });
});
