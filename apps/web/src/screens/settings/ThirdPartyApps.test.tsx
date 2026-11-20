import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ThirdPartyApps } from "./ThirdPartyApps";

type App = components["schemas"]["ThirdPartyApp"];

const acme: App = {
  client_id: "mcp-0123456789abcdef0123",
  name: "Acme Books Assistant",
  publisher: "Acme Software Pte Ltd",
  redirect_uris: ["https://acme.example/oauth/callback"],
  purpose: "Ask questions about the management accounts",
  processing_location: "United States",
  outside_singapore: true,
  inventory_entry: "0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c31",
  status: "pending",
  registered_by: "0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e",
  registered_at: "2026-11-21T09:30:00Z",
};
const admin = { ...ownerSession, roles: ["administrator"] };

// req: AI-46
// req: AI-90
describe("Apps from other publishers on Connected apps", () => {
  it("lists what an Administrator registered, saying Fyniq has not verified it and where it processes, and axe finds nothing", async () => {
    answer({ "GET /v1/connected-apps/third-party-apps": { status: 200, body: { apps: [acme] } } });
    const { container } = renderPage(ThirdPartyApps, { path: "/settings/connected-apps", session: admin });
    const list = within(await screen.findByRole("list", { name: "Third-party apps" }));
    expect(list.getByText("Acme Books Assistant")).toBeTruthy();
    expect(list.getByText(/processing in United States \(outside Singapore: a transfer to assess under the PDPA\)/)).toBeTruthy();
    expect(list.getByText("Waiting for an Owner")).toBeTruthy();
    expect(screen.getByText(/Fyniq has not verified any of them/)).toBeTruthy();
    await noViolations(container);
  });

  it("registers an app by hand with its sign-in addresses, then removes one", async () => {
    const seen = answer({
      "GET /v1/connected-apps/third-party-apps": { status: 200, body: { apps: [acme] } },
      "POST /v1/connected-apps/third-party-apps": { status: 201, body: { ...acme, client_id: "mcp-aaaaaaaaaaaaaaaaaaaa", name: "Beta" } },
      "DELETE /v1/connected-apps/third-party-apps/mcp-0123456789abcdef0123": { status: 200, body: { ...acme, status: "removed" } },
    });
    renderPage(ThirdPartyApps, { path: "/settings/connected-apps", session: admin });
    fireEvent.click(await screen.findByRole("button", { name: "Register an app" }));
    const dialog = await screen.findByRole("dialog");
    const register = within(dialog).getByRole("button", { name: "Register an app" }) as HTMLButtonElement;
    expect(register.disabled).toBe(true);
    fireEvent.change(within(dialog).getByLabelText("App name"), { target: { value: "Beta" } });
    fireEvent.change(within(dialog).getByLabelText("Publisher"), { target: { value: "Beta Ltd" } });
    fireEvent.change(within(dialog).getByLabelText("Sign-in addresses"), { target: { value: "https://beta.example/cb, http://127.0.0.1:8123/cb" } });
    fireEvent.change(within(dialog).getByLabelText("What the organisation uses it for"), { target: { value: "Ask about the books" } });
    fireEvent.change(within(dialog).getByLabelText("Where the publisher processes what it reads"), { target: { value: "Singapore" } });
    expect(register.disabled).toBe(false);
    fireEvent.click(register);
    await waitFor(() => {
      const post = seen.find((s) => s.method === "POST" && s.path === "/v1/connected-apps/third-party-apps");
      expect(post?.body).toEqual({ name: "Beta", publisher: "Beta Ltd", redirect_uris: ["https://beta.example/cb", "http://127.0.0.1:8123/cb"], purpose: "Ask about the books", processing_location: "Singapore", outside_singapore: false });
    });
    fireEvent.click(await screen.findByRole("button", { name: "Remove Acme Books Assistant" }));
    await waitFor(() => expect(seen.some((s) => s.method === "DELETE" && s.path === "/v1/connected-apps/third-party-apps/mcp-0123456789abcdef0123")).toBe(true));
  });

  it("shows a refusal in the form, and offers a Controller the list without the buttons", async () => {
    answer({
      "GET /v1/connected-apps/third-party-apps": { status: 200, body: { apps: [] } },
      "POST /v1/connected-apps/third-party-apps": problem(409, "MCP-REG-05"),
    });
    const view = renderPage(ThirdPartyApps, { path: "/settings/connected-apps", session: admin });
    fireEvent.click(await screen.findByRole("button", { name: "Register an app" }));
    const dialog = await screen.findByRole("dialog");
    for (const [label, value] of [["App name", "Beta"], ["Publisher", "Beta Ltd"], ["Sign-in addresses", "https://beta.example/cb"], ["What the organisation uses it for", "x"], ["Where the publisher processes what it reads", "Singapore"]]) {
      fireEvent.change(within(dialog).getByLabelText(label!), { target: { value } });
    }
    fireEvent.click(within(dialog).getByRole("button", { name: "Register an app" }));
    expect(await within(dialog).findByText("The app was not registered")).toBeTruthy();
    view.unmount();
    answer({ "GET /v1/connected-apps/third-party-apps": { status: 200, body: { apps: [acme] } } });
    renderPage(ThirdPartyApps, { path: "/settings/connected-apps", session: { ...ownerSession, roles: ["controller"] } });
    expect(await screen.findByText("Acme Books Assistant")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Register an app|Remove/ })).toBeNull();
  });

  it("shows nothing when there are none and the person may not register", async () => {
    answer({ "GET /v1/connected-apps/third-party-apps": { status: 200, body: { apps: [] } } });
    const { container } = renderPage(ThirdPartyApps, { path: "/settings/connected-apps", session: { ...ownerSession, roles: ["controller"] } });
    await waitFor(() => expect(container.querySelector("section")).toBeNull());
  });
});
