import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ShareGrants } from "./ShareGrants";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const grant = { id: "55555555-5555-4555-8555-555555555555", version: 1, recipient: "Wei Jie Ong", fair_value: "0.50", valuation: "", service_start: "2026-01-01", tranches: [{ shares: 12000, vest_on: "2026-12-31" }], expense_account: "a-exp", reserve_account: "a-res" };
const accounts = { items: [{ id: "a-exp", code: "6010", name: "Salaries", type: "expense", active: true, group: false, version: 1 }, { id: "a-res", code: "3250", name: "Share-based payment reserve", type: "equity", active: true, group: false, version: 1 }, { id: "a-cap", code: "3010", name: "Share capital", type: "equity", role: "share-capital", active: true, group: false, version: 1 }] };
const schedule = { grant_id: grant.id, total: "6000.00", tranches: [{ months: 12, total: "6000.00", parts: ["500.00"] }], months: [{ month: "2026-01", expense: "500.00", journal_id: "66666666-6666-4666-8666-666666666666" }, { month: "2026-02", expense: "500.00" }], note: "No tax deduction is claimed for newly issued shares." };

// req: FR-EQ-13
// req: QA-UI-05
describe("ShareGrants", () => {
  it("opens a grant, shows its expense by month and proposes a month; the share capital account is not offered as the reserve; axe finds nothing", async () => {
    answer({
      "GET /v1/share-grants": { status: 200, body: { items: [grant] } },
      ["GET /v1/share-grants/" + grant.id]: { status: 200, body: grant, headers: { ETag: '"1-draft"' } },
      ["GET /v1/share-grants/" + grant.id + "/schedule"]: { status: 200, body: schedule },
      "GET /v1/accounts": { status: 200, body: accounts },
      "POST /v1/share-grant-months": { status: 201, body: { grant_id: grant.id, month: "2026-02", expense: "500.00", journal_id: "77777777-7777-4777-8777-777777777777" } },
    });
    renderPage(ShareGrants, { path: "/equity/share-grants", session: accountant, mode: "expert" });
    await screen.findByRole("option", { name: /Wei Jie Ong, from 2026-01-01/ });
    fireEvent.change(screen.getByLabelText("Open a grant"), { target: { value: grant.id } });
    expect(await screen.findByText("Expense over the service months: 6000.00")).toBeTruthy();
    expect(screen.queryByRole("option", { name: /3010 Share capital/ })).toBeNull();
    expect(screen.getByRole("option", { name: /3250 Share-based payment reserve/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Propose 2026-02/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/share-grant-months")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/share-grant-months")!.body).toEqual({ grant_id: grant.id, month: "2026-02" });
    await noViolations(document.body);
  });
});
