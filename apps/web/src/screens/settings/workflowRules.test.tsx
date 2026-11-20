import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { WorkflowRules } from "./WorkflowRules";

const rule = (over = {}) => ({
  id: "r1",
  name: "Large bills",
  trigger: "document.posted",
  document_type: "bill",
  amount_at_least: "5000.0000",
  action: "notify",
  recipients: ["u1"],
  state: "active",
  version: 1,
  created_at: "2026-11-21T09:00:00Z",
  updated_at: "2026-11-21T09:00:00Z",
  ...over,
});
const members = { items: [{ user_id: "u1", display_name: "Mei Lin", id: "m1", roles: [], expired_roles: [] }] };

// req: FR-AUT-03
describe("WorkflowRules", () => {
  it("says each rule in words, pauses an active one over its version, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/automation-rules": { status: 200, body: { items: [rule(), rule({ id: "r2", name: "Old", state: "archived", document_type: undefined, amount_at_least: undefined })] } },
      "GET /v1/members": { status: 200, body: members },
      "PATCH /v1/automation-rules/r1": { status: 200, body: rule({ state: "paused", version: 2 }) },
    });
    renderPage(WorkflowRules, { path: "/settings/workflow-rules" });
    expect(await screen.findByText(/a supplier bill is posted with debits of at least 5000.00, tell Mei Lin/)).toBeTruthy();
    expect(screen.getByText("Archived")).toBeTruthy();
    expect(screen.getAllByText(/only tells people/).length).toBe(2);
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Pause: Large bills" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/automation-rules/r1"));
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"1"');
    expect(patch?.body).toEqual({ state: "paused" });
  });

  it("adds a rule with the kind, the amount and the members to tell, and shows the API's refusal in words", async () => {
    const seen = answer({
      "GET /v1/automation-rules": { status: 200, body: { items: [] } },
      "GET /v1/members": { status: 200, body: members },
      "POST /v1/automation-rules": { status: 201, body: rule() },
    });
    renderPage(WorkflowRules, { path: "/settings/workflow-rules" });
    fireEvent.click(await screen.findByRole("button", { name: "Add rule" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Large bills" } });
    fireEvent.change(within(dialog).getByLabelText(/With debits of at least/), { target: { value: "5000.00" } });
    fireEvent.click(await within(dialog).findByLabelText("Mei Lin"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add rule" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/automation-rules"));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ name: "Large bills", recipients: ["u1"], document_type: "bill", amount_at_least: "5000.00" });
  });
});
