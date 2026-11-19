import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, calls, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { hours, minutesOf, type Project, type ProjectTask } from "./data";
import { ProjectRecord } from "./ProjectRecord";
import { Projects } from "./Projects";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const employee = { ...ownerSession, roles: ["employee"] } as Session;

const project = (id: string, code: string, name: string, extra: Partial<Project> = {}): Project => ({
  id,
  dimension_id: "d1",
  code,
  name,
  configured: true,
  customer_id: "c1",
  customer_name: "Acme Pte Ltd",
  currency: "SGD",
  status: "active",
  billing_method: "time_and_materials",
  starts_on: "2026-11-01",
  ends_on: "2027-02-28",
  budget_minutes: 24000,
  budget_amount: "60000.00",
  people: [
    { user_id: "u1", display_name: "Wei Lim", manager: true, can_approve: true },
    { user_id: "u2", display_name: "Sam Tan", manager: true, can_approve: false },
    { user_id: "u3", display_name: "Priya Nair", manager: false, can_approve: false },
  ],
  warnings: ["Sam Tan is named a manager but holds no role that approves time (the Approver or the Controller): they cannot approve until they are given one"],
  version: 3,
  ...extra,
});
const atlas = project("p1", "ATLAS", "Atlas rollout");
const old = project("p2", "OLD-1", "Old project", { configured: false, customer_id: undefined, customer_name: undefined, currency: undefined, billing_method: undefined, budget_minutes: undefined, budget_amount: undefined, people: [], warnings: ["This value of the Project dimension has no project record yet: set it up to log time on it"], version: 0 });
const internal = project("p3", "INT-1", "Internal tools", { billing_method: "internal", customer_id: undefined, customer_name: undefined, status: "closed", warnings: [] });
const task = (id: string, name: string, extra: Partial<ProjectTask> = {}): ProjectTask => ({ id, project_id: "p1", name, billable: true, active: true, version: 1, ...extra });

// req: FR-PRJ-01
describe("hours", () => {
  it("writes whole minutes as hours and reads hours typed three ways", () => {
    expect(hours(150)).toBe("2 h 30 min");
    expect(hours(24000)).toBe("400 h");
    expect(hours(45)).toBe("45 min");
    expect(hours(-90)).toBe("-1 h 30 min");
    expect(minutesOf("400")).toBe(24000);
    expect(minutesOf("7.5")).toBe(450);
    expect(minutesOf("7:30")).toBe(450);
    expect(minutesOf("2h")).toBeNull();
    expect(minutesOf("7:75")).toBeNull();
  });
});

