import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ownerSession } from "../../test/api";
import { Subscriptions } from "./Subscriptions";

const noContracts = { "GET /v1/subscription-contracts": { status: 200, body: { items: [] } }, "GET /v1/subscription-mrr": { status: 200, body: { as_at: "2026-10-15", totals: [] } } };
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const invoice = (extra: Record<string, unknown> = {}) => ({
  id: "m1",
  provider: "stripe",
  provider_invoice_id: "in_1",
  number: "ACME-0001",
  customer: "Lumen Health Pte Ltd",
  status: "paid",
  total: sgd("100.00"),
  attempt_count: 1,
  state: "posted",
  document_id: "d1",
  seen_at: "2026-11-20T09:00:00Z",
  ...extra,
});
const sub = { id: "s1", provider: "stripe", provider_subscription_id: "sub_1", customer: "Lumen Health Pte Ltd", status: "active", amount: sgd("100.00"), interval: "every month", current_period_end: "2026-12-01", cancel_at_period_end: false };

// req: FR-PSP-03
// req: FR-PSP-07
describe("the subscriptions a provider bills", () => {
  it("shows each subscription and invoice with the provider's collection state beside what Fyniq made, and axe finds nothing", async () => {
    answer({
      ...noContracts,
      "GET /v1/payment-providers/invoices": {
        status: 200,
        body: {
          subscriptions: [sub],
          invoices: [invoice(), invoice({ id: "m2", provider_invoice_id: "in_2", number: "ACME-0002", status: "open", attempt_count: 2, next_attempt_on: "2026-11-27", state: "mirrored", document_id: undefined })],
        },
      },
    });
    const { container } = renderPage(Subscriptions, { path: "/sales/subscriptions", session: accountant });
    expect(await screen.findByText("Read-only mirror")).toBeTruthy();
    expect(screen.getAllByText("Lumen Health Pte Ltd").length).toBeGreaterThan(0);
    expect(screen.getByText(/SGD 100.00 every month/)).toBeTruthy();
    expect(screen.getByText(/2 attempts so far, next on 27 Nov 2026/)).toBeTruthy();
    expect(screen.getByText("Issued")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Issue the drafts" })).toBeNull();
    await noViolations(container);
  });

  it("raises a parked invoice and one the provider voided after it was issued, and offers to issue drafts only to those who may", async () => {
    answer({
      ...noContracts,
      "GET /v1/payment-providers/invoices": {
        status: 200,
        body: {
          subscriptions: [],
          invoices: [
            invoice({ id: "m3", provider_invoice_id: "in_3", number: "", state: "parked", parked_reason: "Not made into an invoice: it has a discount (FR-PSP-07). A person decides it.", document_id: undefined, status: "open" }),
            invoice({ id: "m4", provider_invoice_id: "in_4", number: "ACME-0004", status: "void" }),
            invoice({ id: "m5", provider_invoice_id: "in_5", number: "ACME-0005", state: "drafted", status: "open" }),
          ],
        },
      },
      "POST /v1/payment-providers/invoices/post": { status: 200, body: { posted: 1, parked: 0, waiting: 0 } },
    });
    renderPage(Subscriptions, { path: "/sales/subscriptions", session: accountant });
    expect(await screen.findByText("An invoice waits for a person")).toBeTruthy();
    expect(screen.getByText(/The provider voided or wrote off an invoice you issued/)).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Issue the drafts" }));
    expect(await screen.findByText("1 issued, 0 set aside for a person, 0 waiting.")).toBeTruthy();
  });

  it("says so when no provider is billing, and shows a reader no button", async () => {
    answer({ ...noContracts, "GET /v1/payment-providers/invoices": { status: 200, body: { subscriptions: [], invoices: [invoice({ state: "drafted" })] } } });
    renderPage(Subscriptions, { path: "/sales/subscriptions", session: auditor });
    await waitFor(() => expect(screen.getByText("Draft made, waiting to be issued")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Issue the drafts" })).toBeNull();
    answer({ ...noContracts, "GET /v1/payment-providers/invoices": { status: 200, body: { subscriptions: [], invoices: [] } } });
    renderPage(Subscriptions, { path: "/sales/subscriptions", session: accountant });
    expect(await screen.findByText("No provider is billing customers yet")).toBeTruthy();
  });

  // req: FR-AR-15
  it("lists the company's subscription contracts with the contracted MRR, and records an amendment from a day with the contract's ETag", async () => {
    const contract = { id: "k1", contact_id: "c1", name: "Growth plan", starts_on: "2026-01-15", initial_mrr: sgd("100.00"), status: "active", mrr: sgd("150.00"), version: 2 };
    answer({
      "GET /v1/payment-providers/invoices": { status: 200, body: { subscriptions: [], invoices: [] } },
      "GET /v1/subscription-contracts": { status: 200, body: { items: [contract] } },
      "GET /v1/subscription-mrr": { status: 200, body: { as_at: "2026-10-15", totals: [{ currency: "SGD", mrr: sgd("150.00"), active: 1 }], in_base: { mrr: sgd("150.00"), rates: [{ currency: "SGD", rate: "1", rate_date: "2026-10-15", source: "base" }], missing: ["JPY"] } } },
      "POST /v1/subscription-contracts/k1/amendments": { status: 201, body: { ...contract, version: 3 } },
    });
    const { container } = renderPage(Subscriptions, { path: "/sales/subscriptions", session: accountant });
    expect(await screen.findByText(/Contracted MRR on/)).toBeTruthy();
    expect(screen.getByText(/SGD 150.00 from 1 contract/)).toBeTruthy();
    expect(screen.getByText(/Converted into SGD: SGD 150.00.*Left out, with no rate for the day: JPY/)).toBeTruthy();
    expect(screen.getByText("Active")).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Amend: Growth plan" }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-11-01" } });
    fireEvent.change(screen.getByLabelText("New monthly value"), { target: { value: "200.00" } });
    fireEvent.change(screen.getByLabelText("Why"), { target: { value: "Larger team" } });
    fireEvent.click(screen.getByRole("button", { name: "Save the change" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/subscription-contracts/k1/amendments")?.body).toEqual({ kind: "upgrade", effective_on: "2026-11-01", mrr: "200.00", reason: "Larger team" }));
    expect(seen.find((s) => s.path.endsWith("/amendments"))?.headers.get("If-Match")).toBe('"2"');
  });
});
