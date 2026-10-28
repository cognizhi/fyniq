import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { authenticator, credentialJSON, noAuthenticator } from "../../test/webauthn";
import { ApprovalPage } from "./ApprovalPage";
import { Inbox } from "./Inbox";
import { ReviewLog } from "./ReviewLog";
import type { Approval, ReviewEntry } from "./words";

const hash = "ab".repeat(32);
const other = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d99";
const request: Approval = {
  id: "a-1",
  object_type: "organisation_setting",
  object_key: "self_approval_threshold",
  operation: "organisation_setting.change",
  summary: "Change the self-approval threshold to 1,000.00",
  policy: "founder/organisation_setting@2026-10-20.1",
  version: 3,
  status: "pending",
  requested_at: "2026-10-20T01:00:00Z",
  requested_by: other,
  requested_by_name: "Ben Ong",
  expires_at: "2026-10-27T01:00:00Z",
  current_step: 1,
  steps: [{ quorum: 1, roles: ["owner"], users: [], approvals: 0 }],
  signatures: [],
  diff: [{ field: "Self-approval threshold", before: "2,000.00", after: "1,000.00" }],
  envelope: '{"object":"x"}',
  envelope_hash: hash,
  you_may_approve: true,
};
const challenge = { status: 200, body: { challenge_id: "c-1", expires_at: "2026-10-20T01:05:00Z", options: { publicKey: { challenge: "AQID", rpId: "localhost" } } } };
const entry: ReviewEntry = {
  id: "e-1",
  kind: "approved_change",
  object_type: "organisation_setting",
  object_key: "retention",
  summary: "Changed the retention to 7 years",
  actor: other,
  actor_name: "Ben Ong",
  approval_id: "a-0",
  logged_at: "2026-10-01T01:00:00Z",
  overdue: true,
  you_may_review: true,
};

function detail(extra = {}, a: Approval = request) {
  return answer({ "GET /v1/approvals/a-1": { status: 200, body: a, headers: { ETag: `"${a.version}"` } }, ...extra });
}

