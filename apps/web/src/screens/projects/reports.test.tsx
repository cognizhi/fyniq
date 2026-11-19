import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ProjectReports } from "./ProjectReports";

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const row = { project_id: "p1", code: "ACME-01", name: "Acme rollout", revenue: "1000.00", direct_costs: "300.00", payroll_tagged: "1500.00", time_cost: "210.00", margin: "490.00", minutes: 180, billable_minutes: 180, realisation: "222.2", rate_basis: "standard", budget_used: "30.0" };
const report = (extra = {}) => ({ from: "2026-10-01", to: "2026-10-01", currency: "SGD", costs_visible: true, projects: [row], people: [{ user_id: "u1", person_name: "Priya", minutes: 180, billable_minutes: 180, available_minutes: 10080, utilisation: "1.8" }], untagged: { project_id: "", code: "", name: "Tagged with no project", revenue: "500.00", direct_costs: "0.00", minutes: 0, billable_minutes: 0, rate_basis: "none" }, ...extra });

// req: FR-PRJ-12
describe("the project reports", () => {
  it("shows revenue, costs, time at cost, margin, realisation and utilisation, and says which rate it used", async () => {
    answer({ "GET /v1/reports/projects": { status: 200, body: report() } });
    renderPage(ProjectReports, { path: "/reports/projects", session: controller });
    const t = await screen.findByRole("table", { name: "Project profitability" });
    expect(within(t).getByText(/ACME-01/)).toBeTruthy();
    expect(within(t).getByText("490.00")).toBeTruthy();
    expect(within(t).getByText("222.2%")).toBeTruthy();
    expect(within(t).getByText(/at the standard rate/)).toBeTruthy();
    expect(within(t).getByText(/1,500.00 of salary tagged, listed apart/)).toBeTruthy();
    expect(within(t).getByText("Tagged with no project")).toBeTruthy();
    expect(screen.getByRole("table", { name: "Utilisation by person" })).toBeTruthy();
    await noViolations(document.body);
  });
  it("shows hours and revenue only to a viewer who may not see costs, and says why when switched off", async () => {
    answer({ "GET /v1/reports/projects": { status: 200, body: report({ costs_visible: false, untagged: undefined, projects: [{ ...row, direct_costs: undefined, payroll_tagged: undefined, time_cost: undefined, margin: undefined }] }) } });
    renderPage(ProjectReports, { path: "/reports/projects", session: controller });
    expect(await screen.findByText("You may see revenue and hours only.")).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "Margin" })).toBeNull();
    answer({ "GET /v1/reports/projects": problem(403, "PRJ-09", "Projects and time are switched off for this organisation.") });
    renderPage(ProjectReports, { path: "/reports/projects", session: controller });
    expect(await screen.findAllByText("Projects and time are switched off")).toBeTruthy();
  });
});
