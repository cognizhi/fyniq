import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { ProviderPayouts } from "./ProviderPayouts";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
type Account = components["schemas"]["PaymentProviderAccount"];
type Payout = components["schemas"]["PaymentProviderPayout"];
type Dispute = components["schemas"]["PaymentProviderDispute"];

const account = (extra: Partial<Account> = {}): Account => ({
  money_account_id: "m1",
  money_account_name: "Stripe clearing",
  connector_id: "c1",
  provider: "stripe",
  currency: "SGD",
  fee_account_id: "a5040",
  fee_account_name: "Payment fees",
  disputes_account_id: "a1055",
  disputes_account_name: "Payment disputes clearing",
  book_balance: sgd("1846.2000"),
  provider_balance: sgd("1846.20"),
  swept_through: "2026-10-12",
  version: 1,
  ...extra,
});
const payout = (extra: Partial<Payout> = {}): Payout => ({
  id: "p1",
  payout_id: "po_1Q",
  money_account_id: "m1",
  amount: sgd("5414.27"),
  arrival_date: "2026-10-10",
  automatic: true,
  status: "paid",
  payout_line_posted: true,
  nets_to_zero: true,
  transactions: [
    { txn_id: "txn_ch1", category: "charge", gross: sgd("5450.00"), fee: sgd("35.73"), net: sgd("5414.27"), posted: true },
    { txn_id: "txn_po1", category: "payout", gross: sgd("-5414.27"), fee: sgd("0.00"), net: sgd("-5414.27"), posted: true },
  ],
  ...extra,
});
const dispute = (extra: Partial<Dispute> = {}): Dispute => ({
  dispute_id: "dp_1",
  money_account_id: "m1",
  status: "open",
  all_posted: true,
  over_limit: false,
  disputed: sgd("109.00"),
  held: sgd("124.00"),
  credited: sgd("0.00"),
  ...extra,
});
const reads = (over: Record<string, unknown> = {}) => ({
  "GET /v1/payment-providers/accounts": { status: 200, body: { items: [account()] } },
  "GET /v1/payment-providers/payouts": { status: 200, body: { items: [payout()] } },
  "GET /v1/payment-providers/disputes": { status: 200, body: { items: [] } },
  "GET /v1/payment-providers/events": { status: 200, body: { items: [] } },
  ...over,
});

