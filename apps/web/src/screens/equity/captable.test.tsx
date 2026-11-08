import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { forgetMode, setMode } from "../../mode";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { CapTable, shares } from "./CapTable";

const board = { ...ownerSession, roles: ["board_viewer"] } as Session;
const move = (id: string, n: string, on: string, tx?: string) =>
  ({ id, kind: "issue", shares: n, effective_on: on, ...(tx ? { transaction_id: tx, transaction_number: "EQ-0002" } : { share_issue_id: `si-${id}` }) });
const line = (holder: string, name: string, n: string, own: string, moves: ReturnType<typeof move>[]) => ({
  holder_id: holder, holder_name: name, class_name: "Ordinary shares", account_code: "3010", issued: n, votes: n, fully_diluted: n, ownership: own,
  voting: own, diluted: own, votes_assumed: true, movements: moves,
});
const holder = (id: string, name: string, n: string, own: string, mine = false) =>
  ({ holder_id: id, holder_name: name, issued: n, votes: n, fully_diluted: n, ownership: own, voting: own, diluted: own, mine });
const round = {
  at: "2027-03-12", issued: "1252083", votes: "1252083", fully_diluted: "1252083",
  lines: [line("c-kow", "Ah Kow", "1000000", "79.87", [move("m1", "1000000", "2025-01-02")]),
    line("c-hatch", "Hatch Capital Pte. Ltd.", "52083", "4.16", [move("m3", "52083", "2027-03-12", "eq-2")]),
    line("c-fund", "Seedling Ventures Pte. Ltd.", "200000", "15.97", [move("m2", "200000", "2027-03-12", "eq-2")])],
  holders: [holder("c-kow", "Ah Kow", "1000000", "79.87", true), holder("c-hatch", "Hatch Capital Pte. Ltd.", "52083", "4.16"),
    holder("c-fund", "Seedling Ventures Pte. Ltd.", "200000", "15.97")],
};
const before = { at: "2027-01-31", issued: "1000000", votes: "1000000", fully_diluted: "1000000",
  lines: [round.lines[0]], holders: [holder("c-kow", "Ah Kow", "1000000", "100.00", true)] };
const mine = { at: "2027-03-12", linked: true, holder: round.holders[0], lines: [round.lines[0]], issued: "1252083", votes: "1252083", fully_diluted: "1252083" };
const issue = { id: "eq-2", number: "EQ-0002", status: "effective", effective_on: "2027-03-12", version: 3, checks: [], movements: [], entries: [], evidence: 1,
  details: { kind: "issue_cash", holder_id: "c-fund", class_account_id: "acc-3010", shares: "200000", price: "3.00" },
  parties: [{ contact_id: "c-fund", name: "Seedling Ventures Pte. Ltd.", role: "subscriber" }], amount: { amount: "600000.00", currency: "SGD" } };
const waiting = { ...issue, id: "eq-3", number: "EQ-0003", status: "approved", effective_on: undefined };

afterEach(() => forgetMode());

