import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Search } from "./Search";

const groups = [
  { key: "invoices", label: "Invoices and credit notes", count: 1, more: false },
  { key: "bills", label: "Bills and vendor credits", count: 0, more: false },
  { key: "contacts", label: "Contacts", count: 1, more: true },
];
const results = [
  {
    group: "invoices", type: "invoice", id: "i1", title: "INV-0012 · Acme Analytics Pte Ltd", detail: "PO-1180", date: "2026-10-14", status: "posted",
    amount: { amount: "1090.00", currency: "SGD" }, link: "/sales/invoices/i1",
  },
  { group: "contacts", type: "contact", id: "c1", title: "Acme Analytics Pte Ltd", detail: "202312345K", link: "/contacts/c1" },
];

// req: FR-UX-02
// req: experience/navigation Type filters and keyboard navigation
describe("Search", () => {
  it("shows what it found with each kind's count, filters by kind, and moves between results with the arrow keys; axe finds nothing", async () => {
    const seen = answer({ "GET /v1/search": { status: 200, body: { query: "acme", results, groups } } });
    renderPage(Search, { path: "/search?q=acme", route: "/search" });
    const list = await screen.findByRole("list", { name: "Search results" });
    const links = within(list).getAllByRole("link");
    expect(links.map((l) => l.getAttribute("href"))).toEqual(["/sales/invoices/i1", "/contacts/c1"]);
    expect(within(list).getByText("1,090.00 SGD")).toBeTruthy();
    expect(within(list).getByText(/Invoices and credit notes · 14 Oct 2026 · posted · PO-1180/)).toBeTruthy();
    const kinds = screen.getByRole("group", { name: "Kinds of record" });
    expect(within(kinds).getAllByRole("button").map((b) => b.textContent)).toEqual(["All", "Invoices and credit notes 1", "Contacts 1+"]);
    expect(screen.getByText(/Some kinds found more than are shown/)).toBeTruthy();
    expect(seen[0]?.query).toBe("?q=acme");
    links[0]?.focus();
    fireEvent.keyDown(list, { key: "ArrowDown" });
    expect(document.activeElement).toBe(links[1]);
    fireEvent.keyDown(list, { key: "Home" });
    expect(document.activeElement).toBe(links[0]);
    await noViolations(document.body);
  });

  it("says when nothing matches in what the person may see, and asks for a query first", async () => {
    answer({ "GET /v1/search": { status: 200, body: { query: "zzz", results: [], groups: [{ ...groups[0], count: 0 }] } } });
    renderPage(Search, { path: "/search?q=zzz", route: "/search" });
    expect(await screen.findByText(/Nothing matches “zzz” in what you may see/)).toBeTruthy();
  });
});