// req: FR-PSP-04
// req: FR-PSP-05
// req: FR-PSP-06
describe("provider payouts", () => {
  it("shows a provider against its balance, a payout broken down with its check, and axe finds nothing", async () => {
    answer(reads());
    const { container } = renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    expect(await screen.findByRole("heading", { name: "Stripe · SGD" })).toBeTruthy();
    expect(screen.getByText("Clearing agrees")).toBeTruthy();
    expect(screen.getByText("Complete to 12 Oct 2026")).toBeTruthy();
    const detail = await screen.findByRole("complementary", { name: "Payout po_1Q" });
    expect(within(detail).getByText("Nets to zero")).toBeTruthy();
    expect(within(detail).getByText("Charges (1)")).toBeTruthy();
    expect(within(detail).getByText("5,450.00")).toBeTruthy();
    expect(within(detail).getByText("35.73")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    await noViolations(container);
  });

  it("raises a payout that does not net to zero at the top, and says when the provider's balance differs", async () => {
    answer(
      reads({
        "GET /v1/payment-providers/accounts": { status: 200, body: { items: [account({ book_balance: sgd("12.00"), provider_balance: sgd("0.00") })] } },
        "GET /v1/payment-providers/payouts": {
          status: 200,
          body: { items: [payout({ nets_to_zero: false, difference: sgd("12.00"), missing: "A refund fee Stripe has not reported." })] },
        },
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Stripe payout po_1Q does not net to zero");
    expect(alert.textContent).toContain("A refund fee Stripe has not reported.");
    expect(screen.getByText("Clearing differs")).toBeTruthy();
    expect(screen.getAllByText("12.00 left").length).toBeGreaterThan(0);
  });

  it("says no provider is set up, and that a payout still waits for its bank line", async () => {
    answer(reads({ "GET /v1/payment-providers/accounts": { status: 200, body: { items: [] } } }));
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    expect(await screen.findByText("No payment provider is set up")).toBeTruthy();
    answer(reads({ "GET /v1/payment-providers/payouts": { status: 200, body: { items: [payout({ nets_to_zero: undefined, payout_line_posted: false })] } } }));
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    expect((await screen.findAllByText("Waiting for its bank line")).length).toBeGreaterThan(0);
  });

  it("writes a lost dispute off as a draft journal: above the limit it asks for the confirmation and a reason first", async () => {
    const sent = answer(
      reads({
        "GET /v1/payment-providers/disputes": { status: 200, body: { items: [dispute({ over_limit: true, held: sgd("224.00"), disputed: sgd("209.00") })] } },
        "POST /v1/payment-providers/disputes/dp_1/loss": { status: 201, body: { dispute_id: "dp_1", document_id: "j1", amount: sgd("224.00"), over_limit: true } },
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Write off dispute dp_1" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Prepare the draft journal" }));
    await invalid(dialog, "Why", "Confirm that the invoice's revenue stays as invoiced, or settle a credit note instead.");
    fireEvent.click(within(dialog).getByLabelText("The invoice's revenue stays as invoiced"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Prepare the draft journal" }));
    await invalid(dialog, "Why", "Say why: the reason is kept with the draft journal.");
    fireEvent.change(within(dialog).getByLabelText("Why"), { target: { value: "Chargeback lost" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Prepare the draft journal" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/payment-providers/disputes/dp_1/loss")?.body).toEqual({ money_account_id: "m1", accept_overstated_revenue: true, reason: "Chargeback lost" }));
  });

  it("settles an issued credit note's credit against a lost dispute, and shows a refusal in plain words", async () => {
    const note = { id: "cn1", kind: "credit_note", number: "CN-0001", open: sgd("109.00"), customer: { name: "Acme Analytics" } };
    const sent = answer(
      reads({
        "GET /v1/payment-providers/disputes": { status: 200, body: { items: [dispute()] } },
        "GET /v1/invoices": { status: 200, body: { items: [note, { ...note, id: "cn2", number: "CN-0002", open: sgd("0.00") }] } },
        "POST /v1/payment-providers/disputes/dp_1/credit-settlement": problem(422, "PSP-08", "The credit cannot settle this dispute.", "Credit the invoice of the disputed charge."),
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Settle a credit note against dispute dp_1" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Settle the credit" }));
    await invalid(dialog, "Credit note", "Choose the credit note that credits the disputed invoice.");
    const options = await within(dialog).findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Choose an issued credit note", "CN-0001 · Acme Analytics · 109.00 SGD left"]); // a spent credit note is not offered
    fireEvent.change(within(dialog).getByLabelText("Credit note"), { target: { value: "cn1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Settle the credit" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/payment-providers/disputes/dp_1/credit-settlement")?.body).toEqual({ money_account_id: "m1", credit_note_id: "cn1" }));
    expect(await within(dialog).findByText("The credit was not settled")).toBeTruthy();
  });

  it("shows a credited dispute and the draft journal of a written-off one, with no action left", async () => {
    answer(
      reads({
        "GET /v1/payment-providers/disputes": {
          status: 200,
          body: { items: [dispute({ status: "credited", credited: sgd("109.00"), held: sgd("15.00"), settlement_id: "r1" }), dispute({ dispute_id: "dp_2", status: "written_off", loss_document_id: "j2" })] },
        },
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    expect(await screen.findByText("Credited")).toBeTruthy();
    expect(screen.getByText("109.00 SGD credited")).toBeTruthy();
    expect(screen.getByText("Written off")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open the draft journal" }).getAttribute("href")).toBe("/accounting/journals/j2");
    expect(screen.queryByRole("button", { name: /Write off dispute/ })).toBeNull();
  });

  it("marks a parked event as dealt with, with a reason; a person without the right only reads", async () => {
    const event = { id: "e1", connector_id: "c1", provider: "stripe", event_id: "evt_1", event_type: "invoice.paid", status: "parked", received_at: "2026-10-11T02:00:00Z", parser_version: "1", parked_reason: "Fyniq does not know this event type" };
    const sent = answer(
      reads({
        "GET /v1/payment-providers/events": { status: 200, body: { items: [event] } },
        "POST /v1/payment-providers/events/e1/resolution": { status: 200, body: { ...event, status: "processed" } },
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: accountant });
    expect(await screen.findByText("1 event parked")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Mark the invoice.paid event as dealt with" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark as dealt with" }));
    await invalid(dialog, "What you did, or why nothing is needed", "Say what you did or why nothing is needed: it is kept with the event.");
    fireEvent.change(within(dialog).getByLabelText("What you did, or why nothing is needed"), { target: { value: "Subscription billing is not used" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark as dealt with" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/payment-providers/events/e1/resolution")?.body).toEqual({ reason: "Subscription billing is not used" }));
  });

  it("offers a reader no action", async () => {
    answer(
      reads({
        "GET /v1/payment-providers/disputes": { status: 200, body: { items: [dispute()] } },
        "GET /v1/payment-providers/events": { status: 200, body: { items: [{ id: "e1", connector_id: "c1", provider: "stripe", event_id: "evt_1", event_type: "x.y", status: "parked", received_at: "2026-10-11T02:00:00Z", parser_version: "1" }] } },
      }),
    );
    renderPage(ProviderPayouts, { path: "/banking/payouts", session: auditor });
    await screen.findByRole("table", { name: "Disputes" });
    expect(screen.queryByRole("button", { name: /Write off dispute|Settle a credit note|dealt with/ })).toBeNull();
  });
});
