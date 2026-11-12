import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DataProtection } from "./DataProtection";

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const inventory = {
  classes: [{ key: "members", what: "Members' names and sign-in email addresses", why: "To let people sign in", where: "The database, in Singapore", who: "Users, roles", retention: "While a member",
    protection: "Shreddable in the audit trail", transfers: "Emails go through the email provider" }],
  not_held: [{ key: "nric", what: "NRIC and FIN numbers", why: "Payroll's live in Nemura" }],
  entries: [{ entry_id: "e-1", version: 1, what: "Job applicants' CVs", why: "To hire", where: "A shared drive", who: "The founders", retention: "6 months", transfers: "None",
    needs_dpia: false, state: "open", saved_by: "user:u-1", saved_at: "2026-11-09T02:00:00Z" }],
  schedule: [{ data: "Security events' IP address and browser", retention: "400 days (retention.security_events_days)", deletion: "Erased daily" }],
  review: { quarter: "2026-07-01", label: "Q3 2026", due_by: "2026-10-14", overdue: true },
};

// req: CMP-PD-02
// req: CMP-05
// req: CMP-PD-09
// req: CMP-PD-08
// req: CMP-PD-04
describe("Compliance › Data protection", () => {
  it("shows the inventory, what is never held, the schedule and the review overdue; axe finds nothing", async () => {
    answer({ "GET /v1/compliance/data-inventory": { status: 200, body: inventory } });
    renderPage(DataProtection, { path: "/compliance/data-protection", session: auditor });
    expect(await screen.findByText("Members' names and sign-in email addresses")).toBeTruthy();
    expect(screen.getByText("NRIC and FIN numbers")).toBeTruthy();
    expect(screen.getByText("400 days (retention.security_events_days)")).toBeTruthy();
    expect(screen.getByText(/The inventory's review of Q3 2026 is overdue/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record the review" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add an entry" })).toBeNull();
    await noViolations();
  });

  it("lets the Compliance officer add an entry and record the quarter's review", async () => {
    const seen = answer({
      "GET /v1/compliance/data-inventory": { status: 200, body: inventory },
      "POST /v1/compliance/data-inventory/entries": { status: 201, body: inventory.entries[0] },
      "POST /v1/compliance/data-inventory/reviews": { status: 201, body: { overdue: false, last_quarter: "2026-07-01" } },
    });
    renderPage(DataProtection, { path: "/compliance/data-protection", session: dpo });
    fireEvent.click(await screen.findByRole("button", { name: "Add an entry" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("What personal data"), { target: { value: "A mailing list" } });
    fireEvent.change(within(dialog).getByLabelText("Why the company holds it"), { target: { value: "To send news" } });
    fireEvent.change(within(dialog).getByLabelText("Where it is held"), { target: { value: "An email tool" } });
    fireEvent.change(within(dialog).getByLabelText("Who reads it"), { target: { value: "Marketing" } });
    fireEvent.change(within(dialog).getByLabelText("How long it is kept"), { target: { value: "Until they unsubscribe" } });
    await noViolations();
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the entry" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/compliance/data-inventory/entries")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/compliance/data-inventory/entries")?.body).toEqual({ what: "A mailing list", why: "To send news", where: "An email tool",
      who: "Marketing", retention: "Until they unsubscribe", transfers: "None", needs_dpia: false });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Record the review" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Record the review" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/compliance/data-inventory/reviews")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/compliance/data-inventory/reviews")?.body).toEqual({ quarter: "2026-07-01" });
  });

  it("shows the DPO's contact, the published policies and the penalty; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/data-inventory": { status: 200, body: inventory },
      "GET /v1/organisation/profile": { status: 200, body: { legal_name: "Cognizhi Pte Ltd", fy_start_month: 1, time_zone: "Asia/Singapore", version: 2, dpo_name: "Grace Lim", dpo_email: "dpo@cognizhi.example" } },
    });
    renderPage(DataProtection, { path: "/compliance/data-protection", session: auditor });
    fireEvent.click(await screen.findByRole("tab", { name: "Data Protection Officer" }));
    expect(await screen.findByText(/Data Protection Officer: Grace Lim, dpo@cognizhi.example/)).toBeTruthy();
    expect(screen.getByText(/up to 10% of its annual turnover in Singapore when that is above S\$10/)).toBeTruthy();
    expect(screen.getByRole("link", { name: /The privacy notices/ }).getAttribute("href")).toBe("/compliance/privacy/notices");
    await noViolations();
  });

  it("lists the impact assessments, one switched on and not approved, and lets the Compliance officer approve Fyniq's draft; axe finds nothing", async () => {
    const payroll = { key: "payroll", title: "Data protection impact assessment of the payroll link", required: true, why: "Required now: the payroll link is switched on (features.payroll).",
      version: 0, from: "fyniq-dpia-draft@1", unapproved: true };
    const ai = { key: "ai-assistant", title: "Data protection impact assessment of the AI assistant", required: false, why: "Required before the AI assistant is switched on (features.ai_assistant).",
      version: 0, from: "fyniq-dpia-draft@1", unapproved: true };
    const seen = answer({
      "GET /v1/compliance/data-inventory": { status: 200, body: inventory },
      "GET /v1/compliance/dpias": { status: 200, body: { items: [payroll, ai] } },
      "GET /v1/compliance/dpias/payroll": { status: 200, body: { ...payroll, body: "# Data protection impact assessment of the payroll link\n\n[counsel to confirm] ...", sources: [] } },
      "POST /v1/compliance/dpias/payroll/approvals": { status: 201, body: { ...payroll, version: 1, unapproved: false, approved: { version: 1, by: "user:u-1", at: "2026-11-09T02:00:00Z" } } },
    });
    renderPage(DataProtection, { path: "/compliance/data-protection", session: dpo });
    fireEvent.click(await screen.findByRole("tab", { name: "Impact assessments" }));
    const list = await screen.findByRole("list", { name: "Impact assessments" });
    const [first] = within(list).getAllByRole("listitem");
    expect(within(first!).getByText("Needs approving")).toBeTruthy();
    await noViolations();
    fireEvent.click(within(first!).getByRole("button", { name: /Open Data protection impact assessment of the payroll link/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByRole("button", { name: "Approve Fyniq's draft" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/compliance/dpias/payroll/approvals")).toBe(true));
    expect(seen.find((x) => x.path === "/v1/compliance/dpias/payroll/approvals")?.headers.get("If-Match")).toBe('"v0"');
  });

  it("lists the requests, an overdue one marked, and lets the Compliance officer answer one and read the data held; axe finds nothing", async () => {
    const req = { id: "r-1", kind: "correction", subject: "contact:k-1", requester: "Ada Tan, ada@example.com", details: "My address is wrong.", received_on: "2026-09-01",
      due_on: "2026-10-01", status: "acknowledged", overdue: true, steps: [{ step: "acknowledged", at: "2026-09-02T02:00:00Z", by: "user:u-1" }] };
    const seen = answer({
      "GET /v1/compliance/data-inventory": { status: 200, body: inventory },
      "GET /v1/compliance/personal-data-requests": { status: 200, body: { items: [req] } },
      "GET /v1/compliance/personal-data-requests/r-1/report": { status: 200, body: { subject: "contact:k-1", note: "Records are kept.", items: [{ class: "contacts", field: "Name", value: "Ada Tan" }] } },
      "POST /v1/compliance/personal-data-requests/r-1/steps": { status: 201, body: { ...req, status: "answered" } },
    });
    renderPage(DataProtection, { path: "/compliance/data-protection", session: dpo });
    fireEvent.click(await screen.findByRole("tab", { name: "Requests" }));
    const list = await screen.findByRole("list", { name: "Access and correction requests" });
    expect(within(list).getByText("Overdue")).toBeTruthy();
    await noViolations();
    fireEvent.click(within(list).getByRole("button", { name: /Read the data held/ }));
    expect(await screen.findByText("Ada Tan", { selector: "dd" })).toBeTruthy();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(within(list).getByRole("button", { name: /Next step/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Step"), { target: { value: "answered" } });
    fireEvent.change(within(dialog).getByLabelText("Outcome"), { target: { value: "corrected" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record the step" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/compliance/personal-data-requests/r-1/steps")).toBe(true));
    const step = seen.find((x) => x.path === "/v1/compliance/personal-data-requests/r-1/steps")!;
    expect(step.body).toEqual({ step: "answered", outcome: "corrected" });
    expect(step.headers.get("If-Match")).toBe('"s1"');
  });
});
