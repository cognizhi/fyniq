import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AIActivity } from "./AIActivity";
import { MyAIPlan } from "./MyAIPlan";

const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const tokens = { input: 1000, output: 500, cache_read: 200, cache_write: 0, reasoning: 0 };
const entry = (e: Record<string, unknown>) => ({ id: "e-1", run: "r-1", agent: "chat", route_version: 2, step: 1, connection_id: "c-1", connection: "Claude in Singapore",
  model_requested: "claude-sonnet", person: "user:u-1", classes: ["business-figures"], outcome: "ok", tokens, latency_ms: 900, started_at: "2026-11-14T02:00:00Z",
  masked: false, ...e });
const items = [
  entry({ id: "e-3", step: 2, fell_back_from: 1, connection: "Gemini", model_requested: "gemini-pro", model_reported: "gemini-pro-002", version_reported: "002",
    cost: "0.004", currency: "USD", cost_base: "0.0054" }),
  entry({ id: "e-2", outcome: "timeout", detail: "the provider did not answer in 60 seconds" }),
  entry({ id: "e-4", person: "user:u-2", agent: "payroll_explainer", classes: ["payroll"], outcome: "ok", masked: true, route_version: 1, cost_base: "0.01" }),
];

// req: AI-12
// req: AI-76
describe("Settings › AI activity", () => {
  it("shows every member's entries to the Owner, with fallbacks, versions, exact costs and masked payroll; axe finds nothing", async () => {
    answer({
      "GET /v1/ai/activity": { status: 200, body: { scope: "all", items, next_cursor: "n-1" } },
      "GET /v1/ai/routes": { status: 200, body: { agents: [{ agent: "chat", name: "Assistant", description: "", handles: [], line: "main" }] } },
      "GET /v1/members": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Ah Kow" }, { user_id: "u-2", display_name: "Wei Ling" }] } },
    });
    renderPage(AIActivity, { path: "/settings/ai/activity" });
    expect(await screen.findByText("Every member's entries")).toBeTruthy();
    const table = screen.getByRole("table", { name: "Model calls" });
    const fallback = within(table).getAllByRole("row")[1] as HTMLElement;
    expect(within(fallback).getByText("route v2, step 2, fell back from step 1")).toBeTruthy();
    expect(within(fallback).getByText("gemini-pro → gemini-pro-002 (002)")).toBeTruthy();
    expect(within(fallback).getByText("0.0054")).toBeTruthy();
    expect(within(fallback).getByText("USD 0.004")).toBeTruthy();
    expect(within(fallback).getByText("1,200 / 500")).toBeTruthy();
    expect(within(table).getByText("the provider did not answer in 60 seconds")).toBeTruthy();
    expect(within(table).getByText("Masked: it carried payroll")).toBeTruthy();
    expect(within(table).getByText("Wei Ling")).toBeTruthy();
    expect(screen.getByLabelText("Member")).toBeTruthy();
    expect(within(screen.getByRole("group", { name: "In the entries shown" })).getAllByText("1")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Show more" })).toBeTruthy();
    await noViolations();
  });

  it("shows a member only their own, filters by agent and day, and narrows to one call's fallbacks", async () => {
    const seen = answer({
      "GET /v1/ai/activity": { status: 200, body: { scope: "own", items: items.slice(0, 2) } },
      "GET /v1/ai/routes": problem(403, "API-AUTHZ-01"),
      "GET /v1/members": problem(403, "API-AUTHZ-01"),
    });
    renderPage(AIActivity, { path: "/settings/ai/activity", session: bookkeeper });
    expect(await screen.findByText("Your own entries")).toBeTruthy();
    expect(screen.queryByLabelText("Member")).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "For" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "chat" } });
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-11-01" } });
    await waitFor(() => expect(seen.some((x) => x.query.includes("agent=chat") && x.query.includes("from=2026-11-01"))).toBe(true));
    fireEvent.click((await screen.findAllByRole("button", { name: /and its fallbacks/ }))[0] as HTMLElement);
    await waitFor(() => expect(seen.some((x) => x.query.includes("run=r-1"))).toBe(true));
    expect(screen.getByText("Showing one call and its fallbacks.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show every call" }));
    expect(screen.queryByText("Showing one call and its fallbacks.")).toBeNull();
  });

  it("says when there is nothing yet", async () => {
    answer({ "GET /v1/ai/activity": { status: 200, body: { scope: "own", items: [] } }, "GET /v1/ai/routes": problem(403, "API-AUTHZ-01"), "GET /v1/members": problem(403, "API-AUTHZ-01") });
    renderPage(AIActivity, { path: "/settings/ai/activity", session: bookkeeper });
    expect(await screen.findByText(/No AI activity yet/)).toBeTruthy();
  });
});

// req: AI-73
describe("Profile › Use your own AI plan", () => {
  it("says the option is not offered and what is never supported; axe finds nothing", async () => {
    answer({});
    renderPage(MyAIPlan, { path: "/settings/profile/ai-plan", session: bookkeeper, mode: "basic" });
    expect(await screen.findByText("Not offered yet")).toBeTruthy();
    expect(screen.getByText("Reusing a chat session or a browser cookie.")).toBeTruthy();
    await noViolations();
  });
});
