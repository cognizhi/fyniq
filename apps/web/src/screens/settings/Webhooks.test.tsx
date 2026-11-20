import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Webhooks } from "./Webhooks";

const endpoint = (over = {}) => ({
  id: "e1",
  url: "https://hooks.example.com/fyniq",
  label: "Accounting sync",
  event_types: ["fyniq.ledger.posted.v1"],
  state: "pending",
  secret_hint: "k3Xo",
  failure_run: 0,
  version: 1,
  created_at: "2026-11-21T09:00:00Z",
  ...over,
});

// req: FR-INT-12
describe("Webhooks", () => {
  it("lists endpoints with their state, verifies a pending one over its version, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/webhook-endpoints": { status: 200, body: { items: [endpoint(), endpoint({ id: "e2", label: "Paused one", state: "paused", failure_run: 10, paused_reason: "Ten deliveries failed in a row", version: 4 })] } },
      "POST /v1/webhook-endpoints/e1/verify": { status: 200, body: endpoint({ state: "active", version: 2 }) },
    });
    renderPage(Webhooks, { path: "/settings/integrations/webhooks" });
    expect(await screen.findByText("Accounting sync")).toBeTruthy();
    expect(screen.getByText("Not verified")).toBeTruthy();
    expect(screen.getByText("Paused")).toBeTruthy();
    expect(screen.getByText(/Ten deliveries failed in a row/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Verify: Accounting sync" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/webhook-endpoints/e1/verify"));
    expect(seen.find((s) => s.method === "POST")?.headers.get("If-Match")).toBe('"1"');
  });

  it("adds an endpoint and shows its secret once, and replays a dead letter", async () => {
    const seen = answer({
      "GET /v1/webhook-endpoints": { status: 200, body: { items: [endpoint({ state: "active" })] } },
      "POST /v1/webhook-endpoints": { status: 201, body: { ...endpoint(), secret: "whsec_abc123" } },
      "GET /v1/webhook-endpoints/e1/deliveries": {
        status: 200,
        body: { items: [{ id: "d1", endpoint_id: "e1", event_type: "fyniq.period.closed.v1", message_id: "msg_1", state: "dead", attempts: 12, replays: 0, last_status: 503, created_at: "2026-11-18T09:00:00Z" }] },
      },
      "POST /v1/webhook-deliveries/d1/replay": { status: 200, body: { id: "d1", state: "pending" } },
    });
    renderPage(Webhooks, { path: "/settings/integrations/webhooks" });
    fireEvent.click(await screen.findByRole("button", { name: "Add endpoint" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "ERP" } });
    fireEvent.change(within(dialog).getByLabelText("Address"), { target: { value: "https://erp.example.com/hook" } });
    fireEvent.click(within(dialog).getByLabelText("fyniq.ledger.posted.v1"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add endpoint" }));
    expect(await screen.findByText("whsec_abc123")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.body).toEqual({ label: "ERP", url: "https://erp.example.com/hook", event_types: ["fyniq.ledger.posted.v1"] });
    fireEvent.click(screen.getByRole("button", { name: "I have copied it" }));
    fireEvent.click(await screen.findByRole("button", { name: "Dead letters: Accounting sync" }));
    fireEvent.click(await screen.findByRole("button", { name: /^Replay fyniq.period.closed.v1/ }));
    await waitFor(() => expect(calls()).toContain("POST /v1/webhook-deliveries/d1/replay"));
  });

  it("says in plain words why the API refused", async () => {
    answer({
      "GET /v1/webhook-endpoints": { status: 200, body: { items: [] } },
      "POST /v1/webhook-endpoints": { status: 403, body: { type: "urn:x", title: "Webhooks are off", status: 403, code: "WEBHOOK-01", detail: "Webhooks are off for this organisation.", fix: "Ask an Owner to turn on Webhooks and events." } },
    });
    renderPage(Webhooks, { path: "/settings/integrations/webhooks" });
    expect(await screen.findByText(/No endpoint yet/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add endpoint" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "ERP" } });
    fireEvent.change(within(dialog).getByLabelText("Address"), { target: { value: "https://erp.example.com/hook" } });
    fireEvent.click(within(dialog).getByLabelText("fyniq.ledger.posted.v1"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add endpoint" }));
    expect(await screen.findByText(/Webhooks are off for this organisation/)).toBeTruthy();
  });
});
