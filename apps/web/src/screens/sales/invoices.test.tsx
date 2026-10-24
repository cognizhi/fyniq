import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { InvoicePage, check } from "./InvoicePage";
import { Invoices } from "./Invoices";
import { receiptOf } from "./Receipts";

type Invoice = components["schemas"]["Invoice"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const standard: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "standard", version: 1 };
const founder: Organisation = { ...standard, team_mode: "founder" };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const accounts = {
  status: 200,
  body: {
    items: [
      { id: "a4010", code: "4010", name: "Software revenue", type: "revenue", active: true, group: false, version: 1 },
      { id: "a1100", code: "1100", name: "Accounts receivable", type: "asset", active: true, group: false, version: 1 },
      { id: "a2200", code: "2200", name: "GST control", type: "liability", active: true, group: false, version: 1 },
      { id: "a1010", code: "1010", name: "DBS current", type: "asset", active: true, group: false, version: 1, money_account: true, currency: "SGD" },
    ],
  },
};
const customers = { status: 200, body: { items: [{ id: "c1", legal_name: "Acme Analytics Pte Ltd", customer: true, vendor: false, active: true, version: 1 }] } };
const items = {
  status: 200,
  body: { items: [{ id: "i1", code: "PLAT", name: "Analytics platform", type: "service", active: true, version: 1, revenue_account_id: "a4010", gst_code: "SR", prices: [{ currency: "SGD", unit_price: "1500.00" }] }] },
};
const codes = {
  status: 200,
  body: {
    version: "sg-gst@2026.1",
    rate: "0.09",
    codes: [
      { code: "SR", description: "Standard-rated supply", rated: true },
      { code: "ZR", description: "Zero-rated supply", rated: false },
    ],
  },
};
const states = (extra: Partial<Invoice["states"]> = {}): Invoice["states"] => ({ approval: "draft", ledger: "not_posted", delivery: "not_sent", settlement: "open", due: "not_due", ...extra });
const draft: Invoice = {
  id: "v1",
  kind: "invoice",
  status: "draft",
  version: 1,
  customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
  reference: "PO-88213",
  date: "2026-10-14",
  due_date: "2026-11-13",
  currency: "SGD",
  lines: [{ item_id: "i1", description: "Analytics platform", quantity: "2", unit_price: "1500.00", account_id: "a4010", gst_code: "SR", net: sgd("3000.00") }],
  gst_charged: true,
  gst_subtotals: [{ gst_code: "SR", rate: "0.09", net: sgd("3000.00"), gst: sgd("270.00") }],
  net: sgd("3000.00"),
  gst: sgd("270.00"),
  total: sgd("3270.00"),
  deposits: [],
  receivable: sgd("3270.00"),
  states: states(),
  releases: [],
  checks: [{ severity: "warning", code: "INVOICE-LATE", field: "date", message: "the tax invoice is dated more than 30 days before today", fix: "Check the date of supply" }],
  ledger_impact: [
    { line_no: 1, account_id: "a1100", amount: sgd("3270.00"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("3270.00") },
    { line_no: 2, account_id: "a4010", amount: sgd("-3000.00"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("-3000.00") },
    { line_no: 3, account_id: "a2200", amount: sgd("-270.00"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("-270.00") },
  ],
  sends: [],
  links: [],
};
const issued: Invoice = { ...draft, number: "INV-0042", status: "posted", states: states({ approval: "approved", ledger: "posted", due: "overdue" }), checks: [], open: sgd("3270.00") };
const emailed: Invoice = {
  ...issued,
  states: states({ approval: "approved", ledger: "posted", delivery: "sent" }),
  sends: [
    {
      id: "s1",
      version: 1,
      to: "ap@acme.example",
      sent_at: "2026-10-24T09:30:00Z",
      delivery: "sent",
      message_status: "queued",
      evidence_id: "e1",
      pdf_sha256: "a".repeat(64),
      template_source: "platform",
      template_version: 0,
    },
  ],
};
const read = (i: Invoice) => ({ status: 200, body: i, headers: { ETag: `"${i.version}-${i.status}"` } });
const reads = { "GET /v1/accounts": accounts, "GET /v1/contacts": customers, "GET /v1/items": items, "GET /v1/sales/gst-codes": codes, "GET /v1/invoices/v1/evidence": { status: 200, body: { items: [] } } };

// req: FR-AR-03
describe("Sales › Invoices", () => {
  it("lists the invoices with their states, and an overdue one flagged; axe finds nothing", async () => {
    answer({ "GET /v1/invoices": { status: 200, body: { items: [draft, emailed, issued] } } });
    const { container } = renderPage(Invoices, { path: "/sales/invoices", session: accountant });
    const table = await screen.findByRole("table", { name: "Invoices" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Acme Analytics Pte Ltd");
    expect(rows[1]!.textContent).toContain("Draft");
    expect(rows[2]!.textContent).toContain("INV-0042");
    expect(rows[2]!.textContent).toContain("Sent");
    expect(rows[3]!.textContent).toContain("Overdue");
    expect(screen.getByText("1 invoice shown is overdue")).toBeTruthy();
    expect(screen.getByRole("link", { name: "New invoice" })).toBeTruthy();
    await noViolations(container);
  });

  it("offers no new invoice to a person who only reads them", async () => {
    answer({ "GET /v1/invoices": { status: 200, body: { items: [] } } });
    renderPage(Invoices, { path: "/sales/invoices", session: auditor });
    expect(await screen.findByText("No invoices yet")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "New invoice" })).toBeNull();
    expect(screen.getByText("A Bookkeeper, an AR specialist or an Accountant prepares invoices.")).toBeTruthy();
  });
});

// req: FR-AR-03
// req: FR-AR-06
// req: FM-1
// req: FM-2
describe("An invoice", () => {
  it("is entered from an item as quantity × price and saved as a draft", async () => {
    const sent = answer({ ...reads, "POST /v1/invoices": { status: 201, body: { ...draft, id: "v9" } } });
    renderPage(InvoicePage, { path: "/sales/invoices/new", session: bookkeeper, organisation: standard });
    await screen.findByRole("option", { name: "Acme Analytics Pte Ltd" });
    fireEvent.change(screen.getByLabelText("Customer"), { target: { value: "c1" } });
    fireEvent.change(screen.getByLabelText("Customer's reference"), { target: { value: "PO-88213" } });
    fireEvent.change(screen.getByLabelText("Invoice date"), { target: { value: "2026-10-14" } });
    await screen.findByRole("option", { name: "PLAT Analytics platform" });
    await screen.findByRole("option", { name: /^SR:/ });
    await screen.findByRole("option", { name: /4010 Software revenue/ });
    fireEvent.change(screen.getByLabelText("Item (optional)"), { target: { value: "i1" } });
    // The item fills the description, the price, the account and the GST code.
    expect((screen.getByLabelText("Description") as HTMLInputElement).value).toBe("Analytics platform");
    expect((screen.getByLabelText("Unit price before GST (SGD)") as HTMLInputElement).value).toBe("1500.00");
    expect((screen.getByLabelText("Revenue account") as HTMLSelectElement).value).toBe("a4010");
    expect((screen.getByLabelText("GST code") as HTMLSelectElement).value).toBe("SR");
    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a draft" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/invoices")).toBe(true));
    const body = sent.find((s) => s.method === "POST")!.body as components["schemas"]["InvoiceDraft"];
    expect(body).toMatchObject({ customer_id: "c1", reference: "PO-88213", date: "2026-10-14", currency: "SGD" });
    expect(body.lines![0]).toMatchObject({ item_id: "i1", description: "Analytics platform", quantity: "2", unit_price: "1500.00", account_id: "a4010", gst_code: "SR" });
  });

  it("says what to fix before saving", () => {
    const form = { customer_id: "", reference: "", date: "", due_date: "", currency: "SGD", fx_rate: "", notes: "", invoice_id: "" };
    const line = { key: "l1", item_id: "", description: "x", quantity: "two", unit_price: "", discount: "120", account_id: "", gst_code: "", spread_months: "", spread_from: "" };
    const { draft: d, errors } = check({ ...form, lines: [line] }, "SGD");
    expect(d).toBeUndefined();
    expect(errors["l1.account"]).toBe("Choose the revenue account the line earns.");
    expect(errors["l1.gst_code"]).toBe("Choose the line's GST code, such as SR for a standard-rated supply.");
    expect(errors["l1.quantity"]).toBe("Enter how many, such as 1 or 2.5, with up to 6 decimals.");
    expect(errors["l1.unit_price"]).toBe("Enter the price of one before GST, such as 1,500.00.");
    expect(errors["l1.discount"]).toBe("Enter a percentage from 0 to 100, such as 10, or leave it blank.");
    expect(check({ ...form, lines: [] }, "SGD", true).errors.lines).toBe("Add a line for what the credit note gives back.");
    // A credit note keeps the invoice it credits and has no due date.
    const ok = { ...line, quantity: "1", unit_price: "100", discount: "", account_id: "a4010", gst_code: "SR" };
    const cn = check({ ...form, due_date: "2026-11-30", invoice_id: "v1", lines: [ok] }, "SGD", true).draft;
    expect(cn).toMatchObject({ invoice_id: "v1" });
    expect(cn?.due_date).toBeUndefined();
  });

  it("shows a draft's checks, totals and what issuing writes, and issues it; axe finds nothing", async () => {
    const sent = answer({ ...reads, "GET /v1/invoices/v1": read(draft), "POST /v1/invoices/v1/issue": { status: 200, body: { invoice: issued, entry: { id: "e1" } } } });
    const { container } = renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: accountant, organisation: standard });
    expect(await screen.findByText("To check before it is issued")).toBeTruthy();
    expect(screen.getByText(/dated more than 30 days before today/)).toBeTruthy();
    const totals = screen.getByRole("heading", { name: "Totals" }).closest("section")!;
    expect(totals.textContent).toContain("GST SR on 3,000.00");
    expect(totals.textContent).toContain("3,270.00 SGD");
    expect(screen.getByText(/This invoice adds SGD 3,270.00 to what Acme Analytics Pte Ltd owes/)).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Issue" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/invoices/v1/issue")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/invoices/v1/issue")!.headers.get("If-Match")).toBe(`"1-draft"`);
  });

  it("asks for approval above the self-approval threshold", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/invoices/v1": read(draft),
      "POST /v1/invoices/v1/issue": { status: 403, body: { code: "INVOICE-10", title: "Another person approves it", detail: "The invoice's total is above the self-approval threshold.", status: 403 } },
      "POST /v1/invoices/v1/issue-approval": { status: 202, body: { id: "ap1" } },
    });
    renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Issue" }));
    fireEvent.click(await screen.findByRole("button", { name: "Ask for approval" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/invoices/v1/issue-approval")).toBe(true));
  });

  it("emails an issued invoice and lists its emails", async () => {
    const sent = answer({ ...reads, "GET /v1/invoices/v1": read(issued), "POST /v1/invoices/v1/email": { status: 200, body: emailed, headers: { ETag: `"1-posted"` } } });
    renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Email…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Send to (optional)"), { target: { value: "billing@acme.example" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Email" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/invoices/v1/email")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/invoices/v1/email")!.body).toEqual({ to: "billing@acme.example" });
  });

  it("shows each email with its delivery", async () => {
    answer({ ...reads, "GET /v1/invoices/v1": read(emailed) });
    renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: auditor, organisation: standard });
    const emails = (await screen.findByRole("heading", { name: "Emails" })).closest("section")!;
    expect(emails.textContent).toContain("To ap@acme.example");
    expect(emails.textContent).toContain("Sent");
    // A person who only reads invoices is offered no command.
    expect(screen.queryByRole("button", { name: "Email…" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Void…" })).toBeNull();
  });

  it("in Founder mode asks another founder to void it", async () => {
    const sent = answer({ ...reads, "GET /v1/invoices/v1": read(issued), "POST /v1/invoices/v1/void-approval": { status: 202, body: { id: "ap2" } } });
    renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: accountant, organisation: founder });
    fireEvent.click(await screen.findByRole("button", { name: "Ask to void…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to void" }));
    expect(await within(dialog).findByText("Say why: the audit trail keeps the reason with the reversal.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why it is voided"), { target: { value: "Billed twice" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to void" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/invoices/v1/void-approval")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/invoices/v1/void-approval")!.body).toMatchObject({ reason: "Billed twice" });
  });

  // req: FR-AR-18
  it("records a payment received into the bank", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/invoices/v1": read(issued),
      "POST /v1/customer-receipts": { status: 201, body: { receipt: { id: "r1", number: "RC-0001", status: "posted", version: 1, currency: "SGD", total: sgd("3270.00"), unapplied: sgd("0.00"), invoices: [], ledger_impact: [] }, entry: { id: "e2" } } },
    });
    renderPage(InvoicePage, { path: "/sales/invoices/v1", route: "/sales/invoices/$invoiceId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Record a payment…" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "1010 DBS current" });
    expect((within(dialog).getByLabelText("Amount received (SGD)") as HTMLInputElement).value).toBe("3270.00");
    fireEvent.change(within(dialog).getByLabelText("Date received"), { target: { value: "2026-10-28" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record a payment" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/customer-receipts")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/customer-receipts")!.body).toMatchObject({
      customer_id: "c1",
      date: "2026-10-28",
      money_account_id: "a1010",
      currency: "SGD",
      invoices: [{ invoice_id: "v1", amount: sgd("3270.00") }],
    });
  });
});

