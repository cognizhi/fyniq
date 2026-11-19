import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../api/client";
import type { Mode } from "../registry";
import { routes } from "../registry";
import { createKeys, defaultSettings, matcher, normalise, problems, setShortcutSettings, shortcutSettings, usual, wait, type Shortcut } from "../shortcuts";
import { noViolations } from "../test/axe";
import { onTourStart } from "../tour/state";
import { AppFrame } from "./AppFrame";
import { useBulkActions } from "./bulk";

// Keyboard-first entry (FR-UX-06; experience/navigation "Keyboard-first entry"; docs/design/screens Shortcuts).

const owner: Pick<Session, "roles" | "features"> & { user_id: string } = {
  roles: ["owner"],
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
  user_id: "u-1",
};

function renderFrame(mode: Mode = "expert", page: ReactNode = <h1>Home page</h1>, session = owner) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({
    component: () => (
      <QueryClientProvider client={queries}>
        <AppFrame session={session} title="Home" mode={mode}>
          <Outlet />
        </AppFrame>
      </QueryClientProvider>
    ),
  });
  const at = (path: string, body: ReactNode) => createRoute({ getParentRoute: () => root, path, component: () => body });
  const tree = root.addChildren([at("/", page), at("/banking", <h1>Banking page</h1>), at("/sales/invoices/new", <h1>New invoice page</h1>)]);
  const router = createRouter({ routeTree: tree, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(<RouterProvider router={router} />);
  return router;
}

// press types keys one after the other where focus is (the page, when nothing has it).
function press(...keys: string[]) {
  for (const key of keys) {
    act(() => {
      fireEvent.keyDown(document.activeElement ?? document.body, { key });
    });
  }
}

beforeEach(() => {
  setShortcutSettings("u-1", defaultSettings);
  setShortcutSettings("u-2", defaultSettings);
});

// req: FR-UX-06
// req: experience/navigation FR-UX-06 Keyboard-first entry
describe("keyboard-first entry", () => {
  it("starts a new invoice with C I and goes to Banking with G B", async () => {
    const router = renderFrame();
    await screen.findByRole("heading", { name: "Home page" });
    press("c", "i");
    expect(await screen.findByRole("heading", { name: "New invoice page" })).toBeTruthy();
    press("g", "b");
    expect(await screen.findByRole("heading", { name: "Banking page" })).toBeTruthy();
    // A key that starts no shortcut after G starts again from itself; a second key long after the first is a new one.
    press("g", "x", "g", "b");
    expect(router.state.location.pathname).toBe("/banking");
  });

  it("has keys for every page that creates a record, and none of the usual keys clash", () => {
    const creates = routes.filter((r) => /\/new(\/|$)/.test(r.path)).map((r) => r.id);
    expect(creates.filter((id) => !createKeys.some(([k]) => k === id))).toEqual([]);
    expect(problems({ ...usual })).toEqual({});
    expect(new Set(Object.values(usual)).size).toBe(Object.keys(usual).length);
  });

  it("waits for a second key only so long, and reads keys as people type them", () => {
    const list: Shortcut[] = [{ id: "nav.banking", keys: "gb", label: "Banking", to: "/banking", group: "go" }];
    const step = matcher();
    expect(step("g", 0, list)).toBe("wait");
    expect(step("b", wait + 1, list)).toBeNull();
    expect(step("g", wait + 2, list)).toBe("wait");
    expect(step("b", wait + 3, list)).toMatchObject({ id: "nav.banking" });
    for (const typed of ["G B", "g then b", "gb", "G, B", "G+B"]) expect(normalise(typed)).toBe("gb");
  });

  // req: experience/navigation FR-UX-06 Keyboard-first entry: Enter moves to the next field
  it("moves to the next field with Enter in an entry form, and saves it with Ctrl+Enter", async () => {
    const saved = vi.fn((e: { preventDefault: () => void }) => e.preventDefault());
    renderFrame(
      "basic",
      <>
        <form data-enter="next" onSubmit={saved} aria-label="Entry">
          <input aria-label="Customer" />
          <input type="checkbox" aria-label="Recurring" />
          <select aria-label="Currency">
            <option>SGD</option>
          </select>
          <input aria-label="Reference" disabled />
          <textarea aria-label="Notes" />
          <button type="submit">Save</button>
        </form>
        <form aria-label="Other">
          <input aria-label="Name" />
          <input aria-label="Email" />
        </form>
      </>,
    );
    const customer = await screen.findByLabelText("Customer");
    customer.focus();
    press("Enter");
    expect(document.activeElement).toBe(screen.getByLabelText("Currency"));
    press("Enter");
    expect(document.activeElement).toBe(screen.getByLabelText("Notes"));
    customer.focus();
    act(() => {
      fireEvent.keyDown(customer, { key: "Enter", ctrlKey: true });
    });
    expect(saved).toHaveBeenCalledOnce();
    // A form that does not ask for it keeps Enter's own meaning.
    screen.getByLabelText("Name").focus();
    press("Enter");
    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
    // The entry pages ask for it.
    const pages = ["sales/InvoicePage", "sales/QuotePage", "sales/RecurringInvoicePage", "purchases/BillPage", "purchases/PurchaseOrderPage", "purchases/RecurringBillPage", "accounting/JournalPage", "accounting/RecurringJournalPage", "claims/ClaimEditor", "claims/MileageClaim"];
    for (const p of pages) expect(readFileSync(resolve(__dirname, `../screens/${p}.tsx`), "utf8"), p).toContain('data-enter="next"');
  });

  // req: experience/navigation FR-UX-06 Keyboard-first entry: Bulk actions and shortcut sheet
  it("offers a page's actions on its selected rows in the command palette, and shows the shortcut sheet on ?", async () => {
    const accept = vi.fn();
    function Selected() {
      useBulkActions([{ label: "Accept the selected suggestions (2)", run: accept }]);
      return <h1>Home page</h1>;
    }
    renderFrame("expert", <Selected />);
    await screen.findByRole("heading", { name: "Home page" });
    act(() => {
      fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    });
    const option = await screen.findByRole("option", { name: "Accept the selected suggestions (2)" });
    fireEvent.click(option);
    expect(accept).toHaveBeenCalledOnce();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    press("?");
    const sheet = await screen.findByRole("dialog", { name: "Keyboard shortcuts" });
    const rows: [string, string][] = [
      ["Banking", "GthenB"],
      ["New invoice", "CthenI"],
      ["The next field", "Enter"],
      ["Next request", "J or ↓"],
    ];
    for (const [what, keys] of rows) {
      const term = within(sheet).getByText(what, { selector: "dt" });
      expect(term.nextElementSibling?.textContent).toBe(keys);
    }
    await noViolations(document.body);
  });

  // req: FR-UX-34
  it("starts the first-run tour again from the command palette", async () => {
    const started = vi.fn();
    const off = onTourStart(started);
    renderFrame("basic", <h1>Home page</h1>);
    await screen.findByRole("heading", { name: "Home page" });
    act(() => {
      fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    });
    fireEvent.click(await screen.findByRole("option", { name: "Take the tour again" }));
    expect(started).toHaveBeenCalledWith(0);
    off();
  });

  // req: experience/navigation FR-UX-06 Keyboard-first entry: Single-character shortcuts can be turned off and remapped
  // req: experience/modes Basic and Expert keyboard
  it("turns single-character shortcuts off and changes their keys for that person only", async () => {
    setShortcutSettings("u-1", { on: false, keys: {} });
    const router = renderFrame();
    await screen.findByRole("heading", { name: "Home page" });
    press("g", "b", "?");
    expect(router.state.location.pathname).toBe("/");
    expect(screen.queryByRole("dialog")).toBeNull();
    act(() => setShortcutSettings("u-1", { on: true, keys: { "nav.banking": "gk" } }));
    press("g", "b");
    expect(router.state.location.pathname).toBe("/");
    press("g", "k");
    expect(await screen.findByRole("heading", { name: "Banking page" })).toBeTruthy();
    expect(shortcutSettings("u-2")).toEqual(defaultSettings);
    expect(JSON.parse(window.localStorage.getItem("fyniq.shortcuts.u-1") ?? "{}")).toEqual({ on: true, keys: { "nav.banking": "gk" } });
  });

  it("changes the keys in the sheet, says what clashes with the field it is in, and turns them off there", async () => {
    renderFrame();
    await screen.findByRole("heading", { name: "Home page" });
    press("?");
    const sheet = await screen.findByRole("dialog", { name: "Keyboard shortcuts" });
    fireEvent.click(within(sheet).getByRole("button", { name: "Change the keys" }));
    const banking = within(sheet).getByLabelText("Banking");
    expect((banking as HTMLInputElement).value).toBe("G B");
    fireEvent.change(banking, { target: { value: "G H" } });
    fireEvent.click(within(sheet).getByRole("button", { name: "Save the keys" }));
    const summary = await within(sheet).findByRole("alert");
    await waitFor(() => expect(summary.contains(document.activeElement)).toBe(true));
    expect(summary.textContent).toContain("G then H is already Home: choose other keys.");
    expect(banking.getAttribute("aria-invalid")).toBe("true");
    fireEvent.change(banking, { target: { value: "g then k" } });
    fireEvent.click(within(sheet).getByRole("button", { name: "Save the keys" }));
    expect(await within(sheet).findByText("Your keys are saved. They apply to you only.")).toBeTruthy();
    expect(shortcutSettings("u-1").keys).toEqual({ "nav.banking": "gk" });
    fireEvent.click(within(sheet).getByRole("switch", { name: "Single-character shortcuts" }));
    expect(shortcutSettings("u-1").on).toBe(false);
    expect(within(sheet).getByText(/Your single-character shortcuts are off/)).toBeTruthy();
  });

  // req: experience/navigation FR-UX-06 Keyboard-first entry: No shortcuts while typing
  it("fires no shortcut while focus is in a text field", async () => {
    const router = renderFrame("expert", <input aria-label="Memo" />);
    const memo = await screen.findByLabelText("Memo");
    memo.focus();
    press("g", "b", "?");
    expect(router.state.location.pathname).toBe("/");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  // req: experience/modes Basic and Expert keyboard
  // req: experience/modes Basic and Expert keyboard
  it("keeps ? only in Basic mode, with the way to Expert's keys", async () => {
    const router = renderFrame("basic");
    await screen.findByRole("heading", { name: "Home page" });
    press("g", "b", "c", "i");
    expect(router.state.location.pathname).toBe("/");
    press("?");
    const sheet = await screen.findByRole("dialog", { name: "Keyboard shortcuts" });
    expect(within(sheet).queryByText("Go to")).toBeNull();
    expect(within(sheet).getByRole("button", { name: "Switch to Expert" })).toBeTruthy();
  });
});
