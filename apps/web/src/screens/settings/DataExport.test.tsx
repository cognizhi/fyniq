import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { DataExport } from "./DataExport";

const setting = (key: string, label: string, kind: string, value: string) => ({
  key,
  label,
  kind,
  value,
  group: "Data region and retention",
  guide: "settings.organisation#data-region-and-retention",
  lengthen_only: kind === "years",
  needs_second_approver: true,
  recommended: value,
  status: "recommended",
  reason: "",
});
const settings = [
  setting("data.region", "Data region", "text", "asia-southeast1"),
  setting("retention.records_years", "Records kept for", "years", "7"),
  setting("retention.purge_after_retention", "Purge the ledger and the audit trail after retention", "switch", "false"),
  setting("profile.name", "Not shown", "text", "x"),
].map((s, i) => (i === 3 ? { ...s, group: "Profile" } : s));

const mine = `user:${ownerSession.user_id}`;
const exports = [
  { approval_id: "a-3", status: "awaiting_approval", requested_by: "user:u-2", requested_at: "2026-11-13T01:00:00Z" },
  {
    approval_id: "a-2",
    export_id: "e-2",
    status: "ready",
    requested_by: mine,
    requested_at: "2026-11-12T01:00:00Z",
    file: {
      name: "fyniq-export-2026-11-12.zip",
      sha256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      size: 5242880,
      rows: 125000,
      ledger_revision: 4211,
      ready_at: "2026-11-12T01:05:00Z",
      expires_at: "2026-11-13T01:05:00Z",
    },
  },
  { approval_id: "a-1", export_id: "e-1", status: "failed", requested_by: mine, requested_at: "2026-11-10T01:00:00Z", failure: "the sealed fields' keys are not configured" },
];

function page(extra = {}) {
  return answer({
    "GET /v1/organisation/settings": { status: 200, body: { items: settings } },
    "GET /v1/tenant-exports": { status: 200, body: { items: exports } },
    ...extra,
  });
}

afterEach(() => vi.restoreAllMocks());

// req: FR-MIG-04
describe("Data and export", () => {
  it("shows the data region and retention read only, and each full export with its state, and axe finds nothing", async () => {
    page();
    renderPage(DataExport, { path: "/settings/data" });
    expect(await screen.findByText("Google Cloud, Singapore (asia-southeast1)")).toBeTruthy();
    expect(screen.getByText("Fixed")).toBeTruthy();
    expect(screen.getByText("7 years after the end of their financial year")).toBeTruthy();
    expect(screen.queryByText("Not shown")).toBeNull();
    const list = await screen.findByRole("list", { name: "Full exports" });
    expect(within(list).getByText("Awaiting approval")).toBeTruthy();
    expect(within(list).getByText("Another member asked for it")).toBeTruthy();
    expect(within(list).getByRole("link", { name: "Open the approval" }).getAttribute("href")).toBe("/approvals/a-3");
    expect(within(list).getByText("fyniq-export-2026-11-12.zip")).toBeTruthy();
    expect(within(list).getByText(/5\.0 MB · 125,000 rows/)).toBeTruthy();
    expect(within(list).getByText(/the sealed fields' keys are not configured/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("asks for a full export once, with an idempotency key, and says who approves it", async () => {
    const seen = page({ "POST /v1/tenant-exports": { status: 202, body: { id: "a-4", status: "pending", object_type: "tenant_export" } } });
    renderPage(DataExport, { path: "/settings/data" });
    fireEvent.click(await screen.findByRole("button", { name: "Ask for a full export" }));
    expect(await screen.findByText("Asked: waiting for approval")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.path).toBe("/v1/tenant-exports");
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Approvals" }).getAttribute("href")).toBe("/approvals/a-4");
  });

  it("saves a ready export through its link, for the person who asked only", async () => {
    const go = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({ ...window.location, assign: go });
    page({
      "POST /v1/tenant-exports/e-2/download": {
        status: 200,
        body: { url: "/api/v1/tenant-export-downloads/t.s", expires_at: "2026-11-12T02:10:00Z", file_name: "fyniq-export-2026-11-12.zip", sha256: "9f86" },
      },
    });
    renderPage(DataExport, { path: "/settings/data" });
    const list = await screen.findByRole("list", { name: "Full exports" });
    const saves = within(list).getAllByRole("button", { name: "Save" });
    expect(saves).toHaveLength(1); // the failed one and another member's have none
    fireEvent.click(saves[0] as HTMLElement);
    await vi.waitFor(() => expect(go).toHaveBeenCalledWith("/api/v1/tenant-export-downloads/t.s"));
  });

  it("lets only an Administrator ask", async () => {
    page();
    renderPage(DataExport, { path: "/settings/data", session: { ...ownerSession, roles: ["owner"] } as Session });
    expect(((await screen.findByRole("button", { name: "Ask for a full export" })) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Only an Administrator can ask for it")).toBeTruthy();
  });
});
