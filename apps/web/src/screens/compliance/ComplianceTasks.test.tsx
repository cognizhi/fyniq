import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, type Handler } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { TasksToDo } from "./ComplianceTasks";

type Task = components["schemas"]["ComplianceTask"];

const gst: Task = {
  id: "t-1",
  finding_key: "calendar:gst_f5:2027-01-31",
  title: "File the GST return",
  detail: "Due by 31 January 2027 (sg-gst@2026.1).",
  owner_role: "owner",
  due_on: "2027-01-31",
  escalated: false,
  source: "agent_run",
  status: "open",
  created_at: "2026-11-15T23:01:00Z",
  version: 1,
};
const agm: Task = { ...gst, id: "t-2", finding_key: "agent:agm-room-2027", title: "Book the room for the AGM", due_on: "2026-11-01", source: "ai_proposal", version: 2 };
const theirs: Task = { ...gst, id: "t-3", finding_key: "agent:payroll", title: "Not yours", owner_role: "payroll_administrator" };

function serve(extra: Record<string, Handler> = {}) {
  return answer({ "GET /v1/compliance/tasks": { status: 200, body: { items: [gst, agm, theirs] } }, ...extra });
}

// req: AI-65
// req: ai/scheduled-agents AI company secretary assistant
describe("the Inbox's tasks", () => {
  it("lists the open tasks of your role, the earliest due first, overdue marked, and axe finds nothing", async () => {
    serve();
    const { container } = renderPage(TasksToDo, { path: "/inbox" });
    const items = within(await screen.findByRole("list", { name: "Tasks" })).getAllByRole("listitem");
    expect(items.map((i) => i.querySelector(".fy-list__title")?.textContent)).toEqual(["Book the room for the AGM", "File the GST return"]);
    expect(items[0]?.textContent).toContain("Overdue");
    expect(items[0]?.textContent).toContain("suggested by the AI, approved by a person");
    expect(screen.queryByText("Not yours")).toBeNull();
    await noViolations(container);
  });

  it("marks a task done over the version it shows, and dismisses one with a reason", async () => {
    const seen = serve({
      "POST /v1/compliance/tasks/t-1/done": { status: 200, body: { ...gst, status: "done", version: 2 } },
      "POST /v1/compliance/tasks/t-2/dismiss": { status: 200, body: { ...agm, status: "dismissed", reason: "Booked already", version: 3 } },
    });
    renderPage(TasksToDo, { path: "/inbox" });
    fireEvent.click(await screen.findByRole("button", { name: "Mark done: File the GST return" }));
    await waitFor(() => {
      const done = seen.find((s) => s.path === "/v1/compliance/tasks/t-1/done");
      expect(done?.headers.get("If-Match")).toBe('"1"');
      expect(done?.headers.get("Idempotency-Key")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Dismiss: Book the room for the AGM" }));
    const dialog = await screen.findByRole("dialog");
    expect((within(dialog).getByRole("button", { name: "Dismiss" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(within(dialog).getByLabelText("Why it needs nothing done"), { target: { value: "Booked already" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Dismiss" }));
    await waitFor(() => {
      const d = seen.find((s) => s.path === "/v1/compliance/tasks/t-2/dismiss");
      expect(d?.body).toEqual({ reason: "Booked already" });
      expect(d?.headers.get("If-Match")).toBe('"2"');
    });
  });

  it("offers no Done or Dismiss to a role that may not close a task", async () => {
    answer({ "GET /v1/compliance/tasks": { status: 200, body: { items: [{ ...gst, owner_role: "controller" }] } } });
    renderPage(TasksToDo, { path: "/inbox", session: { ...ownerSession, roles: ["controller"] } });
    expect(await screen.findByText("File the GST return")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Mark done/ })).toBeNull();
  });

  it("shows nothing to those who may not see the tasks", async () => {
    answer({ "GET /v1/compliance/tasks": problem(403, "API-AUTHZ-01") });
    const { container } = renderPage(TasksToDo, { path: "/inbox" });
    await waitFor(() => expect(container.querySelector("section")).toBeNull());
  });
});
