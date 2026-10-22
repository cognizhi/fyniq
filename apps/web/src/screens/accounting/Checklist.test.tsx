import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PeriodClose } from "./PeriodClose";

type Item = components["schemas"]["ChecklistItem"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const owner = { ...ownerSession, roles: ["owner"] } as Session;
const period = { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open" as const, financial_year: 1, version: 1 };
const items: Item[] = [
  { key: "bank", title: "Bank, card and wallet accounts reconciled", owner_role: "accountant", due_on: "2026-11-07", state: "open" },
  { key: "payroll", title: "Payroll and CPF posted", owner_role: "accountant", due_on: "2026-11-07", state: "open" },
  { key: "review", title: "The month reviewed", owner_role: "controller", due_on: "2026-11-12", state: "done", note: "Read through", by: { kind: "user", id: "u2", name: "Wei Ling" }, at: "2026-11-10T02:00:00Z" },
];
const list = (tasks: Item[]) => ({
  period,
  template_version: 0,
  done: tasks.filter((t) => t.state === "done").length,
  skipped: tasks.filter((t) => t.state === "skipped").length,
  open: tasks.filter((t) => t.state === "open").length,
  tasks,
});
const base = {
  "GET /v1/periods/p10/close": { status: 200, body: { period, you_may_attest: false }, headers: { ETag: `"1-open"` } },
  "GET /v1/periods/p10/variances": { status: 200, body: { period, threshold_amount: { amount: "1000.00", currency: "SGD" }, threshold_percent: "10", unexplained: 0, items: [] } },
  "GET /v1/periods/p10/year-end": {
    status: 200,
    body: { number: 1, starts_on: "2026-10-01", ends_on: "2027-09-30", periods: 12, periods_closed: 0, closed: false, last_period: { ...period, id: "p09" },
      income: { amount: "0.00", currency: "SGD" }, expenses: { amount: "0.00", currency: "SGD" }, result: { amount: "0.00", currency: "SGD" } },
  },
};
const at = { path: "/accounting/close/p10", route: "/accounting/close/$periodId" };

// req: FR-GL-07
// req: C.2 #58
describe("the month's close checklist", () => {
  it("marks a task done, skips one with a reason, and opens one again; axe finds nothing", async () => {
    let tasks = items;
    answer({
      ...base,
      "GET /v1/periods/p10/checklist": () => ({ status: 200, body: list(tasks) }),
      "POST /v1/periods/p10/checklist/bank": () => {
        tasks = tasks.map((t) => (t.key === "bank" ? { ...t, state: "done", by: { kind: "user", id: "u1", name: "Ah Kow" }, at: "2026-11-06T01:00:00Z" } : t));
        return { status: 200, body: list(tasks) };
      },
      "POST /v1/periods/p10/checklist/payroll": (body) => {
        tasks = tasks.map((t) => (t.key === "payroll" ? { ...t, state: "skipped", note: (body as { note: string }).note, by: { kind: "user", id: "u1", name: "Ah Kow" } } : t));
        return { status: 200, body: list(tasks) };
      },
      "POST /v1/periods/p10/checklist/review": () => {
        tasks = tasks.map((t) => (t.key === "review" ? { ...t, state: "open" } : t));
        return { status: 200, body: list(tasks) };
      },
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    expect(await screen.findByText("1 of 3 tasks done or skipped")).toBeTruthy();
    expect(screen.getByRole("progressbar", { name: "1 of 3 tasks done or skipped" }).getAttribute("aria-valuenow")).toBe("1");
    expect(screen.getAllByText("Accountant · due 7 Nov 2026")).toHaveLength(2);
    expect(screen.getByText(/Done by Wei Ling on .* · Read through/)).toBeTruthy();
    expect(screen.getByText("The recommended checklist")).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Done: Bank, card and wallet accounts reconciled" }));
    expect(await screen.findByText("Done: Bank, card and wallet accounts reconciled. Run the checks again so they see it.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "POST")!;
    expect(sent.body).toEqual({ state: "done" });
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();

    // A skip needs a reason.
    fireEvent.click(screen.getByRole("button", { name: "Skip: Payroll and CPF posted" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Skip" }));
    expect(await within(dialog).findByText("Say why the task does not apply this month.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why it does not apply this month"), { target: { value: "No employees yet" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Skip" }));
    expect(await screen.findByText("Skipped for this month: Payroll and CPF posted. Run the checks again so they see it.")).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST")[1]!.body).toEqual({ state: "skipped", note: "No employees yet" });
    expect(screen.getByText("Skipped by Ah Kow: No employees yet")).toBeTruthy();
    expect(screen.getByText("3 of 3 tasks done or skipped")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Open again: The month reviewed" }));
    expect(await screen.findByText("Opened again: The month reviewed. Run the checks again so they see it.")).toBeTruthy();
    expect(screen.getByText("2 of 3 tasks done or skipped")).toBeTruthy();
  });

  it("changes the template: reorders, adds and removes tasks, at the version read", async () => {
    let saved = false;
    answer({
      ...base,
      "GET /v1/periods/p10/checklist": () => ({ status: 200, body: { ...list(items), template_version: saved ? 1 : 0 } }),
      "GET /v1/close-checklist-template": {
        status: 200,
        body: {
          version: 0,
          recommended: true,
          tasks: [
            { key: "bank", title: "Bank reconciled", owner_role: "accountant", due_days: 7 },
            { key: "review", title: "The month reviewed", owner_role: "controller", due_days: 12 },
          ],
        },
        headers: { ETag: `"0"` },
      },
      "PUT /v1/close-checklist-template": () => {
        saved = true;
        return { status: 200, body: { version: 1, recommended: false, tasks: [] } };
      },
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Change the template…" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByDisplayValue("Bank reconciled")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Move task 2 up" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a task" }));
    fireEvent.change(within(dialog).getByLabelText("Task 3"), { target: { value: "Board pack sent" } });
    fireEvent.change(within(dialog).getAllByLabelText("Due, days after the month's end")[2]!, { target: { value: "15" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove task 2" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the template" }));
    expect(await screen.findByText("Saved version 1 of the checklist template. Open months use it now.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "PUT")!;
    expect(sent.headers.get("If-Match")).toBe(`"0"`);
    expect(sent.body).toEqual({
      tasks: [
        { key: "review", title: "The month reviewed", owner_role: "controller", due_days: 12 },
        { title: "Board pack sent", due_days: 15 },
      ],
    });
    expect(await screen.findByText("Checklist template, version 1")).toBeTruthy();
  });

  it("shows why a task was not marked", async () => {
    answer({
      ...base,
      "GET /v1/periods/p10/checklist": { status: 200, body: list(items) },
      "POST /v1/periods/p10/checklist/bank": problem(409, "STORAGE-CLOSE-03", "The period is closed.", "Reopen it first."),
    });
    renderPage(PeriodClose, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Done: Bank, card and wallet accounts reconciled" }));
    expect(await screen.findByText("The task was not marked")).toBeTruthy();
  });

  it("lets only those who run the checks mark tasks and change the template", async () => {
    answer({ ...base, "GET /v1/periods/p10/checklist": { status: 200, body: list(items) } });
    renderPage(PeriodClose, { ...at, session: owner });
    expect((await screen.findAllByText("Payroll and CPF posted")).length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /^Done: / })).toBeNull();
    expect(screen.queryByRole("button", { name: "Change the template…" })).toBeNull();
  });
});
