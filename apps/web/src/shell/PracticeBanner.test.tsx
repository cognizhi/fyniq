import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { PracticeBanner } from "./PracticeBanner";

// req: FR-UX-36
describe("PracticeBanner", () => {
  it("says nothing in a real organisation", () => {
    answer({});
    renderPage(() => <PracticeBanner practice={false} />);
    expect(screen.queryByText("Practice organisation")).toBeNull();
  });

  it("labels a practice organisation and takes the person back to the one that keeps their books; axe finds nothing", async () => {
    const back = vi.fn();
    const seen = answer({
      "GET /v1/session/organisations": { status: 200, body: { items: [{ id: "o1", name: "Northwind Pte Ltd", practice: false, current: false }, { id: "o2", name: "Practice: Northwind", practice: true, current: true }] } },
      "POST /v1/session/organisation": { status: 200, body: { id: "o1", name: "Northwind Pte Ltd", practice: false, current: true } },
    });
    renderPage(() => <PracticeBanner practice onSwitched={back} />);
    expect(await screen.findByText("Practice organisation")).toBeTruthy();
    expect(screen.getByText(/Nothing here is real/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(await screen.findByRole("button", { name: "Back to Northwind Pte Ltd" }));
    await vi.waitFor(() => expect(back).toHaveBeenCalled());
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ organisation_id: "o1" });
  });
});
