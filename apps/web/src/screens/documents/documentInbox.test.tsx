import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DocumentInbox } from "./DocumentInbox";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const organisation = { base_currency: "SGD", team_mode: "founder" } as never;
const field = (value: string | null, confidence = 0.95) => ({ value, confidence });

const reading = {
  kind: "bill",
  supplier: field("Atlassian Pty. Ltd.", 0.97),
  supplier_uen: field(null, 0.5),
  gst_registration_no: field("M2-8503432-K", 0.9),
  supplier_address: field("1 Raffles Place", 0.9),
  number: field("AU-2001", 0.95),
  date: field("2026-10-14", 0.96),
  due_date: field(null, 0.5),
  currency: field("SGD", 0.99),
  lines: [{ description: field("Confluence, October", 0.9), net: field("100.00", 0.9), gst_code: field("TX", 0.8), gst: field("9.00", 0.9), amount: field("109.00", 0.9) }],
  gst_total: field("9.00", 0.9),
  total: field("190.00", 0.5),
  gst_in_sgd: field(null, 0.5),
  tax_invoice_label: { value: true, confidence: 0.9 },
  customer_named: { value: true, confidence: 0.9 },
};
const capture = (extra: Record<string, unknown> = {}) => ({
  id: "c1",
  source: "upload",
  file_name: "AU-2001.pdf",
  media_type: "application/pdf",
  size_bytes: 1200,
  sha256: "a".repeat(64),
  status: "read",
  kind: "bill",
  captured_by: "user:u1",
  created_at: "2026-11-20T09:00:00Z",
  own: false,
  reading_is: "the model's reading",
  reading,
  confidence: { total: 0.5, supplier: 0.97 },
  low_confidence: ["total"],
  checks: [{ code: "CAP-TOTALS", field: "total", severity: "warning", message: "The lines come to 109.00, but the document's total is 190.00; check each against the document" }],
  model: "claude-opus-5-5",
  readings: 1,
  supplier: { id: "s1", name: "Atlassian Pty Ltd", by: "name", defaults: { account_id: "a6120", gst_code: "TX", from_bills: 2, saved_on_contact: false } },
  version: 4,
  ...extra,
});
const reads = {
  "GET /v1/captures": { status: 200, body: { items: [capture(), capture({ id: "c2", file_name: "scan.png", status: "failed", kind: undefined, reading: undefined, checks: undefined, note: "Only a PDF or a photo can be read" })] } },
  "GET /v1/organisation/email/capture-addresses": { status: 200, body: { domain: "in.example.com", organisation: { address: "bills@in.example.com", name: "bills", created_at: "2026-11-01T00:00:00Z" }, organisation_etag: '"1"', mine_etag: '"none"', bank_etag: '"none"' } },
  "GET /v1/organisation/email/inbound-messages": { status: 200, body: { items: [{ id: "m1", address: "bills@in.example.com", for: "organisation", kind: "documents", sender: "ar@supplier.example", received_at: "2026-11-20T08:00:00Z", status: "accepted", files: [{ id: "f1", kind: "attachment", file_name: "INV-77.txt", media_type: "text/plain", size_bytes: 40, removed: false }] }] } },
  "GET /v1/organisation/email/inbound-messages/mine": { status: 200, body: { items: [] } },
  "GET /v1/claim-categories": { status: 200, body: { items: [{ id: "travel", name: "Local travel", account_id: "a6100", mileage: false, active: true, version: 1 }] } },
  "GET /v1/accounts": { status: 200, body: { items: [{ id: "a6120", code: "6120", name: "Software", type: "expense", active: true, group: false, version: 1 }] } },
  "GET /v1/contacts": { status: 200, body: { items: [{ id: "s1", legal_name: "Atlassian Pty Ltd", vendor: true, active: true, version: 1 }] } },
};

