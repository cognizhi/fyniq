import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, problem, type Handler } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AgentRunReport } from "./AgentRunReport";
import { CompanySecretary } from "./CompanySecretary";

type Workspace = components["schemas"]["CompanySecretary"];
type Run = components["schemas"]["AgentRunSummary"];
type Report = components["schemas"]["AgentRunReport"];

const counts = { ok: 9, review: 0, blocked: 0, due: 1, not_assessed: 1 };
const done: Run = {
  id: "r-2",
  trigger: "schedule:monitor",
  slot_at: "2026-11-15T23:00:00Z",
  status: "done",
  started_at: "2026-11-15T23:00:05Z",
  finished_at: "2026-11-15T23:01:00Z",
  summary: "2 things need you this week.",
  counts,
  model_used: true,
  new: true,
};
const older: Run = { ...done, id: "r-1", slot_at: "2026-11-14T23:00:00Z", finished_at: "2026-11-14T23:01:00Z", new: false };
const failed: Run = { id: "r-3", trigger: "run_now", slot_at: "2026-11-16T02:00:00Z", status: "failed", reason: "it could not finish: the AI service did not answer", counts: { ...counts, due: 0, not_assessed: 0, ok: 0 }, model_used: false, new: false };
const findings: Workspace["findings"] = [
  { key: "corporate:profile", status: "not_assessed", title: "The corporate profile is not recorded", next: "Record it in Settings", rule: "corporate.profile" },
  { key: "calendar:gst_f5:2027-01-31", status: "due", title: "File the GST return", due_on: "2027-01-31", rule: "gst.return-due", pack: "sg-gst@2026.1", window: "14" },
];
const on: Workspace = {
  state: "on",
  acts_for: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  acts_for_name: "Tan Ah Kow",
  since: "2026-11-15T02:00:00Z",
  skill: "company-secretary@1",
  schedule: {
    monitor: "0 7 * * *",
    cross_check: "0 8 * * 1",
    after_close: true,
    fy_end_pack: true,
    events: ["period_closed", "filing_recorded"],
    quiet_hours: "22:00-07:00",
    recipients: ["owner", "company_secretary"],
    channels: ["inbox", "email"],
    escalate_days: 7,
    tool_calls: 20,
    run_tokens: 200000,
  },
  next_runs: [{ entry: "monitor", at: "2026-11-16T23:00:00Z" }],
  latest: done,
  last_done: done,
  findings,
  ai_summary: { text: "The GST return is due by the end of January; the corporate profile is not recorded yet.", origin: "ai" },
};

function serve(ws: Workspace = on, extra: Record<string, Handler> = {}) {
  return answer({
    "GET /v1/compliance/secretary": { status: 200, body: ws },
    "GET /v1/compliance/secretary/runs": { status: 200, body: { items: [done, older] } },
    "GET /v1/ai/proposals": {
      status: 200,
      body: {
        items: [
          {
            id: "p-1",
            task_type: "sec.draft_reminder",
            task_name: "Draft a reminder about an obligation",
            tier: 1,
            status: "pending",
            summary: 'Emails the reminder "File the GST return" to the company secretary (1 person)',
            requested_by: on.acts_for,
            agent: "company_secretary",
            items: 0,
            approval_id: "a-1",
            proposed_at: "2026-11-15T23:01:00Z",
            expires_at: "2026-11-18T23:01:00Z",
          },
        ],
      },
    },
    "GET /v1/compliance/calendar": {
      status: 200,
      body: { from: "2026-11-01", to: "2027-10-31", today: "2026-11-16", deadlines: [{ key: "gst_f5:2026-12-31", obligation: "gst_f5", title: "GST F5 for October to December", due_on: "2027-01-31", status: "open" }] },
    },
    ...extra,
  });
}

