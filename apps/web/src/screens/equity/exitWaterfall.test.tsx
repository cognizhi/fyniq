import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ExitWaterfall } from "./ExitWaterfall";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const owner = ownerSession as Session;
const klass = (name: string, total: string, extra: Record<string, unknown> = {}) => ({
  class_id: name, name, has_preference: false, preference: sgd("0.00"), residual: sgd(total), total: sgd(total), converts: false,
  holders: [{ holder_id: `h-${name}`, holder_name: `${name} holder`, shares: "1000000", invested: sgd("0.00"), total: sgd(total) }], ...extra,
});
const body = {
  available: true, currency: "SGD", on: "2026-10-15", exit_value: sgd("5000000.00"),
  classes: [klass("Series Seed", "2000000.00", { has_preference: true, multiple: "1", seniority: 1, preference: sgd("2000000.00") }), klass("Ordinary shares", "3000000.00")],
  as_converted: [klass("Series Seed", "555555.56", { has_preference: true, multiple: "1" }), klass("Ordinary shares", "4444444.44")],
  notes: ["Series Seed has anti-dilution protection (broad based), and no adjustment of its conversion is recorded, so none is applied [lawyer to confirm]."],
  modelled: ["An uncapped participating preference"],
};

// req: FR-EQ-29
describe("the exit waterfall page", () => {
  it("shows each class under the preferences and as converted, with what it leaves out; axe finds nothing", async () => {
    answer({ "GET /v1/exit-waterfall": { status: 200, body } });
    renderPage(ExitWaterfall, { path: "/equity/exit", session: owner, mode: "expert" });
    const t = await screen.findByRole("table", { name: "Under the preferences" });
    expect(within(t).getByText(/Series Seed · 1x/)).toBeTruthy();
    expect(within(t).getAllByText("2,000,000.00").length).toBeGreaterThan(0);
    expect(within(screen.getByRole("table", { name: /As converted/ })).getAllByText("555,555.56").length).toBeGreaterThan(0);
    expect(screen.getByText(/anti-dilution protection/)).toBeTruthy();
    expect(seen.some((s) => s.path === "/v1/exit-waterfall" && s.query.includes("exit_value=8000000.00"))).toBe(true);
    await noViolations(document.body);
  });

  it("reads the waterfall over a round scenario with the terms asked for, and says the round is not recorded (rev 0.604)", async () => {
    answer({
      "GET /v1/round-scenarios": { status: 200, body: { items: [{ id: "sc-1", name: "Series A", status: "draft" }] } },
      "GET /v1/exit-waterfall": { status: 200, body: { ...body, round: { scenario_id: "sc-1", name: "Series A", on: "2026-10-15", price_per_share: sgd("4.00"), raised: sgd("4000000.00"), multiple: "1",
        participating: false, seniority: 1 }, notes: ["New round, Series A (not recorded) is not recorded: its class is modelled with the terms asked for"] } },
    });
    renderPage(ExitWaterfall, { path: "/equity/exit", session: owner, mode: "expert" });
    fireEvent.change(await screen.findByLabelText("Round scenario"), { target: { value: "sc-1" } });
    fireEvent.change(await screen.findByLabelText("Its preference multiple"), { target: { value: "1.5" } });
    fireEvent.click(screen.getByLabelText("It then shares the rest as converted"));
    expect(await screen.findByText(/Over the round of Series A, not recorded/)).toBeTruthy();
    await screen.findByRole("table", { name: "Under the preferences" });
    expect(seen.some((s) => s.path === "/v1/exit-waterfall" && s.query.includes("scenario_id=sc-1") && s.query.includes("round_multiple=1.5") && s.query.includes("round_participating=true") &&
      s.query.includes("round_seniority=1"))).toBe(true);
    await noViolations(document.body);
  });

  it("says there is nothing to model while no class has a preference, and names a refused structure", async () => {
    answer({ "GET /v1/exit-waterfall": { status: 200, body: { ...body, available: false, message: "No share class records a liquidation preference.", classes: [], as_converted: [] } } });
    renderPage(ExitWaterfall, { path: "/equity/exit", session: owner, mode: "expert" });
    expect(await screen.findByText("No share class records a liquidation preference.")).toBeTruthy();
    answer({ "GET /v1/exit-waterfall": problem(422, "EQ-WF-01", "the class Series B has a capped participating preference", "Record only the structures the waterfall models") });
    fireEvent.change(screen.getByLabelText(/Exit value/), { target: { value: "9000000.00" } });
    expect(await screen.findByText(/capped participating preference/)).toBeTruthy();
  });
});
