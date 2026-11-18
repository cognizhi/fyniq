import { fireEvent, render, screen } from "@testing-library/react";
import { Trash2 } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import { Icon } from "../../ui";
import { Button } from "./button";

// QA-UI-01's half for the shared components; its tag waits for the forms' validation messages (task 4.2)
describe("the button", () => {
  it("is a button named by its text, styled by its variant and size, and runs its action once a press", async () => {
    const pressed = vi.fn();
    render(
      <>
        <Button variant="primary" onClick={pressed}>
          Post journal
        </Button>
        <Button>Save draft</Button>
        <Button variant="destructive" size="sm">
          Void invoice
        </Button>
      </>,
    );
    const post = screen.getByRole("button", { name: "Post journal" });
    expect(post.className).toContain("bg-primary");
    expect(screen.getByRole("button", { name: "Save draft" }).className).toContain("bg-surface-1"); // secondary by default
    const voiding = screen.getByRole("button", { name: "Void invoice" });
    expect(voiding.className).toContain("text-destructive");
    expect(voiding.className).toContain("h-8");
    fireEvent.click(post);
    expect(pressed).toHaveBeenCalledTimes(1);
    await noViolations(document.body);
  });

  it("does nothing while disabled, and an icon-only button keeps its name", async () => {
    const pressed = vi.fn();
    render(
      <>
        <Button disabled onClick={pressed}>
          Post journal
        </Button>
        <Button size="icon" aria-label="Remove the line">
          <Icon of={Trash2} size={20} />
        </Button>
      </>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Post journal" }));
    expect(pressed).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Remove the line" }).querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
    await noViolations(document.body);
  });
});
