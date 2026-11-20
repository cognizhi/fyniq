import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DisputeCard } from "./DisputeCard";

const record = (extra: Record<string, unknown> = {}) => ({
  id: "d1",
  invoice_id: "i1",
  kind: "dispute",
  amount: { amount: "40.00", currency: "SGD" },
  reason: "The training was not delivered",
  status: "open",
  version: 1,
  created_at: "2026-11-20T09:30:00Z",
  ...extra,
});
const Card = (mayRecord: boolean) => () => <DisputeCard invoiceId="i1" mayRecord={mayRecord} open="100.00 SGD" />;

// req: FR-AR-08
describe("a dispute or promise to pay on an invoice", () => {
  it("records a dispute with its amount and reason, and axe finds nothing", async () => {
    const sent = answer({
      "GET /v1/invoices/i1/disputes": { status: 200, body: { records: [] } },
      "POST /v1/invoices/i1/disputes": { status: 201, body: record() },
    });
    const { container } = renderPage(Card(true), { session: undefined });
    fireEvent.change(await screen.findByLabelText(/Amount in dispute/), { target: { value: "40.00" } });
    fireEvent.change(screen.getByLabelText("Why"), { target: { value: "The training was not delivered" } });
    fireEvent.click(screen.getByRole("button", { name: "Record it" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST")).toBe(true));
    expect(sent.find((s) => s.method === "POST")?.body).toEqual({ kind: "dispute", amount: "40.00", reason: "The training was not delivered" });
    await noViolations(container);
  });

  it("shows an open dispute as pausing reminders and resolves it with how it ended", async () => {
    const sent = answer({
      "GET /v1/invoices/i1/disputes": { status: 200, body: { records: [record()] } },
      "POST /v1/invoice-disputes/d1/resolution": { status: 200, body: record({ status: "resolved", resolution: "Credit note issued" }) },
    });
    renderPage(Card(true), { session: undefined });
    expect(await screen.findByText(/Open: its reminders are paused/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Resolve" }));
    fireEvent.change(await screen.findByLabelText("How it ended"), { target: { value: "Credit note issued" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/invoice-disputes/d1/resolution")).toBe(true));
    expect(sent.find((s) => s.path.endsWith("/resolution"))?.body).toEqual({ resolution: "Credit note issued" });
  });

  it("shows a reader the records but no way to record or resolve", async () => {
    answer({ "GET /v1/invoices/i1/disputes": { status: 200, body: { records: [record()] } } });
    renderPage(Card(false), { session: undefined });
    expect(await screen.findByText(/Disputed 40.00 SGD/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record it" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Resolve" })).toBeNull();
  });
});
