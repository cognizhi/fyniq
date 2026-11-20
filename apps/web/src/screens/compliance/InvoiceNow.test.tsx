import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { InvoiceNow } from "./InvoiceNow";

const accountant = { ...ownerSession, roles: ["accountant", "bookkeeper"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;

const gate = (extra: Record<string, unknown> = {}) => ({
  from: "2026-10-01", to: "2026-12-31", state: "blocked", documents: 3, acknowledged: 1, excluded: 1,
  missing: [{ document_id: "d-1", number: "INV-2026-0007", kind: "sale", date: "2026-10-14", state: "none", needs_choice: false }], ...extra,
});
const sub = { id: "s-1", document_id: "d-2", document_uuid: "0a1b2c3d-1111-4222-8333-444455556666", version: 1, kind: "peppol", direction: "sale", status: "queued",
  recipient: "0195:SGUEN201912345A", created_at: "2026-10-15T09:00:00Z", updated_at: "2026-10-15T09:00:00Z" };
const rx = [{ id: "r-1", message_id: "msg-1", sender: "0195:SGUEN999912345Z", number: "NC-1", received_at: "2026-10-15T09:00:00Z", status: "received", note: "no active supplier has that Peppol ID" }];

// req: FR-TAX-10
// req: FR-TAX-11
// req: FR-AR-12
// req: FR-AP-05
// req: QA-UI-04
describe("InvoiceNow", () => {
  it("shows the gate, its missing documents, the submissions and the received invoices as the server answers them; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/invoicenow-gate": { status: 200, body: gate() },
      "GET /v1/invoicenow/submissions": { status: 200, body: { items: [sub] } },
      "GET /v1/invoicenow/inbound": { status: 200, body: { items: rx } },
    });
    renderPage(InvoiceNow, { path: "/compliance/invoicenow", route: "/compliance/invoicenow", session: accountant });
    expect(await screen.findByText("Waiting for submissions")).toBeTruthy();
    expect(screen.getByText(/3 documents of the period: 1 acknowledged by IRAS, 1 left out, 1 still to do/)).toBeTruthy();
    expect(screen.getByText("INV-2026-0007")).toBeTruthy();
    expect(await screen.findByText("0195:SGUEN201912345A")).toBeTruthy();
    expect(screen.getByText(/Waiting: no active supplier has that Peppol ID/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("queues a sale, sends a queued submission and makes the draft of a waiting invoice, each with an Idempotency-Key", async () => {
    const seen = answer({
      "GET /v1/compliance/invoicenow-gate": { status: 200, body: gate() },
      "GET /v1/invoicenow/submissions": { status: 200, body: { items: [sub] } },
      "GET /v1/invoicenow/inbound": { status: 200, body: { items: rx } },
      "POST /v1/invoicenow/submissions": { status: 201, body: { ...sub, id: "s-2" } },
      "POST /v1/invoicenow/submissions/s-1/send": { status: 200, body: { ...sub, status: "sent" } },
      "POST /v1/invoicenow/inbound/r-1/draft": { status: 200, body: { ...rx[0], status: "drafted", bill_id: "b-1" } },
    });
    renderPage(InvoiceNow, { path: "/compliance/invoicenow", route: "/compliance/invoicenow", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: /Queue for InvoiceNow/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/invoicenow/submissions")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/invoicenow/submissions")?.body).toEqual({ document_id: "d-1" });
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/invoicenow/submissions")?.headers.get("Idempotency-Key")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Send now" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/invoicenow/submissions/s-1/send")).toBe(true));
    fireEvent.click(await screen.findByRole("button", { name: "Make the draft bill" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/invoicenow/inbound/r-1/draft")).toBe(true));
  });

  it("says why the gate is not assessed, and gives a reader no action", async () => {
    answer({
      "GET /v1/compliance/invoicenow-gate": { status: 200, body: gate({ state: "not_assessed", reason: "The InvoiceNow requirement is not marked as applying (setting invoicenow.applies)", missing: [] }) },
      "GET /v1/invoicenow/submissions": { status: 200, body: { items: [sub] } },
      "GET /v1/invoicenow/inbound": { status: 200, body: { items: rx } },
    });
    renderPage(InvoiceNow, { path: "/compliance/invoicenow", route: "/compliance/invoicenow", session: auditor });
    expect(await screen.findByText(/not marked as applying/)).toBeTruthy();
    await screen.findByText("0195:SGUEN201912345A");
    expect(screen.queryByRole("button", { name: /Send now|Queue for|Receive now|Make the draft/ })).toBeNull();
  });

  it("leaves a document out with its reason and approves another person's pending request, each with an Idempotency-Key", async () => {
    const controller = { ...ownerSession, roles: ["accountant", "controller"] } as Session;
    const seen = answer({
      "GET /v1/compliance/invoicenow-gate": { status: 200, body: gate({ pending_contingency: { id: "c-1", period_from: "2026-10-01", period_to: "2026-12-31", reason: "The access point was down on 30 and 31 October", evidence: "Notice INC-1", requested_by: "user:u-2", requested_at: "2026-11-01T09:00:00Z" } }) },
      "GET /v1/invoicenow/submissions": { status: 200, body: { items: [] } },
      "GET /v1/invoicenow/inbound": { status: 200, body: { items: [] } },
      "POST /v1/invoicenow/exclusions": { status: 201, body: { id: "x-1", document_id: "d-1", reason: "other", detail: "IRAS told us", recorded_by: "u", recorded_at: "2026-11-01T09:00:00Z", withdrawn: false } },
      "POST /v1/invoicenow/contingencies/c-1/approve": { status: 200, body: { id: "c-1", approved_by: "user:u-3" } },
    });
    renderPage(InvoiceNow, { path: "/compliance/invoicenow", route: "/compliance/invoicenow", session: controller });
    expect(await screen.findByText(/A request to file with the gap waits for approval/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Approve the request" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/invoicenow/contingencies/c-1/approve")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/invoicenow/contingencies/c-1/approve")?.headers.get("Idempotency-Key")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Leave it out" }));
    fireEvent.change(await screen.findByLabelText("Why it needs no submission"), { target: { value: "other" } });
    const submit = screen.getByRole("form", { name: "Leave a document out" }).querySelector("button[type=submit]") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText(/In a few words/), { target: { value: "IRAS told us" } });
    fireEvent.click(submit);
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/invoicenow/exclusions")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/invoicenow/exclusions")?.body).toEqual({ document_id: "d-1", reason: "other", detail: "IRAS told us" });
    expect(seen.find((s) => s.path === "/v1/invoicenow/exclusions")?.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
