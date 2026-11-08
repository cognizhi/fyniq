import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { InstrumentNew } from "./InstrumentNew";
import { InstrumentPage } from "./InstrumentPage";
import { Instruments } from "./Instruments";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const safe = {
  id: "in-1", status: "recorded" as const, version: 1, holder_name: "Hatch Capital Pte. Ltd.", currency: "SGD", class_code: "3010", evidence: 1, checks: [],
  form: "post_money_safe" as const, holder_id: "c-hatch", amount: "125000.00", signed_on: "2026-06-18", valuation_cap: "2500000", trigger: "priced_round" as const,
  class_account_id: "acc-3010", denominator: "post_money_capitalisation" as const, price_decimals: 4, template: "yc-sg-post-money-cap@1", template_enabled: true,
  classification: "liability" as const, held: sgd("125000.00"),
};
const note = {
  ...safe, id: "in-2", status: "draft" as const, holder_name: "Ah Kow", form: "convertible_note" as const, valuation_cap: undefined, template: undefined,
  template_enabled: false, classification: undefined, interest_rate: "8", maturity_on: "2028-06-18", held: undefined, evidence: 0,
  checks: [{ code: "EQUITY-06", field: "evidence", message: "No evidence is attached", fix: "Attach the signed instrument" }],
};
const template = {
  key: "yc-sg-post-money-cap@1", name: "SAFE: Valuation Cap, No Discount (Singapore)", form: "post_money_safe", enabled: false,
  covers: "A post-money valuation cap, no discount, converting into shares at the next priced round.", source: "Y Combinator's Singapore SAFE [verify]",
};
const round = {
  id: "eq-9", number: "EQ-0009", status: "approved" as const, version: 2, checks: [], movements: [], entries: [], evidence: 1, class_code: "3010",
  details: { kind: "issue_cash" as const, holder_id: "c-seed", class_account_id: "acc-3010", shares: "200000", price: "3.00" },
  parties: [{ contact_id: "c-seed", name: "Seedling Ventures Pte. Ltd.", role: "subscriber" as const }],
};
const preview = {
  round_id: "eq-9", round_price: "3.00", currency: "SGD", shares_before: "1000000", post_money_part: "5.00", capitalisation: "1052631.58",
  conversions: [{ instrument_id: "in-1", holder_name: "Hatch Capital Pte. Ltd.", form: "post_money_safe", amount: sgd("125000.00"), ownership_at_cap: "5.00",
    cap_price: "2.375", price_decimals: 4, price: "2.375", shares: "52631", converted: sgd("124998.63"), left: sgd("1.37"), covered: true }],
};
const capital = { id: "acc-3010", code: "3010", name: "Ordinary share capital", type: "equity", active: true, group: false, money_account: false, control: false,
  payroll_cost: false, revalue: false, parent_id: null, version: 1, role: "share-capital" };

