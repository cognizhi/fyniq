import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BillPage, check } from "./BillPage";
import { Bills } from "./Bills";
import { usualTax } from "./InputTax";
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
  kind: "bill",
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
  releases: [],
  input_tax: { supplier_registered: true, document: "tax_invoice", addressed_to: "company" },
  input_tax_decisions: [{ line: 1, condition: "claimable", reason: "tax_invoice", text: "a tax invoice addressed to the company" }],
};
const posted: Bill = { ...draft, number: "BILL-0007", status: "posted", states: states({ approval: "approved", ledger: "posted", due: "overdue" }), checks: [], open: { amount: "1090.00", currency: "SGD" } };
const partPaid: Bill = {
  ...posted,
  states: states({ approval: "approved", ledger: "posted", payment: "part_paid" }),
  open: { amount: "690.00", currency: "SGD" },
  payments: [{ via: "payment", payment_id: "p1", payment_number: "PAY-0001", date: "2026-10-20", amount: { amount: "400.00", currency: "SGD" }, realised_fx: { amount: "0.00", currency: "SGD" }, voided: false }],
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

  it("says what the company owes suppliers: the next 30 days' cash and, in Expert mode, the ageing (FR-AP-06)", async () => {
    const sgd = (amount: string) => ({ amount, currency: "SGD" });
    answer({
      "GET /v1/bills": { status: 200, body: { items: [posted] } },
      "GET /v1/payables": {
        status: 200,
        body: {
          as_at: "2026-11-30",
          within: 30,
          suppliers: [],
          totals: [{ currency: "SGD", current: sgd("0.00"), days_1_30: sgd("690.00"), days_31_60: sgd("0.00"), days_61_90: sgd("0.00"), days_90_plus: sgd("0.00"), credits: sgd("-25.00"), total: sgd("665.00"), carrying: sgd("665.00") }],
          to_pay: [],
          requirement: [{ currency: "SGD", overdue: sgd("690.00"), within: sgd("0.00"), total: sgd("690.00"), carrying: sgd("690.00") }],
        },
      },
    });
    renderPage(Bills, { path: "/purchases/bills", session: accountant });
    const card = await screen.findByRole("region", { name: "What we owe suppliers" });
    expect(card.textContent).toContain("690.00 SGD to pay in the next 30 days, 690.00 of it overdue.");
    const ageing = within(card).getByRole("table", { name: "AP ageing" });
    expect(within(ageing).getAllByRole("row")[1]!.textContent).toBe("SGD—690.00———−25.00665.00");
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
    const { draft: d, errors } = check({ supplier_id: "", supplier_ref: "", date: "", due_date: "", currency: "SGD", fx_rate: "", notes: "", tax: usualTax(), nature: "", lines: [{ key: "l1", account_id: "", description: "x", gst_code: "", net: "", gst: "", stated_gst: "", spread_months: "", spread_from: "" }] }, "SGD");
    expect(d).toBeUndefined();
    expect(errors["l1.account"]).toBe("Choose what the line is for.");
    expect(errors["l1.gst_code"]).toBe("Choose the GST code on the supplier's invoice.");
    expect(errors["l1.net"]).toBe("Enter the amount before GST, such as 1,200.00.");
  });

  // req: FR-AP-20
  it("sends only the input-tax facts that differ from the usual, and a line's business share", () => {
    const form = (share: string) => ({
      supplier_id: "c1",
      supplier_ref: "",
      date: "2026-10-26",
      due_date: "",
      currency: "SGD",
      fx_rate: "",
      notes: "",
      tax: usualTax(),
      nature: "",
      lines: [{ key: "l1", account_id: "a6120", description: "Phone", gst_code: "TX", net: "100.00", gst: "9.00", stated_gst: "", spread_months: "", spread_from: "", business_share: share }],
    });
    const usual = check(form(""), "SGD").draft!;
    expect(usual.input_tax).toBeUndefined();
    expect(usual.lines?.[0]?.business_share).toBeUndefined();
    expect(check(form("100"), "SGD").draft?.lines?.[0]?.business_share).toBeUndefined();
    expect(check(form("60%"), "SGD").draft?.lines?.[0]?.business_share).toBe("60");
    for (const bad of ["120", "-5", "sixty", "33.333"]) expect(check(form(bad), "SGD").errors["l1.business_share"], bad).toBeTruthy();
    const agent = { ...form(""), tax: { ...usualTax(), registered: false, document: "simplified", addressed_to: "agent", repaid: "director_loan" } };
    expect(check(agent, "SGD").draft?.input_tax).toEqual({ supplier_registered: false, document: "simplified", addressed_to: "agent", repaid: "director_loan" });
    // How an agent was repaid is sent only for an agent's invoice.
    expect(check({ ...form(""), tax: { ...usualTax(), repaid: "director_loan" } }, "SGD").draft?.input_tax).toBeUndefined();
  });

  // req: FR-AP-20
  it("shows how each line's input tax is decided, and asks for the tax agent's decision; axe finds nothing", async () => {
    const waiting: Bill = {
      ...draft,
      input_tax: { supplier_registered: true, document: "tax_invoice", addressed_to: "agent", repaid: "director_loan" },
      lines: [draft.lines[0]!, { ...draft.lines[0]!, description: "Phone", business_share: "60" }],
      input_tax_decisions: [
        { line: 1, condition: "tax_agent", reason: "director_loan", text: "the invoice is in an agent's name and the company credited their director's loan instead of paying them back: the tax agent decides" },
        { line: 2, condition: "claimable", reason: "business_share", text: "a tax invoice, claimed for its business share of 60%", business_share: "60" },
      ],
    };
    const sent = answer({ ...reads, "GET /v1/bills/b1": read(waiting), "PUT /v1/bills/b1": { status: 200, body: waiting, headers: { ETag: `"2-draft"` } } });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    const card = (await screen.findByRole("heading", { name: "Input tax" })).closest("section")!;
    expect(within(card).getByText("For the tax agent")).toBeTruthy();
    expect(within(card).getByText("Claimed for 60%")).toBeTruthy();
    expect((within(card).getByLabelText("How they were paid back") as HTMLSelectElement).value).toBe("director_loan");
    expect((screen.getAllByLabelText("Business share (%)")[1] as HTMLInputElement).value).toBe("60");
    await noViolations(container);
    fireEvent.change(within(card).getByLabelText("The tax agent's decision"), { target: { value: "claimable" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a draft" }));
    await waitFor(() =>
      expect(sent.find((x) => x.method === "PUT" && x.path === "/v1/bills/b1")?.body).toMatchObject({
        input_tax: { addressed_to: "agent", repaid: "director_loan", tax_agent: "claimable" },
        lines: [{ description: "Confluence" }, { description: "Phone", business_share: "60" }],
      }),
    );
  });

  // req: FR-AP-19
  // req: G4-010
  it("asks a non-resident supplier's bill for its nature, and records the tax agent's decision; axe finds nothing", async () => {
    const consultant = { id: "c2", legal_name: "Ava Consulting", vendor: true, customer: false, active: true, version: 1, country: "SG", tax_residence: "AU" };
    const held: Bill = {
      ...posted,
      supplier: { id: "c2", name: "Ava Consulting" },
      nature_of_payment: "services_in_singapore",
      withholding: { residence: "AU", nature_of_payment: "services_in_singapore", held: "the tax agent has not decided whether tax is withheld from it" },
    };
    const sent = answer({
      ...reads,
      "GET /v1/contacts": { status: 200, body: { items: [vendors.body.items[0], consultant] } },
      "GET /v1/bills/b1": read(held),
      "POST /v1/contacts/c2/withholding-decisions": { status: 201, body: { id: "d1", supplier_id: "c2", nature_of_payment: "services_in_singapore", outcome: "withhold", rate_percent: "15", tax_agent: "Grace Ho", recorded_at: "2026-10-23T09:00:00Z" } },
    });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    const card = (await screen.findByRole("heading", { name: "Withholding tax" })).closest("section")!;
    expect(within(card).getByText("Not decided yet")).toBeTruthy();
    expect(within(card).getByText("Its payment waits")).toBeTruthy();
    expect(within(card).getByText("Services performed in Singapore")).toBeTruthy();
    expect((screen.getByLabelText("Nature of payment") as HTMLSelectElement).value).toBe("services_in_singapore");
    await noViolations(container);
    fireEvent.click(within(card).getByRole("button", { name: "Record the tax agent's decision…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Decision"), { target: { value: "withhold" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record decision" }));
    expect(await within(dialog).findByText("Enter the rate the tax agent advised, above 0 and up to 100, such as 15.")).toBeTruthy();
    expect(within(dialog).getByText("Name the tax agent who decided.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Rate (%)"), { target: { value: "15" } });
    fireEvent.change(within(dialog).getByLabelText("Tax agent"), { target: { value: "Grace Ho" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record decision" }));
    await waitFor(() =>
      expect(sent.find((x) => x.path === "/v1/contacts/c2/withholding-decisions")?.body).toEqual({
        nature_of_payment: "services_in_singapore",
        outcome: "withhold",
        rate_percent: "15",
        tax_agent: "Grace Ho",
        bill_id: "b1",
      }),
    );
    expect(await screen.findByText(/edit the bill so it withholds, then record its payment/)).toBeTruthy();
  });

  // req: FR-AP-19
  it("sends the nature of payment for a supplier resident outside Singapore", () => {
    const f = {
      supplier_id: "c2",
      supplier_ref: "",
      date: "2026-10-14",
      due_date: "",
      currency: "SGD",
      fx_rate: "",
      notes: "",
      tax: usualTax(),
      nature: "software_use",
      lines: [{ key: "l1", account_id: "a6120", description: "Linear", gst_code: "NR", net: "120.00", gst: "0", stated_gst: "", spread_months: "", spread_from: "" }],
    };
    expect(check(f, "SGD").draft?.nature_of_payment).toBe("software_use");
    expect(check({ ...f, nature: "" }, "SGD").draft?.nature_of_payment).toBeUndefined();
  });

  // req: FR-GL-03
  it("sends a line's spread over months, and says what to fix in one", () => {
    const form = (spread_months: string, spread_from: string) => ({
      supplier_id: "c1",
      supplier_ref: "",
      date: "2026-10-14",
      due_date: "",
      currency: "SGD",
      fx_rate: "",
      notes: "",
      tax: usualTax(),
      nature: "",
      lines: [{ key: "l1", account_id: "a6120", description: "Jira, a year", gst_code: "TX", net: "13080.00", gst: "1177.20", stated_gst: "", spread_months, spread_from }],
    });
    expect(check(form("12", "2026-09"), "SGD").draft?.lines?.[0]).toMatchObject({ spread_months: 12, spread_from: "2026-09-01" });
    expect(check(form("12", ""), "SGD").draft?.lines?.[0]).toMatchObject({ spread_months: 12, spread_from: undefined });
    expect(check(form("", ""), "SGD").draft?.lines?.[0]?.spread_months).toBeUndefined();
    for (const [months, from, field] of [["0", "", "spread_months"], ["121", "", "spread_months"], ["", "2026-09", "spread_months"], ["12", "Sept", "spread_from"]] as const) {
      expect(check(form(months, from), "SGD").errors[`l1.${field}`], `${months} from ${from}`).toBeTruthy();
    }
  });

  // req: FR-DOC-05
  // req: FR-DOC-07
  it("attaches the supplier's invoice to a draft at the version read; axe finds nothing", async () => {
    let files: components["schemas"]["BillEvidence"][] = [];
    const invoice: components["schemas"]["BillEvidence"] = {
      id: "ev1",
      bill_id: "b1",
      evidence_type: "invoice",
      file_name: "AU-1182.pdf",
      media_type: "application/pdf",
      size_bytes: 48213,
      sha256: "a".repeat(64),
      original_sha256: "a".repeat(64),
      source: "upload",
      data_class: "general",
      uploaded_by: "user:u1",
      uploaded_at: "2026-10-14T01:00:00Z",
      state: "attached",
      version: 1,
    };
    answer({
      ...reads,
      "GET /v1/bills/b1": read(draft),
      "GET /v1/bills/b1/evidence": () => ({ status: 200, body: { items: files } }),
      "POST /v1/bills/b1/evidence": () => {
        files = [invoice];
        return { status: 201, body: invoice };
      },
    });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    expect(await screen.findByText("No files are attached.")).toBeTruthy();
    expect(screen.getByText(/The supplier's invoice, and any other file the bill rests on/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("File"), { target: { files: [new File(["%PDF"], "AU-1182.pdf", { type: "application/pdf" })] } });
    fireEvent.change(screen.getByLabelText("What it is"), { target: { value: "invoice" } });
    fireEvent.click(screen.getByRole("button", { name: "Attach" }));
    expect(await screen.findByText("Attached AU-1182.pdf. It can come off while the bill is a draft.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "POST" && x.path === "/v1/bills/b1/evidence")!;
    expect(sent.headers.get("If-Match")).toBe(`"1-draft"`);
    expect(sent.body).toMatchObject({ evidence_type: "invoice", data_class: "general", source: "upload" });
    expect(within(await screen.findByRole("list", { name: "Files attached" })).getByText("AU-1182.pdf")).toBeTruthy();
    await noViolations(container);
  });

  // req: FR-GL-03
  // req: ENG-05
  it("shows a spread bill's months: those posted, and those to come as projections; axe finds nothing", async () => {
    const sgd = (amount: string) => ({ amount, currency: "SGD" });
    const spread: Bill = {
      ...posted,
      lines: [{ ...draft.lines[0]!, spread_months: 3, spread_from: "2026-09-01" }],
      releases: [
        { line: 1, month: "2026-09", part: 1, of: 3, date: "2026-10-14", amount: sgd("363.33"), status: "posted", entry_id: "e1" },
        { line: 1, month: "2026-10", part: 2, of: 3, date: "2026-10-14", amount: sgd("363.34"), status: "posted", entry_id: "e2" },
        { line: 1, month: "2026-11", part: 3, of: 3, date: "2026-11-01", amount: sgd("363.33"), status: "projected" },
      ],
    };
    answer({ ...reads, "GET /v1/bills/b1": read(spread) });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/b1", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    const table = await screen.findByRole("table", { name: "Spread over months" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Sept 2026");
    expect(rows[1]!.textContent).toContain("Posted");
    expect(rows[3]!.textContent).toContain("1 Nov 2026");
    expect(rows[3]!.textContent).toContain("363.33");
    expect(rows[3]!.textContent).toContain("Projected");
    expect((screen.getByLabelText("Spread over (months, optional)") as HTMLInputElement).value).toBe("3");
    expect((screen.getByLabelText("Spread from") as HTMLInputElement).value).toBe("2026-09");
    await noViolations(container);
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

// req: FR-AP-03
describe("A credit note", () => {
  const credit: Bill = {
    ...posted,
    id: "c9",
    kind: "vendor_credit",
    number: "VC-0001",
    supplier_ref: "CN-12",
    total: { amount: "218.00", currency: "SGD" },
    payable: { amount: "218.00", currency: "SGD" },
    open: { amount: "218.00", currency: "SGD" },
    states: states({ approval: "approved", ledger: "posted" }),
  };
  const openBill: Bill = { ...posted, id: "b7", number: "BILL-0007", open: { amount: "100.00", currency: "SGD" } };
  const other: Bill = { ...posted, id: "b8", number: "BILL-0008", supplier: { id: "c2", name: "Vercel Inc" }, open: { amount: "50.00", currency: "SGD" } };

  it("is entered as a bill is and saved with its kind", async () => {
    const sent = answer({ ...reads, "POST /v1/bills": { status: 201, body: { ...credit, status: "draft" } } });
    renderPage(BillPage, { path: "/purchases/credits/new", session: accountant, organisation: standard });
    expect(await screen.findByRole("heading", { name: "New credit note" })).toBeTruthy();
    await screen.findByRole("option", { name: "Atlassian Pty Ltd" });
    fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "c1" } });
    fireEvent.change(screen.getByLabelText("Their credit note number"), { target: { value: "CN-12" } });
    expect(screen.queryByLabelText("Due date")).toBeNull();
    await screen.findByRole("option", { name: /6120 Software subscriptions/ });
    fireEvent.change(screen.getByLabelText("What it is for"), { target: { value: "a6120" } });
    await screen.findByRole("option", { name: /^TX:/ });
    fireEvent.change(screen.getByLabelText("Amount before GST (SGD)"), { target: { value: "200.00" } });
    fireEvent.change(screen.getByLabelText("GST as stated (SGD)"), { target: { value: "18.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a draft" }));
    await waitFor(() => expect(sent.some((x) => x.method === "POST" && x.path === "/v1/bills")).toBe(true));
    expect(sent.find((x) => x.method === "POST")!.body).toMatchObject({ kind: "vendor_credit", supplier_ref: "CN-12" });
  });

  it("is applied to one of its supplier's open bills, the lesser of the two by default; axe finds nothing", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bills/c9": read(credit),
      "GET /v1/bills": { status: 200, body: { items: [openBill, other] } },
      "POST /v1/credit-applications": { status: 201, body: { application: { amount: { amount: "100.00", currency: "SGD" }, bill: { id: "b7", kind: "bill", number: "BILL-0007" } } } },
    });
    const { container } = renderPage(BillPage, { path: "/purchases/bills/c9", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    expect(await screen.findByText(/218.00 SGD left/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record payment…" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Apply to a bill…" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: /BILL-0007/ });
    expect(within(dialog).queryByRole("option", { name: /BILL-0008/ })).toBeNull(); // another supplier's
    fireEvent.change(within(dialog).getByLabelText("Bill"), { target: { value: "b7" } });
    expect((within(dialog).getByLabelText("Amount to apply (SGD)") as HTMLInputElement).value).toBe("100.00");
    await noViolations(container);
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply to a bill" }));
    await waitFor(() => expect(sent.some((x) => x.path === "/v1/credit-applications")).toBe(true));
    expect(sent.find((x) => x.path === "/v1/credit-applications")!.body).toMatchObject({ credit_id: "c9", bill_id: "b7", amount: { amount: "100.00", currency: "SGD" } });
  });

  it("records a refund from the supplier into a money account", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bills/c9": read(credit),
      "POST /v1/supplier-refunds": { status: 201, body: { refund: { number: "RF-0001", amount: { amount: "218.00", currency: "SGD" } }, entry: {} } },
    });
    renderPage(BillPage, { path: "/purchases/bills/c9", route: "/purchases/bills/$billId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Record refund…" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "Record a refund of this credit note" })).toBeTruthy();
    expect((within(dialog).getByLabelText("Amount refunded (SGD)") as HTMLInputElement).value).toBe("218.00");
    fireEvent.click(within(dialog).getByRole("button", { name: "Record refund" }));
    await waitFor(() => expect(sent.some((x) => x.path === "/v1/supplier-refunds")).toBe(true));
    expect(sent.find((x) => x.path === "/v1/supplier-refunds")!.body).toMatchObject({ credit_id: "c9", money_account_id: "a1010", amount: { amount: "218.00", currency: "SGD" } });
  });
});
