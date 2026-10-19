import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../../test/axe";
import { AiBadge } from "./AiBadge";

// req: experience/visual-design AI accent
describe("the AI badge", () => {
  it("says AI in words beside the sparkles icon, on the violet tokens", async () => {
    const { container } = render(
      <p>
        <AiBadge /> Suggested category
      </p>,
    );
    const badge = container.querySelector("span");
    expect(badge?.textContent).toBe("AI");
    expect(badge?.className).toMatch(/\bbg-ai-subtle\b/);
    expect(badge?.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
    await noViolations(container);
  });
});
