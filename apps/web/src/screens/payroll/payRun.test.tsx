import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PayRun } from "./PayRun";

const payroll = { ...ownerSession, roles: ["payroll_administrator"] } as Session;

// req: FR-PPL-09
// part of FR-PPL-10, FR-PPL-11, FR-PAY-13 (the page; the checks and the posting are the API's)
describe("a pay run from Nemura", () => {
  it("shows each version's steps and what it posted by account, and a refused run in words with its code; axe finds nothing", async () => {
    answer({
      "GET /v1/payroll-runs": {
        status: 200,
        body: {
          items: [
            { run_id: "11111111-aaaa", version: 1, wage_month: "2026-10", outcome: "posted", entry_id: "e1", mapping_version: 3, received_at: "2026-10-30T09:00:00Z", posted: [{ account_id: "a1", amount: "20000.00" }, { account_id: "a2", amount: "-26780.00" }] },
            { run_id: "11111111-aaaa", version: 2, wage_month: "2026-10", outcome: "unchanged", mapping_version: 3, received_at: "2026-10-31T09:00:00Z", posted: [] },
          ],
          refused: [{ run_id: "22222222-bbbb", version: 1, code: "PPL-14", message: "The run was calculated against another account mapping than Fyniq's current one", refused_at: "2026-10-30T10:00:00Z" }],
        },
      },
      "GET /v1/accounts": { status: 200, body: { items: [{ id: "a1", code: "6011", name: "Salaries" }, { id: "a2", code: "2100", name: "Salaries payable" }] } },
    });
    renderPage(PayRun, { path: "/payroll/runs/2026-10", route: "/payroll/runs/$month", session: payroll });
    const v1 = await screen.findByRole("region", { name: "Version 1" });
    expect(within(v1).getByText(/Posted as a journal entry/)).toBeTruthy();
    expect(await within(v1).findByText("6011 Salaries")).toBeTruthy();
    expect(within(v1).getByText("20,000.00")).toBeTruthy();
    expect(within(screen.getByRole("region", { name: "Version 2" })).getByText(/Changed nothing/)).toBeTruthy();
    const refused = screen.getByRole("region", { name: "Refused" });
    expect(within(refused).getByText(/another account mapping than Fyniq's current one/)).toBeTruthy();
    expect(within(refused).getByText(/PPL-14/)).toBeTruthy();
    expect(screen.queryByText(/Ana|salary of/i)).toBeNull();
    await noViolations(document.body);
  });

  it("says nothing arrived for a month with no run", async () => {
    answer({ "GET /v1/payroll-runs": { status: 200, body: { items: [], refused: [] } }, "GET /v1/accounts": { status: 200, body: { items: [] } } });
    renderPage(PayRun, { path: "/payroll/runs/2026-09", route: "/payroll/runs/$month", session: payroll });
    expect(await screen.findByText(/No pay run has arrived for this month/)).toBeTruthy();
  });
});
