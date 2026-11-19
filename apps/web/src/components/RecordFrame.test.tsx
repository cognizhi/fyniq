import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { RecordFrame, RecordHeader, recordTabs } from "./RecordFrame";

const all = { details: <p>Details here</p>, lines: <p>Lines here</p>, ledger: <p>Ledger here</p>, attachments: <p>Files here</p>, activity: <p>Activity here</p>, related: <p>Related here</p> };

function page(props: Partial<Parameters<typeof RecordFrame>[0]> = {}) {
  let opened = "";
  const view = render(
    <RecordFrame
      header={
        <RecordHeader title="Bill BILL-0007" number="BILL-0007" status={<span className="fy-chip">Posted</span>} contact="Atlassian Pty Ltd" amounts={[{ label: "Total", value: "1,090.00 SGD" }, { label: "Open", value: "0.00 SGD" }]}
          primary={<button type="button">Record payment</button>} more={[{ label: "Void", onSelect: () => (opened = "void") }, { label: "Duplicate", onSelect: () => (opened = "duplicate") }]} />
      }
      tabs={all}
      panel={<p>Two checks pass</p>}
      {...props}
    />,
  );
  return { ...view, opened: () => opened };
}

// req: FR-UX-13
// req: experience/navigation Record page anatomy
describe("RecordFrame", () => {
  beforeEach(() => localStorage.clear());

  it("has the header, the six tabs in order and a right panel, and axe finds nothing", async () => {
    page();
    expect(screen.getByRole("heading", { level: 1, name: "Bill BILL-0007" })).toBeTruthy();
    expect(screen.getByText("Posted")).toBeTruthy();
    expect(within(screen.getByLabelText("Key amounts")).getByText("1,090.00 SGD")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Record payment" })).toBeTruthy();
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(recordTabs.map(([, label]) => label));
    expect(screen.getByRole("tab", { name: "Details" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tabpanel").textContent).toBe("Details here");
    expect(screen.getByRole("complementary", { name: "Checks, suggestions and context" }).textContent).toBe("Two checks pass");
    await noViolations(document.body);
  });

  it("moves between tabs with the arrow keys, Home and End, and a tab with no content is left out", () => {
    page({ tabs: { ...all, lines: undefined } });
    expect(screen.queryByRole("tab", { name: "Lines" })).toBeNull();
    const details = screen.getByRole("tab", { name: "Details" });
    details.focus();
    fireEvent.keyDown(details, { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "Ledger impact" }).getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Ledger impact" }));
    expect(screen.getByRole("tabpanel").textContent).toBe("Ledger here");
    fireEvent.keyDown(document.activeElement!, { key: "End" });
    expect(screen.getByRole("tabpanel").textContent).toBe("Related here");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(screen.getByRole("tabpanel").textContent).toBe("Details here");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
    expect(screen.getByRole("tabpanel").textContent).toBe("Related here");
    expect(screen.getByRole("tab", { name: "Details" }).getAttribute("tabindex")).toBe("-1");
  });

  it("collapses the right panel and remembers it", () => {
    const first = page();
    const toggle = screen.getByRole("button", { name: "Hide panel" });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Show panel" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("complementary")).toBeNull();
    first.unmount();
    page();
    expect(screen.getByRole("button", { name: "Show panel" })).toBeTruthy();
    expect(screen.getByRole("tabpanel")).toBeTruthy();
  });

  it("holds the other actions behind a ⋯ menu", async () => {
    const view = page();
    fireEvent.click(screen.getByRole("button", { name: "More actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Duplicate" }));
    expect(view.opened()).toBe("duplicate");
  });

  it("opens on the tab it is asked to, and without a panel the body takes the width", () => {
    page({ initial: "activity", panel: undefined });
    expect(screen.getByRole("tabpanel").textContent).toBe("Activity here");
    expect(screen.queryByRole("button", { name: /panel/ })).toBeNull();
  });
});
