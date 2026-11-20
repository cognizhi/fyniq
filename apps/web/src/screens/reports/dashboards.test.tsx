import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Dashboards } from "./Dashboards";

type Period = components["schemas"]["Period"];
const controller = { ...ownerSession, roles: ["controller"] } as Session;
const periods: Period[] = [{ id: "p", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open", financial_year: 1, version: 1 }];
const kpiItem = (key: string, name: string) => ({ key, name, version: `${key}@1`, unit: "amount", source: "ledger", formula: "x", date_basis: "the month end", month: { value: "100.00" }, before: { value: "90.00" } });
const kpis = { month: "2026-10", from: "2026-10-01", to: "2026-10-31", currency: "SGD", ledger_revision: 42, platform_version: "0.1.0", counts: {}, kpis: [kpiItem("cash", "Cash"), kpiItem("runway", "Runway")] };

// req: FR-VIZ-09
// req: FR-KPI-05
describe("the dashboard", () => {
  it("shows the tiles of the layout that applies, and saves a change as the person's own over its ETag; axe finds nothing", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: kpis },
      "GET /v1/dashboards/mine": { status: 200, body: { source: "role", role: "controller", version: 0, generated_at: "2026-11-23T03:00:00Z", tiles: [{ tile: "cash", x: 0, y: 0, w: 4, h: 1 }] }, headers: { ETag: '"0"' } },
      "GET /v1/dashboard-tiles": { status: 200, body: { items: [{ tile: "cash", name: "Cash", definition: "x" }, { tile: "runway", name: "Runway", definition: "y" }] } },
      "PUT /v1/dashboards/mine": { status: 200, body: { source: "user", version: 1, generated_at: "2026-11-23T03:00:00Z", tiles: [] } },
    });
    renderPage(Dashboards, { path: "/dashboards", session: controller, mode: "basic" });
    expect(await screen.findByText(/the arrangement set for controller/)).toBeTruthy();
    expect(await screen.findByText("Cash")).toBeTruthy();
    await screen.findByRole("option", { name: "Runway" });
    fireEvent.change(screen.getByLabelText("Add a tile"), { target: { value: "runway" } });
    fireEvent.click(screen.getByRole("button", { name: /Add it/ }));
    fireEvent.click(screen.getByRole("button", { name: /Save my dashboard/ }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe('"0"');
    expect(put.body).toMatchObject({ tiles: [{ tile: "cash", x: 0, y: 0, w: 4, h: 1 }, { tile: "runway", x: 4, y: 0, w: 4, h: 1 }] });
    await noViolations(document.body);
  });
});
