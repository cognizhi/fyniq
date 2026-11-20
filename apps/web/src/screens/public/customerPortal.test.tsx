import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { CustomerPortal } from "./CustomerPortal";

type Portal = components["schemas"]["PortalSummary"];

const token = "link".repeat(8);
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const summary: Portal = {
  supplier: "Northwind Analytics Pte. Ltd.",
  customer: "Acme Trading Pte Ltd",
  today: "2026-11-20",
  link_expires_at: "2026-12-20T09:30:00Z",
  statement_currencies: [{ currency: "SGD" }],
  invoices: [
    { id: "i1", number: "INV-0150", date: "2026-10-23", due_date: "2026-11-22", total: sgd("5450.00"), open: sgd("5450.00"), overdue: false, payment_url: "https://pay.example.com/pr_1" },
    { id: "i2", number: "INV-0140", date: "2026-09-01", due_date: "2026-09-30", total: sgd("100.00"), open: sgd("40.00"), overdue: true },
  ],
};

afterEach(() => {
  window.location.hash = "";
});

// req: FR-AR-14
// req: FR-AR-13
describe("the customer portal", () => {
  it("shows the customer's invoices due with a Pay now only where a payment link exists, sends the token in a header and never in the address, and axe finds nothing", async () => {
    window.location.hash = `#${token}`;
    const seen = answer({ "GET /v1/public/portal": { status: 200, body: summary } });
    const { container } = renderPage(CustomerPortal, { path: "/portal", session: undefined });
    expect(await screen.findByText("INV-0150")).toBeTruthy();
    expect(screen.getByText("INV-0140")).toBeTruthy();
    expect(screen.getByText("2 invoices due")).toBeTruthy();
    const pay = screen.getAllByRole("link", { name: "Pay now" });
    expect(pay.length).toBe(1);
    expect(pay[0]?.getAttribute("href")).toBe("https://pay.example.com/pr_1");
    expect(pay[0]?.getAttribute("rel")).toContain("noopener");
    expect(screen.getByText("Overdue")).toBeTruthy();
    expect(screen.getByText(/Where there is no Pay button/)).toBeTruthy();
    const first = seen.find((s) => s.path === "/v1/public/portal");
    expect(first?.headers.get("Fyniq-Portal")).toBe(token);
    expect(first?.query).not.toContain(token);
    await noViolations(container);
  });

  it("says plainly when the link is incomplete, and shows one message when it opens nothing", async () => {
    renderPage(CustomerPortal, { path: "/portal", session: undefined });
    expect(await screen.findByText("This link is incomplete")).toBeTruthy();
    window.location.hash = `#${token}`;
    answer({ "GET /v1/public/portal": problem(404, "PORTAL-01", "This link does not open anything: it may have expired or been withdrawn.", "Ask the company for a new link") });
    renderPage(CustomerPortal, { path: "/portal", session: undefined });
    expect(await screen.findByText("This link does not open anything")).toBeTruthy();
    expect(screen.getByText(/may have expired or been withdrawn/)).toBeTruthy();
  });

  it("reads the statement for a chosen period and says what the customer owes", async () => {
    window.location.hash = `#${token}`;
    const seen = answer({
      "GET /v1/public/portal": { status: 200, body: summary },
      "GET /v1/public/portal/statement": {
        status: 200,
        body: {
          customer: "Acme Trading Pte Ltd",
          currency: "SGD",
          from: "2026-10-21",
          to: "2026-11-20",
          opening: sgd("0.00"),
          closing: sgd("5450.00"),
          overdue: sgd("0.00"),
          lines: [{ date: "2026-10-23", description: "Invoice INV-0150, due 22 Nov 2026", amount: sgd("5450.00"), balance: sgd("5450.00") }],
        },
      },
    });
    renderPage(CustomerPortal, { path: "/portal", session: undefined });
    fireEvent.click(await screen.findByRole("button", { name: "Show the statement" }));
    expect(await screen.findByText("Invoice INV-0150, due 22 Nov 2026")).toBeTruthy();
    expect(screen.getByText(/You owe SGD 5,450.00 on/)).toBeTruthy();
    const call = seen.find((s) => s.path === "/v1/public/portal/statement");
    expect(call?.headers.get("Fyniq-Portal")).toBe(token);
    expect(call?.query).toContain("to=2026-11-20");
    await waitFor(() => expect(screen.getByRole("button", { name: "Print" })).toBeTruthy());
  });
});
