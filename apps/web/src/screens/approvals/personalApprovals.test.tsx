import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PersonalApprovals } from "./PersonalApprovals";

const approver = { ...ownerSession, roles: ["approver"], user_id: "u-approver" } as Session;
const base = { object_type: "expense_claim", operation: "expense_claim.post", requested_at: "2026-11-18T09:00:00Z", expires_at: "2026-12-18T09:00:00Z", status: "pending", signatures: [] };

// req: FR-UX-39
// part of FR-AP-17, FR-APR-02, FR-APR-03 (the page; who may approve is the API's and the department scope's)
describe("to approve (a department approver)", () => {
  it("lists the claims the API says are theirs, never their own, and what they approved; axe finds nothing", async () => {
    answer({
      "GET /v1/approvals": {
        status: 200,
        body: {
          items: [
            { ...base, id: "a1", summary: "Claim by Ben, S$42.00 taxi", requested_by: "u-ben", requested_by_name: "Ben Lim", you_may_approve: true },
            { ...base, id: "a2", summary: "Claim by you, S$10.00 lunch", requested_by: "u-approver", requested_by_name: "You", you_may_approve: true },
            { ...base, id: "a3", summary: "Claim by Cai, S$99.00 hotel", requested_by: "u-cai", requested_by_name: "Cai Yu", you_may_approve: false },
            { ...base, id: "a4", status: "approved", summary: "Claim by Dee, S$5.00 parking", requested_by: "u-dee", requested_by_name: "Dee Ng", you_may_approve: false, signatures: [{ signer: "u-approver", decision: "approve" }] },
            { ...base, id: "a5", operation: "bill.post", summary: "A bill", requested_by: "u-x", requested_by_name: "X", you_may_approve: true },
          ],
        },
      },
    });
    renderPage(PersonalApprovals, { path: "/me/approve", session: approver });
    await screen.findByText("Claim by Ben, S$42.00 taxi");
    const yours = screen.getByRole("region", { name: "Waiting for you" });
    expect(within(yours).getByText("Claim by Ben, S$42.00 taxi")).toBeTruthy();
    expect(within(yours).queryByText(/Claim by you/)).toBeNull(); // never their own
    expect(within(yours).queryByText(/hotel/)).toBeNull();
    expect(within(yours).queryByText("A bill")).toBeNull(); // claims only
    const not = screen.getByRole("region", { name: "Not yours to approve" });
    expect(within(not).getByText(/your own claim, which another person approves/)).toBeTruthy();
    expect(within(not).getAllByText(/outside the departments your role covers/).length).toBeGreaterThan(0);
    expect(within(screen.getByRole("region", { name: "Recently approved by you" })).getByText(/Dee, S\$5.00 parking/)).toBeTruthy();
    await noViolations(document.body);
  });
});
