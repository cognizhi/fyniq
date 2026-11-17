import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { HelpPopover } from "./HelpPopover";

function Labelled() {
  return (
    <div>
      <span>Books health</span>
      <HelpPopover title="Books health" learnMore={<a href="#learn">Learn more</a>}>
        <span>How the books stand by the checks that ran.</span>
      </HelpPopover>
      <button type="button">Elsewhere</button>
    </div>
  );
}

// req: FR-UX-30
// req: experience/help FR-UX-30 Tooltips on every page
describe("help beside what it explains", () => {
  it("opens on hover and on focus, beside a label it never replaces, and names itself after it; axe finds nothing", async () => {
    const { container } = render(<Labelled />);
    expect(screen.getByText("Books health")).toBeTruthy();
    const info = screen.getByRole("button", { name: "About Books health" });
    expect(info.className).toContain("fy-info"); // the kit's 24 × 24 px target (SC 2.5.8)
    fireEvent.mouseEnter(info.parentElement!);
    const pop = screen.getByRole("dialog", { name: "Books health" });
    expect(pop.textContent).toContain("How the books stand");
    expect(info.getAttribute("aria-controls")).toBe(pop.id);
    fireEvent.mouseLeave(info.parentElement!);
    expect(screen.queryByRole("dialog")).toBeNull();
    act(() => info.focus());
    expect(screen.getByRole("dialog", { name: "Books health" })).toBeTruthy();
    await noViolations(container);
  });

  it("stays while the pointer is over it, pins on a click or tap, and Esc closes it and returns focus", () => {
    render(<Labelled />);
    const info = screen.getByRole("button", { name: "About Books health" });
    fireEvent.click(info);
    expect(info.getAttribute("aria-expanded")).toBe("true");
    fireEvent.mouseLeave(info.parentElement!);
    expect(screen.getByRole("dialog")).toBeTruthy(); // pinned
    const more = screen.getByRole("link", { name: "Learn more" });
    act(() => more.focus());
    fireEvent.keyDown(more, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(info);
    expect(info.getAttribute("aria-expanded")).toBe("false");
  });

  it("closes a pinned one on a click outside it", () => {
    render(<Labelled />);
    fireEvent.click(screen.getByRole("button", { name: "About Books health" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    fireEvent.pointerDown(screen.getByRole("button", { name: "Elsewhere" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens from a glossary term's own words", () => {
    render(
      <p>
        It waits in{" "}
        <HelpPopover title="Suspense" term="suspense">
          <span>The account where money waits.</span>
        </HelpPopover>
        .
      </p>,
    );
    const term = screen.getByRole("button", { name: "suspense" });
    expect(term.className).toContain("fy-term");
    fireEvent.click(term);
    expect(screen.getByRole("dialog", { name: "Suspense" }).textContent).toContain("The account where money waits.");
  });
});
