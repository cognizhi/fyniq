import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen, type Answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { checkRecurring, RecurringJournalPage, type RecurringForm } from "./RecurringJournalPage";
import { ordinal, RecurringJournals, repeats } from "./RecurringJournals";
import { newLine } from "./lines";

type Recurring = components["schemas"]["RecurringJournal"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const accounts = {
  items: [
    { id: "a-rent", code: "6100", name: "Rent", type: "expense", is_group: false, active: true, version: 1 },
    { id: "a-accr", code: "2100", name: "Accruals", type: "liability", is_group: false, active: true, version: 1 },
  ],
};
const rent = (extra: Partial<Recurring> = {}): Recurring => ({
  id: "r1",
  name: "Office rent accrual",
  status: "active",
  schedule: { every_months: 1, day_of_month: 31, starts_on: "2026-10-01" },
  next_on: "2026-10-31",
  memo: "Office rent",
  lines: [
    { account_id: "a-rent", amount: { amount: "400.00", currency: "SGD" } },
    { account_id: "a-accr", amount: { amount: "-400.00", currency: "SGD" } },
  ],
  reverses: true,
  debits: [{ amount: "400.00", currency: "SGD" }],
  version: 1,
  content_version: 1,
  edited_by: { kind: "user", id: "u1", name: "Ah Kow" },
  edited_at: "2026-10-21T01:00:00Z",
  checks: [{ severity: "ok", code: "RECURRING-OK", title: "OK", detail: "Its next draft is made on 2026-10-31." }],
  ...extra,
});

// req: FR-GL-02
// req: FR-AR-07
describe("recurring journals", () => {
  it("says when a schedule recurs, in words", () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 31].map(ordinal)).toEqual(["1st", "2nd", "3rd", "4th", "11th", "12th", "13th", "21st", "22nd", "23rd", "31st"]);
    expect(repeats({ every_months: 1, day_of_month: 1, starts_on: "2026-10-01" })).toBe("Monthly on the 1st");
    expect(repeats({ every_months: 3, day_of_month: 31, starts_on: "2026-10-01" })).toBe("Every 3 months on the 31st (the last day of a shorter month)");
    expect(repeats({ every_months: 12, day_of_month: 15, starts_on: "2026-10-01" })).toBe("Yearly on the 15th");
  });

  it("checks the form before it is sent: every field at once", () => {
    const f: RecurringForm = { name: " ", every_months: "1", day_of_month: "32", starts_on: "2026-11-01", ends_on: "2026-10-01", memo: "", reverses: false, lines: [newLine("SGD")] };
    const { input, errors } = checkRecurring(f, "SGD");
    expect(input).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["day_of_month", "ends_on", "lines", "name"]);
  });

  it("lists them with their schedule, next draft, total and status, behind the Recurring tab; axe finds nothing", async () => {
    answer({
      "GET /v1/recurring-journals": {
        status: 200,
        body: {
          items: [
            rent(),
            rent({
              id: "r2",
              name: "Software subscription",
              status: "paused",
              schedule: { every_months: 3, day_of_month: 1, starts_on: "2026-09-01", ends_on: "2027-08-31" },
              next_on: "2026-12-01",
              checks: [{ severity: "error", code: "LEDGER-06", title: "The account is inactive", detail: "account 6110 is inactive" }, { severity: "ok", code: "RECURRING-PAUSED", title: "Paused", detail: "It is paused." }],
            }),
          ],
        },
      },
    });
    renderPage(RecurringJournals, { path: "/accounting/journals/recurring", session: accountant });
    const table = await screen.findByRole("table", { name: "Recurring journals" });
    expect(within(table).getByRole("link", { name: "Office rent accrual" }).getAttribute("href")).toBe("/accounting/journals/recurring/r1");
    expect(within(table).getByText("Monthly on the 31st (the last day of a shorter month)")).toBeTruthy();
    expect(within(table).getByText("Until 31 Aug 2027")).toBeTruthy();
    expect(within(table).getByText("Paused")).toBeTruthy();
    expect(within(table).getByText("The account is inactive")).toBeTruthy(); // req: C.1 #29
    expect(screen.getByRole("link", { name: "Recurring" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "New recurring journal" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("checks a new one without saving, then saves it", async () => {
    answer({
      "GET /v1/accounts": { status: 200, body: accounts },
      "POST /v1/recurring-journals": (_b, req): Answer =>
        req.headers.get("Prefer") === "return=preview"
          ? { status: 200, body: rent({ next_on: "2026-11-30" }), headers: { "Preference-Applied": "return=preview" } }
          : { status: 201, body: rent(), headers: { ETag: '"1"' } },
    });
    renderPage(RecurringJournalPage, { path: "/accounting/journals/recurring/new", session: accountant });
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "Office rent accrual" } });
    fireEvent.change(screen.getByLabelText("On day"), { target: { value: "31" } });
    await screen.findAllByRole("option", { name: "6100 Rent" });
    const [account1, account2] = screen.getAllByLabelText(/Account$/) as [HTMLElement, HTMLElement];
    fireEvent.change(account1, { target: { value: "a-rent" } });
    fireEvent.change(screen.getAllByLabelText(/Debit$/)[0] as HTMLElement, { target: { value: "400.00" } });
    fireEvent.change(account2, { target: { value: "a-accr" } });
    fireEvent.change(screen.getAllByLabelText(/Credit$/)[1] as HTMLElement, { target: { value: "400.00" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Check" })[0] as HTMLElement);
    expect(await screen.findByText("Its next draft would be made on 30 Nov 2026. Nothing was saved.")).toBeTruthy();
    const checked = seen.find((s) => s.method === "POST" && s.path === "/v1/recurring-journals");
    expect(checked?.headers.get("Prefer")).toBe("return=preview");
    expect(checked?.body).toMatchObject({ name: "Office rent accrual", schedule: { every_months: 1, day_of_month: 31 }, lines: [{ amount: { amount: "400.00" } }, { amount: { amount: "-400.00" } }] });
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0] as HTMLElement);
    await waitFor(() => expect(seen.filter((s) => s.method === "POST" && s.path === "/v1/recurring-journals" && s.headers.get("Idempotency-Key")).length).toBe(2));
  });

  it("shows one with what would stop its drafts posting, and pauses it", async () => {
    answer({
      "GET /v1/accounts": { status: 200, body: accounts },
      "GET /v1/recurring-journals/r1": {
        status: 200,
        headers: { ETag: '"2"' },
        body: rent({
          version: 2,
          last_on: "2026-09-30",
          last_journal_id: "j9",
          checks: [{ severity: "warning", code: "RECURRING-DUE", title: "Due but not created", detail: "Its draft for 2026-10-31 is not made yet.", fix: "The scheduler makes it within the hour" }],
        }),
      },
      "POST /v1/recurring-journals/r1/pause": { status: 200, body: rent({ status: "paused", version: 3 }) },
    });
    renderPage(RecurringJournalPage, { path: "/accounting/journals/recurring/r1", route: "/accounting/journals/recurring/$recurringId", session: accountant });
    expect(await screen.findByText("1 thing to look at before its drafts can post")).toBeTruthy(); // req: C.1 #28
    expect(screen.getByRole("link", { name: "open the journal" }).getAttribute("href")).toBe("/accounting/journals/j9");
    fireEvent.click(screen.getAllByRole("button", { name: "Pause" })[0] as HTMLElement);
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/recurring-journals/r1/pause" && s.headers.get("If-Match") === '"2"')).toBe(true));
    expect(await screen.findByText("Paused: it makes no drafts until you resume it.")).toBeTruthy();
  });
});
