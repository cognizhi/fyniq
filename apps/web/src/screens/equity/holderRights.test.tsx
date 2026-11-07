import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { HolderPage } from "./HolderPage";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const holder = { contact_id: "c-kow", name: "Ah Kow", director: true, shareholder: true, investor: false, recorded: false, holdings: [], history: [], issues: [] };
const rights = {
  id: "ir-1", holder_id: "c-seed", holder_name: "Seedling Ventures Pte. Ltd.", status: "recorded" as const, version: 1, confers_control: true, evidence: 1, checks: [],
  recorded_at: "2026-10-15T09:00:00Z",
  rights: { from: "2026-06-18", board_seats: 2, observer: false, board_majority: true, pro_rata: true, information: "Quarterly management accounts within 45 days",
    consents: ["Issuing new shares"], agreements: "Shareholders' agreement of 18 Jun 2026" },
};
const entry = {
  id: "ce-1", holder_id: "c-kow", holder_name: "Ah Kow", status: "recorded" as const, version: 1, evidence: 1, checks: [], recorded_at: "2026-10-15T09:00:00Z",
  entry: { controller: true, grounds: ["shares" as const, "votes" as const], since: "2025-01-02", company_register_on: "2025-01-05", notices: [] },
};
const line = (extra: object) => ({ holder_id: "c-kow", holder_name: "Ah Kow", shares: true, votes: true, ownership: "70.00", voting: "70.00", since: "2025-01-02", rights: false, ...extra });
const tests = (controllers: object[]) => ({ at: "2026-10-15", controllers, lapsed: [], exempt: { members: 2, entities: [], unknown: [], decided: true, exempt: true, differs: false } });

