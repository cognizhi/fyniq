import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AIConnectionPage } from "./AIConnection";

const admin = { ...ownerSession, roles: ["administrator"] } as Session;
const evidence = { kind: "source", url: "https://example.com/data-residency", title: "Data residency", checked_on: "2026-11-01" };
const connection = {
  id: "c-1", name: "Claude in Singapore", connector: "anthropic", auth: "api-key", configuration: { region: "sg" }, host: "api.example.com", self_hosted: false,
  credential: { set: true, set_at: "2026-11-03T02:00:00Z" }, billing: { mode: "usage" }, auto_qualify: false, state: "enabled", version: 4,
  data_handling: { handling: [{ model: "*", endpoint: "api.example.com", feature: "*", processed_in: ["SG"], retention_days: 0, training: "no", evidence }], inventory_id: "inv-1" },
  created_by: "user:u-1", created_at: "2026-11-01T02:00:00Z", changed_by: "user:u-1", changed_at: "2026-11-02T02:00:00Z",
};
const entry = {
  id: "anthropic", name: "Anthropic", publisher: "Fyniq", version: "1.0.0", summary: "Claude models.", scope: "organisation", capabilities: [], data_classes: [], regions: [],
  destinations: [], secrets: [], webhooks: [], health_check: "", test_action: "", local_substitute: "",
  configuration: { type: "object", properties: { region: { type: "string", title: "Region", description: "Where it runs." } } },
  model: { features: ["tools"], auth: ["api-key"], presets: [] },
};
const qualified = { id: "q-1", agent: "chat", standing: "qualified", result: "pass", cases: 4, passed: 4, suite: "chat", suite_version: 1, threshold: "1", run: "r-1",
  ran_at: "2026-11-05T02:00:00Z", ran_by: "user:u-1" };
const price = { id: "p-1", number: 1, source: "litellm", match: "exact", currency: "USD", rates: [{ kind: "input", unit: "per_million_tokens", amount: "3" },
  { kind: "output", unit: "per_million_tokens", amount: "15" }], status: "active", ignored: [], recorded_at: "2026-11-05T02:00:00Z", recorded_by: "system:ai.prices.read" };
const model = (m: Record<string, unknown>) => ({ first_listed: "2026-11-01T02:00:00Z", last_listed: "2026-11-14T02:00:00Z", priced: true, qualifications: [], reports: [],
  serves: false, allowed: false, state: "available", ...m });
const models = {
  last_complete: new Date(Date.now() - 3600 * 1000).toISOString(),
  reads: [{ read_at: "2026-11-14T02:00:00Z", read_by: "system:ai.models", outcome: "complete", pages: 1, added: 0, removed: 0, trigger: "daily" }],
  models: [
    model({ model: "claude-sonnet", allowed: true, serves: true, price, probe: { at: "2026-11-05T02:00:00Z", ok: true }, qualifications: [qualified],
      pending_price: { ...price, id: "p-2", status: "pending", review: "normalised", review_detail: "the list's key differed in case" } }),
    model({ model: "claude-haiku", probe: { at: "2026-11-05T02:00:00Z", ok: true } }),
    model({ model: "claude-new" }),
    model({ model: "claude-gone", state: "no_longer_listed" }),
  ],
};
const routes = { agents: [{ agent: "chat", name: "Assistant", description: "", handles: ["know-how"], line: "main",
  route: { version: 2, classes: ["know-how"], made_at: "2026-11-05T02:00:00Z", made_by: "user:u-1",
    steps: [{ connection_id: "c-1", connection: "Claude in Singapore", model: "claude-sonnet", cache: false, qualification: "qualified", serves: true }] } }] };

function page(extra = {}) {
  return answer({
    "GET /v1/ai/connections/c-1": { status: 200, body: connection, headers: { ETag: '"4"' } },
    "GET /v1/connector-catalogue": { status: 200, body: { items: [entry] } },
    "GET /v1/ai/connections/c-1/models": { status: 200, body: models },
    "GET /v1/ai/routes": { status: 200, body: routes },
    "GET /v1/members": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Ah Kow" }] } },
    ...extra,
  });
}

const open = (session = admin) => renderPage(AIConnectionPage, { path: "/settings/ai/providers/c-1", route: "/settings/ai/providers/$connection", session });

