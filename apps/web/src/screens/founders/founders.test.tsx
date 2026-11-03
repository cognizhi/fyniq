import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { useState } from "react";
import { checkLines, LineEditor, newLine } from "../accounting/lines";
import { cellsOf, draftOf, personKey } from "../accounting/OpeningBalances";
import type { Account } from "../accounting/data";
import { DirectorAccounts } from "./DirectorAccounts";
import { MoneyIPutIn } from "./MoneyIPutIn";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const loan = { account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" as const };
const deferred = { account_id: "acc-2120", code: "2120", name: "Deferred remuneration payable", role: "deferred-remuneration" as const };

// Mei Lin is owed S$1,100.00 on 2500 (a credit of -1100.00); Arjun owes the company S$200.00, a debit.
const mei = {
  contact_id: "c-mei",
  legal_name: "Mei Lin Tan",
  director: true,
  shareholder: true,
  member_user_id: ownerSession.user_id,
  active: true,
  balances: [{ ...deferred, balance: sgd("0.00") }, { ...loan, balance: sgd("-1100.00") }],
  net: sgd("-1100.00"),
  in_debit: false,
};
const arjun = { ...mei, contact_id: "c-arjun", legal_name: "Arjun Rao", member_user_id: undefined, balances: [{ ...deferred, balance: sgd("0.00") }, { ...loan, balance: sgd("200.00") }], net: sgd("200.00"), in_debit: true };
const list = { as_at: "2026-10-31", currency: "SGD", items: [mei, arjun], unkeyed: sgd("0.00") };
const statement = {
  contact_id: "c-mei",
  legal_name: "Mei Lin Tan",
  from: "2026-01-01",
  to: "2026-10-31",
  currency: "SGD",
  accounts: [
    { account_id: "acc-2120", code: "2120", name: "Deferred remuneration payable", opening: sgd("0.00"), by_origin: [], closing: sgd("0.00") },
    { account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", opening: sgd("0.00"), by_origin: [{ origin: "lent", amount: sgd("-1500.00") }, { origin: "paid_out", amount: sgd("400.00") }], closing: sgd("-1100.00") },
  ],
  movements: [
    { entry_id: "e-1", date: "2026-10-01", account_id: "acc-2500", origin: "lent", amount: sgd("-1500.00"), source_type: "bank_transaction", source_id: "b-1", reversal: false, memo: "Loan from Mei Lin" },
    { entry_id: "e-2", date: "2026-10-05", account_id: "acc-2500", origin: "paid_out", amount: sgd("400.00"), source_type: "manual_journal", source_id: "j-1", number: "JNL-0002", reversal: false },
  ],
};

// req: FR-EQ-01
// req: FR-EQ-11
// req: FR-EQ-14
describe("Director accounts", () => {
  it("shows each person's balances owed to them, an account in debit, and their statement by origin, and axe finds nothing", async () => {
    answer({
      "GET /v1/director-accounts": { status: 200, body: list },
      "GET /v1/director-accounts/c-mei/statement": { status: 200, body: statement },
    });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Mei Lin Tan" })).closest("section") as HTMLElement;
    expect(within(card).getByText("1,100.00")).toBeTruthy();
    expect(within(card).getByText(/The company owes Mei Lin Tan/)).toBeTruthy();
    const arjunCard = screen.getByRole("heading", { name: "Arjun Rao" }).closest("section") as HTMLElement;
    expect(within(arjunCard).getByText("Account in debit")).toBeTruthy();
    expect(within(arjunCard).getByText(/Arjun Rao owes the company/)).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Statement" }));
    const table = await screen.findByRole("table");
    expect(within(table).getByText("Lent to the company")).toBeTruthy();
    expect(within(table).getByText("1,500.00")).toBeTruthy();
    expect(within(table).getByText("Paid out to them")).toBeTruthy();
    expect(within(table).getByText(/−400.00|-400.00/)).toBeTruthy();
    await noViolations(document.body);
  });

  // req: FR-EQ-10
  it("shows the related-party schedule on demand, a minus for what a person owes, and downloads it as CSV", async () => {
    const figures = (loan: string, fees = "0.00") => ({ loan: sgd(loan), deferred_pay: sgd(fees), fees_for_year: sgd(fees), converted: sgd("0.00"), waived: sgd("0.00") });
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: list },
      "GET /v1/director-accounts/related-parties": {
        status: 200,
        body: {
          as_at: "2026-10-31",
          year_from: "2026-01-01",
          currency: "SGD",
          rows: [
            { contact_id: "c-mei", legal_name: "Mei Lin Tan", director: true, shareholder: true, figures: figures("1100.00", "800.00") },
            { contact_id: "c-arjun", legal_name: "Arjun Rao", director: true, shareholder: true, figures: figures("-200.00") },
          ],
          totals: figures("900.00", "800.00"),
        },
      },
      "GET /v1/director-accounts/related-parties/export": {
        status: 200,
        body: { export_id: "x-1", file_name: "related-parties-2026-10-31.csv", media_type: "text/csv", sha256: "ab", size_bytes: 2, rows: 2, ledger_revision: 9, content_base64: "YSw=" },
      },
    });
    Object.assign(URL, { createObjectURL: () => "blob:x", revokeObjectURL: () => undefined });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    fireEvent.click(await screen.findByRole("button", { name: "Related parties" }));
    const table = await screen.findByRole("table", { name: "Related parties as at 31 Oct 2026" });
    const arjunRow = within(table).getByText("Arjun Rao").closest("tr") as HTMLElement;
    expect(within(arjunRow).getByText(/−200.00|-200.00/)).toBeTruthy();
    expect(within(table).getByText("900.00")).toBeTruthy();
    expect(screen.getByText(/fees, conversions and waivers from 1 Jan 2026/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "CSV" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/director-accounts/related-parties/export" && x.query.includes("as_at=2026-10-31"))).toBe(true));
    await noViolations(document.body);
  });

  it("asks for a member's link over the contact's version, for another person to approve", async () => {
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: list },
      "GET /v1/members": { status: 200, body: { items: [{ member_id: "m-2", user_id: "u-arjun", display_name: "Arjun Rao", roles: [] }] } },
      "GET /v1/contacts/c-arjun": { status: 200, body: { id: "c-arjun", legal_name: "Arjun Rao", director: true, shareholder: true, customer: false, vendor: false, active: true, version: 3 }, headers: { ETag: '"3"' } },
      "PUT /v1/contacts/c-arjun/member": { status: 202, body: { id: "apr-1", object_type: "contact_member_link", status: "pending" } },
    });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Arjun Rao" })).closest("section") as HTMLElement;
    fireEvent.click(within(card).getByRole("button", { name: "Link member" }));
    const member = await screen.findByLabelText("Member");
    await screen.findByRole("option", { name: "Arjun Rao" });
    fireEvent.change(member, { target: { value: "u-arjun" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Another person who holds the approval right approves it/)).toBeTruthy();
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"3"');
    expect(put?.body).toEqual({ member_user_id: "u-arjun" });
  });

  it("offers the link and settling only to an Owner or the Controller", async () => {
    answer({ "GET /v1/director-accounts": { status: 200, body: list } });
    renderPage(DirectorAccounts, { path: "/accounting/directors", session: { ...ownerSession, roles: ["accountant"] } as Session });
    await screen.findByRole("heading", { name: "Mei Lin Tan" });
    expect(screen.queryByRole("button", { name: "Link member" })).toBeNull();
    expect(screen.queryByRole("link", { name: /Settle/ })).toBeNull();
  });

  // req: FR-EQ-05
  it("links each person's card to settling their balance", async () => {
    answer({ "GET /v1/director-accounts": { status: 200, body: list } });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Mei Lin Tan" })).closest("section") as HTMLElement;
    expect(within(card).getByRole("link", { name: /Settle/ }).getAttribute("href")).toBe("/accounting/directors/c-mei/settle");
  });
});