// req: FR-APR-01
// req: FR-APR-03
// req: FM-1
describe("an approval request", () => {
  it("shows what it changes, its steps and its hash, and axe finds nothing", async () => {
    detail();
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    expect(await screen.findByRole("heading", { level: 1, name: request.summary })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "1,000.00" })).toBeTruthy();
    expect(screen.getByText(/1 approval from Owner/)).toBeTruthy();
    expect(screen.getAllByText(hash).length).toBeGreaterThan(0);
    await noViolations(document.body);
  });

  // req: FR-APR-02
  // req: FM-2
  it("opens the journal a request posts, so the approver sees its lines", async () => {
    const journal: Approval = { ...request, object_type: "journal", object_key: "j-7", operation: "journal.post", summary: "Post the draft journal (Accrued audit fee), debits 2500.00",
      diff: [{ field: "status", before: "draft", after: "posted" }, { field: "debits", before: "", after: "2500.00" }] };
    detail({}, journal);
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    expect(await screen.findByText(/Journal · Post a journal/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open the journal and its lines" }).getAttribute("href")).toBe("/accounting/journals/j-7");
  });

  // req: FR-CT-03
  // req: FR-APR-02
  it("lets the approver of a change of bank details show the new number and the one it replaces, beside the letter", async () => {
    const change: Approval = { ...request, object_type: "payee_bank_change", object_key: "ch1", operation: "payee_bank_change.approve",
      summary: "Change Northwind Analytics Pte Ltd's SGD bank account to DBSSSGSG ••••5544 (Northwind Analytics Pte Ltd)",
      envelope: JSON.stringify({ dependencies: { replaces: "ch0", status: "draft" } }),
      diff: [{ field: "SGD account", before: "DBSSSGSG ••••6789 (Northwind)", after: "DBSSSGSG ••••5544 (Northwind)" }] };
    detail({
      "GET /v1/bank-changes/ch1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/bank-changes/ch1/reveal": { status: 200, body: { change_id: "ch1", account_number: "998877665544" } },
      "POST /v1/bank-changes/ch0/reveal": { status: 200, body: { change_id: "ch0", account_number: "0123456789" } },
    }, change);
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    const compare = await screen.findByRole("region", { name: "Compare with the vendor's letter" });
    fireEvent.click(within(compare).getByRole("button", { name: "Show number: The new account number" }));
    fireEvent.click(within(compare).getByRole("button", { name: "Show number: The account number it replaces" }));
    expect(await within(compare).findByText("998877665544")).toBeTruthy();
    expect(await within(compare).findByText("0123456789")).toBeTruthy();
    expect(await screen.findByText(/The vendor's letter, or a screenshot of their confirmation/)).toBeTruthy();
    await noViolations(document.body);
  });

  // req: FR-AP-22
  // req: FR-APR-02
  // req: purchases/payables Approved by another person
  it("lets the approver of a person's bank account show the number beside the person's screenshot", async () => {
    const change: Approval = { ...request, object_type: "claimant_bank_change", object_key: "pc1", operation: "claimant_bank_change.approve",
      summary: "Change Tan Ah Kow's bank account for reimbursements to DBSSSGSG ••••5544 (Tan Ah Kow)",
      envelope: JSON.stringify({ dependencies: { replaces: "", status: "draft" } }),
      diff: [{ field: "SGD account for reimbursements", before: "", after: "DBSSSGSG ••••5544 (Tan Ah Kow)" }] };
    detail({
      "GET /v1/claimant-bank-changes/pc1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/claimant-bank-changes/pc1/reveal": { status: 200, body: { change_id: "pc1", account_number: "998877665544" } },
    }, change);
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    expect(await screen.findByText(/Bank account for reimbursements · Change a person's bank account/)).toBeTruthy();
    const compare = await screen.findByRole("region", { name: "Compare with the person's screenshot" });
    expect(within(compare).queryByRole("button", { name: /The account number it replaces/ })).toBeNull();
    fireEvent.click(within(compare).getByRole("button", { name: "Show number: The new account number" }));
    expect(await within(compare).findByText("998877665544")).toBeTruthy();
    expect(await screen.findByText(/The person's screenshot of their banking app/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Attach" })).toBeNull();
    await noViolations(document.body);
  });

  // req: FR-UX-03
  it("approves only through the confirmation, signing with a passkey over the version shown", async () => {
    const credentials = authenticator();
    const seen = detail({
      "POST /v1/approvals/a-1/challenge": challenge,
      "POST /v1/approvals/a-1/approve": { status: 200, body: { ...request, status: "approved", current_step: 0 } },
    });
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    fireEvent.click(await screen.findByRole("button", { name: "Approve…" }));
    expect(calls()).not.toContain("POST /v1/approvals/a-1/approve");
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(hash)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Approve with passkey" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/approvals/a-1/approve"));
    expect(credentials.get).toHaveBeenCalledOnce();
    const ch = seen.find((s) => s.path.endsWith("/challenge"));
    expect(ch?.headers.get("If-Match")).toBe('"3"');
    const ap = seen.find((s) => s.path.endsWith("/approve"));
    expect(ap?.headers.get("If-Match")).toBe('"3"');
    expect(ap?.headers.get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
    expect(ap?.body).toEqual({ envelope_hash: hash, passkey: { challenge_id: "c-1", credential: credentialJSON } });
    expect(await screen.findByText("Approved: the change is made.")).toBeTruthy();
  });

  it("signs with the session's step-up when there is no passkey, confirming it's you first", async () => {
    authenticator();
    let n = 0;
    const seen = detail({
      "POST /v1/approvals/a-1/challenge": problem(409, "API-PASSKEY-02", "You have no passkey to sign with."),
      "POST /v1/approvals/a-1/approve": () => (++n === 1 ? problem(403, "API-AUTHZ-05", "Step-up required.") : { status: 200, body: { ...request, status: "approved" } }),
    });
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    fireEvent.click(await screen.findByRole("button", { name: "Approve…" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Approve with passkey" }));
    expect(await screen.findByRole("heading", { name: "This needs a second factor" })).toBeTruthy();
    const approves = seen.filter((s) => s.path.endsWith("/approve"));
    expect(approves).toHaveLength(1);
    expect(approves[0]?.body).toEqual({ envelope_hash: hash });
  });

  it("needs a reason to reject", async () => {
    noAuthenticator();
    detail({ "POST /v1/approvals/a-1/reject": { status: 200, body: { ...request, status: "rejected" } } });
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    fireEvent.click(await screen.findByRole("button", { name: "Reject…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Reject" }));
    expect(await within(dialog).findByText(/Say why you reject it/)).toBeTruthy();
    expect(calls()).not.toContain("POST /v1/approvals/a-1/reject");
    fireEvent.change(within(dialog).getByLabelText(/Why you reject it/), { target: { value: "Not this month" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Reject" }));
    expect(await screen.findByText("Rejected: nothing changes.")).toBeTruthy();
  });

  it("explains a request that changed under the approver (APR-04)", async () => {
    noAuthenticator();
    detail({ "POST /v1/approvals/a-1/approve": problem(409, "APR-04", "The setting changed after the request, so nothing was changed.", "Ask for the change again.") });
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    fireEvent.click(await screen.findByRole("button", { name: "Approve…" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Approve" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("APR-04");
    expect(alert.textContent).toContain("Ask for the change again.");
  });

  // req: FM-1
  it("lets the requester withdraw, and never approve", async () => {
    const mine = { ...request, requested_by: ownerSession.user_id, requested_by_name: "Ann Tan", you_may_approve: false };
    const seen = detail({ "POST /v1/approvals/a-1/withdraw": { status: 200, body: { ...mine, status: "void" } } }, mine);
    renderPage(ApprovalPage, { path: "/approvals/a-1", route: "/approvals/$approvalId" });
    expect(await screen.findByText(/Asked for by you/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Approve…" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Withdraw…" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/approvals/a-1/withdraw"));
    expect(seen.find((s) => s.path.endsWith("/withdraw"))?.headers.get("If-Match")).toBe('"3"');
  });
});

describe("the Inbox", () => {
  // req: FR-UX-03
  // req: FR-UX-21
  it("groups requests by who acts next, and counts the weekly review", async () => {
    const mine = { ...request, id: "a-2", summary: "Give Cat the Bookkeeper role", requested_by: ownerSession.user_id, you_may_approve: false };
    answer({
      "GET /v1/approvals": { status: 200, body: { items: [request, mine] } },
      "GET /v1/review-log": { status: 200, body: { items: [entry], unreviewed: 1, overdue: 1 } },
    });
    renderPage(Inbox, { path: "/inbox" });
    const toApprove = await screen.findByRole("list", { name: "To approve" });
    expect(within(toApprove).getByText(request.summary)).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "Your requests, waiting" })).getByText("Give Cat the Bookkeeper role")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("1 approval waits for you");
    expect(screen.getByText("1 high-risk action not reviewed yet")).toBeTruthy();
    expect(screen.getByText("1 overdue")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Approve/ })).toBeNull();
    await noViolations(document.body);
  });
});

// req: FR-AP-13
describe("the Inbox's bills to pay", () => {
  it("lists the bills to pay soon for whoever records payments; axe finds nothing", async () => {
    const accountant = { ...ownerSession, roles: ["accountant"] } as typeof ownerSession;
    answer({
      "GET /v1/approvals": { status: 200, body: { items: [] } },
      "GET /v1/review-log": { status: 200, body: { items: [], unreviewed: 0, overdue: 0 } },
      "GET /v1/bill-reminders": {
        status: 200,
        body: {
          days: 3,
          items: [
            { bill_id: "b1", number: "BILL-0001", supplier: { id: "c1", name: "Atlassian Pty Ltd" }, due_date: "2026-10-10", open: { amount: "1090.00", currency: "SGD" }, why: "overdue" },
            { bill_id: "b2", number: "BILL-0002", supplier: { id: "c1", name: "Atlassian Pty Ltd" }, due_date: "2026-11-30", planned_payment_on: "2026-10-23", open: { amount: "200.00", currency: "SGD" }, why: "planned" },
          ],
        },
      },
    });
    renderPage(Inbox, { path: "/inbox", session: accountant });
    const list = await screen.findByRole("list", { name: "Bills to pay" });
    const rows = within(list).getAllByRole("link");
    expect(rows[0]!.textContent).toContain("Overdue");
    expect(rows[0]!.getAttribute("href")).toBe("/purchases/bills/b1");
    expect(rows[1]!.textContent).toContain("planned 23 Oct 2026");
    await noViolations(document.body);
  });
});

// req: FM-3
describe("the review log", () => {
  it("marks the actions picked as reviewed, with a note", async () => {
    const own = { ...entry, id: "e-2", summary: "Gave Cat the Owner role", actor: ownerSession.user_id, actor_name: "Ann Tan", you_may_review: false, overdue: false };
    const seen = answer({
      "GET /v1/review-log": { status: 200, body: { items: [entry, own], unreviewed: 2, overdue: 1 } },
      "POST /v1/review-log/reviews": { status: 200, body: { reviewed: [{ ...entry, reviewed_at: "2026-10-20T02:00:00Z" }] } },
    });
    renderPage(ReviewLog, { path: "/review-log" });
    expect(await screen.findByText(entry.summary)).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: `Pick ${own.summary}` })).toBeNull();
    const mark = screen.getByRole("button", { name: /Mark as reviewed/ });
    expect((mark as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: `Pick ${entry.summary}` }));
    fireEvent.click(screen.getByRole("button", { name: "Mark as reviewed (1)" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Note (optional)"), { target: { value: "Agreed on Monday" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark as reviewed" }));
    expect(await screen.findByText("1 action marked as reviewed.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ entry_ids: ["e-1"], note: "Agreed on Monday" });
    await noViolations(document.body);
  });
});
