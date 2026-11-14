import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AIRouteEditor, AIRoutes } from "./AIRoutes";

const admin = { ...ownerSession, roles: ["administrator"] } as Session;
const step = (connection_id: string, connection: string, model: string, extra = {}) => ({ connection_id, connection, model, cache: false, qualification: "qualified", serves: true, ...extra });
const route = { version: 3, classes: ["know-how", "business-figures"], made_at: "2026-11-10T02:00:00Z", made_by: "user:u-1", preset: "A",
  steps: [step("c-1", "Claude in Singapore", "claude-sonnet"), step("c-2", "Gemini", "gemini-pro", { qualification: "out_of_date", serves: false, why: "another version answered" })] };
const chat = { agent: "chat", name: "Assistant", description: "The assistant's chat and tool loop.", handles: ["know-how", "business-figures", "personal-data"], line: "main", route };
const capture = { agent: "capture", name: "Document capture", description: "Reads receipts and bills.", handles: ["business-figures"], line: "main" };
const conn = (id: string, name: string, state = "enabled") => ({ id, name, connector: "anthropic", auth: "api-key", configuration: {}, host: "api.example.com", self_hosted: false,
  credential: { set: true }, billing: { mode: "usage" }, data_handling: { handling: [] }, auto_qualify: false, state, version: 1, created_by: "user:u-1",
  created_at: "2026-11-01T02:00:00Z", changed_by: "user:u-1", changed_at: "2026-11-01T02:00:00Z" });
const listed = (models: string[]) => ({ reads: [], models: models.map((model) => ({ model, allowed: true, serves: true, state: "available", priced: true, qualifications: [], reports: [],
  first_listed: "2026-11-01T02:00:00Z", last_listed: "2026-11-14T02:00:00Z" })) });
const presets = { presets: [{ id: "A", name: "Singapore only", description: "Every call stays in Singapore.", targets: [],
  agents: [{ agent: "chat", notes: [], route: { classes: ["know-how"], preset: "A", steps: [{ connection_id: "c-3", model: "claude-opus", cache: true }] } }] },
  { id: "B", name: "Global", description: "The global endpoint.", targets: [], agents: [{ agent: "chat", notes: [], why: "No connection reaches it." }] }] };

function overview(extra = {}) {
  return answer({
    "GET /v1/ai/routes": { status: 200, body: { agents: [chat, capture] } },
    "GET /v1/ai/usage": { status: 200, body: { month: "2026-11", currency: "SGD", total: {}, by_connection: [], by_model: [], by_person: [],
      by_agent: [{ agent: "chat", cost: "4.25" }] } },
    "GET /v1/checks/ai.model-routes": { status: 200, body: { key: "ai.model-routes", ref: "C.2 #55", area: "AI", title: "The AI's routes can keep serving", status: "warning",
      worst: "warning", count: 1, detail: "One route needs attention.", explain: "", rules: "", nightly: true, items_hidden: false, ledger_revision: 1, owner: {},
      items: [{ kind: "ai_route", id: "chat", label: "Assistant fell back 9 times today", severity: "warning" }] } },
    "GET /v1/ai/presets": { status: 200, body: presets },
    "GET /v1/approvals": { status: 200, body: { items: [{ id: "a-1", object_type: "ai_route", object_key: "chat", status: "pending", summary: "Change the route of Assistant" }] } },
    ...extra,
  });
}

function editor(extra = {}) {
  return answer({
    "GET /v1/ai/routes/chat": { status: 200, body: { ...chat, versions: [route] }, headers: { ETag: '"3"' } },
    "GET /v1/ai/connections": { status: 200, body: { items: [conn("c-1", "Claude in Singapore"), conn("c-2", "Gemini"), conn("c-3", "Claude Opus"), conn("c-4", "Off one", "disabled")] } },
    "GET /v1/ai/connections/c-1/models": { status: 200, body: listed(["claude-sonnet", "claude-haiku"]) },
    "GET /v1/ai/connections/c-2/models": { status: 200, body: listed(["gemini-pro"]) },
    "GET /v1/ai/connections/c-3/models": { status: 200, body: listed(["claude-opus"]) },
    "GET /v1/ai/presets": { status: 200, body: presets },
    "GET /v1/members": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Ah Kow" }] } },
    ...extra,
  });
}

const openEditor = () => renderPage(AIRouteEditor, { path: "/settings/ai/routes/chat", route: "/settings/ai/routes/$agent", session: admin });

