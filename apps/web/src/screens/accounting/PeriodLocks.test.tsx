import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Periods } from "./Periods";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const controller = { ...ownerSession, roles: ["controller"] } as Session;
const oct = { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open" as const, financial_year: 1, version: 1 };
const nov = { id: "p11", starts_on: "2026-11-01", ends_on: "2026-11-30", status: "soft_locked" as const, financial_year: 1, version: 2, locked_modules: ["ar"] };
const base = {
  "GET /v1/periods": { status: 200, body: { items: [oct, nov] } },
  "GET /v1/soft-lock-exceptions": {
    status: 200,
    body: { items: [{ id: "x1", period_id: "p11", entry_id: "e1", user_id: "u1", scope: "period", source_type: "bill", posting_date: "2026-11-03", reason: "A late bill for the auditor", created_at: "2026-11-21T09:00:00Z" }] },
  },
};

// req: FR-GL-09
describe("the locks of the periods page", () => {
  it("shows a soft-locked month and a locked module with their words, and the exceptions report; axe finds nothing", async () => {
    answer(base);
    renderPage(Periods, { path: "/accounting/close", route: "/accounting/close", session: accountant });
    expect(await screen.findByText("Soft-locked")).toBeTruthy();
    expect(screen.getByText("Sales locked")).toBeTruthy();
    expect(await screen.findByText("A late bill for the auditor")).toBeTruthy();
    await noViolations(document.body);
  });

  it("soft-locks a month for an Accountant with If-Match and an Idempotency-Key, and makes a pass only with a reason", async () => {
    answer({ ...base, "POST /v1/periods/p10/soft-lock": { status: 200, body: { ...oct, status: "soft_locked", version: 2 }, headers: { ETag: `"2-soft_locked"` } }, "POST /v1/periods/p11/soft-lock-passes": { status: 201, body: {} } });
    renderPage(Periods, { path: "/accounting/close", route: "/accounting/close", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Locks for October 2026" }));
    fireEvent.click(await screen.findByRole("button", { name: "Soft-lock the month" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/periods/p10/soft-lock")).toBe(true));
    const sent = seen.find((s) => s.path === "/v1/periods/p10/soft-lock")!;
    expect(sent.headers.get("If-Match")).toBe(`"1-open"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("offers a Controller the release only with a reason, and an Accountant no release", async () => {
    answer({ ...base, "POST /v1/periods/p11/unlock": { status: 200, body: { ...nov, status: "open" } } });
    renderPage(Periods, { path: "/accounting/close", route: "/accounting/close", session: controller });
    fireEvent.click(await screen.findByRole("button", { name: "Locks for November 2026" }));
    const release = await screen.findByRole("button", { name: "Release the soft lock" });
    expect((release as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText(/Why \(kept in the audit trail/), { target: { value: "The auditors have what they need." } });
    expect((release as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(release);
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/periods/p11/unlock")).toBe(true));
    expect((seen.find((s) => s.path === "/v1/periods/p11/unlock")!.body as { reason: string }).reason).toBe("The auditors have what they need.");
    // No pass for a Controller: that is the Accountant's alone.
    expect(screen.queryByRole("button", { name: "Make a pass" })).toBeNull();
  });
});
