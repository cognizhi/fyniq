import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { daysOf, Statements, stepWords } from "./Statements";

type Statement = components["schemas"]["Statement"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const reminders = {
  status: 200,
  body: {
    as_at: "2026-10-15",
    items: [
      {
        kind: "invoice",
        document_id: "v1",
        number: "INV-0012",
        customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
        recipient: "ap@acme.example",
        due_on: "2026-10-01",
        step: 7,
        days_overdue: 14,
        amount: sgd("109.00"),
        email_template: "reminder",
      },
      { kind: "quote", document_id: "q1", number: "QT-0003", customer: { id: "c1", name: "Acme Analytics Pte Ltd" }, due_on: "2026-10-17", step: -3, days_overdue: 0, amount: sgd("54.50"), email_template: "quote" },
    ],
  },
};
const groups = { status: 200, body: { standard_reminder_days: [-3, 0, 7, 30], items: [{ id: "g1", name: "Key accounts", reminder_days: [0, 14], active: true, version: 1 }] } };
const customers = { status: 200, body: { items: [{ id: "c1", legal_name: "Acme Analytics Pte Ltd", customer: true, vendor: false, active: true, version: 1 }] } };
const statement: Statement = {
  customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
  currency: "SGD",
  from: "2026-10-10",
  to: "2026-10-31",
  opening_balance: sgd("109.00"),
  lines: [
    { date: "2026-10-14", kind: "invoice", document_id: "v2", number: "INV-0003", description: "Invoice INV-0003, due 20 Oct 2026", amount: sgd("218.00"), balance: sgd("327.00") },
    { date: "2026-10-15", kind: "receipt", document_id: "r1", number: "RC-0001", description: "Payment received RC-0001", amount: sgd("-109.00"), balance: sgd("218.00") },
  ],
  balance_due: sgd("218.00"),
  overdue: sgd("218.00"),
  ageing: { not_yet_due: sgd("0.00"), overdue_1_30: sgd("218.00"), overdue_31_60: sgd("0.00"), overdue_61_90: sgd("0.00"), overdue_over_90: sgd("0.00"), unapplied_credits: sgd("0.00") },
  adds_up: true,
  currencies: ["SGD"],
};
const reads = { "GET /v1/sales-reminders": reminders, "GET /v1/customer-groups": groups, "GET /v1/contacts": customers, "GET /v1/statements": { status: 200, body: { items: [] } } };

// req: FR-AR-08
describe("Sales › Statements and reminders", () => {
  it("says a reminder's day and reads a schedule as typed", () => {
    expect(stepWords(7)).toBe("7 days after the due date");
    expect(stepWords(-3, true)).toBe("3 days before the validity date");
    expect(stepWords(0)).toBe("On the due date");
    expect(daysOf("-3, 0, 7, 30").days).toEqual([-3, 0, 7, 30]);
    expect(daysOf("").days).toEqual([]);
    expect(daysOf("7, 0").error).toBe("List each day once, earliest first, such as -3, 0, 7, 30.");
    expect(daysOf("400").error).toBe("Use -30 (before the due date) to 365 (after it).");
    expect(daysOf("1,2,3,4,5,6,7").error).toBe("Keep at most 6 reminders.");
  });

  it("lists the reminders due and the schedules; axe finds nothing", async () => {
    answer(reads);
    const { container } = renderPage(Statements, { path: "/sales/statements", session: accountant });
    const queue = (await screen.findByRole("heading", { name: "Reminders due" })).closest("section")!;
    expect(await within(queue).findByText(/7 days after the due date/)).toBeTruthy();
    expect(within(queue).getByText(/the customer has no billing email/)).toBeTruthy();
    const schedules = (await screen.findByRole("heading", { name: "Reminder schedules" })).closest("section")!;
    expect(await within(schedules).findByText("Key accounts")).toBeTruthy();
    expect(within(schedules).getByText(/3 days before the due date · On the due date · 7 days after the due date · 30 days after the due date/)).toBeTruthy();
    await noViolations(container);
  });

  it("sends a reminder due, and skips another with a reason", async () => {
    const sent = answer({
      ...reads,
      "POST /v1/sales-reminders/v1/send": { status: 200, body: { id: "d1", document_id: "v1", step: 7, outcome: "sent", send_id: "s1", decided_at: "2026-10-15T09:00:00Z" } },
      "POST /v1/sales-reminders/q1/skip": { status: 200, body: { id: "d2", document_id: "q1", step: -3, outcome: "skipped", decided_at: "2026-10-15T09:00:00Z" } },
    });
    renderPage(Statements, { path: "/sales/statements", session: accountant });
    const queue = (await screen.findByRole("heading", { name: "Reminders due" })).closest("section")!;
    await within(queue).findByText(/INV-0012/);
    const buttons = within(queue).getAllByRole("button", { name: "Send" });
    expect((buttons[1] as HTMLButtonElement).disabled).toBe(true); // no billing email
    fireEvent.click(buttons[0]!);
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/sales-reminders/v1/send")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/sales-reminders/v1/send")!.body).toEqual({ step: 7 });
    fireEvent.click(within(queue).getAllByRole("button", { name: "Skip…" })[1]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why (optional)"), { target: { value: "Asked by phone" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Skip" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/sales-reminders/q1/skip")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/sales-reminders/q1/skip")!.body).toEqual({ step: -3, reason: "Asked by phone" });
  });

  it("works out a statement that adds up, then makes and emails it", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/contacts/c1/statement": { status: 200, body: statement },
      "POST /v1/statements": { status: 201, body: { ...statement, id: "st1", number: "ST-0001", version: 1 }, headers: { ETag: `"1"` } },
      "POST /v1/statements/st1/email": { status: 200, body: { ...statement, id: "st1", number: "ST-0001", version: 1, sends: [{ id: "s1", to: "ap@acme.example" }] } },
    });
    renderPage(Statements, { path: "/sales/statements", session: accountant });
    await screen.findByRole("option", { name: "Acme Analytics Pte Ltd" });
    fireEvent.change(screen.getByLabelText("Customer"), { target: { value: "c1" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-10" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-31" } });
    fireEvent.click(screen.getByRole("button", { name: "Show it" }));
    expect(await screen.findByText("It adds up")).toBeTruthy();
    const lines = screen.getByRole("table", { name: "Statement lines" });
    expect(within(lines).getAllByRole("row")).toHaveLength(5); // heads, brought forward, 2 lines, balance due
    expect(within(lines).getAllByRole("row")[4]!.textContent).toContain("218.00");
    fireEvent.click(screen.getByRole("button", { name: "Make and email" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path === "/v1/statements/st1/email")).toBe(true));
    expect(sent.find((s) => s.method === "POST" && s.path === "/v1/statements")!.body).toEqual({ customer_id: "c1", from: "2026-10-10", to: "2026-10-31", currency: "SGD" });
    expect(sent.find((s) => s.path === "/v1/statements/st1/email")!.headers.get("If-Match")).toBe(`"1"`);
    expect(await screen.findByText(/Made ST-0001 and emailed it to ap@acme.example/)).toBeTruthy();
  });

  it("offers nothing to send or change to a person who only reads", async () => {
    answer(reads);
    renderPage(Statements, { path: "/sales/statements", session: auditor });
    const queue = (await screen.findByRole("heading", { name: "Reminders due" })).closest("section")!;
    await within(queue).findByText(/INV-0012/);
    expect(within(queue).queryByRole("button", { name: "Send" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New group" })).toBeNull();
  });
});
