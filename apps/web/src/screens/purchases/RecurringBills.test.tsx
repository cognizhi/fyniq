import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen, type Answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { newLine } from "./BillLines";
import { checkRecurringBill, RecurringBillPage, type RecurringBillForm } from "./RecurringBillPage";
import { RecurringBills, type RecurringBill } from "./RecurringBills";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const accounts = { items: [{ id: "a6100", code: "6100", name: "Rent", type: "expense", is_group: false, active: true, version: 1, default_gst_code: "TX" }] };
const vendors = { items: [{ id: "c1", legal_name: "Landlord Pte Ltd", vendor: true, customer: false, active: true, version: 1 }] };
const codes = { version: "sg-gst@2024.1", rate: "0.09", codes: [{ code: "TX", description: "Standard-rated purchase whose GST can be claimed", claimable: true }] };
const rent = (extra: Partial<RecurringBill> = {}): RecurringBill => ({
  id: "rb1",
  name: "Office rent",
  status: "active",
  schedule: { every_months: 1, day_of_month: 1, starts_on: "2026-11-01" },
  next_on: "2026-11-01",
  supplier: { id: "c1", name: "Landlord Pte Ltd" },
  currency: "SGD",
  lines: [{ account_id: "a6100", description: "Unit 05-12", gst_code: "TX", net: { amount: "1500.00", currency: "SGD" }, gst: { amount: "135.00", currency: "SGD" } }],
  notes: "Monthly rent",
  total: { amount: "1635.00", currency: "SGD" },
  version: 1,
  content_version: 1,
  edited_by: { kind: "user", id: "u1", name: "Ah Kow" },
  edited_at: "2026-10-23T01:00:00Z",
  checks: [{ severity: "ok", code: "RECURRING-OK", title: "OK", detail: "Its next draft is made on 2026-11-01." }],
  ...extra,
});

