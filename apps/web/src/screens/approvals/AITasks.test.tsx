import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, type Handler } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { noAuthenticator } from "../../test/webauthn";
import { AITasks } from "./AITasks";

type TaskType = components["schemas"]["AITaskTypeView"];
type Row = components["schemas"]["AIProposalSummary"];

const createTask: TaskType = {
  id: "sec.create_task",
  version: 1,
  name: "Create a compliance task",
  description: "Create a compliance task with an owner and a due date from a finding.",
  tier: 1,
  max_items: 5,
  approvers: ["owner", "company_secretary"],
  expiry_hours: 72,
  built: true,
  on: false,
};
const draftDocument: TaskType = { ...createTask, id: "sec.draft_document", name: "Draft a document", description: "Draft a resolution.", built: false };
const executed: Row = {
  id: "p-1",
  task_type: "sec.create_task",
  task_name: "Create a compliance task",
  tier: 1,
  status: "executed",
  summary: "Creates the task Hold the AGM, due 30 June 2027",
  requested_by: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d99",
  agent: "chat",
  items: 0,
  proposed_at: "2026-11-15T01:00:00Z",
  expires_at: "2026-11-18T01:00:00Z",
};

function serve(types: TaskType[] = [createTask, draftDocument], value = "", extra: Record<string, Handler> = {}, ai = "true") {
  return answer({
    "GET /v1/ai/task-types": { status: 200, body: { task_types: types } },
    "GET /v1/ai/proposals": { status: 200, body: { items: [executed] } },
    "GET /v1/organisation/settings/ai.task_types": { status: 200, body: { key: "ai.task_types", value, version: 4 }, headers: { ETag: '"4"' } },
    "GET /v1/organisation/settings/features.ai_assistant": { status: 200, body: { key: "features.ai_assistant", value: ai, version: 1 } },
    ...extra,
  });
}

// req: ai/task-catalogue AI tasks page
// req: AI-32
// req: AI-06
describe("the AI tasks page", () => {
  it("lists every proposal you may see, filtered by state, each linked to its review, and axe finds nothing", async () => {
    const seen = serve();
    const { container } = renderPage(AITasks, { path: "/inbox/ai-tasks" });
    const link = await screen.findByRole("link", { name: "Creates the task Hold the AGM, due 30 June 2027" });
    expect(link.getAttribute("href")).toBe("/inbox/proposals/p-1");
    expect(screen.getByText("Executed", { selector: ".fy-chip" })).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Waiting" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/ai/proposals" && s.query.includes("status=pending"))).toBe(true));
    expect(screen.getByRole("button", { name: "Waiting" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("shows the catalogue by tier with its limits and approvers, every type off, and the never-list, and axe finds nothing", async () => {
    serve();
    const { container } = renderPage(AITasks, { path: "/inbox/ai-tasks" });
    fireEvent.click(await screen.findByRole("tab", { name: "Catalogue" }));
    const tier = await screen.findByRole("heading", { name: "Tier 1 · routine" });
    const card = tier.closest("section") as HTMLElement;
    const row = within(card).getByText("Create a compliance task").closest("li") as HTMLElement;
    expect(within(row).getByText(/At most 5 items · approved by Owner or Company secretary · expires after 72 hours/)).toBeTruthy();
    expect(within(row).getByRole("button", { name: "Turn on…" })).toBeTruthy();
    const unbuilt = within(card).getByText("Draft a document").closest("li") as HTMLElement;
    expect(within(unbuilt).getByText("Not in this release")).toBeTruthy();
    expect(within(unbuilt).queryByRole("button")).toBeNull();
    expect(screen.getByText(/Every task type is off, as each starts/)).toBeTruthy();
    const never = screen.getByRole("heading", { name: "Never proposed" }).closest("section") as HTMLElement;
    expect(within(never).getByText("Releasing or making a payment")).toBeTruthy();
    expect(within(never).getByText("Changing a payee's bank details")).toBeTruthy();
    await noViolations(container);
  });

  it("turns a task type on through a second approver, over the version shown", async () => {
    noAuthenticator();
    const seen = serve([createTask], "sec.draft_reminder", {
      "PATCH /v1/organisation/settings/ai.task_types": { status: 202, body: { id: "a-7", status: "pending" } },
    });
    renderPage(AITasks, { path: "/inbox/ai-tasks" });
    fireEvent.click(await screen.findByRole("tab", { name: "Catalogue" }));
    fireEvent.click(await screen.findByRole("button", { name: "Turn on…" }));
    expect(await screen.findByText(/Turning on Create a compliance task waits for a second approver/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "See the request" }).getAttribute("href")).toBe("/approvals/a-7");
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.body).toEqual({ value: "sec.create_task,sec.draft_reminder" });
    expect(patch?.headers.get("If-Match")).toBe('"4"');
  });

  it("turns a task type off at once", async () => {
    noAuthenticator();
    const seen = serve([{ ...createTask, on: true }], "sec.create_task,sec.draft_reminder", {
      "PATCH /v1/organisation/settings/ai.task_types": { status: 200, body: { key: "ai.task_types", value: "sec.draft_reminder", version: 5 } },
    });
    renderPage(AITasks, { path: "/inbox/ai-tasks" });
    fireEvent.click(await screen.findByRole("tab", { name: "Catalogue" }));
    fireEvent.click(await screen.findByRole("button", { name: "Turn off" }));
    expect(await screen.findByText("Create a compliance task is off.")).toBeTruthy();
    expect(seen.find((s) => s.method === "PATCH")?.body).toEqual({ value: "sec.draft_reminder" });
  });

  it("says when the AI is switched off", async () => {
    serve([createTask], "", {}, "false");
    renderPage(AITasks, { path: "/inbox/ai-tasks" });
    fireEvent.click(await screen.findByRole("tab", { name: "Catalogue" }));
    expect(await screen.findByText("The AI is switched off")).toBeTruthy();
  });
});
