import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Members } from "./Members";

const ann = { id: "m-1", user_id: ownerSession.user_id, display_name: "Ann Tan", roles: [{ role: "owner" }] };
const ben = { id: "m-2", user_id: "u-2", display_name: "Ben Ong", roles: [{ role: "owner" }] };
const dan = { id: "m-4", user_id: "u-4", display_name: "Dan Koh", roles: [{ role: "accountant" }] };
const owner = { ...ownerSession, roles: ["owner"] } as Session;
const succession = {
  id: "s-1", outgoing: { member_id: "m-2", user_id: "u-2", name: "Ben Ong" }, successor: { member_id: "m-4", user_id: "u-4", name: "Dan Koh" },
  reason: "Ben left on 31 October; the board resolved on his removal", evidence_kind: "board_resolution", files: 0,
  started_by: { kind: "user", id: ownerSession.user_id, name: "Ann Tan" }, started_at: "2026-11-06T01:00:00Z", due_at: "2026-11-13T01:00:00Z",
  status: "notice", version: 1, you_may_object: false, you_may_withdraw: true, you_may_attach: true, you_may_complete: false,
};

function page(extra = {}) {
  return answer({
    "GET /v1/members": { status: 200, body: { items: [ann, ben, dan] } },
    "GET /v1/members/invitations": { status: 200, body: { items: [] } },
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    "GET /v1/team-mode": { status: 200, body: { mode: "founder", version: 1, blocks_conflicts: false, history: [], overrides: [], conflicts: [] } },
    "GET /v1/owner-successions/s-1/evidence": { status: 200, body: { items: [] } },
    ...extra,
  });
}

// req: FR-ADM-09
// req: access/users-and-sign-in Owner succession (rev 0.181)
describe("Owner succession", () => {
  it("starts a succession of another Owner with a reason, what it rests on and a successor; axe finds nothing", async () => {
    const seen = page({
      "GET /v1/owner-successions": { status: 200, body: { items: [] } },
      "POST /v1/members/m-2/succession": { status: 201, body: succession },
    });
    renderPage(Members, { path: "/settings/members", session: owner });
    const people = await screen.findByRole("list", { name: "People" });
    expect(within(people).queryByRole("button", { name: "Start a succession: Ann Tan" })).toBeNull(); // never yourself
    expect(within(people).queryByRole("button", { name: "Start a succession: Dan Koh" })).toBeNull(); // only an Owner
    fireEvent.click(within(people).getByRole("button", { name: "Start a succession: Ben Ong" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Start a succession" }));
    expect(await within(dialog).findByText(/Say why the Owner is permanently unavailable/)).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why they are permanently unavailable"), { target: { value: succession.reason } });
    fireEvent.change(within(dialog).getByLabelText("Who becomes an Owner (optional)"), { target: { value: "m-4" } });
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Start a succession" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ reason: succession.reason, evidence_kind: "board_resolution", successor_member_id: "m-4" });
  });

  it("lists a succession in notice, with its evidence, and lets its starter withdraw it over its version", async () => {
    const seen = page({
      "GET /v1/owner-successions": { status: 200, body: { items: [succession] } },
      "POST /v1/owner-successions/s-1/withdraw": { status: 200, body: { ...succession, status: "withdrawn", version: 2 } },
    });
    renderPage(Members, { path: "/settings/members", session: owner });
    const list = await screen.findByRole("list", { name: "Owner successions" });
    expect(within(list).getByText(/Succession of Ben Ong, Dan Koh to become an Owner/)).toBeTruthy();
    expect(within(list).getByText("In notice")).toBeTruthy();
    expect(within(list).getByText(/notice ends 13 Nov 2026/)).toBeTruthy();
    expect(within(list).queryByRole("button", { name: /Complete it/ })).toBeNull();
    await noViolations(document.body);
    fireEvent.click(within(list).getByRole("button", { name: "Withdraw it" }));
    expect(await screen.findByText("You withdrew the succession.")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.path).toBe("/v1/owner-successions/s-1/withdraw");
    expect(post?.headers.get("If-Match")).toBe('"1"');
  });

  it("lets the outgoing Owner object, and a Super admin complete it once its notice is over", async () => {
    const seen = page({
      "GET /v1/owner-successions": { status: 200, body: { items: [{ ...succession, you_may_withdraw: false, you_may_object: true, you_may_attach: false },
        { ...succession, id: "s-2", files: 1, you_may_withdraw: false, you_may_attach: false, you_may_complete: true }] } },
      "POST /v1/owner-successions/s-1/object": { status: 200, body: { ...succession, status: "objected", version: 2 } },
      "POST /v1/owner-successions/s-2/complete": { status: 200, body: { ...succession, id: "s-2", status: "completed", version: 2 } },
    });
    renderPage(Members, { path: "/settings/members", session: owner });
    const list = await screen.findByRole("list", { name: "Owner successions" });
    fireEvent.click(within(list).getByRole("button", { name: "Object: I am not unavailable" }));
    expect(await screen.findByText(/You objected/)).toBeTruthy();
    fireEvent.click(within(list).getByRole("button", { name: "Complete it, as a Super admin" }));
    expect(await screen.findByText(/The succession completed/)).toBeTruthy();
    expect(seen.filter((s) => s.method === "POST").map((s) => s.path)).toEqual(["/v1/owner-successions/s-1/object", "/v1/owner-successions/s-2/complete"]);
  });
});
