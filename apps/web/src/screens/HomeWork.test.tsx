import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation } from "../api/client";
import { SignedInProvider } from "../session";
import { answer, ownerSession, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Deadlines, rowsFor, YourWork } from "./HomeWork";

const organisation: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };

function As({ roles, children }: { roles: string[]; children: React.ReactNode }) {
  return <SignedInProvider value={{ session: { ...ownerSession, roles } as never, organisation, mode: "basic" }}>{children}</SignedInProvider>;
}

const states = (due: string, payment: string) => ({ approval: "approved", due, ledger: "posted", payment });

// §7.1's table, the reference for QA-UI-08: what each role's Home shows beyond the pulse and the Inbox.
// req: FR-UX-40
// req: QA-UI-08
describe("Home's work by role", () => {
  it("gives each role its rows, a person with two roles each row once, and the full-Home roles none", () => {
    expect(rowsFor(["bookkeeper"]).map((w) => w.id)).toEqual(["bank-lines", "draft-invoices", "draft-bills"]);
    expect(rowsFor(["ar_specialist"]).map((w) => w.id)).toEqual(["draft-invoices", "overdue-invoices", "receipts-to-apply", "quotes-waiting"]);
    expect(rowsFor(["payment_releaser"]).map((w) => w.id)).toEqual(["batches-to-release", "payments-rejected"]);
    const both = rowsFor(["bookkeeper", "ap_specialist"]).map((w) => w.id);
    expect(both.filter((id) => id === "draft-bills")).toHaveLength(1);
    expect(both).toContain("bank-detail-changes");
    for (const r of ["owner", "controller", "accountant", "tax_agent", "employee"]) expect(rowsFor([r])).toEqual([]);
  });

  it("counts the AP specialist's work from the lists it opens, and leaves out a read the API refuses", async () => {
    answer({
      "GET /v1/bills": (_b, req) => {
        const q = new URL(req.url).searchParams;
        return q.get("status") === "draft"
          ? { status: 200, body: { items: [{ id: "b0" }] } }
          : { status: 200, body: { items: [{ id: "b1", states: states("overdue", "unpaid") }, { id: "b2", states: states("not_due", "part_paid") }, { id: "b3", states: states("not_due", "paid") }] } };
      },
      "GET /v1/payment-batches": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims": { status: 200, body: { items: [{ id: "c1" }, { id: "c2" }], next_cursor: "more" } },
      "GET /v1/approvals": problem(403, "API-AUTHZ-01"),
    });
    renderPage(() => <As roles={["ap_specialist"]}><YourWork /></As>, { path: "/" });
    const card = await screen.findByRole("region", { name: "Your work" });
    expect(await within(card).findByRole("link", { name: /1 bill overdue/ })).toBeTruthy();
    expect(within(card).getByRole("link", { name: /1 draft bill to finish/ }).getAttribute("href")).toBe("/purchases/bills");
    expect(within(card).getByRole("link", { name: /2 bills to pay/ })).toBeTruthy();
    expect(within(card).getByRole("link", { name: /No payment batches being prepared/ })).toBeTruthy();
    expect(within(card).getByRole("link", { name: /2 or more approved claims to pay/ })).toBeTruthy();
    expect(within(card).queryByText(/bank-detail/)).toBeNull(); // refused: left out, never "No …"
    await noViolations(document.body);
  });

  it("shows the payment releaser its batches, the bank's rejections and the bank balances", async () => {
    answer({
      "GET /v1/payment-batches": (_b, req) => {
        const status = new URL(req.url).searchParams.get("status");
        if (status === "approved") return { status: 200, body: { items: [{ id: "p1", payments: [] }, { id: "p2", payments: [] }] } };
        if (status === "released") return { status: 200, body: { items: [{ id: "p3", payments: [{ rejected: true }, { rejected: false }] }] } };
        return { status: 200, body: { items: [{ id: "p4", payments: [{ rejected: true }] }] } };
      },
      "GET /v1/cash-position": {
        status: 200,
        body: { date: "2026-11-17", total: { amount: "1234.50", currency: "SGD" }, no_rate: [], accounts: [{ money_account_id: "m1", name: "DBS current", kind: "bank", balance: { amount: "1234.50", currency: "SGD" } }] },
      },
    });
    renderPage(() => <As roles={["payment_releaser"]}><YourWork /></As>, { path: "/" });
    const card = await screen.findByRole("region", { name: "Your work" });
    expect(await within(card).findByRole("link", { name: /2 batches waiting for release/ })).toBeTruthy();
    expect(await within(card).findByRole("link", { name: /2 payments the bank rejected/ })).toBeTruthy();
    expect(await within(card).findByRole("heading", { name: "Bank balances on 17 Nov 2026" })).toBeTruthy();
    expect(within(card).getByText("1,234.50 SGD")).toBeTruthy();
  });

  // req: FR-CMP-01
  it("lists the next deadlines, overdue and due soon first, and opens the calendar only for those whose areas hold it", async () => {
    const d = (key: string, due_on: string, status: string) => ({ key, title: key, due_on, status, area: "tax" });
    answer({
      "GET /v1/compliance/calendar": {
        status: 200,
        body: { today: "2026-11-17", deadlines: [d("Annual return", "2027-03-31", "open"), d("GST F5", "2026-11-30", "due_soon"), d("CPF", "2026-11-14", "overdue"), d("ECI", "2026-10-31", "filed")] },
      },
    });
    const { unmount } = renderPage(() => <As roles={["owner"]}><Deadlines /></As>, { path: "/" });
    const list = await screen.findByRole("list", { name: "Upcoming deadlines" });
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["CPFDue 14 Nov 2026Overdue", "GST F5Due 30 Nov 2026Due soon", "Annual returnDue 31 Mar 2027"]);
    expect(screen.getByRole("link", { name: "The statutory calendar" })).toBeTruthy();
    unmount();
    renderPage(() => <As roles={["bookkeeper"]}><Deadlines /></As>, { path: "/" });
    await screen.findByRole("list", { name: "Upcoming deadlines" });
    expect(screen.queryByRole("link", { name: "The statutory calendar" })).toBeNull();
  });
});