// req: FR-CS-04
// req: AI-62
// req: AI-65
// req: QA-UI-04
describe("the company secretary assistant's workspace", () => {
  it("shows who it acts for, the open findings Not assessed first, the drafts, its runs and schedule, and axe finds nothing", async () => {
    serve();
    const { container } = renderPage(CompanySecretary, { path: "/compliance/secretary" });
    expect(await screen.findByText("2 things need you this week.")).toBeTruthy();
    expect(screen.getByText(/It acts for Tan Ah Kow/)).toBeTruthy();
    const needs = within(screen.getByRole("list", { name: "What needs you" })).getAllByRole("listitem");
    expect(needs[0]?.textContent).toContain("Not assessed");
    expect(needs[1]?.textContent).toContain("Due");
    expect(needs[1]?.textContent).toContain("Rule gst.return-due");
    const draft = await screen.findByRole("link", { name: /Emails the reminder "File the GST return"/ });
    expect(draft.getAttribute("href")).toBe("/approvals/a-1");
    const runs = within(screen.getByRole("list", { name: "Its runs" })).getAllByRole("link");
    expect(runs[0]?.getAttribute("href")).toBe("/compliance/secretary/runs/r-2");
    expect(runs[0]?.textContent).toContain("New since the last run");
    expect(runs[1]?.textContent).not.toContain("New since the last run");
    expect(screen.getByText("Daily 07:00")).toBeTruthy();
    expect(screen.getByText("Monday 08:00")).toBeTruthy();
    expect(screen.getByText(/never files, signs, approves or marks an obligation as met/)).toBeTruthy();
    const ai = screen.getByRole("region", { name: "What the AI makes of it" });
    expect(within(ai).getByText("AI")).toBeTruthy();
    expect(within(ai).getByRole("link", { name: "The run's report" }).getAttribute("href")).toBe("/compliance/secretary/runs/r-2");
    await noViolations(container);
  });

  it("shows the findings' statuses in Basic mode, without their rules", async () => {
    serve();
    renderPage(CompanySecretary, { path: "/compliance/secretary", mode: "basic" });
    const needs = within(await screen.findByRole("list", { name: "What needs you" })).getAllByRole("listitem");
    expect(needs[0]?.textContent).toContain("Not assessed");
    expect(needs[1]?.textContent).not.toContain("Rule gst.return-due");
  });

  it("asks for a run now, with an idempotency key, and says a failed run's why", async () => {
    const seen = serve({ ...on, latest: failed }, { "POST /v1/compliance/secretary/runs": { status: 202, body: { ...done, id: "r-9", status: "queued", trigger: "run_now" } } });
    renderPage(CompanySecretary, { path: "/compliance/secretary" });
    expect(await screen.findByText("Its last run failed")).toBeTruthy();
    expect(screen.getByText("it could not finish: the AI service did not answer")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Run now" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/compliance/secretary/runs" && s.headers.get("Idempotency-Key"))).toBe(true));
    expect(await screen.findByText(/A run is queued/)).toBeTruthy();
  });

  it("says it is off, with no Run now, and how an Owner turns it on", async () => {
    serve({ ...on, state: "off", acts_for: undefined, acts_for_name: undefined, since: undefined, next_runs: [], latest: undefined, last_done: undefined, findings: [] });
    const { container } = renderPage(CompanySecretary, { path: "/compliance/secretary" });
    expect(await screen.findByText(/An Owner turns it on in Settings → Organisation/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Run now" })).toBeNull();
    expect(screen.getByText("No run has finished yet.")).toBeTruthy();
    await noViolations(container);
  });

  it("offers Resume while paused, and refuses what the person may not do in plain words", async () => {
    serve(
      { ...on, state: "paused", next_runs: [] },
      {
        "GET /v1/organisation/settings/agents.company_secretary.paused": { status: 200, body: { key: "agents.company_secretary.paused", value: "true", version: 2 }, headers: { ETag: '"2"' } },
        "PATCH /v1/organisation/settings/agents.company_secretary.paused": problem(403, "API-AUTHZ-01", "Your roles hold no \"C\" right.", "Ask an Owner."),
      },
    );
    renderPage(CompanySecretary, { path: "/compliance/secretary" });
    expect(await screen.findByText(/Paused, it runs nothing and keeps its schedule/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Run now" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Resume" }));
    expect(await screen.findByText("That did not happen")).toBeTruthy();
  });
});

const report: Report = {
  ...done,
  skill_version: 1,
  ledger_revision: 42,
  packs: [{ pack: "sg-gst", version: "sg-gst@2026.1" }],
  kb_snapshot: "none",
  findings,
  tasks: [{ id: "t-1", finding_key: "calendar:gst_f5:2027-01-31", title: "File the GST return", owner_role: "owner", due_on: "2027-01-31", escalated: false, source: "agent_run", status: "open", created_at: "2026-11-15T23:01:00Z", version: 1 }],
  proposals: [],
  model: { model: "claude-opus-5-5", text: { text: "The GST return is due by the end of January.", origin: "ai" }, tokens: 1200, tool_calls: 2 },
};

// req: AI-65
// req: AI-64
describe("a run's report", () => {
  it("shows what it read, every finding with its rule, the tasks and the AI's words labelled AI, and axe finds nothing", async () => {
    answer({ "GET /v1/compliance/secretary/runs/r-2": { status: 200, body: report } });
    const { container } = renderPage(AgentRunReport, { path: "/compliance/secretary/runs/r-2", route: "/compliance/secretary/runs/$runId" });
    expect(await screen.findByRole("heading", { name: /^The daily monitor, / })).toBeTruthy();
    expect(screen.getByText("sg-gst@2026.1", { selector: "dd" })).toBeTruthy();
    expect(screen.getAllByText("Not assessed").length).toBeGreaterThan(0);
    expect(screen.getByText("Rule gst.return-due · sg-gst@2026.1")).toBeTruthy();
    expect(screen.getByText(/^Open · due .* · owner$/)).toBeTruthy();
    const ai = screen.getByRole("region", { name: "Summary for the directors" });
    expect(within(ai).getByText("AI")).toBeTruthy();
    expect(within(ai).getByText("The GST return is due by the end of January.")).toBeTruthy();
    await noViolations(container);
  });

  it("says the model was not asked when nothing had changed", async () => {
    answer({ "GET /v1/compliance/secretary/runs/r-1": { status: 200, body: { ...report, id: "r-1", model: undefined, model_used: false } } });
    renderPage(AgentRunReport, { path: "/compliance/secretary/runs/r-1", route: "/compliance/secretary/runs/$runId" });
    expect(await screen.findByText(/The model was not asked/)).toBeTruthy();
  });
});
