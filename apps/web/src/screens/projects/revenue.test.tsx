import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ProjectRevenue } from "./ProjectRevenue";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const position = {
  fee: sgd("1000.00"),
  estimated_cost: sgd("600.00"),
  cost_to_date: sgd("200.00"),
  costs_posted: sgd("300.00"),
  costs_left_out: sgd("100.00"),
  progress: "50.0",
  overrun: false,
  earned: sgd("500.00"),
  invoiced: sgd("400.00"),
  held: sgd("400.00"),
  adjustment: sgd("100.00"),
  contract_asset: sgd("100.00"),
  contract_liability: sgd("0.00"),
  nothing_to_propose: false,
};

// req: FR-PRJ-11
describe("a fixed-fee project's revenue over time", () => {
  it("is elected with the estimate by an accountant, and a reader sees only what it says", async () => {
    answer({
      "GET /v1/projects/p1/revenue": { status: 200, body: { election: { method: "milestone", version: 0 }, runs: [] } },
      "PUT /v1/projects/p1/revenue-election": { status: 200, body: { election: { method: "over_time", estimated_cost: sgd("600.00"), version: 1 }, runs: [] } },
    });
    renderPage(() => <ProjectRevenue project="p1" />, { path: "/projects/p1", session: accountant });
    expect(await screen.findByText(/Revenue is recognised as each milestone is invoiced/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Estimated total cost"), { target: { value: "600.00" } });
    fireEvent.change(screen.getByLabelText("Costs to leave out of progress (if any)"), { target: { value: "50.00" } });
    fireEvent.change(screen.getByLabelText("Why they do not show progress"), { target: { value: "Rework" } });
    fireEvent.click(screen.getByRole("button", { name: "Elect revenue over time" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/projects/p1/revenue-election")?.body).toEqual({ method: "over_time", estimated_cost: "600.00", excluded_cost: "50.00", excluded_reason: "Rework" }));
    expect(seen.find((s) => s.path === "/v1/projects/p1/revenue-election")?.headers.get("If-Match")).toBeNull();
  });

  it("shows the month's figures, proposes its journal as a draft and lists the months proposed", async () => {
    answer({
      "GET /v1/projects/p1/revenue": {
        status: 200,
        body: { election: { method: "over_time", estimated_cost: sgd("600.00"), excluded_cost: sgd("100.00"), excluded_reason: "Rework", version: 1 }, month: "2026-10-01", position, runs: [{ month: "2026-09-01", journal_id: "j1", adjustment: sgd("50.00"), earned: sgd("50.00") }] },
      },
      "POST /v1/projects/p1/revenue-journals": { status: 201, body: { run: { month: "2026-10-01", journal_id: "j2", adjustment: sgd("100.00"), earned: sgd("500.00") }, figures: position } },
    });
    const { container } = renderPage(() => <ProjectRevenue project="p1" />, { path: "/projects/p1", session: accountant });
    const dl = await screen.findByLabelText(/Revenue over time, /);
    expect(within(dl).getByText("SGD 200.00 of SGD 600.00, 50.0%")).toBeTruthy();
    expect(within(dl).getByText(/SGD 300.00, of which SGD 100.00 left out: Rework/)).toBeTruthy();
    expect(within(dl).getByText("Contract asset (earned, not invoiced)")).toBeTruthy();
    expect(screen.getByRole("list", { name: "Months proposed" })).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Propose the month's journal" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/projects/p1/revenue-journals")?.body).toEqual({ month: "2026-10-01" }));
    expect(await screen.findByText(/proposed as a draft, booking SGD 100.00/)).toBeTruthy();
  });

  it("says why a month cannot be worked out, and gives a reader no buttons", async () => {
    answer({
      "GET /v1/projects/p1/revenue": {
        status: 200,
        body: { election: { method: "over_time", estimated_cost: sgd("600.00"), version: 1 }, problem: "The project has no milestones, so it has no fee to recognise. Add the milestones that make up its fixed fee.", runs: [] },
      },
    });
    renderPage(() => <ProjectRevenue project="p1" />, { path: "/projects/p1", session: auditor });
    expect(await screen.findByText(/The project has no milestones/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Back to milestones" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Propose the month's journal" })).toBeNull();
  });
});
