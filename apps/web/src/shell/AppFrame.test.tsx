import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { startAppearance } from "../appearance";
import { noViolations } from "../test/axe";
import type { Mode } from "../registry";
import { AppFrame } from "./AppFrame";

const owner: Pick<Session, "roles" | "features"> = {
  roles: ["owner"],
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};

function renderFrame(session = owner, mode: Mode = "basic") {
  const root = createRootRoute();
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => (
      <AppFrame session={session} title="Home" mode={mode}>
        <h1>Home</h1>
      </AppFrame>
    ),
  });
  const router = createRouter({ routeTree: root.addChildren([home]), history: createMemoryHistory({ initialEntries: ["/"] }) });
  return render(<RouterProvider router={router} />);
}

const html = () => document.documentElement;

describe("the app frame", () => {
  // req: NFR-A11Y-01
  // req: experience/visual-design Icons
  it("names every control, marks the current page, and axe finds nothing", async () => {
    renderFrame();
    await screen.findByRole("banner");
    for (const name of ["Search and commands", "Appearance", "Sign out", "Skip to content"]) expect(screen.getAllByRole(name === "Skip to content" ? "link" : "button", { name: new RegExp(name) }).length).toBeGreaterThan(0);
    const side = screen.getByRole("navigation", { name: "Main" });
    expect(within(side).getByRole("link", { name: "Home" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("main").id).toBe("main");
    await noViolations(document.body);
  });

  // req: FR-UX-40
  it("shows a person with no role only what every signed-in person may see", async () => {
    renderFrame({ roles: [], features: undefined });
    const side = await screen.findByRole("navigation", { name: "Main" });
    expect(within(side).getAllByRole("link").map((a) => a.getAttribute("aria-label") ?? a.textContent)).toEqual(["Fyniq home", "Home", "Inbox", "Settings"]);
  });

  // req: experience/visual-design Themes
  it("switches the theme from the header's appearance menu", async () => {
    startAppearance();
    renderFrame();
    fireEvent.click(await screen.findByRole("button", { name: "Appearance" }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: /Dark/ }));
    expect(html().classList.contains("dark")).toBe(true);
    fireEvent.click(screen.getByRole("menuitemradio", { name: /High contrast/ }));
    expect(html().dataset.contrast).toBe("more");
    await noViolations(document.body);
  });

  // req: experience/visual-design Themes
  // req: FR-UX-01
  it("opens the command palette with Ctrl+K and switches the theme from it", async () => {
    startAppearance();
    renderFrame();
    await screen.findByRole("banner");
    act(() => {
      fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    });
    const input = await screen.findByRole("combobox");
    fireEvent.change(input, { target: { value: "dark theme" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(html().classList.contains("dark")).toBe(true));
  });

  // req: FR-UX-23
  // req: FR-UX-24
  it("shows the mode in the header and switches it, by click or arrow key", async () => {
    renderFrame(owner, "basic");
    const group = await screen.findByRole("radiogroup", { name: "Experience mode" });
    const basic = within(group).getByRole("radio", { name: "Basic" });
    expect(basic.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(within(group).getByRole("radio", { name: "Expert" }));
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
    fireEvent.keyDown(basic, { key: "ArrowRight" });
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
    await noViolations(document.body);
  });
});
