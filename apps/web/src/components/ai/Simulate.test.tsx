import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { CashSimulate } from "./Simulate";

const projection = (lowest: string, on: string, below?: string) => ({ today: "2026-11-15", days: 90, currency: "SGD", opening: { amount: "50000.00", currency: "SGD" },
  lowest: { amount: lowest, currency: "SGD" }, lowest_on: on, below_buffer_on: below, buffer: {}, daily: [], weekly: [], not_included: [],
  movements: [{ key: "b-1", kind: "bill", id: "b-1", number: "S-77", expected_on: "2026-11-20", basis: "due", amount: { amount: "-8000.00", currency: "SGD" } }] });

// req: FR-PLN-08
describe("Simulate", () => {
  it("moves the bill asked about to another day through the what-if, which saves nothing; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/cash-projection": { status: 200, body: projection("12000.00", "2026-11-20", "2026-11-20") },
      "POST /v1/cash-projection:what-if": { status: 200, body: projection("20000.00", "2026-12-31") },
    });
    renderPage(() => <CashSimulate context={{ kind: "bill", id: "b-1", label: "Bill S-77" }} />);
    expect(await screen.findByText(/Bill S-77: −8,000.00 SGD, expected on/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("On"), { target: { value: "2026-12-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Simulate" }));
    const table = await screen.findByRole("table", { name: "The cash projection, now and in the what-if" });
    expect(table.textContent).toContain("None");
    expect(seen.find((s) => s.path === "/v1/cash-projection:what-if")?.body).toEqual({ days: 90, moves: [{ key: "b-1", to: "2026-12-15" }] });
    expect(screen.getByText(/Nothing was saved or changed/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("says what to open when nothing is asked about", async () => {
    renderPage(() => <CashSimulate />);
    expect(await screen.findByRole("link", { name: "Cash ahead" })).toBeTruthy();
  });
});
