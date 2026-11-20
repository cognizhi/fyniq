import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AuditorEngagement } from "./AuditorEngagement";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const AUD = "11111111-1111-4111-8111-111111111111";
const ACC = "22222222-2222-4222-8222-222222222222";
const eng = { id: "e1", name: "FY2026 audit", auditor_user_id: AUD, period_from: "2026-01-01", period_to: "2026-12-31", opens_on: "2026-10-01", closes_on: "2026-12-31", status: "pending", opened_at: "2026-10-01T02:00:00Z" };
const members = {
  items: [
    { id: "m1", user_id: AUD, display_name: "Ada Auditor", roles: [{ role: "auditor" }] },
    { id: "m2", user_id: ACC, display_name: "Ann Accountant", roles: [{ role: "accountant" }] },
  ],
};

// req: FR-UX-42
describe("the auditor's engagement", () => {
  it("lets an Owner open an engagement and confirm a pending one; axe finds nothing", async () => {
    answer({
      "GET /v1/audit-engagements": { status: 200, body: { items: [eng] } },
      "GET /v1/members": { status: 200, body: members },
      "GET /v1/audit-engagements/e1/requests": { status: 200, body: { items: [] } },
      "GET /v1/audit-engagements/e1/samples": { status: 200, body: { items: [] } },
      "POST /v1/audit-engagements": { status: 201, body: eng },
      "POST /v1/audit-engagements/e1/confirm": { status: 200, body: { ...eng, status: "active" } },
    });
    renderPage(AuditorEngagement, { path: "/auditor/engagement", session: owner, mode: "expert" });
    await screen.findByText(/it is pending/);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "FY2026 audit" } });
    fireEvent.change(await screen.findByLabelText("Auditor"), { target: { value: AUD } });
    const dates: [string, string][] = [["Period from", "2026-01-01"], ["Period to", "2026-12-31"], ["Opens on", "2026-10-01"], ["Closes on", "2026-12-31"]];
    for (const [label, v] of dates) fireEvent.change(screen.getByLabelText(label), { target: { value: v } });
    fireEvent.click(screen.getByRole("button", { name: /Open it/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/audit-engagements")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/audit-engagements" && s.method === "POST")!.body).toMatchObject({ name: "FY2026 audit", auditor_user_id: AUD, closes_on: "2026-12-31" });
    fireEvent.click(await screen.findByRole("button", { name: /Confirm it/ }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit-engagements/e1/confirm")).toBe(true));
    await noViolations(document.body);
  });

  it("lets the auditor ask, draw a sample and accept an answer, and shows the sampled lines", async () => {
    const active = { ...eng, status: "active" };
    const req = { id: "r1", engagement_id: "e1", number: 1, title: "Bank confirmation", assignee_user_id: ACC, status: "answered", requested_at: "2026-10-02T02:00:00Z", answer: "Attached", evidence_ids: [] };
    const sample = { id: "s1", engagement_id: "e1", rule: { from: "2026-01-01", to: "2026-12-31", count: 5 }, seed: "abc", population: 12, drawn_at: "2026-10-03T02:00:00Z",
      items: [{ entry_id: "x1", line_no: 1, posting_date: "2026-06-15", account: "6120", account_name: "Software", base_amount: "-1500.00", source_type: "bill" }] };
    answer({
      "GET /v1/audit-engagements": { status: 200, body: { items: [active] } },
      "GET /v1/members": { status: 200, body: members },
      "GET /v1/audit-engagements/e1/requests": { status: 200, body: { items: [req] } },
      "GET /v1/audit-engagements/e1/samples": { status: 200, body: { items: [sample] } },
      "POST /v1/audit-engagements/e1/requests": { status: 201, body: req },
      "POST /v1/audit-engagements/e1/samples": { status: 201, body: sample },
      "POST /v1/audit-requests/r1/close": { status: 200, body: { ...req, status: "closed" } },
    });
    renderPage(AuditorEngagement, { path: "/auditor/engagement", session: auditor, mode: "expert" });
    expect(await screen.findByText(/1 of 12 lines|1 of 12/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Open it/ })).toBeNull();
    fireEvent.change(screen.getByLabelText("Ask for"), { target: { value: "Board minutes" } });
    fireEvent.change(await screen.findByLabelText("Answered by"), { target: { value: ACC } });
    fireEvent.click(screen.getByRole("button", { name: /^Ask/ }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit-engagements/e1/requests" && s.method === "POST")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/audit-engagements/e1/requests" && s.method === "POST")!.body).toMatchObject({ title: "Board minutes", assignee_user_id: ACC });
    fireEvent.click(screen.getByRole("button", { name: /Draw a sample/ }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit-engagements/e1/samples" && s.method === "POST")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/audit-engagements/e1/samples" && s.method === "POST")!.body).toMatchObject({ rule: { from: "2026-01-01", to: "2026-12-31", count: 25 } });
    fireEvent.click(screen.getByRole("button", { name: /Accept and close request 1/ }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit-requests/r1/close")).toBe(true));
    await noViolations(document.body);
  });
});
