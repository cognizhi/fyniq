import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BillPage, check } from "./BillPage";
import { Bills } from "./Bills";
import { paymentOf } from "./Payments";

type Bill = components["schemas"]["Bill"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const standard: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "standard", version: 1 };
const founder: Organisation = { ...standard, team_mode: "founder" };
const accounts = {
  status: 200,
  body: {
    items: [
      { id: "a6120", code: "6120", name: "Software subscriptions", type: "expense", active: true, group: false, version: 1, default_gst_code: "TX" },
      { id: "a2010", code: "2010", name: "Accounts payable", type: "liability", active: true, group: false, version: 1 },
      { id: "a1010", code: "1010", name: "DBS current", type: "asset", active: true, group: false, version: 1, money_account: true, currency: "SGD" },
      { id: "a1021", code: "1021", name: "Wise USD", type: "asset", active: true, group: false, version: 1, money_account: true, currency: "USD" },
    ],
  },
};
const vendors = { status: 200, body: { items: [{ id: "c1", legal_name: "Atlassian Pty Ltd", vendor: true, customer: false, active: true, version: 1 }] } };
const codes = {
  status: 200,
  body: {
    version: "sg-gst@2026.1",
    rate: "0.09",
    codes: [
      { code: "TX", description: "Standard-rated purchase whose GST can be claimed", claimable: true },
      { code: "BL", description: "Purchase whose input tax is disallowed", claimable: false },
    ],
  },
};
const states = (extra: Partial<Bill["states"]> = {}): Bill["states"] => ({ approval: "draft", ledger: "not_posted", payment: "unpaid", due: "not_due", ...extra });
const draft: Bill = {
  id: "b1",
  status: "draft",
  version: 1,
  supplier: { id: "c1", name: "Atlassian Pty Ltd" },
  supplier_ref: "AU-1182",
  date: "2026-10-14",
  due_date: "2026-11-13",
  currency: "SGD",
  lines: [{ account_id: "a6120", description: "Confluence", gst_code: "TX", net: { amount: "1000.00", currency: "SGD" }, gst: { amount: "90.00", currency: "SGD" } }],
  total: { amount: "1090.00", currency: "SGD" },
  payable: { amount: "1090.00", currency: "SGD" },
  states: states(),
  checks: [{ severity: "warning", code: "BILL-GST-DIFFERS", field: "lines", message: "the stated GST 90.00 SGD differs by 0.01 SGD from the recomputation", fix: "Check the GST against the supplier's invoice" }],
  ledger_impact: [
    { line_no: 1, account_id: "a6120", amount: { amount: "1090.00", currency: "SGD" }, fx_rate: "1", fx_rate_source: "base", base_amount: { amount: "1090.00", currency: "SGD" } },
    { line_no: 2, account_id: "a2010", amount: { amount: "-1090.00", currency: "SGD" }, fx_rate: "1", fx_rate_source: "base", base_amount: { amount: "-1090.00", currency: "SGD" } },
  ],
  payments: [],
};
const posted: Bill = { ...draft, number: "BILL-0007", status: "posted", states: states({ approval: "approved", ledger: "posted", due: "overdue" }), checks: [], open: { amount: "1090.00", currency: "SGD" } };
const partPaid: Bill = {
  ...posted,
  states: states({ approval: "approved", ledger: "posted", payment: "part_paid" }),
  open: { amount: "690.00", currency: "SGD" },
  payments: [{ payment_id: "p1", payment_number: "PAY-0001", date: "2026-10-20", amount: { amount: "400.00", currency: "SGD" }, realised_fx: { amount: "0.00", currency: "SGD" }, voided: false }],
};
const read = (b: Bill) => ({ status: 200, body: b, headers: { ETag: `"${b.version}-${b.status}"` } });
const reads = { "GET /v1/accounts": accounts, "GET /v1/contacts": vendors, "GET /v1/purchases/gst-codes": codes };

// req: FR-AP-02
describe("Purchases › Bills", () => {
  it("lists the bills with their states, and an overdue one flagged; axe finds nothing", async () => {
    answer({ "GET /v1/bills": { status: 200, body: { items: [draft, posted] } } });
    const { container } = renderPage(Bills, { path: "/purchases/bills", session: accountant });
    const table = await screen.findByRole("table", { name: "Bills" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Atlassian Pty Ltd");
    expect(rows[1]!.textContent).toContain("Draft");
    expect(rows[2]!.textContent).toContain("BILL-0007");
    expect(rows[2]!.textContent).toContain("Overdue");
    expect(screen.getByText("1 bill shown is overdue")).toBeTruthy();
    expect(screen.getByRole("link", { name: "New bill" })).toBeTruthy();
    await noViolations(container);
  });

  it("offers no new bill to a person who only reads them", async () => {
    answer({ "GET /v1/bills": { status: 200, body: { items: [] } } });
    renderPage(Bills, { path: "/purchases/bills", session: auditor });
    expect(await screen.findByText("No bills yet")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "New bill" })).toBeNull();
    expect(screen.getByText("A Bookkeeper, an AP specialist or an Accountant enters bills.")).toBeTruthy();
  });
});

