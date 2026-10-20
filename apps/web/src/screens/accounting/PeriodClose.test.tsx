import { fireEvent, screen } from "@testing-library/react";
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

// req: FR-GL-07
// req: FM-4
describe("the period close page", () => {
  it("runs the checks and shows what they found; axe finds nothing", async () => {
    let ran = false;
    answer({
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
});
