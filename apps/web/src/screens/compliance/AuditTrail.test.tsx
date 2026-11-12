import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AuditTrail } from "./AuditTrail";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const user = (id: string) => ({ type: "user", id });
const summary = {
  id: "e2", seq: 342, occurred_at: "2026-10-12T08:04:07Z", actor: { type: "service", id: "proposal-executor" }, on_behalf_of: user("u-arjun"),
  originator: "p-309", action: "bill.updated", record: { type: "bill", id: "b-918", version: 2 }, reason: "Accepted AI categorisation", fields: ["category", "payee_account"],
};
const view = {
  ...summary,
  approval_ids: ["apr-2207"],
  request: { id: "req-1", session: "s-1", details: "erased" },
  changes: [
    { field: "category", before: "6120", after: "5010" },
    { field: "payee_account", before: { fingerprint: "x" }, after: { fingerprint: "y" }, proof: "changed_back", hint: "******4471" },
    { field: "memo", before: null, after: "Cloud hosting" },
  ],
  prev_hash: "41b0",
  hash: "9f3a",
};
const verification = { from: "2026-10-01", to: "2026-10-31", first_seq: 118, last_seq: 342, events: 225, anchors: 31, close_anchors: 1, details: 12, details_checked: true, anchored_through: 342 };
const recovery = {
  id: "r1", epoch: 1, restored_seq: 4120, missing_from: 4121, missing_to: 4388, detected_at: "2026-11-10T02:00:04Z", anchor_key: "audit-anchors/o/recovery/1.json",
  runbook: "docs/runbooks/audit-restore.md", version: 1, acknowledged_by: null, acknowledged_at: null,
};
const purges = {
  items: [
    { kind: "trail", last_seq: 4120, last_occurred_at: "2019-03-31T15:59:59Z", approval: "retention.purge_after_retention v1", counts: { events: 4120, security_events: 310 }, anchor_key: "audit-boundaries/o/1.json", written_at: "2027-04-04T02:00:00Z" },
    { kind: "ledger", boundary: "2020-01-01", carried_revision: 18342, approval: "retention.purge_after_retention v1", counts: { entries_purged: 1840, carry_forward_entries: 212 }, anchor_key: "ledger-boundaries/o/2020-01-01.json", written_at: "2027-04-05T02:00:00Z" },
  ],
};
const q = (s: { query: string }) => new URLSearchParams(s.query);