// req: FR-EQ-22
// req: FR-EQ-25
// req: G4-101
describe("a holder's rights and controller standing", () => {
  it("shows the rights in force, that they could confer control, and the controller test not assessed; axe finds nothing", async () => {
    answer({
      "GET /v1/holders/c-seed": { status: 200, body: { ...holder, contact_id: "c-seed", name: "Seedling Ventures Pte. Ltd.", investor: true, shareholder: false, director: false,
        rights, rights_history: [rights], controller_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([line({ holder_id: "c-seed", holder_name: "Seedling Ventures Pte. Ltd.", shares: false, votes: false,
        ownership: undefined, voting: undefined, since: undefined, rights: true, standing: "not_assessed" })]) },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-seed", route: "/equity/holders/$contactId", session: accountant });
    const card = (await screen.findByRole("heading", { name: "Rights from the agreements" })).closest("section") as HTMLElement;
    expect(within(card).getByText("Could confer control")).toBeTruthy();
    expect(within(card).getByText("Appoints 2")).toBeTruthy();
    expect(within(card).getByText("Appoints or removes a majority of the board")).toBeTruthy();
    expect(within(card).getByText("Issuing new shares")).toBeTruthy();
    expect(within(card).getByText("Shareholders' agreement of 18 Jun 2026, from 18 Jun 2026")).toBeTruthy();
    const ctl = (await screen.findByRole("heading", { name: "Registrable controller" })).closest("section") as HTMLElement;
    expect(await within(ctl).findByText("Not assessed")).toBeTruthy();
    expect(within(ctl).getByText(/could confer control: the company secretary concludes whether they do/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("drafts rights with their consent matters, empty ones left out", async () => {
    const seen = answer({
      "GET /v1/holders/c-seed": { status: 200, body: { ...holder, contact_id: "c-seed", name: "Seedling Ventures Pte. Ltd.", rights_history: [], controller_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([]) },
      "POST /v1/investor-rights": { status: 201, body: { ...rights, status: "draft", evidence: 0 } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-seed", route: "/equity/holders/$contactId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Record rights" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("From the agreements"), { target: { value: "Side letter of 18 Jun 2026" } });
    fireEvent.change(within(dialog).getByLabelText("In force from"), { target: { value: "2026-06-18" } });
    fireEvent.change(within(dialog).getByLabelText("Directors they may appoint"), { target: { value: "1" } });
    fireEvent.click(within(dialog).getByLabelText("A pro-rata right in later rounds"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a consent matter" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a consent matter" }));
    fireEvent.change(within(dialog).getByLabelText("Consent matter 1"), { target: { value: "Issuing new shares " } });
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the rights" }));
    expect(await screen.findByText("The rights are drafted. Attach the agreements, then ask for approval.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/investor-rights")?.body).toEqual({
      holder_id: "c-seed", from: "2026-06-18", board_seats: 1, observer: false, board_majority: false, pro_rata: true, consents: ["Issuing new shares"],
      agreements: "Side letter of 18 Jun 2026",
    });
  });

  it("shows a controller to lodge with ACRA, and records the next version with its central register day and a notice", async () => {
    const seen = answer({
      "GET /v1/holders/c-kow": { status: 200, body: { ...holder, controller: entry, controller_history: [entry], rights_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([line({ standing: "to_lodge", central_register_due: "2025-01-07", entry })]) },
      "POST /v1/controller-entries": { status: 201, body: { ...entry, id: "ce-2", status: "draft", evidence: 0 } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-kow", route: "/equity/holders/$contactId", session: accountant });
    const ctl = (await screen.findByRole("heading", { name: "Registrable controller" })).closest("section") as HTMLElement;
    expect(await within(ctl).findByText("Update ACRA's register by 7 Jan 2025")).toBeTruthy();
    expect(within(ctl).getByText("Holds 70.00% of the shares and 70.00% of the votes since 2 Jan 2025, more than 25%: a registrable controller.")).toBeTruthy();
    expect(within(ctl).getByText("A registrable controller: more than 25% of the shares; more than 25% of the votes")).toBeTruthy();
    fireEvent.click(within(ctl).getByRole("button", { name: "Update the entry" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("ACRA's central register updated"), { target: { value: "2025-01-07" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a notice" }));
    fireEvent.change(within(dialog).getByLabelText("Notice 1: sent on"), { target: { value: "2026-01-04" } });
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the entry" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/controller-entries")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/controller-entries")?.body).toEqual({
      holder_id: "c-kow", controller: true, grounds: ["shares", "votes"], since: "2025-01-02", company_register_on: "2025-01-05", central_register_on: "2025-01-07",
      notices: [{ sent_on: "2026-01-04" }],
    });
  });

  it("records a holder found not to be a controller with the reason, and no register days", async () => {
    const seen = answer({
      "GET /v1/holders/c-seed": { status: 200, body: { ...holder, contact_id: "c-seed", name: "Seedling Ventures Pte. Ltd.", rights, rights_history: [rights], controller_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([line({ holder_id: "c-seed", shares: false, votes: false, since: undefined, rights: true, standing: "not_assessed" })]) },
      "POST /v1/controller-entries": { status: 201, body: { ...entry, id: "ce-3", holder_id: "c-seed", status: "draft", evidence: 0 } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-seed", route: "/equity/holders/$contactId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Record the conclusion" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("radio", { name: "Not a controller" }));
    fireEvent.change(within(dialog).getByLabelText("Why they are not a controller"), { target: { value: "The consent matters protect the investment" } });
    fireEvent.change(within(dialog).getByLabelText("Concluded on"), { target: { value: "2026-10-15" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the entry" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/controller-entries")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/controller-entries")?.body).toEqual({
      holder_id: "c-seed", controller: false, grounds: [], reason: "The consent matters protect the investment", since: "2026-10-15", notices: [],
    });
  });

  it("lists the investments: each share issue with its price and amount, and each SAFE they hold", async () => {
    const issue = { id: "si-1", holder_id: "c-lion", holder_name: "Lion Rock Ventures Pte. Ltd.", class_account_id: "acc-3010", class_code: "3010", shares: "160000",
      price: "0.9375", amount: { amount: "150000.00", currency: "SGD" }, consideration: "cash", resolution_refs: "Board resolution 5 Mar 2027", effective_on: "2027-03-12",
      before_books: false, recorded_at: "2027-03-12T09:00:00Z" };
    const safe = { id: "in-1", holder_id: "c-lion", holder_name: "Lion Rock Ventures Pte. Ltd.", form: "post_money_safe", amount: "500000.00", currency: "SGD", signed_on: "2026-06-18",
      status: "recorded", valuation_cap: "8000000", class_account_id: "acc-3010", class_code: "3010", denominator: "post_money_capitalisation", trigger: "priced_round",
      price_decimals: 4, version: 1, evidence: 1, checks: [] };
    answer({
      "GET /v1/holders/c-lion": { status: 200, body: { ...holder, contact_id: "c-lion", name: "Lion Rock Ventures Pte. Ltd.", investor: true, issues: [issue], rights_history: [],
        controller_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([]) },
      "GET /v1/instruments": { status: 200, body: { items: [safe, { ...safe, id: "in-2", holder_id: "c-other" }] } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-lion", route: "/equity/holders/$contactId", session: accountant });
    const table = await screen.findByRole("table", { name: "Investments" });
    await waitFor(() => expect(within(table).getAllByRole("row")).toHaveLength(3));
    const rows = within(table).getAllByRole("row").map((r) => r.textContent);
    expect(rows[1]).toBe("Shares for cash on 3010Board resolution 5 Mar 202712 Mar 2027160,0000.9375150,000.00");
    expect(rows[2]).toBe("Post-money SAFERecorded · cap S$8,000,00018 Jun 2026––500,000.00");
    await noViolations(document.body);
  });

  it("says a holder under the threshold is no controller, and offers no drafting to an Owner", async () => {
    answer({
      "GET /v1/holders/c-kow": { status: 200, body: { ...holder, rights_history: [], controller_history: [] } },
      "GET /v1/cap-table/statutory-tests": { status: 200, body: tests([]) },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-kow", route: "/equity/holders/$contactId" });
    expect(await screen.findByText(/Not a registrable controller on the cap table's test: 25% or less/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record rights" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Record the conclusion" })).toBeNull();
  });
});
