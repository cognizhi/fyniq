import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { SignedInProvider } from "../../session";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { cellsOf, draftOf, OpeningBalances } from "./OpeningBalances";

type Account = components["schemas"]["Account"];
type Opening = components["schemas"]["OpeningBalances"];

const organisation: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const acc = (id: string, code: string, name: string, type: Account["type"]) =>
  ({ id, code, name, type, active: true, group: false, control: false, money_account: false, payroll_cost: false, version: 1 }) as Account;
const bank = acc("a-bank", "1010", "DBS current account", "asset");
const shares = acc("a-shares", "3010", "Share capital, ordinary", "equity");
const rent = acc("a-rent", "6110", "Rent", "expense");
const accounts = { status: 200, body: { items: [bank, shares, rent] } };
const profile = { status: 200, body: { books_start: "2026-10-01", fy_start_month: 1, time_zone: "Asia/Singapore", version: 2 } };
const none: Opening = { status: "none", version: 0, lines: [] };
const draft: Opening = {
  id: "ob1",
  status: "draft",
  version: 1,
  date: "2026-10-01",
  lines: [
    { account_id: "a-bank", amount: { amount: "10000.00", currency: "SGD" } },
    { account_id: "a-shares", amount: { amount: "-10000.00", currency: "SGD" } },
  ],
  prepared_by: { kind: "user", id: "u1", name: "Ah Kow" },
};

function page(session: Session) {
  return function Page() {
    return (
      <SignedInProvider value={{ session, organisation, mode: "expert" }}>
        <OpeningBalances />
      </SignedInProvider>
    );
  };
}

// req: FR-GL-06
describe("the opening balances' form", () => {
  it("reads saved balances as debits and credits, and sends only the accounts with one", () => {
    const cells = cellsOf(draft);
    expect(cells).toEqual({ "a-bank": { debit: "10000.00", credit: "" }, "a-shares": { debit: "", credit: "10000.00" } });
    expect(draftOf({ ...cells, "a-rent": { debit: "", credit: "" } }, "SGD", " TB ")).toEqual({
      draft: {
        memo: "TB",
        lines: [
          { account_id: "a-bank", amount: { amount: "10000.00", currency: "SGD" } },
          { account_id: "a-shares", amount: { amount: "-10000.00", currency: "SGD" } },
        ],
      },
      errors: {},
    });
    expect(draftOf({ x: { debit: "5", credit: "5" }, y: { debit: "abc", credit: "" }, z: { debit: "(5)", credit: "" } }, "SGD", "").errors).toEqual({
      x: "Enter a debit or a credit balance, not both.",
      y: "Enter an amount, such as 1,200.00.",
      z: "Enter the balance without a minus sign: a credit balance goes under Credit.",
    });
  });
});

// req: FR-GL-06
// req: FR-UX-07
describe("the opening balances page", () => {
  it("enters the balances account by account, shows whether they balance, and saves a draft at the version read; axe finds nothing", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/organisation/profile": profile,
      "GET /v1/opening-balances": { status: 200, body: none, headers: { ETag: `"0-none"` } },
      "PUT /v1/opening-balances": { status: 200, body: draft, headers: { ETag: `"1-draft"` } },
    });
    renderPage(page(accountant), { path: "/accounting/opening-balances" });
    expect(await screen.findByRole("heading", { name: "Opening balances at 1 Oct 2026" })).toBeTruthy();
    expect(screen.queryByLabelText("6110 Rent: debit")).toBeNull(); // revenue and expenses on request
    fireEvent.change(await screen.findByLabelText("1010 DBS current account: debit"), { target: { value: "10,000.00" } });
    fireEvent.change(screen.getByLabelText("3010 Share capital, ordinary: credit"), { target: { value: "9,000" } });
    expect(screen.getByText("Debits exceed credits by 1,000.00 SGD")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("3010 Share capital, ordinary: credit"), { target: { value: "10,000.00" } });
    expect(screen.getByText("Balanced")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Show revenue and expense accounts"));
    expect(screen.getByLabelText("6110 Rent: debit")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getAllByRole("button", { name: "Save as a draft" })[0]!);
    expect(await screen.findByText("Saved as a draft.")).toBeTruthy();
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe(`"0-none"`);
    expect(put.body).toEqual({
      lines: [
        { account_id: "a-bank", amount: { amount: "10000.00", currency: "SGD" } },
        { account_id: "a-shares", amount: { amount: "-10000.00", currency: "SGD" } },
      ],
    });
  });

  it("checks a draft without saving, then posts it once", async () => {
    let posted = false;
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/organisation/profile": profile,
      "GET /v1/opening-balances": () => ({ status: 200, body: posted ? { ...draft, status: "posted", posted_by: { kind: "user", id: "u2", name: "Wei Ling" } } : draft, headers: { ETag: posted ? `"1-posted"` : `"1-draft"` } }),
      "POST /v1/opening-balances/post": (_b, req) => {
        const entry = { id: "e1", ledger_revision: 2, posting_date: "2026-10-01", period_id: "p", posting_identity: "x", rule_version: "opening_balance@1", recorded_at: "2026-10-21T00:00:00Z", lines: [{}, {}] };
        if (req.headers.get("Prefer") !== "return=preview") posted = true;
        return { status: 200, body: { opening_balances: { ...draft, status: posted ? "posted" : "draft" }, entry } };
      },
    });
    renderPage(page(accountant), { path: "/accounting/opening-balances" });
    expect(await screen.findByText("Prepared by Ah Kow")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Check" })[0]!);
    expect(await screen.findByText("Ready to post")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")!.headers.get("Prefer")).toBe("return=preview");
    fireEvent.click(screen.getAllByRole("button", { name: "Post" })[0]!);
    expect(await screen.findByText("Posted at 1 Oct 2026.")).toBeTruthy();
    const sent = seen.filter((s) => s.method === "POST")[1]!;
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect(sent.headers.get("If-Match")).toBe(`"1-draft"`);
    await waitFor(() => expect(screen.getByText("Posted opening balances do not change here yet")).toBeTruthy());
    expect(screen.queryByLabelText("1010 DBS current account: debit")).toBeNull();
    expect(within(screen.getByRole("table", { name: "Accounts" })).getAllByText("10,000.00")).toHaveLength(4); // a debit, a credit and both totals
  });

  it("asks for the books start first", async () => {
    answer({
      "GET /v1/accounts": accounts,
      "GET /v1/organisation/profile": { status: 200, body: { fy_start_month: 1, time_zone: "Asia/Singapore", version: 1 } },
      "GET /v1/opening-balances": { status: 200, body: none, headers: { ETag: `"0-none"` } },
    });
    renderPage(page(accountant), { path: "/accounting/opening-balances" });
    expect(await screen.findByText("Set when the books start first")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });
});
