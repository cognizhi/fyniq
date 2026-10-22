import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { components } from "../../api/schema";
import { ChartOfAccounts, ordered } from "./ChartOfAccounts";

type Account = components["schemas"]["Account"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const acc = (id: string, code: string, name: string, extra: Record<string, unknown> = {}) =>
  ({ id, code, name, type: "expense", active: true, group: false, control: false, money_account: false, payroll_cost: false, parent_id: null, version: 1, ...extra }) as Account;
const office = acc("g1", "6100", "Premises & office", { group: true });
const rent = acc("a1", "6110", "Rent", { parent_id: "g1", report_group: "Premises & office", default_gst_code: "TX" });
const desk = acc("a2", "6115", "Co-working", { parent_id: "g1" });
const ar = acc("a3", "1100", "Accounts receivable", { type: "asset", role: "accounts_receivable", control: true });
const old = acc("a4", "6990", "Old account", { active: false });

// req: FR-COA-01
// req: FR-COA-02
// req: FR-COA-05
describe("the chart of accounts", () => {
  it("puts each account under its group, by code", () => {
    expect(ordered([desk, rent, office, ar]).map(({ a, depth }) => `${depth}:${a.code}`)).toEqual(["0:1100", "0:6100", "1:6110", "1:6115"]);
  });

  it("lists the accounts by type, hides inactive ones until asked, and axe finds nothing", async () => {
    answer({ "GET /v1/accounts": { status: 200, body: { items: [office, rent, desk, ar, old] } } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    const expenses = await screen.findByRole("table", { name: "Expenses" });
    expect(within(expenses).getByText("Rent")).toBeTruthy();
    expect(within(expenses).queryByText("Old account")).toBeNull();
    expect(within(screen.getByRole("table", { name: "Assets" })).getByText("System")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByLabelText("Show inactive accounts"));
    expect(within(screen.getByRole("table", { name: "Expenses" })).getByText("Old account")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Find an account"), { target: { value: "co-w" } });
    expect(screen.getAllByRole("row")).toHaveLength(2);
  });

  // req: FR-COA-01
  it("offers the standard chart to an empty chart", async () => {
    const seen = answer({ "GET /v1/accounts": { status: 200, body: { items: [] } }, "POST /v1/accounts:load-standard-chart": { status: 201, body: { accounts: 86 } } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Load the standard chart" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Load the standard chart" }));
    expect(await screen.findByText("The standard chart is loaded: 86 accounts.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows an Owner the chart without the buttons that change it", async () => {
    answer({ "GET /v1/accounts": { status: 200, body: { items: [] } } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts" });
    expect(await screen.findByText(/An Accountant or a Tax agent sets up the chart/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Load the standard chart" })).toBeNull();
  });

  it("adds an account, checking its code first", async () => {
    const seen = answer({ "GET /v1/accounts": { status: 200, body: { items: [office, rent] } }, "POST /v1/accounts": { status: 201, body: desk } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "New account" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "61 15" } });
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Co-working" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the account" }));
    expect(await within(dialog).findByText(/1 to 20 letters, digits, dots or hyphens, such as 6115/)).toBeTruthy();
    expect(calls()).not.toContain("POST /v1/accounts");
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "6115" } });
    fireEvent.change(within(dialog).getByLabelText("Under group"), { target: { value: "g1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the account" }));
    expect(await screen.findByText("6115 Co-working added.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ code: "6115", name: "Co-working", type: "expense", group: false, money_account: false, parent_id: "g1" });
  });

  // req: FR-COA-04
  it("saves only what changed, over the account's version", async () => {
    const seen = answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, rent] } },
      "GET /v1/accounts/a1": { status: 200, body: rent, headers: { ETag: '"1"' } },
      "PATCH /v1/accounts/a1": { status: 200, body: { ...rent, active: false, version: 2 } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Change 6110 Rent" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByLabelText(/Active: takes new postings/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the account" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/accounts/a1"));
    const patch = seen.find((s) => s.method === "PATCH");
    expect(patch?.body).toEqual({ active: false });
    expect(patch?.headers.get("If-Match")).toBe('"1"');
  });

  // req: FR-GL-04
  it("marks an asset or a liability for month-end revaluation, and offers it on nothing else", async () => {
    const wise = acc("a5", "1030", "Wise USD", { type: "asset", money_account: true, currency: "USD", revalue: false });
    const seen = answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, rent, wise] } },
      "GET /v1/accounts/a1": { status: 200, body: rent, headers: { ETag: '"1"' } },
      "GET /v1/accounts/a5": { status: 200, body: wise, headers: { ETag: '"1"' } },
      "PATCH /v1/accounts/a5": { status: 200, body: { ...wise, revalue: true, version: 2 } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Change 6110 Rent" }));
    let dialog = await screen.findByRole("dialog");
    await within(dialog).findByLabelText(/Active: takes new postings/);
    expect(within(dialog).queryByLabelText(/Revalued at month end/)).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Change 1030 Wise USD" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByLabelText(/Revalued at month end/));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the account" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/accounts/a5"));
    expect(seen.find((s) => s.method === "PATCH")?.body).toEqual({ revalue: true });
  });
});
