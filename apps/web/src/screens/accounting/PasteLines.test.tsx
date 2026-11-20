import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import type { Account } from "./data";
import { PasteLines } from "./PasteLines";

const accounts = [
  { id: "a-rent", code: "6200", name: "Office rent" },
  { id: "a-ap", code: "2010", name: "Accounts payable" },
] as Account[];

// req: FR-UX-07
// req: FR-UX-20
describe("PasteLines", () => {
  it("shows a pasted table before it is set, then adds the ready lines to the journal", async () => {
    const onAdd = vi.fn();
    render(<PasteLines open onClose={() => undefined} accounts={accounts} base="SGD" onAdd={onAdd} />);
    const dialog = await screen.findByRole("dialog");
    const grid = within(dialog).getByRole("grid", { name: "Journal lines to add" });
    act(() => (grid.querySelector('[role="gridcell"][tabindex="0"]') as HTMLElement).focus());
    const add = within(dialog).getByRole("button", { name: /^Add 0 lines/ }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    fireEvent.paste(grid, { clipboardData: { getData: () => "6200\tOctober rent\t(3,000.00)\t\n2010\tOctober rent\t\t3000.00" } });
    const preview = within(dialog).getByRole("group", { name: "Paste preview" });
    expect(within(preview).getByText(/Paste 8 cells/)).toBeTruthy();
    fireEvent.click(within(preview).getByRole("button", { name: "Apply paste" }));
    // (3,000.00) reads as -3000.00 in a Debit column, which a debit may not be: the row says so and nothing is added yet.
    expect(within(dialog).getByText(/1 row has a problem/)).toBeTruthy();
    expect((within(dialog).getByRole("button", { name: /^Add 1 line/ }) as HTMLButtonElement).disabled).toBe(true);
    await noViolations(document.body);
  });

  it("adds lines whose rows are ready", async () => {
    const onAdd = vi.fn();
    render(<PasteLines open onClose={() => undefined} accounts={accounts} base="SGD" onAdd={onAdd} />);
    const dialog = await screen.findByRole("dialog");
    const grid = within(dialog).getByRole("grid");
    act(() => (grid.querySelector('[role="gridcell"][tabindex="0"]') as HTMLElement).focus());
    fireEvent.paste(grid, { clipboardData: { getData: () => "6200\tOctober rent\t3000.00\t\n2010\tOctober rent\t\t3,000.00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply paste" }));
    const addButton = within(dialog).getByRole("button", { name: /^Add 2 lines/ });
    fireEvent.click(addButton);
    expect(onAdd).toHaveBeenCalledTimes(1);
    const lines = onAdd.mock.calls[0]![0];
    expect(lines.map((l: { account_id: string; debit: string; credit: string; currency: string }) => [l.account_id, l.debit, l.credit, l.currency])).toEqual([
      ["a-rent", "3000.00", "", "SGD"],
      ["a-ap", "", "3000.00", "SGD"],
    ]);
  });
});
