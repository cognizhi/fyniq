import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen, type Answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { personalOnly } from "../Home";
import { checkClaim, ClaimEditor, newReceipt, type ClaimForm } from "./ClaimEditor";
import type { Advance, CardLine, Claim } from "./claims";
import { checkTrips, MileageClaim, newTrip } from "./MileageClaim";
import { claimsCSV, MyClaims } from "./MyClaims";

const me = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01";
const employee = { ...ownerSession, user_id: me, roles: ["employee"] } as Session;
const organisation = { base_currency: "SGD", team_mode: "founder" } as never;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const claim = (extra: Partial<Claim> = {}): Claim => ({
  id: "c1",
  number: "CLM-0001",
  status: "draft",
  version: 1,
  claimant: { kind: "user", id: me, name: "Ah Kow" },
  stage: "draft",
  date: "2026-10-15",
  title: "Acme visit",
  lines: [{ kind: "receipt", date: "2026-10-14", merchant: "ComfortDelGro", category_id: "travel", amount: sgd("27.30"), gst_code: "NR" }],
  total: sgd("27.30"),
  input_tax_decisions: [],
  checks: [],
  ledger_impact: [],
  ...extra,
});
const categories = {
  items: [
    { id: "travel", name: "Local travel", account_id: "a6100", mileage: false, active: true, version: 1 },
    { id: "meals", name: "Meals", account_id: "a6530", mileage: false, active: true, version: 1, limit: sgd("50.00") },
    { id: "mileage", name: "Mileage", account_id: "a6540", mileage: true, active: true, version: 1 },
  ],
};
const codes = { version: "sg-gst@2024.1", rate: "0.09", codes: [{ code: "TX", description: "Standard-rated purchase", claimable: true }, { code: "NR", description: "Not GST-registered", claimable: false }] };
const line = (extra: Partial<CardLine> = {}): CardLine => ({
  id: "l1",
  money_account_id: "m1",
  card: "Corporate Visa",
  booking_date: "2026-10-14",
  amount: sgd("-27.30"),
  counterparty: "Grab",
  status: "unreconciled",
  version: 1,
  ...extra,
});
const advance = (extra: Partial<Advance> = {}): Advance => ({
  id: "a1",
  number: "ADV-0001",
  kind: "advance",
  status: "posted",
  version: 1,
  person: { kind: "user", id: me, name: "Ah Kow" },
  date: "2026-10-05",
  money_account_id: "m2",
  amount: sgd("800.00"),
  open: sgd("57.85"),
  settled: sgd("742.15"),
  refunded: sgd("0.00"),
  settled_by: [],
  checks: [],
  ledger_impact: [],
  ...extra,
});

