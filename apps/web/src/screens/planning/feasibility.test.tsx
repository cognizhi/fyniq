import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Feasibility, percent } from "./Feasibility";

type Results = components["schemas"]["FeasibilityResults"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const id = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c9e01";
const plan = {
  name: "One-year idea", years: 1, discount_rate: "0.1", max_payback: "1", investment: "100.00", life: 1, salvage: "0.00", working_capital: "0",
  units: "1", unit_growth: "0", price: "300.00", price_growth: "0", variable_cost: "0.00", variable_cost_growth: "0", fixed: "0.00", fixed_growth: "0",
};
const year = (n: number, flow: string, pv: string, cum: string) => ({
  year: n, units: n ? "1.0" : "0.0", revenue: n ? "300.00" : "0.00", variable_costs: "0.00", fixed_costs: "0.00", depreciation: n ? "100.00" : "0.00",
  ebit: n ? "200.00" : "0.00", tax: n ? "34.00" : "0.00", nopat: n ? "166.00" : "0.00", working_capital: "0.00", working_capital_change: "0.00",
  salvage: "0.00", working_capital_release: "0.00", cash_flow: flow, present_value: pv, cumulative: cum, cumulative_present_value: cum,
});
// The results as the API works them out at 17% (TestFeasibilityThroughTheAPI's figures).
const results: Results = {
  study_id: id, currency: "SGD", tax_rate: "0.17", tax_pack: "sg-cit@2026.1",
  years: [year(0, "-100.00", "-100.00", "-100.00"), year(1, "266.00", "241.82", "166.00")],
  npv: "141.82", irr: "1.6600", payback_years: "0.4", discounted_payback_years: "0.4", profitability_index: "2.42", break_even_units: "0.0",
  break_even_revenue: "0.00", margin_of_safety_percent: "100.0",
  sensitivity: ["0.8", "0.9", "1", "1.1", "1.2"].map((v) => ({ volume: v, npv_by_fixed_costs: ["1.00", "2.00", "141.82", "4.00", "5.00"] })),
  npv_at_rates: [{ rate: "0.08", npv: "146.30" }, { rate: "0.4", npv: "90.00" }],
  verdict: "feasible", verdict_words: "Feasible: a positive NPV, an IRR of 166.00%, and a payback of 0.4 years",
  note: "A study never posts: nothing here changes the books (FR-PLN-08).",
};

// req: FR-PLN-06
// req: FR-PLN-08
// req: QA-UI-05
describe("Feasibility", () => {
  it("saves a new study and shows its verdict, figures, cash flows and sensitivity; axe finds nothing", async () => {
    answer({
      "GET /v1/feasibility-studies": { status: 200, body: { items: [] } },
      "POST /v1/feasibility-studies": { status: 201, body: { id, version: 1, date: "2026-10-15", study: plan }, headers: { ETag: `"1-draft"` } },
      [`GET /v1/feasibility-studies/${id}`]: { status: 200, body: { id, version: 1, date: "2026-10-15", study: plan }, headers: { ETag: `"1-draft"` } },
      [`GET /v1/feasibility-studies/${id}/results`]: { status: 200, body: results },
    });
    renderPage(Feasibility, { path: "/planning/feasibility", session: accountant, mode: "expert" });
    expect(await screen.findByText("Sandbox: nothing here posts")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "One-year idea" } });
    fireEvent.change(screen.getByLabelText("Years"), { target: { value: "1" } });
    fireEvent.change(screen.getByLabelText("Discount rate"), { target: { value: "0.1" } });
    fireEvent.change(screen.getByLabelText("Investment"), { target: { value: "100.00" } });
    fireEvent.change(screen.getByLabelText("Price per unit"), { target: { value: "300.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Feasible: a positive NPV, an IRR of 166.00%, and a payback of 0.4 years")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect((sent.body as Record<string, unknown>).name).toBe("One-year idea");
    expect((sent.body as Record<string, unknown>).years).toBe(1);
    expect((sent.body as Record<string, unknown>).investment).toBe("100.00");
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect(screen.getByText("166.00%")).toBeTruthy();
    expect(screen.getByText("At sg-cit's rate of 17% on the study's date (sg-cit@2026.1), fixed for its years.")).toBeTruthy();
    const flows = screen.getByRole("table", { name: /cash flows by year/ });
    expect(within(flows).getByRole("columnheader", { name: "Year 1" })).toBeTruthy();
    expect(within(flows).getByText("241.82")).toBeTruthy();
    const grid = screen.getByRole("table", { name: /The NPV by sales volume/ });
    expect(within(grid).getByRole("rowheader", { name: "80%" })).toBeTruthy();
    expect(screen.getByRole("rowheader", { name: "40%" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("names each field the API refuses, under the field", async () => {
    answer({
      "GET /v1/feasibility-studies": { status: 200, body: { items: [] } },
      "POST /v1/feasibility-studies": {
        status: 422,
        body: { ...problem(422, "API-INPUT-02").body as object, errors: [{ code: "STUDY-01", field: "body.salvage", message: "is more than the investment", fix: "Enter a salvage value no more than the investment" }] },
      },
    });
    renderPage(Feasibility, { path: "/planning/feasibility", session: accountant, mode: "expert" });
    await screen.findByText("Sandbox: nothing here posts");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("is more than the investment: Enter a salvage value no more than the investment")).toBeTruthy();
    expect(screen.getByLabelText("Salvage value").getAttribute("aria-invalid")).toBe("true");
  });

  it("changes a saved study with its ETag", async () => {
    answer({
      "GET /v1/feasibility-studies": { status: 200, body: { items: [{ id, version: 1, date: "2026-10-15", study: plan }] } },
      [`GET /v1/feasibility-studies/${id}`]: { status: 200, body: { id, version: 1, date: "2026-10-15", study: plan }, headers: { ETag: `"1-draft"` } },
      [`GET /v1/feasibility-studies/${id}/results`]: { status: 200, body: results },
      [`PUT /v1/feasibility-studies/${id}`]: { status: 200, body: { id, version: 2, date: "2026-10-15", study: { ...plan, max_payback: "0.2" } }, headers: { ETag: `"2-draft"` } },
    });
    renderPage(Feasibility, { path: "/planning/feasibility", session: accountant, mode: "expert" });
    fireEvent.change(await screen.findByLabelText("Study"), { target: { value: id } });
    await vi.waitFor(() => expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("One-year idea"));
    fireEvent.change(screen.getByLabelText("Longest acceptable payback"), { target: { value: "0.2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await vi.waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe(`"1-draft"`);
    expect((put.body as Record<string, unknown>).max_payback).toBe("0.2");
  });

  it("shows a fraction as a percentage without arithmetic", () => {
    expect(percent("0.2690")).toBe("26.90%");
    expect(percent("1.6600")).toBe("166.00%");
    expect(percent("-0.0500")).toBe("−5.00%");
    expect(percent("0.8", true)).toBe("80%");
    expect(percent("0.125", true)).toBe("12.5%");
    expect(percent("1", true)).toBe("100%");
  });
});