// req: FR-EQ-18
// req: FR-EQ-20
describe("the SAFEs and convertible notes", () => {
  it("lists each with its terms, what 2700 holds and where it stands; axe finds nothing", async () => {
    answer({
      "GET /v1/instruments": { status: 200, body: { items: [safe, { ...note, approval_id: "ap-1" }, { ...safe, id: "in-3", holder_name: "Lion Rock", conversion: { transaction_id: "eq-4", number: "EQ-0004", status: "effective" } }] } },
      "GET /v1/instrument-templates": { status: 200, body: { items: [template] } },
      "GET /v1/equity-transactions": { status: 200, body: { items: [round] } },
    });
    renderPage(Instruments, { path: "/equity/instruments", route: "/equity/instruments", session: accountant });
    const table = await screen.findByRole("table", { name: "SAFEs and convertible notes" });
    const rows = within(table).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement, HTMLElement];
    expect(within(rows[1]).getByText("Post-money SAFE")).toBeTruthy();
    expect(within(rows[1]).getByText(/^cap 2,500,000/)).toBeTruthy();
    expect(within(rows[1]).getByText("Liability · 2700")).toBeTruthy();
    expect(within(rows[1]).getByText("Recorded")).toBeTruthy();
    expect(within(rows[2]).getByText("Waiting for approval")).toBeTruthy();
    expect(within(rows[2]).getByText("8% a year, matures 18 Jun 2028")).toBeTruthy();
    expect(within(rows[3]).getByText("Converted")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add instrument" }).getAttribute("href")).toBe("/equity/instruments/new");
    expect(await screen.findByText("SAFE: Valuation Cap, No Discount (Singapore)")).toBeTruthy();
    await noViolations(document.body);
  });

  it("previews the conversions at an approved round, step by step, and drafts them there", async () => {
    const seen = answer({
      "GET /v1/instruments": { status: 200, body: { items: [safe] } },
      "GET /v1/instrument-templates": { status: 200, body: { items: [{ ...template, enabled: true, enablement_status: "recorded" }] } },
      "GET /v1/equity-transactions": { status: 200, body: { items: [round] } },
      "GET /v1/instruments/conversion-preview": { status: 200, body: preview },
      "GET /v1/equity-transactions/eq-9": { status: 200, body: round, headers: { ETag: '"2-posted"' } },
      "POST /v1/equity-transactions/eq-9/conversions": { status: 201, body: { items: [{ ...round, id: "eq-10" }] } },
    });
    renderPage(Instruments, { path: "/equity/instruments", route: "/equity/instruments", session: accountant });
    const form = await screen.findByRole("form", { name: "Preview the conversions" });
    await screen.findByRole("option", { name: /EQ-0009/ });
    fireEvent.change(within(form).getByLabelText("At the round"), { target: { value: "eq-9" } });
    fireEvent.click(within(form).getByRole("button", { name: "Preview" }));
    const card = await screen.findByRole("region", { name: "Hatch Capital Pte. Ltd.'s conversion" });
    expect(seen.find((s) => s.path === "/v1/instruments/conversion-preview")?.query).toBe("?round_id=eq-9");
    expect(screen.getByText(/The post-money SAFEs take 5\.00% of the company, so its capitalisation once they convert is 1,052,631\.58\./)).toBeTruthy();
    expect(within(card).getByText("Cap price: 2.375.")).toBeTruthy();
    expect(within(card).getByText("Whole shares (RND-SHARE): 52,631, for 124,998.63 SGD.")).toBeTruthy();
    expect(within(card).getByText("Remainder: 1.37 SGD stays on 2700 until it is settled under the terms.")).toBeTruthy();
    expect(within(card).getByText("Automatic")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Draft the SAFE conversions" }));
    expect(await screen.findByText("Drafted 1 conversion: open each in Transactions, attach the resolutions and ask for approval.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions/eq-9/conversions")?.headers.get("If-Match")).toBe('"2-posted"');
  });

  it("previews at a price typed in, and shows what the accountant completes", async () => {
    const seen = answer({
      "GET /v1/instruments": { status: 200, body: { items: [safe] } },
      "GET /v1/instrument-templates": { status: 200, body: { items: [template] } },
      "GET /v1/equity-transactions": { status: 200, body: { items: [] } },
      "GET /v1/instruments/conversion-preview": { status: 200, body: { ...preview, round_id: undefined, round_price: "2.00",
        conversions: [{ ...preview.conversions[0], price: "2.00", shares: "62500", converted: sgd("125000.00"), left: sgd("0.00"), covered: false,
          review: "the accountant completes it: its template is not enabled, the round's price is below its cap price" }] } },
    });
    renderPage(Instruments, { path: "/equity/instruments", route: "/equity/instruments", session: accountant });
    const form = await screen.findByRole("form", { name: "Preview the conversions" });
    fireEvent.change(within(form).getByLabelText("Round price per share"), { target: { value: "2.00" } });
    fireEvent.click(within(form).getByRole("button", { name: "Preview" }));
    expect(await screen.findByText(/The accountant completes it: its template is not enabled, the round's price is below its cap price\./)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/instruments/conversion-preview")?.query).toBe("?price=2.00");
    expect(screen.getByText("Accountant")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Draft the SAFE conversions" })).toBeNull();
  });

  it("enables a template with its lawyer's confirmation, on another person's approval", async () => {
    const seen = answer({
      "GET /v1/instruments": { status: 200, body: { items: [] } },
      "GET /v1/instrument-templates": { status: 200, body: { items: [{ ...template, enablement_id: "en-1", enablement_status: "draft", version: 1, evidence: 1 }] } },
      "GET /v1/equity-transactions": { status: 200, body: { items: [] } },
      "GET /v1/instrument-template-enablements/en-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/instrument-template-enablements/en-1/approval": { status: 202, body: { id: "ap-2", status: "pending" } },
    });
    renderPage(Instruments, { path: "/equity/instruments", route: "/equity/instruments", session: accountant });
    const region = await screen.findByRole("region", { name: "Forms Fyniq converts" });
    fireEvent.click(await within(region).findByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/instrument-template-enablements/en-1/approval")?.headers.get("If-Match")).toBe('"1-draft"');
  });

  it("drafts a discount-only SAFE with its own terms only", async () => {
    const seen = answer({
      "GET /v1/holders": { status: 200, body: { items: [{ contact_id: "c-hatch", name: "Hatch Capital Pte. Ltd.", director: false, shareholder: false, investor: true, recorded: false, holdings: [] }] } },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/instrument-templates": { status: 200, body: { items: [template] } },
      "POST /v1/instruments": { status: 201, body: { ...safe, id: "in-9", status: "draft" } },
    });
    renderPage(InstrumentNew, { path: "/equity/instruments/new", route: "/equity/instruments/new", session: accountant });
    await screen.findByRole("option", { name: "Hatch Capital Pte. Ltd." });
    fireEvent.change(screen.getByLabelText("Holder"), { target: { value: "c-hatch" } });
    fireEvent.change(screen.getByLabelText("Form"), { target: { value: "discount_safe" } });
    fireEvent.change(screen.getByLabelText("Amount paid"), { target: { value: "50,000.00" } });
    fireEvent.change(screen.getByLabelText("Signed on"), { target: { value: "2026-07-01" } });
    fireEvent.change(screen.getByLabelText("Discount"), { target: { value: "15" } });
    expect(screen.queryByLabelText("Valuation cap")).toBeNull();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Draft the instrument" }));
    await screen.findByRole("form", { name: "New SAFE or note" });
    await new Promise((r) => setTimeout(r, 0));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/instruments")?.body).toEqual({
      form: "discount_safe", holder_id: "c-hatch", amount: "50000.00", signed_on: "2026-07-01", discount: "15", trigger: "priced_round",
      class_account_id: "acc-3010", denominator: "pre_money_shares", price_decimals: 4,
    });
  });

  it("asks for a drafted note's approval at its version once its signed copy is attached; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/instruments/in-2": { status: 200, body: { ...note, evidence: 1, checks: [] }, headers: { ETag: '"1-draft"' } },
      "GET /v1/instruments/in-2/evidence": { status: 200, body: { items: [] } },
      "POST /v1/instruments/in-2/approval": { status: 202, body: { id: "ap-3", status: "pending" } },
    });
    renderPage(InstrumentPage, { path: "/equity/instruments/in-2", route: "/equity/instruments/$instrumentId", session: accountant });
    expect(await screen.findByRole("heading", { name: "Convertible note: Ah Kow" })).toBeTruthy();
    expect(screen.getByText("8% a year, matures 18 Jun 2028")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/instruments/in-2/approval")?.headers.get("If-Match")).toBe('"1-draft"');
    cleanup();
    answer({
      "GET /v1/instruments/in-1": { status: 200, body: { ...safe, conversion: { transaction_id: "eq-4", number: "EQ-0004", status: "effective" } }, headers: { ETag: '"1-posted"' } },
      "GET /v1/instruments/in-1/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(InstrumentPage, { path: "/equity/instruments/in-1", route: "/equity/instruments/$instrumentId", session: accountant });
    expect((await screen.findByRole("link", { name: "Its conversion, EQ-0004" })).getAttribute("href")).toBe("/equity/transactions/eq-4");
    expect(screen.getAllByText("125,000.00 SGD", { selector: "dd" })).toHaveLength(2); // paid, and held on 2700 today
  });
});
