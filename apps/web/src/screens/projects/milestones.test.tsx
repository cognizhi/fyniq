import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { ProjectMilestones } from "./ProjectMilestones";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const money = (amount: string) => ({ amount, currency: "SGD" });
const list = (extra = {}) => ({
  milestones: [
    { id: "m1", project_id: "p1", kind: "milestone", name: "Design sign-off", amount: money("1500.00"), due_on: "2026-12-15", version: 1, can_invoice: false },
    { id: "m2", project_id: "p1", kind: "milestone", name: "Launch", amount: money("3000.00"), accepted_on: "2026-10-14", version: 2, can_invoice: true },
  ],
  ...extra,
});

// req: FR-PRJ-10
describe("a project's milestones", () => {
  it("lists them, records an acceptance and adds one with its problems said", async () => {
    const seen = answer({
      "GET /v1/projects/p1/milestones": { status: 200, body: list() },
      "POST /v1/project-milestones/m1/accept": { status: 200, body: { ...list().milestones[0], accepted_on: "2026-10-14", version: 2 } },
      "POST /v1/project-milestones": { status: 201, body: list().milestones[0] },
    });
    renderPage(() => <ProjectMilestones project="p1" method="fixed_fee" />, { path: "/projects/p1", session: accountant });
    const t = await screen.findByRole("table", { name: "Milestones" });
    expect(within(t).getByText("Awaiting acceptance")).toBeTruthy();
    expect(within(t).getByText(/Accepted/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Record acceptance" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/project-milestones/m1/accept")).toBe(true));
    expect(seen.find((x) => x.path === "/v1/project-milestones/m1/accept")?.headers.get("If-Match")).toBe('"1"');
    fireEvent.click(screen.getByRole("button", { name: "Add a milestone" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await invalid(dialog, "Milestone", "Name the milestone, such as Design sign-off.");
    await invalid(dialog, "Amount", "Write what it bills as a number above zero, such as 15000.00.");
    fireEvent.change(within(dialog).getByLabelText("Milestone"), { target: { value: "Handover" } });
    fireEvent.change(within(dialog).getByLabelText("Amount"), { target: { value: "500.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(seen.find((x) => x.method === "POST" && x.path === "/v1/project-milestones")?.body).toEqual({ project_id: "p1", name: "Handover", amount: "500.00" }));
  });
  it("shows a retainer's balance and drafts an invoice for a period", async () => {
    const seen = answer({
      "GET /v1/projects/p1/milestones": {
        status: 200,
        body: list({ milestones: [{ id: "m3", project_id: "p1", kind: "retainer", name: "November", amount: money("1000.00"), version: 1, can_invoice: true }], retainer: { billed: money("0.00"), drawn: money("300.00"), left: money("-300.00"), unpriced_entries: 0 } }),
      },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "a1", code: "4000", name: "Sales", type: "revenue", group: false, active: true }] } },
      "POST /v1/project-invoice-drafts": { status: 201, body: { id: "inv1" } },
    });
    renderPage(() => <ProjectMilestones project="p1" method="retainer" />, { path: "/projects/p1", session: accountant });
    expect(await screen.findByText(/drawn down by time SGD 300.00, SGD .300.00 left/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Draft an invoice" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "4000 Sales" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the invoice" }));
    await invalid(dialog, "Revenue account", "Choose the revenue account the invoice's line earns.");
    fireEvent.change(within(dialog).getByLabelText("Revenue account"), { target: { value: "a1" } });
    fireEvent.change(within(dialog).getByLabelText("GST code"), { target: { value: "" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the invoice" }));
    await invalid(dialog, "GST code", "Enter the GST code, such as SR.");
    fireEvent.change(within(dialog).getByLabelText("GST code"), { target: { value: "SR" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the invoice" }));
    await waitFor(() => expect(seen.find((x) => x.path === "/v1/project-invoice-drafts")?.body).toEqual({ project_id: "p1", revenue_account_id: "a1", gst_code: "SR", milestone_id: "m3" }));
  });
});
