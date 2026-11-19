import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { SavedDialog } from "./SavedMenu";

const view = { id: "v1", kind: "view", title: "Overdue", path: "/sales/invoices", query: "?status=overdue", saved_at: "2026-11-20T09:00:00Z" };
const fav = { id: "/banking", kind: "favourite", title: "Banking", path: "/banking", saved_at: "2026-11-19T09:00:00Z" };

// req: FR-UX-12
describe("favourites and saved views", () => {
  it("lists what the person saved, pins the page they are on, saves it with its filters and takes one away; axe finds nothing", async () => {
    let items = [fav, view];
    const seen = answer({
      "GET /v1/saved-items": () => ({ status: 200, body: { items } }),
      "POST /v1/saved-items": (body) => {
        const b = body as { kind: string; title: string; path: string; query?: string };
        items = [...items, { id: b.kind === "view" ? "v2" : b.path, kind: b.kind, title: b.title, path: b.path, query: b.query, saved_at: "2026-11-20T10:00:00Z" } as never];
        return { status: 200, body: items.at(-1) };
      },
      "DELETE /v1/saved-items/view": () => {
        items = items.filter((i) => i.id !== "v1");
        return { status: 200, body: view };
      },
    });
    renderPage(() => <SavedDialog open onOpenChange={() => undefined} />, { path: "/sales/invoices" });
    const dialog = await screen.findByRole("dialog", { name: "Favourites and views" });
    const favourites = await within(dialog).findByRole("list", { name: "Favourites" });
    expect(within(favourites).getByText("Banking")).toBeTruthy();
    const views = within(dialog).getByRole("list", { name: "Views" });
    expect(within(views).getByText("/sales/invoices?status=overdue")).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(within(dialog).getByRole("button", { name: "Add this page to favourites" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && (s.body as { kind: string }).kind === "favourite")).toBe(true));
    expect(await within(dialog).findByRole("button", { name: "Remove this page from favourites" })).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.change(within(dialog).getByLabelText(/Save this page/), { target: { value: "  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the view" }));
    expect(await within(dialog).findByText("Give the view a name first.")).toBeTruthy();
    fireEvent.change(within(dialog).getByLabelText(/Save this page/), { target: { value: "My invoices" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the view" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && (s.body as { kind: string; title: string }).title === "My invoices")).toBe(true));

    fireEvent.click(within(dialog).getByRole("button", { name: "Take Overdue away" }));
    await waitFor(() => expect(seen.some((s) => s.method === "DELETE" && s.query.includes("id=v1"))).toBe(true));
    await waitFor(() => expect(within(dialog).queryByText("Overdue")).toBeNull());
  });
});
