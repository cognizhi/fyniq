import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BadDebts } from "../sales/BadDebts";
import { FilingPack } from "./FilingPack";

const taxAgent = { ...ownerSession, roles: ["tax_agent"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const paper = { ya: 2027, from: "2026-01-01", to: "2026-12-31", revenue: "1000.00", profit_before_tax: "1000.00", adjustments: "400.00", chargeable_income: "1400.00", loss_forward: "0.00",
  exempt: "1050.00", normal_chargeable: "350.00", rate: "0.17", tax_before_rebate: "59.50", rebate: "0.00", tax_payable: "59.50", tax_payable_rounded: "59.50", form: "C-S (Lite)",
  optional_form: "C-S", cit_pack: "sg-cit@2026.1", forms_pack: "sg-tax-filings@2026.1" };

// req: FR-TAX-06
// req: ENG-33
// req: QA-UI-04
describe("the year-end tax working paper", () => {
  it("works a year out as the server answers it, sends the adjustments with their reasons, and lists the updates waiting; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/statutory-packs/updates": { status: 200, body: { items: [{ pack: "sg-gst", current: "sg-gst@2026.2", available: "sg-gst@2026.3", changes: [{ table: "limits", kind: "added", key: "registration_threshold", was: "", now: "registration_threshold|2026-11-07" }] }] } },
      "POST /v1/tax/working-paper": { status: 200, body: paper },
    });
    renderPage(FilingPack, { path: "/compliance/filing-packs", route: "/compliance/filing-packs", session: taxAgent });
    expect(await screen.findByText(/sg-gst: sg-gst@2026.2 to sg-gst@2026.3, 1 change/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Financial year ends"), { target: { value: "2026-12-31" } });
    fireEvent.click(screen.getByRole("button", { name: "Add an adjustment" }));
    fireEvent.change(screen.getByLabelText("Adjustment 1 amount (negative takes off)"), { target: { value: "400.00" } });
    fireEvent.change(screen.getByLabelText("Adjustment 1 reason"), { target: { value: "Fines" } });
    fireEvent.click(screen.getByRole("button", { name: "Work it out" }));
    expect(await screen.findByText("Year of Assessment 2027")).toBeTruthy();
    expect(screen.getByText(/Form C-S \(Lite\), or Form C-S, for revenue of S\$1,000.00/)).toBeTruthy();
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/tax/working-paper")).toBe(true));
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ financial_year_end: "2026-12-31", scheme: "pte", cash_grant_eligible: false,
      adjustments: [{ kind: "non_deductible", amount: "400.00", reason: "Fines" }] });
    await noViolations(document.body);
  });

  it("gives a reader the updates and no form", async () => {
    answer({ "GET /v1/statutory-packs/updates": { status: 200, body: { items: [] } } });
    renderPage(FilingPack, { path: "/compliance/filing-packs", route: "/compliance/filing-packs", session: auditor });
    expect(await screen.findByText(/Every statutory pack you have activated is the newest in force/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Work it out" })).toBeNull();
  });
});

// req: FR-AR-11
// req: QA-UI-04
describe("the bad debt relief queue", () => {
  it("shows each debt with the GST in it and the dates the server gives; axe finds nothing", async () => {
    answer({
      "GET /v1/tax/bad-debt-relief": { status: 200, body: { today: "2026-10-15", items: [{ write_off_id: "w-1", invoice_id: "i-1", invoice_number: "INV-0001", supply_date: "2026-10-14", written_off_on: "2026-10-31",
        written_off: { amount: "10.00", currency: "SGD" }, gst: { amount: "0.83", currency: "SGD" }, eligible_from: "2027-10-14", claim_by: "2031-10-13", status: "not_yet", pack: "sg-tax-filings@2026.1" }] } },
    });
    renderPage(BadDebts, { path: "/sales/bad-debts", route: "/sales/bad-debts", session: taxAgent });
    expect(await screen.findByText("INV-0001")).toBeTruthy();
    expect(screen.getByText("Not yet")).toBeTruthy();
    expect(screen.getByText(/SGD 0.83/)).toBeTruthy();
    await noViolations(document.body);
  });
});
