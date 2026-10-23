import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { checkOrder, PurchaseOrderPage, type OrderForm } from "./PurchaseOrderPage";
import { PurchaseOrders, type Order } from "./PurchaseOrders";

const standard: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "standard", version: 1 };
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const accounts = { status: 200, body: { items: [{ id: "a6100", code: "6100", name: "Software", type: "expense", is_group: false, active: true, version: 1 }] } };
const vendors = { status: 200, body: { items: [{ id: "c1", legal_name: "Atlassian Pty Ltd", vendor: true, customer: false, active: true, version: 1 }] } };
const codes = { status: 200, body: { version: "sg-gst@2024.1", rate: "0.09", codes: [{ code: "TX", description: "Standard-rated purchase whose GST can be claimed", claimable: true }] } };
const order = (extra: Partial<Order> = {}): Order => ({
  id: "o1",
  status: "draft",
  version: 1,
  supplier: { id: "c1", name: "Atlassian Pty Ltd" },
  reference: "Q-118",
  date: "2026-10-14",
  currency: "SGD",
  lines: [
    { account_id: "a6100", description: "Licence", quantity: "10", unit_price: "250.00", gst_code: "TX", amount: sgd("2500.00"), gst: sgd("225.00"), billed: "0", on_drafts: "0", left: "10" },
  ],
  net: sgd("2500.00"),
  gst: sgd("225.00"),
  total: sgd("2725.00"),
  total_base: sgd("2725.00"),
  states: { approval: "draft", billing: "not_billed", closed: false },
  checks: [],
  bills: [],
  ...extra,
});
const read = (o: Order) => ({ status: 200, body: o, headers: { ETag: `"${o.version}-${o.status}"` } });
const reads = { "GET /v1/accounts": accounts, "GET /v1/contacts": vendors, "GET /v1/purchases/gst-codes": codes };

// req: FR-AP-01
describe("purchase orders", () => {
  it("lists the orders with their totals and states, behind the Orders tab; axe finds nothing", async () => {
    answer({
      "GET /v1/purchase-orders": {
        status: 200,
        body: {
          items: [
            order({ id: "o1", number: "PO-0002", status: "approved", states: { approval: "approved", billing: "part_billed", closed: false } }),
            order({ id: "o2", states: { approval: "awaiting_approval", billing: "not_billed", closed: false } }),
          ],
        },
      },
    });
    const { container } = renderPage(PurchaseOrders, { path: "/purchases/orders", session: accountant });
    const table = await screen.findByRole("table", { name: "Purchase orders" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("PO-0002");
    expect(rows[1]!.textContent).toContain("Part billed");
    expect(rows[2]!.textContent).toContain("Awaiting approval");
    expect(screen.getByText(/1 order shown is waiting for approval/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Orders" }).getAttribute("aria-current")).toBe("page");
    await noViolations(container);
  });

  it("says what to fix before saving, every field at once", () => {
    const f: OrderForm = { supplier_id: "", reference: "", date: "2026-10-14", expected_on: "", currency: "SGD", notes: "", lines: [{ key: "l1", account_id: "", description: "x", quantity: "0", unit_price: "1.1234567", gst_code: "" }] };
    const { draft, errors } = checkOrder(f, "SGD");
    expect(draft).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["l1.account", "l1.gst_code", "l1.quantity", "l1.unit_price"]);
    f.lines = [{ key: "l1", account_id: "a6100", description: "Licence", quantity: "2.5", unit_price: "1,850.00", gst_code: "TX" }];
    expect(checkOrder(f, "SGD").draft?.lines?.[0]).toMatchObject({ quantity: "2.5", unit_price: "1850.00" });
  });

  it("asks for approval above the threshold", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/purchase-orders/o1": read(order()),
      "POST /v1/purchase-orders/o1/approve": problem(409, "PO-14", "The order's total is 2725.00 SGD, above the self-approval threshold of 2000.00."),
      "POST /v1/purchase-orders/o1/approval": { status: 202, body: { id: "ap1" } },
    });
    renderPage(PurchaseOrderPage, { path: "/purchases/orders/o1", route: "/purchases/orders/$orderId", session: accountant, organisation: standard });
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
    expect(await screen.findByText("Another person approves this order")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/purchase-orders/o1/approval" && s.headers.get("If-Match") === `"1-draft"`)).toBe(true));
  });

  it("shows an approved order's lines as billed, and makes a bill of some of a line", async () => {
    const approved = order({ number: "PO-0002", status: "approved", version: 1, states: { approval: "approved", billing: "part_billed", closed: false },
      lines: [{ ...order().lines[0]!, billed: "4", left: "6" }], bills: [{ id: "b9", number: "BILL-0012", status: "posted", date: "2026-10-20" }] });
    answer({
      ...reads,
      "GET /v1/purchase-orders/o1": read(approved),
      "POST /v1/purchase-orders/o1/bill": { status: 201, body: { id: "b10" } },
      "GET /v1/bills/b10": { status: 404, body: {} },
    });
    renderPage(PurchaseOrderPage, { path: "/purchases/orders/o1", route: "/purchases/orders/$orderId", session: accountant, organisation: standard });
    const table = await screen.findByRole("table", { name: "What is ordered and billed" });
    expect(within(table).getAllByRole("row")[1]!.textContent).toContain("6");
    expect(screen.getByRole("link", { name: "BILL-0012" }).getAttribute("href")).toBe("/purchases/bills/b9");
    fireEvent.click(screen.getByRole("button", { name: "Make a bill…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Licence (6 left)"), { target: { value: "2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Make a bill" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/purchase-orders/o1/bill")?.body).toEqual({ lines: [{ line: 1, quantity: "2" }] }));
  });
});
