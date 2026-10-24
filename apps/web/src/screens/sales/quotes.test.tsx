import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { QuotePage, check } from "./QuotePage";
import { Quotes } from "./Quotes";

type Quote = components["schemas"]["Quote"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const standard: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "standard", version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const reads = {
  "GET /v1/accounts": { status: 200, body: { items: [{ id: "a4010", code: "4010", name: "Software revenue", type: "revenue", active: true, group: false, version: 1 }] } },
  "GET /v1/contacts": { status: 200, body: { items: [{ id: "c1", legal_name: "Acme Analytics Pte Ltd", customer: true, vendor: false, active: true, version: 1 }] } },
  "GET /v1/items": { status: 200, body: { items: [] } },
  "GET /v1/sales/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", rate: "0.09", codes: [{ code: "SR", description: "Standard-rated supply", rated: true }] } },
};
const draft: Quote = {
  id: "q1",
  kind: "quote",
  status: "draft",
  version: 1,
  customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
  date: "2026-10-14",
  valid_until: "2026-11-13",
  currency: "SGD",
  lines: [{ description: "Analytics platform", quantity: "1", unit_price: "100.00", account_id: "a4010", gst_code: "SR", net: sgd("100.00") }],
  gst_charged: true,
  gst_subtotals: [{ gst_code: "SR", rate: "0.09", net: sgd("100.00"), gst: sgd("9.00") }],
  net: sgd("100.00"),
  gst: sgd("9.00"),
  total: sgd("109.00"),
  checks: [],
  invoicing: [],
  invoices: [],
  delivery: "not_sent",
  sends: [],
  links: [],
};
const sent: Quote = { ...draft, number: "QT-0001", status: "sent", invoicing: [{ line: 1, quoted: "1", invoiced: "0", drafted: "0", left: "1" }] };
const accepted: Quote = {
  ...sent,
  status: "accepted",
  delivery: "viewed",
  answer: { outcome: "accepted", date: "2026-10-20", name: "Jane Tan", note: "Accepted online", recorded_at: "2026-10-20T09:00:00Z" },
};
const read = (q: Quote) => ({ status: 200, body: q, headers: { ETag: `"${q.version}-${q.status}"` } });

// req: FR-AR-01
// req: FR-AR-02
describe("Sales › Quotes and orders", () => {
  it("lists the quotes with their status and email, the pipeline, and those ready to invoice; axe finds nothing", async () => {
    answer({
      "GET /v1/quotes": { status: 200, body: { items: [draft, sent, accepted] } },
      "GET /v1/quotes/pipeline": { status: 200, body: { as_at: "2026-10-24", quotes: { count: 2, total: sgd("218.00") }, orders: { count: 0, total: sgd("0.00") }, total: sgd("218.00") } },
    });
    const { container } = renderPage(Quotes, { path: "/sales/quotes", session: accountant });
    const table = await screen.findByRole("table", { name: "Quotes" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Draft");
    expect(rows[2]!.textContent).toContain("QT-0001");
    expect(rows[3]!.textContent).toContain("Accepted");
    expect(rows[3]!.textContent).toContain("Viewed");
    expect(await screen.findByText(/218.00 SGD open: 2 quotes/)).toBeTruthy();
    expect(screen.getByText("1 accepted quote shown is ready to invoice")).toBeTruthy();
    await noViolations(container);
  });
});

// req: FR-AR-01
// req: FR-AR-02
// req: FR-AR-06
describe("A quote", () => {
  it("needs its validity date; an order may name none", () => {
    const form = { customer_id: "c1", reference: "", date: "2026-10-14", valid_until: "", currency: "SGD", fx_rate: "", notes: "" };
    const line = { key: "l1", item_id: "", description: "x", quantity: "1", unit_price: "100", discount: "", account_id: "a4010", gst_code: "SR", spread_months: "", spread_from: "" };
    expect(check({ ...form, lines: [line] }, "SGD").errors.valid_until).toBe("Enter the last day the quote is valid.");
    expect(check({ ...form, valid_until: "2026-10-01", lines: [line] }, "SGD").errors.valid_until).toBe("Enter a day on or after the quote's date.");
    expect(check({ ...form, lines: [line] }, "SGD", true).draft).toMatchObject({ customer_id: "c1", lines: [{ quantity: "1", unit_price: "100" }] });
  });

  it("is sent from its draft", async () => {
    const sentNow = answer({ ...reads, "GET /v1/quotes/q1": read(draft), "POST /v1/quotes/q1/send": { status: 200, body: sent, headers: { ETag: `"1-sent"` } } });
    renderPage(QuotePage, { path: "/sales/quotes/q1", route: "/sales/quotes/$quoteId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Send" }));
    await waitFor(() => expect(sentNow.some((s) => s.method === "POST" && s.path === "/v1/quotes/q1/send")).toBe(true));
  });

  it("records the customer's acceptance from a sent quote", async () => {
    const calls = answer({ ...reads, "GET /v1/quotes/q1": read(sent), "POST /v1/quotes/q1/accept": { status: 200, body: accepted } });
    renderPage(QuotePage, { path: "/sales/quotes/q1", route: "/sales/quotes/$quoteId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Record accepted…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Who answered (optional)"), { target: { value: "Jane Tan" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record accepted" }));
    await waitFor(() => expect(calls.some((s) => s.method === "POST" && s.path === "/v1/quotes/q1/accept")).toBe(true));
    expect(calls.find((s) => s.path === "/v1/quotes/q1/accept")!.body).toMatchObject({ name: "Jane Tan" });
  });

  it("shows the answer and makes the invoice of an accepted quote, for a preparer too; axe finds nothing", async () => {
    const calls = answer({ ...reads, "GET /v1/quotes/q1": read(accepted), "POST /v1/quotes/q1/invoice": { status: 201, body: { id: "v7" } } });
    const { container } = renderPage(QuotePage, { path: "/sales/quotes/q1", route: "/sales/quotes/$quoteId", session: bookkeeper, organisation: standard });
    expect(await screen.findByText(/Accepted on 20 Oct 2026 by Jane Tan/)).toBeTruthy();
    const invoicing = screen.getByRole("heading", { name: "Invoicing" }).closest("section")!;
    expect(within(invoicing).getAllByRole("row")[1]!.textContent).toBe("Line 11001");
    // A Bookkeeper makes the invoice, but neither emails nor cancels.
    expect(screen.queryByRole("button", { name: "Email…" })).toBeNull();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Make the invoice" }));
    await waitFor(() => expect(calls.some((s) => s.method === "POST" && s.path === "/v1/quotes/q1/invoice")).toBe(true));
  });

  it("is cancelled only with a reason", async () => {
    const calls = answer({ ...reads, "GET /v1/quotes/q1": read(sent), "POST /v1/quotes/q1/cancel": { status: 200, body: { ...sent, status: "cancelled" } } });
    renderPage(QuotePage, { path: "/sales/quotes/q1", route: "/sales/quotes/$quoteId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Cancel…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(await within(dialog).findByText("Say why: the audit trail keeps the reason.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why it is cancelled"), { target: { value: "The customer chose another supplier" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(calls.some((s) => s.method === "POST" && s.path === "/v1/quotes/q1/cancel")).toBe(true));
  });
});
