import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { TransactionNew } from "./TransactionNew";
import { TransactionPage } from "./TransactionPage";
import { EquityTransactions } from "./Transactions";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const fund = { contact_id: "c-fund", name: "Seedling Ventures Pte. Ltd.", director: false, shareholder: false, investor: true, recorded: false, holdings: [] };
const mei = { contact_id: "c-mei", name: "Mei Lin Tan", director: true, shareholder: true, investor: false, recorded: true,
  holdings: [{ account_code: "3010", class_name: "Ordinary shares", shares: "10000" }] };
const base = { version: 2, checks: [], movements: [], entries: [], evidence: 1, class_code: "3010" };
const issue = { ...base, id: "eq-1", number: "EQ-0001", status: "approved" as const, approved_on: "2026-10-15",
  details: { kind: "issue_cash" as const, holder_id: "c-fund", class_account_id: "acc-3010", shares: "250000", price: "2.40", amount: "600000.00",
    resolution_refs: "Board resolution 15 Oct 2026" },
  parties: [{ contact_id: "c-fund", name: "Seedling Ventures Pte. Ltd.", role: "subscriber" as const }], amount: sgd("600000.00"), paid: sgd("600000.00"),
  acra: { agm_due_on: "2027-06-30", steps: [{ id: "st-1", kind: "resolved" as const, on: "2026-10-15", recorded_at: "2026-10-15T09:00:00Z" }],
    flags: [{ code: "EQUITY-11", kind: "due" as const, message: "the shareholders' approval to issue the shares is due to be lodged with ACRA by 29 Oct 2026",
      fix: "Lodge it, attach ACRA's acknowledgement and record the day it was lodged", due_on: "2026-10-29" }] } };
const transfer = { ...base, id: "eq-2", number: "EQ-0002", status: "effective" as const, effective_on: "2026-10-15", class_code: "3010",
  details: { kind: "transfer" as const, seller_id: "c-mei", buyer_id: "c-fund", class_account_id: "acc-3010", shares: "500" },
  parties: [{ contact_id: "c-mei", name: "Mei Lin Tan", role: "seller" as const }, { contact_id: "c-fund", name: "Seedling Ventures Pte. Ltd.", role: "buyer" as const }],
  movements: [{ id: "m-1", kind: "transfer" as const, holder_id: "c-mei", holder_name: "Mei Lin Tan", class_code: "3010", shares: "-500", effective_on: "2026-10-15" },
    { id: "m-2", kind: "transfer" as const, holder_id: "c-fund", holder_name: "Seedling Ventures Pte. Ltd.", class_code: "3010", shares: "500", effective_on: "2026-10-15" }],
  acra: { steps: [], flags: [] } };
const account = (id: string, code: string, name: string) => ({ id, code, name, type: "equity", active: true, group: false, money_account: false, control: false,
  payroll_cost: false, revalue: false, parent_id: null, version: 1, role: "share-capital" });