// req: FR-EQ-20
// req: FR-EQ-27
describe("who owns the company", () => {
  it("groups shares for reading", () => {
    expect(shares("1252083")).toBe("1,252,083");
    expect(shares("500")).toBe("500");
    expect(shares("1500000.5")).toBe("1,500,000.5");
  });

  it("shows the ownership, my holding, the latest round and the shareholders in Basic; axe finds nothing", async () => {
    setMode("basic");
    answer({
      "GET /v1/cap-table": { status: 200, body: round },
      "GET /v1/me/holding": { status: 200, body: mine },
      "GET /v1/equity-transactions": { status: 200, body: { items: [issue, waiting] } },
    });
    renderPage(CapTable, { path: "/equity" });
    expect(await screen.findByRole("heading", { name: "Who owns the company" })).toBeTruthy();
    expect(await screen.findByRole("img", { name: /1,252,083 shares issued\. Ah Kow 79\.87%/ })).toBeTruthy();
    expect(within(screen.getByRole("region", { name: "My holding" })).getByText("79.87%")).toBeTruthy();
    expect(await screen.findByText("600,000.00 SGD")).toBeTruthy();
    const table = screen.getByRole("table", { name: "Shareholders" });
    expect(within(table).getByText("Ah Kow (you)")).toBeTruthy();
    expect(within(table).getByText("15.97%")).toBeTruthy();
    expect(screen.getByText(/1 transaction waits for ACRA/)).toBeTruthy();
    expect(screen.queryByLabelText("Compare with")).toBeNull();
    expect(screen.queryByRole("table", { name: "By holder and class" })).toBeNull();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("radio", { name: "Table" }));
    expect(within(screen.getByRole("table", { name: "Ownership" })).getByText("1,000,000")).toBeTruthy();
  });

  it("compares two days and opens a figure's movements in Expert; axe finds nothing", async () => {
    setMode("expert");
    const seen = answer({
      "GET /v1/cap-table": { status: 200, body: { ...round, compared: before } },
      "GET /v1/me/holding": { status: 200, body: mine },
      "GET /v1/equity-transactions": { status: 200, body: { items: [issue] } },
    });
    renderPage(CapTable, { path: "/equity" });
    fireEvent.change(await screen.findByLabelText("Compare with"), { target: { value: "2027-01-31" } });
    expect(await screen.findByRole("heading", { name: "Since 31 Jan 2027" })).toBeTruthy();
    expect(screen.getByText("Issued shares 1,000,000 → 1,252,083.")).toBeTruthy();
    expect(screen.getByText("Ah Kow: 100.00% → 79.87%")).toBeTruthy();
    expect(seen.some((s) => s.method === "GET" && s.path === "/v1/cap-table" && new URLSearchParams(s.query).get("compare") === "2027-01-31")).toBe(true);
    const table = screen.getByRole("table", { name: "By holder and class" });
    expect(within(table).getAllByText("Votes assumed")).toHaveLength(3);
    expect(within(table).getAllByText("1,252,083")).toHaveLength(2); // issued and fully diluted, equal with nothing dilutive
    await noViolations(document.body);
    fireEvent.click(within(table).getByRole("button", { name: "200,000 shares of Seedling Ventures Pte. Ltd.: their movements" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("link", { name: "EQ-0002" }).getAttribute("href")).toBe("/equity/transactions/eq-2");
  });

  it("counts the options and the option pool fully diluted; axe finds nothing", async () => {
    // req: FR-EQ-19
    setMode("expert");
    const pooled = {
      at: "2027-02-01", issued: "1000000", votes: "1000000", options: "48000", unallocated_pool: "52000", pool_diluted: "4.73", fully_diluted: "1100000",
      lines: [{ ...line("c-kow", "Ah Kow", "1000000", "100.00", [move("m1", "1000000", "2025-01-02")]), diluted: "90.91" },
        { ...line("c-priya", "Priya Nair", "0", "0.00", []), votes: "0", fully_diluted: "48000", options: "48000", diluted: "4.36" }],
      holders: [{ ...holder("c-kow", "Ah Kow", "1000000", "100.00", true), diluted: "90.91" },
        { ...holder("c-priya", "Priya Nair", "0", "0.00"), votes: "0", fully_diluted: "48000", options: "48000", diluted: "4.36" }],
    };
    answer({
      "GET /v1/cap-table": { status: 200, body: pooled },
      "GET /v1/me/holding": { status: 200, body: { ...mine, at: "2027-02-01", issued: "1000000", votes: "1000000", fully_diluted: "1100000" } },
      "GET /v1/equity-transactions": { status: 200, body: { items: [] } },
    });
    renderPage(CapTable, { path: "/equity" });
    const table = await screen.findByRole("table", { name: "By holder and class" });
    expect(within(table).getByText("incl. 48,000 options")).toBeTruthy();
    const pool = within(table).getByText("Unallocated option pool").closest("tr") as HTMLElement;
    expect(within(pool).getByText("52,000")).toBeTruthy();
    expect(within(pool).getByText("4.73")).toBeTruthy();
    expect(screen.getByText("Fully diluted · 1,100,000 shares, with 48,000 options granted and 52,000 left in the option pool")).toBeTruthy();
    expect(screen.getByRole("img", { name: "1,000,000 shares issued. Ah Kow 100.00%." })).toBeTruthy(); // Priya holds no shares, so owns none
    await noViolations(document.body);
  });

  it("exports the cap table at its day", async () => {
    setMode("basic");
    const seen = answer({
      "GET /v1/cap-table": { status: 200, body: round },
      "GET /v1/me/holding": { status: 200, body: mine },
      "GET /v1/equity-transactions": { status: 200, body: { items: [] } },
      "GET /v1/cap-table/export": { status: 200, body: { export_id: "x-1", file_name: "cap-table-2027-03-12.pdf", media_type: "application/pdf",
        sha256: "00", size_bytes: 4, rows: 3, ledger_revision: 1, content_base64: "JVBERg==" } },
    });
    renderPage(CapTable, { path: "/equity" });
    fireEvent.change(await screen.findByLabelText("As at"), { target: { value: "2027-03-12" } });
    fireEvent.click(await screen.findByRole("button", { name: "PDF" }));
    await new Promise((r) => setTimeout(r, 0));
    const call = seen.find((s) => s.path === "/v1/cap-table/export");
    const q = new URLSearchParams(call?.query);
    expect(q.get("format")).toBe("pdf");
    expect(q.get("at")).toBe("2027-03-12");
  });

  it("shows the Board viewer their own holding alone", async () => {
    setMode("basic");
    answer({
      "GET /v1/cap-table": { status: 403, body: { code: "API-AUTHZ-01", detail: "Your roles hold no \"V\" right." } },
      "GET /v1/me/holding": { status: 200, body: mine },
    });
    renderPage(CapTable, { path: "/equity", session: board });
    expect(await screen.findByRole("region", { name: "My holding" })).toBeTruthy();
    expect(screen.queryByRole("navigation", { name: "Equity views" })).toBeNull();
    expect(screen.queryByRole("button", { name: "PDF" })).toBeNull();
    expect(screen.queryByText("Seedling Ventures Pte. Ltd.")).toBeNull();
    await noViolations(document.body);
  });
});
