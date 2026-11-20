import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import type { Account } from "../accounting/data";
import { PasteBudget } from "./PasteBudget";

const accounts = [{ id: "a-ads", code: "6110", name: "Advertising" }, { id: "a-rent", code: "6200", name: "Office rent" }] as Account[];

// req: FR-UX-07
// req: FR-UX-20
describe("PasteBudget", () => {
  it("shows a paste of account codes and monthly amounts before it is set, then adds the ready lines", async () => {
    const onAdd = vi.fn();
    render(<PasteBudget open onClose={() => undefined} accounts={accounts} months={["2027-01", "2027-02"]} scale={2} onAdd={onAdd} />);
    const dialog = await screen.findByRole("dialog");
    const grid = within(dialog).getByRole("grid", { name: "Budget lines to add" });
    act(() => (grid.querySelector('[role="gridcell"][tabindex="0"]') as HTMLElement).focus());
    fireEvent.paste(grid, { clipboardData: { getData: () => "6110\t2,000.00\t=1000*2.5\n6200\t3000.00\t3000.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply paste" }));
    // "=1000*2.5" is text from a spreadsheet, not a sum, so its cell does not fit and the row says so.
    expect(within(dialog).getByText(/1 row has a problem/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: /^Add 1 line/ }) as HTMLElement);
    expect(onAdd).not.toHaveBeenCalled(); // blocked until fixed
  });

  it("adds the lines when every used row is ready", async () => {
    const onAdd = vi.fn();
    render(<PasteBudget open onClose={() => undefined} accounts={accounts} months={["2027-01", "2027-02"]} scale={2} onAdd={onAdd} />);
    const dialog = await screen.findByRole("dialog");
    const grid = within(dialog).getByRole("grid");
    act(() => (grid.querySelector('[role="gridcell"][tabindex="0"]') as HTMLElement).focus());
    fireEvent.paste(grid, { clipboardData: { getData: () => "6110\t2,000.00\t2000\n6200\t3000.00\t3000.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply paste" }));
    fireEvent.click(within(dialog).getByRole("button", { name: /^Add 2 lines/ }));
    expect(onAdd).toHaveBeenCalledWith([
      { account_id: "a-ads", amounts: ["2000.00", "2000"] },
      { account_id: "a-rent", amounts: ["3000.00", "3000.00"] },
    ]);
  });
});