// req: FR-CTL-03
// req: AUD-07
// req: AUD-02
describe("the audit trail", () => {
  it("finds events by the filters and opens one's before and after, a sensitive value only as its proof; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [summary, { ...summary, id: "e1", seq: 341, originator: undefined, action: "bill.created", on_behalf_of: undefined, actor: user("u-arjun") }] } },
      "GET /v1/audit/events/e2": { status: 200, body: view },
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    const list = await screen.findByRole("list", { name: "Audit events" });
    expect(within(list).getByText("bill.updated")).toBeTruthy();
    expect(await screen.findByText(/Nothing was purged after its retention/)).toBeTruthy();
    fireEvent.click(within(list).getByText("bill.updated"));
    const table = await screen.findByRole("table", { name: "Before and after" });
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["category", "6120", "5010"]);
    expect(within(rows[2]!).getByText("Changed back")).toBeTruthy();
    expect(within(rows[2]!).getByText("******4471")).toBeTruthy();
    expect(rows[2]!.textContent).not.toContain("fingerprint");
    expect(within(rows[3]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["memo", "—", "Cloud hosting"]);
    expect(screen.getByText(/were erased when the security-log retention ended/)).toBeTruthy();
    expect(screen.getByText("p-309")).toBeTruthy();
    await noViolations(document.body);

    fireEvent.change(screen.getByLabelText("Action"), { target: { value: "bill." } });
    fireEvent.click(screen.getByLabelText("Only what AI proposals changed"));
    fireEvent.change(screen.getByLabelText("Field"), { target: { value: "category" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit/events" && q(s).get("action") === "bill.")).toBe(true));
    const last = seen.filter((s) => s.path === "/v1/audit/events").at(-1)!;
    expect(q(last).get("ai")).toBe("any");
    expect(q(last).get("field")).toBe("category");
    expect(q(last).get("from")).toBe("2026-10-01");
  });

// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
  it("shows a person's sealed values opened, and a shredded one as shredded; axe finds nothing", async () => {
    const person = { ...summary, id: "e3", action: "contact.updated", record: { type: "contact", id: "c-1", version: 2 }, fields: ["legal_name", "phone"] };
    answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [person] } },
      "GET /v1/audit/events/e3": {
        status: 200,
        body: { ...view, ...person, changes: [
          { field: "legal_name", before: "Ada Tan", after: "Ada Lim", sealed: "opened" },
          { field: "phone", before: null, after: null, sealed: "shredded", shredded_year: "2019-12-31", shredded_on: "2027-01-03T17:50:00Z" },
        ] },
      },
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    fireEvent.click(within(await screen.findByRole("list", { name: "Audit events" })).getByText("contact.updated"));
    const rows = within(await screen.findByRole("table", { name: "Before and after" })).getAllByRole("row");
    expect(rows[1]!.textContent).toContain("Ada Lim");
    expect(within(rows[1]!).getByText("Sealed")).toBeTruthy();
    expect(within(rows[2]!).getByText("Shredded")).toBeTruthy();
    expect(rows[2]!.textContent).toContain("year ending 31 Dec 2019");
    await noViolations(document.body);
  });

    // req: AUD-03
  it("verifies a range of days: passed, failed where it was altered, or explained by a purge; axe finds nothing in each", async () => {
    let outcome: Record<string, unknown> = { ...verification, outcome: "passed" };
    answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [] } },
      "GET /v1/audit/verification": () => ({ status: 200, body: outcome }),
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    await screen.findByText("No event matches.");
    fireEvent.change(screen.getByLabelText("Events from"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getAllByLabelText("to")[0]!, { target: { value: "2026-10-31" } });
    const verify = screen.getByRole("button", { name: "Verify integrity" });
    fireEvent.click(verify);
    expect(await screen.findByText("Chain verified: 1 Oct 2026 to 31 Oct 2026")).toBeTruthy();
    expect(screen.getByText(/225 events recomputed against 31 signed anchors/)).toBeTruthy();
    await noViolations(document.body);

    outcome = { ...verification, outcome: "failed", failed_seq: 201, problem: "event 201 was altered: its body no longer gives its stored hash" };
    fireEvent.click(verify);
    expect(await screen.findByText("The audit trail was altered: 1 Oct 2026 to 31 Oct 2026")).toBeTruthy();
    expect(screen.getByText(/its body no longer gives its stored hash \(event 201\)/)).toBeTruthy();
    await noViolations(document.body);

    outcome = { ...verification, outcome: "explained", details_checked: false, problem: "the events up to 117 were purged after their retention" };
    fireEvent.click(verify);
    expect(await screen.findByText("Verified, with an explained gap: 1 Oct 2026 to 31 Oct 2026")).toBeTruthy();
    expect(screen.getByText(/The gap: the events up to 117 were purged after their retention\. This installation holds no fingerprint key/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("says why it cannot verify where the installation holds no anchors", async () => {
    answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [] } },
      "GET /v1/audit/verification": problem(503, "API-UNAVAILABLE-01", "This installation holds no audit anchors to verify against."),
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    fireEvent.click(await screen.findByRole("button", { name: "Verify integrity" }));
    expect(await screen.findByText("The audit trail could not be verified")).toBeTruthy();
  });

  // req: AUD-09
  // req: AUD-10
  it("lists the purges and a restore that waits, which the Owner acknowledges with its version; axe finds nothing", async () => {
    let acknowledged = false;
    const seen = answer({
      "GET /v1/audit/recoveries": () => ({
        status: 200,
        body: { items: [acknowledged ? { ...recovery, version: 2, acknowledged_by: "u-owner", acknowledged_at: "2026-11-10T09:12:00Z" } : recovery] },
      }),
      "POST /v1/audit/recoveries/r1/acknowledge": () => {
        acknowledged = true;
        return { status: 200, body: { ...recovery, version: 2 }, headers: { ETag: '"2"' } };
      },
      "GET /v1/audit/purges": { status: 200, body: purges },
      "GET /v1/audit/events": { status: 200, body: { items: [] } },
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail" });
    expect(await screen.findByText("The audit trail was restored behind its anchors")).toBeTruthy();
    const kept = await screen.findByRole("list", { name: "Purges and restores" });
    expect(within(kept).getByText(/The ledger before 1 Jan 2020 carried forward/)).toBeTruthy();
    expect(within(kept).getByText(/1,840 entries purged, 212 carry-forward entries/)).toBeTruthy();
    expect(within(kept).getByText(/The audit trail's events up to 4120/)).toBeTruthy();
    expect(within(kept).getByText("Waiting")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: /^Acknowledge: the restore found on/ }));
    await waitFor(() => expect(within(kept).getByText("Acknowledged")).toBeTruthy());
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.headers.get("If-Match")).toBe('"1"');
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();
    expect(screen.queryByText("The audit trail was restored behind its anchors")).toBeNull();
  });

  it("shows a restore that waits to an auditor without the Owner's button", async () => {
    answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [recovery] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [] } },
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    expect(await screen.findByText("The audit trail was restored behind its anchors")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Acknowledge/ })).toBeNull();
  });

  // req: AUD-05
  it("asks for the export with the filters searched with", async () => {
    const seen = answer({
      "GET /v1/audit/recoveries": { status: 200, body: { items: [] } },
      "GET /v1/audit/purges": { status: 200, body: { items: [] } },
      "GET /v1/audit/events": { status: 200, body: { items: [] } },
      "POST /v1/audit/exports": { status: 202, body: { id: "x1", report: "audit_trail", status: "pending", format: "csv", from: "2026-10-01", to: "2026-10-31" } },
      "GET /v1/report-exports": { status: 200, body: { items: [{ id: "x1", report: "audit_trail", status: "pending", format: "csv", from: "2026-10-01", to: "2026-10-31", asked_at: "2026-11-09T01:00:00Z" }] } },
    });
    renderPage(AuditTrail, { path: "/compliance/audit-trail", session: auditor });
    await screen.findByText("No event matches.");
    fireEvent.change(screen.getByLabelText("Action"), { target: { value: "security." } });
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/audit/events" && q(s).get("action") === "security.")).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    fireEvent.change(screen.getByLabelText("Export from"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("Format"), { target: { value: "csv" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask for the export" }));
    expect(await screen.findByText(/The export is being prepared/)).toBeTruthy();
    const post = seen.find((s) => s.method === "POST" && s.path === "/v1/audit/exports")!;
    expect(post.body).toMatchObject({ from: "2026-10-01", format: "csv", filters: { action: "security." } });
    expect(await screen.findByText(/Audit trail, 2026-10-01 to 2026-10-31/)).toBeTruthy();
    await noViolations(document.body);
  });
});
