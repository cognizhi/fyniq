import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation } from "../api/client";
import { SignedInProvider } from "../session";
import { answer, ownerSession, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Home } from "./Home";

const organisation: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };

function WithOrganisation() {
  return (
    <SignedInProvider value={{ session: ownerSession as never, organisation, mode: "basic" }}>
      <Home />
    </SignedInProvider>
  );
}

// req: FR-UX-10
// req: FR-UX-21
describe("Home", () => {
  it("shows what waits for you and the set-up steps the API can tell, and axe finds nothing", async () => {
    answer({
      "GET /v1/approvals": { status: 200, body: { items: [{ id: "a1", you_may_approve: true }, { id: "a2", you_may_approve: false }] } },
      "GET /v1/review-log": { status: 200, body: { items: [{ id: "e1", you_may_review: true }], unreviewed: 1, overdue: 1 } },
      "GET /v1/organisation/profile": { status: 200, body: { legal_name: "Cognizhi Pte Ltd", uen: "202612345K", fy_start_month: 1, time_zone: "Asia/Singapore", version: 1 } },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "x", money_account: true, active: true }] } },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m1" }] } },
      "GET /v1/members/invitations": { status: 200, body: { items: [] } },
      "GET /v1/session/passkeys": problem(403, "API-AUTHZ-01"),
    });
    renderPage(WithOrganisation, { path: "/" });
    expect(await screen.findByText("1 request to approve")).toBeTruthy();
    expect(await screen.findByText("1 high-risk action to review")).toBeTruthy();
    const setup = await screen.findByRole("list", { name: "Set up the company" });
    await screen.findByText("Add customers and vendors");
    const steps = within(setup).getAllByRole("listitem");
    expect(steps.map((s) => s.textContent?.includes("Done"))).toEqual([true, true, false, true, true, false, false]); // the owner session also holds Controller
    expect(within(setup).queryByText("Add a passkey")).toBeNull(); // a read refused leaves its step out
    expect(screen.getByRole("progressbar", { name: "Set-up progress" }).getAttribute("aria-valuenow")).toBe("4");
    await noViolations(document.body);
  });

  // req: FR-ADM-01
  it("asks a founder set up as Owner alone to give themselves the roles they work in", async () => {
    answer({
      "GET /v1/approvals": { status: 200, body: { items: [] } },
      "GET /v1/review-log": { status: 200, body: { items: [], unreviewed: 0, overdue: 0 } },
      "GET /v1/organisation/profile": { status: 200, body: { fy_start_month: 1, time_zone: "Asia/Singapore", version: 1 } },
      "GET /v1/accounts": { status: 200, body: { items: [] } },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m1" }] } },
      "GET /v1/members/invitations": { status: 200, body: { items: [] } },
      "GET /v1/session/passkeys": { status: 200, body: { items: [] } },
    });
    function OwnerOnly() {
      return (
        <SignedInProvider value={{ session: { ...ownerSession, roles: ["owner"] } as never, organisation, mode: "basic" }}>
          <Home />
        </SignedInProvider>
      );
    }
    renderPage(OwnerOnly, { path: "/" });
    const setup = await screen.findByRole("list", { name: "Set up the company" });
    const first = within(setup).getAllByRole("listitem")[0];
    expect(first?.textContent).toContain("Give yourself the roles you work in");
    expect(first?.textContent).toContain("To do");
  });
});