// req: FR-EQ-17
// req: FR-EQ-09
// req: FR-EQ-27
describe("the equity transactions", () => {
  it("lists them with their status and ACRA flags, and the register below; axe finds nothing", async () => {
    answer({
      "GET /v1/equity-transactions": { status: 200, body: { items: [transfer, issue] } },
      "GET /v1/holders": { status: 200, body: { items: [mei, fund] } },
      "GET /v1/share-issues": { status: 200, body: { items: [] } },
    });
    renderPage(EquityTransactions, { path: "/equity/transactions", session: accountant });
    const table = await screen.findByRole("table", { name: "Equity transactions" });
    expect(within(table).getByRole("link", { name: "EQ-0001" })).toBeTruthy();
    expect(within(table).getByText("At ACRA")).toBeTruthy();
    expect(within(table).getByText("Due")).toBeTruthy();
    expect(within(table).getByText("Mei Lin Tan (seller), Seedling Ventures Pte. Ltd. (buyer)")).toBeTruthy();
    expect(screen.getByRole("link", { name: "New transaction" }).getAttribute("href")).toBe("/equity/transactions/new");
    expect(await screen.findByRole("heading", { name: "Register of share issues" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Effective 1" }));
    expect(within(table).queryByText("EQ-0001")).toBeNull();
  });

  it("drafts a cash issue to one subscriber, saying what it will post", async () => {
    const seen = answer({
      "GET /v1/holders": { status: 200, body: { items: [mei, fund] } },
      "GET /v1/accounts": { status: 200, body: { items: [account("acc-3010", "3010", "Ordinary share capital")] } },
      "GET /v1/share-classes": { status: 200, body: { items: [] } },
      "POST /v1/equity-transactions": { status: 201, body: { ...issue, status: "draft", number: undefined } },
    });
    renderPage(TransactionNew, { path: "/equity/transactions/new", session: accountant });
    const form = await screen.findByRole("form", { name: "New equity transaction" });
    await within(form).findByRole("option", { name: "Seedling Ventures Pte. Ltd." });
    await within(form).findByRole("option", { name: "3010 Ordinary share capital" });
    fireEvent.change(within(form).getByLabelText("Subscriber"), { target: { value: "c-fund" } });
    fireEvent.change(within(form).getByLabelText("Share capital"), { target: { value: "acc-3010" } });
    fireEvent.change(within(form).getByLabelText("Shares"), { target: { value: "250,000" } });
    fireEvent.change(within(form).getByLabelText("Price per share"), { target: { value: "2.40" } });
    fireEvent.change(within(form).getByLabelText("Resolutions"), { target: { value: "Board resolution 15 Oct 2026" } });
    expect(within(form).getByRole("note").textContent).toContain("Dr 2040 Share application monies, naming the subscriber · Cr 3010");
    await noViolations(document.body);
    fireEvent.click(within(form).getByRole("button", { name: "Draft the transaction" }));
    await screen.findByRole("form", { name: "New equity transaction" });
    await new Promise((r) => setTimeout(r, 0));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions")?.body).toEqual({
      kind: "issue_cash", holder_id: "c-fund", class_account_id: "acc-3010", shares: "250000", price: "2.40", resolution_refs: "Board resolution 15 Oct 2026",
    });
  });

  it("asks for approval of a draft at its version", async () => {
    const draft = { ...issue, status: "draft" as const, number: undefined, approved_on: undefined, acra: undefined, paid: undefined, version: 3 };
    const seen = answer({
      "GET /v1/equity-transactions/eq-1": { status: 200, body: draft, headers: { ETag: '"3-draft"' } },
      "GET /v1/equity-transactions/eq-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/equity-transactions/eq-1/approval": { status: 202, body: { id: "ap-1", status: "pending" } },
    });
    renderPage(TransactionPage, { path: "/equity/transactions/eq-1", route: "/equity/transactions/$transactionId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions/eq-1/approval")?.headers.get("If-Match")).toBe('"3-draft"');
  });

  it("records an ACRA step and the effective date of an approved issue; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/equity-transactions/eq-1": { status: 200, body: issue, headers: { ETag: '"2-posted"' } },
      "GET /v1/equity-transactions/eq-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/equity-transactions/eq-1/acra-steps": { status: 201, body: issue },
      "POST /v1/equity-transactions/eq-1/effective": { status: 200, body: { ...issue, status: "effective", effective_on: "2026-10-15" } },
    });
    renderPage(TransactionPage, { path: "/equity/transactions/eq-1", route: "/equity/transactions/$transactionId" });
    expect(await screen.findByRole("heading", { name: "EQ-0001: Issue for cash" })).toBeTruthy();
    expect(screen.getByText("Paid into share application monies: 600,000.00 of 600,000.00 SGD. It takes effect only once paid.")).toBeTruthy();
    await noViolations(document.body);
    const steps = screen.getByRole("form", { name: "Record an ACRA step" });
    fireEvent.change(within(steps).getByLabelText("Step"), { target: { value: "approval_lodged" } });
    fireEvent.change(within(steps).getByLabelText("On"), { target: { value: "2026-10-15" } });
    fireEvent.click(within(steps).getByRole("button", { name: "Record the step" }));
    expect(await screen.findByText("Recorded: Approval lodged with ACRA.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions/eq-1/acra-steps")?.body).toEqual({ kind: "approval_lodged", on: "2026-10-15" });
    fireEvent.change(screen.getByLabelText("Effective date"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Record the effective date" }));
    expect(await screen.findByText("Effective on 15 Oct 2026: the holdings moved.")).toBeTruthy();
    const call = seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions/eq-1/effective");
    expect(call?.headers.get("If-Match")).toBe('"2-posted"');
    expect(call?.body).toEqual({ effective_on: "2026-10-15" });
  });

  it("shows an effective transfer's movements and offers its reversal to the Accountant only", async () => {
    answer({
      "GET /v1/equity-transactions/eq-2": { status: 200, body: transfer, headers: { ETag: '"2-posted"' } },
      "GET /v1/equity-transactions/eq-2/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(TransactionPage, { path: "/equity/transactions/eq-2", route: "/equity/transactions/$transactionId", session: accountant });
    const moved = await screen.findByRole("table", { name: "Holdings it moved" });
    expect(within(moved).getByText("−500")).toBeTruthy();
    expect(screen.getByText("No entry in the company's books")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Reverse it" }).getAttribute("href")).toBe("/equity/transactions/new?reverses=eq-2");
    expect(screen.queryByRole("form", { name: "Record an ACRA step" })).toBeNull();
  });

  // req: FR-EQ-18
  it("drafts the SAFE conversions at an approved round, and shows what the accountant completes; axe finds nothing", async () => {
    const conversion = { ...base, id: "eq-3", status: "draft" as const, version: 1, evidence: 0,
      details: { kind: "instrument_conversion" as const, instrument_id: "in-1", round_id: "eq-1", holder_id: "c-hatch", class_account_id: "acc-3010",
        shares: "52083", price: "2.40", review: "it follows no template; a discount" },
      parties: [{ contact_id: "c-hatch", name: "Hatch Capital Pte. Ltd.", role: "subscriber" as const }], amount: sgd("124999.20") };
    const seen = answer({
      "GET /v1/equity-transactions/eq-1": { status: 200, body: issue, headers: { ETag: '"2-posted"' } },
      "GET /v1/equity-transactions/eq-1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/equity-transactions/eq-1/conversions": { status: 201, body: { items: [conversion] } },
      "GET /v1/equity-transactions/eq-3": { status: 200, body: conversion, headers: { ETag: '"1-draft"' } },
      "GET /v1/equity-transactions/eq-3/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(TransactionPage, { path: "/equity/transactions/eq-1", route: "/equity/transactions/$transactionId", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Draft the SAFE conversions" }));
    expect(await screen.findByText("Drafted 1 conversion: open each in Transactions, attach the resolutions and ask for approval.")).toBeTruthy();
    const call = seen.find((s) => s.method === "POST" && s.path === "/v1/equity-transactions/eq-1/conversions");
    expect(call?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(call?.headers.get("If-Match")).toBe('"2-posted"');

    cleanup();
    renderPage(TransactionPage, { path: "/equity/transactions/eq-3", route: "/equity/transactions/$transactionId", session: accountant });
    expect(await screen.findByRole("heading", { name: "SAFE or note conversion" })).toBeTruthy();
    expect(screen.getByText(/The accountant completes it: it follows no template; a discount\./)).toBeTruthy();
    expect(screen.getByText("Conversion price per share")).toBeTruthy();
    expect(screen.getByText("124,999.20 SGD")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Draft the SAFE conversions" })).toBeNull();
    await noViolations(document.body);
  });
});
