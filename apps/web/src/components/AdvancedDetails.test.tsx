import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { forgetMode, setMode } from "../mode";
import { noViolations } from "../test/axe";
import { AdvancedDetails } from "./AdvancedDetails";

const details = [
  { label: "Line 1 · Department", value: "DEL Delivery" },
  { label: "Line 2 · Discount", value: "10%" },
];

// req: FR-UX-25
// req: experience/modes FR-UX-25 Nothing silently hidden on records
describe("advanced details", () => {
  it("shows a record's Expert values in Basic, read-only, with the way to Expert mode; axe finds nothing", async () => {
    forgetMode();
    const { container } = render(<AdvancedDetails mode="basic" details={details} />);
    const toggle = screen.getByRole("button", { name: "Has advanced details (2)" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const group = screen.getByRole("group", { name: "Has advanced details (2)" });
    expect(within(group).getByText("DEL Delivery")).toBeTruthy();
    expect(within(group).queryByRole("textbox")).toBeNull();
    await noViolations(container);
    fireEvent.click(screen.getByRole("button", { name: "Open in Expert mode" }));
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
    setMode("basic");
  });

  it("shows nothing in Expert mode, or for a record with no such values", () => {
    const { container } = render(<AdvancedDetails mode="expert" details={details} />);
    expect(container.textContent).toBe("");
    const none = render(<AdvancedDetails mode="basic" details={[]} />);
    expect(none.container.textContent).toBe("");
  });
});
