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
    const seen = answer({ "GET /v1/accounts": { status: 200, body: { items: [] } }, "POST /v1/accounts:load-standard-chart": { status: 201, body: { accounts: 88 } } });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Load the standard chart" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Load the standard chart" }));
    expect(await screen.findByText("The standard chart is loaded: 88 accounts.")).toBeTruthy();
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

  // req: ledger/chart-of-accounts Default useful life (rev 0.151)
  it("gives an asset account of the register a default useful life, and offers it on nothing else", async () => {
    const furniture = acc("a6", "1520", "Office equipment & furniture", { type: "asset", report_group: "Property, plant & equipment", useful_life_months: 60 });
    const seen = answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, rent, furniture] } },
      "GET /v1/accounts/a1": { status: 200, body: rent, headers: { ETag: '"1"' } },
      "GET /v1/accounts/a6": { status: 200, body: furniture, headers: { ETag: '"1"' } },
      "PATCH /v1/accounts/a6": { status: 200, body: { ...furniture, useful_life_months: 48, version: 2 } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Change 6110 Rent" }));
    let dialog = await screen.findByRole("dialog");
    await within(dialog).findByLabelText(/Active: takes new postings/);
    expect(within(dialog).queryByLabelText("Default useful life (months)")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Change 1520 Office equipment & furniture" }));
    dialog = await screen.findByRole("dialog");
    const life = (await within(dialog).findByLabelText("Default useful life (months)")) as HTMLInputElement;
    expect(life.value).toBe("60");
    fireEvent.change(life, { target: { value: "601" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the account" }));
    expect(await within(dialog).findByText(/Enter whole months from 1 to 600/)).toBeTruthy();
    fireEvent.change(life, { target: { value: "48" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the account" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/accounts/a6"));
    expect(seen.find((s) => s.method === "PATCH")?.body).toEqual({ useful_life_months: 48 });
  });

  // req: FR-DIM-01
  it("requires dimensions on an account: only those in use are offered, the whole list goes, and one out of use only comes off", async () => {
    const dept = { id: "d1", kind: "department", name: "Department", active: true, version: 1 };
    const project = { id: "d2", kind: "project", name: "Project", active: true, version: 1 };
    const grant = { id: "d3", kind: "custom", place: 1, name: "Grant", active: false, version: 1 };
    const tagged = { ...rent, required_dimension_ids: ["d1", "d3"] } as Account;
    const seen = answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, tagged] } },
      "GET /v1/accounts/a1": { status: 200, body: tagged, headers: { ETag: '"1"' } },
      "GET /v1/dimensions": { status: 200, body: { items: [dept, project, grant] } },
      "GET /v1/dimension-values": { status: 200, body: { items: [] } },
      "POST /v1/accounts": { status: 201, body: desk },
      "PATCH /v1/accounts/a1": { status: 200, body: { ...tagged, required_dimension_ids: ["d3", "d2"], version: 2 } },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "New account" }));
    let dialog = await screen.findByRole("dialog");
    const required = await within(dialog).findByRole("group", { name: "Required dimensions" });
    expect(within(required).queryByLabelText(/Grant/)).toBeNull(); // out of use: not offered
    fireEvent.change(within(dialog).getByLabelText("Code"), { target: { value: "6115" } });
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Co-working" } });
    fireEvent.click(within(required).getByLabelText("Department"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the account" }));
    expect(await screen.findByText("6115 Co-working added.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ code: "6115", name: "Co-working", type: "expense", group: false, money_account: false, required_dimension_ids: ["d1"] });

    fireEvent.click(screen.getByRole("button", { name: "Change 6110 Rent" }));
    dialog = await screen.findByRole("dialog");
    const again = await within(dialog).findByRole("group", { name: "Required dimensions" });
    expect((within(again).getByLabelText(/Grant \(out of use/) as HTMLInputElement).checked).toBe(true);
    fireEvent.click(within(again).getByLabelText("Department"));
    fireEvent.click(within(again).getByLabelText("Project"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the account" }));
    await waitFor(() => expect(calls()).toContain("PATCH /v1/accounts/a1"));
    expect(seen.find((s) => s.method === "PATCH")?.body).toEqual({ required_dimension_ids: ["d3", "d2"] });
  });

  // req: FR-COA-04
  it("asks for a merge into an account of the same type, over the account's version, and offers none on a system account", async () => {
    const asked = { id: "ap1", object_type: "account", object_key: "a1", operation: "account.merge", summary: "Merge 6110 Rent into 6115 Co-working on 2026-11-30", status: "pending" };
    const seen = answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, rent, desk, ar] } },
      "GET /v1/approvals": { status: 200, body: { items: [] } },
      "POST /v1/accounts/a1/merge-approval": { status: 202, body: asked },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Merge 6110 Rent into another account" }));
    expect(screen.queryByRole("button", { name: "Merge 1100 Accounts receivable into another account" })).toBeNull();
    const dialog = await screen.findByRole("dialog");
    const into = within(dialog).getByLabelText("Merge it into") as HTMLSelectElement;
    expect([...into.options].map((o) => o.text)).toEqual(["Choose an account", "6115 Co-working"]);
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for approval" }));
    expect(await within(dialog).findByText("Choose the account to merge it into.")).toBeTruthy();
    fireEvent.change(into, { target: { value: "a2" } });
    fireEvent.change(within(dialog).getByLabelText("On"), { target: { value: "2026-11-30" } });
    await noViolations(dialog);
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Merge 6110 Rent into 6115 Co-working on 2026-11-30: waiting for approval.")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ into_account_id: "a2", date: "2026-11-30" });
    expect(post?.headers.get("If-Match")).toBe('"1"');
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  // req: FR-COA-04
  it("shows a merge waiting for approval, and an account merged into another for good", async () => {
    const merged = acc("a5", "6135", "SaaS tools", { active: false, merged_into_id: "a2" });
    answer({
      "GET /v1/accounts": { status: 200, body: { items: [office, rent, desk, merged] } },
      "GET /v1/approvals": {
        status: 200,
        body: { items: [{ id: "ap1", object_type: "account", object_key: "a1", operation: "account.merge", summary: "Merge 6110 Rent into 6115 Co-working on 2026-11-30", status: "pending" }] },
      },
    });
    renderPage(ChartOfAccounts, { path: "/accounting/chart-of-accounts", session: accountant });
    expect(await screen.findByText("Merge waiting for approval")).toBeTruthy();
    const expenses = await screen.findByRole("table", { name: "Expenses" });
    expect(within(expenses).getByText("Merge awaiting approval")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Merge 6110 Rent into another account" })).toBeNull();
    fireEvent.click(screen.getByLabelText("Show inactive accounts"));
    expect(within(screen.getByRole("table", { name: "Expenses" })).getByText("Merged into 6115")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Change 6135 SaaS tools" })).toBeNull();
    await noViolations(document.body);
  });
});
