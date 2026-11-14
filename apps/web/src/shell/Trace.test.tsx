import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer, seen } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { TracePanel, words, type TraceFigure } from "./Trace";

const m = (amount: string) => ({ amount, currency: "SGD" });
const figure: TraceFigure = {
  title: "Software",
  amount: "389.30",
  currency: "SGD",
  context: "Profit and loss, Sept 2026",
  from: "2026-09-01",
  to: "2026-09-30",
  revision: 18412,
  accounts: [
    { id: "a-6120", code: "6120", name: "Software subscriptions", amount: "389.30" },
    { id: "a-6130", code: "6130", name: "Cloud hosting", amount: "0.00" },
  ],
};
const ledger = {
  from: "2026-09-01",
  to: "2026-09-30",
  currency: "SGD",
  ledger_revision: 18412,
  accounts: [
    {
      account_id: "a-6120",
      code: "6120",
      name: "Software subscriptions",
      opening: m("0.00"),
      debits: m("389.30"),
      credits: m("0.00"),
      closing: m("389.30"),
      lines: [
        { posting_date: "2026-09-05", entry_id: "e1", ledger_revision: 18402, source_type: "bill", source_id: "b905", source_version: 1, number: "BILL-0905",
          reversal: false, memo: "GitHub Team, September", amount: { amount: "290.00", currency: "USD" }, base: m("389.30"), balance: m("389.30") },
      ],
    },
  ],
};
const entry = {
  id: "e1",
  posting_date: "2026-09-05",
  ledger_revision: 18402,
  memo: "GitHub Team, September",
  source: { type: "bill", id: "b905", version: 1, document_type: "bill", number: "BILL-0905", status: "posted", date: "2026-09-05", party: "GitHub, Inc.",
    current_version: 1, link: "/purchases/bills/b905" },
  lines: [
    { line_no: 1, account_id: "a-6120", code: "6120", name: "Software subscriptions", amount: { amount: "290.00", currency: "USD" }, base: m("389.30") },
    { line_no: 2, account_id: "a-2010", code: "2010", name: "Accounts payable", amount: { amount: "-290.00", currency: "USD" }, base: m("-389.30") },
  ],
};
const history = {
  items: [
    { id: "x2", seq: 9, occurred_at: "2026-09-06T02:00:00Z", actor: { type: "user", id: "u1" }, action: "document.posted", record: { type: "document", id: "b905" }, fields: [] },
    { id: "x1", seq: 8, occurred_at: "2026-09-05T02:00:00Z", actor: { type: "user", id: "u2" }, action: "document.created", record: { type: "document", id: "b905" }, fields: [] },
  ],
};

function render(onClose = () => undefined) {
  return renderPage(() => <TracePanel figure={figure} onClose={onClose} />, { path: "/reports/statements/pl" });
}

// req: FR-UX-04
// req: FR-UX-05
// req: FR-RPT-08
describe("the Trace panel", () => {
  it("follows a total to its accounts, an account's lines as at the report's revision, a line's source and its history; axe finds nothing", async () => {
    answer({
      "GET /v1/reports/general-ledger": { status: 200, body: ledger },
      "GET /v1/ledger/entries/e1": { status: 200, body: entry },
      "GET /v1/audit/events": { status: 200, body: history },
    });
    const closed = vi.fn();
    render(closed);
    const panel = await screen.findByRole("dialog", { name: "Software" });
    expect(within(panel).getAllByText("389.30").length).toBeGreaterThan(0);
    expect(within(panel).getByText(/Read at ledger revision 18412/)).toBeTruthy();
    expect((within(panel).getByRole("tab", { name: "3 Source" }) as HTMLButtonElement).disabled).toBe(true);
    await noViolations(document.body);

    fireEvent.click(within(panel).getByRole("button", { name: /Software subscriptions/ }));
    const line = await within(panel).findByRole("button", { name: /GitHub Team, September · BILL-0905/ });
    const gl = seen.find((s) => s.path === "/v1/reports/general-ledger")!;
    expect(gl.query).toContain("account_id=a-6120");
    expect(gl.query).toContain("as_at_revision=18412");
    expect(within(panel).getByText("1 line")).toBeTruthy();

    fireEvent.click(line);
    expect(await within(panel).findByRole("heading", { name: "Source: BILL-0905" })).toBeTruthy();
    expect(within(panel).getByText("GitHub, Inc.")).toBeTruthy();
    expect(within(panel).getByRole("link", { name: "Open it" }).getAttribute("href")).toBe("/purchases/bills/b905");
    expect(within(panel).getByRole("navigation", { name: "Trace path" }).textContent).toContain("BILL-0905");

    fireEvent.click(within(panel).getByRole("button", { name: "See its history" }));
    expect(await within(panel).findByText("Posted")).toBeTruthy();
    expect(within(panel).getByText("Created")).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/audit/events")!.query).toContain("record_id=b905");
    await noViolations(document.body);

    // The path goes back a step; its first step, and closing, return to the report.
    fireEvent.click(within(panel).getByRole("button", { name: "Software subscriptions" }));
    expect(await within(panel).findByRole("button", { name: /GitHub Team, September/ })).toBeTruthy();
    fireEvent.click(within(panel).getByRole("button", { name: "Profit and loss, Sept 2026" }));
    expect(closed).toHaveBeenCalled();
  });

  it("says the history is the audit trail's when the person's roles do not read it", async () => {
    answer({
      "GET /v1/reports/general-ledger": { status: 200, body: ledger },
      "GET /v1/ledger/entries/e1": { status: 200, body: entry },
      "GET /v1/audit/events": { status: 403, body: { code: "API-AUTHZ-01", detail: "Your roles do not allow this." } },
    });
    render();
    const panel = await screen.findByRole("dialog", { name: "Software" });
    fireEvent.click(within(panel).getByRole("button", { name: /Software subscriptions/ }));
    fireEvent.click(await within(panel).findByRole("button", { name: /GitHub Team, September/ }));
    fireEvent.click(await within(panel).findByRole("button", { name: "See its history" }));
    expect(await within(panel).findByText(/which your roles do not show/)).toBeTruthy();
  });

  it("names an audit action in plain words", () => {
    expect(words("document.posted")).toBe("Posted");
    expect(words("bill_payment.voided_by_owner")).toBe("Voided by owner");
  });
});