// req: FR-AP-02
// req: FM-1
// req: FM-2
describe("A bill", () => {
  it("is entered as the supplier's invoice states it and saved as a draft", async () => {
    const sent = answer({ ...reads, "POST /v1/bills": { status: 201, body: { ...draft, id: "b9" } } });
    renderPage(BillPage, { path: "/purchases/bills/new", session: accountant, organisation: standard });
    await screen.findByRole("option", { name: "Atlassian Pty Ltd" });
    fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "c1" } });
    fireEvent.change(screen.getByLabelText("Their invoice number"), { target: { value: "AU-1182" } });
    fireEvent.change(screen.getByLabelText("Invoice date"), { target: { value: "2026-10-14" } });
    await screen.findByRole("option", { name: /6120 Software subscriptions/ });
    fireEvent.change(screen.getByLabelText("What it is for"), { target: { value: "a6120" } });
    await screen.findByRole("option", { name: /^TX:/ });
    expect((screen.getByLabelText("GST code") as HTMLSelectElement).value).toBe("TX"); // the account's default code
    fireEvent.change(screen.getByLabelText("Amount before GST (SGD)"), { target: { value: "1,000.00" } });
    fireEvent.change(screen.getByLabelText("GST as stated (SGD)"), { target: { value: "90.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a draft" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/bills")).toBe(true));
    const body = sent.find((s) => s.method === "POST")!.body as components["schemas"]["BillDraft"];
    expect(body).toMatchObject({ supplier_id: "c1", supplier_ref: "AU-1182", date: "2026-10-14", currency: "SGD" });
    expect(body.lines![0]).toMatchObject({ account_id: "a6120", gst_code: "TX", net: { amount: "1000.00", currency: "SGD" }, gst: { amount: "90.00", currency: "SGD" } });
  });

  it("says what to fix before saving", () => {
    const { draft: d, errors } = check({ supplier_id: "", supplier_ref: "", date: "", due_date: "", currency: "SGD", fx_rate: "", notes: "", lines: [{ key: "l1", account_id: "", description: "x", gst_code: "", net: "", gst: "", stated_gst: "" }] }, "SGD");
    expect(d).toBeUndefined();
    expect(errors["l1.account"]).toBe("Choose what the line is for.");
    expect(errors["l1.gst_code"]).toBe("Choose the GST code on the supplier's invoice.");
    expect(errors["l1.net"]).toBe("Enter the amount before GST, such as 1,200.00.");
  });

  it("shows what posting writes and its warnings, and asks for approval above the threshold; axe finds nothing", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bills/b1": read(draft),
      "POST /v1/bills/b1/post": problem(409, "BILL-09", "The bill's total is 5450.00 SGD, above the self-approval threshold of 2000.00."),
      "POST /v1/bills/b1/post-approval": { status: 202, body: { id: "ap1" } },
    });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    expect(await screen.findByText(/differs by 0.01 SGD from the recomputation/)).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Ledger impact" })).toBeTruthy();
    expect(screen.getByText(/This bill adds SGD 1,090.00 to what we owe Atlassian Pty Ltd/)).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Post" }));
    const ask = await screen.findByRole("button", { name: "Ask for approval" });
    expect(screen.getByText("Another person approves this bill")).toBeTruthy();
    fireEvent.click(ask);
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bills/b1/post-approval")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/bills/b1/post-approval")!.headers.get("If-Match")).toBe('"1-draft"');
  });

  it("in Founder mode asks to void a posted bill, with a reason", async () => {
    const sent = answer({ ...reads, "GET /v1/bills/b1": read(posted), "POST /v1/bills/b1/void-approval": { status: 202, body: { id: "ap2" } } });
    renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: founder });
    fireEvent.click(await screen.findByRole("button", { name: "Ask to void…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to void" }));
    expect(await within(dialog).findByText("Say why: the audit trail keeps the reason with the reversal.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why it is voided"), { target: { value: "Entered twice" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to void" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bills/b1/void-approval")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/bills/b1/void-approval")!.body).toMatchObject({ reason: "Entered twice" });
    expect(screen.queryByRole("button", { name: "Void…" })).toBeNull();
  });

  it("edits a posted bill with a reason, as the content sent", async () => {
    const sent = answer({ ...reads, "GET /v1/bills/b1": read(posted), "POST /v1/bills/b1/edit": { status: 200, body: { bill: { ...posted, version: 2 }, entry: {}, reversal: {} } } });
    renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Amount before GST (SGD)"), { target: { value: "900.00" } });
    fireEvent.change(screen.getByLabelText("GST as stated (SGD)"), { target: { value: "81.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Post the edit" }));
    expect(await screen.findByText("Say why: the audit trail keeps the reason with the edit.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Why it is edited"), { target: { value: "One seat less" } });
    fireEvent.click(screen.getByRole("button", { name: "Post the edit" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bills/b1/edit")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/bills/b1/edit")!.body).toMatchObject({ reason: "One seat less", lines: [{ net: { amount: "900.00" } }] });
  });
});

