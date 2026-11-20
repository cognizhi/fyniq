import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ReportBuilder } from "./ReportBuilder";

type Saved = components["schemas"]["ReportDefinition"];
type Run = components["schemas"]["ReportRun"];
type Period = components["schemas"]["Period"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const period = (id: string, starts: string, ends: string): Period => ({ id, starts_on: starts, ends_on: ends, status: "open", financial_year: 1, version: 1 });
const periods = [period("p-oct", "2026-10-01", "2026-10-31"), period("p-nov", "2026-11-01", "2026-11-30")];
const saved: Saved = {
  id: "11111111-1111-4111-8111-111111111111", name: "Gross margin", description: "", visibility: "shared", owner_id: ownerSession.user_id, mine: true, version: 1,
  updated_at: "2026-11-23T03:00:00Z", archived: false,
  definition: { schema: "fyniq-report@1", rows: [{ id: "rev", label: "Revenue", kind: "group", group: "Revenue" }, { id: "gm", label: "Margin per pound", kind: "calc", formula: "rev / 0", format: "ratio" }], columns: [{ key: "current" }, { key: "prior_year" }], filters: {} },
};
const run: Run = {
  currency: "SGD", from: "2026-11-01", to: "2026-11-30", ledger_revision: 42, platform_version: "0.1.0",
  columns: [{ key: "current", label: "Nov 2026", available: true }, { key: "prior_year", label: "Prior year", available: false }],
  rows: [
    { id: "rev", label: "Revenue", kind: "group", format: "amount", cells: [{ value: "6000.00" }, { not_available: true }] },
    { id: "gm", label: "Margin per pound", kind: "calc", format: "ratio", cells: [{ not_available: true }, { not_available: true }] },
  ],
};

// req: FR-RPT-04
// req: FR-RPT-07
describe("the report builder", () => {
  it("lists the saved reports, shows the figures with not available where a figure cannot be worked out, and reads at one revision; axe finds nothing", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/report-definitions": { status: 200, body: { items: [saved] } },
      "GET /v1/members": { status: 200, body: { items: [] } },
      "GET /v1/report-schedules": { status: 200, body: { items: [] } },
      "POST /v1/report-builder/run": { status: 200, body: run },
    });
    renderPage(ReportBuilder, { path: "/reports/builder", session: accountant, mode: "expert" });
    fireEvent.click(await screen.findByRole("button", { name: "Gross margin" }));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Gross margin");
    fireEvent.click(screen.getByRole("button", { name: /Show the figures/ }));
    const table = await screen.findByRole("table", { name: /The report from 2026-11-01 to 2026-11-30/ });
    const rows = within(table).getAllByRole("row").map((r) => r.textContent);
    expect(rows).toEqual(["RowNov 2026Prior year", "Revenue6,000.00not available", "Margin per poundnot availablenot available"]);
    expect(screen.getByText(/Read at ledger revision 42/)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/report-builder/run")!.body).toMatchObject({ definition: { schema: "fyniq-report@1" } });
    await noViolations(document.body);
  });

  it("saves a change as the next version over the report's ETag, and shows what the server answered", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/report-definitions": { status: 200, body: { items: [saved] } },
      "GET /v1/members": { status: 200, body: { items: [] } },
      "GET /v1/report-schedules": { status: 200, body: { items: [] } },
      "PUT /v1/report-definitions/11111111-1111-4111-8111-111111111111": { status: 200, body: { ...saved, name: "Gross margin, revised", version: 2 } },
    });
    renderPage(ReportBuilder, { path: "/reports/builder", session: accountant, mode: "expert" });
    fireEvent.click(await screen.findByRole("button", { name: "Gross margin" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Gross margin, revised" } });
    fireEvent.click(screen.getByRole("button", { name: /Save as the next version/ }));
    await waitFor(() => expect(screen.getByText(/is saved as version 2/)).toBeTruthy());
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe('"1"');
    expect(put.body).toMatchObject({ name: "Gross margin, revised", visibility: "shared" });
  });
});
