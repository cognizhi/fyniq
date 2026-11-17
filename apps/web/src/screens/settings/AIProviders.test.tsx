import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AIProviders } from "./AIProviders";

const admin = { ...ownerSession, roles: ["administrator"] } as Session;
const connections = [
  { id: "c-1", name: "Claude in Singapore", connector: "anthropic", auth: "workload-identity", configuration: {}, host: "asia-southeast1-aiplatform.googleapis.com",
    self_hosted: false, credential: { set: false }, billing: { mode: "usage" }, data_handling: { handling: [] }, auto_qualify: false, state: "enabled", version: 2,
    created_by: "user:u-1", created_at: "2026-11-01T02:00:00Z", changed_by: "user:u-2", changed_at: "2026-11-02T02:00:00Z" },
  { id: "c-2", name: "Kimi plan", connector: "openai-compatible", preset: "kimi", auth: "api-key", configuration: {}, host: "api.moonshot.ai", self_hosted: false,
    credential: { set: true, set_at: "2026-11-03T02:00:00Z" }, billing: { mode: "subscription", quota: { amount: 1000000, unit: "tokens", window: "month", fee: "19.00", currency: "USD" } },
    data_handling: { handling: [] }, auto_qualify: false, state: "disabled", version: 1, created_by: "user:u-1", created_at: "2026-11-03T02:00:00Z",
    changed_by: "user:u-1", changed_at: "2026-11-03T02:00:00Z" },
];
const compatible = {
  id: "openai-compatible", name: "OpenAI-compatible", publisher: "Fyniq", version: "1.0.0", summary: "Any provider behind an OpenAI-compatible endpoint.",
  scope: "organisation", capabilities: [], data_classes: [], regions: [], destinations: [], secrets: [], webhooks: [], health_check: "", test_action: "", local_substitute: "",
  configuration: { type: "object", properties: { base_url: { type: "string", title: "API base", description: "The provider's base URL." } } },
  model: { features: ["tools"], auth: ["api-key", "none"], presets: [{ id: "kimi", name: "Moonshot Kimi", base_url: "https://api.moonshot.ai/v1", auth: ["api-key"], self_hosted: false }] },
};
const mail = { ...compatible, id: "zeptomail", name: "Zoho ZeptoMail", model: undefined };

function page(extra = {}) {
  return answer({
    "GET /v1/ai/connections": { status: 200, body: { items: connections } },
    "GET /v1/connector-catalogue": { status: 200, body: { items: [compatible, mail] } },
    "GET /v1/ai/data-policy": { status: 200, body: { version: 1, allow: {}, exclude: { "business-figures": ["c-2"] }, defaults: "" } },
    "GET /v1/ai/usage": { status: 200, body: { month: "2026-11", currency: "SGD", total: {}, by_connection: [{ connection_id: "c-1", name: "Claude in Singapore", cost: "12.5" }],
      by_model: [], by_agent: [], by_person: [] } },
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    ...extra,
  });
}

// req: AI-71
// req: AI-72
// req: AI-73
// req: AI-75
describe("Settings › AI › AI providers", () => {
  it("lists the connections, the data policy and the providers to add, but not other connectors; axe finds nothing", async () => {
    page();
    renderPage(AIProviders, { path: "/settings/ai/providers", session: admin });
    const table = await screen.findByRole("table", { name: "Connections" });
    const claude = within(table).getByRole("row", { name: /Claude in Singapore/ });
    expect(within(claude).getByText("On")).toBeTruthy();
    expect(within(claude).getByText("The service's own cloud account")).toBeTruthy();
    expect(within(claude).getByText("12.5")).toBeTruthy();
    const kimi = within(table).getByRole("row", { name: /Kimi plan/ });
    expect(within(kimi).getByText("Subscription, 1,000,000 tokens a month for USD 19.00")).toBeTruthy();
    expect(within(kimi).getByText("Set, never shown")).toBeTruthy();
    expect(screen.getByText("business-figures, never to: Kimi plan")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Set up OpenAI-compatible" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Set up Moonshot Kimi" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Set up Zoho ZeptoMail" })).toBeNull();
    await noViolations();
  });

  it("asks to add a connection from a preset, with its key and a subscription, and shows the request", async () => {
    const seen = page({ "POST /v1/ai/connections": { status: 202, body: { id: "a-9", status: "pending", summary: "Add the AI connection Kimi" } } });
    renderPage(AIProviders, { path: "/settings/ai/providers", session: admin });
    fireEvent.click(await screen.findByRole("button", { name: "Set up Moonshot Kimi" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to add it" }));
    expect(await within(dialog).findByText("Paste the provider's API key.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText(/API key/), { target: { value: "not-a-real-key" } });
    fireEvent.click(within(dialog).getByLabelText(/Subscription: a fixed fee with a quota/));
    fireEvent.change(within(dialog).getByLabelText("Quota"), { target: { value: "1000000" } });
    fireEvent.change(within(dialog).getByLabelText("Fee each time"), { target: { value: "19.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to add it" }));
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/ai/connections")).toBe(true));
    const post = seen.find((x) => x.method === "POST");
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(post?.body).toEqual({
      connector: "openai-compatible",
      name: "Moonshot Kimi",
      preset: "kimi",
      auth: "api-key",
      configuration: {},
      billing: { mode: "subscription", quota: { amount: 1000000, unit: "tokens", window: "month", fee: "19.00", currency: "USD" } },
      credential: "not-a-real-key",
      auto_qualify: false,
    });
    expect(await screen.findByText(/The connection is added once another member who approves it does/)).toBeTruthy();
  });
});
