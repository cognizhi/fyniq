import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Delegations } from "./Delegations";

const delegation = (over = {}) => ({
  id: "d1",
  delegator: "u1",
  delegator_name: "Ann",
  delegate: "u2",
  delegate_name: "Cat",
  starts_on: "2099-03-10",
  ends_on: "2099-03-14",
  reason: "Annual leave",
  status: "active",
  in_force: true,
  version: 1,
  created_at: "2099-03-01T09:00:00Z",
  ...over,
});

// req: FR-APR-06
describe("Delegations", () => {
  it("lists a delegation with whether it is in force, ends it over its version, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/approval-delegations": { status: 200, body: { items: [delegation(), delegation({ id: "d2", status: "ended", in_force: false, delegate_name: "Ben", reason: "Trip" })] } },
      "GET /v1/members": { status: 200, body: { items: [] } },
      "POST /v1/approval-delegations/d1/end": { status: 200, body: delegation({ status: "ended", in_force: false, version: 2 }) },
    });
    renderPage(Delegations, { path: "/delegations", mode: "basic" });
    expect(await screen.findByText("In force today")).toBeTruthy();
    expect(screen.getByText("Ended")).toBeTruthy();
    expect(screen.getByText(/Annual leave/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "End: Ann to Cat" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/approval-delegations/d1/end"));
    expect(seen.find((s) => s.method === "POST")?.headers.get("If-Match")).toBe('"1"');
  });

  it("delegates to another member for chosen days and says the delegation lends no right", async () => {
    const seen = answer({
      "GET /v1/approval-delegations": { status: 200, body: { items: [] } },
      "GET /v1/members": { status: 200, body: { items: [{ user_id: "u9", display_name: "Wei Ling", id: "m9", roles: [], expired_roles: [] }] } },
      "POST /v1/approval-delegations": { status: 201, body: delegation() },
    });
    renderPage(Delegations, { path: "/delegations", mode: "basic" });
    expect(await screen.findByText(/lends no right/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Delegate my approvals" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "Wei Ling" });
    fireEvent.change(within(dialog).getByLabelText("Delegate"), { target: { value: "u9" } });
    fireEvent.change(within(dialog).getByLabelText("First day"), { target: { value: "2099-03-10" } });
    fireEvent.change(within(dialog).getByLabelText("Last day"), { target: { value: "2099-03-14" } });
    fireEvent.change(within(dialog).getByLabelText("Reason"), { target: { value: "Annual leave" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Delegate my approvals" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/approval-delegations"));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ delegate: "u9", starts_on: "2099-03-10", ends_on: "2099-03-14", reason: "Annual leave" });
  });
});
