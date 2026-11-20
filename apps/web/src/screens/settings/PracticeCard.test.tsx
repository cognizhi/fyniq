import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PracticeCard } from "./PracticeCard";

const real = { id: "o1", name: "Northwind Pte Ltd", practice: false, current: true };
const practice = { id: "o2", name: "Practice: Northwind Pte Ltd", practice: true, current: false };

// req: FR-UX-36
describe("PracticeCard", () => {
  it("lets an Owner open one practice organisation, then offers to switch to it; axe finds nothing", async () => {
    let items = [real];
    const seen = answer({
      "GET /v1/session/organisations": () => ({ status: 200, body: { items } }),
      "POST /v1/organisation/practice": () => {
        items = [real, practice];
        return { status: 201, body: { id: "o2", name: practice.name, base_currency: "SGD", practice: true } };
      },
    });
    renderPage(() => <PracticeCard mayOpen />);
    const open = await screen.findByRole("button", { name: "Open a practice organisation" });
    await noViolations(document.body);
    fireEvent.click(open);
    expect(await screen.findByText(/Practice: Northwind Pte Ltd is open/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("Idempotency-Key")).toBeTruthy();
    const list = await screen.findByRole("list", { name: "Your organisations" });
    expect(within(list).getByText("Practice")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Open a practice organisation" }) as HTMLButtonElement).disabled).toBe(true); // one at a time
    expect(screen.getByText("You hold one already.")).toBeTruthy();
  });

  it("switches to another organisation", async () => {
    const switched = vi.fn();
    const seen = answer({
      "GET /v1/session/organisations": { status: 200, body: { items: [real, practice] } },
      "POST /v1/session/organisation": { status: 200, body: { ...practice, current: true } },
    });
    renderPage(() => <PracticeCard mayOpen={false} onSwitched={switched} />);
    fireEvent.click(await screen.findByRole("button", { name: "Switch to Practice: Northwind Pte Ltd" }));
    await vi.waitFor(() => expect(switched).toHaveBeenCalled());
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ organisation_id: "o2" });
  });

  it("shows nothing to someone who may not open one and belongs to one organisation", async () => {
    answer({ "GET /v1/session/organisations": { status: 200, body: { items: [real] } } });
    renderPage(() => <PracticeCard mayOpen={false} />);
    await vi.waitFor(() => expect(screen.queryByText("Practice organisation")).toBeNull());
  });
});