// req: AI-74
// req: AI-76
// req: AI-26
describe("Settings › AI › Model routes", () => {
  it("shows each agent's steps, qualification and cost, what needs attention and a waiting change; axe finds nothing", async () => {
    overview();
    renderPage(AIRoutes, { path: "/settings/ai/routes", session: admin });
    expect(await screen.findByRole("link", { name: "Assistant" })).toBeTruthy();
    expect(screen.getByText("Data: Know-how, Business figures")).toBeTruthy();
    expect(screen.getByText("Qualified")).toBeTruthy();
    expect(screen.getByText("To qualify again")).toBeTruthy();
    expect(screen.getByText("not serving: another version answered")).toBeTruthy();
    expect(screen.getByText("No route: the agent does not run until one is set.")).toBeTruthy();
    expect(await screen.findByText("4.25")).toBeTruthy();
    expect(await screen.findByText("Assistant fell back 9 times today")).toBeTruthy();
    expect(await screen.findByRole("link", { name: "Change the route of Assistant" })).toBeTruthy();
    expect(screen.getByText("A: Singapore only")).toBeTruthy();
    await noViolations();
  });
});

describe("Settings › AI › Model routes › an agent's route", () => {
  it("reorders the steps with buttons and saves a narrowing change at once, with the route's ETag", async () => {
    const seen = editor({ "PUT /v1/ai/routes/chat": { status: 200, body: { route: { ...route, version: 4 } } } });
    openEditor();
    expect(await screen.findByRole("heading", { name: "Route: Assistant" })).toBeTruthy();
    const save = screen.getByRole("button", { name: "Save the route" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Move step 1 up" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Move step 2 up" }));
    expect(screen.getByText("Unsaved changes")).toBeTruthy();
    expect((screen.getByLabelText("Connection", { selector: "#route-c-0" }) as HTMLSelectElement).value).toBe("c-2");
    expect(screen.queryByRole("option", { name: "Off one" })).toBeNull();
    fireEvent.click(save);
    await waitFor(() => expect(seen.some((x) => x.method === "PUT")).toBe(true));
    const put = seen.find((x) => x.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"3"');
    expect(put?.headers.get("Idempotency-Key")).toBeTruthy();
    expect(put?.body).toEqual({ classes: ["know-how", "business-figures"], steps: [
      { connection_id: "c-2", model: "gemini-pro", cache: false },
      { connection_id: "c-1", model: "claude-sonnet", cache: false },
    ] });
    expect(screen.queryByText("Waiting for the Owner or a second approver")).toBeNull();
    await noViolations();
  });

  it("checks a widening change without saving, then asks for it; a model comes only from the connection's allow-list", async () => {
    const seen = editor({
      "PUT /v1/ai/routes/chat": (_body: unknown, req: Request) =>
        req.headers.get("Prefer") === "return=preview" ? { status: 200, body: { approval: { id: "preview" } } } : { status: 202, body: { approval: { id: "a-5" } } },
    });
    openEditor();
    fireEvent.click(await screen.findByRole("checkbox", { name: "Personal data" }));
    fireEvent.click(screen.getByRole("button", { name: "Add a fallback" }));
    const connection = screen.getByLabelText("Connection", { selector: "#route-c-2" });
    fireEvent.change(connection, { target: { value: "c-1" } });
    const model = screen.getByLabelText("Model", { selector: "#route-m-2" });
    expect(within(model).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose a model", "claude-sonnet", "claude-haiku"]);
    fireEvent.change(model, { target: { value: "claude-haiku" } });
    fireEvent.click(screen.getByRole("button", { name: "Check it" }));
    expect(await screen.findByText(/it widens where the agent's data goes/)).toBeTruthy();
    expect(seen.filter((x) => x.method === "PUT")[0]?.headers.get("Prefer")).toBe("return=preview");
    fireEvent.click(screen.getByRole("button", { name: "Save the route" }));
    expect(await screen.findByText("Waiting for the Owner or a second approver")).toBeTruthy();
    expect(seen.filter((x) => x.method === "PUT")[1]?.body).toMatchObject({ classes: ["know-how", "business-figures", "personal-data"] });
  });

  it("fills from a preset that gives a route, and shows a refusal with its fix", async () => {
    editor({ "PUT /v1/ai/routes/chat": problem(422, "AI-ROUTE-02", "Claude Opus may not receive personal data.", "Choose a step with Singapore processing.") });
    openEditor();
    fireEvent.click(await screen.findByRole("button", { name: "Fill from preset A" }));
    expect(screen.queryByRole("button", { name: "Fill from preset B" })).toBeNull();
    expect((screen.getByLabelText("Model", { selector: "#route-m-0" }) as HTMLSelectElement).value).toBe("claude-opus");
    fireEvent.click(screen.getByRole("button", { name: "Save the route" }));
    expect(await screen.findByText(/Claude Opus may not receive personal data/)).toBeTruthy();
    expect(screen.getByText("Version 3, from preset A")).toBeTruthy();
  });
});
