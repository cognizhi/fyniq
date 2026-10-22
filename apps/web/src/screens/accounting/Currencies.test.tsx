import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Currencies, parseRates } from "./Currencies";

type Rate = components["schemas"]["FXRate"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const grace = { kind: "user" as const, id: "u1", name: "Grace Ho" };
const rate = (id: string, extra: Partial<Rate>): Rate => ({
  id,
  version: 1,
  kind: "day",
  currency: "USD",
  on: "2026-10-18",
  quote: "1.2953",
  quote_form: "base-per-unit",
  rate: "1.2953",
  base_currency: "SGD",
  entered_by: grace,
  entered_at: "2026-10-18T02:00:00Z",
  withdrawn: false,
  ...extra,
});
const usd = rate("r1", { note: "Bank's board rate" });
const eur1 = rate("r2", { kind: "month", currency: "EUR", on: "2026-10-01", quote: "1.48", rate: "1.48" });
const eur2 = rate("r3", { kind: "month", currency: "EUR", on: "2026-10-01", quote: "0.68", quote_form: "foreign-per-base", rate: "1.4705882353" });
const list = { status: 200, body: { items: [eur1, eur2, usd], duplicates: [{ currency: "EUR", month: "2026-10-01" }] } };
const feed = { status: 200, body: { items: [{ currency: "USD", base_currency: "SGD", on: "2026-10-16", rate: "1.2953", quote: "1.2953", quote_form: "base-per-unit", feed: "MAS" }] } };
const at = { path: "/accounting/fx" };

// req: FR-GL-05
// req: ENG-40
// req: RND-RATE
describe("Accounting › Currencies", () => {
  it("lists the organisation's rates with the monthly duplicates ⚠ and the daily feed; axe finds nothing", async () => {
    answer({ "GET /v1/fx/rates": list, "GET /v1/fx/feed": feed });
    renderPage(Currencies, { ...at, session: accountant });
    const own = within((await screen.findByRole("heading", { name: "Your rates" })).closest("section")!);
    expect(await own.findByText("Bank's board rate", { exact: false })).toBeTruthy();
    const rows = own.getAllByRole("row");
    expect(rows[1]!.textContent).toContain("October 2026 (monthly table)");
    expect(rows[1]!.textContent).toContain("Averaged with another");
    expect(rows[2]!.textContent).toContain("0.68 EUR for 1 SGD");
    expect(rows[3]!.textContent).toContain("18 Oct 2026");
    expect(rows[3]!.textContent).toContain("1.2953 SGD for 1 USD");
    expect(rows[3]!.textContent).toContain("In use");
    expect(screen.getByText("A month has more than one rate")).toBeTruthy();
    expect(screen.getByText(/EUR in October 2026: postings take the average/)).toBeTruthy();
    const daily = within(screen.getByRole("heading", { name: "Daily feed, last two weeks" }).closest("section")!);
    expect(daily.getByText("MAS")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add a rate" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Import a file" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("shows the Auditor the rates without the buttons that change them", async () => {
    answer({ "GET /v1/fx/rates": list, "GET /v1/fx/feed": feed });
    renderPage(Currencies, { ...at, session: auditor });
    expect(await screen.findByText("Bank's board rate", { exact: false })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a rate" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Withdraw/ })).toBeNull();
  });

  it("adds a rate for a day as it is quoted, and withdraws one with a reason and its version", async () => {
    const sent = answer({
      "GET /v1/fx/rates": list,
      "GET /v1/fx/feed": feed,
      "POST /v1/fx/rates": { status: 201, body: rate("r9", { currency: "JPY", quote: "90.45", quote_form: "base-per-100", rate: "0.9045" }) },
      "POST /v1/fx/rates/r1/withdraw": { status: 200, body: { ...usd, version: 2, withdrawn: true } },
    });
    renderPage(Currencies, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Add a rate" }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText("Currency"), { target: { value: "jpy" } });
    fireEvent.change(dialog.getByLabelText("Day"), { target: { value: "2026-10-17" } });
    fireEvent.change(dialog.getByLabelText("Rate"), { target: { value: "90.45" } });
    fireEvent.change(dialog.getByLabelText("Quoted as"), { target: { value: "base-per-100" } });
    fireEvent.click(dialog.getByRole("button", { name: "Add a rate" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const post = sent.find((x) => x.method === "POST" && x.path === "/v1/fx/rates")!;
    expect(post.body).toEqual({ kind: "day", currency: "JPY", on: "2026-10-17", quote: "90.45", quote_form: "base-per-100" });
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Withdraw the USD rate for 18 Oct 2026" }));
    const w = within(await screen.findByRole("dialog"));
    fireEvent.click(w.getByRole("button", { name: "Withdraw" }));
    expect(await w.findByText("Say why the rate is wrong or no longer used.")).toBeTruthy();
    fireEvent.change(w.getByLabelText("Why"), { target: { value: "Typed the wrong day" } });
    fireEvent.click(w.getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const withdraw = seen.find((x) => x.path === "/v1/fx/rates/r1/withdraw")!;
    expect(withdraw.body).toEqual({ reason: "Typed the wrong day" });
    expect(withdraw.headers.get("If-Match")).toBe('"1"');
  });

  it("refuses a rate of the base currency before sending it", async () => {
    answer({ "GET /v1/fx/rates": list, "GET /v1/fx/feed": feed });
    renderPage(Currencies, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Add a rate" }));
    const dialog = within(await screen.findByRole("dialog"));
    fireEvent.change(dialog.getByLabelText("Currency"), { target: { value: "SGD" } });
    fireEvent.change(dialog.getByLabelText("Rate"), { target: { value: "1" } });
    fireEvent.click(dialog.getByRole("button", { name: "Add a rate" }));
    expect(await dialog.findByText("SGD is the base currency: it takes rate 1.")).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST")).toEqual([]);
  });

  it("finds the rate a posting takes, and says when there is none", async () => {
    answer({
      "GET /v1/fx/rates": list,
      "GET /v1/fx/feed": feed,
      "GET /v1/fx/rate": (_body, req) =>
        new URL(req.url).searchParams.get("currency") === "USD"
          ? { status: 200, body: { currency: "USD", base_currency: "SGD", date: "2026-10-18", rate: "1.2953", source: "MAS daily feed of 2026-10-16", step: "feed", of: "2026-10-16", entries: 1 } }
          : {
              status: 422,
              headers: { "Content-Type": "application/problem+json" },
              body: { type: "urn:fyniq:problem:FX-01", title: "No exchange rate for this currency", status: 422, code: "FX-01", detail: "✖ No exchange rate for GBP on 18 Oct 2026.", fix: "Enter the GBP rate." },
            },
    });
    renderPage(Currencies, { ...at, session: accountant });
    const find = within((await screen.findByRole("heading", { name: "Find the rate a posting takes" })).closest("section")!);
    fireEvent.change(find.getByLabelText("Currency"), { target: { value: "usd" } });
    fireEvent.change(find.getByLabelText("Date"), { target: { value: "2026-10-18" } });
    fireEvent.click(find.getByRole("button", { name: "Find" }));
    expect(await find.findByText("1 USD = 1.2953 SGD")).toBeTruthy();
    expect(find.getByText("on 18 Oct 2026, from the MAS daily feed of 2026-10-16")).toBeTruthy();
    fireEvent.change(find.getByLabelText("Currency"), { target: { value: "GBP" } });
    fireEvent.click(find.getByRole("button", { name: "Find" }));
    expect(await find.findByText(/No exchange rate for GBP on 18 Oct 2026/)).toBeTruthy();
  });

  it("imports a file's rates, each on its own, and lists the lines it could not read or enter", async () => {
    const sent = answer({
      "GET /v1/fx/rates": list,
      "GET /v1/fx/feed": feed,
      "POST /v1/fx/rates:bulk": {
        status: 200,
        body: {
          saved: 1,
          refused: 1,
          results: [
            { index: 0, status: 201, id: "r10" },
            { index: 1, status: 422, problem: { type: "x", title: "A field is not valid", status: 422, code: "API-INPUT-02", errors: [{ code: "FX-02", field: "body.items[1].currency", message: "SGD is the base currency, which takes rate 1" }] } },
          ],
        },
      },
    });
    renderPage(Currencies, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Import a file" }));
    const dialog = within(await screen.findByRole("dialog"));
    const csv = "currency,date,quote,note\nUSD,2026-10-16,1.2953,DBS\nSGD,2026-10-16,1\nGBP,16/10/2026,1.7\n";
    fireEvent.change(dialog.getByLabelText("Rates file (CSV)"), { target: { files: [new File([csv], "october.csv", { type: "text/csv" })] } });
    expect(await dialog.findByText("october.csv: 2 rates to import, 1 line that cannot be read.")).toBeTruthy();
    expect(dialog.getByText('Line 4: "16/10/2026" is not a date written as 2026-10-16.')).toBeTruthy();
    fireEvent.click(dialog.getByRole("button", { name: "Import 2 rates" }));
    expect(await dialog.findByText("1 imported")).toBeTruthy();
    expect(dialog.getByText("Line 3: SGD is the base currency, which takes rate 1")).toBeTruthy();
    const post = sent.find((x) => x.path === "/v1/fx/rates:bulk")!;
    expect(post.body).toEqual({
      items: [
        { kind: "day", currency: "USD", on: "2026-10-16", quote: "1.2953", quote_form: "base-per-unit", note: "DBS" },
        { kind: "day", currency: "SGD", on: "2026-10-16", quote: "1", quote_form: "base-per-unit" },
      ],
    });
  });
});

describe("reading a rates file", () => {
  it("needs the columns, reads them in any order, and dates a monthly rate on its month's first day", () => {
    expect(parseRates("", "day", "base-per-unit").problem).toBe("The file is empty.");
    expect(parseRates("ccy,day,rate\nUSD,2026-10-16,1.3", "day", "base-per-unit").problem).toMatch(/must name the columns currency, date and quote/);
    expect(parseRates("currency,date,quote\n", "day", "base-per-unit").problem).toBe("The file has no rates under its first line.");
    const { rows } = parseRates('quote,currency,date\n"1.35",usd,2026-10-16\n1.2.3,USD,2026-10-17\n', "month", "foreign-per-base");
    expect(rows).toEqual([
      { line: 2, entry: { kind: "month", currency: "USD", on: "2026-10-01", quote: "1.35", quote_form: "foreign-per-base", note: undefined } },
      { line: 3, problem: '"1.2.3" is not a rate, such as 1.2953.' },
    ]);
  });
});