// req: FR-EQ-14
describe("Money I put in", () => {
  it("says what the company owes you, and axe finds nothing", async () => {
    answer({
      "GET /v1/me/director-account": { status: 200, body: { linked: true, account: mei } },
      "GET /v1/director-accounts/c-mei/statement": { status: 200, body: statement },
    });
    renderPage(MoneyIPutIn, { path: "/me/money-i-put-in", mode: "basic" });
    expect(await screen.findByRole("heading", { name: "The company owes you" })).toBeTruthy();
    expect(screen.getByText("1,100.00 SGD")).toBeTruthy();
    expect(await screen.findByText("Lent to the company")).toBeTruthy();
    await noViolations(document.body);
  });

  it("says when no director account is linked to you", async () => {
    answer({ "GET /v1/me/director-account": { status: 200, body: { linked: false } } });
    renderPage(MoneyIPutIn, { path: "/me/money-i-put-in", mode: "basic" });
    expect(await screen.findByText("No director account is linked to you")).toBeTruthy();
  });
});

// req: FR-EQ-01
// req: equity/founders-money Every line carries the person
describe("the person on a director's account's line", () => {
  const accounts = new Map<string, Account>([
    ["acc-2500", { id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" } as Account],
    ["acc-1010", { id: "acc-1010", code: "1010", name: "Bank" } as Account],
  ]);

  it("asks a journal line on 2500 for its person and origin, and sends them only there", () => {
    const bank = { ...newLine("SGD"), account_id: "acc-1010", debit: "100.00", party_id: "c-mei", origin: "lent" };
    const owed = { ...newLine("SGD"), account_id: "acc-2500", credit: "100.00" };
    const errors: Record<string, string> = {};
    checkLines([bank, owed], "SGD", errors, accounts);
    expect(errors[`${owed.key}.party`]).toMatch(/whose account/);
    expect(errors[`${owed.key}.origin`]).toMatch(/where the amount came from/);
    const fixed: Record<string, string> = {};
    const lines = checkLines([bank, { ...owed, party_id: "c-mei", origin: "lent" }], "SGD", fixed, accounts);
    expect(fixed).toEqual({});
    expect(lines[0]).not.toHaveProperty("party_id", "c-mei");
    expect(lines[1]).toMatchObject({ party_id: "c-mei", origin: "lent" });
  });

  it("keeps an opening balance per person on a director's account, with where it came from", () => {
    const saved = cellsOf({ status: "draft", version: 1, lines: [{ account_id: "acc-2500", amount: sgd("-1500.00"), party_id: "c-mei", origin: "lent" }] } as never);
    expect(saved[personKey("acc-2500", "c-mei")]).toEqual({ debit: "", credit: "1500.00", origin: "lent" });
    const missing = draftOf({ [personKey("acc-2500", "c-arjun")]: { debit: "", credit: "300.00" } }, "SGD", "");
    expect(missing.errors[personKey("acc-2500", "c-arjun")]).toMatch(/where the balance came from/);
    const ok = draftOf({ ...saved, "acc-1010": { debit: "1500.00", credit: "" } }, "SGD", "");
    expect(ok.draft?.lines).toEqual([
      { account_id: "acc-2500", amount: sgd("-1500.00"), party_id: "c-mei", origin: "lent" },
      { account_id: "acc-1010", amount: sgd("1500.00") },
    ]);
  });
});

// A journal line's account on 2500 brings its person and origin pickers, listing the directors and shareholders.
function Editor() {
  const accounts = new Map<string, Account>([["acc-2500", { id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" } as Account]]);
  const [lines, setLines] = useState([{ ...newLine("SGD"), account_id: "acc-2500" }]);
  return <LineEditor id="t" lines={lines} onChange={setLines} base="SGD" choices={[...accounts.values()]} byId={accounts} errors={{}} />;
}

// req: FR-EQ-01
describe("the journal line's pickers", () => {
  it("ask a line on 2500 whose account it is and where it came from, and axe finds nothing", async () => {
    answer({ "GET /v1/contacts": { status: 200, body: { items: [{ id: "c-mei", legal_name: "Mei Lin Tan", director: true, shareholder: true, customer: false, vendor: false, active: true, version: 1 }] } } });
    renderPage(Editor, { path: "/accounting/journals/new" });
    expect(await screen.findByRole("option", { name: "Mei Lin Tan" })).toBeTruthy();
    const origin = screen.getByLabelText(/Where it came from/);
    expect(within(origin).getByRole("option", { name: "Paid personally" })).toBeTruthy();
    await waitFor(() => noViolations(document.body));
  });
});

// req: FR-EQ-04
// req: equity/founders-money Choice recorded with the signed decision
describe("a director's pay choice", () => {
  it("shows the choice in force, drafts a new one, and records it once the signed decision is attached", async () => {
    const withChoice = { ...list, items: [{ ...mei, pay_choice: { choice: "no_salary", default: true } }] };
    const drafted = { id: "pc-1", status: "draft", version: 1, contact_id: "c-mei", legal_name: "Mei Lin Tan", from: "2026-11-01", choice: "salary_deferred", decisions: 1, checks: [] };
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: withChoice },
      "POST /v1/director-pay-choices": { status: 201, body: drafted, headers: { ETag: '"1-draft"' } },
      "GET /v1/director-pay-choices/pc-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/director-pay-choices/pc-1/record": { status: 200, body: { ...drafted, status: "recorded" } },
    });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Mei Lin Tan" })).closest("section") as HTMLElement;
    expect(within(card).getByText("Pay: No salary")).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Change pay choice" }));
    fireEvent.change(await screen.findByLabelText("From the month"), { target: { value: "2026-11" } });
    fireEvent.change(screen.getByLabelText("Pay choice"), { target: { value: "salary_deferred" } });
    expect(screen.getByText(/CPF is still due at the end of each month/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next: attach the signed decision" }));
    const record = await screen.findByRole("button", { name: "Record the pay choice" });
    fireEvent.click(record);
    expect(await screen.findByText(/Recorded Mei Lin Tan's pay choice: Salary deferred from Nov 2026/)).toBeTruthy();
    const post = seen.find((s) => s.method === "POST" && s.path === "/v1/director-pay-choices");
    expect(post?.body).toEqual({ contact_id: "c-mei", from: "2026-11-01", choice: "salary_deferred" });
    expect(seen.find((s) => s.path === "/v1/director-pay-choices/pc-1/record")?.headers.get("If-Match")).toBe('"1-draft"');
  });
});

// req: FR-EQ-04
// req: equity/founders-money A director's fee posts to the person (rev 0.167)
describe("a director's fee", () => {
  it("records the fee voted, then asks another person to approve it over its version", async () => {
    const drafted = { id: "df-1", status: "draft", version: 1, contact_id: "c-mei", legal_name: "Mei Lin Tan", entitled_on: "2026-11-30", period_from: "2026-01-01",
      period_to: "2026-12-31", amount: sgd("6000.00"), account_id: "acc-6050", resolutions: 1, checks: [], ledger_impact: [] };
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: list },
      "GET /v1/director-fees": { status: 200, body: { items: [] } },
      "POST /v1/director-fees": { status: 201, body: drafted, headers: { ETag: '"1-draft"' } },
      "GET /v1/director-fees/df-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/director-fees/df-1/post-approval": { status: 202, body: { id: "apr-9", object_type: "director_fee", status: "pending" } },
    });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Mei Lin Tan" })).closest("section") as HTMLElement;
    fireEvent.click(within(card).getByRole("button", { name: "Fees" }));
    fireEvent.change(await screen.findByLabelText("Voted on"), { target: { value: "2026-11-30" } });
    fireEvent.change(screen.getByLabelText("Fee (SGD)"), { target: { value: "6,000" } });
    fireEvent.change(screen.getByLabelText("For the period from"), { target: { value: "2026-01-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-12-31" } });
    fireEvent.click(screen.getByRole("button", { name: "Next: attach the resolution" }));
    fireEvent.click(await screen.findByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Asked for approval of Mei Lin Tan's fee/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/director-fees")?.body).toEqual({
      contact_id: "c-mei", entitled_on: "2026-11-30", period_from: "2026-01-01", period_to: "2026-12-31", amount: sgd("6000"),
    });
    expect(seen.find((s) => s.path === "/v1/director-fees/df-1/post-approval")?.headers.get("If-Match")).toBe('"1-draft"');
  });
});

// req: FR-EQ-11
// req: equity/founders-money Status recorded before the corporate profile (rev 0.167)
describe("the exempt private company status", () => {
  it("says the status in force and the server's warning on an account in debit, and records a new status", async () => {
    const warned = { ...list, exempt_status: { status: "not_recorded" }, items: [{ ...arjun, debit_warning: "No exempt-private-company status is recorded, so the company is treated as not exempt: section 162 of the Companies Act bans loans to directors." }] };
    const drafted = { id: "ex-1", status: "draft", version: 1, from: "2025-03-14", exempt: true, evidence: 1, checks: [] };
    const seen = answer({
      "GET /v1/director-accounts": { status: 200, body: warned },
      "POST /v1/exempt-statuses": { status: 201, body: drafted, headers: { ETag: '"1-draft"' } },
      "GET /v1/exempt-statuses/ex-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/exempt-statuses/ex-1/record": { status: 200, body: { ...drafted, status: "recorded" } },
    });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    expect(await screen.findByText(/No exempt-private-company status is recorded, so a director's account in debit/)).toBeTruthy();
    const card = screen.getByRole("heading", { name: "Arjun Rao" }).closest("section") as HTMLElement;
    expect(within(card).getByText(/section 162 of the Companies Act bans loans to directors/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Record exempt status" }));
    fireEvent.change(await screen.findByLabelText("From"), { target: { value: "2025-03-14" } });
    fireEvent.click(screen.getByLabelText("Yes, exempt"));
    fireEvent.click(screen.getByRole("button", { name: "Next: attach the business profile" }));
    fireEvent.click(await screen.findByRole("button", { name: "Record the status" }));
    expect(await screen.findByText(/Recorded the company as an exempt private company from 14 Mar 2025/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/exempt-statuses")?.body).toEqual({ from: "2025-03-14", exempt: true });
    expect(seen.find((s) => s.path === "/v1/exempt-statuses/ex-1/record")?.headers.get("If-Match")).toBe('"1-draft"');
  });
});

// req: FR-EQ-12
// req: equity/founders-money Director confirms with a passkey
describe("the year-end confirmation", () => {
  it("asks a founder to confirm their balances at the year end, and says when they changed since", async () => {
    const waiting = { ...mei, year_end: { year_end: "2026-09-30", status: "not_confirmed" } };
    const seen = answer({
      "GET /v1/me/director-account": { status: 200, body: { linked: true, account: waiting } },
      "GET /v1/director-accounts/c-mei/statement": { status: 200, body: statement },
      "POST /v1/me/director-account/confirmations": { status: 201, body: { id: "dc-1", year_end: "2026-09-30", balances: [] } },
    });
    renderPage(MoneyIPutIn, { path: "/me/money-i-put-in", mode: "basic" });
    expect(await screen.findByText("Not confirmed")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Confirm my balances at 30 Sept? 2026 with my passkey/ }));
    expect(await screen.findByText(/Confirmed your balances at 30 Sept? 2026/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ year_end: "2026-09-30" });
  });

  it("shows a card's confirmation changed since, and what to do", async () => {
    answer({ "GET /v1/director-accounts": { status: 200, body: { ...list, items: [{ ...mei, year_end: { year_end: "2026-09-30", status: "changed_since" } }] } } });
    renderPage(DirectorAccounts, { path: "/accounting/directors" });
    const card = (await screen.findByRole("heading", { name: "Mei Lin Tan" })).closest("section") as HTMLElement;
    expect(within(card).getByText("Changed since confirmed")).toBeTruthy();
    expect(within(card).getByText(/the person confirms them with a passkey in Money I put in/)).toBeTruthy();
  });
});