// req: AI-72
// req: AI-75
// req: AI-78
// req: AI-79
// req: AI-80
// req: AI-82
// req: AI-88
describe("Settings › AI › AI providers › a connection", () => {
  it("shows its status, key, settings and the agents that use it; turning it off takes effect at once; axe finds nothing", async () => {
    const seen = page({ "POST /v1/ai/connections/c-1/disable": { status: 200, body: { ...connection, state: "disabled", version: 5 } } });
    open();
    expect(await screen.findByRole("heading", { name: "Claude in Singapore" })).toBeTruthy();
    expect(await screen.findByText(/added by Ah Kow/)).toBeTruthy();
    expect(screen.getByText(/never shown again/)).toBeTruthy();
    expect(await screen.findByText("Assistant")).toBeTruthy();
    expect(screen.getByText("primary: claude-sonnet")).toBeTruthy();
    expect(await screen.findByText("sg")).toBeTruthy();
    expect(screen.queryByText("Models that draw at another rate")).toBeNull();
    await noViolations();
    fireEvent.click(screen.getByRole("button", { name: "Turn it off" }));
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/ai/connections/c-1/disable")).toBe(true));
    const post = seen.find((x) => x.path.endsWith("/disable"));
    expect(post?.headers.get("If-Match")).toBe('"4"');
    expect(screen.queryByText("Waiting for a second approver")).toBeNull();
  });

  it("lists the allowed models with their qualifications and prices, probes and asks to allow a listed one, and never offers one no longer listed", async () => {
    const seen = page({
      "POST /v1/ai/connections/c-1/models/probe": { status: 200, body: { model: "claude-new", probe: { at: "2026-11-14T02:00:00Z", ok: true } } },
      "POST /v1/ai/connections/c-1/models/allow": { status: 202, body: { id: "a-7", status: "pending" } },
    });
    open();
    fireEvent.click(await screen.findByRole("tab", { name: "Models" }));
    expect(await screen.findByText("chat: Qualified")).toBeTruthy();
    expect(screen.getByText(/USD input 3 a million tokens, output 15 a million tokens, from Fyniq's model price list/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Confirm it" })).toBeNull();
    expect(screen.queryByText("claude-gone")).toBeNull();
    expect((screen.getByRole("button", { name: "Ask to allow claude-new" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Probe claude-new" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/ai/connections/c-1/models/probe")).toBe(true));
    expect(seen.find((x) => x.path.endsWith("/probe"))?.body).toEqual({ model: "claude-new" });
    fireEvent.click(screen.getByRole("button", { name: "Ask to allow claude-haiku" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/ai/connections/c-1/models/allow")).toBe(true));
    const allow = seen.find((x) => x.path.endsWith("/allow"));
    expect(allow?.body).toEqual({ model: "claude-haiku" });
    expect(allow?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(await screen.findByText("Waiting for a second approver")).toBeTruthy();
    await noViolations();
  });

  it("lets the Owner review a pending price and ask for their own, and warns when the provider's list is old", async () => {
    const seen = page({
      "GET /v1/ai/connections/c-1/models": { status: 200, body: { ...models, last_complete: "2026-11-01T02:00:00Z" } },
      "POST /v1/ai/prices/p-2/review": { status: 200, body: { ...price, id: "p-2" } },
      "POST /v1/ai/connections/c-1/models/price": { status: 202, body: { id: "a-8", status: "pending" } },
    });
    open(ownerSession as Session);
    fireEvent.click(await screen.findByRole("tab", { name: "Models" }));
    expect(await screen.findByText("The provider's list is more than 3 days old")).toBeTruthy();
    expect(screen.getByText(/the list's key differed in case/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Confirm it" }));
    await waitFor(() => expect(seen.find((x) => x.path === "/v1/ai/prices/p-2/review")?.body).toEqual({ decision: "confirm" }));
    fireEvent.click(screen.getByRole("button", { name: "Enter a price for claude-sonnet" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("A million input tokens"), { target: { value: "0" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to use this price" }));
    expect(await within(dialog).findByText("Give the price of a million input tokens, above zero.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("A million input tokens"), { target: { value: "2.50" } });
    fireEvent.change(within(dialog).getByLabelText("A million output tokens"), { target: { value: "10" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to use this price" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/ai/connections/c-1/models/price")).toBe(true));
    expect(seen.find((x) => x.path.endsWith("/price"))?.body).toEqual({ model: "claude-sonnet", currency: "USD",
      rates: [{ kind: "input", amount: "2.50" }, { kind: "output", amount: "10" }] });
  });

  it("asks to replace the key, never sending an empty one", async () => {
    const seen = page({ "PUT /v1/ai/connections/c-1/credential": { status: 202, body: { id: "a-9", status: "pending" } } });
    open();
    fireEvent.click(await screen.findByRole("button", { name: "Replace the key" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to replace it" }));
    expect(await within(dialog).findByText("Paste the new API key.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("New API key"), { target: { value: "not-a-real-key" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to replace it" }));
    await waitFor(() => expect(seen.some((x) => x.method === "PUT")).toBe(true));
    const put = seen.find((x) => x.method === "PUT");
    expect(put?.body).toEqual({ value: "not-a-real-key" });
    expect(put?.headers.get("If-Match")).toBe('"4"');
    expect(await screen.findByText("Waiting for a second approver")).toBeTruthy();
  });

  it("shows where its prompts go and asks to change it with the whole definition", async () => {
    const seen = page({ "PUT /v1/ai/connections/c-1": { status: 202, body: { id: "a-10", status: "pending" } } });
    open();
    fireEvent.click(await screen.findByRole("tab", { name: "Data handling" }));
    expect(await screen.findByText("Not kept: zero data retention")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Data residency" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Kept by the provider (days; 0 for none)"), { target: { value: "30" } });
    fireEvent.change(within(dialog).getByLabelText("Its transfer assessment (ID), outside Singapore"), { target: { value: "tr-1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask to change it" }));
    await waitFor(() => expect(seen.some((x) => x.method === "PUT")).toBe(true));
    const put = seen.find((x) => x.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"4"');
    expect(put?.body).toMatchObject({
      name: "Claude in Singapore",
      auth: "api-key",
      billing: { mode: "usage" },
      configuration: { region: "sg" },
      data_handling: { handling: [{ model: "*", processed_in: ["SG"], retention_days: 30, training: "no", evidence }], inventory_id: "inv-1", transfer_id: "tr-1" },
    });
  });

  // req: AI-73
  it("shows a plan's per-model rates with the terms they come from", async () => {
    page({
      "GET /v1/ai/connections/c-1": { status: 200, body: { ...connection, billing: { mode: "subscription", quota: { amount: 300, unit: "requests", window: "month",
        fee: "19.00", currency: "USD", rates: [{ model: "kimi-big", rate: "10" }], terms: { url: "https://example.com/plan-terms", title: "Plan terms",
        checked_on: "2026-11-01" } } } } },
    });
    open();
    expect(await screen.findByText("Models that draw at another rate")).toBeTruthy();
    expect(screen.getByText(/kimi-big 10; others 1/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Plan terms" })).toBeTruthy();
  });
});
