import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ProjectProfitability } from "./ProjectProfitability";
import { ProjectReports } from "./ProjectReports";

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const row = { project_id: "p1", code: "ACME-01", name: "Acme rollout", revenue: "1000.00", direct_costs: "300.00", payroll_tagged: "0.00", time_cost: "210.00", margin: "490.00", minutes: 180, billable_minutes: 180,
  standard_value: "450.00", realisation: "222.2", rate_basis: "standard", budget_minutes: 600, budget_used: "30.0" };
const report = (extra = {}, r = {}) => ({ from: "2026-01-01", to: "2026-11-01", currency: "SGD", costs_visible: true, projects: [{ ...row, ...r }], people: [], ...extra });
const file = { export_id: "x1", file_name: "projects-2026-10.csv", media_type: "text/csv; charset=utf-8", sha256: "ab", size_bytes: 12, rows: 1, ledger_revision: 4, content_base64: "Y29kZQ==" };

// req: FR-PRJ-12
describe("a project's profitability card", () => {
  it("shows the project's own figures at the viewer's tier, names the rate, warns of a small group and lists the approved time on request; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/reports/projects": { status: 200, body: report() },
      "GET /v1/projects/p1/time-entries": { status: 200, body: { items: [{ id: "e1", project_id: "p1", task_id: "t1", work_date: "2026-10-13", minutes: 180, billable: true, note: "Migration", status: "approved", version: 1 }] } },
    });
    renderPage(() => <ProjectProfitability project="p1" startsOn="2026-01-05" />, { path: "/projects/p1", session: controller });
    const tiles = within(await screen.findByRole("group", { name: "The project's profitability" }));
    expect(tiles.getByText("SGD 1,000.00")).toBeTruthy();
    expect(tiles.getByText("SGD 490.00")).toBeTruthy();
    expect(tiles.getByText("Time at the standard rate")).toBeTruthy();
    expect(tiles.getByText("3 h")).toBeTruthy();
    expect(screen.getByText(/Fewer than three people with a rate hold each role/)).toBeTruthy();
    expect(seen.find((x) => x.path === "/v1/reports/projects")?.query).toContain("from=2026-01-01");
    fireEvent.click(screen.getByRole("button", { name: "Show the approved time" }));
    const t = await screen.findByRole("table", { name: "Approved time on the project" });
    expect(within(t).getByText("Migration")).toBeTruthy();
    expect(within(t).queryByText(/rate|cost/i)).toBeNull();
    await noViolations(document.body);
  });

  it("shows revenue and hours only to a viewer who may not see costs, and nothing to one with no right to the report", async () => {
    answer({ "GET /v1/reports/projects": { status: 200, body: report({ costs_visible: false }, { direct_costs: undefined, time_cost: undefined, margin: undefined, standard_value: undefined, realisation: undefined, rate_basis: "none" }) } });
    renderPage(() => <ProjectProfitability project="p1" />, { path: "/projects/p1", session: controller });
    expect(await screen.findByText("You may see revenue and hours only.")).toBeTruthy();
    expect(screen.queryByText("Margin")).toBeNull();
    expect(screen.queryByText(/Fewer than three people/)).toBeNull();
    answer({ "GET /v1/reports/projects": problem(403, "FORBIDDEN-01", "You may not read project reports.") });
    const { container } = renderPage(() => <ProjectProfitability project="p1" />, { path: "/projects/p1", session: controller });
    await waitFor(() => expect(container.textContent ?? "").not.toContain("Profitability"));
  });

  it("exports the report as CSV or Excel for the months chosen, as the server wrote it", async () => {
    const seen = answer({ "GET /v1/reports/projects": { status: 200, body: report() }, "GET /v1/reports/projects/export": { status: 200, body: file } });
    renderPage(ProjectReports, { path: "/reports/projects", session: controller });
    const csv = await screen.findByRole("button", { name: "Export CSV" });
    await waitFor(() => expect((csv as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(csv);
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/reports/projects/export")).toBe(true));
    const call = seen.find((x) => x.path === "/v1/reports/projects/export");
    expect(call?.query).toContain("format=csv");
    expect(call?.query).toMatch(/from=\d{4}-\d{2}-01/);
    answer({ "GET /v1/reports/projects": { status: 200, body: report() }, "GET /v1/reports/projects/export": problem(403, "FORBIDDEN-01", "You may not export project reports.") });
    fireEvent.click(await screen.findByRole("button", { name: "Export Excel" }));
    expect(await screen.findByText(/You may not export project reports/)).toBeTruthy();
  });
});
