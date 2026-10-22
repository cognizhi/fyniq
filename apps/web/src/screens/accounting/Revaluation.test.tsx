import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Currencies } from "./Currencies";
import { defaultPeriod } from "./Revaluation";

type Reval = components["schemas"]["FXRevaluation"];
type Period = components["schemas"]["Period"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const period = (id: string, starts: string, ends: string, status: Period["status"] = "open"): Period => ({ id, starts_on: starts, ends_on: ends, status, financial_year: 1, version: 1 });
const sep = period("p09", "2026-09-01", "2026-09-30", "closed");
const oct = period("p10", "2026-10-01", "2026-10-31");
const nov = period("p11", "2026-11-01", "2026-11-30");
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const usd = (amount: string) => ({ amount, currency: "USD" });
// G4-039's October at account level: USD 10,000 carried at S$13,500.00, S$13,000.00 at 1.30, a S$500.00 loss.
const due: Reval = {
  period: oct,
  date: "2026-10-31",
  reverse_on: "2026-11-01",
  state: "due",
  balances: [{ account_id: "a1100", code: "1100", name: "Accounts receivable", open: usd("10000.00"), carrying: sgd("13500.00"), rate: "1.3", rate_source: "MAS daily feed of 2026-10-30", change: sgd("-500.00") }],
  missing: [],
  not_revalued: [{ account_id: "a1210", code: "1210", name: "Prepayments", amount: usd("120.00"), carrying: sgd("162.00") }],
  lines: [
    { account_id: "a1100", code: "1100", name: "Accounts receivable", amount: usd("0.00"), base: sgd("-500.00"), description: "10000.00 USD revalued at 1.3" },
    { account_id: "a7040", code: "7040", name: "Unrealised FX gain or loss", amount: sgd("500.00"), base: sgd("500.00"), description: "Unrealised FX loss" },
  ],
};
const done: Reval = {
  ...due,
  state: "revalued",
  balances: [{ ...due.balances![0]!, carrying: sgd("13000.00"), change: sgd("0.00") }],
  lines: [],
  latest: { document_id: "d1", posted_by: { kind: "user", id: "u1", name: "Grace Ho" }, posted_at: "2026-11-02T02:00:00Z", revision: 9, entry_id: "e1", reversal_id: "e2", current: true },
};
const rates = { status: 200, body: { items: [], duplicates: [] } };
const feed = { status: 200, body: { items: [] } };
const periods = { status: 200, body: { items: [sep, oct, nov] } };
const at = { path: "/accounting/fx" };

// req: FR-GL-04
describe("the month's revaluation on Accounting › Currencies", () => {
  it("shows the month's balances at the rate, the posting and what is not revalued, then revalues it; axe finds nothing", async () => {
    let posted = false;
    answer({
      "GET /v1/fx/rates": rates,
      "GET /v1/fx/feed": feed,
      "GET /v1/periods": periods,
      "GET /v1/periods/p10/fx-revaluation": () => ({ status: 200, body: posted ? done : due, headers: { ETag: '"1-open"' } }),
      "POST /v1/periods/p10/fx-revaluation": () => ((posted = true), { status: 201, body: done }),
    });
    renderPage(Currencies, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("tab", { name: "Revaluation" }));
    expect(await screen.findByText("Not revalued", { selector: ".fy-chip" })).toBeTruthy();
    expect((screen.getByLabelText("Month") as HTMLSelectElement).value).toBe("p10");
    const items = within(screen.getByRole("heading", { name: "Revaluation at 31 Oct 2026" }).closest("section")!);
    const row = items.getByText("Accounts receivable").closest("tr")!.textContent;
    expect(row).toContain("USD 10,000.00");
    expect(row).toContain("13,500.00");
    expect(row).toContain("MAS daily feed of 2026-10-30");
    expect(row).toContain("−500.00");
    const preview = within(screen.getByRole("heading", { name: "Posting preview" }).closest("section")!);
    const [, ar, fx] = preview.getAllByRole("row");
    expect(ar!.textContent).toContain("1100");
    expect(within(ar!).getAllByRole("cell")[2]!.textContent).toBe("500.00");
    expect(within(fx!).getAllByRole("cell")[1]!.textContent).toBe("500.00");
    expect(screen.getByText(/USD 120.00, carried at SGD 162.00/)).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Revalue October 2026" }));
    expect(await screen.findByText(/October 2026 is revalued\./)).toBeTruthy();
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.headers.get("If-Match")).toBe('"1-open"');
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText("Revalued", { selector: ".fy-chip" })).toBeTruthy();
    expect(screen.getByText(/Last revalued by Grace Ho on 2 Nov 2026, reversed on 1 Nov 2026\./)).toBeTruthy();
  });

  it("names a currency with no rate, offers to add it for the month's last day, and lets the Auditor only look", async () => {
    answer({
      "GET /v1/fx/rates": rates,
      "GET /v1/fx/feed": feed,
      "GET /v1/periods": periods,
      "GET /v1/periods/p10/fx-revaluation": { status: 200, body: { ...due, missing: [{ currency: "AUD", detail: "✖ No exchange rate for AUD on 31 Oct 2026" }], lines: [] } },
    });
    renderPage(Currencies, { ...at, session: accountant });
    fireEvent.click(await screen.findByRole("tab", { name: "Revaluation" }));
    expect(await screen.findByText("No AUD rate for 31 Oct 2026")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Revalue October 2026" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Add a AUD rate" }));
    const dialog = within(await screen.findByRole("dialog"));
    expect((dialog.getByLabelText("Currency") as HTMLInputElement).value).toBe("AUD");
    expect((dialog.getByLabelText("Day") as HTMLInputElement).value).toBe("2026-10-31");
  });

  it("shows the Auditor the revaluation without the button that runs it", async () => {
    answer({ "GET /v1/fx/rates": rates, "GET /v1/fx/feed": feed, "GET /v1/periods": periods, "GET /v1/periods/p10/fx-revaluation": { status: 200, body: due } });
    renderPage(Currencies, { ...at, session: auditor });
    fireEvent.click(await screen.findByRole("tab", { name: "Revaluation" }));
    expect(await screen.findByText("Not revalued", { selector: ".fy-chip" })).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("button", { name: /^Revalue / })).toBeNull());
  });

  it("starts on the latest open month that has ended", () => {
    expect(defaultPeriod([sep, oct, nov], "2026-11-15")?.id).toBe("p10");
    expect(defaultPeriod([sep, oct, nov], "2026-10-15")?.id).toBe("p10"); // none ended: the earliest open one
    expect(defaultPeriod([sep], "2026-10-15")?.id).toBe("p09");
  });
});
