import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AISettings } from "./AISettings";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const admin = { ...ownerSession, roles: ["administrator"] } as Session;
const person = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d09";

const aiSwitch = (value: string, version: number) => ({
  key: "features.ai_assistant",
  label: "AI assistant",
  group: "Features",
  kind: "switch",
  value,
  recommended: "false",
  status: value === "false" ? "recommended" : "changed",
  reason: "AI is off until an Owner turns it on with a second approver (closed by default); turning it off takes effect at once",
  guide: "/help/organisation-settings#features",
  needs_second_approver: true,
  off_at_once: true,
  version,
});
const budgets = {
  month: "2026-11",
  currency: "SGD",
  budgets: [
    { key: "organisation", scope: "organisation", unit: "base", amount: "300", used: "120.86", version: 1, alerted: false, set_by: "user:u-1" },
    { key: "tokens:organisation", scope: "tokens_organisation", unit: "tokens", amount: "60000000", used: "27278100", version: 1, alerted: false, set_by: "user:u-1" },
    { key: `tokens:person:${person}`, scope: "tokens_person", unit: "tokens", subject: `user:${person}`, amount: "3000000", used: "2430000", version: 2, alerted: true,
      set_by: "user:u-1" },
  ],
};
const agents = [
  { agent: "chat", name: "Assistant", description: "The assistant", handles: ["business-figures"], line: "main",
    route: { version: 1, classes: ["business-figures"], steps: [], made_by: "user:u-1", made_at: "2026-11-10T02:00:00Z" } },
  { agent: "kb_compiler", name: "Knowledge-base compiler", description: "Compiles the guide", handles: ["know-how"], line: "main" },
];
const connections = [
  { id: "c-1", name: "Claude in Singapore", state: "enabled" },
  { id: "c-2", name: "OpenAI", state: "disabled" },
];
const people = [{ id: "m-1", user_id: person, display_name: "Arjun Rao", roles: [], expired_roles: [] }];

function page(value: string, version: number, extra = {}) {
  return answer({
    "GET /v1/ai/settings": { status: 200, body: { switch: aiSwitch(value, version), budgets } },
    "GET /v1/ai/routes": { status: 200, body: { agents } },
    "GET /v1/ai/connections": { status: 200, body: { items: connections } },
    "GET /v1/members": { status: 200, body: { items: people } },
    ...extra,
  });
}

// req: AI-13
// req: AI-77
// req: AI-86
describe("Settings › AI", () => {
  it("shows the switch on, turns it off at once for the Owner after a second look, and lists the budgets; axe finds nothing", async () => {
    const seen = page("true", 1, { "POST /v1/ai/settings/switch-off": { status: 200, body: { switch: aiSwitch("false", 2), budgets } } });
    renderPage(AISettings, { path: "/settings/ai", session: owner });
    const sw = await screen.findByRole("region", { name: "The AI switch" });
    expect(within(sw).getByText("On")).toBeTruthy();
    const list = await screen.findByRole("list", { name: "Budgets" });
    expect(within(list).getByText("SGD 120.86 of SGD 300 used · 40%")).toBeTruthy();
    expect(within(list).getByText("27,278,100 tokens of 60,000,000 tokens used · 45%")).toBeTruthy();
    expect(within(list).getByText("Arjun Rao, in tokens")).toBeTruthy();
    expect(within(list).getByText("80% reached")).toBeTruthy();
    expect(screen.getByText("Claude in Singapore")).toBeTruthy();
    expect(screen.getByText(/1 of 2 have a route/)).toBeTruthy();
    await noViolations();
    fireEvent.click(within(sw).getByRole("button", { name: "Turn all AI off now" }));
    fireEvent.click(within(sw).getByRole("button", { name: "Yes, turn it off now" }));
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/ai/settings/switch-off")).toBe(true));
    expect(seen.find((x) => x.path === "/v1/ai/settings/switch-off")?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("asks to turn the switch on, which waits for a second approver; only the Owner changes budgets", async () => {
    const seen = page("false", 2, {
      "PATCH /v1/organisation/settings/features.ai_assistant": { status: 202, body: { id: "a-1", status: "pending" } },
    });
    renderPage(AISettings, { path: "/settings/ai", session: admin });
    const sw = await screen.findByRole("region", { name: "The AI switch" });
    expect(within(sw).getByText("Off")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a budget" })).toBeNull();
    fireEvent.click(within(sw).getByRole("button", { name: "Ask to turn it on" }));
    expect(await within(sw).findByText(/turns on once another member who approves settings approves it/)).toBeTruthy();
    const patch = seen.find((x) => x.method === "PATCH");
    expect(patch?.headers.get("If-Match")).toBe('"2"');
    expect(patch?.body).toEqual({ value: "true" });
  });

  it("lets the Owner change a budget and add one in tokens", async () => {
    const seen = page("true", 1, {
      "PUT /v1/ai/budgets/organisation": { status: 200, body: budgets.budgets[0] },
      "PUT /v1/ai/budgets/tokens:person:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d09": { status: 200, body: budgets.budgets[2] },
    });
    renderPage(AISettings, { path: "/settings/ai", session: owner });
    fireEvent.click(await screen.findByRole("button", { name: "Change the budget of The organisation" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Amount a month (SGD)"), { target: { value: "500.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the budget" }));
    await waitFor(() => expect(seen.some((x) => x.method === "PUT" && x.path === "/v1/ai/budgets/organisation")).toBe(true));
    const put = seen.find((x) => x.path === "/v1/ai/budgets/organisation");
    expect(put?.headers.get("If-Match")).toBe('"1"');
    expect(put?.body).toEqual({ amount: "500.00" });

    fireEvent.click(await screen.findByRole("button", { name: "Add a budget" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Budget for"), { target: { value: `tokens:person:${person}` } });
    fireEvent.change(within(dialog).getByLabelText("Tokens a month"), { target: { value: "1.5" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the budget" }));
    expect(await within(dialog).findByText("Enter a whole number of tokens, such as 5000000.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText("Tokens a month"), { target: { value: "2,000,000" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the budget" }));
    await waitFor(() => expect(seen.some((x) => x.path === `/v1/ai/budgets/tokens:person:${person}`)).toBe(true));
    expect(seen.find((x) => x.path === `/v1/ai/budgets/tokens:person:${person}`)?.body).toEqual({ amount: "2000000" });
  });
});
