import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { csvCell } from "../../format/csv";
import { csvOf, GeneralLedger } from "./GeneralLedger";

type Account = components["schemas"]["Account"];
type Report = components["schemas"]["GeneralLedgerReport"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const m = (amount: string) => ({ amount, currency: "SGD" });
const bank = { id: "a-bank", code: "1010", name: "DBS current account", type: "asset", active: true, group: false, control: false, money_account: true, payroll_cost: false, version: 1 } as Account;
const report: Report = {
  from: "2026-10-01",
  to: "2026-10-31",
  currency: "SGD",
  ledger_revision: 7,
  accounts: [
    {
      account_id: "a-bank",
      code: "1010",
      name: "DBS current account",
      opening: m("1000.00"),
      debits: m("50.00"),
      credits: m("1200.00"),
      closing: m("-150.00"),
      lines: [
        { posting_date: "2026-10-20", entry_id: "e1", ledger_revision: 5, source_type: "manual_journal", source_id: "j1", source_version: 1, number: "JNL-0001", reversal: false, memo: "=October rent", amount: m("-1200.00"), base: m("-1200.00"), balance: m("-200.00") },
        { posting_date: "2026-10-25", entry_id: "e2", ledger_revision: 6, source_type: "opening_balance", source_id: "ob", source_version: 1, reversal: false, memo: "Opening", amount: { amount: "37.04", currency: "USD" }, base: m("50.00"), balance: m("-150.00") },
      ],
    },
  ],
};

afterEach(() => vi.restoreAllMocks());

// req: FR-GL-11
// req: FR-UX-20
describe("the general ledger's export", () => {
  it("writes plain decimals and makes formulas inert", () => {
    expect(csvCell("-1234.56", true)).toBe("-1234.56");
    expect(csvCell("=SUM(A1)")).toBe("'=SUM(A1)");
    expect(csvCell("a, b")).toBe('"a, b"');
    expect(csvCell('say "hi"')).toBe('"say ""hi"""');
    const lines = csvOf(report).trim().split("\r\n");
    expect(lines[0]).toBe("Account,Name,Date,Number,Source,Memo,Description,Debit,Credit,Balance");
    expect(lines[1]).toBe("1010,DBS current account,2026-10-01,,,Opening balance,,,,1000.00");
    expect(lines[2]).toBe("1010,DBS current account,2026-10-20,JNL-0001,manual_journal,'=October rent,,,1200.00,-200.00");
    expect(lines[4]).toBe("1010,DBS current account,2026-10-31,,,Closing balance,,50.00,1200.00,-150.00");
  });
});

// req: FR-GL-11
describe("the general ledger page", () => {
  it("reads the accounts chosen over two dates, with the source of each line; axe finds nothing", async () => {
    answer({ "GET /v1/accounts": { status: 200, body: { items: [bank] } }, "GET /v1/reports/general-ledger": { status: 200, body: report } });
    renderPage(GeneralLedger, { path: "/reports/general-ledger", session: accountant });
    expect(await screen.findByRole("heading", { name: "Choose an account" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-31" } });
    fireEvent.change(await screen.findByLabelText("Add an account"), { target: { value: "a-bank" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    const table = await screen.findByRole("table", { name: /Ledger lines for account 1010/ });
    expect(within(table).getByText("Opening balance, 1 Oct 2026")).toBeTruthy();
    expect(within(table).getByRole("link", { name: "JNL-0001" }).getAttribute("href")).toBe("/accounting/journals/j1");
    expect(within(table).getByRole("link", { name: "Opening" }).getAttribute("href")).toBe("/accounting/opening-balances");
    expect(within(table).getByText("(37.04 USD)")).toBeTruthy();
    expect(within(table).getAllByText("−150.00")).toHaveLength(2); // the last line's balance and the closing balance
    expect(screen.getByText(/Read at ledger revision 7/)).toBeTruthy();
    const q = seen.find((s) => s.path === "/v1/reports/general-ledger")!.query;
    expect(q).toContain("account_id=a-bank");
    expect(q).toContain("from=2026-10-01");
    expect(q).toContain("to=2026-10-31");
    await noViolations(document.body);
    const create = vi.fn(() => "blob:x");
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() });
    fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
    expect(create).toHaveBeenCalledTimes(1);
  });
});
