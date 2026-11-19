import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ConnectedApps } from "./ConnectedApps";

const view = {
  access: { opted_in: false, organisation_on: true, apps: ["claude-code"], can_connect: false, etag: '"none"' },
  registration_on: true,
  apps: [
    { id: "claude-code", name: "Claude Code", publisher: "Anthropic", verified: true, allowed: true, status: "first_party" },
    { id: "claude-desktop", name: "Claude Desktop", publisher: "Anthropic", verified: true, allowed: false, status: "first_party" },
    { id: "mcp-0123456789abcdef0123", name: "Acme Books", verified: false, allowed: false, status: "pending" },
  ],
  connections: [{ client_id: "claude-code", name: "Claude Code", scopes: ["openid", "fyniq.read.reports"], granted_at: "2026-11-01T09:00:00Z", expires_at: "2027-01-30T09:00:00Z" }],
};

function page(extra = {}) {
  return answer({
    "GET /v1/connected-apps": { status: 200, body: view },
    "GET /v1/connected-apps/registrations": { status: 200, body: { registrations: [{ id: "mcp-0123456789abcdef0123", name: "Acme Books", redirect_uris: ["https://acme.example/cb"], registered_at: "2026-11-18T09:00:00Z" }] } },
    "GET /v1/connected-apps/activity": { status: 200, body: { calls: [{ id: "c1", user_id: "u1", app: "claude-code", tool: "metrics.query", outcome: "read", facts: 1, at: "2026-11-19T01:00:00Z" }] } },
    ...extra,
  });
}

// req: AI-44
// req: AI-35
// req: AI-90
describe("Connected apps", () => {
  it("says what the organisation allows and what the person has allowed, marks an app Fyniq has not verified, and axe finds nothing", async () => {
    page();
    renderPage(ConnectedApps, { path: "/settings/connected-apps" });
    expect(await screen.findByText("AI apps cannot connect as you")).toBeTruthy();
    const apps = screen.getByRole("list", { name: "Apps" });
    expect(within(apps).getAllByText("Verified")).toHaveLength(2);
    expect(within(apps).getByText("Not verified")).toBeTruthy();
    expect(within(apps).getByText("Registered itself: Fyniq has not verified who publishes it")).toBeTruthy();
    const mine = screen.getByRole("list", { name: "Your connections" });
    expect(within(mine).getByText(/Access: openid, fyniq.read.reports/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("opts in over the choice's ETag, disconnects an app, and denies a waiting registration, showing where it sends people", async () => {
    const seen = page({
      "PUT /v1/me/mcp": { status: 200, body: { ...view.access, opted_in: true, can_connect: true, etag: '"1"' } },
      "DELETE /v1/connected-apps/connections/claude-code": { status: 204 },
      "POST /v1/connected-apps/registrations/mcp-0123456789abcdef0123/deny": { status: 204 },
    });
    renderPage(ConnectedApps, { path: "/settings/connected-apps" });
    fireEvent.click(await screen.findByRole("button", { name: "Allow AI apps to work for me" }));
    await waitFor(() => expect(calls()).toContain("PUT /v1/me/mcp"));
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"none"');
    expect(put?.body).toEqual({ opted_in: true });
    fireEvent.click(screen.getByRole("button", { name: "Disconnect Claude Code" }));
    await waitFor(() => expect(calls()).toContain("DELETE /v1/connected-apps/connections/claude-code"));
    const waiting = await screen.findByRole("list", { name: "Registrations waiting for an Owner" });
    expect(within(waiting).getByText(/sends you to https:\/\/acme.example\/cb/)).toBeTruthy();
    fireEvent.click(within(waiting).getByRole("button", { name: "Deny Acme Books" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/connected-apps/registrations/mcp-0123456789abcdef0123/deny"));
  });

  it("shows nothing waiting or done when the person may not see it", async () => {
    page({ "GET /v1/connected-apps/registrations": { status: 403, body: { code: "API-AUTHZ-01", detail: "no", title: "no", status: 403 } }, "GET /v1/connected-apps/activity": { status: 200, body: { calls: [] } } });
    renderPage(ConnectedApps, { path: "/settings/connected-apps" });
    await screen.findByText("AI apps cannot connect as you");
    expect(screen.queryByRole("list", { name: "Registrations waiting for an Owner" })).toBeNull();
    expect(screen.queryByRole("list", { name: "Recent activity" })).toBeNull();
  });
});
