import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Organisation, Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BankReconciliation, lastEnded } from "./BankReconciliation";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const controller = { ...ownerSession, roles: ["controller"] } as Session;
const founderOrg: Organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const standardOrg: Organisation = { ...founderOrg, team_mode: "standard" };
const periods = [
  { id: "p9", starts_on: "2026-09-01", ends_on: "2026-09-30", status: "open" as const, version: 1 },
  { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open" as const, version: 1 },
];
const rec = (extra: Partial<components["schemas"]["BankReconciliation"]> = {}): components["schemas"]["BankReconciliation"] => ({
  id: "rec1",
  money_account_id: "m1",
  period_id: "p9",
  statement_balance: sgd("1200.00"),
  book_balance: sgd("1100.00"),
  unreconciled: sgd("100.00"),
  unreconciled_count: 1,
  difference: sgd("0.00"),
  ledger_revision: 42,
  prepared_by: "user:someone-else",
  prepared_at: "2026-10-03T01:00:00Z",
  version: 1,
  unreconciled_lines: [{ id: "l1", money_account_id: "m1", statement_id: "s1", booking_date: "2026-09-29", amount: sgd("100.00"), description: "FAST CR", status: "unreconciled", suggestions: [], version: 1 }],
  ...extra,
});
const reads = (r: unknown) => ({
  "GET /v1/money-accounts/m1": { status: 200, body: { id: "m1", account_id: "a1010", account_code: "1010", kind: "bank", name: "DBS current", currency: "SGD", freshness: { open_gaps: 0 }, waiting: 1, gaps: [], sources: [], version: 1 } },
  "GET /v1/periods": { status: 200, body: { items: periods } },
  "GET /v1/money-accounts/m1/reconciliations/p9": r === null ? { status: 404, body: { status: 404, code: "NOT-FOUND", title: "Not found" } } : { status: 200, body: r, headers: { ETag: '"1"' } },
  "GET /v1/bank-lines": { status: 200, body: { items: [] } },
});
const at = { path: "/banking/reconciliations/m1/p9", route: "/banking/reconciliations/$accountId/$periodId" };

// req: FR-BNK-10
// req: FM-5
describe("reconciling an account for a month", () => {
  it("takes the latest month ended by today", () => {
    expect(lastEnded(periods, "2026-10-26")?.id).toBe("p9");
    expect(lastEnded(periods, "2026-08-01")?.id).toBe("p9"); // none ended: the first
  });

  it("is prepared from the imported statement in Founder mode, no balance typed; axe finds nothing", async () => {
    let prepared = false;
    const sent = answer({
      ...reads(null),
      "GET /v1/money-accounts/m1/reconciliations/p9": () => (prepared ? { status: 200, body: rec({ difference: sgd("10.00"), prepared_by: `user:${ownerSession.user_id}` }), headers: { ETag: '"1"' } } : { status: 404, body: { status: 404, code: "NOT-FOUND" } }),
      "POST /v1/bank-reconciliations": () => {
        prepared = true;
        return { status: 201, body: rec() };
      },
    });
    const { container } = renderPage(BankReconciliation, { ...at, session: bookkeeper, organisation: founderOrg });
    expect(await screen.findByText("Not prepared yet")).toBeTruthy();
    expect(screen.getByText(/In Founder mode the statement's balance comes only from an imported statement/)).toBeTruthy();
    expect(screen.queryByLabelText("The e-statement's balance (optional)")).toBeNull();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Prepare it" }));
    expect(await screen.findByText("A difference to explain")).toBeTruthy();
    expect(sent.find((s) => s.path === "/v1/bank-reconciliations")?.body).toEqual({ money_account_id: "m1", period_id: "p9" });
    expect(screen.getByRole("table", { name: "Unreconciled lines" }).textContent).toContain("FAST CR");
  });

  it("takes a typed e-statement balance in Standard mode", async () => {
    const sent = answer({ ...reads(null), "POST /v1/bank-reconciliations": { status: 201, body: rec() } });
    renderPage(BankReconciliation, { ...at, session: bookkeeper, organisation: standardOrg });
    fireEvent.change(await screen.findByLabelText("The e-statement's balance (optional)"), { target: { value: "1200.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Prepare it" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/bank-reconciliations")?.body).toEqual({ money_account_id: "m1", period_id: "p9", statement_balance: sgd("1200.00") }));
  });

  it("is signed off by another person at its version; never by its preparer", async () => {
    const sent = answer({ ...reads(rec()), "POST /v1/money-accounts/m1/reconciliations/p9/sign": { status: 200, body: rec({ signed_by: `user:${ownerSession.user_id}`, signed_at: "2026-10-04T01:00:00Z" }) } });
    const { container } = renderPage(BankReconciliation, { ...at, session: controller, organisation: standardOrg });
    expect(await screen.findByText("Ready to sign off")).toBeTruthy();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Sign it off" }));
    await waitFor(() => expect(sent.find((s) => s.path === "/v1/money-accounts/m1/reconciliations/p9/sign")?.headers.get("If-Match")).toBe('"1"'));
  });

  it("tells its preparer that another person signs it off", async () => {
    answer(reads(rec({ prepared_by: `user:${ownerSession.user_id}` })));
    renderPage(BankReconciliation, { ...at, session: controller, organisation: standardOrg });
    expect(await screen.findByText("You prepared it: another person signs it off.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Sign it off" })).toBeNull();
  });

  it("shows a signed-off reconciliation as locked with the month", async () => {
    answer(reads(rec({ signed_by: "user:x", signed_at: "2026-10-04T01:00:00Z" })));
    renderPage(BankReconciliation, { ...at, session: bookkeeper, organisation: standardOrg });
    expect(await screen.findByText("Signed off: locked with the month")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Prepare it/ })).toBeNull();
  });
});
