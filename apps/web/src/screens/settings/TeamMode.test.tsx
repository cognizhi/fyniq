import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ApprovalPolicies } from "./ApprovalPolicies";
import { TeamMode } from "./TeamMode";

const conflict = { conflict: "SOD-6", display_name: "Ann Tan", member_id: "m-1", user_id: ownerSession.user_id, roles: ["controller", "administrator"], overridden: false };
const team = { mode: "founder", version: 1, blocks_conflicts: false, conflicts: [conflict], overrides: [], history: [] };
const asked = { id: "a-20", summary: "Switch the team mode to Standard", status: "pending", object_type: "team_mode" };

function teamPage(extra = {}, t: Record<string, unknown> = team) {
  return answer({
    "GET /v1/team-mode": { status: 200, body: t, headers: { ETag: '"1"' } },
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    ...extra,
  });
}

// req: FR-ADM-04
// req: access/team-modes Mode switching
describe("Team mode", () => {
  it("shows the mode and the conflicts, and axe finds nothing", async () => {
    teamPage();
    renderPage(TeamMode, { path: "/settings/team-mode" });
    const group = await screen.findByRole("radiogroup", { name: "Mode" });
    expect(within(group).getByRole("radio", { name: /Founder/ }).getAttribute("aria-checked")).toBe("true");
    expect(within(group).getByRole("radio", { name: /Enterprise/ }).getAttribute("aria-disabled")).toBe("true");
    expect(within(screen.getByRole("list", { name: "Role conflicts" })).getByText("Allowed in Founder mode")).toBeTruthy();
    await noViolations(document.body);
  });

  it("asks to switch to Standard with a reason for each conflict, over the version shown", async () => {
    const seen = teamPage({ "PATCH /v1/team-mode": { status: 202, body: asked } });
    renderPage(TeamMode, { path: "/settings/team-mode" });
    const standard = await screen.findByRole("radio", { name: /Standard/ });
    fireEvent.click(standard);
    fireEvent.click(await screen.findByRole("button", { name: "Ask to switch to Standard" }));
    expect(await screen.findByText(/Give the reason Ann Tan keeps both roles/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Why Ann Tan keeps/), { target: { value: "Founder who keeps the books" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask to switch to Standard" }));
    expect(await screen.findByText(/Switch the team mode to Standard/)).toBeTruthy();
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"1"');
    expect(patch?.body).toEqual({ mode: "standard", overrides: [{ member_id: "m-1", conflict: "SOD-6", reason: "Founder who keeps the books" }] });
  });

  it("needs an invitation whose roles conflict withdrawn first", async () => {
    teamPage({}, { ...team, conflicts: [{ conflict: "SOD-6", display_name: "Dan Koh", invitation_id: "i-3", roles: ["administrator", "bookkeeper"], overridden: false }] });
    renderPage(TeamMode, { path: "/settings/team-mode" });
    fireEvent.click(await screen.findByRole("radio", { name: /Standard/ }));
    expect(await screen.findByText(/Withdraw an invitation first/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Ask to switch to Standard" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("moves between the modes with the arrow keys, skipping one not available", async () => {
    teamPage();
    renderPage(TeamMode, { path: "/settings/team-mode" });
    const founder = await screen.findByRole("radio", { name: /Founder/ });
    fireEvent.keyDown(founder, { key: "ArrowRight" });
    await waitFor(() => expect(screen.getByRole("radio", { name: /Standard/ }).getAttribute("aria-checked")).toBe("true"));
    fireEvent.keyDown(screen.getByRole("radio", { name: /Standard/ }), { key: "ArrowRight" });
    await waitFor(() => expect(founder.getAttribute("aria-checked")).toBe("true"));
  });
});

const policy = { object_type: "organisation_setting", row: "Organisation profile, financial year, defaults (rev 0.29)", number: 0, version: "builtin", built_in: true, fixed: false, steps: [{ quorum: 1, roles: [], users: [] }] };
const fixed = { ...policy, object_type: "approval_policy", row: "Approval policies, AI catalogue", fixed: true };

// req: FR-APR-01
// req: FR-APR-09
describe("Approval policies", () => {
  it("lists who approves each kind of change, and asks for new steps", async () => {
    const seen = answer({
      "GET /v1/approval-policies": { status: 200, body: { items: [policy, fixed] } },
      "GET /v1/approvals": { status: 200, body: { items: [] } },
      "GET /v1/approval-policies/organisation_setting": { status: 200, body: { ...policy, history: [] }, headers: { ETag: '"0"' } },
      "GET /v1/members": { status: 200, body: { items: [] } },
      "PATCH /v1/approval-policies/organisation_setting": { status: 202, body: { id: "a-30", summary: "Change who approves organisation settings", status: "pending" } },
    });
    renderPage(ApprovalPolicies, { path: "/settings/approval-policies" });
    const list = await screen.findByRole("list", { name: "Kinds of change" });
    expect(within(list).getByText("Fixed")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(list).getByRole("button", { name: "Change policy: Organisation setting" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByRole("button", { name: "Add a step" }));
    fireEvent.change(within(dialog).getAllByLabelText("Who may approve")[1] as HTMLElement, { target: { value: "roles" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for this policy" }));
    expect(await within(dialog).findByText(/Step 2: choose at least one role/)).toBeTruthy();
    fireEvent.click(within(within(dialog).getByRole("group", { name: "Step 2: roles" })).getByRole("checkbox", { name: "Owner" }));
    fireEvent.change(within(dialog).getAllByLabelText("Approvals needed")[1] as HTMLElement, { target: { value: "2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for this policy" }));
    expect(await screen.findByText(/Change who approves organisation settings/)).toBeTruthy();
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"0"');
    expect(patch?.body).toEqual({ steps: [{ quorum: 1 }, { quorum: 2, roles: ["owner"] }] });
  });
});