// req: FR-DOC-14
// req: FR-AP-04
describe("Purchases › Document inbox", () => {
  it("lists the captured files with their state and shows the reading apart, in the AI's card, with what was doubted; axe finds nothing", async () => {
    answer(reads);
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    const table = await screen.findByRole("table", { name: "Captured documents" });
    expect(within(table).getByText("Read: check it")).toBeTruthy();
    expect(within(table).getByText("Could not be read")).toBeTruthy();
    expect(screen.getByText("bills@in.example.com")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open AU-2001.pdf" }));
    expect(await screen.findByText("What the model read")).toBeTruthy();
    expect(screen.getByText(/It is not a figure Fyniq decided/)).toBeTruthy();
    expect(screen.getByText("50% sure: check it")).toBeTruthy();
    expect(screen.getByText(/the document's total is 190\.00/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Read it again" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("makes the draft bill from the reading as corrected: the supplier it matched, the amounts as decimal strings, the capture's version in If-Match", async () => {
    answer({
      ...reads,
      "POST /v1/captures/c1/bill": { status: 201, body: { capture: capture({ status: "drafted", version: 5 }), draft: { kind: "bill", id: "b1" } } },
    });
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Open AU-2001.pdf" }));
    await screen.findByRole("button", { name: "Save draft bill" });
    expect(screen.getByText("Suggested from Atlassian Pty Ltd's 2 posted bills.")).toBeTruthy(); // the supplier's usual account, for the person to check
    await screen.findByRole("option", { name: "6120 Software" });
    fireEvent.change(screen.getByLabelText("Account of line 1"), { target: { value: "a6120" } });
    fireEvent.change(screen.getByLabelText("Net of line 1"), { target: { value: "100.00" } });
    fireEvent.click(screen.getByLabelText(/Keep the first line's account and GST code as Atlassian Pty Ltd's defaults/));
    await waitFor(() => expect((screen.getByRole("button", { name: "Save draft bill" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Save draft bill" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/captures/c1/bill")).toBe(true));
    const sent = seen.find((s) => s.path === "/v1/captures/c1/bill");
    expect(sent?.headers.get("If-Match")).toBe('"4-read"');
    expect(sent?.body).toMatchObject({
      supplier_id: "s1",
      supplier_ref: "AU-2001",
      date: "2026-10-14",
      currency: "SGD",
      lines: [{ account_id: "a6120", description: "Confluence, October", gst_code: "TX", net: { amount: "100.00", currency: "SGD" }, gst: { amount: "9.00", currency: "SGD" } }],
      save_defaults: true,
    });
    expect(await screen.findByText(/The draft bill is saved with the document attached/)).toBeTruthy();
  });

  it("says why a duplicate bill was refused, and offers its files to people who may not draft only to read", async () => {
    answer({
      ...reads,
      "POST /v1/captures/c1/bill": problem(409, "CAP-02", "Atlassian Pty Ltd AU-2001 has the same supplier, number and total: it is refused as a duplicate.", "Say why in duplicate_reason"),
    });
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Open AU-2001.pdf" }));
    await screen.findByRole("option", { name: "6120 Software" });
    fireEvent.change(screen.getByLabelText("Account of line 1"), { target: { value: "a6120" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft bill" }));
    expect(await screen.findByText(/refused as a duplicate/)).toBeTruthy();
  });

  it("says a document with an identity number in full is not read, and offers no reading of it", async () => {
    answer({ ...reads, "GET /v1/captures": { status: 200, body: { items: [capture({ id: "c3", file_name: "ic.pdf", status: "received", kind: undefined, reading: undefined, checks: undefined, nric_flagged: true })] } } });
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Open ic.pdf" }));
    expect(await screen.findByText("It shows an NRIC or FIN in full")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Have it read" })).toBeTruthy(); // read with the number hidden first
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeTruthy();
  });

  it("lists accepted mail waiting to be captured and captures one file on a person's action", async () => {
    answer({ ...reads, "POST /v1/captures/from-message": { status: 201, body: capture({ id: "c9", file_name: "INV-77.txt", source: "email", status: "received", message_id: "m1", kind: undefined, reading: undefined, checks: undefined }) } });
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Capture INV-77.txt from ar@supplier.example" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/captures/from-message")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/captures/from-message")?.body).toEqual({ message_id: "m1", file_id: "f1" });
  });

  it("makes the caller's own draft claim from a read receipt, the document fact none until they say", async () => {
    answer({
      ...reads,
      "GET /v1/captures": { status: 200, body: { items: [capture({ kind: "receipt" })] } },
      "POST /v1/captures/c1/claim": { status: 201, body: { capture: capture({ status: "drafted", version: 5 }), draft: { kind: "claim", id: "cl1" } } },
    });
    renderPage(DocumentInbox, { path: "/purchases/documents", session: accountant, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Open AU-2001.pdf" }));
    await screen.findByRole("option", { name: "Local travel" });
    fireEvent.change(screen.getByLabelText("Claim category"), { target: { value: "travel" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft claim" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/captures/c1/claim")).toBe(true));
    const sent = seen.find((s) => s.path === "/v1/captures/c1/claim");
    expect(sent?.headers.get("If-Match")).toBe('"4-read"');
    expect(sent?.body).toMatchObject({ lines: [{ kind: "receipt", category_id: "travel", document: "none", amount: { amount: "190.00", currency: "SGD" } }] });
  });

  it("offers no upload, read or draft to someone who may not enter bills", async () => {
    answer(reads);
    renderPage(DocumentInbox, { path: "/purchases/documents", session: auditor, organisation, mode: "basic" });
    await screen.findByRole("table", { name: "Captured documents" });
    expect(screen.queryByText("Upload a document")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open AU-2001.pdf" }));
    await screen.findByText("What the model read");
    expect(screen.queryByRole("button", { name: "Read it again" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save draft bill" })).toBeNull();
  });
});
