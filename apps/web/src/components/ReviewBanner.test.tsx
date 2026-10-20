import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import register from "../../../../docs/prd/reviews/review-status.json";
import { noViolations } from "../test/axe";
import { ReviewBanner, reviewsFor } from "./ReviewBanner";

// req: FR-UX-41
// req: QA-UI-09
describe("the Needs additional verification banner", () => {
  it("shows on a screen the register lists while its area is pending, with the reviewer's message", async () => {
    const pending = register.areas.filter((a) => a.status === "pending" && a.screens.includes("statutory-packs"));
    render(<ReviewBanner screen="statutory-packs" />);
    if (pending.length === 0) {
      expect(screen.queryByRole("note")).toBeNull();
      return;
    }
    const note = screen.getByRole("note", { name: "Needs additional verification" });
    for (const a of pending) expect(note.textContent).toContain(a.message);
    await noViolations(document.body);
  });

  it("never shows on a screen the register does not list, or one it never shows on", () => {
    expect(reviewsFor("profile-preferences")).toEqual([]);
    for (const s of register.never) expect(reviewsFor(s)).toEqual([]);
    render(<ReviewBanner screen="profile-preferences" />);
    expect(screen.queryByRole("note")).toBeNull();
  });
});
