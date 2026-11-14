import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AIUsage, shift } from "./AIUsage";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const person = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d09";
const totals = (cost: string, input: number, output: number) => ({
  requests: 12, served: 11, fallbacks: 1, errors: 1, skipped: 2, input_tokens: input, output_tokens: output, cache_read_tokens: 0, cache_write_tokens: 0,
  reasoning_tokens: 0, cost, unpriced: 0,
});
const usage = {
  month: "2026-11",
  currency: "SGD",
  total: totals("4.3125", 812000, 96000),
  by_connection: [{ connection_id: "c-2", name: "Kimi plan", ...totals("0", 1000, 100),
    quota: { amount: 1000000, unit: "tokens", window: "month", used: 908000, resets: "2026-11-30T16:00:00Z" } }],
  by_model: [{ connection_id: "c-1", model: "claude-opus-4-6", ...totals("4.3125", 812000, 96000) }],
  by_agent: [{ agent: "chat", ...totals("4.3125", 812000, 96000) }],
  by_person: [{ person: `user:${person}`, ...totals("4.3125", 812000, 96000) }],
};
const budgets = {
  month: "2026-11",
  currency: "SGD",
  budgets: [
    { key: "agent:chat", scope: "agent", unit: "base", subject: "chat", amount: "10", used: "4.3125", version: 1, alerted: false, set_by: "user:u-1" },
    { key: `tokens:person:${person}`, scope: "tokens_person", unit: "tokens", subject: `user:${person}`, amount: "3000000", used: "908000", version: 1,
      alerted: false, set_by: "user:u-1" },
  ],
};

// req: AI-77
// req: AI-73
// req: AI-13
describe("Settings › AI › Usage and budgets", () => {
  it("shows the month's use by agent, model and person against their budgets, and a subscription's quota; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/ai/usage": { status: 200, body: usage },
      "GET /v1/ai/budgets": { status: 200, body: budgets },
      "GET /v1/ai/routes": { status: 200, body: { agents: [{ agent: "chat", name: "Assistant", description: "", handles: [], line: "main" }] } },
      "GET /v1/ai/connections": { status: 200, body: { items: [{ id: "c-1", name: "Claude in Singapore", state: "enabled" }, { id: "c-2", name: "Kimi plan", state: "enabled" }] } },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m-1", user_id: person, display_name: "Arjun Rao", roles: [], expired_roles: [] }] } },
    });
    renderPage(AIUsage, { path: "/settings/ai/usage", session: auditor });
    const agents = await screen.findByRole("table", { name: "By agent" });
    const row = within(agents).getByRole("row", { name: /Assistant/ });
    expect(within(row).getByText("908,000")).toBeTruthy();
    expect(within(row).getByText("4.3125")).toBeTruthy();
    expect(within(row).getByText("43% of SGD 10")).toBeTruthy();
    const people = screen.getByRole("table", { name: "By person" });
    expect(within(people).getByText("Arjun Rao")).toBeTruthy();
    expect(within(people).getByText("30% of 3,000,000 tokens")).toBeTruthy();
    expect(within(screen.getByRole("table", { name: "By connection and model" })).getByText("claude-opus-4-6")).toBeTruthy();
    expect(screen.getByText(/Kimi plan: 908,000 of 1,000,000 tokens this month, 90%/)).toBeTruthy();
    await noViolations();
    fireEvent.click(screen.getByRole("button", { name: "The month before" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/ai/usage" && x.query === "?month=2026-10")).toBe(true));
  });

  it("moves by calendar months", () => {
    expect(shift("2026-01", -1)).toBe("2025-12");
    expect(shift("2026-11", 1)).toBe("2026-12");
    expect(shift("2026-12", 1)).toBe("2027-01");
  });
});
