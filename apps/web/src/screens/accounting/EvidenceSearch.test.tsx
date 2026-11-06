import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { EvidenceSearch } from "./EvidenceSearch";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const found = {
  id: "ev1", file_name: "AU-100.pdf", evidence_type: "supplier invoice", media_type: "application/pdf", size_bytes: 1200, data_class: "general",
  uploaded_by: "user:u1", uploaded_at: "2026-10-20T01:00:00Z", state: "final",
  record: { activity: "bill", type: "bill", id: "b1", number: "BILL-0012", date: "2026-10-20", status: "posted" },
};

// req: FR-DOC-10
// req: FR-DOC-06
describe("evidence search", () => {
  it("finds files by kind of record, type and date, and lists what is missing; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/evidence": { status: 200, body: { items: [found, { ...found, id: "ev2", file_name: "staff.csv", unmasked_nric: true, record: { ...found.record, id: "j1", type: "journal", activity: "journal", number: "JNL-0007" } }], activities: ["bill", "journal"] } },
      "GET /v1/checks/evidence.missing": { status: 200, body: { key: "evidence.missing", status: "warning", items: [{ kind: "bill", id: "b9", label: "BILL-0019 of 2026-10-22", detail: "No the supplier's invoice or receipt attached" }] } },
    });
    renderPage(EvidenceSearch, { path: "/compliance/evidence", session: auditor });
    const table = await screen.findByRole("table", { name: "Evidence found" });
    expect(within(table).getByRole("link", { name: "BILL-0012" }).getAttribute("href")).toBe("/purchases/bills/b1");
    expect(within(table).getByText("Shows an NRIC")).toBeTruthy();
    const missing = await screen.findByRole("list", { name: "Missing evidence" });
    expect(within(missing).getByRole("link", { name: "BILL-0019 of 2026-10-22" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Records"), { target: { value: "journal" } });
    fireEvent.change(screen.getByLabelText("Evidence type"), { target: { value: "calc" } });
    fireEvent.change(screen.getByLabelText("Records from"), { target: { value: "2026-10-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    const q = (s: { query: string }) => new URLSearchParams(s.query);
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/evidence" && q(s).get("activity") === "journal")).toBe(true));
    const last = seen.filter((s) => s.path === "/v1/evidence").at(-1)!;
    expect(q(last).get("evidence_type")).toBe("calc");
    expect(q(last).get("from")).toBe("2026-10-01");
  });
});
