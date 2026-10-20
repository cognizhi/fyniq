import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Contacts } from "./Contacts";
import { Items } from "./Items";

const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const northwind = { id: "c1", legal_name: "Northwind Analytics Pte Ltd", customer: true, vendor: false, active: true, uen_or_tax_id: "202312345K", billing_email: "ap@northwind.example", payment_terms_days: 30, possible_duplicates: [], version: 1 };
const aws = { id: "c2", legal_name: "Amazon Web Services Singapore", customer: false, vendor: true, active: true, possible_duplicates: [{ id: "c3", legal_name: "AWS Singapore", same_name: false, same_uen: true, same_email: false }], version: 1 };
const gone = { ...northwind, id: "c4", legal_name: "Old Client", active: false };

// req: FR-CT-01
// req: FR-CT-04
describe("Contacts", () => {
  it("lists customers and vendors, filters by role, and flags a possible duplicate; axe finds nothing", async () => {
    answer({ "GET /v1/contacts": { status: 200, body: { items: [northwind, aws, gone] } } });
    renderPage(Contacts, { path: "/contacts", session: bookkeeper });
    const list = await screen.findByRole("list", { name: "Contacts shown" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(within(list).getByText("Possible duplicate")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: /Vendors/ }));
    expect(within(screen.getByRole("list", { name: "Contacts shown" })).getAllByRole("listitem")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: /^All/ }));
    fireEvent.change(screen.getByLabelText("Find a contact"), { target: { value: "2023123" } });
    expect(within(screen.getByRole("list", { name: "Contacts shown" })).getByText("Northwind Analytics Pte Ltd")).toBeTruthy();
  });

  it("checks for duplicates before adding, and adds it anyway once seen", async () => {
    const seen = answer({
      "GET /v1/contacts": { status: 200, body: { items: [northwind] } },
      "POST /v1/contacts": (_b: unknown, req: Request) =>
        req.headers.get("Prefer") ? { status: 200, body: { ...northwind, id: "new", possible_duplicates: [{ id: "c1", legal_name: northwind.legal_name, same_name: true, same_uen: false, same_email: false }] } } : { status: 201, body: { ...northwind, id: "c9" } },
    });
    renderPage(Contacts, { path: "/contacts", session: bookkeeper });
    fireEvent.click(await screen.findByRole("button", { name: "New contact" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Legal name"), { target: { value: "Northwind Analytics Pte. Ltd." } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the contact" }));
    expect(await within(dialog).findByText(/It may be a contact you have already/)).toBeTruthy();
    expect(seen.filter((s) => s.method === "POST")).toHaveLength(1);
    fireEvent.click(within(dialog).getByRole("button", { name: "Add it anyway" }));
    expect(await screen.findByText("Northwind Analytics Pte. Ltd. added.")).toBeTruthy();
    const posts = seen.filter((s) => s.method === "POST");
    expect(posts[1]?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(posts[1]?.body).toEqual({ legal_name: "Northwind Analytics Pte. Ltd.", customer: true, vendor: false, country: "SG", currency: "SGD" });
  });

  it("needs a role, and clears payment terms with -1 over the contact's version", async () => {
    const seen = answer({
      "GET /v1/contacts": { status: 200, body: { items: [northwind] } },
      "GET /v1/contacts/c1": { status: 200, body: northwind, headers: { ETag: '"1"' } },
      "PATCH /v1/contacts/c1": { status: 200, body: { ...northwind, version: 2 } },
    });
    renderPage(Contacts, { path: "/contacts", session: bookkeeper });
    fireEvent.click(await screen.findByRole("button", { name: "Change Northwind Analytics Pte Ltd" }));
    const dialog = await screen.findByRole("dialog");
    const customer = await within(dialog).findByRole("checkbox", { name: "A customer" });
    fireEvent.click(customer);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the contact" }));
    expect(await within(dialog).findByText(/Mark it as a customer, a vendor or both/)).toBeTruthy();
    fireEvent.click(customer);
    fireEvent.change(within(dialog).getByLabelText("Payment terms (days)"), { target: { value: "" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the contact" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/contacts/c1"));
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.body).toEqual({ payment_terms_days: -1 });
    expect(patch?.headers.get("If-Match")).toBe('"1"');
  });
});

const consult = { id: "i1", code: "CONSULT", name: "Consulting", type: "service", unit: "hour", gst_code: "SR", revenue_account_id: "r1", prices: [{ currency: "SGD", unit_price: "180.00" }], active: true, version: 1 };

// req: FR-ITM-01
describe("Items and prices", () => {
  it("lists items with their prices as decimals, and adds one, checking each price", async () => {
    const seen = answer({
      "GET /v1/items": { status: 200, body: { items: [consult] } },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "r1", code: "4020", name: "Services revenue", type: "revenue", active: true, group: false, control: false, money_account: false, payroll_cost: false, parent_id: null, version: 1 }] } },
      "POST /v1/items": { status: 201, body: { ...consult, id: "i2", code: "SUB" } },
    });
    renderPage(Items, { path: "/items", session: bookkeeper });
    expect(await screen.findByText("SGD 180.00")).toBeTruthy();
    expect(await screen.findByText("4020 Services revenue")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "New item" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "SUB" } });
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Subscription" } });
    fireEvent.change(within(dialog).getByLabelText("Unit price 1"), { target: { value: "1,200" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the item" }));
    expect(await within(dialog).findByText(/plain decimal with up to 6 decimals/)).toBeTruthy();
    expect(calls()).not.toContain("POST /v1/items");
    fireEvent.change(within(dialog).getByLabelText("Unit price 1"), { target: { value: "1200.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a price in another currency" }));
    fireEvent.change(within(dialog).getByLabelText("Currency 2"), { target: { value: "usd" } });
    fireEvent.change(within(dialog).getByLabelText("Unit price 2"), { target: { value: "900" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the item" }));
    expect(await screen.findByText("SUB Subscription added.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ code: "SUB", name: "Subscription", type: "service", prices: [{ currency: "SGD", unit_price: "1200.00" }, { currency: "USD", unit_price: "900" }] });
  });
});
