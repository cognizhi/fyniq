import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer, calls } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { OperationsTray } from "./OperationsTray";

const running = { id: "op-1", kind: "report_export", status: "running", resource: "/v1/report-exports/x-1", requested_at: "2026-11-11T02:00:00Z" };
const done = { id: "op-2", kind: "report_export", status: "succeeded", resource: "/v1/report-exports/x-2", requested_at: "2026-11-11T01:00:00Z", finished_at: "2026-11-11T01:00:04Z" };
const failed = { id: "op-3", kind: "checks_run", status: "failed", resource: "/v1/checks", requested_at: "2026-11-11T00:00:00Z", finished_at: "2026-11-11T00:05:00Z" };
const brokenExport = { id: "op-4", kind: "report_export", status: "failed", resource: "/v1/report-exports/x-3", requested_at: "2026-11-10T23:00:00Z", finished_at: "2026-11-10T23:00:09Z" };
const exportOf = (id: string, rest: object) => ({ id, report: "trial_balance", format: "csv", to: "2026-10-31", requested_at: "2026-11-11T01:00:00Z", ...rest });
const exports = [
  exportOf("x-1", { status: "pending" }),
  exportOf("x-2", { status: "ready", file_name: "trial-balance-2026-10-31.csv", expires_at: "2026-11-12T01:00:04Z" }),
  exportOf("x-3", { report: "general_ledger", from: "2026-01-01", status: "failed", why: "the range crosses the start of a financial year: export each year apart" }),
];

// req: FR-INT-11, NFR-PERF-04, NFR-PERF-05
describe("the operations tray", () => {
  it("counts the running, lists the caller's work with indeterminate progress, downloads a finished export, says why one failed, and axe finds nothing", async () => {
    answer({
      "GET /v1/operations": { status: 200, body: { items: [running, done, failed, brokenExport] } },
      "GET /v1/report-exports": { status: 200, body: { items: exports } },
      "GET /v1/report-exports/x-2/file": { status: 200, body: { export_id: "x-2", file_name: "trial-balance-2026-10-31.csv", media_type: "text/csv", sha256: "ab", size_bytes: 3, rows: 1, ledger_revision: 42, content_base64: btoa("abc") } },
    });
    Object.assign(URL, { createObjectURL: vi.fn(() => "blob:x"), revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    renderPage(OperationsTray);
    const button = await screen.findByRole("button", { name: "Operations, 1 running" });
    expect(screen.getByRole("status").textContent).toBe("1 operation running");
    fireEvent.click(button);
    const sheet = await screen.findByRole("dialog");
    const list = within(sheet).getByRole("list", { name: "Your operations" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(4);
    const bar = await within(sheet).findByRole("progressbar", { name: "Export Trial balance (CSV): running" });
    expect(bar.getAttribute("aria-valuenow")).toBeNull(); // indeterminate: operations carry no percentage yet
    expect(within(sheet).getByText("Run every check")).toBeTruthy();
    expect(within(sheet).getByRole("link", { name: "Run the checks again" }).getAttribute("href")).toBe("/checks");
    expect(within(sheet).getByText(/the range crosses the start of a financial year/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(sheet).getByRole("button", { name: "Download trial-balance-2026-10-31.csv" }));
    await waitFor(() => expect(calls()).toContain("GET /v1/report-exports/x-2/file"));
    expect(click).toHaveBeenCalled();
    click.mockRestore();
  });

  // req: FR-MIG-01
  it("names a workbook's validation, says its report is ready, and how to try again when it failed", async () => {
    const validating = { id: "op-5", kind: "migration_validation", status: "running", resource: "/v1/migrations/m-1/report", requested_at: "2026-11-13T02:00:00Z" };
    const validated = { ...validating, id: "op-6", status: "succeeded", finished_at: "2026-11-13T02:00:20Z" };
    const stopped = { ...validating, id: "op-7", status: "failed", finished_at: "2026-11-13T02:10:00Z" };
    answer({ "GET /v1/operations": { status: 200, body: { items: [validating, validated, stopped] } } });
    renderPage(OperationsTray);
    fireEvent.click(await screen.findByRole("button", { name: "Operations, 1 running" }));
    const sheet = await screen.findByRole("dialog");
    expect(await within(sheet).findByRole("progressbar", { name: "Validate the workbook: running" })).toBeTruthy();
    expect(within(sheet).getByText(/the validation report is kept when it is done/)).toBeTruthy();
    expect(within(sheet).getByText("Its validation report is ready.")).toBeTruthy();
    expect(within(sheet).getByText(/Upload the workbook again/)).toBeTruthy();
    expect(calls()).not.toContain("GET /v1/report-exports");
    await noViolations(document.body);
  });

  it("shows no count when nothing runs, and says what shows there", async () => {
    answer({ "GET /v1/operations": { status: 200, body: { items: [] } } });
    renderPage(OperationsTray);
    fireEvent.click(await screen.findByRole("button", { name: "Operations" }));
    const sheet = await screen.findByRole("dialog");
    expect(await within(sheet).findByText(/Nothing yet/)).toBeTruthy();
    expect(calls()).not.toContain("GET /v1/report-exports");
  });
});
