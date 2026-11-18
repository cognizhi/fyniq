import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../test/axe";
import { stepsFor, variantOf, variants, type Step } from "./steps";
import { Tour } from "./Tour";

const steps: Step[] = [
  { id: "a", target: "a", title: "First", text: "The first thing." },
  { id: "hidden", target: "hidden", title: "Hidden", text: "Not shown at this size." },
  { id: "b", target: "b", title: "Second", text: "The second thing." },
  { id: "c", target: "c", title: "Third", text: "The third thing." },
];

// Page draws the marked elements; jsdom has no layout, so the shown ones get a size.
function Page() {
  return (
    <main>
      <h1>Home</h1>
      <button type="button">Where I was</button>
      <span data-tour="a">A</span>
      <span data-tour="hidden">hidden</span>
      <span data-tour="b">B</span>
      <span data-tour="c">C</span>
    </main>
  );
}

function sized() {
  for (const el of document.querySelectorAll<HTMLElement>("[data-tour], button")) {
    const w = el.dataset.tour === "hidden" ? 0 : 40;
    el.getBoundingClientRect = () => ({ x: 10, y: 10, left: 10, top: 10, width: w, height: w ? 20 : 0, right: 10 + w, bottom: w ? 30 : 10, toJSON: () => ({}) });
  }
}

// req: FR-UX-34
// req: QA-UI-06
describe("the first-run tour", () => {
  it("skips what is not shown, moves to its title, steps with the keys and the buttons, and announces each step; axe finds nothing", async () => {
    const page = render(<Page />);
    sized();
    const back = screen.getByRole("button", { name: "Where I was" });
    back.focus();
    const onStep = vi.fn();
    const onClose = vi.fn();
    render(<Tour steps={steps} start={0} onStep={onStep} onClose={onClose} />, { container: page.container.appendChild(document.createElement("div")) });
    const card = screen.getByRole("dialog", { name: "First" });
    expect(card.textContent).toContain("1 of 3");
    expect(document.activeElement).toBe(screen.getByRole("heading", { name: "First" }));
    expect(screen.getByRole("button", { name: "Back" }).hasAttribute("disabled")).toBe(true);
    await noViolations(document.body);
    fireEvent.keyDown(card, { key: "ArrowRight" });
    expect(screen.getByRole("dialog", { name: "Second" }).textContent).toContain("2 of 3"); // Hidden was skipped
    expect(screen.getByRole("status").textContent).toBe("Step 2 of 3. Second. The second thing.");
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "ArrowLeft" });
    expect(screen.getByRole("dialog", { name: "First" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByRole("button", { name: "Done" })).toBeTruthy();
    expect(onStep.mock.calls.map((c) => c[0])).toEqual([0, 1, 0, 1, 2]);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onClose).toHaveBeenCalledWith("finished", 2);
    expect(document.activeElement).toBe(back); // focus returns where it was
  });

  it("closes with Esc or Skip tour, saying where it stopped, and resumes at a step", () => {
    const page = render(<Page />);
    sized();
    const onClose = vi.fn();
    const tour = render(<Tour steps={steps} start={1} onStep={() => undefined} onClose={onClose} />, { container: page.container.appendChild(document.createElement("div")) });
    expect(screen.getByRole("dialog", { name: "Second" })).toBeTruthy(); // resumed at the second shown step
    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(onClose).toHaveBeenCalledWith("skipped", 1);
    tour.unmount();
    const skip = vi.fn();
    render(<Tour steps={steps} start={0} onStep={() => undefined} onClose={skip} />, { container: page.container.appendChild(document.createElement("div")) });
    fireEvent.click(screen.getByRole("button", { name: "Skip tour" }));
    expect(skip).toHaveBeenCalledWith("skipped", 0);
  });

  it("has a variant of at most seven steps for each role", () => {
    for (const v of Object.values(variants)) expect(v.length).toBeLessThanOrEqual(7);
    expect(variantOf(["owner", "controller"])).toBe("founder");
    expect(variantOf(["accountant"])).toBe("accountant");
    expect(variantOf(["employee"])).toBe("employee");
    expect(variantOf(["approver"])).toBe("approver");
    expect(variantOf(["board_viewer"])).toBe("board");
    expect(stepsFor(["employee"]).map((s) => s.id)).toContain("claims");
    expect(stepsFor(["employee"]).map((s) => s.id)).not.toContain("inbox");
  });
});
