import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Categorise } from "./Categorise";
import { acceptRequest, checkCategorisation, settles, type Line, type Suggestion } from "./lineActions";

const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const line = (extra: Partial<Line> = {}): Line => ({
  id: "l1",
  money_account_id: "m1",
  statement_id: "s1",
  booking_date: "2026-10-20",
  amount: sgd("10900.00"),
  description: "GIRO ACME ROBOTICS INV-0138",
  counterparty: "ACME ROBOTICS",
  status: "unreconciled",
  suggestions: [],
  version: 4,
  ...extra,
});
const invoice: Suggestion = { found: "reference", kind: "receipt", document_id: "inv1", number: "INV-0138", contact_id: "c1", contact: "Acme Robotics Pte. Ltd.", open: sgd("10900.00") };
const ruleHit: Suggestion = { found: "rule", kind: "categorised", rule_id: "r1", rule_name: "AWS" };
const queue = (lines: Line[], detail: Record<string, Line> = {}) => ({
  "GET /v1/money-accounts": { status: 200, body: { items: [{ id: "m1", account_id: "a1010", account_code: "1010", kind: "bank", name: "DBS current", currency: "SGD", freshness: { open_gaps: 0 }, waiting: lines.length, gaps: [], sources: [], version: 1 }] } },
  "GET /v1/bank-lines": { status: 200, body: { items: lines } },
  ...Object.fromEntries(Object.entries(detail).map(([id, l]) => [`GET /v1/bank-lines/${id}`, { status: 200, body: l, headers: { ETag: `"${l.version}"` } }])),
});

// req: FR-BNK-06
// req: FR-BNK-07
describe("the requests a line's actions send", () => {
  it("settles what is open, or the line when it is smaller, and reads a rule's action", () => {
    expect(settles(line({ amount: sgd("500.00") }), sgd("10900.00"))).toEqual(sgd("500.00"));
    expect(settles(line({ amount: sgd("-12000.00") }), sgd("10900.00"))).toEqual(sgd("10900.00"));
    expect(acceptRequest(line(), invoice)).toEqual({ action: "match-invoices", body: { customer_id: "c1", invoices: [{ invoice_id: "inv1", amount: sgd("10900.00") }] } });
    expect(acceptRequest(line(), ruleHit)).toBeNull(); // a rule is read first
    const rule = { id: "r1", name: "AWS", position: 1, active: true, auto_apply: false, from_ai_proposal: false, created_by: "u", version: 1, when: {}, then: { account_id: "a6100", gst_code: "TX" } };
    expect(acceptRequest(line({ amount: sgd("-25.00") }), ruleHit, rule)).toEqual({ action: "categorise", body: { parts: [{ account_id: "a6100", gst_code: "TX" }] } });
  });

  it("says what is wrong with a split, every part at once", () => {
    const { errors } = checkCategorisation(
      [
        { key: "p1", account_id: "", amount: "", gst_code: "", description: "", dimension_value_ids: [] },
        { key: "p2", account_id: "a1", amount: "", gst_code: "", description: "", dimension_value_ids: [] },
        { key: "p3", account_id: "a1", amount: "1.2.3", gst_code: "", description: "", dimension_value_ids: [] },
      ],
      "",
      "",
      "SGD",
    );
    expect(Object.keys(errors).sort()).toEqual(["p1.account", "p3.amount", "parts"]);
  });
});

