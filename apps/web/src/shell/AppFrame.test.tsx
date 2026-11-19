import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { startAppearance } from "../appearance";
import { noViolations } from "../test/axe";
import type { Mode } from "../registry";
import { AskAboutThis } from "../components/ai/AskAboutThis";
import { forgetMode } from "../mode";
import { answer } from "../test/api";
import { AppFrame } from "./AppFrame";

const owner: Pick<Session, "roles" | "features"> = {
  roles: ["owner"],
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};

function renderFrame(session = owner, mode: Mode = "basic", page: React.ReactNode = <h1>Home</h1>) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute();
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => (
      <QueryClientProvider client={queries}>
        <AppFrame session={session} title="Home" mode={mode}>
          {page}
        </AppFrame>
      </QueryClientProvider>
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

  // In compact, the top bar keeps the title, search and the assistant; More holds the appearance, help and signing out,
  // for every person, so the top bar fits the folded foldable's 344 px (MASTER.md §5.1).
  // req: FR-UX-37
  it("puts appearance, help and signing out in More, for every person", async () => {
    renderFrame({ roles: [], features: undefined });
    await screen.findByRole("banner");
    fireEvent.click(screen.getByRole("button", { name: "More" }));
    const more = await screen.findByRole("dialog", { name: "More" });
    // The account the compact header has no room for (FR-UX-37): the appearance choices, About and signing out.
    expect(within(more).getByRole("heading", { name: "Appearance" })).toBeTruthy();
    for (const name of ["About Fyniq", "Sign out"]) expect(within(more).getByRole("button", { name })).toBeTruthy();
    await noViolations(more);
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
  // req: FR-UX-17
  it("opens the assistant from its button or with Ctrl+J, docked beside the page, and gives focus back when it closes", async () => {
    renderFrame();
    const button = await screen.findByRole("button", { name: "Assistant" });
    expect(button.getAttribute("aria-keyshortcuts")).toBe("Control+J Meta+J");
    fireEvent.click(button);
    const panel = await screen.findByRole("complementary", { name: "Assistant" });
    expect(within(panel).getByText("The assistant is switched off")).toBeTruthy(); // this session's AI switch is off
    expect(document.querySelector(".fy-app")?.getAttribute("data-ai")).toBe("open");
    act(() => {
      fireEvent.keyDown(window, { key: "j", ctrlKey: true });
    });
    await waitFor(() => expect(screen.queryByRole("complementary", { name: "Assistant" })).toBeNull());
    act(() => {
      fireEvent.keyDown(window, { key: "j", metaKey: true });
    });
    fireEvent.click(await screen.findByRole("button", { name: "Close the assistant" }));
    await waitFor(() => expect(screen.queryByRole("complementary", { name: "Assistant" })).toBeNull());
    expect(document.activeElement).toBe(button);
    expect(document.querySelector(".fy-app")?.getAttribute("data-ai")).toBeNull();
  });

  // req: FR-UX-17
  it("opens the assistant from Ask about this with the record as its context, and hides it while the AI is off", async () => {
    const ask = <AskAboutThis context={{ kind: "invoice", id: "i-1", label: "INV-0136 · Kopi Labs" }} />;
    renderFrame({ ...owner, features: { ...owner.features!, ai_assistant: true } }, "basic", ask);
    fireEvent.click(await screen.findByRole("button", { name: "Ask about this" }));
    const panel = await screen.findByRole("complementary", { name: "Assistant" });
    expect(within(panel).getByText("INV-0136 · Kopi Labs")).toBeTruthy();
    expect((within(panel).getByLabelText("Ask the assistant") as HTMLTextAreaElement).placeholder).toBe("Ask about INV-0136 · Kopi Labs…");
    fireEvent.click(within(panel).getByRole("button", { name: "Remove the context" }));
    expect(within(panel).queryByText("INV-0136 · Kopi Labs")).toBeNull();
  });

  it("shows no Ask about this while the AI is switched off", async () => {
    renderFrame(owner, "basic", <AskAboutThis context={{ kind: "invoice", id: "i-1", label: "INV-0136" }} />);
    await screen.findByRole("banner");
    expect(screen.queryByRole("button", { name: "Ask about this" })).toBeNull();
  });

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

  // req: FR-UX-01
  // req: FR-UX-02
  it("offers to search the records for what is typed in the command palette", async () => {
    renderFrame();
    await screen.findByRole("banner");
    act(() => {
      fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    });
    const input = await screen.findByRole("combobox");
    fireEvent.change(input, { target: { value: "INV-0138" } });
    expect(await screen.findByRole("option", { name: /Search the records for “INV-0138”/ })).toBeTruthy();
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

  // req: FR-UX-23
  // req: experience/modes Basic and Expert keyboard
  it("switches the mode from the command palette and with Alt+Shift+M, offering the other mode each time", async () => {
    forgetMode();
    const first = renderFrame(owner, "basic");
    await screen.findByRole("banner");
    act(() => {
      fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    });
    const input = await screen.findByRole("combobox");
    fireEvent.change(input, { target: { value: "Switch to" } });
    expect(await screen.findByRole("option", { name: /Switch to Expert mode/ })).toBeTruthy();
    expect(screen.queryByRole("option", { name: /Switch to Basic mode/ })).toBeNull();
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(window.localStorage.getItem("fyniq.mode")).toBe("expert"));
    // The chord works in either mode, and not as a single character: the frame now shows Expert.
    first.unmount();
    renderFrame(owner, "expert");
    await screen.findByRole("banner");
    act(() => {
      fireEvent.keyDown(window, { key: "Ä", code: "KeyM", altKey: true, shiftKey: true });
    });
    await waitFor(() => expect(window.localStorage.getItem("fyniq.mode")).toBe("basic"));
    act(() => {
      fireEvent.keyDown(window, { key: "m", code: "KeyM" });
    });
    expect(window.localStorage.getItem("fyniq.mode")).toBe("basic");
  });

  // req: FR-UX-21
  it("shows each badge in words, and announces a change once, as a phrase, through one polite status region", async () => {
    const approvals = (n: number) => ({ status: 200, body: { items: Array.from({ length: n }, (_, i) => ({ id: `a${i}`, you_may_approve: true })) } });
    let waiting = 2;
    answer({
      "GET /v1/approvals": () => approvals(waiting),
      "GET /v1/invoices": { status: 200, body: { items: [{ id: "i1", states: { due: "overdue" } }, { id: "i2", states: { due: "none" } }] } },
      "GET /v1/bill-reminders": { status: 200, body: { items: [{ id: "b1" }, { id: "b2" }, { id: "b3" }] } },
      "GET /v1/bank-lines": { status: 403, body: {} },
      "GET /v1/compliance/calendar": { status: 200, body: { deadlines: [{ key: "gst", status: "overdue" }, { key: "cpf", status: "due_soon" }, { key: "eci", status: "open" }] } },
    });
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const root = createRootRoute();
    const home = createRoute({
      getParentRoute: () => root,
      path: "/",
      component: () => (
        <QueryClientProvider client={queries}>
          <AppFrame session={{ ...owner, user_id: "u-1", roles: ["owner", "accountant"] } as never} title="Home" mode="expert">
            <h1>Home</h1>
          </AppFrame>
        </QueryClientProvider>
      ),
    });
    render(<RouterProvider router={createRouter({ routeTree: root.addChildren([home]), history: createMemoryHistory({ initialEntries: ["/"] }) })} />);
    const nav = await screen.findByRole("navigation", { name: "Main" });
    // Said in words for a screen reader; the digits are for the eye alone.
    expect((await within(nav).findByRole("link", { name: /Inbox, 2 approvals waiting/ })).querySelector(".fy-badge")?.getAttribute("aria-hidden")).toBe("true");
    expect(within(nav).getByRole("link", { name: /Sales, 1 invoice overdue/ })).toBeTruthy();
    expect(within(nav).getByRole("link", { name: /Purchases, 3 bills due/ })).toBeTruthy();
    expect(within(nav).getByRole("link", { name: /Compliance, 2 deadlines due/ })).toBeTruthy();
    expect(within(nav).getByRole("link", { name: "Banking" }), "a read the roles refuse gives no badge").toBeTruthy();
    // One polite, atomic region for the shell's counts; the page's own state is not announced.
    const regions = document.querySelectorAll('[role="status"][aria-live="polite"][aria-atomic="true"]');
    expect(regions).toHaveLength(1);
    expect(regions[0]?.textContent).toBe("");
    // A change is announced as a phrase, and focus does not move.
    const before = document.activeElement;
    waiting = 3;
    await act(async () => {
      await queries.invalidateQueries({ queryKey: ["nav-badge"] });
    });
    await waitFor(() => expect(regions[0]?.textContent).toBe("3 approvals waiting"));
    expect(document.activeElement).toBe(before);
    waiting = 0;
    await act(async () => {
      await queries.invalidateQueries({ queryKey: ["nav-badge"] });
    });
    await waitFor(() => expect(regions[0]?.textContent).toBe("No approvals waiting"));
    expect(within(nav).getByRole("link", { name: "Inbox" })).toBeTruthy(); // a count of none is no badge
  });

  // req: experience/navigation AI assistant side panel
  it("keeps the assistant out of the navigation: it is a panel any page opens, not an item", async () => {
    renderFrame({ ...owner, features: { ...owner.features!, ai_assistant: true } } as typeof owner, "expert", <h1>Home</h1>);
    const nav = await screen.findByRole("navigation", { name: "Main" });
    expect(within(nav).queryByText(/assistant|Ask AI|AI assistant/i)).toBeNull();
    expect(screen.getByRole("button", { name: /assistant/i })).toBeTruthy(); // in the header, on every page
  });
});
