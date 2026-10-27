import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { checkDisposal, checkRegister, emptyRegister, FixedAssets, lifeText, type Disposal, type FixedAsset, type Unregistered } from "./FixedAssets";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const organisation = { base_currency: "SGD", team_mode: "founder" } as never;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const asset = (extra: Partial<FixedAsset> = {}): FixedAsset => ({
  id: "fa1",
  number: "FA-0001",
  status: "in_use",
  version: 1,
  name: "MacBook Pro 14",
  account_id: "a1510",
  source: { document_id: "b1", type: "bill", number: "BILL-0042", version: 1, line: 1 },
  purchased_on: "2026-10-14",
  cost: sgd("3600.00"),
  residual: sgd("0.00"),
  life_months: 36,
  method: "straight_line",
  start: "2026-10",
  end: "2029-09",
  accumulated_depreciation: sgd("100.00"),
  net_book_value: sgd("3500.00"),
  depreciated_to: "2026-10",
  depreciation: [
    { month: "2026-10", date: "2026-10-31", version: 1, amount: sgd("100.00"), status: "posted", entry_id: "e1" },
    { month: "2026-11", date: "2026-11-30", version: 1, amount: sgd("100.00"), status: "waiting" },
    { month: "2026-12", date: "2026-12-31", version: 1, amount: sgd("100.00"), status: "projected" },
  ],
  checks: [],
  ...extra,
});
const accounts = {
  items: [
    { id: "a1510", code: "1510", name: "Computer equipment", type: "asset", group: false, active: true, money_account: false, control: false, report_group: "Property, plant & equipment", version: 1 },
    { id: "a1011", code: "1011", name: "DBS current", type: "asset", group: false, active: true, money_account: true, control: false, version: 1 },
    { id: "a7050", code: "7050", name: "Gain / (loss) on disposal of assets", type: "revenue", group: false, active: true, money_account: false, control: false, version: 1 },
  ],
};
const waiting: Unregistered = {
  document_id: "b2",
  type: "bill",
  number: "BILL-0043",
  line: 1,
  date: "2026-10-20",
  account_id: "a1510",
  description: "Dell monitor",
  cost: sgd("1200.00"),
  at_threshold: true,
};
const disposal = (extra: Partial<Disposal> = {}): Disposal => ({
  id: "d1",
  number: "DSP-0001",
  status: "posted",
  version: 1,
  asset_id: "fa1",
  asset_number: "FA-0001",
  asset_name: "MacBook Pro 14",
  date: "2026-11-15",
  whole: true,
  cost: sgd("3600.00"),
  accumulated_depreciation: sgd("100.00"),
  net_book_value: sgd("3500.00"),
  proceeds: "money",
  money_account_id: "a1011",
  amount: sgd("2000.00"),
  net_proceeds: sgd("2000.00"),
  gain_or_loss: sgd("-1500.00"),
  checks: [],
  ledger_impact: [
    { line_no: 1, account_id: "a1590", amount: sgd("100.00"), base_amount: sgd("100.00"), fx_rate: "1", fx_rate_source: "base" },
    { line_no: 2, account_id: "a1011", amount: sgd("2000.00"), base_amount: sgd("2000.00"), fx_rate: "1", fx_rate_source: "base" },
    { line_no: 3, account_id: "a1510", amount: sgd("-3600.00"), base_amount: sgd("-3600.00"), fx_rate: "1", fx_rate_source: "base" },
    { line_no: 4, account_id: "a7050", amount: sgd("1500.00"), base_amount: sgd("1500.00"), fx_rate: "1", fx_rate_source: "base" },
  ],
  ...extra,
});
const register = (extra: Record<string, unknown> = {}) => ({
  "GET /v1/fixed-assets": { status: 200, body: { items: [asset(), asset({ id: "fa2", number: "FA-0002", name: "Dell monitor 27", status: "fully_depreciated", net_book_value: sgd("0.00") })] } },
  "GET /v1/fixed-assets/unregistered": { status: 200, body: { items: [waiting] } },
  "GET /v1/fixed-assets/fa1": { status: 200, body: asset(), headers: { ETag: '"1-posted"' } },
  "GET /v1/asset-disposals": { status: 200, body: { items: [] } },
  "GET /v1/accounts": { status: 200, body: accounts },
  ...extra,
});

