import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation } from "../api/client";
import { SignedInProvider } from "../session";
import { answer, ownerSession, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Compliance } from "./compliance/Compliance";
import { ApproverWork, AuditorAccess, Deadlines, landingRoles, rowsFor, YourWork } from "./HomeWork";

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

  it("gives the roles that land elsewhere their rows on the page they land on, and only theirs", () => {
    expect(rowsFor(["administrator"]).map((w) => w.id)).toEqual(["join-requests", "invitations-open", "role-changes", "integrations-failing"]);
    expect(rowsFor(["company_secretary"]).map((w) => w.id)).toEqual(["secretary-findings", "register-approvals"]);
    expect(rowsFor(["dpo"]).map((w) => w.id)).toEqual(["data-requests", "breaches-open"]);
    expect(rowsFor(["bookkeeper", "administrator"].filter((r) => landingRoles.settings.includes(r as never))).map((w) => w.id)).not.toContain("bank-lines");
  });

  it("shows the Administrator on Settings who asks to join, invitations, role changes and failing integrations", async () => {
    answer({
      "GET /v1/members/join-requests": { status: 200, body: { items: [{ id: "j1", status: "pending" }, { id: "j2", status: "approved" }] } },
      "GET /v1/members/invitations": { status: 200, body: { items: [{ id: "i1" }, { id: "i2", withdrawn_at: "2026-11-01T00:00:00Z" }] } },
      "GET /v1/approvals": { status: 200, body: { items: [{ id: "a1", object_type: "membership" }, { id: "a2", object_type: "bill" }] } },
      "GET /v1/organisation/connectors": { status: 200, body: { items: [{ id: "c1", health: "failing" }, { id: "c2", health: "ok" }] } },
    });
    renderPage(() => <As roles={["administrator", "bookkeeper"]}><YourWork only={landingRoles.settings} /></As>, { path: "/settings" });
    const card = await screen.findByRole("region", { name: "Your work" });
    expect(await within(card).findByRole("link", { name: /1 person asking to join/ })).toBeTruthy();
    expect(within(card).getByRole("link", { name: /1 invitation waiting to be accepted/ }).getAttribute("href")).toBe("/settings/members");
    expect(within(card).getByRole("link", { name: /1 role change waiting for a second approver/ }).getAttribute("href")).toBe("/inbox");
    expect(within(card).getByRole("link", { name: /1 integration failing/ }).getAttribute("href")).toBe("/settings/integrations");
    expect(within(card).queryByText(/bank line/)).toBeNull(); // the Bookkeeper's rows are Home's
    await noViolations(document.body);
  });

  it("shows the DPO and the Company secretary their work first on Compliance, and the secretary the deadlines", async () => {
    answer({
      "GET /v1/compliance/personal-data-requests": { status: 200, body: { items: [{ id: "r1", status: "received" }, { id: "r2", status: "answered" }] } },
      "GET /v1/compliance/breaches": { status: 200, body: { items: [{ id: "b1", status: "assessed" }, { id: "b2", status: "closed" }] } },
      "GET /v1/compliance/secretary": { status: 200, body: { state: "on", findings: [{ key: "f1", status: "due" }, { key: "f2", status: "ok" }, { key: "f3", status: "review" }] } },
      "GET /v1/approvals": { status: 200, body: { items: [{ id: "a1", object_type: "holder_record" }, { id: "a2", object_type: "membership" }] } },
      "GET /v1/compliance/calendar": { status: 200, body: { today: "2026-11-17", deadlines: [{ key: "AR", title: "Annual return", due_on: "2027-03-31", status: "open", area: "corporate" }] } },
    });
    const { unmount } = renderPage(() => <As roles={["dpo"]}><Compliance /></As>, { path: "/compliance" });
    const card = await screen.findByRole("region", { name: "Your work" });
    expect(await within(card).findByRole("link", { name: /1 data request to answer/ })).toBeTruthy();
    expect(within(card).getByRole("link", { name: /1 breach still open/ }).getAttribute("href")).toBe("/compliance/data-protection");
    expect(screen.queryByRole("list", { name: "Upcoming deadlines" })).toBeNull();
    await noViolations(document.body);
    unmount();
    renderPage(() => <As roles={["company_secretary"]}><Compliance /></As>, { path: "/compliance" });
    const mine = await screen.findByRole("region", { name: "Your work" });
    expect(await within(mine).findByRole("link", { name: /2 corporate items to see to/ })).toBeTruthy();
    expect(within(mine).getByRole("link", { name: /1 register update waiting for approval/ })).toBeTruthy();
    expect(await screen.findByRole("list", { name: "Upcoming deadlines" })).toBeTruthy();
  });

  it("tells the Auditor until when their read-only access lasts, and opens what an auditor reads", async () => {
    answer({
      "GET /v1/members": { status: 200, body: { items: [{ id: "m1", user_id: ownerSession.user_id, display_name: "Ivy Audit", roles: [{ role: "auditor", expires_at: "2027-01-31T00:00:00Z" }] }] } },
    });
    const { unmount } = renderPage(() => <As roles={["auditor"]}><AuditorAccess /></As>, { path: "/reports" });
    const card = await screen.findByRole("region", { name: "Your engagement" });
    expect(await within(card).findByText(/Read-only, until 31 Jan 2027/)).toBeTruthy();
    expect(within(card).getByRole("link", { name: /Audit trail/ }).getAttribute("href")).toBe("/compliance/audit-trail");
    // What the design's auditor workspace lists: the trial balance, the general ledger, the journals, the access list.
    for (const [name, href] of [[/Trial balance/, "/reports/statements/tb"], [/General ledger/, "/reports/general-ledger"], [/Journals/, "/accounting/journals"], [/Who has access/, "/settings/access-review"]] as const) {
      expect(within(card).getByRole("link", { name }).getAttribute("href")).toBe(href);
    }
    await noViolations(document.body);
    unmount();
    renderPage(() => <As roles={["accountant"]}><AuditorAccess /></As>, { path: "/reports" });
    expect(screen.queryByRole("region", { name: "Your engagement" })).toBeNull();
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

  // req: QA-UI-08
  it("lists the Approver's requests that wait for them, the oldest first, each with its step and who it needs", async () => {
    const a = (id: string, at: string, mine: boolean) => ({
      id, status: "pending", you_may_approve: mine, summary: `Pay ${id}`, operation: "bill.post", requested_by_name: "Bea", requested_at: at,
      current_step: 1, steps: [{ quorum: 1, roles: ["approver"], users: [], approvals: [] }],
    });
    answer({ "GET /v1/approvals": { status: 200, body: { items: [a("b2", "2026-11-02T00:00:00Z", true), a("b1", "2026-11-01T00:00:00Z", true), a("b3", "2026-11-03T00:00:00Z", false)] } } });
    const { unmount } = renderPage(() => <As roles={["approver"]}><ApproverWork /></As>, { path: "/" });
    const card = await screen.findByRole("region", { name: /Waiting for your approval \(2\)/ });
    const rows = within(card).getAllByRole("listitem");
    expect(rows.map((r) => r.textContent)).toEqual([expect.stringContaining("Pay b1"), expect.stringContaining("Pay b2")]);
    expect(rows[0]!.textContent).toContain("step 1 of 1: 1 approval from Approver");
    expect(within(card).getByRole("link", { name: /Pay b1/ }).getAttribute("href")).toBe("/approvals/b1");
    await noViolations(document.body);
    unmount();
    renderPage(() => <As roles={["bookkeeper"]}><ApproverWork /></As>, { path: "/" });
    expect(screen.queryByRole("region", { name: /Waiting for your approval/ })).toBeNull();
  });
});
