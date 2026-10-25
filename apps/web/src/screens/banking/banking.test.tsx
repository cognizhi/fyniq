import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AddBankAccount, checkAccount, senders } from "./AddBankAccount";
import { BankAccounts, type MoneyAccount } from "./BankAccounts";

const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const founder = { ...ownerSession, roles: ["owner", "accountant", "administrator"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const money = (extra: Partial<MoneyAccount> = {}): MoneyAccount => ({
  id: "m1",
  account_id: "a1010",
  account_code: "1010",
  kind: "bank",
  name: "DBS current",
  institution: "DBS",
  identifier: "…4567",
  currency: "SGD",
  freshness: { last_import: "2026-10-26T01:00:00Z", complete_to: "2026-10-25", open_gaps: 0 },
  waiting: 12,
  gaps: [],
  sources: [],
  version: 1,
  ...extra,
});
const position = {
  date: "2026-10-26",
  accounts: [
    { money_account_id: "m1", name: "DBS current", kind: "bank", balance: sgd("287940.22") },
    { money_account_id: "m2", name: "Wise USD", kind: "wallet", balance: { amount: "15862.65", currency: "USD" }, value: sgd("20410.47"), rate: "1.2867", rate_date: "2026-10-26" },
  ],
  total: sgd("308350.69"),
  no_rate: ["Airwallex EUR"],
};

// req: FR-BNK-01
// req: FR-BNK-13
// req: FR-BNK-15
// req: FR-BNK-16
describe("bank accounts", () => {
  it("shows the cash position from the server and each account with its freshness, source and waiting lines; axe finds nothing", async () => {
    answer({
      "GET /v1/money-accounts": {
        status: 200,
        body: {
          items: [
            money(),
            money({
              id: "m2",
              kind: "wallet",
              name: "Wise USD",
              institution: "Wise",
              currency: "USD",
              waiting: 1,
              source: { kind: "connector", from_date: "2026-10-01", senders: [], connector_id: "c1" },
              freshness: { last_import: "2026-10-20T01:00:00Z", complete_to: "2026-10-18", open_gaps: 1 },
            }),
            money({ id: "m3", name: "Petty cash", kind: "cash", identifier: undefined, institution: undefined, waiting: 0, freshness: { open_gaps: 0 } }),
          ],
        },
      },
      "GET /v1/cash-position": { status: 200, body: position },
    });
    const { container } = renderPage(BankAccounts, { path: "/banking", session: bookkeeper });
    const cash = await screen.findByRole("table", { name: "Cash by account" });
    expect(within(cash).getAllByRole("row")[2]!.textContent).toContain("20,410.47");
    expect(screen.getByText("308,350.69")).toBeTruthy();
    expect(screen.getByText(/Airwallex EUR is left out of the total/)).toBeTruthy();
    const dbs = screen.getByRole("article", { name: "DBS current" });
    expect(dbs.textContent).toContain("Complete to");
    expect(dbs.textContent).toContain("12 lines to categorise");
    expect(dbs.textContent).toContain("Statement files you import");
    expect(dbs.textContent).toContain("…4567");
    const wise = screen.getByRole("article", { name: "Wise USD" });
    expect(wise.textContent).toContain("A gap in its statements");
    expect(wise.textContent).toContain("A connector syncs it");
    expect(screen.getByRole("article", { name: "Petty cash" }).textContent).toContain("No statements yet");
    expect(screen.getByText("3 accounts · 13 lines wait")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add account" }).getAttribute("href")).toBe("/banking/accounts/new");
    await noViolations(container);
  });

  it("teaches what to do with no accounts, and offers Add account only to those who may", async () => {
    answer({ "GET /v1/money-accounts": { status: 200, body: { items: [] } }, "GET /v1/cash-position": { status: 200, body: { date: "2026-10-26", accounts: [], total: sgd("0.00"), no_rate: [] } } });
    renderPage(BankAccounts, { path: "/banking", session: auditor });
    expect(await screen.findByText("No money accounts yet")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Add account" })).toBeNull();
    expect(screen.getByText("A Bookkeeper or an Accountant adds money accounts.")).toBeTruthy();
  });

  it("says why the accounts could not be listed", async () => {
    answer({ "GET /v1/money-accounts": problem(403, "FORBIDDEN", "Your roles do not include viewing bank transactions."), "GET /v1/cash-position": problem(403, "FORBIDDEN") });
    renderPage(BankAccounts, { path: "/banking", session: auditor });
    expect(await screen.findByText("The money accounts could not be listed")).toBeTruthy();
  });
});

const chart = {
  status: 200,
  body: {
    items: [
      { id: "a1010", code: "1010", name: "DBS current", type: "asset", group: false, active: true, money_account: true, currency: "SGD", control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
      { id: "a1020", code: "1020", name: "OCBC current", type: "asset", group: false, active: true, money_account: true, currency: "SGD", control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
      { id: "a6100", code: "6100", name: "Software", type: "expense", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
    ],
  },
};

// req: FR-BNK-01
// req: FR-BNK-15
// req: FR-BNK-17
// req: FR-INT-22
describe("adding a money account", () => {
  it("says what is missing in the first step, every field at once", () => {
    expect(Object.keys(checkAccount({ kind: "bank", name: " ", institution: "", account_id: "", identifier: "", opened_on: "" }).errors).sort()).toEqual(["account_id", "name"]);
    expect(checkAccount({ kind: "wallet", name: "Wise USD", institution: "", account_id: "a1", identifier: " 200001 ", opened_on: "" }).draft).toEqual({
      kind: "wallet",
      name: "Wise USD",
      account_id: "a1",
      institution: undefined,
      identifier: "200001",
      opened_on: undefined,
    });
    expect(senders("IDEAL@dbs.example, alerts@dbs.example\nmore@dbs.example")).toEqual(["ideal@dbs.example", "alerts@dbs.example", "more@dbs.example"]);
  });

  it("adds the account on a free money account of the chart, then names the bank-ingest address and registers the bank's senders; axe finds nothing", async () => {
    const sent = answer({
      "GET /v1/accounts": chart,
      "GET /v1/money-accounts": { status: 200, body: { items: [money({ account_id: "a1010" })] } },
      "POST /v1/money-accounts": { status: 201, body: money({ id: "m9", account_id: "a1020", name: "OCBC current", freshness: { open_gaps: 0 }, waiting: 0 }), headers: { ETag: '"1"' } },
      "GET /v1/organisation/email/capture-addresses": { status: 200, body: { domain: "in.example.com", bank_etag: '"none"', mine_etag: '"none"', organisation_etag: '"none"' } },
      "GET /v1/statement-mappings": { status: 200, body: { items: [{ id: "map1", name: "OCBC daily", format: "csv", version: 1 }] } },
      "PUT /v1/organisation/email/capture-addresses/bank": { status: 200, body: { domain: "in.example.com", bank: { name: "bank", address: "bank@in.example.com", created_at: "2026-10-26T01:00:00Z" }, bank_etag: '"1"', mine_etag: '"none"', organisation_etag: '"none"' } },
      "POST /v1/money-accounts/m9/sources": { status: 200, body: money({ id: "m9", version: 2 }) },
    });
    const { container } = renderPage(AddBankAccount, { path: "/banking/accounts/new", session: founder });
    const select = await screen.findByLabelText("Account in the chart");
    await waitFor(() => expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose an account", "1020 OCBC current (SGD)"]));
    fireEvent.click(screen.getByRole("button", { name: "Add the account" }));
    expect(await screen.findByText("Name the account, such as DBS current.")).toBeTruthy();
    expect(seen.some((s) => s.method === "POST")).toBe(false);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "OCBC current" } });
    fireEvent.change(select, { target: { value: "a1020" } });
    fireEvent.click(screen.getByRole("button", { name: "Add the account" }));
    expect(await screen.findByText("Where does OCBC current's data come from?")).toBeTruthy();
    expect(sent.find((s) => s.path === "/v1/money-accounts" && s.method === "POST")?.body).toMatchObject({ kind: "bank", name: "OCBC current", account_id: "a1020" });
    expect(screen.getByRole("radio", { name: /Bank API or host-to-host/ }).getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(screen.getByRole("radio", { name: /Bank reports by email/ }));
    fireEvent.click(screen.getByRole("button", { name: "Set the data source" }));
    expect(await screen.findByText("Name the bank-ingest address first, such as bank.")).toBeTruthy();
    expect(screen.getByText("Enter the address the bank sends its reports from.")).toBeTruthy();
    expect(screen.getByText("Choose the saved column mapping its reports follow.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Name the bank-ingest address"), { target: { value: "bank" } });
    fireEvent.change(screen.getByLabelText("The bank's sender addresses"), { target: { value: "Reports@ocbc.example" } });
    fireEvent.change(await screen.findByLabelText("Column mapping"), { target: { value: "map1" } });
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Set the data source" }));
    expect(await screen.findByText("Awaiting its first report")).toBeTruthy();
    const named = sent.find((s) => s.method === "PUT");
    expect(named?.body).toEqual({ name: "bank" });
    expect(named?.headers.get("If-Match")).toBe('"none"');
    const source = sent.find((s) => s.path === "/v1/money-accounts/m9/sources");
    expect(source?.headers.get("If-Match")).toBe('"1"');
    expect(source?.body).toMatchObject({ kind: "email", senders: ["reports@ocbc.example"], report_format: "csv", mapping_id: "map1" });
  });

  it("leaves emailed reports and connectors to an Administrator: a Bookkeeper's account takes statement files", async () => {
    answer({
      "GET /v1/accounts": chart,
      "GET /v1/money-accounts": { status: 200, body: { items: [] } },
      "POST /v1/money-accounts": { status: 201, body: money({ id: "m9" }), headers: { ETag: '"1"' } },
    });
    renderPage(AddBankAccount, { path: "/banking/accounts/new", session: bookkeeper });
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "DBS current" } });
    const select = screen.getByLabelText("Account in the chart");
    await waitFor(() => expect(within(select).getAllByRole("option")).toHaveLength(3));
    fireEvent.change(select, { target: { value: "a1010" } });
    fireEvent.click(screen.getByRole("button", { name: "Add the account" }));
    expect((await screen.findByRole("radio", { name: /Bank reports by email/ })).getAttribute("aria-disabled")).toBe("true");
    expect(screen.getByText(/An Administrator sets up bank reports by email and connectors/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("DBS current is added")).toBeTruthy();
    expect(seen.some((s) => s.path.endsWith("/sources"))).toBe(false);
  });

  it("shows what the API refused", async () => {
    answer({
      "GET /v1/accounts": chart,
      "GET /v1/money-accounts": { status: 200, body: { items: [] } },
      "POST /v1/money-accounts": problem(409, "STORAGE-DUPLICATE-01", "The chart account 1010 has a money account already.", "Choose another account."),
    });
    renderPage(AddBankAccount, { path: "/banking/accounts/new", session: bookkeeper });
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "DBS current" } });
    const select = screen.getByLabelText("Account in the chart");
    await waitFor(() => expect(within(select).getAllByRole("option")).toHaveLength(3));
    fireEvent.change(select, { target: { value: "a1010" } });
    fireEvent.click(screen.getByRole("button", { name: "Add the account" }));
    expect(await screen.findByText("The account was not added")).toBeTruthy();
    expect(screen.getByText(/has a money account already/)).toBeTruthy();
  });
});
