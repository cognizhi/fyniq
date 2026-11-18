import { fireEvent, screen } from "@testing-library/react";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { renderPage } from "../test/render";
import { entryOf, glossary, parseGlossary } from "./glossary";
import { HelpInfo } from "./HelpInfo";
import { slug } from "./links";
import { Term } from "./Term";

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.(test|stories)\.tsx$/.test(name) ? [path] : [];
  });
}

// The glossary terms of FR-UX-11 (experience/navigation); its tag waits for the "What happened" helper and the empty
// states, which complete it.
describe("the glossary", () => {
  it("reads every term of docs/user-guide/glossary.md with its definition and its heading's anchor", () => {
    expect(glossary.size).toBeGreaterThanOrEqual(20);
    for (const e of glossary.values()) {
      expect(e.definition.length, e.term).toBeGreaterThan(20);
      expect(e.anchor).toBe(slug(e.term));
    }
    const parsed = parseGlossary("---\nkey: x\n---\n# Title\n\nIntro.\n\n## Accrual\n\nIn the month\nit belongs to.\n\nFor example, this.\n");
    expect(parsed.get("accrual")).toEqual({ term: "Accrual", anchor: "accrual", definition: "In the month it belongs to. For example, this." });
  });

  it("has every term a page names", () => {
    const root = resolve(__dirname, "..");
    const named = sources(root).flatMap((p) => [...readFileSync(p, "utf8").matchAll(/<Term name="([^"]+)"/g)].map((m) => m[1] ?? ""));
    expect(named.length).toBeGreaterThan(0);
    expect(named.filter((n) => !entryOf(n))).toEqual([]);
  });

  it("shows a term's definition from its words, with the way to the glossary", async () => {
    renderPage(() => (
      <p>
        It waits in <Term name="Suspense">suspense</Term>.
      </p>
    ));
    fireEvent.click(await screen.findByRole("button", { name: "suspense" }));
    const pop = screen.getByRole("dialog", { name: "Suspense" });
    expect(pop.textContent).toContain("until someone says what it is for");
    expect(screen.getByRole("link", { name: "Learn more in the glossary" }).getAttribute("href")).toBe("/help/reference.glossary#suspense");
  });
});

// req: experience/help FR-UX-30 Tooltips on every page: Wording by mode
// req: experience/help FR-UX-30 Tooltips on every page: Info popover content
describe("an ⓘ from a help key", () => {
  it("says what it is, why it matters and an example in Basic, adds the accounting detail in Expert, and links its guide page", async () => {
    const basic = renderPage(() => <HelpInfo help="home.books-health" mode="basic" />, { mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "About Books health" }));
    let pop = screen.getByRole("dialog", { name: "Books health" });
    expect(pop.textContent).toContain("Fyniq runs checks on the books");
    expect(pop.textContent).toContain("For example: a bank line left in suspense");
    expect(pop.textContent).not.toContain("worst status");
    expect(screen.getByRole("link", { name: "Learn more" }).getAttribute("href")).toBe("/help/checks.overview");
    basic.unmount();
    renderPage(() => <HelpInfo help="home.books-health" mode="expert" />);
    fireEvent.click(await screen.findByRole("button", { name: "About Books health" }));
    pop = screen.getByRole("dialog", { name: "Books health" });
    expect(pop.textContent).toContain("The score is the worst status among the checks that ran");
  });
});