// req: FR-FA-01
// req: FR-FA-02
// req: FR-FA-03
describe("the fixed asset register", () => {
  it("checks an asset and a disposal before they are sent", () => {
    expect(lifeText(36)).toBe("3 years");
    expect(lifeText(12)).toBe("1 year");
    expect(lifeText(18)).toBe("18 months");
    expect(Object.keys(checkRegister({ ...emptyRegister, life_months: "0", residual: "-1", start: "March" }, "SGD", { document_id: "b1", line: 1 }).errors).sort()).toEqual([
      "life_months",
      "residual",
      "start",
    ]);
    expect(checkRegister({ ...emptyRegister, life_months: "36" }, "SGD", { document_id: "b1", line: 1 }).draft).toMatchObject({ source: { document_id: "b1", line: 1 }, life_months: 36 });
    expect(Object.keys(checkRegister({ ...emptyRegister, life_months: "36" }, "SGD", null).errors).sort()).toEqual(["account_id", "cost", "name", "purchased_on"]);
    const opening = checkRegister({ ...emptyRegister, name: "Dell monitor", life_months: "36", account_id: "a1510", purchased_on: "2025-10-03", cost: "3600", accumulated: "1200" }, "SGD", null);
    expect(opening.draft?.opening).toEqual({ account_id: "a1510", purchased_on: "2025-10-03", cost: sgd("3600"), accumulated_depreciation: sgd("1200") });
    const form = { date: "", part: true, cost: "0", proceeds: "money" as const, money_account_id: "", amount: "", gst_code: "", invoice_id: "", invoice_line: "1", notes: "" };
    expect(Object.keys(checkDisposal(form, "SGD").errors).sort()).toEqual(["amount", "cost", "date", "money_account_id"]);
    expect(Object.keys(checkDisposal({ ...form, date: "2026-11-15", part: false, proceeds: "invoice", invoice_line: "x" }, "SGD").errors).sort()).toEqual(["invoice_id", "invoice_line"]);
    expect(checkDisposal({ ...form, date: "2026-11-15", part: false, proceeds: "none" }, "SGD").draft).toEqual({ date: "2026-11-15", proceeds: "none" });
  });

  it("lists the register with its totals, and an asset's schedule posted, waiting and projected; axe finds nothing", async () => {
    answer(register());
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    const table = await screen.findByRole("table", { name: "Assets" });
    expect(within(table).getByText("Fully depreciated")).toBeTruthy();
    expect(within(table).getByText("In use")).toBeTruthy();
    expect(screen.getByText(/2 assets · cost SGD 7,200.00 · net book value SGD 3,500.00/)).toBeTruthy();
    fireEvent.click(within(table).getByRole("button", { name: "MacBook Pro 14" }));
    const schedule = await screen.findByRole("table", { name: "Depreciation schedule" });
    expect(within(schedule).getByText("Posted")).toBeTruthy();
    expect(within(schedule).getByText("Waiting")).toBeTruthy();
    expect(within(schedule).getByText("Projected")).toBeTruthy();
    expect(screen.getByRole("link", { name: "BILL-0042" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("registers a purchase not in the register yet", async () => {
    answer(register({ "POST /v1/fixed-assets": { status: 201, body: asset({ id: "fa3", number: "FA-0003", name: "Dell monitor" }) } }));
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(await screen.findByRole("tab", { name: /Not in the register yet \(1\)/ }));
    const list = await screen.findByRole("table", { name: "Purchases not in the register" });
    expect(within(list).getByText("At or above")).toBeTruthy();
    fireEvent.click(within(list).getByRole("button", { name: "Register" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Useful life (months)"), { target: { value: "36" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Register" }));
    await waitFor(() => expect(seen.find((s) => s.method === "POST" && s.path === "/v1/fixed-assets")?.body).toMatchObject({ source: { document_id: "b2", line: 1 }, life_months: 36, name: "Dell monitor" }));
    expect(await screen.findByText("FA-0003 Dell monitor is in the register.")).toBeTruthy();
  });

  it("checks a sale before recording it, then records it", async () => {
    answer(
      register({
        "POST /v1/fixed-assets/fa1/disposals": (_: unknown, req: Request) => ({ status: req.headers.get("Prefer") ? 200 : 201, body: disposal() }),
      }),
    );
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(within(await screen.findByRole("table", { name: "Assets" })).getByRole("button", { name: "MacBook Pro 14" }));
    fireEvent.click(await screen.findByRole("button", { name: "Sell or write off" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Sold or written off on"), { target: { value: "2026-11-15" } });
    await within(dialog).findByRole("option", { name: "1011 DBS current" });
    fireEvent.change(within(dialog).getByLabelText("Money account"), { target: { value: "a1011" } });
    fireEvent.change(within(dialog).getByLabelText("Amount received (SGD, GST included)"), { target: { value: "2000" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Check" }));
    expect(await within(dialog).findByText("A loss of SGD 1,500.00")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("Prefer")).toBe("return=preview");
    fireEvent.click(within(dialog).getByRole("button", { name: "Record" }));
    expect(await screen.findByText("DSP-0001 records it: a loss of SGD 1,500.00.")).toBeTruthy();
    const sent = seen.filter((s) => s.method === "POST");
    expect(sent[1]?.headers.get("Prefer")).toBeNull();
    expect(sent[1]?.headers.get("If-Match")).toBe('"1-posted"');
    expect(sent[1]?.body).toEqual({ date: "2026-11-15", proceeds: "money", money_account_id: "a1011", amount: sgd("2000") });
  });

  it("asks another person to void a disposal in Founder mode", async () => {
    answer(
      register({
        "GET /v1/asset-disposals": { status: 200, body: { items: [disposal()] } },
        "POST /v1/asset-disposals/d1/void": problem(409, "ASSET-19", "In Founder mode, voiding a disposal needs a second person (FM-1)."),
        "POST /v1/asset-disposals/d1/void-approval": { status: 202, body: { id: "r1" } },
      }),
    );
    renderPage(FixedAssets, { path: "/accounting/assets", session: accountant, organisation, mode: "expert" });
    fireEvent.click(within(await screen.findByRole("table", { name: "Assets" })).getByRole("button", { name: "MacBook Pro 14" }));
    fireEvent.click(await screen.findByRole("button", { name: "Void DSP-0001" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why"), { target: { value: "Not sold after all" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Void" }));
    expect(await screen.findByText("Voiding DSP-0001 waits for another person's approval.")).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/asset-disposals/d1/void-approval")?.body).toMatchObject({ reason: "Not sold after all" });
  });

  it("offers nothing to change to a Bookkeeper, who reads the register", async () => {
    answer(register());
    renderPage(FixedAssets, { path: "/accounting/assets", session: { ...ownerSession, roles: ["bookkeeper"] } as Session, organisation, mode: "expert" });
    fireEvent.click(within(await screen.findByRole("table", { name: "Assets" })).getByRole("button", { name: "MacBook Pro 14" }));
    await screen.findByRole("table", { name: "Depreciation schedule" });
    expect(screen.queryByRole("button", { name: "Sell or write off" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Register one bought earlier" })).toBeNull();
  });
});