// req: FR-AR-18
describe("A payment of an invoice", () => {
  const f = { date: "2026-10-28", account: "a1010", accountCurrency: "SGD", amount: "", rate: "", received: "", reference: "", inFull: false };
  it("keeps what the customer paid beyond the invoice as their credit", () => {
    const r = receiptOf(issued, "SGD", { ...f, amount: "3,300.00" }).receipt!;
    expect(r.invoices![0]).toMatchObject({ amount: sgd("3270.00") });
    expect(r.invoices![0]!.in_full).toBeUndefined();
    expect(r.unapplied).toEqual(sgd("30.00"));
  });

  it("settles in full only when asked, and says what to fix", () => {
    expect(receiptOf(issued, "SGD", { ...f, amount: "3269.50", inFull: true }).receipt!.invoices![0]).toMatchObject({ amount: { amount: "3269.50" }, in_full: true });
    expect(receiptOf(issued, "SGD", { ...f, amount: "3269.50" }).receipt!.invoices![0]!.in_full).toBeUndefined();
    const { errors } = receiptOf(issued, "SGD", { ...f, date: "", account: "", amount: "0" });
    expect(errors.date).toBe("Enter the day the money came into the account.");
    expect(errors.account).toBe("Choose the account the money came into.");
    expect(errors.amount).toBe("Enter what the customer paid in SGD, such as 400.00.");
  });
});
