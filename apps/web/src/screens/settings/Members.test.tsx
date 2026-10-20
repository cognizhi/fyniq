import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Members } from "./Members";

const ann = { id: "m-1", user_id: ownerSession.user_id, display_name: "Ann Tan", roles: [{ role: "owner" }, { role: "controller" }, { role: "administrator" }] };
const ben = { id: "m-2", user_id: "u-2", display_name: "Ben Ong", roles: [{ role: "owner" }] };
const grace = { id: "m-3", user_id: "u-3", display_name: "Grace Ho", roles: [{ role: "accountant", expires_at: "2026-12-31T15:59:59Z" }] };
const invitation = { id: "i-1", display_name: "Cat Lim", roles: ["bookkeeper"], created_at: "2026-10-19T01:00:00Z", expires_at: "2026-10-26T01:00:00Z" };
const waiting = { id: "a-9", object_type: "membership", object_key: "m-3", operation: "role.assign", summary: "Give Grace Ho the Bookkeeper role", status: "pending" };
const team = { mode: "founder", version: 1, blocks_conflicts: false, history: [], overrides: [], conflicts: [{ conflict: "SOD-6", display_name: "Ann Tan", member_id: "m-1", user_id: ownerSession.user_id, roles: ["controller", "administrator"], overridden: false }] };
const request = { id: "a-10", summary: "Invite Dan Koh", status: "pending", invitation_code: "ABCDE-FGHIJ-KLMNO" };

function page(extra = {}, people = [ann, ben, grace]) {
  return answer({
    "GET /v1/members": { status: 200, body: { items: people } },
    "GET /v1/members/invitations": { status: 200, body: { items: [invitation] } },
    "GET /v1/approvals": { status: 200, body: { items: [waiting, { ...waiting, id: "a-8", object_type: "organisation_setting", summary: "Another kind" }] } },
    "GET /v1/team-mode": { status: 200, body: team },
    ...extra,
  });
}

// req: FR-ADM-01
// req: FM-1
describe("Members and roles", () => {
  // req: SOD-6
  it("lists people, their roles and conflicts, unused invitations and changes waiting, and axe finds nothing", async () => {
    page();
    renderPage(Members, { path: "/settings/members" });
    const people = await screen.findByRole("list", { name: "People" });
    expect(within(people).getAllByRole("listitem")).toHaveLength(3);
    expect(within(people).getByText("Ann Tan (you)")).toBeTruthy();
    expect(within(people).getByText("Accountant to 31 Dec 2026")).toBeTruthy();
    expect(within(people).getAllByText("Role conflict")).toHaveLength(1);
    expect(await screen.findByText(/Give Grace Ho the Bookkeeper role/)).toBeTruthy();
    expect(screen.queryByText(/Another kind/)).toBeNull();
    expect(within(await screen.findByRole("list", { name: "Invitations not yet used" })).getByText("Cat Lim")).toBeTruthy();
    expect(screen.getByText(/Role changes wait for another Owner's approval/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("shows the founder bootstrap's invitation code once", async () => {
    const seen = page({ "POST /v1/members/invitations": { status: 201, body: { ...invitation, id: "i-2", display_name: "Dan Koh", code: "ZYXWV-UTSRQ" } } }, [ann]);
    renderPage(Members, { path: "/settings/members" });
    expect(await screen.findByText(/Until a second person joins/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Invite" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/Name/), { target: { value: "Dan Koh" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Owner/ }));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Controller/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    expect(await within(dialog).findByText("ZYXWV-UTSRQ")).toBeTruthy();
    expect(within(dialog).getByText("Invitation made")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ display_name: "Dan Koh", roles: ["owner", "controller"] });
  });

  it("says an invitation waits for another Owner, and still shows its code", async () => {
    page({ "POST /v1/members/invitations": { status: 202, body: request } });
    renderPage(Members, { path: "/settings/members" });
    fireEvent.click(await screen.findByRole("button", { name: "Invite" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/Name/), { target: { value: "Dan Koh" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Bookkeeper/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    expect(await within(dialog).findByText("ABCDE-FGHIJ-KLMNO")).toBeTruthy();
    expect(within(dialog).getByText(/waits for another Owner's approval/)).toBeTruthy();
  });

  it("checks the invitation before sending it", async () => {
    page();
    renderPage(Members, { path: "/settings/members" });
    fireEvent.click(await screen.findByRole("button", { name: "Invite" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Owner/ }));
    fireEvent.change(within(dialog).getByLabelText(/Roles end on/), { target: { value: "2026-12-31" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    expect(await within(dialog).findByText(/Enter the person's name/)).toBeTruthy();
    expect(within(dialog).getByText(/The Owner role never ends/)).toBeTruthy();
    expect(calls()).not.toContain("POST /v1/members/invitations");
  });

  it("asks for a role, which waits for approval once there is a second member", async () => {
    const seen = page({ "POST /v1/members/m-2/roles": { status: 202, body: { ...waiting, id: "a-11", summary: "Give Ben Ong the Controller role" } } });
    renderPage(Members, { path: "/settings/members" });
    fireEvent.click(await screen.findByRole("button", { name: "Change roles: Ben Ong" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Role to add"), { target: { value: "controller" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add role" }));
    expect(await within(dialog).findByText(/Give Ben Ong the Controller role/)).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ role: "controller" });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("ends a role after the person confirms it's you", async () => {
    let n = 0;
    page({ "DELETE /v1/members/m-3/roles/accountant": () => (++n === 1 ? problem(403, "API-AUTHZ-05") : { status: 200, body: { ...grace, roles: [] } }) });
    renderPage(Members, { path: "/settings/members" });
    fireEvent.click(await screen.findByRole("button", { name: "Change roles: Grace Ho" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "End role: Accountant" }));
    expect(await screen.findByRole("heading", { name: "This needs a second factor" })).toBeTruthy();
  });

  it("withdraws an unused invitation", async () => {
    page({ "DELETE /v1/members/invitations/i-1": { status: 200, body: { ...invitation, withdrawn_at: "2026-10-20T02:00:00Z" } } });
    renderPage(Members, { path: "/settings/members" });
    fireEvent.click(await screen.findByRole("button", { name: "Withdraw: Cat Lim" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(calls()).toContain("DELETE /v1/members/invitations/i-1"));
  });
});
