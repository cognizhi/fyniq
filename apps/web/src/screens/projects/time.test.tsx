import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { MyTime } from "./MyTime";
import { mondayOf, rowsOf, shift, hoursText } from "./TimeGrid";
import { Timesheet } from "./Timesheet";

type Sheet = components["schemas"]["Timesheet"];
const employee = { ...ownerSession, roles: ["employee"] } as Session;
const approver = { ...ownerSession, roles: ["approver"] } as Session;

const days = ["2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19", "2026-11-20", "2026-11-21", "2026-11-22"].map((date, i) => ({
  date,
  weekday: (["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"] as const)[i]!,
  missing: i === 2,
  minutes: i === 1 ? 135 : 0,
}));
const sheet = (extra: Partial<Sheet> = {}): Sheet => ({
  week_start: "2026-11-16",
  week_end: "2026-11-22",
  days,
  entries: [{ id: "e1", project_id: "p1", task_id: "t1", work_date: "2026-11-17", minutes: 135, billable: true, note: "Migrated tables", status: "draft", version: 1 }],
  submissions: [],
  projects: [{ id: "p1", code: "ACME-01", name: "Acme rollout", billing_method: "time_and_materials", tasks: [{ id: "t1", name: "Data migration", billable: true }, { id: "t2", name: "Support", billable: false }] }],
  total_minutes: 135,
  ...extra,
});
const week = (s: Sheet, more: Record<string, Parameters<typeof answer>[0][string]> = {}) =>
  answer({ "GET /v1/timesheet/2026-11-16": { status: 200, body: s, headers: { ETag: '"1-1-1"' } }, "GET /v1/timesheet-submissions": { status: 200, body: { items: [] } }, ...more });

// req: FR-PRJ-05
describe("the week's arithmetic", () => {
  it("finds a week's Monday, moves by weeks and writes minutes as typed", () => {
    expect(mondayOf("2026-11-19")).toBe("2026-11-16");
    expect(mondayOf("2026-11-22")).toBe("2026-11-16");
    expect(shift("2026-11-16", -1)).toBe("2026-11-09");
    expect(hoursText(135)).toBe("2:15");
    expect(hoursText(480)).toBe("8:00");
  });
  it("groups entries by project and task, adding the rows a person starts", () => {
    const rows = rowsOf(sheet(), [{ project_id: "p1", task_id: "t2" }]);
    expect(rows.map((r) => r.task)).toEqual(["Data migration", "Support"]);
    expect(rows[1]?.billable).toBe(false);
  });
});

