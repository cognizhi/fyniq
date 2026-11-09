import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { forgetMode, setMode } from "../../mode";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { points, roundBody, blankRound } from "./roundWords";
import { RoundSimulator } from "./RoundSimulator";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const employee = { ...ownerSession, roles: ["employee"] } as Session;
const capital = { id: "acc-3010", code: "3010", name: "Ordinary share capital", type: "equity", active: true, group: false, money_account: false, control: false,
  payroll_cost: false, revalue: false, parent_id: null, version: 1, role: "share-capital" };
const holder = (id: string, name: string, investor: boolean) => ({ contact_id: id, name, director: false, shareholder: !investor, investor, recorded: true, holdings: [] });
const holders = { items: [holder("c-mei", "Mei Lin Tan", false), holder("c-arjun", "Arjun Rao", false), holder("c-lrv", "Lion Rock Ventures", true), holder("c-lead", "Seed Lead Pte. Ltd.", true)] };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const line = (id: string, name: string, before: string, after: string, pb: string | undefined, pa: string, dilution: string, you = false) => ({
  holder_id: id, holder_name: name, issued_before: before, issued_after: after, fully_diluted_before: before, fully_diluted_after: after,
  ownership_before: pb, ownership_after: pa, diluted_before: pb, diluted_after: pa, dilution, pro_rata: before === "0" ? "0" : "100000", ...(you ? { you: true } : {}),
});
// The design's scenario A (docs/design/screens RoundSimulator), as the server would answer it.
const outcome = {
  price: "0.9375", currency: "SGD", pre_money: sgd("10000000.00"), post_money: sgd("11650000.00"), raised: sgd("1650000.00"),
  issues: [
    { holder_id: "c-lead", holder_name: "Seed Lead Pte. Ltd.", shares: "1600000", amount: sgd("1500000.00"), left: sgd("0.00") },
    { holder_id: "c-lrv", holder_name: "Lion Rock Ventures", shares: "160000", amount: sgd("150000.00"), left: sgd("0.00") },
  ],
  conversions: [],
  holders: [
    line("c-arjun", "Arjun Rao", "4500000", "4500000", "42.19", "36.21", "5.98"),
    line("c-lrv", "Lion Rock Ventures", "666666", "826666", "6.25", "6.65", "-0.40"),
    line("c-mei", "Mei Lin Tan", "4500000", "4500000", "42.19", "36.21", "5.98", true),
    line("c-lead", "Seed Lead Pte. Ltd.", "0", "1600000", undefined, "12.88", "-12.88"),
  ],
  issued_before: "9666666", issued_after: "11426666", fully_diluted_before: "10666666", fully_diluted_after: "12426666",
  unallocated_before: "1000000", unallocated_after: "1000000", new_shares: "1760000", pool_diluted_before: "9.38", pool_diluted_after: "8.05", pool_dilution: "1.33",
};
const savedA = {
  id: "rs-a", status: "draft" as const, version: 1, name: "A: S$10M pre-money", on: "2026-10-15", pre_money: "10000000", price_decimals: 4,
  class_account_id: "acc-3010", investors: [{ holder_id: "c-lead", amount: "1500000.00" }, { holder_id: "c-lrv", amount: "150000.00" }], instruments: [],
  resolution_refs: "Board resolution 14 Oct 2026", outcome, problems: [],
};
const today = new Date().toISOString().slice(0, 10);

afterEach(() => forgetMode());

// req: FR-EQ-21
// req: FR-UX-24
describe("a round's body", () => {
  it("sends the basis chosen, plain amounts and counts, and leaves out what is empty", () => {
    const f = blankRound("2026-10-15", "acc-3010", "A");
    f.draft.investors = [{ holder_id: "c-lead", amount: "S$1,500,000" }];
    f.draft.pre_money = "10,000,000";
    f.draft.price = "0.90";
    f.draft.top_up = "500,000";
    expect(roundBody(f)).toEqual({
      name: "A", on: "2026-10-15", price_decimals: 4, class_account_id: "acc-3010", investors: [{ holder_id: "c-lead", amount: "1500000" }], instruments: [],
      pre_money: "10000000", top_up: "500000", top_up_before: true,
    });
    expect(roundBody({ ...f, basis: "price" }).price).toBe("0.90");
    expect(roundBody({ ...f, basis: "price" }).pre_money).toBeUndefined();
    expect([points("5.98"), points("-0.40"), points("0.00"), points(undefined)]).toEqual(["−5.98", "+0.40", "0.00", "–"]);
  });
});

