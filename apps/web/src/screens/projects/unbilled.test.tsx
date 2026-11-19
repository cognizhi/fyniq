import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { invalid } from "../../test/invalid";
import { renderPage } from "../../test/render";
import { ProjectUnbilled } from "./ProjectUnbilled";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const money = (amount: string) => ({ amount, currency: "SGD" });
const unbilled = (extra = {}) => ({
  project_id: "p1",
  billing_method: "time_and_materials",
  currency: "SGD",
  has_customer: true,
  time: [{ entry_id: "e1", user_id: "u1", person_name: "Priya Nair", work_date: "2026-10-13", minutes: 750, rate: "20.00", value: "250.00" }],
  unpriced_time: [],
  expenses: [{ document_id: "d1", line: 0, source: "bill", number: "BILL-0007", label: "Venue hire", cost: money("420.00"), markup_percent: "10", markup: money("42.00"), billable: money("462.00") }],
  expenses_in_other_currencies: [],
  time_value: money("250.00"),
  expense_value: money("462.00"),
  total: money("712.00"),
  ...extra,
});

// req: FR-PRJ-10
describe("a project's unbilled work", () => {
  it("lists the time at billing rates and the costs at cost plus markup, and drafts the invoice", async () => {
    const seen = answer({
      "GET /v1/projects/p1/unbilled": { status: 200, body: unbilled() },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "a1", code: "4000", name: "Sales", type: "revenue", group: false, active: true }] } },
      "POST /v1/project-invoice-drafts": { status: 201, body: { id: "inv1" } },
    });
    renderPage(() => <ProjectUnbilled project="p1" />, { path: "/projects/p1", session: accountant });
    const t = await screen.findByRole("table", { name: "Unbilled time" });
    expect(within(t).getByText("Priya Nair")).toBeTruthy();
    expect(within(t).getByText("250.00")).toBeTruthy();
    const c = screen.getByRole("table", { name: "Unbilled costs" });
    expect(within(c).getByText("462.00")).toBeTruthy();
    expect(within(c).getByText(/42.00 \(10%\)/)).toBeTruthy();
    expect(screen.getByText(/SGD 712.00 to bill/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Draft an invoice" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "4000 Sales" });
    fireEvent.change(within(dialog).getByLabelText("GST code"), { target: { value: "" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the invoice" }));
    await invalid(dialog, "Revenue account", "Choose the revenue account the invoice's lines earn.");
    await invalid(dialog, "GST code", "Enter the GST code, such as SR.");
    fireEvent.change(within(dialog).getByLabelText("GST code"), { target: { value: "SR" } });
    fireEvent.change(within(dialog).getByLabelText("Revenue account"), { target: { value: "a1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the invoice" }));
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/project-invoice-drafts")).toBe(true));
    expect(seen.find((x) => x.method === "POST")?.body).toEqual({ project_id: "p1", revenue_account_id: "a1", gst_code: "SR" });
  });
  it("warns of time with no billing rate, hides the button from those who cannot invoice, and shows nothing for an internal project", async () => {
    answer({ "GET /v1/projects/p1/unbilled": { status: 200, body: unbilled({ time: [], expenses: [], unpriced_time: [{ entry_id: "e2", user_id: "u1", person_name: "Priya", work_date: "2026-10-13", minutes: 60 }], total: money("0.00") }) } });
    renderPage(() => <ProjectUnbilled project="p1" />, { path: "/projects/p1", session: ownerSession as Session });
    expect(await screen.findByText(/no billing rate, so they cannot be invoiced/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Draft an invoice" })).toBeNull();
    answer({ "GET /v1/projects/p2/unbilled": { status: 200, body: unbilled({ billing_method: "internal" }) } });
    renderPage(() => <ProjectUnbilled project="p2" />, { path: "/projects/p2", session: accountant });
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryAllByText("Unbilled work").length).toBe(1);
  });
});