// req: FR-AP-09
describe("recurring bills", () => {
  it("checks the form before it is sent: every field at once", () => {
    const f: RecurringBillForm = { name: " ", every_months: "1", day_of_month: "32", starts_on: "2026-11-01", ends_on: "2026-10-01", supplier_id: "", currency: "SGD", notes: "", lines: [newLine()] };
    const { input, errors } = checkRecurringBill(f, "SGD");
    expect(input).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["day_of_month", "ends_on", "lines", "name"]);
  });

  it("lists them with their schedule, next draft, total and status, behind the Recurring tab; axe finds nothing", async () => {
    answer({
      "GET /v1/recurring-bills": {
        status: 200,
        body: {
          items: [
            rent(),
            rent({
              id: "rb2",
              name: "Cleaning",
              status: "paused",
              schedule: { every_months: 3, day_of_month: 31, starts_on: "2026-09-01", ends_on: "2027-08-31" },
              checks: [{ severity: "error", code: "BILL-06", title: "Not a GST code for purchases", detail: "\"SR\" is not a GST code for purchases" }],
            }),
          ],
        },
      },
    });
    renderPage(RecurringBills, { path: "/purchases/bills/recurring", session: accountant });
    const table = await screen.findByRole("table", { name: "Recurring bills" });
    expect(within(table).getByRole("link", { name: "Office rent" }).getAttribute("href")).toBe("/purchases/bills/recurring/rb1");
    expect(within(table).getAllByText("Landlord Pte Ltd").length).toBe(2);
    expect(within(table).getByText("Every 3 months on the 31st (the last day of a shorter month)")).toBeTruthy();
    expect(within(table).getAllByText("1,635.00 SGD").length).toBe(2);
    expect(within(table).getByText("Paused")).toBeTruthy();
    expect(within(table).getByText("Not a GST code for purchases")).toBeTruthy(); // req: C.1 #29
    expect(screen.getByRole("link", { name: "Recurring" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "New recurring bill" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("checks a new one without saving, then saves it", async () => {
    answer({
      "GET /v1/accounts": { status: 200, body: accounts },
      "GET /v1/contacts": { status: 200, body: vendors },
      "GET /v1/purchases/gst-codes": { status: 200, body: codes },
      "POST /v1/recurring-bills": (_b, req): Answer =>
        req.headers.get("Prefer") === "return=preview"
          ? { status: 200, body: rent(), headers: { "Preference-Applied": "return=preview" } }
          : { status: 201, body: rent(), headers: { ETag: '"1"' } },
    });
    renderPage(RecurringBillPage, { path: "/purchases/bills/recurring/new", session: accountant });
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "Office rent" } });
    await screen.findByRole("option", { name: "Landlord Pte Ltd" });
    fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "c1" } });
    fireEvent.change(screen.getByLabelText("Starts on"), { target: { value: "2026-11-01" } });
    await screen.findByRole("option", { name: /6100 Rent/ });
    fireEvent.change(screen.getByLabelText("What it is for"), { target: { value: "a6100" } });
    await screen.findByRole("option", { name: /^TX:/ });
    fireEvent.change(screen.getByLabelText("Amount before GST (SGD)"), { target: { value: "1,500.00" } });
    fireEvent.change(screen.getByLabelText("GST as stated (SGD)"), { target: { value: "135.00" } });
    expect(screen.queryByLabelText("Spread over (months, optional)")).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "Check" })[0] as HTMLElement);
    expect(await screen.findByText("Its next draft would be made on 1 Nov 2026. Nothing was saved.")).toBeTruthy();
    const checked = seen.find((s) => s.method === "POST" && s.path === "/v1/recurring-bills");
    expect(checked?.headers.get("Prefer")).toBe("return=preview");
    expect(checked?.body).toMatchObject({
      name: "Office rent",
      schedule: { every_months: 1, day_of_month: 1, starts_on: "2026-11-01" },
      supplier_id: "c1",
      currency: "SGD",
      lines: [{ account_id: "a6100", gst_code: "TX", net: { amount: "1500.00" }, gst: { amount: "135.00" } }],
    });
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0] as HTMLElement);
    await waitFor(() => expect(seen.filter((s) => s.method === "POST" && s.path === "/v1/recurring-bills" && s.headers.get("Idempotency-Key")).length).toBe(2));
  });

  it("shows one with its last draft and what would stop its drafts posting, and pauses it", async () => {
    answer({
      "GET /v1/accounts": { status: 200, body: accounts },
      "GET /v1/contacts": { status: 200, body: vendors },
      "GET /v1/purchases/gst-codes": { status: 200, body: codes },
      "GET /v1/recurring-bills/rb1": {
        status: 200,
        headers: { ETag: '"2"' },
        body: rent({
          version: 2,
          last_on: "2026-10-01",
          last_bill_id: "b9",
          checks: [{ severity: "warning", code: "RECURRING-DUE", title: "Due but not created", detail: "Its draft for 2026-11-01 is not made yet.", fix: "The scheduler makes it within the hour" }],
        }),
      },
      "POST /v1/recurring-bills/rb1/pause": { status: 200, body: rent({ status: "paused", version: 3 }) },
    });
    renderPage(RecurringBillPage, { path: "/purchases/bills/recurring/rb1", route: "/purchases/bills/recurring/$recurringBillId", session: accountant });
    expect(await screen.findByText("1 thing to look at before its drafts can post")).toBeTruthy(); // req: C.1 #28
    expect(screen.getByRole("link", { name: "open the bill" }).getAttribute("href")).toBe("/purchases/bills/b9");
    expect(screen.getByText("Each draft's total 1,635.00 SGD")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Pause" })[0] as HTMLElement);
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/recurring-bills/rb1/pause" && s.headers.get("If-Match") === '"2"')).toBe(true));
    expect(await screen.findByText("Paused: it makes no drafts until you resume it.")).toBeTruthy();
  });
});
