import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { EvidenceSearch, recordLink } from "./EvidenceSearch";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const found = {
  id: "ev1", file_name: "AU-100.pdf", evidence_type: "supplier invoice", media_type: "application/pdf", size_bytes: 1200, data_class: "general",
  uploaded_by: "user:u1", uploaded_at: "2026-10-20T01:00:00Z", state: "final",
  record: { activity: "bill", type: "bill", id: "b1", number: "BILL-0012", date: "2026-10-20", status: "posted" },
};
const at = { path: "/compliance/evidence", session: auditor };
const q = (s: { query: string }) => new URLSearchParams(s.query);

// link is a download link to a file, as createEvidenceDownload answers (SEC-24).
const link = (id: string, fileName: string, mediaType: string) => ({
  url: `/api/v1/evidence-downloads/${id}.signed`, expires_at: "2026-10-15T09:05:00Z", file_name: fileName, media_type: mediaType, sha256: "a".repeat(64),
});

// req: FR-DOC-10
// req: FR-DOC-06
describe("evidence search", () => {
  it("finds files by kind of record, type and date, and lists what is missing; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/evidence": { status: 200, body: { items: [found, { ...found, id: "ev2", file_name: "staff.csv", unmasked_nric: true, record: { ...found.record, id: "j1", type: "journal", activity: "journal", number: "JNL-0007" } }], activities: ["bill", "journal"] } },
      "GET /v1/checks/evidence.missing": {
        status: 200,
        body: {
          key: "evidence.missing", status: "warning",
          items: [
            { kind: "bill", id: "b9", label: "BILL-0019 of 2026-10-22", detail: "No the supplier's invoice or receipt attached" },
            { kind: "statutory_filing", id: "f1", label: "gst_f5:2026-09-30", detail: "Recorded as filed with no the submission or payment acknowledgement attached" },
          ],
        },
      },
    });
    renderPage(EvidenceSearch, at);
    const table = await screen.findByRole("table", { name: "Evidence found" });
    expect(within(table).getByRole("link", { name: "BILL-0012" }).getAttribute("href")).toBe("/purchases/bills/b1");
    expect(within(table).getByText("Shows an NRIC")).toBeTruthy();
    const missing = await screen.findByRole("list", { name: "Missing evidence" });
    expect(within(missing).getByRole("link", { name: "BILL-0019 of 2026-10-22" }).getAttribute("href")).toBe("/purchases/bills/b9");
    // A filing is named as one and opens the calendar, not a bill's page.
    expect(within(missing).getByText("Filing")).toBeTruthy();
    expect(within(missing).getByRole("link", { name: "gst_f5:2026-09-30" }).getAttribute("href")).toBe("/compliance/calendar");
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Records"), { target: { value: "journal" } });
    fireEvent.change(screen.getByLabelText("Evidence type"), { target: { value: "calc" } });
    fireEvent.change(screen.getByLabelText("Records from"), { target: { value: "2026-10-01" } });
    fireEvent.click(screen.getByLabelText("Only files I attached"));
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/evidence" && q(s).get("activity") === "journal")).toBe(true));
    const last = seen.filter((s) => s.path === "/v1/evidence").at(-1)!;
    expect(q(last).get("evidence_type")).toBe("calc");
    expect(q(last).get("from")).toBe("2026-10-01");
    expect(q(last).get("uploaded_by")).toBe(`user:${auditor.user_id}`);
  });

  // req: FR-DOC-01
  it("opens each file from its row: a picture shows below the list, any other file is saved under a safe name", async () => {
    const seen = answer({
      "GET /v1/evidence": {
        status: 200,
        body: { items: [{ ...found, id: "ev1", file_name: "receipt.png", media_type: "image/png" }, { ...found, id: "ev2", file_name: "../AU: 100?.pdf" }], activities: ["bill"] },
      },
      "GET /v1/checks/evidence.missing": { status: 200, body: { key: "evidence.missing", status: "ok", items: [] } },
      "POST /v1/evidence/ev1/download": { status: 200, body: link("ev1", "receipt.png", "image/png") },
      "POST /v1/evidence/ev2/download": { status: 200, body: link("ev2", "../AU: 100?.pdf", "application/pdf") },
    });
    const saved: { name: string; href: string }[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push({ name: this.download, href: this.getAttribute("href") ?? "" });
    });
    renderPage(EvidenceSearch, at);
    fireEvent.click(await screen.findByRole("button", { name: "Show receipt.png" }));
    const picture = await screen.findByRole("img", { name: "receipt.png, as attached" });
    expect(picture.getAttribute("src")).toBe("/api/v1/evidence-downloads/ev1.signed");
    fireEvent.click(screen.getByRole("button", { name: "Save ../AU: 100?.pdf" }));
    await vi.waitFor(() => expect(saved).toEqual([{ name: "AU_ 100_.pdf", href: "/api/v1/evidence-downloads/ev2.signed" }]));
    expect(seen.filter((x) => x.path.endsWith("/download")).map((x) => `${x.method} ${x.path}`)).toEqual(["POST /v1/evidence/ev1/download", "POST /v1/evidence/ev2/download"]);
    await noViolations(document.body);
    vi.restoreAllMocks();
  });

  it("says why a file did not open, and shows the next page of files on asking", async () => {
    const seen = answer({
      "GET /v1/evidence": (_b, req) => {
        const next = new URL(req.url).searchParams.get("cursor");
        return next === "c2"
          ? { status: 200, body: { items: [{ ...found, id: "ev3", file_name: "older.pdf", record: { ...found.record, id: "f9", type: "statutory_filing", activity: "filing", number: undefined } }], activities: ["bill", "filing"] } }
          : { status: 200, body: { items: [found], activities: ["bill", "filing"], next_cursor: "c2" } };
      },
      "GET /v1/checks/evidence.missing": { status: 200, body: { key: "evidence.missing", status: "ok", items: [] } },
      "POST /v1/evidence/ev1/download": { status: 404, body: { type: "about:blank", title: "Not found", status: 404, code: "API-NOT-FOUND", detail: "No file you may open has that ID.", fix: "Open it from its record's Evidence panel." } },
    });
    renderPage(EvidenceSearch, at);
    fireEvent.click(await screen.findByRole("button", { name: "Save AU-100.pdf" }));
    expect(await screen.findByText("The file could not be opened")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show more" }));
    const table = screen.getByRole("table", { name: "Evidence found" });
    expect(await within(table).findByText("older.pdf")).toBeTruthy();
    expect(within(table).getByRole("link", { name: "Statutory filings and payments" }).getAttribute("href")).toBe("/compliance/calendar");
    expect(seen.filter((s) => s.path === "/v1/evidence").map((s) => q(s).get("cursor"))).toEqual([null, "c2"]);
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("links each kind of record to its page", () => {
    expect(recordLink("expense_claim", "c1")).toBe("/purchases/claims/c1");
    expect(recordLink("equity_transaction", "t1")).toBe("/equity/transactions/t1");
    expect(recordLink("option_pool", "p1")).toBe("/equity/options/resolutions/p1");
    expect(recordLink("director_settlement", "s1")).toBe("/accounting/directors");
    expect(recordLink("gst_forecast", "g1")).toBe("/compliance/gst");
    expect(recordLink("holder_record", "h1")).toBeUndefined();
  });
});
