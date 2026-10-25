import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AutoReconciled } from "./AutoReconciled";
import { BankRules, checkRule, conditionWords } from "./BankRules";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
type Rule = components["schemas"]["BankRule"];
const rule = (extra: Partial<Rule> = {}): Rule => ({
  id: "r1",
  name: "AWS to software",
  position: 1,
  active: true,
  auto_apply: false,
  from_ai_proposal: false,
  created_by: "user:u1",
  version: 2,
  when: { direction: "out", description_contains: "aws" },
  then: { account_id: "a6100", gst_code: "TX" },
  ...extra,
});
const reads = {
  "GET /v1/accounts": { status: 200, body: { items: [{ id: "a6100", code: "6100", name: "Software", type: "expense", group: false, active: true, money_account: false, control: false, payroll_cost: false, revalue: false, parent_id: null, version: 1 }] } },
  "GET /v1/money-accounts": { status: 200, body: { items: [] } },
  "GET /v1/contacts": { status: 200, body: { items: [] } },
  "GET /v1/purchases/gst-codes": { status: 200, body: { version: "sg-gst@2026.1", codes: [{ code: "TX", description: "Standard-rated purchase", claimable: true }] } },
};

// req: FR-BNK-05
// req: FR-BNK-14
describe("bank rules", () => {
  it("words a rule's conditions, and says what a rule form lacks, every field at once", () => {
    expect(conditionWords({ direction: "out", description_contains: "aws", amount_max: "500.00" }, new Map())).toBe('money out, description contains "aws", at most 500.00');
    expect(conditionWords({}, new Map())).toBe("every line");
    expect(Object.keys(checkRule({ name: "", position: "0", active: true, when: { amount_min: "1,000" }, then: { account_id: "" } }).errors).sort()).toEqual(["account", "amount_min", "name", "position"]);
    expect(checkRule({ name: "x", position: "1", active: true, when: {}, then: { account_id: "a1" } }).errors.when).toBeTruthy();
    expect(checkRule({ name: " AWS ", position: "3", active: true, when: { description_contains: "aws", currency: undefined }, then: { account_id: "a1", gst_code: "" } }).draft).toEqual({
      name: "AWS",
      position: 3,
      active: true,
      when: { description_contains: "aws" },
      then: { account_id: "a1" },
    });
  });

  it("lists the rules in order with their status, the AI's labelled; offers rules to consider; axe finds nothing", async () => {
    answer({
      ...reads,
      "GET /v1/bank-rules": {
        status: 200,
        body: {
          items: [
            rule(),
            rule({ id: "r2", name: "Grab rides", position: 2, auto_apply: true }),
            rule({ id: "r3", name: "Old rent", position: 3, active: false }),
            rule({ id: "r4", name: "Stripe fees", position: 4, problem: "the account is not active", from_ai_proposal: true }),
          ],
        },
      },
      "GET /v1/bank-rule-offers": { status: 200, body: { items: [{ when: { counterparty_contains: "atlassian" }, then: { account_id: "a6100" }, lines: 5 }] } },
    });
    const { container } = renderPage(BankRules, { path: "/banking/rules", session: accountant });
    const table = await screen.findByRole("table", { name: "Bank rules" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Suggests");
    expect(rows[1]!.textContent).toContain('money out, description contains "aws"');
    expect(rows[2]!.textContent).toContain("Applies by itself");
    expect(rows[3]!.textContent).toContain("Off");
    expect(rows[4]!.textContent).toContain("Does not read: the account is not active");
    expect(within(rows[4]!).getByText("AI")).toBeTruthy();
    expect(within(rows[1]!).getByRole("button", { name: "Ask to apply by itself" })).toBeTruthy();
    expect(within(rows[2]!).queryByRole("button", { name: "Ask to apply by itself" })).toBeNull();
    expect(await screen.findByText(/counterparty contains "atlassian" → 6100 Software · 5 lines/)).toBeTruthy();
    await noViolations(container);
  });

  it("tests a new rule against the lines held, then saves it", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bank-rules": { status: 200, body: { items: [rule()] } },
      "GET /v1/bank-rule-offers": { status: 200, body: { items: [] } },
      "POST /v1/bank-rules:back-test": { status: 200, body: { examined: 420, matched: 12, by_status: { categorised: 12 }, sample: [] } },
      "POST /v1/bank-rules": { status: 201, body: rule({ id: "r9", name: "Atlassian", position: 2 }) },
    });
    renderPage(BankRules, { path: "/banking/rules", session: accountant });
    await screen.findByRole("table", { name: "Bank rules" });
    fireEvent.click(screen.getByRole("button", { name: "New rule" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rule" }));
    expect(within(dialog).getByText("Give the rule at least one condition.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Atlassian" } });
    fireEvent.change(within(dialog).getByLabelText("Counterparty contains"), { target: { value: "atlassian" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Test it" }));
    expect(await within(dialog).findByText("It would have matched 12 of the 420 lines held.")).toBeTruthy();
    const to = within(dialog).getByLabelText("Account");
    await waitFor(() => expect(within(to).getAllByRole("option")).toHaveLength(2));
    fireEvent.change(to, { target: { value: "a6100" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the rule" }));
    await waitFor(() =>
      expect(sent.find((s) => s.method === "POST" && s.path === "/v1/bank-rules")?.body).toEqual({
        name: "Atlassian",
        position: 2,
        active: true,
        when: { counterparty_contains: "atlassian" },
        then: { account_id: "a6100" },
      }),
    );
    expect(sent.find((s) => s.path === "/v1/bank-rules:back-test")?.body).toEqual({ counterparty_contains: "atlassian" });
  });

  it("asks another person to let a rule apply by itself, on the rule's version", async () => {
    const sent = answer({
      ...reads,
      "GET /v1/bank-rules": { status: 200, body: { items: [rule()] } },
      "GET /v1/bank-rule-offers": { status: 200, body: { items: [] } },
      "POST /v1/bank-rules/r1/auto-apply-approval": { status: 202, body: { id: "ap1" } },
    });
    renderPage(BankRules, { path: "/banking/rules", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Ask to apply by itself" }));
    expect(await screen.findByText(/Asked for approval to let AWS to software apply by itself/)).toBeTruthy();
    expect(sent.find((s) => s.path === "/v1/bank-rules/r1/auto-apply-approval")?.headers.get("If-Match")).toBe('"2"');
  });
});

const line = (extra: Partial<components["schemas"]["BankLine"]> = {}): components["schemas"]["BankLine"] => ({
  id: "l1",
  money_account_id: "m1",
  statement_id: "s1",
  booking_date: "2026-10-20",
  amount: sgd("-25.00"),
  description: "AWS EMEA",
  status: "categorised",
  suggestions: [],
  version: 3,
  match: { kind: "categorised", basis: "rule", rule_id: "r2", made_by: "service:bank.auto-reconcile", made_at: "2026-10-21T01:00:00Z" },
  ...extra,
});

// req: FR-BNK-14
// req: FR-AUT-02
describe("what the system reconciled", () => {
  it("lists each with why, by whom and when, within the limits shown; undoes one with a reason; axe finds nothing", async () => {
    const sent = answer({
      "GET /v1/auto-reconciliations": {
        status: 200,
        body: {
          items: [
            { line: line(), rule_name: "AWS to software" },
            { line: line({ id: "l2", amount: sgd("109.00"), description: "GIRO ACME", match: { kind: "receipt", basis: "exact", made_by: "service:bank.auto-reconcile", made_at: "2026-10-21T01:00:00Z" } }) },
          ],
        },
      },
      "GET /v1/organisation/settings": {
        status: 200,
        body: { items: [{ key: "bank.auto_cap_line", label: "Auto-reconcile, the largest line", value: "500.00", group: "Banking", kind: "amount_sgd", guide: "", lengthen_only: false, needs_second_approver: true, reason: "", recommended_value: "500.00", requirement: "FR-BNK-14", set_by_platform: false, status: "recommended", version: 0 }] },
      },
      "POST /v1/bank-lines/l1/unreconcile": { status: 200, body: { line: line({ status: "unreconciled" }) } },
    });
    const { container } = renderPage(AutoReconciled, { path: "/banking/auto-reconciled", session: accountant });
    const table = await screen.findByRole("table", { name: "Lines reconciled by the system" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("The rule AWS to software");
    expect(rows[1]!.textContent).toContain("service:bank.auto-reconcile");
    expect(rows[2]!.textContent).toContain("Exact match: amount, number and contact");
    expect(await screen.findByText(/Auto-reconcile, the largest line/)).toBeTruthy();
    await noViolations(container);
    fireEvent.click(within(rows[1]!).getByRole("button", { name: /Undo the line/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Undo it" }));
    expect(within(dialog).getByText(/Say why/)).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Why undo it"), { target: { value: "Not AWS: a refund" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Undo it" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/bank-lines/l1/unreconcile")?.body).toEqual({ reason: "Not AWS: a refund" }));
    expect(seen.find((s) => s.path === "/v1/bank-lines/l1/unreconcile")?.headers.get("If-Match")).toBe('"3"');
  });

  it("says when nothing was reconciled by itself", async () => {
    answer({ "GET /v1/auto-reconciliations": { status: 200, body: { items: [] } }, "GET /v1/organisation/settings": { status: 200, body: { items: [] } } });
    renderPage(AutoReconciled, { path: "/banking/auto-reconciled", session: accountant });
    expect(await screen.findByText("Nothing reconciled by itself in the last 30 days")).toBeTruthy();
  });
});