// req: FR-AP-02
// req: FR-AR-18
// req: FM-1
describe("A bill's payments", () => {
  it("are recorded by hand, in part, from a money account; axe finds nothing", async () => {
    const sent = answer({ ...reads, "GET /v1/bills/b1": read(posted), "POST /v1/bill-payments": { status: 201, body: { payment: { number: "PAY-0001", total: { amount: "400.00", currency: "SGD" } }, entry: {} } } });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    expect(await screen.findByText(/1,090.00 SGD open/)).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Record payment…" }));
    const dialog = await screen.findByRole("dialog");
    expect((within(dialog).getByLabelText("Amount paid of the bill (SGD)") as HTMLInputElement).value).toBe("1090.00");
    expect(within(dialog).queryByRole("option", { name: /Wise USD/ })).toBeNull(); // an SGD bill is not paid from a USD wallet
    await noViolations(container);
    fireEvent.change(within(dialog).getByLabelText("Amount paid of the bill (SGD)"), { target: { value: "400" } });
    fireEvent.change(within(dialog).getByLabelText("Bank reference (optional)"), { target: { value: "FAST 88231" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record payment" }));
    await waitFor(() => expect(sent.some((x) => x.method === "POST" && x.path === "/v1/bill-payments")).toBe(true));
    const body = sent.find((x) => x.path === "/v1/bill-payments")!.body as components["schemas"]["BillPaymentDraft"];
    expect(body).toMatchObject({ money_account_id: "a1010", currency: "SGD", reference: "FAST 88231", supplier_id: "c1", bills: [{ bill_id: "b1", amount: { amount: "400", currency: "SGD" } }] });
    expect(sent.find((x) => x.path === "/v1/bill-payments")!.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("are listed on the bill, which is part paid, and in Founder mode a void is asked for", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bills/b1": read(partPaid),
      "GET /v1/bill-payments/p1": { status: 200, body: { id: "p1", status: "posted", version: 1 }, headers: { ETag: '"1-posted"' } },
      "POST /v1/bill-payments/p1/void-approval": { status: 202, body: { id: "ap3" } },
    });
    renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: founder });
    expect(await screen.findByText("Part paid")).toBeTruthy();
    const table = await screen.findByRole("table", { name: "Payments" });
    expect(within(table).getByText("PAY-0001")).toBeTruthy();
    fireEvent.click(within(table).getByRole("button", { name: "Ask to void PAY-0001" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why it is voided"), { target: { value: "Paid from the wrong account" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to void" }));
    await waitFor(() => expect(sent.some((x) => x.path === "/v1/bill-payments/p1/void-approval")).toBe(true));
    const asked = sent.find((x) => x.path === "/v1/bill-payments/p1/void-approval")!;
    expect(asked.headers.get("If-Match")).toBe('"1-posted"');
    expect(asked.body).toMatchObject({ reason: "Paid from the wrong account" });
  });

  it("say what to fix: a bill in another currency paid from an SGD account names what the account paid", () => {
    const usd: Bill = { ...posted, currency: "USD", total: { amount: "1090.00", currency: "USD" }, open: { amount: "1090.00", currency: "USD" } };
    const f = { date: "2026-10-20", account: "a1010", accountCurrency: "SGD", amount: "1090.00", rate: "", paid: "", reference: "" };
    expect(paymentOf(usd, "SGD", f).errors.paid).toBe("Enter what the account paid in SGD, as the statement shows it.");
    expect(paymentOf(usd, "SGD", { ...f, paid: "1,420.00" }).payment).toMatchObject({ paid: { amount: "1420.00", currency: "SGD" }, fx_rate: undefined });
    expect(paymentOf(usd, "SGD", { ...f, account: "a1021", accountCurrency: "USD", rate: "1.30" }).payment).toMatchObject({ fx_rate: "1.30", paid: undefined });
    expect(paymentOf(posted, "SGD", { ...f, amount: "0" }).errors.amount).toBe("Enter what was paid of the bill in SGD, such as 400.00.");
  });
});