// req: FR-PRJ-05
// req: FR-UX-39
describe("my time", () => {
  it("shows the week's grid, the missing day and the total, and axe finds nothing", async () => {
    week(sheet());
    renderPage(MyTime, { path: "/me/time", session: employee });
    const table = await screen.findByRole("table", { name: "Your week" });
    expect((within(table).getByLabelText("Data migration on Tue 17") as HTMLInputElement).value).toBe("2:15");
    expect(screen.getByText(/No time yet on Wed 18/)).toBeTruthy();
    expect(within(table).getAllByText("2 h 15 min").length).toBeGreaterThan(0);
    await noViolations(document.body);
  });

  it("says what a day's time needs, and saves the week over its ETag", async () => {
    const seen = week(sheet(), { "PUT /v1/timesheet/2026-11-16": { status: 200, body: sheet(), headers: { ETag: '"2-2-2"' } } });
    renderPage(MyTime, { path: "/me/time", session: employee });
    const table = await screen.findByRole("table", { name: "Your week" });
    const cell = within(table).getByLabelText("Data migration on Wed 18");
    fireEvent.change(cell, { target: { value: "lots" } });
    fireEvent.click(screen.getByRole("button", { name: "Save the week" }));
    await invalid(table, "Data migration on Wed 18", "Write the time as hours, such as 2.25 or 2:15, up to 24 hours.");
    expect(seen.find((s) => s.method === "PUT")).toBeUndefined();
    fireEvent.change(cell, { target: { value: "7:30" } });
    fireEvent.click(screen.getByRole("button", { name: "Save the week" }));
    await waitFor(() => expect(seen.find((s) => s.method === "PUT")).toBeTruthy());
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"1-1-1"');
    expect(put?.body).toEqual({
      entries: [
        { id: "e1", project_id: "p1", task_id: "t1", work_date: "2026-11-17", minutes: 135, billable: true, note: "Migrated tables" },
        { id: undefined, project_id: "p1", task_id: "t1", work_date: "2026-11-18", minutes: 450, billable: true, note: "Migrated tables" },
      ].map((e) => JSON.parse(JSON.stringify(e))),
    });
  });

  it("starts and stops the timer, and sends the week for approval", async () => {
    const seen = week(sheet({ entries: [] , total_minutes: 0}), {
      "POST /v1/timer/start": { status: 200, body: { running: true } },
      "POST /v1/timesheet/2026-11-16/submit": { status: 200, body: sheet() },
    });
    renderPage(MyTime, { path: "/me/time", session: employee });
    fireEvent.change((await screen.findAllByLabelText("Project"))[0]!, { target: { value: "p1" } });
    fireEvent.change(screen.getAllByLabelText("Task")[0]!, { target: { value: "t1" } });
    fireEvent.click(screen.getByRole("button", { name: "Start the timer" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/timer/start")).toBeTruthy());
    expect(seen.find((s) => s.path === "/v1/timer/start")?.body).toEqual({ project_id: "p1", task_id: "t1" });
  });

  it("shows a running timer, a locked cell and what was sent", async () => {
    week(
      sheet({
        timer: { project_id: "p1", task_id: "t1", billable: true, started_at: "2026-11-19T09:00:00+08:00", elapsed_minutes: 135 },
        entries: [{ id: "e2", project_id: "p1", task_id: "t1", work_date: "2026-11-17", minutes: 135, billable: true, status: "approved", version: 2, submission_id: "s1" }],
        submissions: [{ id: "s1", project_id: "p1", project_name: "Acme rollout", user_id: "u1", person_name: "Priya", week_start: "2026-11-16", status: "approved", submitted_at: "2026-11-20T17:00:00+08:00", decided_by_name: "Wei Lim", minutes: 135, can_decide: false, version: 2 }],
      }),
    );
    renderPage(MyTime, { path: "/me/time", session: employee });
    expect(await screen.findByText(/Running for 2 h 15 min on Acme rollout/)).toBeTruthy();
    expect(screen.getByText("2 h 15 min locked")).toBeTruthy();
    expect(screen.getAllByText("Approved").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Correct the 2 h 15 min on 2026-11-17" })).toBeTruthy();
  });

  it("says when Projects and time are switched off, and when a person is on no project", async () => {
    answer({ "GET /v1/timesheet/2026-11-16": problem(403, "PRJ-09", "Projects and time are switched off for this organisation.") });
    renderPage(MyTime, { path: "/me/time", session: employee });
    expect(await screen.findByText("Projects and time are switched off")).toBeTruthy();
    week(sheet({ entries: [], projects: [], total_minutes: 0 }));
    renderPage(MyTime, { path: "/me/time", session: employee });
    expect(await screen.findByText("You are on no project yet")).toBeTruthy();
  });
});

// req: FR-PRJ-07
// req: FR-UX-40
describe("the team's time", () => {
  const waiting = {
    id: "s9", project_id: "p1", project_name: "Acme rollout", user_id: "u9", person_name: "Priya Nair", week_start: "2026-11-16", status: "submitted" as const,
    submitted_at: "2026-11-20T17:00:00+08:00", minutes: 630, can_decide: true, version: 1,
  };
  const reads = (more: Record<string, Parameters<typeof answer>[0][string]> = {}) =>
    week(sheet({ entries: [], total_minutes: 0 }), {
      "GET /v1/timesheet-submissions": { status: 200, body: { items: [waiting] } },
      "GET /v1/timesheet-submissions/s9": { status: 200, headers: { ETag: '"1-submitted"' }, body: { ...waiting, entries: [{ id: "e1", project_id: "p1", task_id: "t1", work_date: "2026-11-17", minutes: 630, billable: true, note: "Migrated", status: "submitted", version: 2 }] } },
      ...more,
    });

  it("lists the weeks to approve, and returns one only with a comment", async () => {
    const seen = reads({ "POST /v1/timesheet-submissions/s9/return": { status: 200, body: { ...waiting, status: "returned", entries: [] } } });
    renderPage(Timesheet, { path: "/projects/timesheet", session: approver });
    expect(await screen.findByText("Priya Nair · Acme rollout")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Review the week" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/Migrated/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Return with a comment" }));
    await invalid(dialog, /Comment/, "Say what the person should fix.");
    expect(seen.find((s) => s.method === "POST")).toBeUndefined();
    fireEvent.change(within(dialog).getByLabelText(/Comment/), { target: { value: "Add Wednesday." } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Return with a comment" }));
    await waitFor(() => expect(seen.find((s) => s.method === "POST")).toBeTruthy());
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ comment: "Add Wednesday." });
    expect(post?.headers.get("If-Match")).toBe('"1-submitted"');
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("approves a week over its ETag", async () => {
    const seen = reads({ "POST /v1/timesheet-submissions/s9/approve": { status: 200, body: { ...waiting, status: "approved", entries: [] } } });
    renderPage(Timesheet, { path: "/projects/timesheet", session: approver });
    fireEvent.click(await screen.findByRole("button", { name: "Review the week" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText(/Migrated/);
    fireEvent.click(within(dialog).getByRole("button", { name: "Approve the week" }));
    await waitFor(() => expect(seen.find((s) => s.path.endsWith("/approve"))).toBeTruthy());
    expect(seen.find((s) => s.path.endsWith("/approve"))?.headers.get("If-Match")).toBe('"1-submitted"');
  });
});
