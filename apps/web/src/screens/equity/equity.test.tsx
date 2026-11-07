import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { HolderPage } from "./HolderPage";
import { Holders } from "./Holders";
import { ShareClasses } from "./ShareClasses";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const mei = { contact_id: "c-mei", name: "Mei Lin Tan", director: true, shareholder: true, investor: false, kind: "individual" as const, identifier: "NRIC ending 567D",
  recorded: true, holdings: [{ account_code: "3010", class_id: "cl-ord", class_name: "Ordinary shares", shares: "10000" }] };
const fund = { contact_id: "c-fund", name: "Seedling Ventures Pte. Ltd.", director: false, shareholder: false, investor: true, recorded: false, pending_record_id: "hr-2", holdings: [] };
const recorded = { id: "hr-1", contact_id: "c-mei", status: "recorded" as const, version: 2, evidence: 1, checks: [], recorded_at: "2026-11-02T09:00:00Z",
  details: { kind: "individual" as const, id_type: "nric" as const, id_last4: "567D", nationality: "Singaporean", address: "1 Example Road, Singapore 000001", contact_people: [] } };
const issue = { id: "si-1", holder_id: "c-mei", holder_name: "Mei Lin Tan", class_account_id: "acc-3010", class_code: "3010", shares: "10000", price: "1.00",
  amount: { amount: "10000.00", currency: "SGD" }, consideration: "cash", resolution_refs: "Incorporation subscription", effective_on: "2025-01-02", before_books: true, recorded_at: "2026-11-03T09:00:00Z" };
const detail = (extra = {}) => ({ ...mei, record: recorded, history: [recorded], issues: [issue], rights_history: [], controller_history: [], ...extra });

