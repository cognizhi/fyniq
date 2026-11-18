import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { setLedgerView, useLedgerView, viewWords } from "../view";
import type { Mode } from "../registry";
import { LedgerViewToggle } from "./LedgerViewToggle";

function Columns({ mode }: { mode: Mode }) {
  const w = viewWords(useLedgerView(mode));
  return <p>{`${w.in} / ${w.out} / ${w.what}`}</p>;
}

// req: FR-UX-08
// req: experience/navigation FR-UX-08 Plain view and Accountant view
describe("Plain view and Accountant view", () => {
  it("switches the columns between Money in / Money out / Category and Debit / Credit / Account in Expert; axe finds nothing", async () => {
    setLedgerView("plain");
    const { container } = render(
      <>
        <LedgerViewToggle mode="expert" />
        <Columns mode="expert" />
      </>,
    );
    expect(screen.getByText("Money in / Money out / Category")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "Accountant view" }));
    expect(screen.getByText("Debit / Credit / Account")).toBeTruthy();
    expect(window.localStorage.getItem("fyniq.view")).toBe("accountant");
    fireEvent.keyDown(screen.getByRole("radio", { name: "Accountant view" }), { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: "Plain view" }).getAttribute("aria-checked")).toBe("true");
    await noViolations(container);
  });

  // req: experience/navigation FR-UX-08 Plain view and Accountant view: Only in Expert mode
  it("uses Plain view in Basic mode and offers no toggle, whatever was chosen in Expert", () => {
    setLedgerView("accountant");
    render(
      <>
        <LedgerViewToggle mode="basic" />
        <Columns mode="basic" />
      </>,
    );
    expect(screen.queryByRole("radiogroup")).toBeNull();
    expect(screen.getByText("Money in / Money out / Category")).toBeTruthy();
    setLedgerView("plain");
  });
});