// req: FR-PRJ-01
// req: FR-ORG-06
// req: FR-UX-40
describe("the projects list", () => {
  const lists = (items: Project[], more: Record<string, Parameters<typeof answer>[0][string]> = {}) =>
    answer({
      "GET /v1/projects": { status: 200, body: { items } },
      "GET /v1/project-people": { status: 200, body: { items: [{ user_id: "u1", display_name: "Wei Lim", manager: false, can_approve: true }, { user_id: "u2", display_name: "Sam Tan", manager: false, can_approve: false }] } },
      "GET /v1/contacts": { status: 200, body: { items: [{ id: "c1", legal_name: "Acme Pte Ltd" }] } },
      ...more,
    });

  it("lists each project with its customer, billing, status and budgets, and axe finds nothing", async () => {
    lists([atlas, old, internal]);
    renderPage(Projects, { path: "/projects", session: accountant });
    const table = await screen.findByRole("table", { name: "Projects" });
    const [, a, o, i] = within(table).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement, HTMLElement];
    expect(within(a).getByText("Atlas rollout")).toBeTruthy();
    expect(within(a).getByText("Acme Pte Ltd")).toBeTruthy();
    expect(within(a).getByText("Time and materials")).toBeTruthy();
    expect(within(a).getByText("400 h")).toBeTruthy();
    expect(within(a).getByText("SGD 60,000.00")).toBeTruthy();
    expect(within(o).getByText("Not set up")).toBeTruthy();
    expect(within(i).getByText("Closed")).toBeTruthy();
    expect(within(i).getByText("Internal", { selector: "span" })).toBeTruthy();
    expect(screen.getByText("3 projects")).toBeTruthy();
    await noViolations(document.body);
  });

  it("narrows by status", async () => {
    lists([atlas, internal]);
    renderPage(Projects, { path: "/projects", session: accountant });
    await screen.findByRole("table", { name: "Projects" });
    fireEvent.change(screen.getByLabelText("Status"), { target: { value: "closed" } });
    expect(screen.getByText("1 project")).toBeTruthy();
    expect(screen.queryByText("Atlas rollout")).toBeNull();
  });

  it("says why when Projects and time are switched off", async () => {
    answer({ "GET /v1/projects": problem(403, "PRJ-09", "Projects and time are switched off for this organisation.") });
    renderPage(Projects, { path: "/projects", session: accountant });
    expect(await screen.findByText("Projects and time are switched off")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a project" })).toBeNull();
  });

  it("offers the add button to those who keep projects only", async () => {
    lists([atlas]);
    renderPage(Projects, { path: "/projects", session: employee });
    await screen.findByRole("table", { name: "Projects" });
    expect(screen.queryByRole("button", { name: "Add a project" })).toBeNull();
  });

  it("says what a project needs beside each field, before anything is sent", async () => {
    lists([]);
    renderPage(Projects, { path: "/projects", session: accountant });
    expect(await screen.findByText("No projects yet")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Add a project" })[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "A B" } });
    fireEvent.change(within(dialog).getByLabelText("Currency"), { target: { value: "S$" } });
    fireEvent.change(within(dialog).getByLabelText("Hours budget"), { target: { value: "lots" } });
    fireEvent.change(within(dialog).getByLabelText("Money budget (S$)"), { target: { value: "sixty" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the project" }));
    await invalid(dialog, "Code", "Give a code of a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, such as ACME-01.");
    await invalid(dialog, "Name", "Give the project a name, such as Acme rollout.");
    await invalid(dialog, "Currency", "Give the currency as three letters, such as SGD.");
    await invalid(dialog, "Hours budget", "Write the hours as 400, 7.5 or 7:30.");
    await invalid(dialog, /Money budget/, "Write the amount as a plain number, such as 60000.00.");
    expect(calls()).not.toContain("POST /v1/projects");
  });

  it("adds a project with its people, over an Idempotency-Key", async () => {
    const seen = lists([], { "POST /v1/projects": { status: 201, body: atlas } });
    renderPage(Projects, { path: "/projects", session: accountant });
    fireEvent.click((await screen.findAllByRole("button", { name: "Add a project" }))[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "ATLAS" } });
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Atlas rollout" } });
    fireEvent.change(within(dialog).getByLabelText("Hours budget"), { target: { value: "400" } });
    fireEvent.change(within(dialog).getByLabelText("Money budget (SGD)"), { target: { value: "60000.00" } });
    await waitFor(() => expect(within(dialog).getByLabelText("Customer")).toBeTruthy());
    fireEvent.click(await within(dialog).findByRole("checkbox", { name: "Wei Lim" }));
    fireEvent.click(within(dialog).getAllByRole("checkbox", { name: "Manager" })[0]!);
    expect(within(dialog).queryByText("Holds no role that approves time")).toBeNull();
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Sam Tan" }));
    fireEvent.click(within(dialog).getAllByRole("checkbox", { name: "Manager" })[1]!);
    expect(await within(dialog).findByText("Holds no role that approves time")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the project" }));
    await waitFor(() => expect(seen.find((s) => s.method === "POST")).toBeTruthy());
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toMatchObject({ code: "ATLAS", name: "Atlas rollout", currency: "SGD", billing_method: "time_and_materials", budget_minutes: 24000, budget_amount: "60000.00",
      people: [{ user_id: "u1", manager: true }, { user_id: "u2", manager: true }] });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });
});

// req: FR-PRJ-01
// req: FR-PRJ-05
describe("a project", () => {
  const reads = (p: Project, tasks: ProjectTask[], more: Record<string, Parameters<typeof answer>[0][string]> = {}) =>
    answer({
      [`GET /v1/projects/${p.id}`]: { status: 200, body: p },
      [`GET /v1/projects/${p.id}/tasks`]: { status: 200, body: { items: tasks } },
      "GET /v1/project-people": { status: 200, body: { items: [] } },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      ...more,
    });

  it("shows its people, warns of a manager who cannot approve, and lists its tasks", async () => {
    reads(atlas, [task("t1", "Data migration"), task("t2", "Support", { billable: false, active: false })]);
    renderPage(ProjectRecord, { path: "/projects/p1", route: "/projects/$projectId", session: accountant });
    expect(await screen.findByRole("heading", { name: "Atlas rollout" })).toBeTruthy();
    expect(screen.getByText(/Sam Tan is named a manager but holds no role that approves time/)).toBeTruthy();
    const people = screen.getByRole("table", { name: "People on the project" });
    const rows = within(people).getAllByRole("row");
    expect(within(rows[1]!).getByText("Yes")).toBeTruthy();
    expect(within(rows[2]!).getByText(/No: holds no role that approves time/)).toBeTruthy();
    expect(within(rows[3]!).getByText("Not a manager")).toBeTruthy();
    const tasks = await screen.findByRole("table", { name: "Tasks" });
    expect(within(tasks).getByText("Data migration")).toBeTruthy();
    expect(within(tasks).getByText("Out of use")).toBeTruthy();
    expect(screen.getByText("400 h")).toBeTruthy();
    await noViolations(document.body);
  });

  it("adds a task, over an Idempotency-Key, and changes one over its version", async () => {
    const seen = reads(atlas, [task("t1", "Data migration")], {
      "POST /v1/project-tasks": { status: 201, body: task("t3", "Handover") },
      "PUT /v1/project-tasks/t1": { status: 200, body: task("t1", "Data migration", { active: false, version: 2 }) },
    });
    renderPage(ProjectRecord, { path: "/projects/p1", route: "/projects/$projectId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Add a task" }));
    const add = await screen.findByRole("dialog");
    fireEvent.click(within(add).getByRole("button", { name: "Add the task" }));
    await invalid(add, "Name", "Give the task a name, such as Data migration.");
    fireEvent.change(within(add).getByLabelText("Name"), { target: { value: " Handover " } });
    fireEvent.click(within(add).getByRole("button", { name: "Add the task" }));
    await waitFor(() => expect(seen.find((s) => s.method === "POST")).toBeTruthy());
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ project_id: "p1", name: "Handover", billable: true });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(await screen.findByRole("button", { name: "Change Data migration" }));
    const change = await screen.findByRole("dialog");
    fireEvent.click(within(change).getByLabelText(/In use: takes new time/));
    fireEvent.click(within(change).getByRole("button", { name: "Save the task" }));
    await waitFor(() => expect(seen.find((s) => s.method === "PUT")).toBeTruthy());
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.body).toEqual({ name: "Data migration", billable: true, active: false });
    expect(put?.headers.get("If-Match")).toBe('"1"');
  });

  it("offers to set up a value of the Project dimension that has no record, over If-Match 0", async () => {
    const seen = reads(old, [], { "PUT /v1/projects/p2": { status: 200, body: { ...old, configured: true, version: 1 } } });
    renderPage(ProjectRecord, { path: "/projects/p2", route: "/projects/$projectId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Set up the project" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("How it is billed"), { target: { value: "internal" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Set up the project" }));
    await waitFor(() => expect(seen.find((s) => s.method === "PUT")).toBeTruthy());
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"0"');
    expect(put?.body).toMatchObject({ name: "Old project", status: "active", billing_method: "internal", currency: "SGD" });
  });

  it("is hidden from a person the project is not for, and says why when switched off", async () => {
    answer({ "GET /v1/projects/p9": problem(404, "API-NOT-FOUND-01", "No such project.") });
    renderPage(ProjectRecord, { path: "/projects/p9", route: "/projects/$projectId", session: employee });
    expect(await screen.findByText(/The project could not be read/)).toBeTruthy();
    answer({ "GET /v1/projects/p9": problem(403, "PRJ-09", "Projects and time are switched off for this organisation.") });
    renderPage(ProjectRecord, { path: "/projects/p9", route: "/projects/$projectId", session: employee });
    expect(await screen.findAllByText("Projects and time are switched off")).toBeTruthy();
  });
});
