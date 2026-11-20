import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PaymentLinkCard } from "./PaymentLinkCard";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const link = (extra: Record<string, unknown> = {}) => ({
  id: "l1",
  invoice_id: "i1",
  connector_id: "c1",
  url: "https://pay.example.com/pr_1",
  reference: "INV-0150",
  amount: sgd("5450.00"),
  status: "live",
  expires_at: "2026-12-31T00:00:00Z",
  created_at: "2026-11-20T03:00:00Z",
  version: 1,
  payments: [],
  ...extra,
});
const Card = (mayAsk: boolean) => () => <PaymentLinkCard invoiceId="i1" mayAsk={mayAsk} />;

// req: FR-AR-13
// req: FR-PSP-02
describe("the payment link of an invoice", () => {
  it("asks for the link without sending an amount, then shows its page, the amount and the reference, and axe finds nothing", async () => {
    const sent = answer({
      "GET /v1/invoices/i1/payment-link": problem(404, "API-NOT-FOUND-01", "The invoice has no payment link.", "Ask for one."),
      "POST /v1/invoices/i1/payment-link": { status: 201, body: link() },
    });
    const { container } = renderPage(Card(true), { session: undefined });
    fireEvent.click(await screen.findByRole("button", { name: "Add a payment link" }));
    expect(await screen.findByDisplayValue("https://pay.example.com/pr_1")).toBeTruthy();
    expect(screen.getByText("Live")).toBeTruthy();
    expect(screen.getByText(/5,450\.00 SGD, reference INV-0150/)).toBeTruthy();
    const post = sent.find((s) => s.method === "POST");
    expect(post?.body).toEqual({}); // the amount is the invoice's open balance, read by the server
    await noViolations(container);
  });

  it("shows what the provider charged under a paid link and offers no new one", async () => {
    answer({
      "GET /v1/invoices/i1/payment-link": {
        status: 200,
        body: link({ status: "paid", payments: [{ provider_payment_id: "ch_1", amount: sgd("5450.00"), paid_at: "2026-11-21T02:00:00Z" }] }),
      },
    });
    renderPage(Card(true));
    expect(await screen.findByText("Paid")).toBeTruthy();
    expect(screen.getByText(/the provider's charge ch_1/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a payment link" })).toBeNull();
  });

  it("cancels a live link only with a reason, and says the provider's page stays payable", async () => {
    const sent = answer({
      "GET /v1/invoices/i1/payment-link": { status: 200, body: link() },
      "POST /v1/invoices/i1/payment-link/cancellation": { status: 200, body: link({ status: "cancelled", closed_reason: "Asked for a new link" }) },
    });
    renderPage(Card(true));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel the link" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/stays payable until it expires/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel the link" }));
    expect(await within(dialog).findByText("Say why the link is cancelled.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why"), { target: { value: "Asked for a new link" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel the link" }));
    await waitFor(() => expect(sent.some((s) => s.path.endsWith("/cancellation"))).toBe(true));
    expect(await screen.findByText("Cancelled")).toBeTruthy();
    expect(screen.getByText("Closed: Asked for a new link")).toBeTruthy();
  });

  it("shows the provider's refusal in plain words, and nothing to a reader with no right and no link", async () => {
    answer({
      "GET /v1/invoices/i1/payment-link": problem(404, "API-NOT-FOUND-01", "The invoice has no payment link.", "Ask for one."),
      "POST /v1/invoices/i1/payment-link": problem(422, "PSP-12", "The organisation has no single enabled payment-link connector.", "Enable HitPay under Settings › Integrations"),
    });
    renderPage(Card(true));
    fireEvent.click(await screen.findByRole("button", { name: "Add a payment link" }));
    expect(await screen.findByText(/no single enabled payment-link connector/)).toBeTruthy();
    answer({ "GET /v1/invoices/i1/payment-link": problem(404, "API-NOT-FOUND-01", "The invoice has no payment link.", "Ask for one.") });
    const { container } = renderPage(Card(false));
    await waitFor(() => expect(container.textContent).not.toContain("Payment link"));
  });
});
