import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import { Button } from "./button";
import { Dialog } from "./dialog";

// QA-UI-01's half for the shared components; its tag waits for the forms' validation messages (task 4.2)
describe("the dialog", () => {
  it("is a dialog named by its title and described by its description; focus moves into it, and Esc or Close closes it", async () => {
    const changed = vi.fn();
    render(
      <Dialog open onOpenChange={changed} title="Void INV-0042?" description="Voiding posts a reversal dated today." footer={<Button>Keep it</Button>}>
        <p>S$1,090.00 to Acme Analytics Pte Ltd.</p>
      </Dialog>,
    );
    const dialog = await screen.findByRole("dialog", { name: "Void INV-0042?" });
    expect(dialog.getAttribute("aria-describedby")).toBeTruthy();
    expect(document.getElementById(dialog.getAttribute("aria-describedby")!)?.textContent).toBe("Voiding posts a reversal dated today.");
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    await noViolations(document.body);
    fireEvent.keyDown(document.activeElement ?? dialog, { key: "Escape" });
    await waitFor(() => expect(changed).toHaveBeenCalledWith(false));
    changed.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(changed).toHaveBeenCalledWith(false));
  });

  it("while a ceremony runs, has no Close and ignores Esc", async () => {
    const changed = vi.fn();
    render(<Dialog open onOpenChange={changed} title="Signing with your passkey" dismissible={false} />);
    const dialog = await screen.findByRole("dialog", { name: "Signing with your passkey" });
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
    fireEvent.keyDown(dialog, { key: "Escape" });
    await new Promise((r) => setTimeout(r, 50));
    expect(changed).not.toHaveBeenCalled();
  });
});