// req: FR-EQ-15
// req: FR-EQ-27
describe("the holders", () => {
  it("lists shareholders and investors with their identifier and shares, and filters them; axe finds nothing", async () => {
    answer({ "GET /v1/holders": { status: 200, body: { items: [mei, fund] } } });
    renderPage(Holders, { path: "/equity/holders" });
    const table = await screen.findByRole("table", { name: "Holders" });
    expect(within(table).getByRole("link", { name: "Mei Lin Tan" })).toBeTruthy();
    expect(within(table).getByText("NRIC ending 567D")).toBeTruthy();
    expect(within(table).getByText("10,000 ordinary shares")).toBeTruthy();
    expect(within(table).getByText("No shares yet")).toBeTruthy();
    expect(within(table).getByText("Draft")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add a holder" }).getAttribute("href")).toBe("/contacts");
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Investors 1" }));
    expect(within(table).queryByText("Mei Lin Tan")).toBeNull();
    expect(within(table).getByText("Seedling Ventures Pte. Ltd.")).toBeTruthy();
  });
});

// req: FR-EQ-15
// req: CMP-PD-07
describe("a holder's record", () => {
  it("drafts the details with the last 4 characters only; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/holders/c-mei": { status: 200, body: detail() },
      "POST /v1/holder-records": { status: 201, body: { ...recorded, id: "hr-3", status: "draft", version: 1, evidence: 0 } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-mei", route: "/equity/holders/$contactId", session: accountant });
    expect(await screen.findByRole("heading", { name: "Details in force" })).toBeTruthy();
    expect(screen.getByText("1 Example Road, Singapore 000001")).toBeTruthy();
    expect(screen.getByRole("cell", { name: /Shares for cash on 3010/ })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "10,000.00" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Change the details" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Last 4 characters"), { target: { value: "890a" } });
    fireEvent.change(within(dialog).getByLabelText("Nationality"), { target: { value: "Singaporean" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the record" }));
    expect(await screen.findByText("The record is drafted. Attach the identity document, then ask for approval.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/holder-records")?.body).toEqual({
      contact_id: "c-mei", kind: "individual", id_type: "nric", id_last4: "890A", nationality: "Singaporean", address: "1 Example Road, Singapore 000001", contact_people: [],
    });
  });

  it("asks for approval of a draft with its identity document, at the draft's version", async () => {
    const draft = { ...recorded, id: "hr-3", status: "draft" as const, version: 3, evidence: 1, recorded_at: undefined };
    const seen = answer({
      "GET /v1/holders/c-mei": { status: 200, body: detail({ history: [draft, recorded], pending_record_id: "hr-3" }) },
      "GET /v1/holder-records/hr-3/evidence": { status: 200, body: { items: [] } },
      "POST /v1/holder-records/hr-3/approval": { status: 202, body: { id: "ap-1", status: "pending" } },
    });
    renderPage(HolderPage, { path: "/equity/holders/c-mei", route: "/equity/holders/$contactId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/holder-records/hr-3/approval")?.headers.get("If-Match")).toBe('"3-draft"');
  });

  it("offers drafting only to the Accountant", async () => {
    answer({ "GET /v1/holders/c-mei": { status: 200, body: detail() } });
    renderPage(HolderPage, { path: "/equity/holders/c-mei", route: "/equity/holders/$contactId" });
    await screen.findByRole("heading", { name: "Details in force" });
    expect(screen.queryByRole("button", { name: "Change the details" })).toBeNull();
  });
});

const ordinary = { id: "cl-ord", version_id: "cl-ord", name: "Ordinary shares", kind: "ordinary" as const, account_id: "acc-3010", account_code: "3010", status: "recorded" as const,
  version: 2, evidence: 1, checks: [], votes_per_share: "1", liquidation: {}, dividend: { kind: "none" as const }, anti_dilution: "none" as const, recorded_at: "2026-11-02T09:00:00Z" };
const seriesA = { ...ordinary, id: "cl-a", version_id: "cl-a", name: "Series A preference shares", kind: "preference" as const, series: "A", account_id: "acc-3020", account_code: "3020",
  status: "draft" as const, version: 1, liquidation: { multiple: "1", participating: false, seniority: 1 }, dividend: { kind: "non_cumulative" as const }, anti_dilution: "broad_based" as const,
  conversion_ratio: "1", pro_rata: true, recorded_at: undefined };
const account = (id: string, code: string, name: string) => ({ id, code, name, type: "equity", active: true, group: false, money_account: false, control: false, payroll_cost: false, revalue: false,
  parent_id: null, version: 1, role: "share-capital" });

// req: FR-EQ-16
describe("the share classes", () => {
  it("shows each class's rights and drafts a class; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/share-classes": { status: 200, body: { items: [ordinary, seriesA] } },
      "GET /v1/share-classes/cl-a/evidence": { status: 200, body: { items: [] } },
      "GET /v1/accounts": { status: 200, body: { items: [account("acc-3010", "3010", "Ordinary share capital"), account("acc-3020", "3020", "Preference share capital")] } },
      "POST /v1/share-classes": { status: 201, body: { ...ordinary, id: "cl-b", version_id: "cl-b", name: "Founders' shares", status: "draft", version: 1, evidence: 0 } },
    });
    renderPage(ShareClasses, { path: "/equity/classes", session: accountant });
    expect(await screen.findByRole("heading", { name: /Series A preference shares/ })).toBeTruthy();
    expect(screen.getByText("Broad-based weighted average")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Draft a class" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: /3010/ });
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Founders' shares" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the class" }));
    expect(await screen.findByText("The class is drafted. Attach the constitution, then ask for approval.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/share-classes")?.body).toEqual({
      name: "Founders' shares", kind: "ordinary", account_id: "acc-3010", votes_per_share: "1", liquidation: {}, dividend: { kind: "none" }, anti_dilution: "none",
    });
  });

  it("asks for approval of a draft class at its version", async () => {
    const seen = answer({
      "GET /v1/share-classes": { status: 200, body: { items: [seriesA] } },
      "GET /v1/share-classes/cl-a/evidence": { status: 200, body: { items: [] } },
      "POST /v1/share-classes/cl-a/approval": { status: 202, body: { id: "ap-2", status: "pending" } },
    });
    renderPage(ShareClasses, { path: "/equity/classes", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval of Series A preference shares. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/share-classes/cl-a/approval")?.headers.get("If-Match")).toBe('"1-draft"');
  });
});