// req: FR-EQ-21
// req: FR-EQ-27
// req: FR-UX-23
describe("the round simulator", () => {
  it("tries a round from a few plain inputs in Basic, saving nothing, and shows what you would own; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/round-scenarios": { status: 200, body: { items: [] } },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/holders": { status: 200, body: holders },
      "POST /v1/round-models": { status: 200, body: outcome },
    });
    setMode("basic");
    renderPage(RoundSimulator, { path: "/equity/rounds", route: "/equity/rounds", session: ownerSession as Session, mode: "basic" });
    expect(await screen.findByRole("heading", { name: "Round simulator" })).toBeTruthy();
    expect(screen.getByText("Sandbox: nothing here is posted or recorded")).toBeTruthy();
    expect(await screen.findByText(/Enter the value before the money/)).toBeTruthy();
    await screen.findByRole("option", { name: "Seed Lead Pte. Ltd." });
    fireEvent.change(screen.getByLabelText("How much you raise"), { target: { value: "1,500,000" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "c-lead" } });
    fireEvent.change(screen.getByLabelText("Value of the company before the money"), { target: { value: "10,000,000" } });
    expect(await screen.findByText("You would own")).toBeTruthy();
    const tried = seen.filter((s) => s.method === "POST" && s.path === "/v1/round-models");
    expect(tried.at(-1)?.body).toEqual({
      name: "Scenario A", on: today, price_decimals: 4, class_account_id: "acc-3010", investors: [{ holder_id: "c-lead", amount: "1500000" }], instruments: [],
      pre_money: "10000000",
    });
    expect(screen.getByText("−5.98 points")).toBeTruthy();
    expect(screen.getByText(/from 42.19% today/)).toBeTruthy();
    expect(screen.getByText("1,600,000")).toBeTruthy();
    expect(screen.getByText(/shares for Seed Lead Pte. Ltd. at S\$0.9375 each for S\$1,500,000.00/)).toBeTruthy();
    expect(screen.getAllByText("42.19 → 36.21%", { selector: ".fy-legend span span" })).toHaveLength(2);
    expect(screen.getByText(/Mei Lin Tan \(you\)/, { selector: ".fy-legend span" })).toBeTruthy();
    expect(screen.getByRole("img", { name: /After the round: Arjun Rao 36.21%, Lion Rock Ventures 6.65%, Mei Lin Tan 36.21%, Seed Lead Pte. Ltd. 12.88%, Option pool, not granted 8.05%/ })).toBeTruthy();
    // An Owner tries rounds; saving one needs the right to draft.
    expect(screen.queryByRole("button", { name: "Save scenario" })).toBeNull();
    expect(screen.getByText(/More options for employees/)).toBeTruthy();
    expect(seen.some((s) => s.path.startsWith("/v1/round-scenarios") && s.method !== "GET")).toBe(false);
    await noViolations(document.body);
  });

  it("says what stops a round in plain words without taking the focus", async () => {
    answer({
      "GET /v1/round-scenarios": { status: 200, body: { items: [] } },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/holders": { status: 200, body: holders },
      "POST /v1/round-models": {
        status: 422,
        body: { title: "Unprocessable Entity", status: 422, detail: "The round does not fit", code: "EQUITY-19",
          errors: [{ field: "body.investors[0].holder_id", message: "Northwind Analytics is not marked investor or shareholder", fix: "Mark the contact an investor in Contacts first" }] },
      },
    });
    setMode("basic");
    renderPage(RoundSimulator, { path: "/equity/rounds", route: "/equity/rounds", session: ownerSession as Session, mode: "basic" });
    await screen.findByRole("option", { name: "Seed Lead Pte. Ltd." });
    const value = screen.getByLabelText("Value of the company before the money");
    fireEvent.change(screen.getByLabelText("How much you raise"), { target: { value: "1500000" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "c-lead" } });
    value.focus();
    fireEvent.change(value, { target: { value: "10000000" } });
    expect(await screen.findByRole("heading", { name: /The round cannot be worked out yet/ })).toBeTruthy();
    expect(screen.getByText(/Investors 1 holder id: Northwind Analytics is not marked investor or shareholder. Mark the contact an investor in Contacts first./)).toBeTruthy();
    expect(document.activeElement).toBe(value);
  });

  it("models a saved scenario in Expert with the pro-forma cap table, saves a change at its version and sends it for approval", async () => {
    const seen = answer({
      "GET /v1/round-scenarios": { status: 200, body: { items: [savedA, { ...savedA, id: "rs-b", name: "B: S$12M pre-money", pre_money: "12000000",
        outcome: { ...outcome, price: "1.1250", raised: sgd("2150000.00"), holders: outcome.holders.map((h) => ({ ...h, diluted_after: h.you ? "34.41" : h.diluted_after })) } }] } },
      "GET /v1/round-scenarios/rs-a": { status: 200, body: savedA, headers: { ETag: `"1-draft"` } },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/holders": { status: 200, body: holders },
      "GET /v1/instruments": { status: 200, body: { items: [] } },
      "POST /v1/round-models": { status: 200, body: outcome },
      "PUT /v1/round-scenarios/rs-a": { status: 200, body: { ...savedA, version: 2, pre_money: "11000000" }, headers: { ETag: `"2-draft"` } },
      "POST /v1/round-scenarios/rs-a/approval": { status: 202, body: { id: "ap-1", status: "pending" } },
    });
    setMode("expert");
    renderPage(RoundSimulator, { path: "/equity/rounds", route: "/equity/rounds", session: accountant });
    expect(await screen.findByRole("radio", { name: "A: S$10M pre-money" })).toBeTruthy();
    expect((await screen.findByLabelText("Pre-money valuation") as HTMLInputElement).value).toBe("10000000");
    const table = await screen.findByRole("table", { name: "Pro-forma cap table, fully diluted" });
    const rows = within(table).getAllByRole("row");
    expect(rows.map((r) => r.textContent)).toEqual([
      "HolderShares nowNow %Shares afterAfter %PointsNew money (SGD)",
      "Arjun Rao4,500,00042.194,500,00036.21−5.98–",
      "Lion Rock Ventures666,6666.25826,6666.65+0.40150,000.00",
      "Mei Lin Tan (you)4,500,00042.194,500,00036.21−5.98–",
      "Seed Lead Pte. Ltd.––1,600,00012.88+12.881,500,000.00",
      "Option pool, not granted1,000,0009.381,000,0008.05−1.33–",
      "Total10,666,66612,426,6661,650,000.00",
    ]);
    expect(screen.getByText("Pre-money S$10,000,000.00 · S$1,650,000.00 new money")).toBeTruthy();
    const compare = screen.getByRole("table", { name: "Side by side" });
    expect(within(compare).getByRole("row", { name: /^You 36.21% 34.41%$/ })).toBeTruthy();
    expect(within(compare).getByRole("row", { name: /Money raised 1,650,000.00 2,150,000.00/ })).toBeTruthy();

    await noViolations(document.body);
    const ask = screen.getByRole("button", { name: "Send for approval" }) as HTMLButtonElement;
    expect(ask.disabled).toBe(false);
    fireEvent.click(ask);
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/round-scenarios/rs-a/approval")).toBe(true));
    expect(await screen.findByText(/Sent for approval/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Pre-money valuation"), { target: { value: "11,000,000" } });
    expect((screen.getByRole("button", { name: "Send for approval" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save scenario" }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe(`"1-draft"`);
    expect(put.body).toMatchObject({ name: "A: S$10M pre-money", pre_money: "11000000", resolution_refs: "Board resolution 14 Oct 2026",
      investors: [{ holder_id: "c-lead", amount: "1500000.00" }, { holder_id: "c-lrv", amount: "150000.00" }] });
  });

  it("is refused to an Employee, who sees why", async () => {
    answer({
      "GET /v1/round-scenarios": { status: 403, body: { title: "Forbidden", status: 403, detail: "Your role does not allow this", code: "API-AUTHZ-01" } },
      "GET /v1/accounts": { status: 403, body: { title: "Forbidden", status: 403, code: "API-AUTHZ-01" } },
    });
    renderPage(RoundSimulator, { path: "/equity/rounds", route: "/equity/rounds", session: employee });
    expect(await screen.findByText("The round simulator could not be shown")).toBeTruthy();
  });
});
