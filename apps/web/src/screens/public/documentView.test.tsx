import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DocumentView, tokenOf } from "./DocumentView";

type PublicDocument = components["schemas"]["PublicDocument"];

const token = "q7Kx3m9VbN2pR8tY1wE5uI0oP4aS6dF3gH7jK9lZ2xC";
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const invoice: PublicDocument = {
  kind: "invoice",
  supplier: "Northwind Analytics Pte. Ltd.",
  customer: "Acme Trading Pte Ltd",
  number: "INV-0042",
  date: "2026-10-23",
  due_date: "2026-11-22",
  tax_invoice: true,
  lines: [{ description: "Analytics platform, October", quantity: "2", unit_price: "1500.00", gst_rate: "9%", amount: sgd("3000.00") }],
  subtotal: sgd("3000.00"),
  gst: sgd("270.00"),
  total: sgd("3270.00"),
  amount_due: sgd("3270.00"),
  can_answer: false,
  link_expires_at: "2027-04-22T09:30:00Z",
};
const quote: PublicDocument = { ...invoice, kind: "quote", number: "QT-0003", due_date: undefined, valid_until: "2026-11-22", amount_due: undefined, status: "sent", can_answer: true };

afterEach(() => {
  window.location.hash = "";
});

// req: FR-AR-06
// req: FR-AR-16
describe("The customer's view of a document", () => {
  it("reads the token only from the address's fragment", () => {
    expect(tokenOf(`#${token}`)).toBe(token);
    expect(tokenOf("#short")).toBe("");
    expect(tokenOf("#a token with spaces in it, longer")).toBe("");
  });

  it("shows an invoice as its PDF does, with the token in the Fyniq-Link header; axe finds nothing", async () => {
    window.location.hash = `#${token}`;
    const sent = answer({ "GET /v1/public/document": { status: 200, body: invoice } });
    const { container } = renderPage(DocumentView, { path: "/d" });
    expect(await screen.findByRole("heading", { name: "Tax invoice INV-0042" })).toBeTruthy();
    expect(screen.getByText(/Amount due SGD 3,270.00 · due 22 Nov 2026/)).toBeTruthy();
    const lines = screen.getByRole("table", { name: "Lines" });
    expect(within(lines).getAllByRole("row")[1]!.textContent).toContain("Analytics platform, October");
    expect(screen.queryByRole("button", { name: "Accept the quote" })).toBeNull();
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/public/document")).toBe(true));
    const read = sent.find((s) => s.path === "/v1/public/document")!;
    expect(read.headers.get("Fyniq-Link")).toBe(token);
    expect(`${read.path}${read.query}`).not.toContain(token);
    await noViolations(container);
  });

  it("takes a quote's acceptance only with a name", async () => {
    window.location.hash = `#${token}`;
    const sent = answer({ "GET /v1/public/document": { status: 200, body: quote }, "POST /v1/public/document/accept": { status: 200, body: { ...quote, status: "accepted", can_answer: false } } });
    renderPage(DocumentView, { path: "/d" });
    fireEvent.click(await screen.findByRole("button", { name: "Accept the quote" }));
    expect(await screen.findByText("Enter your name to accept the quote.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Your full name"), { target: { value: "Jane Tan" } });
    fireEvent.change(screen.getByLabelText("Comment (optional)"), { target: { value: "Please start on 1 November" } });
    fireEvent.click(screen.getByRole("button", { name: "Accept the quote" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/public/document/accept")).toBe(true));
    const post = sent.find((s) => s.path === "/v1/public/document/accept")!;
    expect(post.body).toEqual({ name: "Jane Tan", comment: "Please start on 1 November" });
    expect(post.headers.get("Fyniq-Link")).toBe(token);
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("says a link without its key is incomplete, and asks for nothing", async () => {
    renderPage(DocumentView, { path: "/d" });
    expect(await screen.findByRole("heading", { name: "This link is incomplete" })).toBeTruthy();
  });
});