// req: FR-AP-14
// req: FR-AP-18
// req: FR-UX-39
describe("my claims", () => {
  it("lands a person whose rights are personal only on My claims", () => {
    expect(personalOnly(["employee"])).toBe(true);
    expect(personalOnly(["employee", "approver"])).toBe(false);
    expect(personalOnly([])).toBe(false);
  });

  it("lists the member's claims by stage, what is owed, and a returned claim's comment first; axe finds nothing", async () => {
    answer({
      "GET /v1/expense-claims": {
        status: 200,
        body: {
          items: [
            claim(),
            claim({ id: "c2", number: "CLM-0002", title: "Client lunch", stage: "returned", total: sgd("64.80"), returned: { by: { kind: "user", id: "u9", name: "Wei Ling" }, at: "2026-10-15T01:00:00Z", comment: "Add who attended." } }),
            claim({ id: "c3", number: "CLM-0003", title: "Conference ticket", status: "posted", stage: "approved", total: sgd("249.00") }),
          ],
        },
      },
      "GET /v1/expense-claims/owed": { status: 200, body: { as_at: "2026-10-15", people: [{ claimant: { kind: "user", id: me, name: "Ah Kow" }, open: sgd("249.00"), claims: [{ claim_id: "c3", owed: sgd("249.00"), paid: sgd("0.00"), open: sgd("249.00") }] }], total: sgd("249.00") } },
      "GET /v1/card-lines": { status: 200, body: { items: [line()] } },
    });
    renderPage(MyClaims, { path: "/me/claims", session: employee, organisation, mode: "basic" });
    const table = await screen.findByRole("table", { name: "Your claims" });
    expect(within(table).getByRole("link", { name: "Acme visit" }).getAttribute("href")).toBe("/me/claims/c1");
    expect(within(table).getByText("Returned")).toBeTruthy();
    expect(screen.getByText("Wei Ling returned your claim")).toBeTruthy();
    expect(screen.getByText(/“Add who attended\.”/)).toBeTruthy();
    expect(await screen.findByText("249.00")).toBeTruthy();
    expect(screen.getByText("1 approved claim not yet paid back")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Returned 1" }));
    expect(within(table).queryByText("Acme visit")).toBeNull();
    expect(screen.getByRole("button", { name: /Card lines/ }).textContent).toContain("1");
    await noViolations(document.body);
  });

  it("completes a card line with its category, then offers its receipt; or marks it personal spend", async () => {
    answer({
      "GET /v1/expense-claims": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/owed": { status: 200, body: { as_at: "2026-10-15", people: [], total: sgd("0.00") } },
      "GET /v1/card-lines": { status: 200, body: { items: [line(), line({ id: "l2", counterparty: "Din Tai Fung", amount: sgd("-45.00") })] } },
      "GET /v1/claim-categories": { status: 200, body: categories },
      "GET /v1/expense-claims/gst-codes": { status: 200, body: codes },
      "POST /v1/card-lines/l1/complete": { status: 200, body: line({ status: "categorised", completed: "spend", document_id: "bt1", version: 2 }) },
      "POST /v1/card-lines/l2/complete": { status: 200, body: line({ id: "l2", status: "matched", completed: "personal", document_id: "adv9", version: 2 }) },
      "GET /v1/card-lines/l1/evidence": { status: 200, body: { items: [] } },
      "GET /v1/card-lines/l2/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(MyClaims, { path: "/me/claims", session: employee, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: /Card lines/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Complete: Grab" }));
    fireEvent.click(await screen.findByRole("button", { name: "Complete" }));
    expect(await screen.findByText("Choose the category it was for, or mark it personal spend.")).toBeTruthy();
    await screen.findByRole("option", { name: "Local travel" });
    expect(screen.queryByRole("option", { name: "Mileage" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Category"), { target: { value: "travel" } });
    await screen.findByRole("option", { name: /^NR:/ });
    fireEvent.change(screen.getByLabelText("GST on the receipt"), { target: { value: "NR" } });
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/card-lines/l1/complete")).toBe(true));
    const sent = seen.find((s) => s.path === "/v1/card-lines/l1/complete");
    expect(sent?.headers.get("If-Match")).toBe('"1"');
    expect(sent?.body).toMatchObject({ category_id: "travel", gst_code: "NR", description: "Grab" });
    expect(await screen.findByText("Completed. Attach its receipt now.")).toBeTruthy();
    expect(await screen.findByText("The receipt")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    fireEvent.click(await screen.findByRole("button", { name: "Complete: Din Tai Fung" }));
    fireEvent.click(await screen.findByLabelText("Myself: personal spend I refund"));
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/card-lines/l2/complete")?.body).toMatchObject({ personal: true }));
  });

  it("exports the history as CSV: plain decimals, and a title that starts like a formula made inert", () => {
    const csv = claimsCSV([claim(), claim({ id: "c4", number: "CLM-0004", title: "=HYPERLINK(1)", stage: "paid", total: sgd("1234.50") })]);
    expect(csv).toBe("Number,Date,Title,Stage,Amount,Currency\r\nCLM-0001,2026-10-15,Acme visit,Draft,27.30,SGD\r\nCLM-0004,2026-10-15,'=HYPERLINK(1),Paid,1234.50,SGD\r\n");
  });

  it("shows the member's cash advances with what is open, and one open past the deadline", async () => {
    answer({
      "GET /v1/expense-claims": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/owed": { status: 200, body: { as_at: "2026-10-15", people: [], total: sgd("0.00") } },
      "GET /v1/card-lines": { status: 200, body: { items: [] } },
      "GET /v1/cash-advances": {
        status: 200,
        body: { items: [advance(), advance({ id: "a2", number: "ADV-0002", open: sgd("20.00"), checks: [{ severity: "warning", code: "CLAIM-ADVANCE-OPEN", message: "20.00 SGD is still open", fix: "Ask for the claims" }] })] },
      },
    });
    renderPage(MyClaims, { path: "/me/claims", session: employee, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Cash advances" }));
    const table = await screen.findByRole("table", { name: "Your cash advances" });
    expect(within(table).getByText("57.85 SGD")).toBeTruthy();
    expect(within(table).getByText("Open past the deadline")).toBeTruthy();
    await noViolations(document.body);
  });
});

// req: FR-AP-14
// req: FR-AP-15
// req: FR-AP-17
describe("the claim editor", () => {
  it("checks the form before it is sent: every field at once", () => {
    const f: ClaimForm = { date: "2026-10-15", title: "", notes: "", late_reason: "", advance_id: "", trips: [], lines: [{ ...newReceipt("SGD", "2026-10-14"), amount: "abc", gst: "x", currency: "sg" }] };
    const { draft, errors } = checkClaim(f, "SGD");
    expect(draft).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["lines.0.amount", "lines.0.category_id", "lines.0.currency", "lines.0.gst"]);
  });

  it("saves a new claim, then submits it, confirming it's the claimant", async () => {
    answer({
      "GET /v1/claim-categories": { status: 200, body: categories },
      "GET /v1/expense-claims/gst-codes": { status: 200, body: codes },
      "GET /v1/cash-advances": { status: 200, body: { items: [advance()] } },
      "POST /v1/expense-claims": { status: 201, body: claim(), headers: { ETag: '"1-draft"' } },
    });
    renderPage(ClaimEditor, { path: "/me/claims/new", session: employee, organisation, mode: "basic" });
    fireEvent.change(await screen.findByLabelText("What it is for"), { target: { value: "Acme visit" } });
    fireEvent.change(screen.getByLabelText("Paid to"), { target: { value: "ComfortDelGro" } });
    await screen.findByRole("option", { name: "Local travel" });
    fireEvent.change(screen.getByLabelText("Category"), { target: { value: "travel" } });
    fireEvent.change(screen.getByLabelText("Amount on the receipt (SGD)"), { target: { value: "S$27.30" } });
    await screen.findByRole("option", { name: /^NR:/ });
    fireEvent.change(screen.getByLabelText("GST on the receipt"), { target: { value: "NR" } });
    await screen.findByRole("option", { name: /ADV-0001/ });
    fireEvent.change(screen.getByLabelText("Settles a cash advance"), { target: { value: "a1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/expense-claims")).toBe(true));
    const sent = seen.find((s) => s.method === "POST" && s.path === "/v1/expense-claims");
    expect(sent?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(sent?.body).toMatchObject({ title: "Acme visit", advance_id: "a1", lines: [{ kind: "receipt", merchant: "ComfortDelGro", category_id: "travel", amount: { amount: "27.30", currency: "SGD" }, gst_code: "NR" }] });
  });

  it("shows a returned claim's checks, submits it once fixed and withdraws it", async () => {
    let stage: Claim["stage"] = "returned";
    const submitted: Answer = { status: 202, body: { id: "r1", status: "pending" } };
    answer({
      "GET /v1/expense-claims/c2": () => ({
        status: 200,
        headers: { ETag: '"2-draft"' },
        body: claim({
          id: "c2",
          version: 2,
          stage,
          returned: stage === "returned" ? { at: "2026-10-15T01:00:00Z", comment: "Add who attended." } : undefined,
          checks: [{ severity: "warning", code: "CLAIM-ATTENDEES", field: "lines[0].attendees", message: "a Meals line above 50.00 SGD names who attended", fix: "Name who attended" }],
          ledger_impact: [{ line_no: 1, account_id: "a6530", amount: sgd("64.80"), fx_rate: "1", fx_rate_source: "base", base_amount: sgd("64.80"), description: "Din Tai Fung" }],
        }),
      }),
      "GET /v1/claim-categories": { status: 200, body: categories },
      "GET /v1/expense-claims/gst-codes": { status: 200, body: codes },
      "GET /v1/cash-advances": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/c2/evidence": { status: 200, body: { items: [] } },
      "PUT /v1/expense-claims/c2": { status: 200, body: claim({ id: "c2", version: 3 }), headers: { ETag: '"3-draft"' } },
      "POST /v1/expense-claims/c2/submit": () => {
        stage = "submitted";
        return submitted;
      },
      "POST /v1/expense-claims/c2/withdraw": problem(409, "APR-03", "The request is not pending."),
    });
    renderPage(ClaimEditor, { path: "/me/claims/c2", route: "/me/claims/$claimId", session: employee, organisation, mode: "expert" });
    expect(await screen.findByText(/“Add who attended\.”/)).toBeTruthy();
    expect(screen.getByText("Submitted with these for the approver")).toBeTruthy();
    expect(screen.getByText("Ledger impact")).toBeTruthy();
    fireEvent.change(await screen.findByLabelText("Who attended (meals and entertainment)"), { target: { value: "Mei Ling Tan" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/expense-claims/c2/submit")).toBe(true));
    expect(seen.find((s) => s.method === "PUT")?.headers.get("If-Match")).toBe('"2-draft"');
    expect(seen.find((s) => s.method === "PUT")?.body).toMatchObject({ lines: [{ attendees: "Mei Ling Tan" }] });
    expect(seen.find((s) => s.path === "/v1/expense-claims/c2/submit")?.headers.get("If-Match")).toBe('"3-draft"');
    expect(await screen.findByText("Submitted. It waits for approval; you can withdraw it to change it.")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Withdraw" }));
    expect(await screen.findByText("The request is not pending.", { exact: false })).toBeTruthy();
  });
});

// req: FR-AP-16
describe("mileage trips", () => {
  it("checks the trips before they are sent: every field at once, and the policy's mileage category", () => {
    const { lines, errors } = checkTrips([{ ...newTrip("2026-10-14"), date: "", distance: "18.45.1" }], "");
    expect(lines).toBeUndefined();
    expect(Object.keys(errors).sort()).toEqual(["category", "trips.0.date", "trips.0.distance", "trips.0.from", "trips.0.to"]);
    const ok = checkTrips([{ ...newTrip("2026-10-14"), from: "Office", to: "Acme HQ", return: true, distance: "36.8" }], "mileage");
    expect(ok.lines).toEqual([{ kind: "mileage", date: "2026-10-14", category_id: "mileage", from: "Office", to: "Acme HQ", return: true, vehicle: "car", distance: "36.8" }]);
  });

  it("saves a new mileage claim from saved places, keeping no address beyond Fyniq", async () => {
    answer({
      "GET /v1/claim-categories": { status: 200, body: categories },
      "GET /v1/claim-places": { status: 200, body: { items: [{ id: "p1", name: "Office", address: "1 Raffles Place", active: true, version: 1 }, { id: "p2", name: "Acme HQ", address: "10 Anson Road", active: true, version: 1 }] } },
      "POST /v1/expense-claims": { status: 201, body: claim({ lines: [] }), headers: { ETag: '"1-draft"' } },
    });
    renderPage(MileageClaim, { path: "/me/claims/new/mileage", session: employee, organisation, mode: "basic" });
    const from = await screen.findByRole("group", { name: "Saved places for From" });
    fireEvent.click(within(from).getByRole("button", { name: "Office" }));
    const to = screen.getByRole("group", { name: "Saved places for To" });
    fireEvent.click(within(to).getByRole("button", { name: "Acme HQ" }));
    fireEvent.change(screen.getByLabelText("Distance driven, the whole trip (km)"), { target: { value: "36.8" } });
    fireEvent.click(screen.getByLabelText("A return trip"));
    fireEvent.click(screen.getByLabelText("Motorcycle"));
    await screen.findByRole("button", { name: "Save draft" });
    await waitFor(() => expect(screen.queryByText(/no mileage category/)).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/expense-claims")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/expense-claims")?.body).toMatchObject({
      lines: [{ kind: "mileage", category_id: "mileage", from: "1 Raffles Place", to: "10 Anson Road", return: true, vehicle: "motorcycle", distance: "36.8" }],
    });
    await noViolations(document.body);
  });

  it("shows the month's trips in the Mileage tab", async () => {
    answer({
      "GET /v1/expense-claims": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/owed": { status: 200, body: { as_at: "2026-10-15", people: [], total: sgd("0.00") } },
      "GET /v1/card-lines": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/mileage-log": {
        status: 200,
        body: { month: "2026-10", claimant: { kind: "user", id: me }, total: sgd("22.08"), trips: [{ date: "2026-10-14", claim_id: "c1", stage: "draft", purpose: "Client visit", from: "Office", to: "Acme HQ", return: true, vehicle: "car", claimed_km: "36.8", amount: sgd("22.08") }] },
      },
    });
    renderPage(MyClaims, { path: "/me/claims", session: employee, organisation, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Mileage" }));
    const table = await screen.findByRole("table", { name: "Your trips" });
    expect(within(table).getByRole("link", { name: "Office to Acme HQ and back" }).getAttribute("href")).toBe("/me/claims/c1");
    expect(within(table).getByText("36.8")).toBeTruthy();
    expect(screen.getByText("The month's trips come to 22.08 SGD.")).toBeTruthy();
  });
});

// req: FR-AP-14
describe("a claim's receipt", () => {
  it("is taken from a file dropped on the evidence panel, and attached as a receipt", async () => {
    answer({
      "GET /v1/expense-claims/c1": { status: 200, headers: { ETag: '"1-draft"' }, body: claim() },
      "GET /v1/claim-categories": { status: 200, body: categories },
      "GET /v1/expense-claims/gst-codes": { status: 200, body: codes },
      "GET /v1/cash-advances": { status: 200, body: { items: [] } },
      "GET /v1/expense-claims/c1/evidence": { status: 200, body: { items: [] } },
      "POST /v1/expense-claims/c1/evidence": { status: 201, body: { id: "e1", claim_id: "c1", evidence_type: "receipt", file_name: "taxi.png", media_type: "image/png", size_bytes: 9, sha256: "a", original_sha256: "a", source: "upload", data_class: "general", uploaded_by: "user:x", uploaded_at: "2026-10-15T01:00:00Z", state: "attached", version: 1 } },
    });
    renderPage(ClaimEditor, { path: "/me/claims/c1", route: "/me/claims/$claimId", session: employee, organisation, mode: "basic" });
    const zone = await screen.findByLabelText("Drop or paste a file to attach to the claim");
    fireEvent.drop(zone, { dataTransfer: { files: [new File(["receipt"], "taxi.png", { type: "image/png" })] } });
    expect(await screen.findByText("Chosen: taxi.png")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Attach" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/expense-claims/c1/evidence")).toBe(true));
    const sent = seen.find((s) => s.method === "POST" && s.path === "/v1/expense-claims/c1/evidence");
    expect(sent?.headers.get("If-Match")).toBe('"1-draft"');
    // The test DOM's File reaches the request without its bytes, which a browser sends, so only the part is checked.
    expect(sent?.body).toMatchObject({ evidence_type: "receipt", data_class: "general", source: "upload" });
    expect((sent?.body as Record<string, unknown>).file).toBeDefined();
  });
});
