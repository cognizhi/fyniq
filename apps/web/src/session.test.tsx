import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SignedIn } from "./session";
import { answer, ownerSession } from "./test/api";
import { noViolations } from "./test/axe";
import { authenticator } from "./test/webauthn";

function renderLayout(path = "/settings/profile") {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={queries}>
        <Outlet />
      </QueryClientProvider>
    ),
  });
  const layout = createRoute({ getParentRoute: () => root, id: "signed-in", component: SignedIn });
  const page = createRoute({ getParentRoute: () => layout, path, component: () => <p>The page</p> });
  const router = createRouter({ routeTree: root.addChildren([layout.addChildren([page])]), history: createMemoryHistory({ initialEntries: [path] }) });
  return render(<RouterProvider router={router} />);
}

const organisation = { id: ownerSession.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };

describe("the signed-in layout", () => {
  // req: FR-UX-40
  it("shows the page in the frame, titled by its route", async () => {
    answer({ "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: ownerSession }, "GET /v1/organisation": { status: 200, body: organisation } });
    renderLayout();
    expect(await screen.findByText("The page")).toBeTruthy();
    const header = screen.getByRole("banner");
    expect(within(header).getByText("Profile and security")).toBeTruthy();
    expect(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Settings" }).getAttribute("aria-current")).toBe("page");
    await noViolations(document.body);
  });

  // req: SEC-01
  it("shows a session without a second factor nothing but the way to confirm it's you", async () => {
    authenticator();
    const { step_up_method: _, stepped_up_until: __, ...single } = ownerSession;
    answer({ "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: single } });
    renderLayout();
    expect(await screen.findByRole("heading", { level: 1, name: "Confirm it's you" })).toBeTruthy();
    expect(screen.queryByText("The page")).toBeNull();
    expect(screen.getByRole("button", { name: "Use a passkey" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sign in again" })).toBeTruthy();
  });

  // req: FR-UX-26
  // req: FR-UX-24
  it("shows an Expert page in Basic mode too, saying so and offering to switch", async () => {
    answer({ "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: ownerSession }, "GET /v1/organisation": { status: 200, body: organisation } });
    renderLayout("/settings/members");
    expect(await screen.findByText("The page")).toBeTruthy();
    expect(screen.getByText(/This page is part of Expert mode/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Switch to Expert" }));
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
    expect(await screen.findByRole("radio", { name: "Expert", checked: true })).toBeTruthy();
    expect(screen.queryByText(/This page is part of Expert mode/)).toBeNull();
  });
});