// req: FR-BNK-06
// req: FR-BNK-07
// req: FR-UX-24
// req: QA-UI-04
describe("the categorise queue", () => {
  it("shows one line at a time with its suggestions, and accepts the best one; axe finds nothing", async () => {
    const sent = answer({
      ...queue([line(), line({ id: "l2", amount: sgd("-25.00"), description: "AWS EMEA" })], { l1: line({ suggestions: [invoice] }) }),
      "POST /v1/bank-lines/l1/match-invoices": { status: 200, body: { line: line({ status: "matched" }) } },
    });
    const { container } = renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText("Line 1 of 2")).toBeTruthy();
    expect(screen.getByText("0 done · 2 lines wait")).toBeTruthy();
    const choice = await screen.findByRole("radio", { name: /Payment of INV-0138 from Acme Robotics Pte. Ltd./ });
    expect(choice.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/split a line over several accounts, or mark it as a transfer, in Expert mode/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Split" })).toBeNull();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bank-lines/l1/match-invoices")).toBe(true));
    const req = sent.find((s) => s.path === "/v1/bank-lines/l1/match-invoices")!;
    expect(req.headers.get("If-Match")).toBe('"4"');
    expect(req.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("sends the same request for the same accept in Basic's card and Expert's grid; axe finds nothing in the grid", async () => {
    const take = async (mode: "basic" | "expert") => {
      const sent = answer({
        ...queue([line()], { l1: line({ suggestions: [invoice] }) }),
        "POST /v1/bank-lines/l1/match-invoices": { status: 200, body: { line: line({ status: "matched" }) } },
      });
      const { unmount, container } = renderPage(Categorise, { path: mode === "basic" ? "/banking/categorise" : "/banking/categorise?view=grid", route: "/banking/categorise", session: bookkeeper, mode });
      if (mode === "basic") {
        await screen.findByRole("radio", { name: /INV-0138/ });
        fireEvent.click(screen.getByRole("button", { name: "Accept" }));
      } else {
        const accept = await screen.findByRole("button", { name: /Accept the suggestion for the line of/ });
        expect(screen.getByRole("table", { name: "Waiting lines" }).textContent).toContain("Payment of INV-0138");
        await noViolations(container);
        fireEvent.click(accept);
      }
      await waitFor(() => expect(sent.some((s) => s.method === "POST")).toBe(true));
      const req = sent.find((s) => s.method === "POST")!;
      unmount();
      return { path: req.path, body: req.body, match: req.headers.get("If-Match") };
    };
    const basic = await take("basic");
    const expert = await take("expert");
    expect(expert).toEqual(basic);
  });

  it("accepts a rule's suggestion as the rule's category", async () => {
    const sent = answer({
      ...queue([line({ amount: sgd("-25.00") })], { l1: line({ amount: sgd("-25.00"), suggestions: [ruleHit] }) }),
      "GET /v1/bank-rules/r1": { status: 200, body: { id: "r1", name: "AWS", position: 1, active: true, auto_apply: false, from_ai_proposal: false, created_by: "u", version: 1, when: {}, then: { account_id: "a6100", gst_code: "TX", contact_id: "c9" } } },
      "POST /v1/bank-lines/l1/categorise": { status: 200, body: { line: line({ status: "categorised" }) } },
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    await screen.findByRole("radio", { name: /Rule: AWS/ });
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/bank-lines/l1/categorise")?.body).toEqual({ contact_id: "c9", parts: [{ account_id: "a6100", gst_code: "TX" }] }));
  });

  it("changes a line's category in a short form, and shows what the API refused", async () => {
    const sent = answer({
      ...queue([line({ amount: sgd("-25.00") })], { l1: line({ amount: sgd("-25.00") }) }),
      "GET /v1/accounts": {
        status: 200,
        body: {
          items: [
            { id: "a6100", code: "6100", name: "Software", type: "expense", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
            // Receivables and payables take a line by another path (a receipt, a bill payment): never offered as a category.
            { id: "a1100", code: "1100", name: "Accounts receivable", type: "asset", role: "accounts-receivable", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
            { id: "a2010", code: "2010", name: "Accounts payable", type: "liability", role: "accounts-payable", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 },
          ],
        },
      },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      "GET /v1/purchases/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", codes: [{ code: "TX", description: "Standard-rated purchase", claimable: true }] } },
      "POST /v1/bank-lines/l1/categorise": problem(409, "LEDGER-09", "October 2026 is locked.", "Ask the Controller to reopen it, or post it in an open month."),
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText(/No suggestion/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Post it" }));
    expect(within(dialog).getByText("Choose the account it posts to.")).toBeTruthy();
    const category = within(dialog).getByLabelText("Category");
    await waitFor(() => expect(within(category).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose an account", "6100 Software"]));
    fireEvent.change(category, { target: { value: "a6100" } });
    const gst = within(dialog).getByLabelText("GST code");
    await waitFor(() => expect(within(gst).getAllByRole("option")).toHaveLength(2));
    fireEvent.change(gst, { target: { value: "TX" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Post it" }));
    expect(await within(dialog).findByText(/October 2026 is locked/)).toBeTruthy();
    expect(sent.find((s) => s.path === "/v1/bank-lines/l1/categorise")?.body).toEqual({ parts: [{ account_id: "a6100", gst_code: "TX" }] });
  });

  // req: FR-EQ-18
  // req: FR-EQ-17
  it("names the SAFE a part on 2700 is for, and the subscriber of a part on 2040; other control accounts stay off", async () => {
    const acct = (id: string, code: string, name: string, role: string, control: boolean) => ({ id, code, name, type: "liability", role, group: false,
      active: true, money_account: false, control, payroll_cost: false, revalue: false, parent_id: null, version: 1 });
    const sent = answer({
      ...queue([line({ amount: sgd("125000.00") })], { l1: line({ amount: sgd("125000.00") }) }),
      "GET /v1/accounts": { status: 200, body: { items: [acct("a2700", "2700", "Convertible notes / SAFE", "convertible-instruments", true),
        acct("a2040", "2040", "Share application monies", "share-application-monies", true), acct("a2500", "2500", "Loan from director", "director-loan", true)] } },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      "GET /v1/purchases/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", codes: [] } },
      "GET /v1/instruments": { status: 200, body: { items: [{ id: "in-1", status: "recorded", version: 1, holder_name: "Hatch Capital Pte. Ltd.", currency: "SGD", class_code: "3010",
        evidence: 1, checks: [], form: "post_money_safe", holder_id: "c-hatch", amount: "125000.00", signed_on: "2026-06-18", trigger: "priced_round",
        class_account_id: "acc-3010", denominator: "post_money_capitalisation", price_decimals: 4 }] } },
      "POST /v1/bank-lines/l1/categorise": { status: 200, body: { line: line({ status: "categorised" }) } },
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText(/No suggestion/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog");
    const category = within(dialog).getByLabelText("Category");
    await waitFor(() => expect(within(category).getAllByRole("option").map((o) => o.textContent)).toEqual(
      ["Choose an account", "2700 Convertible notes / SAFE", "2040 Share application monies"]));
    fireEvent.change(category, { target: { value: "a2040" } });
    expect(within(dialog).getByLabelText("Subscriber")).toBeTruthy();
    expect(within(dialog).queryByLabelText("SAFE or note")).toBeNull();
    fireEvent.change(category, { target: { value: "a2700" } });
    const safe = await within(dialog).findByLabelText("SAFE or note");
    await waitFor(() => expect(within(safe).getAllByRole("option")).toHaveLength(2));
    fireEvent.change(safe, { target: { value: "in-1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Post it" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/bank-lines/l1/categorise")?.body).toEqual({ instrument_id: "in-1", parts: [{ account_id: "a2700" }] }));
  });

  // req: FR-DIM-01
  // req: FR-DIM-02
  it("asks for a dimension its category requires, even in Basic, and sends the value with the part", async () => {
    const sent = answer({
      ...queue([line({ amount: sgd("-25.00") })], { l1: line({ amount: sgd("-25.00") }) }),
      "GET /v1/accounts": {
        status: 200,
        body: {
          items: [
            { id: "a6100", code: "6100", name: "Software", type: "expense", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1, required_dimension_ids: ["d1"] },
          ],
        },
      },
      "GET /v1/dimensions": { status: 200, body: { items: [{ id: "d1", kind: "department", name: "Department", active: true, version: 1 }] } },
      "GET /v1/dimension-values": { status: 200, body: { items: [{ id: "v1", dimension_id: "d1", code: "ENG", name: "Engineering", active: true, version: 1 }] } },
      "GET /v1/contacts": { status: 200, body: { items: [] } },
      "GET /v1/purchases/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", codes: [{ code: "TX", description: "Standard-rated purchase", claimable: true }] } },
      "POST /v1/bank-lines/l1/categorise": { status: 200, body: { line: line({ status: "categorised" }) } },
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText(/No suggestion/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog");
    const category = within(dialog).getByLabelText("Category");
    await waitFor(() => expect(within(category).getAllByRole("option")).toHaveLength(2));
    await within(dialog).findByRole("option", { name: "ENG Engineering" });
    fireEvent.change(category, { target: { value: "a6100" } });
    expect(within(dialog).getByText("6100 needs one")).toBeTruthy();
    expect(within(dialog).getByLabelText("Department").closest(".fy-field")!.className).not.toContain("fy-expert-only");
    fireEvent.change(within(dialog).getByLabelText("Department"), { target: { value: "v1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Post it" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bank-lines/l1/categorise")).toBe(true));
    expect(sent.find((s) => s.path === "/v1/bank-lines/l1/categorise")?.body).toEqual({ parts: [{ account_id: "a6100", dimension_value_ids: ["v1"] }] });
  });

  it("offers split, transfer, match and suspense in Expert, and puts a line in suspense", async () => {
    const sent = answer({
      ...queue([line({ amount: sgd("-25.00") })], { l1: line({ amount: sgd("-25.00") }) }),
      "POST /v1/bank-lines/l1/suspend": { status: 200, body: { line: line({ status: "suspense" }) } },
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "expert" });
    expect(await screen.findByRole("button", { name: "Split" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Transfer" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Match" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Suspense" }));
    await waitFor(() => expect(sent.some((s) => s.path === "/v1/bank-lines/l1/suspend" && s.headers.get("If-Match") === '"4"')).toBe(true));
  });

  it("runs Reconcile now and says what it left, and why", async () => {
    answer({
      ...queue([line()], { l1: line() }),
      "POST /v1/bank-lines:auto-reconcile": { status: 200, body: { reconciled: 3, left: { line_cap: 1, no_match: 2 } } },
    });
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Reconcile now" }));
    expect(await screen.findByText(/3 lines reconciled by Fyniq itself\. Left for you: 1 over the line limit, 2 no exact match\./)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/bank-lines:auto-reconcile")?.body).toEqual({});
  });

  it("is all caught up with no waiting line", async () => {
    answer(queue([]));
    renderPage(Categorise, { path: "/banking/categorise", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText("All caught up")).toBeTruthy();
  });
});
