import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AdminConsole } from "./AdminConsole";

// req: FR-ADM-07
describe("the installation console", () => {
  it("says the platform is degraded and why, lists the jobs and the backlog, and warns of a single Super admin; axe finds nothing", async () => {
    answer({
      "GET /v1/installation/health": { status: 200, body: { status: "degraded", reasons: ["The Scheduler's tick last ran 40 minutes ago."], as_at: "2026-11-20T09:30:00Z",
        jobs: [{ name: "audit.anchor", last_beat: "2026-11-20T09:00:00Z", detail: "ok" }, { name: "tick", last_beat: "2026-11-20T08:50:00Z" }],
        outbox: { pending: 3, dead: 1, oldest_pending: "2026-11-20T09:20:00Z" } } },
      "GET /v1/installation/super-admins": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Wei Ling Tan", added_at: "2026-10-01T09:00:00Z" }], warning: "Only one Super admin: invite a second." } },
    });
    renderPage(AdminConsole, { path: "/settings/installation" });
    expect(await screen.findByText("Degraded")).toBeTruthy();
    expect(screen.getByText("The Scheduler's tick last ran 40 minutes ago.")).toBeTruthy();
    const jobs = screen.getByRole("table", { name: "Platform jobs" });
    expect(within(jobs).getByText("audit.anchor")).toBeTruthy();
    expect(within(jobs).getByText("tick")).toBeTruthy();
    expect(screen.getByText("Background work that failed").nextElementSibling?.textContent).toBe("1");
    expect(await screen.findByText("Only one Super admin: invite a second.")).toBeTruthy();
    expect(screen.getByText("Wei Ling Tan")).toBeTruthy();
    await noViolations(document.body);
  });

  it("says healthy when it is, and says plainly when a read is refused", async () => {
    answer({
      "GET /v1/installation/health": { status: 200, body: { status: "ok", reasons: [], jobs: [], outbox: { pending: 0, dead: 0 }, as_at: "2026-11-20T09:30:00Z" } },
      "GET /v1/installation/super-admins": { status: 403, body: { type: "about:blank", title: "Forbidden", status: 403, code: "API-AUTHZ-01", detail: "Listing the Super admins is for a Super admin of this installation.", fix: "Ask a Super admin to do it." } },
    });
    renderPage(AdminConsole, { path: "/settings/installation" });
    expect(await screen.findByText("Healthy")).toBeTruthy();
    expect(screen.getByText("No platform job has run yet.")).toBeTruthy();
    expect(await screen.findByText("The Super admins could not be listed")).toBeTruthy();
  });
});
