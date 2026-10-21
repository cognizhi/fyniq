import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PeriodClose } from "./PeriodClose";

type State = components["schemas"]["PeriodCloseState"];

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const owner = { ...ownerSession, roles: ["owner"] } as Session;
const period = { id: "p10", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open" as const, financial_year: 1, version: 1 };
const checks = {
  id: "c1",
  checked_by: { kind: "user" as const, id: "u1", name: "Ah Kow" },
  checked_at: "2026-11-03T01:30:00Z",
  ledger_revision: 41,
  clean: true,
  stale: false,
  checks: [
    { key: "earlier", severity: "ok" as const, title: "Every earlier period is closed" },
    { key: "drafts", severity: "warning" as const, title: "Draft journals are dated in the period", detail: "2 draft journal(s)" },
  ],
};
const at = (route = "/accounting/close/p10") => ({ path: route, route: "/accounting/close/$periodId" });
// October is the first month: it has no earlier month to compare (FR-GL-14).
const firstMonth = {
  "GET /v1/periods/p10/variances": { status: 200, body: { period, threshold_amount: { amount: "1000.00", currency: "SGD" }, threshold_percent: "10", unexplained: 0, items: [] } },
};

// req: FR-GL-07
// req: FM-4
describe("the period close page", () => {
  it("runs the checks and shows what they found; axe finds nothing", async () => {
    let ran = false;
    answer({
      ...firstMonth,
      "GET /v1/periods/p10/close": () => ({ status: 200, body: ran ? { period, checks, you_may_attest: false } : { period, you_may_attest: false }, headers: { ETag: `"1-open"` } }),
      "POST /v1/periods/p10/close-checks": () => {
        ran = true;
        return { status: 200, body: { period, checks, you_may_attest: false } as State };
      },
    });
    renderPage(PeriodClose, { ...at(), session: controller });
    expect(await screen.findByRole("heading", { name: "Close October 2026" })).toBeTruthy();
    expect(screen.getByText(/Run the checks first/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Run the checks" }));
    expect(await screen.findByText("The checks are clean: another founder can attest and close.")).toBeTruthy();
    expect(await screen.findByText("Draft journals are dated in the period")).toBeTruthy();
    expect(screen.getByText(/Checks run by Ah Kow/)).toBeTruthy();
    // He ran them: another founder attests.
    expect(screen.getByText("Another founder attests: you ran the checks")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.headers.get("If-Match")).toBe(`"1-open"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    await noViolations(document.body);
  });

  it("lets another founder attest and close, with what they attest", async () => {
    let closed = false;
    const record = { id: "x1", checks_id: "c1", attested_by: { kind: "user" as const, id: "u2", name: "Wei Ling" }, attested_at: "2026-11-03T02:00:00Z", statement: "Reviewed.", closed_revision: 41, snapshot_sha256: "3f9a".padEnd(60, "0") + "c21e" };
    answer({
      ...firstMonth,
      "GET /v1/periods/p10/close": () => ({
        status: 200,
        body: closed ? { period: { ...period, status: "closed", closed_revision: 41, version: 2 }, close: record, you_may_attest: false } : { period, checks, you_may_attest: true },
        headers: { ETag: closed ? `"2-closed"` : `"1-open"` },
      }),
      "POST /v1/periods/p10/close": () => {
        closed = true;
        return { status: 200, body: { period: { ...period, status: "closed" }, close: record, you_may_attest: false } };
      },
    });
    renderPage(PeriodClose, { ...at(), session: owner });
    const statement = await screen.findByLabelText("What you attest");
    expect((statement as HTMLTextAreaElement).value).toContain("I have reviewed the close of October 2026");
    fireEvent.change(statement, { target: { value: "Reviewed." } });
    fireEvent.click(screen.getByRole("button", { name: "Attest and close" }));
    expect(await screen.findByText("October 2026 is closed.")).toBeTruthy();
    const sent = seen.find((s) => s.method === "POST")!;
    expect(sent.body).toEqual({ checks_id: "c1", statement: "Reviewed." });
    expect(sent.headers.get("If-Match")).toBe(`"1-open"`);
    expect(await screen.findByText("Books closed")).toBeTruthy();
    expect(screen.getByText("sha256 3f9a…c21e")).toBeTruthy();
    expect(screen.getByText(/Attested by Wei Ling/)).toBeTruthy();
  });

  it("says when the checks block the close or are out of date, and shows the refusal", async () => {
    answer({
      ...firstMonth,
      "GET /v1/periods/p10/close": {
        status: 200,
        body: { period, checks: { ...checks, clean: false, stale: true, checks: [{ key: "earlier", severity: "error", title: "Earlier periods are open" }] }, you_may_attest: false },
        headers: { ETag: `"1-open"` },
      },
      "POST /v1/periods/p10/close-checks": problem(409, "CLOSE-01", "The period is closed.", "Only an open period is checked."),
    });
    renderPage(PeriodClose, { ...at(), session: controller });
    expect(await screen.findByText("1 check blocks the close")).toBeTruthy();
    expect(screen.getByText("The checks are out of date")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Run the checks again" }));
    expect(await screen.findByText("The checks did not run")).toBeTruthy();
  });

  // req: FR-GL-08
  it("asks to reopen a closed month with a reason, then shows the request and the reopening", async () => {
    const record = { id: "x1", checks_id: "c1", attested_by: { kind: "user" as const, id: "u2", name: "Wei Ling" }, attested_at: "2026-11-03T02:00:00Z", statement: "Reviewed.", closed_revision: 41, snapshot_sha256: "3f9a".padEnd(60, "0") + "c21e" };
    const closed = { ...period, status: "closed" as const, closed_revision: 41, version: 2 };
    let asked = false;
    answer({
      ...firstMonth,
      "GET /v1/periods/p10/close": () => ({ status: 200, body: asked ? { period: closed, close: record, you_may_attest: false, reopen_asked: "a1" } : { period: closed, close: record, you_may_attest: false }, headers: { ETag: `"2-closed"` } }),
      "POST /v1/periods/p10/reopen": () => {
        asked = true;
        return { status: 202, body: { id: "a1", status: "pending" } };
      },
    });
    renderPage(PeriodClose, { ...at(), session: controller });
    fireEvent.click(await screen.findByRole("button", { name: "Ask to reopen…" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to reopen" }));
    expect(await within(dialog).findByText(/Say why/)).toBeTruthy();
    expect(seen.filter((x) => x.method === "POST")).toEqual([]);
    fireEvent.change(within(dialog).getByLabelText("Why it reopens"), { target: { value: "A late invoice" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to reopen" }));
    expect(await screen.findByText("Asked to reopen October 2026: another founder approves it.")).toBeTruthy();
    const sent = seen.find((x) => x.method === "POST")!;
    expect(sent.body).toEqual({ reason: "A late invoice" });
    expect(sent.headers.get("If-Match")).toBe(`"2-closed"`);
    expect(sent.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText("A request to reopen October 2026 waits for approval")).toBeTruthy();
    expect(screen.getByRole("link", { name: "See the request" }).getAttribute("href")).toBe("/approvals/a1");
    expect(screen.queryByRole("button", { name: "Ask to reopen…" })).toBeNull();
  });

  it("shows the latest reopening with its snapshots", async () => {
    answer({
      ...firstMonth,
      "GET /v1/periods/p10/close": {
        status: 200,
        body: { period, checks, you_may_attest: false, reopen: { id: "r1", approval_id: "a1", reopened_by: { kind: "user", id: "u2", name: "Wei Ling" }, reopened_at: "2026-11-05T01:00:00Z", reason: "A late invoice", reopened_revision: 42, before_sha256: "aaaa".padEnd(60, "0") + "bbbb", after_sha256: "cccc".padEnd(60, "0") + "dddd" } },
        headers: { ETag: `"3-open"` },
      },
    });
    renderPage(PeriodClose, { ...at(), session: controller });
    expect(await screen.findByText("Reopened")).toBeTruthy();
    expect(screen.getByText(/Approved by Wei Ling/)).toBeTruthy();
    expect(screen.getByText("sha256 aaaa…bbbb")).toBeTruthy();
    expect(screen.getByText("sha256 cccc…dddd")).toBeTruthy();
  });
});
