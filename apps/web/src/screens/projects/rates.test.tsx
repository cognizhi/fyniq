import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { CostRates } from "./CostRates";
import { ProjectRates } from "./ProjectRates";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const manager = { ...ownerSession, roles: ["approver"] } as Session;

// req: FR-PRJ-06
// req: FR-PAY-13
describe("cost rates", () => {
  const rates = { items: [{ id: "r1", user_id: "u1", person_name: "Ann", role: "Consultant", effective_from: "2026-01-01", hourly_rate: "60.000000", currency: "SGD" }], people: [{ user_id: "u1", display_name: "Ann", manager: false, can_approve: false }] };

  it("lists the rates, and axe finds nothing", async () => {
    answer({ "GET /v1/cost-rates": { status: 200, body: rates } });
    renderPage(CostRates, { path: "/projects/cost-rates", session: owner });
    const t = await screen.findByRole("table", { name: "Cost rates" });
    expect(within(t).getByText("Ann")).toBeTruthy();
    expect(within(t).getByText("SGD 60.000000")).toBeTruthy();
    await noViolations(document.body);
  });

  it("says what a rate needs, then enters it over an Idempotency-Key", async () => {
    const seen = answer({ "GET /v1/cost-rates": { status: 200, body: rates }, "POST /v1/cost-rates": { status: 201, body: rates.items[0] } });
    renderPage(CostRates, { path: "/projects/cost-rates", session: owner });
    fireEvent.click((await screen.findAllByRole("button", { name: "Enter a cost rate" }))[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rate" }));
    await invalid(dialog, "Person", "Choose the person, or the standard rate.");
    await invalid(dialog, "Starts", "Enter the day the rate starts.");
    await invalid(dialog, "Cost of an hour", "Write the cost of an hour as a plain number with at most 6 decimals, such as 62.50.");
    fireEvent.change(within(dialog).getByLabelText("Person"), { target: { value: "u1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rate" }));
    await invalid(dialog, "Role", "Name the role the rate is grouped under, such as Consultant.");
    fireEvent.change(within(dialog).getByLabelText("Role"), { target: { value: "Consultant" } });
    fireEvent.change(within(dialog).getByLabelText("Starts"), { target: { value: "2026-02-01" } });
    fireEvent.change(within(dialog).getByLabelText("Cost of an hour"), { target: { value: "62.50" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rate" }));
    await waitFor(() => expect(seen.find((s) => s.method === "POST")).toBeTruthy());
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ user_id: "u1", role: "Consultant", effective_from: "2026-02-01", hourly_rate: "62.50" });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });
});

describe("a project's time at cost", () => {
  const read = (cost: object) =>
    answer({
      "GET /v1/projects/p1/billing-rates": { status: 200, body: { items: [{ id: "b1", project_id: "p1", role: "Consultant", effective_from: "2026-01-01", hourly_rate: "150.00", currency: "SGD" }] } },
      "GET /v1/projects/p1/time-cost": { status: 200, body: cost },
    });
  it("shows the cost at the role average without any person's cost, and says so", async () => {
    read({ project_id: "p1", from: "2026-11-01", to: "2026-11-30", minutes: 180, billable_minutes: 180, cost: "240.51", rate_basis: "role_average", currency: "SGD", people: [{ user_id: "u1", person_name: "Ann", minutes: 60, billable_minutes: 60 }] });
    renderPage(() => <ProjectRates project="p1" billed may={false} />, { path: "/projects/p1", session: manager });
    expect(await screen.findByText(/Cost SGD 240.51/)).toBeTruthy();
    expect(screen.getByText(/average rate of their role/)).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "Cost" })).toBeNull();
    expect(screen.getByRole("table", { name: "Billing rates" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Enter a billing rate" })).toBeNull();
  });
  it("shows hours only when no rate may be used, and each person's cost at the payroll tier", async () => {
    read({ project_id: "p1", from: "2026-11-01", to: "2026-11-30", minutes: 60, billable_minutes: 0, rate_basis: "none", currency: "SGD", people: [{ user_id: "u1", person_name: "Ann", minutes: 60, billable_minutes: 0 }] });
    renderPage(() => <ProjectRates project="p1" billed={false} may />, { path: "/projects/p1", session: owner });
    expect(await screen.findByText(/No cost: you may see hours only/)).toBeTruthy();
    answer({ "GET /v1/projects/p1/billing-rates": { status: 200, body: { items: [] } }, "GET /v1/projects/p1/time-cost": { status: 200, body: { project_id: "p1", from: "a", to: "b", minutes: 60, billable_minutes: 60, cost: "60.00", rate_basis: "person", currency: "SGD", people: [{ user_id: "u1", person_name: "Ann", minutes: 60, billable_minutes: 60, cost: "60.00" }] } } });
    renderPage(() => <ProjectRates project="p1" billed may />, { path: "/projects/p1", session: owner });
    expect(await screen.findAllByRole("columnheader", { name: "Cost" })).toBeTruthy();
  });
  it("says what a billing rate needs before anything is sent", async () => {
    const seen = answer({ "GET /v1/projects/p1/billing-rates": { status: 200, body: { items: [] } }, "GET /v1/projects/p1/time-cost": { status: 200, body: { project_id: "p1", from: "a", to: "b", minutes: 0, billable_minutes: 0, rate_basis: "none", currency: "SGD", people: [] } } });
    renderPage(() => <ProjectRates project="p1" billed may />, { path: "/projects/p1", session: owner });
    fireEvent.click(await screen.findByRole("button", { name: "Enter a billing rate" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rate" }));
    await invalid(dialog, "Starts", "Enter the day the rate starts.");
    await invalid(dialog, "Price of an hour", "Write the price of an hour as a plain number, such as 150.00.");
    expect(seen.find((s) => s.method === "POST")).toBeUndefined();
  });
});
