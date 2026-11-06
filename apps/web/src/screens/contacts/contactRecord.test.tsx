import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ContactRecord } from "./ContactRecord";

const both = { id: "c1", legal_name: "Northwind Analytics Pte Ltd", customer: true, vendor: true, active: true, uen_or_tax_id: "202312345K", possible_duplicates: [], version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const row = { currency: "SGD", current: sgd("0.00"), days_1_30: sgd("100.00"), days_31_60: sgd("0.00"), days_61_90: sgd("0.00"), days_90_plus: sgd("0.00"), credits: sgd("0.00"), total: sgd("100.00"), carrying: sgd("100.00") };
const summary = {
  as_at: "2026-11-06",
  receivable: [row],
  payable: [{ ...row, days_1_30: sgd("0.00"), current: sgd("218.00"), total: sgd("218.00"), carrying: sgd("218.00") }],
  year: { from: "2026-01-01", to: "2026-12-31" },
  invoiced: sgd("100.00"),
  billed: sgd("218.00"),
  timeline: [
    { type: "bill", id: "b1", number: "AU-100", date: "2026-11-01", status: "posted", amount: sgd("218.00") },
    { type: "invoice", id: "i1", number: "INV-0012", date: "2026-10-01", status: "posted", amount: sgd("100.00") },
    { type: "customer_receipt", id: "r1", status: "draft" },
  ],
};

// req: FR-CT-02
describe("ContactRecord", () => {
  it("shows what the contact owes and is owed, the year's totals, the ageing and the documents; axe finds nothing", async () => {
    answer({ "GET /v1/contacts/c1": { status: 200, body: both }, "GET /v1/contacts/c1/summary": { status: 200, body: summary } });
    renderPage(ContactRecord, { path: "/contacts/c1", route: "/contacts/$contactId" });
    expect(await screen.findByRole("heading", { level: 1, name: "Northwind Analytics Pte Ltd" })).toBeTruthy();
    const tiles = await screen.findByRole("group", { name: "What this contact stands at" });
    expect(within(tiles).getByText("Owed to us").parentElement?.textContent).toContain("100.00 SGD");
    expect(within(tiles).getByText("We owe").parentElement?.textContent).toContain("218.00 SGD");
    expect(within(tiles).getByText("Invoiced this financial year").parentElement?.textContent).toContain("100.00 SGD");
    expect(within(tiles).getByText("Billed this financial year").parentElement?.textContent).toContain("218.00 SGD");
    expect(within(screen.getByRole("table", { name: "What they owe us" })).getAllByText("100.00")).toHaveLength(2);
    expect(within(screen.getByRole("table", { name: "What we owe them" })).getAllByText("218.00")).toHaveLength(2);
    const activity = screen.getByRole("list", { name: "Activity" });
    expect(within(activity).getByRole("link", { name: "Bill AU-100" }).getAttribute("href")).toBe("/purchases/bills/b1");
    expect(within(activity).getByRole("link", { name: "Invoice INV-0012" }).getAttribute("href")).toBe("/sales/invoices/i1");
    expect(within(activity).getByText("Receipt")).toBeTruthy();
    expect(within(activity).getByText(/No date yet · draft/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("leaves out a side the person does not view, and says when there is nothing yet", async () => {
    answer({
      "GET /v1/contacts/c1": { status: 200, body: { ...both, vendor: false } },
      "GET /v1/contacts/c1/summary": { status: 200, body: { as_at: "2026-11-06", receivable: [], payable: [], invoiced: sgd("0.00"), timeline: [] } },
    });
    renderPage(ContactRecord, { path: "/contacts/c1", route: "/contacts/$contactId" });
    const tiles = await screen.findByRole("group", { name: "What this contact stands at" });
    expect(within(tiles).getByText("Owed to us").parentElement?.textContent).toContain("Nothing");
    expect(within(tiles).queryByText("We owe")).toBeNull();
    expect(within(tiles).queryByText("Billed this financial year")).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getByText("No documents yet.")).toBeTruthy();
  });

  it("says when the contact cannot be read", async () => {
    answer({ "GET /v1/contacts/c1": problem(404, "NOT-FOUND", "No such contact.", "Pick one from Contacts."), "GET /v1/contacts/c1/summary": problem(404, "NOT-FOUND") });
    renderPage(ContactRecord, { path: "/contacts/c1", route: "/contacts/$contactId" });
    expect(await screen.findByText("The contact could not be read")).toBeTruthy();
  });
});
