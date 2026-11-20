import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Payroll } from "./Payroll";

const payroll = { ...ownerSession, roles: ["payroll_administrator"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const batch = {
  id: "s1", number: "SB-0003", run_id: "r1", run_version: 1, wage_month: "2026-10", status: "approved", state: "approved", money_account_id: "a1", payment_date: "2026-10-31", method: "giro", line_count: 3,
  total: sgd("26000.43"), released: 0, paid: 0, waiting: 1, reapproval_needed: false, approval_id: "q1", releases: [], lines_visible: false, checks: [], version: 2, lines_version: 1,
};

// req: FR-PPL-08
// part of FR-PPL-04, FR-PPL-05, FR-PAY-07 (the page; the runs and their refusals are the API's and the checks')
describe("payroll in the books", () => {
  it("shows where each feature lives, the link, how the month is met and the latest salary batches; axe finds nothing", async () => {
    answer({
      "GET /v1/people-link": { status: 200, body: { state: "active", epoch: 1, sources: [{ feature: "payroll", source: "nemura", effective_from: "2026-11-01", set_at: "2026-11-01T09:00:00Z" }], inbox: [] } },
      "GET /v1/people-link/health": { status: 200, body: { state: "active", reachable: true, checked_at: "2026-11-21T09:00:00Z", nemura_highest_sequence: 9, received_highest_sequence: 9, waiting: 0, failing: 1, refused: 0, missing: [], requested: [] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: ["payslips", "kets"], met: false } },
      "GET /v1/salary-batches": { status: 200, body: { items: [batch] } },
      "GET /v1/people-link/availability": { status: 200, body: { items: [{ entry_id: "e1", person_id: "p1", person_name: "Ana Lim", start_date: "2026-12-21", end_date: "2026-12-24", start_half: false, end_half: false, days: "4.00" }] } },
      "GET /v1/people-link/people": { status: 200, body: { items: [{ person_id: "p1", display_name: "Ana Lim", held: false, removed: false }] } },
    });
    renderPage(Payroll, { path: "/payroll", session: payroll });
    expect(await screen.findByText("Payroll in the books")).toBeTruthy();
    const awayList = await screen.findByRole("region", { name: "Away in the next 30 days" });
    expect(within(awayList).getByText(/Ana Lim: 21 Dec 2026 to 24 Dec 2026, 4.00 day\(s\)/)).toBeTruthy();
    const where = await screen.findByRole("region", { name: "What lives in Nemura" });
    expect(within(where).getByText(/Payroll: Nemura/)).toBeTruthy();
    expect(within(where).getByText(/People records: Manual journal/)).toBeTruthy();
    expect(await screen.findByText(/The link is not delivering everything/)).toBeTruthy();
    expect(await screen.findByText(/2 arrangement\(s\) are not recorded/)).toBeTruthy();
    const table = await screen.findByRole("table", { name: "Latest salary batches" });
    expect(within(table).getByText("SB-0003")).toBeTruthy();
    expect(within(table).getByText("Approved, ready to release")).toBeTruthy();
    expect(within(table).getByText("26,000.43")).toBeTruthy();
    await noViolations(document.body);
  });

  it("still shows what a role may see when another part is refused, and says nothing is linked when it is not", async () => {
    answer({
      "GET /v1/people-link": { status: 200, body: { state: "none", sources: [], inbox: [] } },
      "GET /v1/payroll-arrangements": { status: 403, body: { code: "API-AUTHZ-01", detail: "no", fix: "no" } },
      "GET /v1/salary-batches": { status: 200, body: { items: [] } },
    });
    renderPage(Payroll, { path: "/payroll", session: payroll });
    expect(await screen.findByText(/Payroll is posted by manual journal/)).toBeTruthy();
    expect(await screen.findByText(/not shown to your role/)).toBeTruthy();
    expect(await screen.findByText(/No pay run has sent its salary lines yet/)).toBeTruthy();
  });
});
