import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { Help, HelpPage } from "../screens/help/Help";
import { ownerSession } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { forPerson, guideFor, headings, pageOf, pages, parse, search } from "./guide";
import { Markdown } from "./markdown";

// The user guide in the app (FR-UX-32): docs/user-guide/'s pages built in, filtered by role and mode, searchable,
// deep-linkable and printable; the Help menu opens the page of the screen the person is on (FR-UX-33).
// req: FR-UX-32
describe("the user guide", () => {
  it("builds every page of docs/user-guide/ in, each with its key and title, once", () => {
    expect(pages.length).toBeGreaterThan(60);
    expect(new Set(pages.map((p) => p.key)).size).toBe(pages.length);
    expect(pageOf("home.overview")?.title).toBe("The Home page and finding your way around");
    expect(pageOf("help.guide")?.mode).toBe("basic");
    expect(pages.every((p) => p.body.length > 0 && !p.body.startsWith("---"))).toBe(true);
  });

  it("reads a page's front matter: its roles by their keys, every role when it names none, and its mode", () => {
    const p = parse("x.md", "---\nkey: x.page\ntitle: \"A page\"\nmode: expert   # a note\nroles: [admin, accountant]\nstatus: draft\n---\n\n# A page\n\nWords.\n");
    expect(p).toMatchObject({ key: "x.page", title: "A page", mode: "expert", roles: ["administrator", "accountant"], draft: true });
    expect(parse("y.md", "---\nkey: y.page\ntitle: Y\n---\nBody")?.roles).toEqual(["all"]);
    expect(parse("z.md", "# No front matter")).toBeNull();
  });

  it("shows a person the pages for their roles, Basic ones in Basic mode with how many Expert adds, and finds by every word", () => {
    const all = [parse("a.md", "---\nkey: a\ntitle: Banking basics\nmode: basic\nroles: [all]\n---\nRecord a bank line"), parse("b.md", "---\nkey: b\ntitle: Journals\nmode: expert\nroles: [accountant]\n---\nPost a journal"), parse("c.md", "---\nkey: c\ntitle: Payroll\nmode: basic\nroles: [payroll_administrator]\n---\nPay runs")].filter((p) => p !== null);
    expect(forPerson(all, ["accountant"], "basic")).toEqual({ shown: [all[0]], hidden: 1 });
    expect(forPerson(all, ["accountant"], "expert").shown.map((p) => p.key)).toEqual(["a", "b"]);
    expect(forPerson(all, ["owner"], "expert").shown).toHaveLength(3);
    expect(search(all, "bank LINE").map((p) => p.key)).toEqual(["a"]);
    expect(search(all, "bank journal")).toEqual([]);
  });

  it("renders Markdown as elements: headings with anchors, nested lists, tables, and links inside the guide, out, or as words", async () => {
    const source =
      "# Title\n\n## Do it now\n\nOne **bold** and `code`; <script>alert(1)</script>\n\n- First\n  - Nested\n- Second, [Home](home.md#your-work)\n\n1. Then\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n[IRAS](https://www.iras.gov.sg) and [odd](javascript:alert(1))";
    renderPage(() => <Markdown source={source} />, { path: "/" });
    expect((await screen.findByRole("heading", { level: 2, name: "Do it now" })).id).toBe("do-it-now");
    expect(screen.queryByRole("heading", { level: 1 })).toBeNull();
    expect(screen.getByText("<script>alert(1)</script>", { exact: false })).toBeTruthy();
    expect(document.querySelector("script")).toBeNull();
    const lists = screen.getAllByRole("list");
    expect(within(lists[0]!).getByText("Nested").closest("ul")).not.toBe(lists[0]);
    expect(screen.getByRole("link", { name: "Home" }).getAttribute("href")).toBe("/help/home.overview#your-work");
    expect(screen.getByRole("link", { name: "IRAS" }).getAttribute("target")).toBe("_blank");
    expect(screen.queryByRole("link", { name: "odd" })).toBeNull();
    expect(screen.getByRole("cell", { name: "2" })).toBeTruthy();
    expect(headings("## One\n\ntext\n\n## Two words\n")).toEqual([
      { text: "One", anchor: "one" },
      { text: "Two words", anchor: "two-words" },
    ]);
  });

  // req: FR-UX-33
  it("opens the guide on the page of the screen the person is on", () => {
    expect(guideFor("/")).toEqual({ to: "/help/home.overview" });
    expect(guideFor("/sales/invoices/0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b")?.to).toMatch(/^\/help\/sales\./);
    expect(guideFor("/checks")?.to).toBe("/help/checks.overview");
  });

  it("lists the guide for a Basic person, says how many more Expert adds, and searches as they type; axe finds nothing", async () => {
    renderPage(Help, { path: "/help", session: { ...ownerSession, roles: ["bookkeeper"] } as Session, mode: "basic" });
    const list = await screen.findByRole("list", { name: "Pages of the guide" });
    expect(within(list).getByRole("link", { name: /What's new/ })).toBeTruthy();
    expect(screen.getByRole("status").textContent).toMatch(/Expert mode adds \d+ more/);
    fireEvent.change(screen.getByLabelText("Search the guide"), { target: { value: "user guide" } });
    expect(within(list).getByRole("link", { name: "Read the user guide" })).toBeTruthy();
    expect(screen.getByRole("status").textContent).toMatch(/page(s)? found/);
    await noViolations(document.body);
  });

  it("shows a page with its sections and a print button, and says when it has no such page", async () => {
    const page = renderPage(HelpPage, { path: "/help/help.guide", route: "/help/$key" });
    expect(await screen.findByRole("heading", { level: 1, name: "Read the user guide" })).toBeTruthy();
    expect(within(screen.getByRole("navigation", { name: "On this page" })).getByRole("link", { name: "Search the guide" }).getAttribute("href")).toBe("#search-the-guide");
    expect(screen.getByRole("button", { name: "Print" })).toBeTruthy();
    await noViolations(document.body);
    page.unmount();
    renderPage(HelpPage, { path: "/help/nothing.here", route: "/help/$key" });
    expect(await screen.findByRole("heading", { level: 1, name: "No such page" })).toBeTruthy();
  });
});
